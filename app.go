package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"watch-together-reborn/internal/chunk"
	"watch-together-reborn/internal/discovery"
	"watch-together-reborn/internal/network"
	"watch-together-reborn/internal/passcode"
	"watch-together-reborn/internal/segment"
	"watch-together-reborn/internal/server"
	"watch-together-reborn/internal/torrentproc"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const chunkingProgressEvent = "chunking:progress"

// App is the main application struct exposed to the frontend.
type App struct {
	ctx         context.Context
	wsServer    *server.WSServer
	serverPort  int
	defaultPort int
	isServerUp  bool
	segmenter   *segment.Segmenter
	torrentProc *torrentproc.Process
	lanMu       sync.Mutex
	lanCancel   context.CancelFunc
}

func NewApp() *App {
	return &App{
		defaultPort: 55511,
	}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.wsServer = server.NewWSServer(a.defaultPort)
}

func (a *App) Shutdown(ctx context.Context) {
	a.StopLANRoomBroadcast()
	if a.segmenter != nil {
		a.segmenter.Cleanup()
		a.segmenter = nil
	}
	if a.torrentProc != nil {
		a.torrentProc.Stop()
		a.torrentProc = nil
	}
	if a.wsServer != nil {
		a.wsServer.Stop()
	}
}

// StartServer starts the WebSocket server on the default port or the next available one.
func (a *App) StartServer() (int, error) {
	if a.isServerUp {
		return a.serverPort, nil
	}

	port, err := a.wsServer.Start()
	if err != nil {
		return 0, fmt.Errorf("failed to start server: %w", err)
	}

	a.serverPort = port
	a.isServerUp = true
	return port, nil
}

// StopServer stops the WebSocket server.
func (a *App) StopServer() {
	if a.wsServer != nil {
		a.wsServer.Stop()
		a.isServerUp = false
	}
}

// GetServerPort returns the port the server is running on.
func (a *App) GetServerPort() int {
	return a.serverPort
}

// IsDefaultPort returns true if the server is using the default port (55511).
func (a *App) IsDefaultPort() bool {
	return a.serverPort == a.defaultPort
}

// IsServerRunning returns whether the server is currently running.
func (a *App) IsServerRunning() bool {
	return a.isServerUp
}

// GetLocalIPs returns all non-loopback IPv4 addresses of the machine.
func (a *App) GetLocalIPs() []string {
	return network.GetLocalIPv4s()
}

// GetLocalIPv6s returns public and ULA IPv6 addresses of the machine.
// Link-local, multicast, and other non-routable addresses are filtered out.
func (a *App) GetLocalIPv6s() []string {
	return network.GetLocalIPv6s()
}

// GetIPv6Addresses returns IPv6 addresses with classification metadata.
// Public addresses are listed first (recommended for remote connectivity),
// followed by ULA addresses. Each entry includes whether it's public/ULA.
func (a *App) GetIPv6Addresses() []network.IPv6AddrInfo {
	return network.GetLocalIPv6sWithInfo()
}

// DecodePasscode decodes a passcode into connection information.
func (a *App) DecodePasscode(code string) (*passcode.ConnectionInfo, error) {
	return passcode.Decode(code)
}

// EncodePasscode encodes connection info into a passcode string.
func (a *App) EncodePasscode(ip string, port int, roomID string) string {
	return passcode.Encode(ip, port, roomID)
}

// StartLANRoomBroadcast periodically announces a direct LAN room on UDP.
func (a *App) StartLANRoomBroadcast(roomID string, port int, username string) error {
	a.StopLANRoomBroadcast()

	ips := append(network.GetLocalIPv4s(), network.GetLocalIPv6s()...)
	ctx, cancel := context.WithCancel(context.Background())

	a.lanMu.Lock()
	a.lanCancel = cancel
	a.lanMu.Unlock()

	go func() {
		ticker := time.NewTicker(1200 * time.Millisecond)
		defer ticker.Stop()

		for {
			_ = discovery.BroadcastRoom(roomID, username, port, ips)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return nil
}

// StopLANRoomBroadcast stops the current LAN room announcement loop.
func (a *App) StopLANRoomBroadcast() {
	a.lanMu.Lock()
	cancel := a.lanCancel
	a.lanCancel = nil
	a.lanMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// DiscoverLANRooms scans the local network for room announcements.
func (a *App) DiscoverLANRooms(timeoutMs int) ([]discovery.DiscoveredRoom, error) {
	return discovery.DiscoverRooms(timeoutMs)
}

// SelectVideoFile opens a file dialog and returns the selected file path.
func (a *App) SelectVideoFile() (string, error) {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择视频文件",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "视频文件",
				Pattern:     "*.mp4;*.webm;*.mkv;*.avi;*.mov;*.flv;*.m4v;*.ogg;*.ogv;*.ts;*.m3u8;*.mpd",
			},
			{
				DisplayName: "所有文件",
				Pattern:     "*.*",
			},
		},
	})
	if err != nil {
		return "", err
	}
	return selection, nil
}

// ServeVideoFile sets the video file to be served by the server.
// Returns the video endpoint path (use with the server port to construct full URL).
func (a *App) ServeVideoFile(filePath string) (string, error) {
	if !a.isServerUp {
		return "", fmt.Errorf("server is not running")
	}
	a.wsServer.SetVideoFile(filePath)
	return "/video", nil
}

// StopVideoServe clears the current video file.
func (a *App) StopVideoServe() {
	if a.segmenter != nil {
		a.segmenter.Cleanup()
		a.segmenter = nil
	}
	if a.torrentProc != nil {
		a.torrentProc.Stop()
		a.torrentProc = nil
	}
	if a.wsServer != nil {
		a.wsServer.ClearVideoFile()
		a.wsServer.ClearChunkManager()
		a.wsServer.ClearHLSDir()
	}
}

// ServeVideoFileSegmented starts an HLS session and returns the playlist path.
func (a *App) ServeVideoFileSegmented(filePath string) (string, error) {
	if !a.isServerUp {
		return "", fmt.Errorf("server is not running")
	}
	if a.segmenter != nil {
		a.segmenter.Cleanup()
		a.segmenter = nil
	}

	seg, err := segment.Start(filePath)
	if err != nil {
		return "", err
	}
	if err := seg.WaitReady(25 * time.Second); err != nil {
		seg.Cleanup()
		return "", err
	}

	a.segmenter = seg
	a.wsServer.SetHLSDir(seg.Dir())
	return "/video/hls/" + segment.PlaylistName, nil
}

// ServeVideoFileChunked sets up P2P chunked video serving.
// Returns the manifest URL path.
func (a *App) ServeVideoFileChunked(filePath string) (string, error) {
	if !a.isServerUp {
		return "", fmt.Errorf("server is not running")
	}
	if a.segmenter == nil {
		return "", fmt.Errorf("segmented video is not ready")
	}

	runtime.EventsEmit(a.ctx, chunkingProgressEvent, chunk.Progress{
		Stage:   "starting",
		Current: 0,
		Total:   0,
		Percent: 0,
	})

	mgr, err := chunk.NewChunkManagerFromHLSDir(a.segmenter.Dir(), func(progress chunk.Progress) {
		runtime.EventsEmit(a.ctx, chunkingProgressEvent, progress)
	})
	if err != nil {
		runtime.EventsEmit(a.ctx, chunkingProgressEvent, chunk.Progress{
			Stage:   "error",
			Current: 0,
			Total:   0,
			Percent: 0,
		})
		return "", fmt.Errorf("create chunk manager: %w", err)
	}

	a.wsServer.SetChunkManager(mgr)
	return "/video/manifest", nil
}

// ServeMagnetVideo starts a local torrent session and returns the local HTTP path.
func (a *App) ServeMagnetVideo(magnetURI string) (string, error) {
	if !a.isServerUp {
		return "", fmt.Errorf("server is not running")
	}

	if a.torrentProc != nil {
		a.torrentProc.Stop()
		a.torrentProc = nil
	}

	proc, err := torrentproc.Start(magnetURI)
	if err != nil {
		return "", err
	}
	a.torrentProc = proc
	return proc.URL(), nil
}
