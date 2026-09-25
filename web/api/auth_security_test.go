package api_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/config"
	"github.com/komari-monitor/komari/web/api"
	publicapi "github.com/komari-monitor/komari/web/api/public"
	reportcache "github.com/komari-monitor/komari/web/report"
	"github.com/komari-monitor/komari/web/security"
)

type countingBody struct{ remaining, consumed int }

func (b *countingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := range p[:n] {
		p[i] = ' '
	}
	b.remaining -= n
	b.consumed += n
	return n, nil
}

func TestAuthenticationSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:auth_security?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	if err := config.Set(config.ApiKeyKey, "security-test-admin-key"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(config.PrivateSiteKey, true); err != nil {
		t.Fatal(err)
	}
	uuid := "00000000-0000-4000-8000-000000000001"
	if err := db.Create(&models.Client{UUID: uuid, Token: "security-test-node-token"}).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(api.IdentityMiddleware(), api.PrivateSiteMiddleware())
	r.POST("/api/admin/protected", api.RequireRole(api.RoleAdmin), func(c *gin.Context) { c.Status(204) })
	r.PUT("/api/admin/theme/upload", api.RequireRole(api.RoleAdmin), func(c *gin.Context) { c.Status(204) })
	r.POST("/api/clients/report", api.RequireRole(api.RoleClient), func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Error(err)
		}
		c.Data(200, "application/json", b)
	})
	r.GET("/api/recent/:uuid", publicapi.GetClientRecentRecords)
	r.GET("/api/public-extra", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/clients", func(c *gin.Context) { c.Status(204) })
	for _, path := range []string{"/api/admin/protected", "/api/clients/report"} {
		t.Run("bounded_"+path, func(t *testing.T) {
			body := &countingBody{remaining: 8 << 20}
			req := httptest.NewRequest("POST", path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if path == "/api/admin/protected" {
				if w.Code != 401 || body.consumed != 0 {
					t.Fatalf("status=%d consumed=%d", w.Code, body.consumed)
				}
			} else if w.Code != 413 || int64(body.consumed) > security.MaxMessageBytes+1 {
				t.Fatalf("status=%d consumed=%d", w.Code, body.consumed)
			}
		})
	}
	for _, auth := range []string{"header", "query", "body"} {
		t.Run("node_"+auth, func(t *testing.T) {
			path := "/api/clients/report"
			payload := `{"token":"security-test-node-token","cpu":1}`
			if auth == "query" {
				path += "?token=security-test-node-token"
			}
			req := httptest.NewRequest("POST", path, strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			if auth == "header" {
				req.Header.Set("X-Client-Token", "security-test-node-token")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 || w.Body.String() != payload {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	// An authenticated theme upload retains its larger limit.
	req := httptest.NewRequest("PUT", "/api/admin/theme/upload", bytes.NewReader(make([]byte, 2<<20)))
	req.Header.Set("Authorization", "Bearer security-test-admin-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("admin upload: %d", w.Code)
	}
	reportcache.Records.Set(uuid, []map[string]any{{"cpu": 12.5}}, time.Minute)
	for _, path := range []string{"/api/recent/" + uuid, "/api/public-extra", "/api/clients"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("private endpoint %s returned %d", path, w.Code)
		}
	}
	if err := config.Set(config.PrivateSiteKey, false); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/recent/"+uuid, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "12.5") {
		t.Fatalf("public recent: %d %s", w.Code, w.Body.String())
	}
	if err := db.Model(&models.Client{}).Where("uuid = ?", uuid).Update("hidden", true).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/recent/"+uuid, nil))
	if w.Code == http.StatusOK {
		t.Fatal("hidden node leaked")
	}
}
