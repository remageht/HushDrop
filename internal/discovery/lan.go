package discovery

import (
	"net"
)

var (
	// RFC 1918 Private IPv4 CIDRs
	privateCIDRs = []*net.IPNet{
		parseCIDR("10.0.0.0/8"),
		parseCIDR("172.16.0.0/12"),
		parseCIDR("192.168.0.0/16"),
		parseCIDR("127.0.0.0/8"),
		parseCIDR("169.254.0.0/16"), // Link-local IPv4
	}

	// IPv6 Private & Local CIDRs
	privateIPv6CIDRs = []*net.IPNet{
		parseCIDR("::1/128"),     // Loopback
		parseCIDR("fc00::/7"),    // Unique Local Address (ULA)
		parseCIDR("fe80::/10"),   // Link-local
	}
)

func parseCIDR(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic("invalid CIDR: " + s)
	}
	return ipnet
}

// IsPrivateIP checks if an IP belongs strictly to a private/local network
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if ip4 := ip.To4(); ip4 != nil {
		for _, cidr := range privateCIDRs {
			if cidr.Contains(ip4) {
				return true
			}
		}
		return false
	}

	for _, cidr := range privateIPv6CIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// GetLANIPs returns all private non-loopback IPv4 addresses found on network interfaces
func GetLANIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	for _, iface := range ifaces {
		// Skip down and loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip4 := ip.To4()
			if ip4 != nil && IsPrivateIP(ip4) && !ip4.IsLinkLocalUnicast() {
				ips = append(ips, ip4)
			}
		}
	}

	return ips, nil
}

// GetPrimaryLANIP returns the most likely LAN IP address for local discovery
func GetPrimaryLANIP() net.IP {
	ips, err := GetLANIPs()
	if err == nil && len(ips) > 0 {
		// Prefer 192.168.x.x, then 10.x.x.x, then others
		for _, ip := range ips {
			if ip[0] == 192 && ip[1] == 168 {
				return ip
			}
		}
		for _, ip := range ips {
			if ip[0] == 10 {
				return ip
			}
		}
		return ips[0]
	}

	// Fallback to loopback
	return net.ParseIP("127.0.0.1")
}
