// Package ddns ports the behavior of yunjianj/Komari-DDNS v0.1.3 to the native
// server. Original copyright (c) 2026 穿云箭 (yunjianj), MIT; see
// docs/licenses/Komari-DDNS-MIT.txt. No plugin runtime is required.
package ddns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const cloudflareURL = "https://api.cloudflare.com/client/v4"

type cloudflare struct {
	base string
	http *http.Client
}
type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type dnsRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}
type cfResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func newCloudflare() *cloudflare {
	return &cloudflare{base: cloudflareURL, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *cloudflare) request(ctx context.Context, method, path, token string, body any, result any) (int, error) {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Cloudflare 请求失败: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return 0, fmt.Errorf("读取 Cloudflare 响应失败: %w", err)
	}
	if len(raw) > 1<<20 {
		return 0, fmt.Errorf("Cloudflare 响应超过大小限制")
	}
	var response cfResponse
	if err = json.Unmarshal(raw, &response); err != nil {
		return 0, fmt.Errorf("Cloudflare 返回无效响应 (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !response.Success {
		message := fmt.Sprintf("Cloudflare HTTP %d", res.StatusCode)
		if len(response.Errors) > 0 {
			message = fmt.Sprintf("Cloudflare %d: %s", response.Errors[0].Code, response.Errors[0].Message)
		}
		return 0, fmt.Errorf("%s", redact(message, token))
	}
	if result != nil {
		if err = json.Unmarshal(response.Result, result); err != nil {
			return 0, fmt.Errorf("Cloudflare 返回无效数据")
		}
	}
	return response.ResultInfo.TotalPages, nil
}
func (c *cloudflare) zones(ctx context.Context, token string) ([]zone, error) {
	var zones []zone
	for page := 1; page <= 100; page++ {
		var part []zone
		pages, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), token, nil, &part)
		if err != nil {
			return nil, err
		}
		zones = append(zones, part...)
		if pages <= page {
			return zones, nil
		}
	}
	return nil, fmt.Errorf("Cloudflare 域名数量超过查询上限，请填写 Zone ID")
}
func resolveZone(zones []zone, name string) string {
	best := zone{}
	for _, z := range zones {
		n := strings.ToLower(strings.TrimSuffix(z.Name, "."))
		if (name == n || strings.HasSuffix(name, "."+n)) && len(n) > len(best.Name) {
			best = zone{ID: z.ID, Name: n}
		}
	}
	return best.ID
}
func (c *cloudflare) lookup(ctx context.Context, token, zoneID, name, kind string) (*dnsRecord, error) {
	q := url.Values{"name": {name}, "type": {kind}, "per_page": {"100"}}
	var list []dnsRecord
	_, err := c.request(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/dns_records?"+q.Encode(), token, nil, &list)
	if err != nil {
		return nil, err
	}
	matches := []dnsRecord{}
	for _, r := range list {
		if strings.EqualFold(strings.TrimSuffix(r.Name, "."), name) && r.Type == kind {
			matches = append(matches, r)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("存在多条同名同类型 DNS 记录，请先在 Cloudflare 明确保留一条")
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if !validID(matches[0].ID) {
		return nil, fmt.Errorf("Cloudflare 返回无效记录 ID")
	}
	return &matches[0], nil
}
func (c *cloudflare) write(ctx context.Context, token, zoneID, id string, payload dnsRecord) error {
	method, path := http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records"
	// PATCH preserves unrelated Cloudflare metadata such as comments and tags.
	if id != "" {
		method = http.MethodPatch
		path += "/" + url.PathEscape(id)
	}
	_, err := c.request(ctx, method, path, token, payload, nil)
	return err
}
func sameIP(a, b string) bool {
	x, e := netip.ParseAddr(a)
	y, f := netip.ParseAddr(b)
	return e == nil && f == nil && x.Unmap() == y.Unmap()
}
func redact(message string, tokens ...string) string {
	for _, token := range tokens {
		if token != "" {
			message = strings.ReplaceAll(message, token, "[REDACTED]")
		}
	}
	message = strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " ")
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}
