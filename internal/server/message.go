package server

import "encoding/json"

// Message represents a WebSocket protocol message exchanged between client and server.
type Message struct {
	Type string `json:"type"`

	// Room & user
	RoomID   string `json:"roomId,omitempty"`
	Username string `json:"username,omitempty"`
	UserID   string `json:"userId,omitempty"`

	// Chat
	Text      string `json:"text,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`

	// Video state
	Source        string  `json:"source,omitempty"`
	SourceType    string  `json:"sourceType,omitempty"`
	ChunkManifest string  `json:"chunkManifest,omitempty"`
	Playing       bool    `json:"playing,omitempty"`
	CurrentTime   float64 `json:"currentTime,omitempty"`
	Speed         float64 `json:"speed,omitempty"`

	// WebRTC signaling
	Target    string          `json:"target,omitempty"`
	From      string          `json:"from,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`

	// P2P chunk coordination
	Chunks     []int `json:"chunks,omitempty"`
	NeedChunks []int `json:"needChunks,omitempty"`

	// Host connectivity hints
	HostCandidates    []string `json:"hostCandidates,omitempty"`
	PreferredHostIP   string   `json:"preferredHostIp,omitempty"`
	ChunkManifestData string   `json:"chunkManifestData,omitempty"`

	// Room info payload
	Users   []UserInfo `json:"users,omitempty"`
	HostID  string     `json:"hostId,omitempty"`
	Message string     `json:"message,omitempty"`
}

// UserInfo represents a user in a room.
type UserInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IsHost bool   `json:"isHost"`
}

// VideoState represents the current state of the video player.
type VideoState struct {
	Source        string  `json:"source"`
	SourceType    string  `json:"sourceType"`
	ChunkManifest string  `json:"chunkManifest,omitempty"`
	Playing       bool    `json:"playing"`
	CurrentTime   float64 `json:"currentTime"`
	Speed         float64 `json:"speed"`
}

const (
	MsgCreateRoom      = "create_room"
	MsgJoinRoom        = "join_room"
	MsgLeaveRoom       = "leave_room"
	MsgRoomCreated     = "room_created"
	MsgRoomJoined      = "room_joined"
	MsgRoomError       = "room_error"
	MsgRoomClosed      = "room_closed"
	MsgUserJoined      = "user_joined"
	MsgUserLeft        = "user_left"
	MsgChat            = "chat"
	MsgVideoSource     = "video_source"
	MsgVideoPlay       = "video_play"
	MsgVideoSeek       = "video_seek"
	MsgVideoSpeed      = "video_speed"
	MsgWebRTCOffer     = "webrtc_offer"
	MsgWebRTCAnswer    = "webrtc_answer"
	MsgWebRTCICE       = "webrtc_ice"
	MsgWebRTCRequest   = "webrtc_request"
	MsgP2PManifest     = "p2p_manifest"
	MsgHostChanged     = "host_changed"
	MsgHostNetworkInfo = "host_network_info"
	MsgP2PChunkOffer   = "p2p_chunk_offer"
	MsgP2PChunkRequest = "p2p_chunk_request"
)
