package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketCreateAndJoinFlow(t *testing.T) {
	srv, wsURL := newTestWebSocketServer(t)

	hostConn := mustDialWS(t, wsURL)
	defer hostConn.Close()

	mustWriteJSON(t, hostConn, Message{
		Type:     MsgCreateRoom,
		Username: "host",
	})

	hostCreated := mustReadMessageType(t, hostConn, MsgRoomCreated)
	if hostCreated.RoomID == "" {
		t.Fatal("expected room_created to include room ID")
	}
	if hostCreated.UserID == "" {
		t.Fatal("expected room_created to include user ID")
	}

	guestConn := mustDialWS(t, wsURL)
	defer guestConn.Close()

	mustWriteJSON(t, guestConn, Message{
		Type:     MsgJoinRoom,
		RoomID:   hostCreated.RoomID,
		Username: "guest",
	})

	guestJoined := mustReadMessageType(t, guestConn, MsgRoomJoined)
	if guestJoined.RoomID != hostCreated.RoomID {
		t.Fatalf("expected joined room %q, got %q", hostCreated.RoomID, guestJoined.RoomID)
	}
	if guestJoined.HostID != hostCreated.UserID {
		t.Fatalf("expected host ID %q, got %q", hostCreated.UserID, guestJoined.HostID)
	}
	if guestJoined.UserID == "" {
		t.Fatal("expected joined guest user ID")
	}
	if len(guestJoined.Users) != 2 {
		t.Fatalf("expected 2 users in joined payload, got %d", len(guestJoined.Users))
	}

	hostUserJoined := mustReadMessageType(t, hostConn, MsgUserJoined)
	if hostUserJoined.UserID != guestJoined.UserID {
		t.Fatalf("expected host to be notified about guest %q, got %q", guestJoined.UserID, hostUserJoined.UserID)
	}
	if hostUserJoined.Username != "guest" {
		t.Fatalf("expected guest username in broadcast, got %q", hostUserJoined.Username)
	}

	srv.Close()
}

func TestWebSocketJoinByIPReturnsRoomErrorWhenNoRoomExists(t *testing.T) {
	srv, wsURL := newTestWebSocketServer(t)
	defer srv.Close()

	conn := mustDialWS(t, wsURL)
	defer conn.Close()

	mustWriteJSON(t, conn, Message{
		Type:     MsgJoinRoom,
		RoomID:   "",
		Username: "guest",
	})

	msg := mustReadMessageType(t, conn, MsgRoomError)
	if msg.Message == "" {
		t.Fatal("expected room_error to include message")
	}
}

func TestWebSocketLateJoinReceivesExistingChunkedVideoSource(t *testing.T) {
	srv, wsURL := newTestWebSocketServer(t)
	defer srv.Close()

	hostConn := mustDialWS(t, wsURL)
	defer hostConn.Close()

	mustWriteJSON(t, hostConn, Message{
		Type:     MsgCreateRoom,
		Username: "host",
	})

	hostCreated := mustReadMessageType(t, hostConn, MsgRoomCreated)
	videoURL := "http://10.126.126.2:55511/video"
	manifestURL := "http://10.126.126.2:55511/video/manifest"

	mustWriteJSON(t, hostConn, Message{
		Type:          MsgVideoSource,
		Source:        videoURL,
		SourceType:    "file",
		ChunkManifest: manifestURL,
	})

	guestConn := mustDialWS(t, wsURL)
	defer guestConn.Close()

	mustWriteJSON(t, guestConn, Message{
		Type:     MsgJoinRoom,
		RoomID:   hostCreated.RoomID,
		Username: "guest",
	})

	guestJoined := mustReadMessageType(t, guestConn, MsgRoomJoined)
	if guestJoined.Source != videoURL {
		t.Fatalf("expected joined payload source %q, got %q", videoURL, guestJoined.Source)
	}
	if guestJoined.SourceType != "file" {
		t.Fatalf("expected joined payload sourceType file, got %q", guestJoined.SourceType)
	}
	if guestJoined.ChunkManifest != manifestURL {
		t.Fatalf("expected joined payload chunkManifest %q, got %q", manifestURL, guestJoined.ChunkManifest)
	}
}

func TestWebSocketSecureInviteRequiresMatchingCapability(t *testing.T) {
	srv, wsURL := newTestWebSocketServer(t)
	defer srv.Close()

	invite := "A1b2C3d4E5f6"
	hostConn := mustDialWS(t, wsURL)
	defer hostConn.Close()
	mustWriteJSON(t, hostConn, Message{
		Type:        MsgCreateRoom,
		RoomID:      invite,
		AccessToken: invite,
		Username:    "host",
	})
	_ = mustReadMessageType(t, hostConn, MsgRoomCreated)

	wrongConn := mustDialWS(t, wsURL)
	defer wrongConn.Close()
	mustWriteJSON(t, wrongConn, Message{
		Type:        MsgJoinRoom,
		RoomID:      invite,
		AccessToken: "Z9y8X7w6V5u4",
		Username:    "wrong",
	})
	wrong := mustReadMessageType(t, wrongConn, MsgRoomError)
	if wrong.Message == "" {
		t.Fatal("expected secure invite rejection message")
	}

	guestConn := mustDialWS(t, wsURL)
	defer guestConn.Close()
	mustWriteJSON(t, guestConn, Message{
		Type:        MsgJoinRoom,
		RoomID:      invite,
		AccessToken: invite,
		Username:    "guest",
	})
	joined := mustReadMessageType(t, guestConn, MsgRoomJoined)
	if joined.UserID == "" {
		t.Fatal("expected matching secure invite to join")
	}
}

func TestMediaAccessTokenProtectsVideo(t *testing.T) {
	video := t.TempDir() + "/video.mp4"
	if err := os.WriteFile(video, []byte("video-data"), 0o600); err != nil {
		t.Fatalf("write test video: %v", err)
	}

	server := NewWSServer(0)
	server.SetVideoFile(video)
	server.SetMediaAccessToken("A1b2C3d4E5f6")

	denied := httptest.NewRecorder()
	server.handleVideo(denied, httptest.NewRequest(http.MethodGet, "/video", nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized media request, got %d", denied.Code)
	}

	allowed := httptest.NewRecorder()
	server.handleVideo(allowed, httptest.NewRequest(http.MethodGet, "/video?access_token=A1b2C3d4E5f6", nil))
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected authorized media request, got %d", allowed.Code)
	}
}

func TestWebSocketRelaysP2PDownloadStatus(t *testing.T) {
	srv, wsURL := newTestWebSocketServer(t)
	defer srv.Close()

	hostConn := mustDialWS(t, wsURL)
	defer hostConn.Close()
	mustWriteJSON(t, hostConn, Message{Type: MsgCreateRoom, Username: "host"})
	hostCreated := mustReadMessageType(t, hostConn, MsgRoomCreated)

	guestConn := mustDialWS(t, wsURL)
	defer guestConn.Close()
	mustWriteJSON(t, guestConn, Message{Type: MsgJoinRoom, RoomID: hostCreated.RoomID, Username: "guest"})
	guestJoined := mustReadMessageType(t, guestConn, MsgRoomJoined)
	_ = mustReadMessageType(t, hostConn, MsgUserJoined)

	mustWriteJSON(t, guestConn, Message{
		Type: MsgP2PStatus,
		P2PStatus: &P2PDownloadStatus{
			State: "downloading", Progress: 42.5, BytesPerSecond: 512 * 1024,
			BufferedSeconds: 90, Downloaded: 12, Total: 30,
		},
	})

	status := mustReadMessageType(t, hostConn, MsgP2PStatus)
	if status.UserID != guestJoined.UserID {
		t.Fatalf("expected status sender %q, got %q", guestJoined.UserID, status.UserID)
	}
	if status.P2PStatus == nil || status.P2PStatus.Progress != 42.5 || status.P2PStatus.BufferedSeconds != 90 {
		t.Fatalf("unexpected relayed P2P status: %#v", status.P2PStatus)
	}
}

func newTestWebSocketServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	wsServer := NewWSServer(0)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsServer.handleWebSocket)
	mux.HandleFunc("/health", wsServer.handleHealth)

	srv := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return srv, wsURL
}

func mustDialWS(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	return conn
}

func mustWriteJSON(t *testing.T, conn *websocket.Conn, msg Message) {
	t.Helper()

	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}

func mustReadMessageType(t *testing.T, conn *websocket.Conn, wantType string) Message {
	t.Helper()

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read JSON: %v", err)
		}
		if msg.Type == wantType {
			return msg
		}
	}
}
