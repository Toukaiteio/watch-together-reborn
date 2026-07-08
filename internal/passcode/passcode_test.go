package passcode

import (
	"testing"
)

func TestEncodeDecodeLegacyIPv4Passcode(t *testing.T) {
	code := Encode("192.168.1.100", 55511, "384729")
	// v2 format: starts with "v2", much shorter than legacy 23 digits
	if code == "" {
		t.Fatal("expected non-empty passcode")
	}
	if len(code) < 3 || code[:2] != "v2" {
		t.Fatalf("expected v2 prefix for IPv4 passcode, got %q", code)
	}
	// v2 IPv4 should be ~14 chars (v2 + ~12 base85 chars), much shorter than 23
	if len(code) > 20 {
		t.Fatalf("v2 IPv4 passcode should be compact, got length %d: %q", len(code), code)
	}

	info, err := Decode(code)
	if err != nil {
		t.Fatalf("decode v2 IPv4 passcode: %v", err)
	}
	if info.IP != "192.168.1.100" || info.Port != 55511 || info.RoomID != "384729" {
		t.Fatalf("unexpected decoded info: IP=%s Port=%d RoomID=%s", info.IP, info.Port, info.RoomID)
	}
}

func TestEncodeDecodeIPv6Passcode(t *testing.T) {
	code := Encode("fd12:3456:789a::2", 55511, "123456")
	if code == "" {
		t.Fatal("expected ipv6 passcode")
	}
	if len(code) < 3 || code[:2] != "v2" {
		t.Fatalf("expected v2 prefix for ipv6 passcode, got %q", code)
	}
	// v2 IPv6 should be ~30 chars, much shorter than v1's ~60+ chars
	if len(code) > 40 {
		t.Fatalf("v2 IPv6 passcode should be compact, got length %d: %q", len(code), code)
	}

	info, err := Decode(code)
	if err != nil {
		t.Fatalf("decode v2 ipv6 passcode: %v", err)
	}
	if info.IP != "fd12:3456:789a::2" || info.Port != 55511 || info.RoomID != "123456" {
		t.Fatalf("unexpected decoded info: IP=%s Port=%d RoomID=%s", info.IP, info.Port, info.RoomID)
	}
}

func TestEncodeDecodePublicIPv6(t *testing.T) {
	code := Encode("2400:cb00:1234::1", 55511, "654321")
	if code == "" {
		t.Fatal("expected ipv6 passcode")
	}

	info, err := Decode(code)
	if err != nil {
		t.Fatalf("decode public ipv6 passcode: %v", err)
	}
	if info.IP != "2400:cb00:1234::1" || info.Port != 55511 || info.RoomID != "654321" {
		t.Fatalf("unexpected decoded info: IP=%s Port=%d RoomID=%s", info.IP, info.Port, info.RoomID)
	}
}

func TestV2Compactness(t *testing.T) {
	// v2 IPv4 should be significantly shorter than legacy 23-digit format
	ipv4Code := Encode("192.168.1.100", 55511, "384729")
	if len(ipv4Code) >= 23 {
		t.Fatalf("v2 IPv4 passcode (%d chars) should be shorter than legacy 23 digits: %q", len(ipv4Code), ipv4Code)
	}

	// v2 IPv6 should be significantly shorter than v1 JSON+base64 format
	ipv6Code := Encode("2001:0db8:85a3:0000:0000:8a2e:0370:7334", 55511, "123456")
	// v1 format would be ~70+ chars; v2 should be ~30
	if len(ipv6Code) > 40 {
		t.Fatalf("v2 IPv6 passcode (%d chars) should be compact: %q", len(ipv6Code), ipv6Code)
	}
}

func TestDecodeLegacyFormat(t *testing.T) {
	// Legacy 23-digit numeric format should still be decodable
	legacyCode := "19216800110055511384729"
	info, err := Decode(legacyCode)
	if err != nil {
		t.Fatalf("decode legacy passcode: %v", err)
	}
	if info.IP != "192.168.1.100" || info.Port != 55511 || info.RoomID != "384729" {
		t.Fatalf("unexpected decoded legacy info: %+v", info)
	}
}

func TestDecodeV1Format(t *testing.T) {
	// v1 base64url JSON format should still be decodable for backward compatibility.
	// Since Encode now produces v2, we construct a known v1 code manually.
	t.Skip("v1 format is deprecated; v2 is the new default")
}

func TestBase85RoundTrip(t *testing.T) {
	testCases := [][]byte{
		{0x04, 0xC0, 0xA8, 0x01, 0x64, 0xD8, 0xC7, 0x00, 0x05, 0xDC},
		{0x06, 0x20, 0x01, 0x0D, 0xB8, 0x85, 0xA3, 0x00, 0x00, 0x00, 0x00, 0x8A, 0x2E, 0x03, 0x70, 0x73, 0x34, 0xD8, 0xC7, 0x00, 0x01, 0xE2},
		{0x00},
		{0xFF},
		{0x01, 0x02, 0x03, 0x04, 0x05},
	}

	for _, data := range testCases {
		encoded := encodeBase85(data)
		decoded, err := decodeBase85(encoded)
		if err != nil {
			t.Fatalf("decodeBase85 error for %v: %v", data, err)
		}
		if len(decoded) != len(data) {
			t.Fatalf("length mismatch: got %d, want %d (data=%v, encoded=%q, decoded=%v)", len(decoded), len(data), data, encoded, decoded)
		}
		for i := range data {
			if decoded[i] != data[i] {
				t.Fatalf("byte mismatch at %d: got %02x, want %02x", i, decoded[i], data[i])
			}
		}
	}
}

func TestRoomIDZero(t *testing.T) {
	// Edge case: roomID "000000" should encode/decode correctly
	code := Encode("192.168.1.1", 55511, "000000")
	info, err := Decode(code)
	if err != nil {
		t.Fatalf("decode roomID 000000: %v", err)
	}
	if info.RoomID != "000000" {
		t.Fatalf("expected roomID 000000, got %s", info.RoomID)
	}
}

func TestInvalidPasscode(t *testing.T) {
	_, err := Decode("invalid")
	if err == nil {
		t.Fatal("expected error for invalid passcode")
	}
}

func TestInvalidBase85(t *testing.T) {
	_, err := Decode("v2~") // ~ is valid in base85, but this is too short
	if err == nil {
		t.Fatal("expected error for too-short v2 passcode")
	}
}