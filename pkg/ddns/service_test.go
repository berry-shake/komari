package ddns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testNode = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const testZone = "11111111111111111111111111111111"
const testRecord = "22222222222222222222222222222222"

func setup(t *testing.T, online bool) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "komari.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Client{}, &models.DDNSSettings{}, &models.DDNSRecord{}, &models.DDNSLog{}); err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { sql.Close() })
	if err = db.Create(&models.Client{UUID: testNode, Token: "node-secret", Name: "Test node", IPv4: "8.8.8.8", IPv6: "2001:4860:4860::8888"}).Error; err != nil {
		t.Fatal(err)
	}
	s := New(db, func() []string {
		if online {
			return []string{testNode}
		}
		return nil
	})
	t.Cleanup(s.Stop)
	return s
}
func pointer(s string) *string { return &s }
func configure(t *testing.T, s *Service) RecordView {
	t.Helper()
	if err := s.SaveSettings(SettingsInput{Interval: 5, APIToken: pointer("global-secret")}); err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveRecord("", RecordInput{RecordName: "home.example.com", RecordType: "A", SourceNodes: []string{testNode}, TTL: 60})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func mockCloudflare(t *testing.T, s *Service, fn http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(fn)
	t.Cleanup(server.Close)
	s.cf = &cloudflare{base: server.URL, http: server.Client()}
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v, "result_info": map[string]int{"total_pages": 1}})
}
func TestSyncCreateUpdateNoopAndTTL(t *testing.T) {
	s := setup(t, true)
	r := configure(t, s)
	var current *dnsRecord
	writes := []string{}
	zones := 0
	mockCloudflare(t, s, func(w http.ResponseWriter, q *http.Request) {
		if q.Header.Get("Authorization") != "Bearer global-secret" {
			t.Error("token not sent as bearer")
		}
		if q.URL.Path == "/zones" {
			zones++
			respond(w, []zone{{ID: "33333333333333333333333333333333", Name: "com"}, {ID: testZone, Name: "example.com"}})
			return
		}
		if !strings.HasPrefix(q.URL.Path, "/zones/"+testZone+"/dns_records") {
			t.Error("wrong zone selected")
		}
		switch q.Method {
		case "GET":
			if current == nil {
				respond(w, []dnsRecord{})
			} else {
				respond(w, []dnsRecord{*current})
			}
		case "POST", "PATCH":
			var payload dnsRecord
			if err := json.NewDecoder(q.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			payload.ID = testRecord
			current = &payload
			writes = append(writes, q.Method)
			respond(w, current)
		default:
			t.Error("unexpected DNS method", q.Method)
		}
	})
	result, err := s.Sync(context.Background())
	if err != nil || result.Changed != 1 || result.Records[0].Action != "create" {
		t.Fatalf("create: %+v %v", result, err)
	}
	result, err = s.Sync(context.Background())
	if err != nil || result.Skipped != 1 || len(writes) != 1 {
		t.Fatalf("noop: %+v %v", result, err)
	}
	_, err = s.SaveRecord(r.ID, RecordInput{RecordName: r.RecordName, RecordType: "A", SourceNodes: []string{testNode}, TTL: 120})
	if err != nil {
		t.Fatal(err)
	}
	result, err = s.Sync(context.Background())
	if err != nil || result.Changed != 1 || current.TTL != 120 || writes[1] != "PATCH" {
		t.Fatalf("TTL-only change: %+v %v", result, err)
	}
	if zones != 3 {
		t.Fatalf("zone lookup count %d", zones)
	}
	records, _ := s.Records()
	if records[0].LastIP != "8.8.8.8" || records[0].LastUpdateAt == nil {
		t.Fatalf("state not persisted: %+v", records)
	}
	other := New(s.db, s.online)
	defer other.Stop()
	after, _ := other.Records()
	cfg, _ := other.Settings()
	if len(after) != 1 || after[0].LastIP != "8.8.8.8" || !cfg.TokenSet {
		t.Fatal("configuration/state lost across service restart")
	}
	if err = s.DeleteRecord(r.ID); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatal("local removal changed Cloudflare")
	}
}
func TestAAAAProxyOverrideAndNotification(t *testing.T) {
	s := setup(t, true)
	configure(t, s)
	if err := s.SaveSettings(SettingsInput{Interval: 5, Notify: true}); err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveRecord("", RecordInput{RecordName: "v6.example.com", RecordType: "AAAA", ZoneID: testZone, SourceNodes: []string{testNode}, TTL: 120, Proxied: true, APIToken: pointer("record-secret")})
	if err != nil {
		t.Fatal(err)
	}
	notifications := 0
	s.notify = func(event models.EventMessage) error {
		notifications++
		if event.Event != "DDNS" || event.Clients[0].UUID != testNode {
			t.Error("notification event lacks node association")
		}
		return nil
	}
	mockCloudflare(t, s, func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/zones" {
			respond(w, []zone{{ID: testZone, Name: "example.com"}})
			return
		}
		if q.Method == "GET" {
			kind := q.URL.Query().Get("type")
			if kind == "A" {
				respond(w, []dnsRecord{{ID: testRecord, Type: "A", Name: "home.example.com", Content: "8.8.8.8", TTL: 60}})
			} else {
				respond(w, []dnsRecord{})
			}
			return
		}
		if q.Header.Get("Authorization") != "Bearer record-secret" {
			t.Error("per-record override missing")
		}
		var payload dnsRecord
		_ = json.NewDecoder(q.Body).Decode(&payload)
		if payload.Type != "AAAA" || payload.Content != "2001:4860:4860::8888" || payload.TTL != 1 || !payload.Proxied {
			t.Errorf("bad AAAA/proxy payload: %+v", payload)
		}
		respond(w, payload)
	})
	result, err := s.Sync(context.Background())
	if err != nil || result.Changed != 1 || notifications != 1 {
		t.Fatalf("AAAA: %+v %v notify=%d", result, err, notifications)
	}
	_, err = s.SaveRecord(r.ID, RecordInput{RecordName: "renamed.example.com", RecordType: "AAAA", ZoneID: testZone, SourceNodes: []string{testNode}, TTL: 1})
	if err != nil {
		t.Fatal(err)
	}
	var stored models.DDNSRecord
	s.db.First(&stored, "id = ?", r.ID)
	if stored.APIToken != "record-secret" {
		t.Fatal("rename lost secret")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "record-secret") || strings.Contains(string(raw), "global-secret") {
		t.Fatal("record response leaks token")
	}
	cfg, _ := s.Settings()
	raw, _ = json.Marshal(cfg)
	if strings.Contains(string(raw), "global-secret") {
		t.Fatal("settings response leaks token")
	}
}
func TestSkipOfflineAndPrivateWithoutProviderCalls(t *testing.T) {
	for _, online := range []bool{false, true} {
		t.Run(fmt.Sprint(online), func(t *testing.T) {
			s := setup(t, online)
			configure(t, s)
			if online {
				s.db.Model(&models.Client{}).Where("uuid = ?", testNode).Update("ipv4", "192.168.1.10")
			}
			mockCloudflare(t, s, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid node must not contact provider") })
			result, err := s.Sync(context.Background())
			if err != nil || result.Skipped != 1 {
				t.Fatalf("skip: %+v %v", result, err)
			}
		})
	}
}
func TestProviderErrorRedactionAndAmbiguousRecords(t *testing.T) {
	s := setup(t, true)
	configure(t, s)
	mockCloudflare(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"bad global-secret"}]}`)
	})
	result, err := s.Sync(context.Background())
	if err != nil || result.Errors != 1 {
		t.Fatalf("provider error: %+v %v", result, err)
	}
	records, _ := s.Records()
	logs, _ := s.Logs("", "", 1, 100)
	raw, _ := json.Marshal([]any{result, records, logs})
	if strings.Contains(string(raw), "global-secret") || !strings.Contains(string(raw), "REDACTED") {
		t.Fatal("secret redaction missing")
	}
	mockCloudflare(t, s, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			respond(w, []zone{{ID: testZone, Name: "example.com"}})
			return
		}
		if r.Method != "GET" {
			t.Error("ambiguous record was written")
		}
		respond(w, []dnsRecord{{ID: testRecord, Name: "home.example.com", Type: "A"}, {ID: testZone, Name: "home.example.com", Type: "A"}})
	})
	result, err = s.Sync(context.Background())
	if err != nil || result.Errors != 1 {
		t.Fatalf("ambiguous: %+v %v", result, err)
	}
}
func TestConcurrentSyncAndEditAreRejected(t *testing.T) {
	s := setup(t, true)
	configure(t, s)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mockCloudflare(t, s, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		respond(w, []zone{})
	})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.Sync(context.Background()) }()
	<-entered
	if _, err := s.Sync(context.Background()); err != ErrBusy {
		t.Fatalf("parallel sync: %v", err)
	}
	if err := s.SaveSettings(SettingsInput{Interval: 5}); err != ErrBusy {
		t.Fatalf("edit during sync: %v", err)
	}
	close(release)
	<-done
	if err := s.SaveSettings(SettingsInput{Interval: 5}); err != nil {
		t.Fatal(err)
	}
	// A scheduled callback that was queued before disable must recheck under lock.
	calls := 0
	mockCloudflare(t, s, func(w http.ResponseWriter, r *http.Request) { calls++; respond(w, []zone{}) })
	_, err := s.sync(context.Background(), true)
	if err != nil || calls != 0 {
		t.Fatal("disabled scheduler contacted Cloudflare")
	}
}
func TestValidationTokenPreservationAndBoundedLogs(t *testing.T) {
	s := setup(t, true)
	r := configure(t, s)
	for _, in := range []RecordInput{{RecordName: "invalid"}, {RecordName: "home.example.com", RecordType: "TXT", SourceNodes: []string{testNode}, TTL: 60}, {RecordName: "home.example.com", RecordType: "A", SourceNodes: []string{testNode}, TTL: 2}, {RecordName: "other.example.com", RecordType: "A", ZoneID: "../escape", SourceNodes: []string{testNode}, TTL: 60}} {
		if _, err := s.SaveRecord("", in); err == nil {
			t.Errorf("accepted invalid record: %+v", in)
		}
	}
	if err := s.SaveSettings(SettingsInput{Interval: 0}); err == nil {
		t.Fatal("accepted zero interval")
	}
	if err := s.SaveSettings(SettingsInput{Interval: 10}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.settings()
	if cfg.APIToken != "global-secret" {
		t.Fatal("omitted token erased saved token")
	}
	if err := s.SaveSettings(SettingsInput{Interval: 10, ClearToken: true}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.settings()
	if cfg.APIToken != "" {
		t.Fatal("explicit clear ignored")
	}
	rows := make([]models.DDNSLog, 505)
	for i := range rows {
		rows[i] = models.DDNSLog{Time: time.Now(), RecordName: r.RecordName, Action: "skip", Success: true}
	}
	if err := s.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := appendLog(s.db, models.DDNSLog{Action: "sync"}); err != nil {
		t.Fatal(err)
	}
	logs, err := s.Logs("", "", 1, 1000)
	if err != nil || logs.Total != 500 || len(logs.Logs) != 500 {
		t.Fatalf("retention: %d %d %v", logs.Total, len(logs.Logs), err)
	}
	if err = s.ClearLogs(); err != nil {
		t.Fatal(err)
	}
}
func TestDomainZoneAndAddressNormalization(t *testing.T) {
	name, err := normalizeName(" Home.Example.COM. ")
	if err != nil || name != "home.example.com" {
		t.Fatal(name, err)
	}
	if resolveZone([]zone{{ID: "a", Name: "example.com"}, {ID: "b", Name: "sub.example.com"}}, "home.sub.example.com") != "b" {
		t.Fatal("did not select longest suffix")
	}
	if resolveZone([]zone{{ID: "a", Name: "example.com"}}, "notexample.com") != "" {
		t.Fatal("zone suffix lacks label boundary")
	}
	for _, ip := range []string{"127.0.0.1", "192.168.0.1", "100.64.0.1", "::1", "fe80::1", "invalid"} {
		if publicIP(ip, "A") != "" || publicIP(ip, "AAAA") != "" {
			t.Fatal("accepted nonpublic address", ip)
		}
	}
	if !sameIP("2001:4860:0:0::8888", "2001:4860::8888") {
		t.Fatal("IPv6 spelling would cause repeated updates")
	}
}

func TestCloudflarePaginationAndHTTPBoundaries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("per_page") != "50" {
			t.Error("wrong zone page size")
		}
		page := r.URL.Query().Get("page")
		fmt.Fprintf(w, `{"success":true,"result":[{"id":"%s","name":"page%s.example.com"}],"result_info":{"total_pages":2}}`, testZone, page)
	}))
	defer server.Close()
	cf := newCloudflare()
	cf.base = server.URL
	zones, err := cf.zones(context.Background(), "secret")
	if err != nil || len(zones) != 2 || calls != 2 {
		t.Fatalf("pagination: %d %d %v", len(zones), calls, err)
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed API redirect with credentials") }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	cf.base = redirect.URL
	if _, err = cf.zones(context.Background(), "secret"); err == nil {
		t.Fatal("accepted API redirect")
	}
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502); fmt.Fprint(w, "upstream raw secret") }))
	defer invalid.Close()
	cf.base = invalid.URL
	if _, err = cf.zones(context.Background(), "secret"); err == nil || strings.Contains(err.Error(), "raw secret") {
		t.Fatal("exposed invalid provider response", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = cf.zones(ctx, "secret"); err == nil {
		t.Fatal("ignored canceled request")
	}
}
