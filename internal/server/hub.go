package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Hub manages all rooms and routes messages between clients.
type Hub struct {
	mu           sync.RWMutex
	rooms        map[string]*Room
	authAttempts map[string]authAttempt
}

const maxRoomClients = 12
const secureInviteLength = 12
const maxAuthAttempts = 8
const authAttemptWindow = 5 * time.Minute

type authAttempt struct {
	startedAt time.Time
	count     int
}

func NewHub() *Hub {
	return &Hub{
		rooms:        make(map[string]*Room),
		authAttempts: make(map[string]authAttempt),
	}
}

// HandleMessage processes an incoming message from a client.
func (h *Hub) HandleMessage(c *Client, msg *Message) {
	log.Printf("hub handling message type=%s client=%s target=%s", msg.Type, c.ID, msg.Target)
	switch msg.Type {
	case MsgCreateRoom:
		h.handleCreateRoom(c, msg)
	case MsgJoinRoom:
		h.handleJoinRoom(c, msg)
	case MsgLeaveRoom:
		h.handleLeaveRoom(c)
	case MsgChat:
		h.handleChat(c, msg)
	case MsgVideoSource:
		h.handleVideoSource(c, msg)
	case MsgVideoPlay:
		h.handleVideoPlay(c, msg)
	case MsgVideoSeek:
		h.handleVideoSeek(c, msg)
	case MsgVideoSpeed:
		h.handleVideoSpeed(c, msg)
	case MsgWebRTCOffer, MsgWebRTCAnswer, MsgWebRTCICE, MsgWebRTCRequest:
		h.handleWebRTC(c, msg)
	case MsgHostNetworkInfo:
		h.handleHostNetworkInfo(c, msg)
	case MsgP2PManifest:
		h.handleP2PManifest(c, msg)
	case MsgP2PChunkOffer:
		h.handleP2PChunkOffer(c, msg)
	case MsgP2PChunkRequest:
		h.handleP2PChunkRequest(c, msg)
	case MsgP2PStatus:
		h.handleP2PStatus(c, msg)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (h *Hub) handleCreateRoom(c *Client, msg *Message) {
	c.Username = msg.Username

	roomID := generateRoomID()
	secure := isSecureInviteCode(msg.RoomID)
	var inviteHash [sha256.Size]byte
	if secure {
		if msg.AccessToken != msg.RoomID {
			c.SendJSON(&Message{Type: MsgRoomError, Message: "安全邀请码无效"})
			return
		}
		inviteHash = sha256.Sum256([]byte(msg.AccessToken))
	}
	room := &Room{
		ID:         roomID,
		StorageKey: roomStorageKey(roomID, inviteHash, secure),
		HostID:     c.ID,
		Clients:    make(map[string]*Client),
		VideoState: &VideoState{},
		Secure:     secure,
		InviteHash: inviteHash,
	}
	room.Clients[c.ID] = c
	c.Room = room

	h.mu.Lock()
	h.rooms[room.StorageKey] = room
	h.mu.Unlock()

	log.Printf("room created secure=%t hostID=%s username=%s", secure, c.ID, c.Username)

	c.SendJSON(&Message{
		Type:   MsgRoomCreated,
		RoomID: roomID,
		UserID: c.ID,
	})
}

func (h *Hub) handleJoinRoom(c *Client, msg *Message) {
	c.Username = msg.Username
	secureAttempt := isSecureInviteCode(msg.RoomID)
	if secureAttempt && !h.allowAuthAttempt(c.RemoteIP) {
		c.SendJSON(&Message{Type: MsgRoomError, Message: "尝试过于频繁，请稍后再试"})
		return
	}

	roomID := msg.RoomID
	var room *Room
	var ok bool
	// If no room ID specified, auto-join the first available room (for IP-based join)
	if roomID == "" {
		h.mu.RLock()
		for _, candidate := range h.rooms {
			room = candidate
			ok = true
			break
		}
		h.mu.RUnlock()
		if !ok {
			c.SendJSON(&Message{
				Type:    MsgRoomError,
				Message: "没有可用的房间",
			})
			return
		}
	}

	if roomID != "" {
		lookupID := roomStorageKey(roomID, sha256.Sum256([]byte(msg.RoomID)), secureAttempt)
		h.mu.RLock()
		room, ok = h.rooms[lookupID]
		h.mu.RUnlock()
	}

	if !ok {
		log.Printf("join rejected: room not found client=%s username=%s", c.ID, c.Username)
		c.SendJSON(&Message{
			Type:    MsgRoomError,
			Message: "房间不存在",
		})
		return
	}
	if room.Secure {
		providedHash := sha256.Sum256([]byte(msg.AccessToken))
		if !secureAttempt || subtle.ConstantTimeCompare(providedHash[:], room.InviteHash[:]) != 1 {
			c.SendJSON(&Message{Type: MsgRoomError, Message: "安全邀请码无效"})
			return
		}
	}

	room.mu.Lock()
	if len(room.Clients) >= maxRoomClients {
		room.mu.Unlock()
		c.SendJSON(&Message{Type: MsgRoomError, Message: "房间人数已达上限"})
		return
	}
	room.Clients[c.ID] = c
	c.Room = room
	users := make([]UserInfo, 0, len(room.Clients))
	for _, client := range room.Clients {
		users = append(users, UserInfo{
			ID:     client.ID,
			Name:   client.Username,
			IsHost: client.ID == room.HostID,
		})
	}
	state := *room.VideoState
	room.mu.Unlock()

	log.Printf("room joined secure=%t client=%s username=%s users=%d", room.Secure, c.ID, c.Username, len(users))

	c.SendJSON(&Message{
		Type:          MsgRoomJoined,
		RoomID:        room.ID,
		UserID:        c.ID,
		HostID:        room.HostID,
		Users:         users,
		Source:        state.Source,
		SourceType:    state.SourceType,
		ChunkManifest: state.ChunkManifest,
		Playing:       state.Playing,
		CurrentTime:   state.CurrentTime,
		Speed:         state.Speed,
	})

	// Notify others
	room.BroadcastExcept(c.ID, &Message{
		Type:     MsgUserJoined,
		UserID:   c.ID,
		Username: c.Username,
	})
}

func (h *Hub) allowAuthAttempt(remoteIP string) bool {
	key := strings.TrimSpace(remoteIP)
	if key == "" {
		key = "unknown"
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.authAttempts[key]
	if entry.startedAt.IsZero() || now.Sub(entry.startedAt) >= authAttemptWindow {
		h.authAttempts[key] = authAttempt{startedAt: now, count: 1}
		return true
	}
	if entry.count >= maxAuthAttempts {
		return false
	}
	entry.count++
	h.authAttempts[key] = entry
	return true
}

func isSecureInviteCode(value string) bool {
	if len(value) != secureInviteLength {
		return false
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!#$%&()*+-;<=>?@^_`{|}~"
	for i := 0; i < len(value); i++ {
		if !strings.ContainsRune(alphabet, rune(value[i])) {
			return false
		}
	}
	return true
}

func roomStorageKey(roomID string, inviteHash [sha256.Size]byte, secure bool) string {
	if secure {
		return "secure:" + fmt.Sprintf("%x", inviteHash[:])
	}
	return "direct:" + roomID
}

func (h *Hub) handleLeaveRoom(c *Client) {
	if c.Room == nil {
		return
	}
	h.removeClient(c)
}

func (h *Hub) handleChat(c *Client, msg *Message) {
	if c.Room == nil {
		return
	}
	c.Room.Broadcast(&Message{
		Type:      MsgChat,
		UserID:    c.ID,
		Username:  c.Username,
		Text:      msg.Text,
		Timestamp: time.Now().UnixMilli(),
	})
}

func (h *Hub) handleVideoSource(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID {
		return
	}
	c.Room.mu.Lock()
	c.Room.VideoState.Source = msg.Source
	c.Room.VideoState.SourceType = msg.SourceType
	c.Room.VideoState.ChunkManifest = msg.ChunkManifest
	c.Room.mu.Unlock()

	c.Room.BroadcastExcept(c.ID, &Message{
		Type:          MsgVideoSource,
		Source:        msg.Source,
		SourceType:    msg.SourceType,
		ChunkManifest: msg.ChunkManifest,
	})
}

func (h *Hub) handleVideoPlay(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID {
		return
	}
	c.Room.mu.Lock()
	c.Room.VideoState.Playing = msg.Playing
	c.Room.VideoState.CurrentTime = msg.CurrentTime
	c.Room.mu.Unlock()

	c.Room.BroadcastExcept(c.ID, &Message{
		Type:        MsgVideoPlay,
		Playing:     msg.Playing,
		CurrentTime: msg.CurrentTime,
	})
}

func (h *Hub) handleVideoSeek(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID {
		return
	}
	c.Room.mu.Lock()
	c.Room.VideoState.CurrentTime = msg.CurrentTime
	c.Room.mu.Unlock()

	c.Room.BroadcastExcept(c.ID, &Message{
		Type:        MsgVideoSeek,
		CurrentTime: msg.CurrentTime,
	})
}

func (h *Hub) handleVideoSpeed(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID {
		return
	}
	c.Room.mu.Lock()
	c.Room.VideoState.Speed = msg.Speed
	c.Room.mu.Unlock()

	c.Room.BroadcastExcept(c.ID, &Message{
		Type:  MsgVideoSpeed,
		Speed: msg.Speed,
	})
}

func (h *Hub) handleWebRTC(c *Client, msg *Message) {
	if c.Room == nil {
		return
	}
	target := c.Room.GetClient(msg.Target)
	if target == nil {
		return
	}
	target.SendJSON(&Message{
		Type:      msg.Type,
		From:      c.ID,
		SDP:       msg.SDP,
		Candidate: msg.Candidate,
		Target:    c.ID,
	})
}

func (h *Hub) handleHostNetworkInfo(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID {
		return
	}
	c.Room.BroadcastExcept(c.ID, &Message{
		Type:            MsgHostNetworkInfo,
		UserID:          c.ID,
		HostCandidates:  msg.HostCandidates,
		PreferredHostIP: msg.PreferredHostIP,
	})
}

func (h *Hub) handleP2PManifest(c *Client, msg *Message) {
	if c.Room == nil || c.Room.HostID != c.ID || msg.ChunkManifestData == "" {
		return
	}
	c.Room.BroadcastExcept(c.ID, &Message{
		Type:              MsgP2PManifest,
		UserID:            c.ID,
		ChunkManifest:     msg.ChunkManifest,
		ChunkManifestData: msg.ChunkManifestData,
	})
}

// handleP2PChunkOffer broadcasts a chunk availability announcement to all other room members.
func (h *Hub) handleP2PChunkOffer(c *Client, msg *Message) {
	if c.Room == nil {
		return
	}
	c.Room.BroadcastExcept(c.ID, &Message{
		Type:   MsgP2PChunkOffer,
		UserID: c.ID,
		Chunks: msg.Chunks,
	})
}

// handleP2PChunkRequest routes a chunk request to a specific peer.
func (h *Hub) handleP2PChunkRequest(c *Client, msg *Message) {
	if c.Room == nil || msg.Target == "" {
		return
	}
	target := c.Room.GetClient(msg.Target)
	if target == nil {
		return
	}
	target.SendJSON(&Message{
		Type:       MsgP2PChunkRequest,
		UserID:     c.ID,
		NeedChunks: msg.NeedChunks,
	})
}

func (h *Hub) handleP2PStatus(c *Client, msg *Message) {
	if c.Room == nil || msg.P2PStatus == nil {
		return
	}
	status := *msg.P2PStatus
	if status.Progress < 0 {
		status.Progress = 0
	}
	if status.Progress > 100 {
		status.Progress = 100
	}
	if status.BytesPerSecond < 0 {
		status.BytesPerSecond = 0
	}
	if status.BufferedSeconds < 0 {
		status.BufferedSeconds = 0
	}
	if status.Downloaded < 0 {
		status.Downloaded = 0
	}
	if status.Total < 0 {
		status.Total = 0
	}
	c.Room.BroadcastExcept(c.ID, &Message{
		Type:      MsgP2PStatus,
		UserID:    c.ID,
		P2PStatus: &status,
	})
}

// removeClient removes a client from its room and handles room shutdown.
func (h *Hub) removeClient(c *Client) {
	room := c.Room
	if room == nil {
		return
	}

	room.mu.Lock()
	delete(room.Clients, c.ID)
	c.Room = nil

	wasHost := room.HostID == c.ID
	remaining := len(room.Clients)

	if remaining == 0 {
		room.mu.Unlock()
		h.mu.Lock()
		delete(h.rooms, room.StorageKey)
		h.mu.Unlock()
		return
	}

	if wasHost {
		remainingClients := make([]*Client, 0, len(room.Clients))
		for _, client := range room.Clients {
			client.Room = nil
			remainingClients = append(remainingClients, client)
		}
		room.mu.Unlock()

		h.mu.Lock()
		delete(h.rooms, room.StorageKey)
		h.mu.Unlock()

		for _, client := range remainingClients {
			client.SendJSON(&Message{
				Type:    MsgRoomClosed,
				Message: "房主已退出，房间已关闭",
			})
		}
		return
	}

	room.mu.Unlock()

	room.Broadcast(&Message{
		Type:   MsgUserLeft,
		UserID: c.ID,
	})
}

// Room represents a watch-together room.
type Room struct {
	ID         string
	StorageKey string
	HostID     string
	Clients    map[string]*Client
	VideoState *VideoState
	Secure     bool
	InviteHash [sha256.Size]byte
	mu         sync.RWMutex
}

// BroadcastExcept sends a message to all clients in the room except the specified one.
func (r *Room) BroadcastExcept(exceptID string, msg *Message) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, c := range r.Clients {
		if id != exceptID {
			c.SendJSON(msg)
		}
	}
}

// Broadcast sends a message to all clients in the room.
func (r *Room) Broadcast(msg *Message) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.Clients {
		c.SendJSON(msg)
	}
}

// GetClient returns a client by ID.
func (r *Room) GetClient(id string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Clients[id]
}

// RemoveClient removes a client from the room.
func (r *Room) RemoveClient(c *Client) {
	r.mu.Lock()
	delete(r.Clients, c.ID)
	r.mu.Unlock()
}

// UserList returns a list of UserInfo for all clients in the room.
func (r *Room) UserList() []UserInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users := make([]UserInfo, 0, len(r.Clients))
	for _, c := range r.Clients {
		users = append(users, UserInfo{
			ID:     c.ID,
			Name:   c.Username,
			IsHost: c.ID == r.HostID,
		})
	}
	return users
}

func generateRoomID() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}
