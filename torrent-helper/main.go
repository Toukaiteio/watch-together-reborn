package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	torrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/types"
)

const metadataTimeout = 45 * time.Second
const maxRangeResponseSize int64 = 1 << 20
const firstByteTimeout = 90 * time.Second
const firstByteRetryDelay = 250 * time.Millisecond

var defaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://open.stealth.si:80/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://tracker.dler.org:6969/announce",
	"udp://tracker2.dler.org:80/announce",
	"http://tracker.opentrackr.org:1337/announce",
	"https://tracker.opentrackr.org:443/announce",
}

type session struct {
	mu        sync.RWMutex
	dataDir   string
	client    *torrent.Client
	tor       *torrent.Torrent
	file      *torrent.File
	magnetURI string
	once      sync.Once
}

type status struct {
	MagnetURI      string  `json:"magnetUri"`
	FileName       string  `json:"fileName"`
	FileSize       int64   `json:"fileSize"`
	DataDir        string  `json:"dataDir"`
	BytesCompleted int64   `json:"bytesCompleted"`
	Progress       float64 `json:"progress"`
	Ready          bool    `json:"ready"`
	PeerStats      stats   `json:"peerStats"`
}

type stats struct {
	TotalPeers          int   `json:"totalPeers"`
	PendingPeers        int   `json:"pendingPeers"`
	ActivePeers         int   `json:"activePeers"`
	ConnectedSeeders    int   `json:"connectedSeeders"`
	HalfOpenPeers       int   `json:"halfOpenPeers"`
	PeerConns           int   `json:"peerConns"`
	KnownSwarmPeers     int   `json:"knownSwarmPeers"`
	PiecesComplete      int   `json:"piecesComplete"`
	BytesReadData       int64 `json:"bytesReadData"`
	BytesReadUsefulData int64 `json:"bytesReadUsefulData"`
	MetadataChunksRead  int64 `json:"metadataChunksRead"`
}

type helperState struct {
	mu         sync.RWMutex
	session    *session
	state      string
	errMessage string
	statusText string
	ready      bool
	magnetURI  string
}

func main() {
	var magnetURI string
	var listenHost string

	flag.StringVar(&magnetURI, "magnet", "", "magnet URI to stream")
	flag.StringVar(&listenHost, "host", "127.0.0.1", "host interface for local HTTP server")
	flag.Parse()

	if magnetURI == "" {
		log.Fatal("missing --magnet")
	}

	state := &helperState{
		state:      "starting",
		statusText: "正在初始化磁力下载器",
		magnetURI:  magnetURI,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/video", state.serveHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.getStatus())
	})

	ln, err := net.Listen("tcp", net.JoinHostPort(listenHost, "0"))
	if err != nil {
		log.Fatalf("listen helper server: %v", err)
	}
	defer ln.Close()

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	fmt.Fprintf(os.Stdout, "http://%s/video\n", ln.Addr().String())
	_ = os.Stdout.Sync()

	go state.prepare(magnetURI)

	go func() {
		if serveErr := server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Printf("torrent helper serve error: %v", serveErr)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	_ = server.Close()
	state.cleanup()
}

func startSession(magnetURI string) (*session, error) {
	magnetURI = normalizeMagnetURI(magnetURI)
	cfg := torrent.NewDefaultClientConfig()
	cfg.ListenPort = 0
	cfg.Seed = true

	dataDir, err := createTorrentDataDir()
	if err != nil {
		return nil, fmt.Errorf("create magnet temp dir: %w", err)
	}
	cfg.DataDir = dataDir

	client, err := torrent.NewClient(cfg)
	if err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, fmt.Errorf("create torrent client: %w", err)
	}

	tor, err := client.AddMagnet(magnetURI)
	if err != nil {
		client.Close()
		_ = os.RemoveAll(dataDir)
		return nil, fmt.Errorf("add magnet: %w", err)
	}

	sess := &session{
		dataDir:   dataDir,
		client:    client,
		tor:       tor,
		magnetURI: magnetURI,
	}
	if err := sess.waitAndSelectFile(metadataTimeout); err != nil {
		sess.cleanup()
		return nil, err
	}
	return sess, nil
}

func (s *helperState) prepare(magnetURI string) {
	s.setState("fetching_metadata", "正在获取磁力元数据", "")
	sess, err := startSession(magnetURI)
	if err != nil {
		s.setState("error", "", err.Error())
		return
	}

	s.mu.Lock()
	s.session = sess
	s.ready = true
	s.state = "ready"
	s.errMessage = ""
	s.statusText = "磁力元数据已就绪，正在边下边播"
	s.mu.Unlock()
}

func (s *helperState) getStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := map[string]any{
		"state":      s.state,
		"ready":      s.ready,
		"error":      s.errMessage,
		"statusText": s.statusText,
		"magnetUri":  s.magnetURI,
	}
	if s.session != nil {
		result["stream"] = s.session.getStatus()
	}
	return result
}

func (s *helperState) setState(state string, statusText string, errMessage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	s.statusText = statusText
	s.errMessage = errMessage
	s.ready = state == "ready"
}

func (s *helperState) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	sess := s.session
	state := s.state
	errMessage := s.errMessage
	s.mu.RUnlock()

	if sess == nil {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Retry-After", "3")
		if state == "error" {
			http.Error(w, "Magnet stream failed: "+errMessage, http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Magnet stream is preparing", http.StatusServiceUnavailable)
		return
	}
	sess.serveHTTP(w, r)
}

func (s *helperState) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		s.session.cleanup()
		s.session = nil
	}
}

func (s *session) waitAndSelectFile(timeout time.Duration) error {
	select {
	case <-s.tor.GotInfo():
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for magnet metadata")
	}

	file := selectLargestVideoFile(s.tor.Files())
	if file == nil {
		return fmt.Errorf("no playable video file found in torrent")
	}
	if err := ensureEnoughDiskSpace(s.dataDir, file.Length()); err != nil {
		return err
	}
	prioritizeStartupPieces(file)
	file.Download()

	s.mu.Lock()
	s.file = file
	s.mu.Unlock()
	return nil
}

func prioritizeStartupPieces(file *torrent.File) {
	file.SetPriority(types.PiecePriorityHigh)

	tor := file.Torrent()
	begin := file.BeginPieceIndex()
	end := file.EndPieceIndex()
	urgentEnd := begin + 4
	if urgentEnd > end {
		urgentEnd = end
	}
	for idx := begin; idx < urgentEnd; idx++ {
		tor.Piece(idx).SetPriority(types.PiecePriorityNow)
	}
	if begin < urgentEnd {
		tor.DownloadPieces(begin, urgentEnd)
	}
}

func (s *session) getStatus() status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := status{
		MagnetURI: s.magnetURI,
	}
	if s.file == nil {
		return result
	}
	result.FileName = filepath.Base(s.file.DisplayPath())
	result.FileSize = s.file.Length()
	result.DataDir = s.dataDir
	result.BytesCompleted = s.file.BytesCompleted()
	result.Ready = true
	result.PeerStats = s.stats()
	if result.FileSize > 0 {
		result.Progress = float64(result.BytesCompleted) / float64(result.FileSize) * 100
	}
	return result
}

func (s *session) stats() stats {
	if s == nil || s.tor == nil {
		return stats{}
	}
	torrentStats := s.tor.Stats()
	return stats{
		TotalPeers:          torrentStats.TotalPeers,
		PendingPeers:        torrentStats.PendingPeers,
		ActivePeers:         torrentStats.ActivePeers,
		ConnectedSeeders:    torrentStats.ConnectedSeeders,
		HalfOpenPeers:       torrentStats.HalfOpenPeers,
		PeerConns:           len(s.tor.PeerConns()),
		KnownSwarmPeers:     len(s.tor.KnownSwarm()),
		PiecesComplete:      torrentStats.PiecesComplete,
		BytesReadData:       torrentStats.BytesReadData.Int64(),
		BytesReadUsefulData: torrentStats.BytesReadUsefulData.Int64(),
		MetadataChunksRead:  torrentStats.MetadataChunksRead.Int64(),
	}
}

func (s *session) diagnosticText() string {
	stats := s.stats()
	return fmt.Sprintf(
		"peers total=%d active=%d conns=%d pending=%d seeders=%d known=%d bytesRead=%d useful=%d metadataChunks=%d",
		stats.TotalPeers,
		stats.ActivePeers,
		stats.PeerConns,
		stats.PendingPeers,
		stats.ConnectedSeeders,
		stats.KnownSwarmPeers,
		stats.BytesReadData,
		stats.BytesReadUsefulData,
		stats.MetadataChunksRead,
	)
}

func (s *session) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	file := s.file
	s.mu.RUnlock()
	if file == nil {
		http.Error(w, "Magnet stream not ready", http.StatusServiceUnavailable)
		return
	}

	file.Download()

	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(file.DisplayPath()))); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")

	start, end, partial, err := parseByteRange(r.Header.Get("Range"), file.Length())
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}

	if end-start+1 > maxRangeResponseSize {
		end = start + maxRangeResponseSize - 1
		if end >= file.Length() {
			end = file.Length() - 1
		}
		partial = true
	}

	contentLen := end - start + 1
	data, err := readTorrentRange(r.Context(), file, start, contentLen)
	if err != nil {
		http.Error(w, "read torrent stream failed: "+err.Error(), http.StatusGatewayTimeout)
		return
	}
	contentLen = int64(len(data))
	end = start + contentLen - 1
	partial = true

	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Length()))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLen))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Length()))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLen))
		w.WriteHeader(http.StatusPartialContent)
	}

	if _, err := w.Write(data); err != nil {
		log.Printf("torrent helper stream write failed: %v", err)
	}
}

func readTorrentRange(ctx context.Context, file *torrent.File, start int64, maxLength int64) ([]byte, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("empty torrent range")
	}
	if maxLength > maxRangeResponseSize {
		return nil, fmt.Errorf("range too large")
	}

	ctx, cancel := context.WithTimeout(ctx, firstByteTimeout)
	defer cancel()

	buf := make([]byte, int(maxLength))
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		reader := file.NewReader()
		reader.SetContext(ctx)
		reader.SetReadahead(32 << 20)
		_, seekErr := reader.Seek(start, io.SeekStart)
		if seekErr != nil {
			_ = reader.Close()
			return nil, fmt.Errorf("seek byte %d: %w", start, seekErr)
		}

		n, readErr := reader.Read(buf)
		_ = reader.Close()
		if n > 0 {
			return buf[:n], nil
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return nil, readErr
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(firstByteRetryDelay):
		}
	}
}

func (s *session) cleanup() {
	s.once.Do(func() {
		if s.client != nil {
			s.client.Close()
			s.client = nil
		}
		if s.dataDir != "" {
			_ = os.RemoveAll(s.dataDir)
			s.dataDir = ""
		}
	})
}

func selectLargestVideoFile(files []*torrent.File) *torrent.File {
	var best *torrent.File
	for _, file := range files {
		if file == nil {
			continue
		}
		if !isSupportedVideoPath(file.DisplayPath()) {
			continue
		}
		if best == nil || file.Length() > best.Length() {
			best = file
		}
	}
	if best != nil {
		return best
	}
	return nil
}

func isSupportedVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".webm", ".m4v", ".mov", ".ogv":
		return true
	default:
		return false
	}
}

func normalizeMagnetURI(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "magnet") {
		return raw
	}
	query := parsed.Query()
	if len(query["tr"]) > 0 {
		return parsed.String()
	}
	for _, tracker := range defaultTrackers {
		query.Add("tr", tracker)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func parseByteRange(header string, fileSize int64) (start int64, end int64, partial bool, err error) {
	if fileSize <= 0 {
		return 0, 0, false, fmt.Errorf("empty torrent file")
	}
	if header == "" {
		return 0, fileSize - 1, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, fmt.Errorf("unsupported range")
	}

	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf("multiple ranges are not supported")
	}

	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, fmt.Errorf("invalid range")
	}

	switch {
	case parts[0] == "":
		suffixLen, convErr := strconv.ParseInt(parts[1], 10, 64)
		if convErr != nil || suffixLen <= 0 {
			return 0, 0, false, fmt.Errorf("invalid suffix range")
		}
		if suffixLen > fileSize {
			suffixLen = fileSize
		}
		start = fileSize - suffixLen
		end = fileSize - 1
	case parts[1] == "":
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, false, fmt.Errorf("invalid range start")
		}
		end = fileSize - 1
	default:
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, false, fmt.Errorf("invalid range start")
		}
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, false, fmt.Errorf("invalid range end")
		}
	}

	if start < 0 || start >= fileSize || end < start {
		return 0, 0, false, fmt.Errorf("range out of bounds")
	}
	if end >= fileSize {
		end = fileSize - 1
	}
	return start, end, true, nil
}
