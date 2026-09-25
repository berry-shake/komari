package admin

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/ddns"
	"github.com/komari-monitor/komari/web/api"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDDNSAdminAPI(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "komari.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Client{}, &models.DDNSSettings{}, &models.DDNSRecord{}, &models.DDNSLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	const node = "11111111-1111-4111-8111-111111111111"
	if err = db.Create(&models.Client{UUID: node, Name: "Test node", Token: "private-agent-token"}).Error; err != nil {
		t.Fatal(err)
	}
	service := ddns.New(db, nil)
	t.Cleanup(service.Stop)
	router := gin.New()
	// Model the role set by IdentityMiddleware while exercising the real guard.
	router.Use(func(c *gin.Context) { c.Set("role", c.GetHeader("X-Test-Role")); c.Next() })
	RegisterDDNSRoutes(router.Group("/api/admin", api.RequireRole(api.RoleAdmin)), service)
	call := func(role, method, path, body string, expected int) string {
		t.Helper()
		r := httptest.NewRequest(method, "/api/admin/ddns/"+path, strings.NewReader(body))
		r.Header.Set("X-Test-Role", role)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, w.Code, expected, w.Body.String())
		}
		return w.Body.String()
	}
	for _, role := range []string{api.RoleGuest, api.RoleClient, ""} {
		for _, route := range [][2]string{{"GET", "settings"}, {"PUT", "settings"}, {"GET", "records"}, {"POST", "records"}, {"PUT", "records/x"}, {"DELETE", "records/x"}, {"GET", "nodes"}, {"POST", "sync"}, {"GET", "logs"}, {"DELETE", "logs"}} {
			call(role, route[0], route[1], "{}", 401)
		}
	}
	call(api.RoleAdmin, "PUT", "settings", `{"interval":5,"api_token":"private-cf-token"}`, 200)
	call(api.RoleAdmin, "PUT", "settings", `{"interval":10}`, 200)
	raw := call(api.RoleAdmin, "GET", "settings", "", 200)
	if strings.Contains(raw, "private-cf-token") || !strings.Contains(raw, `"api_token_set":true`) {
		t.Fatal("unsafe settings response")
	}
	var cfg models.DDNSSettings
	db.First(&cfg, 1)
	if cfg.APIToken != "private-cf-token" {
		t.Fatal("omitted token was lost")
	}
	for _, body := range []string{`{"interval":5,"unknown":true}`, `{"interval":5} {}`, `{"interval":0}`, `{"interval":5,"api_token":"` + strings.Repeat("x", 65536) + `"}`} {
		call(api.RoleAdmin, "PUT", "settings", body, 400)
	}
	raw = call(api.RoleAdmin, "POST", "records", `{"record_name":"home.example.com","record_type":"A","source_node":["`+node+`"],"ttl":60,"api_token":"private-record-token"}`, 200)
	if strings.Contains(raw, "private-record-token") {
		t.Fatal("record token leaked")
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatal(err)
	}
	raw = call(api.RoleAdmin, "GET", "nodes", "", 200)
	if strings.Contains(raw, "private-agent-token") {
		t.Fatal("node token leaked")
	}
	call(api.RoleAdmin, "POST", "sync", "", 200) // offline node must not make provider calls
	raw = call(api.RoleAdmin, "GET", "logs", "", 200)
	if strings.Contains(raw, "private-record-token") {
		t.Fatal("log token leaked")
	}
	call(api.RoleAdmin, "DELETE", "records/"+envelope.Data.ID, "", 200)
	call(api.RoleAdmin, "DELETE", "records/"+envelope.Data.ID, "", 404)
	call(api.RoleAdmin, "DELETE", "logs", "", 200)
	rows := make([]models.DDNSLog, 25)
	for i := range rows {
		rows[i] = models.DDNSLog{RecordName: "home.example.com", Action: "skip", Success: true}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data ddns.LogView `json:"data"`
	}
	raw = call(api.RoleAdmin, "GET", "logs?page=2&page_size=20&record=home.example.com&action=skip", "", 200)
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data.Total != 25 || page.Data.Page != 2 || page.Data.TotalPages != 2 || page.Data.PageSize != 20 || len(page.Data.Logs) != 5 {
		t.Fatalf("unexpected paginated response: %s", raw)
	}
	for _, query := range []string{"page=0", "page=-1", "page=abc", "page=99999999999999999999999", "page_size=0", "page_size=-2", "page_size=x", "limit=oops"} {
		call(api.RoleAdmin, "GET", "logs?"+query, "", 400)
	}
	raw = call(api.RoleAdmin, "GET", "logs?limit=5", "", 200)
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Data.Logs) != 5 || page.Data.PageSize != 5 {
		t.Fatalf("legacy limit parameter: %s %v", raw, err)
	}
}
