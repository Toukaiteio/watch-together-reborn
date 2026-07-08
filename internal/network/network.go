package network

import (
	"encoding/json"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// IPv6AddrInfo holds an IPv6 address with classification metadata.
type IPv6AddrInfo struct {
	Address     string `json:"address"`
	IsPublic    bool   `json:"isPublic"`
	IsULA       bool   `json:"isUla"`
	IsTemporary bool   `json:"isTemporary"`
	Type        string `json:"type"` // "public", "ula"
}

// GetLocalIPv4s returns all non-loopback IPv4 addresses.
func GetLocalIPv4s() []string {
	var preferred []string
	var fallback []string
	seen := make(map[string]struct{})

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	sort.SliceStable(ifaces, func(i, j int) bool {
		return scoreInterface(ifaces[i]) < scoreInterface(ifaces[j])
	})

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}

			ipStr := ip.String()
			if _, ok := seen[ipStr]; ok {
				continue
			}
			seen[ipStr] = struct{}{}

			if isPreferredShareInterface(iface) {
				preferred = append(preferred, ipStr)
			} else {
				fallback = append(fallback, ipStr)
			}
		}
	}

	if len(preferred) > 0 {
		return append(preferred, fallback...)
	}
	return fallback
}

// GetLocalIPv6s returns non-loopback IPv6 addresses filtered to only
// include globally-routable (public) and Unique Local Addresses (ULA).
// Link-local, multicast, IPv4-mapped, and other special-purpose addresses
// are excluded because they are not useful for remote peer connectivity.
func GetLocalIPv6s() []string {
	infos := GetLocalIPv6sWithInfo()
	ips := make([]string, len(infos))
	for i, info := range infos {
		ips[i] = info.Address
	}
	return ips
}

// GetLocalIPv6sWithInfo returns IPv6 addresses with classification metadata,
// filtered to only public and ULA addresses. Results are sorted with public
// addresses first (preferred for remote connectivity), then ULA.
func GetLocalIPv6sWithInfo() []IPv6AddrInfo {
	var publicTemporary []IPv6AddrInfo
	var publicStable []IPv6AddrInfo
	var ula []IPv6AddrInfo
	seen := make(map[string]struct{})
	temporaryLookup := getTemporaryIPv6Lookup()

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	// Sort interfaces the same way as GetLocalIPv4s for consistency
	sort.SliceStable(ifaces, func(i, j int) bool {
		return scoreInterface(ifaces[i]) < scoreInterface(ifaces[j])
	})

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			ip := ipNet.IP
			if ip.To4() != nil || ip.To16() == nil {
				continue
			}
			ipStr := ip.String()
			if _, ok := seen[ipStr]; ok {
				continue
			}
			seen[ipStr] = struct{}{}

			isTemporary := temporaryLookup[ipStr]

			if isPublicIPv6(ip) {
				info := IPv6AddrInfo{
					Address:     ipStr,
					IsPublic:    true,
					IsULA:       false,
					IsTemporary: isTemporary,
					Type:        "public",
				}
				if isTemporary {
					publicTemporary = append(publicTemporary, info)
				} else {
					publicStable = append(publicStable, info)
				}
			} else if isULAIPv6(ip) {
				ula = append(ula, IPv6AddrInfo{
					Address:     ipStr,
					IsPublic:    false,
					IsULA:       true,
					IsTemporary: false,
					Type:        "ula",
				})
			}
			// Skip link-local, multicast, IPv4-mapped, etc.
		}
	}

	return append(append(publicTemporary, publicStable...), ula...)
}

// isPublicIPv6 returns true if the IP is a globally routable IPv6 address.
// Global Unicast addresses have the prefix 2000::/3 (first 3 bits = 001).
func isPublicIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return false
	}
	// Global Unicast: 2000::/3 — first 3 bits are 001
	// First byte: 0010 0000 (0x20) to 0011 1111 (0x3F)
	return ip16[0]&0xE0 == 0x20
}

// isULAIPv6 returns true if the IP is a Unique Local Address (ULA).
// ULA addresses have the prefix fc00::/7 (first 7 bits = 1111 110).
func isULAIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return false
	}
	// ULA: fc00::/7 — first 7 bits are 1111 110
	// First byte: 1111 1100 (0xFC) or 1111 1101 (0xFD)
	return ip16[0]&0xFE == 0xFC
}

// IsPortAvailable checks if a TCP port is available on both IPv4 and IPv6.
func IsPortAvailable(port int) bool {
	addr4 := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	ln4, err4 := net.Listen("tcp", addr4)
	if err4 != nil {
		return false
	}
	ln4.Close()

	addr6 := net.JoinHostPort("[::]", strconv.Itoa(port))
	ln6, err6 := net.Listen("tcp6", addr6)
	if err6 != nil {
		// IPv6 might not be available, that's okay
		return true
	}
	ln6.Close()
	return true
}

// FindAvailablePort starts from the given port and finds the next available one.
func FindAvailablePort(startPort int) int {
	for port := startPort; port < 65535; port++ {
		if IsPortAvailable(port) {
			return port
		}
	}
	return startPort
}

func isPreferredShareInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagPointToPoint != 0 {
		return false
	}

	name := strings.ToLower(iface.Name)
	virtualHints := []string{
		"vethernet", "hyper-v", "vmware", "virtual", "docker", "wsl",
		"vpn", "tun", "tap", "tailscale", "zerotier", "wireguard",
	}
	for _, hint := range virtualHints {
		if strings.Contains(name, hint) {
			return false
		}
	}

	return true
}

const windowsSuffixOriginRandom = 5

type windowsIPv6Meta struct {
	IPAddress    string `json:"IPAddress"`
	SuffixOrigin int    `json:"SuffixOrigin"`
}

func getTemporaryIPv6Lookup() map[string]bool {
	lookup := make(map[string]bool)
	if runtime.GOOS != "windows" {
		return lookup
	}

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Get-NetIPAddress -AddressFamily IPv6 | Select-Object IPAddress,SuffixOrigin | ConvertTo-Json -Compress",
	)
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return lookup
	}

	var many []windowsIPv6Meta
	if err := json.Unmarshal(output, &many); err == nil {
		for _, item := range many {
			if item.SuffixOrigin == windowsSuffixOriginRandom {
				lookup[item.IPAddress] = true
			}
		}
		return lookup
	}

	var one windowsIPv6Meta
	if err := json.Unmarshal(output, &one); err == nil && one.SuffixOrigin == windowsSuffixOriginRandom {
		lookup[one.IPAddress] = true
	}

	return lookup
}

func scoreInterface(iface net.Interface) int {
	score := 100
	name := strings.ToLower(iface.Name)

	if isPreferredShareInterface(iface) {
		score -= 50
	}
	if iface.Flags&net.FlagBroadcast != 0 {
		score -= 10
	}
	if iface.Flags&net.FlagMulticast != 0 {
		score -= 5
	}

	preferredHints := []string{"wi-fi", "wifi", "wlan", "ethernet", "eth", "en"}
	for _, hint := range preferredHints {
		if strings.Contains(name, hint) {
			score -= 20
			break
		}
	}

	return score
}
