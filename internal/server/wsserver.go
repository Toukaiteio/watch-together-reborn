package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
	"watch-together-reborn/internal/chunk"
	"watch-together-reborn/internal/network"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSServer is the main server handling WebSocket connections and video file serving.
type WSServer struct {
	defaultPort int
	port        int
	hub         *Hub
	httpServer  *http.Server
	listener    net.Listener
	listener6   net.Listener

	mu        sync.Mutex
	videoPath string
	chunkMgr  *chunk.ChunkManager
	hlsDir    string
}

func NewWSServer(defaultPort int) *WSServer {
	return &WSServer{
		defaultPort: defaultPort,
		hub:         NewHub(),
	}
}

// Start starts the HTTP server on the default port or the next available one.
// Returns the actual port used.
func (s *WSServer) Start() (int, error) {
	port := network.FindAvailablePort(s.defaultPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/video", s.handleVideo)
	mux.HandleFunc("/video/hls/", s.handleVideoHLS)
	mux.HandleFunc("/video/manifest", s.handleVideoManifest)
	mux.HandleFunc("/video/chunk/", s.handleVideoChunk)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{Handler: mux}

	addr4 := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	ln, err := net.Listen("tcp4", addr4)
	if err != nil {
		return 0, fmt.Errorf("failed to listen on %s: %w", addr4, err)
	}

	s.port = port
	s.listener = ln

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("server (v4) error: %v", err)
		}
	}()

	// Also listen on IPv6 (dedicated socket) so remote peers can reach us via
	// IPv6 addresses. On Windows/some Linux setups a dual-stack ::-listener
	// would also cover v4, but binding an explicit tcp6 socket alongside the
	// tcp4 one is the most portable way to guarantee both stacks are served.
	addr6 := net.JoinHostPort("::", strconv.Itoa(port))
	if ln6, err6 := net.Listen("tcp6", addr6); err6 == nil {
		s.listener6 = ln6
		go func() {
			if err := s.httpServer.Serve(ln6); err != nil && err != http.ErrServerClosed {
				log.Printf("server (v6) error: %v", err)
			}
		}()
		log.Printf("WebSocket server listening on 0.0.0.0:%d and [::]:%d", port, port)
	} else {
		log.Printf("WebSocket server listening on 0.0.0.0:%d (IPv6 unavailable: %v)", port, err6)
	}

	return port, nil
}

// Stop stops the server.
func (s *WSServer) Stop() {
	if s.httpServer != nil {
		s.httpServer.Close()
		s.httpServer = nil
	}
	if s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}
	if s.listener6 != nil {
		s.listener6.Close()
		s.listener6 = nil
	}
}

// Port returns the port the server is running on.
func (s *WSServer) Port() int {
	return s.port
}

// IsDefaultPort returns true if the server is using the default port.
func (s *WSServer) IsDefaultPort() bool {
	return s.port == s.defaultPort
}

// SetVideoFile sets the video file to be served at /video.
func (s *WSServer) SetVideoFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.videoPath = path
}

// ClearVideoFile clears the current video file.
func (s *WSServer) ClearVideoFile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.videoPath = ""
}

func (s *WSServer) SetHLSDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hlsDir = dir
}

func (s *WSServer) ClearHLSDir() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hlsDir = ""
}

func (s *WSServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("ws upgrade request from %s to %s", r.RemoteAddr, r.URL.Path)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	log.Printf("ws connected from %s", r.RemoteAddr)

	client := NewClient(s.hub, conn)

	go client.writePump()
	go client.readPump()
}

func (s *WSServer) handleVideo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	path := s.videoPath
	s.mu.Unlock()

	if path == "" {
		http.Error(w, "No video file available", http.StatusNotFound)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeFile(w, r, path)
}

func (s *WSServer) handleVideoHLS(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	dir := s.hlsDir
	s.mu.Unlock()

	if dir == "" {
		http.Error(w, "No HLS video available", http.StatusNotFound)
		return
	}

	name := filepath.Base(r.URL.Path)
	if name == "." || name == "/" || name == "" {
		name = "index.m3u8"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "HLS asset not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length")
	if filepath.Ext(name) == ".m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
	} else if filepath.Ext(name) == ".m4s" {
		w.Header().Set("Content-Type", "video/iso.segment")
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else if filepath.Ext(name) == ".mp4" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	http.ServeFile(w, r, path)
}

// SetChunkManager sets the chunk manager for P2P video serving.
func (s *WSServer) SetChunkManager(mgr *chunk.ChunkManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunkMgr = mgr
}

// ClearChunkManager clears the current chunk manager.
func (s *WSServer) ClearChunkManager() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chunkMgr != nil {
		s.chunkMgr.Cleanup()
		s.chunkMgr = nil
	}
}

func (s *WSServer) handleVideoManifest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mgr := s.chunkMgr
	s.mu.Unlock()

	if mgr == nil {
		http.Error(w, "No video manifest available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(mgr.ManifestJSON())
}

func (s *WSServer) handleVideoChunk(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mgr := s.chunkMgr
	s.mu.Unlock()

	if mgr == nil {
		http.Error(w, "No video chunks available", http.StatusNotFound)
		return
	}

	// Extract chunk index from URL: /video/chunk/{index}
	idxStr := r.URL.Path[len("/video/chunk/"):]
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, "Invalid chunk index", http.StatusBadRequest)
		return
	}

	data, err := mgr.GetChunk(idx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *WSServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
