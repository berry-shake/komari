package utils

import (
	"net"
	"os"
	"strings"
)

// Reverse proxies must strip incoming forwarding headers and set their own.
// Override with a comma-separated IP/CIDR list for proxies on public networks.
func TrustedProxies() []string {
	if value, ok := os.LookupEnv("KOMARI_TRUSTED_PROXIES"); ok {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
	}
	return []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}
}
func trustedProxy(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, entry := range TrustedProxies() {
		if exact := net.ParseIP(entry); exact != nil && exact.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
