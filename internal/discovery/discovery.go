package discovery

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	udpPort      = 55512
	magic        = "watch-together-reborn-room-v1"
	maxPacketLen = 2048
)

// RoomAdvert is the small LAN-discovery payload broadcast by a host.
type RoomAdvert struct {
	Magic     string   `json:"magic"`
	RoomID    string   `json:"roomId"`
	Username  string   `json:"username"`
	Port      int      `json:"port"`
	IPs       []string `json:"ips"`
	Timestamp int64    `json:"timestamp"`
}

// DiscoveredRoom is returned to the frontend after a LAN discovery scan.
type DiscoveredRoom struct {
	RoomID   string   `json:"roomId"`
	Username string   `json:"username"`
	Port     int      `json:"port"`
	IPs      []string `json:"ips"`
	From     string   `json:"from"`
	AgeMs    int64    `json:"ageMs"`
}

// BroadcastRoom sends one UDP broadcast announcement for LAN room discovery.
func BroadcastRoom(roomID, username string, port int, ips []string) error {
	if strings.TrimSpace(roomID) == "" {
		return errors.New("room ID is required")
	}
	if port <= 0 || port > 65535 {
		return errors.New("valid port is required")
	}

	payload, err := json.Marshal(RoomAdvert{
		Magic:     magic,
		RoomID:    roomID,
		Username:  username,
		Port:      port,
		IPs:       uniqueStrings(ips),
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: udpPort,
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write(payload)
	return err
}

// DiscoverRooms listens for UDP broadcast announcements for the supplied duration.
func DiscoverRooms(timeoutMs int) ([]DiscoveredRoom, error) {
	if timeoutMs <= 0 {
		timeoutMs = 1500
	}
	if timeoutMs > 8000 {
		timeoutMs = 8000
	}

	addr := &net.UDPAddr{IP: net.IPv4zero, Port: udpPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	rooms := make(map[string]DiscoveredRoom)
	buf := make([]byte, maxPacketLen)

	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}

		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			return nil, err
		}

		var advert RoomAdvert
		if err := json.Unmarshal(buf[:n], &advert); err != nil {
			continue
		}
		if advert.Magic != magic || advert.RoomID == "" || advert.Port <= 0 {
			continue
		}

		from := ""
		if remote != nil {
			from = remote.IP.String()
		}
		ips := uniqueStrings(append(advert.IPs, from))
		key := advert.RoomID + "@" + strconv.Itoa(advert.Port)
		rooms[key] = DiscoveredRoom{
			RoomID:   advert.RoomID,
			Username: advert.Username,
			Port:     advert.Port,
			IPs:      ips,
			From:     from,
			AgeMs:    time.Now().UnixMilli() - advert.Timestamp,
		}
	}

	result := make([]DiscoveredRoom, 0, len(rooms))
	for _, room := range rooms {
		result = append(result, room)
	}
	return result, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
