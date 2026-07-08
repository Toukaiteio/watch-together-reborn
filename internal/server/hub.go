package server

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// Hub manages all rooms and routes messages between clients.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]*Room),
	}
}

// HandleMessage processes an incoming message from a client.
func (h *Hub) HandleMessage(c *Client, msg *Message) {
	log.Printf("hub handling message type=%s client=%s room=%s target=%s", msg.Type, c.ID, msg.RoomID, msg.Target)
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
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (h *Hub) handleCreateRoom(c *Client, msg *Message) {
	c.Username = msg.Username

	roomID := generateRoomID()
	room := &Room{
		ID:         roomID,
		HostID:     c.ID,
		Clients:    make(map[string]*Client),
		VideoState: &VideoState{},
	}
	room.Clients[c.ID] = c
	c.Room = room

	h.mu.Lock()
	h.rooms[roomID] = room
	h.mu.Unlock()

	log.Printf("room created roomID=%s hostID=%s username=%s", roomID, c.ID, c.Username)

	c.SendJSON(&Message{
		Type:   MsgRoomCreated,
		RoomID: roomID,
		UserID: c.ID,
	})
}

func (h *Hub) handleJoinRoom(c *Client, msg *Message) {
	c.Username = msg.Username

	roomID := msg.RoomID
	// If no room ID specified, auto-join the first available room (for IP-based join)
	if roomID == "" {
		h.mu.RLock()
		for id := range h.rooms {
			roomID = id
			break
		}
		h.mu.RUnlock()
		if roomID == "" {
			c.SendJSON(&Message{
				Type:    MsgRoomError,
				Message: "没有可用的房间",
			})
			return
		}
	}

	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		log.Printf("join rejected: room not found roomID=%s client=%s username=%s", roomID, c.ID, c.Username)
		c.SendJSON(&Message{
			Type:    MsgRoomError,
			Message: "房间不存在",
		})
		return
	}

	room.mu.Lock()
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

	log.Printf("room joined roomID=%s client=%s username=%s users=%d", room.ID, c.ID, c.Username, len(users))

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
		delete(h.rooms, room.ID)
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
		delete(h.rooms, room.ID)
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
	HostID     string
	Clients    map[string]*Client
	VideoState *VideoState
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
