package passcode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

// base85Alphabet is a compact encoding alphabet (RFC 1924 inspired).
// Each character encodes log2(85) ≈ 6.41 bits, far more than decimal (3.32)
// or hex (4). Special symbols act as compression: one symbol carries as much
// information as ~6.4 binary digits, replacing multiple decimal/hex characters.
const base85Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!#$%&()*+-;<=>?@^_`{|}~"

var base85DecodeMap map[byte]*big.Int

func init() {
	rand.Seed(time.Now().UnixNano())
	base85DecodeMap = make(map[byte]*big.Int)
	for i := 0; i < len(base85Alphabet); i++ {
		base85DecodeMap[base85Alphabet[i]] = big.NewInt(int64(i))
	}
}

// encodeBase85 encodes a byte slice into a base85 string using the compact alphabet.
func encodeBase85(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	num := new(big.Int).SetBytes(data)
	base := big.NewInt(85)
	zero := big.NewInt(0)
	mod := new(big.Int)

	var chars []byte
	for num.Cmp(zero) > 0 {
		num.DivMod(num, base, mod)
		chars = append(chars, base85Alphabet[mod.Int64()])
	}

	// Reverse to get most-significant-first
	for i, j := 0, len(chars)-1; i < j; i, j = i+1, j-1 {
		chars[i], chars[j] = chars[j], chars[i]
	}

	// Preserve leading zero bytes: each leading 0x00 in data maps to alphabet[0]
	for _, b := range data {
		if b != 0 {
			break
		}
		chars = append([]byte{base85Alphabet[0]}, chars...)
	}

	return string(chars)
}

// decodeBase85 decodes a base85 string back to a byte slice.
func decodeBase85(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}

	num := big.NewInt(0)
	base := big.NewInt(85)

	for i := 0; i < len(s); i++ {
		val, ok := base85DecodeMap[s[i]]
		if !ok {
			return nil, fmt.Errorf("invalid base85 character: %c", s[i])
		}
		num.Mul(num, base)
		num.Add(num, val)
	}

	// Count leading zeros (first char = alphabet[0])
	leadingZeros := 0
	for i := 0; i < len(s); i++ {
		if s[i] == base85Alphabet[0] {
			leadingZeros++
		} else {
			break
		}
	}

	data := num.Bytes()
	result := make([]byte, leadingZeros+len(data))
	copy(result[leadingZeros:], data)

	return result, nil
}

// ConnectionInfo holds the decoded information from a passcode.
type ConnectionInfo struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	RoomID string `json:"roomId"`
}

type encodedConnectionInfo struct {
	Version int    `json:"v"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	RoomID  string `json:"roomId"`
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateRoomID generates a random 6-digit numeric room ID.
func GenerateRoomID() string {
	n := rand.Intn(1000000)
	return fmt.Sprintf("%06d", n)
}

// Encode encodes an IP, port, and room ID into a shareable passcode string.
// All addresses now use the compact v2 format (base85 binary encoding).
// The v2 format is significantly shorter than the legacy formats:
//   - IPv4: ~14 chars vs 23 digits (legacy)
//   - IPv6: ~30 chars vs ~60+ chars (v1 JSON+base64)
//
// The base85 alphabet uses special symbols to compress multiple characters
// of information into a single symbol, achieving ~6.41 bits per character.
func Encode(ip string, port int, roomID string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ""
	}
	return encodeV2(parsedIP, port, roomID)
}

// Decode decodes a passcode string back into connection information.
// Supports v2 (compact base85), v1 (base64url JSON), and legacy IPv4 (23-digit).
func Decode(code string) (*ConnectionInfo, error) {
	if strings.HasPrefix(code, "v2") {
		return decodeV2(code)
	}
	if strings.HasPrefix(code, "v1-") {
		return decodeV1(code)
	}
	return decodeLegacyIPv4(code)
}

// encodeV2 packs IP + port + roomID into a compact binary payload and
// encodes it with the base85 alphabet for maximum compression.
//
// Binary layout:
//
//	IPv4: [0x04] [4 bytes IP] [2 bytes port BE] [3 bytes roomID BE] = 10 bytes → ~14 chars
//	IPv6: [0x06] [16 bytes IP] [2 bytes port BE] [3 bytes roomID BE] = 22 bytes → ~30 chars
func encodeV2(ip net.IP, port int, roomID string) string {
	var buf []byte

	if ipv4 := ip.To4(); ipv4 != nil {
		buf = make([]byte, 10)
		buf[0] = 0x04 // type: IPv4
		copy(buf[1:5], ipv4)
		buf[5] = byte(port >> 8)
		buf[6] = byte(port & 0xFF)
	} else {
		ip6 := ip.To16()
		buf = make([]byte, 22)
		buf[0] = 0x06 // type: IPv6
		copy(buf[1:17], ip6)
		buf[17] = byte(port >> 8)
		buf[18] = byte(port & 0xFF)
	}

	// roomID as 3-byte big-endian (max 999999 < 2^20, fits in 3 bytes)
	roomIDInt, _ := strconv.Atoi(roomID)
	buf[len(buf)-3] = byte(roomIDInt >> 16)
	buf[len(buf)-2] = byte((roomIDInt >> 8) & 0xFF)
	buf[len(buf)-1] = byte(roomIDInt & 0xFF)

	return "v2" + encodeBase85(buf)
}

// decodeV2 decodes the compact v2 base85 passcode format.
func decodeV2(code string) (*ConnectionInfo, error) {
	payload := strings.TrimPrefix(code, "v2")
	data, err := decodeBase85(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid v2 passcode: %w", err)
	}

	if len(data) < 1 {
		return nil, fmt.Errorf("invalid v2 passcode: empty payload")
	}

	addrType := data[0]

	switch addrType {
	case 0x04: // IPv4
		// Pad to expected length of 10 bytes
		if len(data) < 10 {
			padded := make([]byte, 10)
			copy(padded[10-len(data):], data)
			data = padded
		}
		ip := net.IP{data[1], data[2], data[3], data[4]}
		port := int(data[5])<<8 | int(data[6])
		roomIDInt := int(data[7])<<16 | int(data[8])<<8 | int(data[9])
		return &ConnectionInfo{
			IP:     ip.String(),
			Port:   port,
			RoomID: fmt.Sprintf("%06d", roomIDInt),
		}, nil

	case 0x06: // IPv6
		// Pad to expected length of 22 bytes
		if len(data) < 22 {
			padded := make([]byte, 22)
			copy(padded[22-len(data):], data)
			data = padded
		}
		ip := make(net.IP, 16)
		copy(ip, data[1:17])
		port := int(data[17])<<8 | int(data[18])
		roomIDInt := int(data[19])<<16 | int(data[20])<<8 | int(data[21])
		return &ConnectionInfo{
			IP:     ip.String(),
			Port:   port,
			RoomID: fmt.Sprintf("%06d", roomIDInt),
		}, nil

	default:
		return nil, fmt.Errorf("invalid v2 passcode: unknown address type %d", addrType)
	}
}

func decodeLegacyIPv4(code string) (*ConnectionInfo, error) {
	if len(code) != 23 {
		return nil, fmt.Errorf("invalid passcode length: expected 23 digits or v1 payload, got %d", len(code))
	}

	for _, c := range code {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("invalid passcode: must contain only digits")
		}
	}

	octet1, err := strconv.Atoi(code[0:3])
	if err != nil || octet1 > 255 {
		return nil, fmt.Errorf("invalid IP octet in passcode")
	}
	octet2, err := strconv.Atoi(code[3:6])
	if err != nil || octet2 > 255 {
		return nil, fmt.Errorf("invalid IP octet in passcode")
	}
	octet3, err := strconv.Atoi(code[6:9])
	if err != nil || octet3 > 255 {
		return nil, fmt.Errorf("invalid IP octet in passcode")
	}
	octet4, err := strconv.Atoi(code[9:12])
	if err != nil || octet4 > 255 {
		return nil, fmt.Errorf("invalid IP octet in passcode")
	}

	port, err := strconv.Atoi(code[12:17])
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port in passcode")
	}

	roomID := code[17:23]

	return &ConnectionInfo{
		IP:     fmt.Sprintf("%d.%d.%d.%d", octet1, octet2, octet3, octet4),
		Port:   port,
		RoomID: roomID,
	}, nil
}

func decodeV1(code string) (*ConnectionInfo, error) {
	payload := strings.TrimPrefix(code, "v1-")
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid passcode payload")
	}

	var decoded encodedConnectionInfo
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid passcode payload")
	}
	if decoded.Version != 1 {
		return nil, fmt.Errorf("unsupported passcode version: %d", decoded.Version)
	}
	if net.ParseIP(decoded.IP) == nil {
		return nil, fmt.Errorf("invalid IP in passcode")
	}
	if decoded.Port < 1 || decoded.Port > 65535 {
		return nil, fmt.Errorf("invalid port in passcode")
	}
	if decoded.RoomID == "" {
		return nil, fmt.Errorf("missing room ID in passcode")
	}

	return &ConnectionInfo{
		IP:     decoded.IP,
		Port:   decoded.Port,
		RoomID: decoded.RoomID,
	}, nil
}
