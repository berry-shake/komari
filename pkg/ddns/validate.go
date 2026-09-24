package ddns

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var idPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validID(v string) bool { return idPattern.MatchString(v) }
func normalizeName(raw string) (string, error) {
	name, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), ".")))
	if err != nil || len(name) > 253 || !strings.Contains(name, ".") {
		return "", fmt.Errorf("请输入完整域名，例如 home.example.com")
	}
	labels := strings.Split(name, ".")
	for i, label := range labels {
		if i == 0 && label == "*" {
			continue
		}
		if !labelPattern.MatchString(label) {
			return "", fmt.Errorf("域名格式无效")
		}
	}
	return name, nil
}
func validToken(token string) bool {
	return len(token) <= 4096 && !strings.ContainsAny(token, "\r\n\x00")
}
func publicIP(raw, kind string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || addr.Zone() != "" {
		return ""
	}
	addr = addr.Unmap()
	if (kind == "A") != addr.Is4() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return ""
	}
	if addr.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(addr) {
		return ""
	}
	return addr.String()
}
