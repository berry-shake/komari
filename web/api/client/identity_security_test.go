package client

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/config"
	agent "github.com/komari-monitor/komari/web/agent"
	"github.com/komari-monitor/komari/web/api"
)

func TestAgentIdentityBoundary(t *testing.T) {
	t.Chdir(t.TempDir())
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:audit_review?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	if err := config.Set(config.GeoIpEnabledKey, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&models.Client{}, "uuid IN ?", []string{"audit-a", "audit-b"}) })
	if err := db.Create(&models.Client{UUID: "audit-a", Token: "audit-token-a", Name: "node-a", Hidden: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Client{UUID: "audit-b", Token: "audit-token-b", Name: "node-b"}).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(api.IdentityMiddleware())
	g := r.Group("/api/clients", api.RequireRole(api.RoleAdmin, api.RoleClient))
	g.POST("/report", UploadReport)
	g.POST("/uploadBasicInfo", UploadBasicInfo)
	t.Run("A01_cross_node_report", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/clients/report", strings.NewReader(`{"uuid":"audit-b","cpu":{"usage":73}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Client-Token", "audit-token-a")
		r.ServeHTTP(w, req)
		got := agent.GetLatestReport()["audit-b"]
		if w.Code != 403 || got != nil {
			t.Fatalf("cross-node report was accepted: code=%d, report=%+v", w.Code, got)
		}
		req = httptest.NewRequest("POST", "/api/clients/report", strings.NewReader(`{"cpu":{"usage":73}}`))
		req.Header.Set("X-Client-Token", "audit-token-a")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		own := agent.GetLatestReport()["audit-a"]
		if w.Code != 200 || own == nil || own.CPU.Usage != 73 {
			t.Fatal("own report rejected")
		}
		postPresenceMu.Lock()
		if p := postPresenceStates["audit-a"]; p != nil {
			p.timer.Stop()
		}
		delete(postPresenceStates, "audit-a")
		postPresenceMu.Unlock()
	})
	t.Run("A02_agent_writes_admin_fields", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/clients/uploadBasicInfo", strings.NewReader(`{"token":"audit-replacement","hidden":false,"name":"changed-by-agent","Token":"case-bypass","os":"safe-os"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Client-Token", "audit-token-a")
		r.ServeHTTP(w, req)
		var got models.Client
		if err := db.First(&got, "uuid = ?", "audit-a").Error; err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got.Token != "audit-token-a" || !got.Hidden || got.Name != "node-a" || got.OS != "safe-os" {
			t.Fatalf("management fields changed or safe fields rejected: code=%d", w.Code)
		}

	})
}
