package discovery

import (
	"errors"
	"fmt"
	"net"
)

var (
	// RFC 1918 Private IPv4 CIDRs
	rfc1918CIDRs = []*net.IPNet{
		parseCIDR("10.0.0.0/8"),
		parseCIDR("172.16.0.0/12"),
		parseCIDR("192.168.0.0/16"),
	}

	// RFC 1918 & Local Private IPv4 CIDRs (used for general LAN checks)
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

// IsRFC1918 checks if an IP belongs strictly to RFC 1918 private IPv4 ranges (10/8, 172.16/12, 192.168/16)
func IsRFC1918(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	for _, cidr := range rfc1918CIDRs {
		if cidr.Contains(ip4) {
			return true
		}
	}
	return false
}

// GetPrimarySubnet identifies the active RFC 1918 subnet and primary LAN IP of the host
func GetPrimarySubnet() (*net.IPNet, net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}

	primaryIP := GetPrimaryLANIP()

	// 1. Try to find the exact subnet belonging to GetPrimaryLANIP()
	if primaryIP != nil && !primaryIP.IsLoopback() && IsRFC1918(primaryIP) {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok {
					ip4 := ipnet.IP.To4()
					if ip4 != nil && ip4.Equal(primaryIP) {
						return &net.IPNet{
							IP:   ip4,
							Mask: ipnet.Mask,
						}, ip4, nil
					}
				}
			}
		}
	}

	// 2. Fallback to any active RFC 1918 interface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				ip4 := ipnet.IP.To4()
				if ip4 != nil && IsRFC1918(ip4) && !ip4.IsLinkLocalUnicast() {
					return &net.IPNet{
						IP:   ip4,
						Mask: ipnet.Mask,
					}, ip4, nil
				}
			}
		}
	}

	return nil, nil, errors.New("no active RFC1918 LAN interface found")
}

// GenerateSubnetIPs generates host IP addresses for a given RFC 1918 subnet,
// clamping to /24 (at most 254 hosts) and capping to capLimit addresses.
func GenerateSubnetIPs(subnet *net.IPNet, capLimit int) ([]net.IP, error) {
	if subnet == nil || subnet.IP == nil {
		return nil, errors.New("subnet cannot be nil")
	}
	ip4 := subnet.IP.To4()
	if ip4 == nil {
		return nil, errors.New("only IPv4 subnets are supported")
	}
	if !IsRFC1918(ip4) {
		return nil, fmt.Errorf("ip %s is not an RFC 1918 private address", ip4.String())
	}

	if capLimit <= 0 || capLimit > 256 {
		capLimit = 256
	}

	ones, bits := subnet.Mask.Size()
	if bits != 32 || ones <= 0 {
		ones = 24
	}

	// Clamp prefix to at least /24 so we never storm larger networks (/16 or /8)
	if ones < 24 {
		ones = 24
	}

	mask := net.CIDRMask(ones, 32)
	netIP := ip4.Mask(mask).To4()

	// Number of host addresses
	hostBits := 32 - ones
	totalHosts := (1 << hostBits) - 2 // Exclude network (.0) and broadcast (.255)
	if totalHosts <= 0 {
		return nil, errors.New("no usable host addresses in subnet")
	}

	baseUint := (uint32(netIP[0]) << 24) | (uint32(netIP[1]) << 16) | (uint32(netIP[2]) << 8) | uint32(netIP[3])

	var ips []net.IP
	for i := 1; i <= totalHosts && len(ips) < capLimit; i++ {
		cur := baseUint + uint32(i)
		candidate := net.IPv4(byte(cur>>24), byte(cur>>16), byte(cur>>8), byte(cur))
		if IsRFC1918(candidate) {
			ips = append(ips, candidate)
		}
	}

	return ips, nil
}

