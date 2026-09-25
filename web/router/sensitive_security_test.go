package router_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
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
	"github.com/komari-monitor/komari/web/router"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type unreadUpload struct{ read bool }

func (b *unreadUpload) Read(p []byte) (int, error) { b.read = true; return 0, io.EOF }

func TestSensitiveRoutesRequireExistingFactor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:router_sensitive?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	const userID = "sensitive-route-user"
	const session = "sensitive-route-session"
	const factor = "JBSWY3DPEHPK3PXP"
	const apiKey = "sensitive-route-admin-api-key"
	require.NoError(t, db.Create(&models.User{UUID: userID, Username: userID, Passwd: "unused", TwoFactor: factor}).Error)
	require.NoError(t, db.Create(&models.Session{UUID: userID, Session: session, Expires: models.LocalTime(time.Now().Add(time.Hour))}).Error)
	require.NoError(t, config.Set(config.ApiKeyKey, ""))
	r := gin.New()
	r.Use(api.IdentityMiddleware())
	router.Register(r)
	call := func(method, path, body, auth, otp string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth == "session" {
			req.AddCookie(&http.Cookie{Name: "session_token", Value: session})
		} else if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-2FA-Code", otp)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	code := func() string { c, err := totp.GenerateCode(factor, time.Now()); require.NoError(t, err); return c }
	keyInDB := func() string { key, err := config.GetAs[string](config.ApiKeyKey); require.NoError(t, err); return key }

	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/admin/2fa/generate", ""},
		{"POST", "/api/admin/2fa/disable", ""},
		{"POST", "/api/admin/settings/", fmt.Sprintf(`{"api_key":%q}`, apiKey)},
		{"POST", "/api/admin/settings/api-key/reveal", ""},
		{"GET", "/api/admin/download/backup", ""},
		{"POST", "/api/admin/upload/backup", ""},
	} {
		t.Run("unauthenticated_"+route.path, func(t *testing.T) { require.Equal(t, 401, call(route.method, route.path, route.body, "", "").Code) })
		t.Run("session_without_otp_"+route.path, func(t *testing.T) {
			require.Equal(t, 401, call(route.method, route.path, route.body, "session", "").Code)
		})
	}
	require.Empty(t, keyInDB(), "creating a key without OTP must not succeed")
	require.Equal(t, 401, call("POST", "/api/admin/client/test-node/terminal/ticket", "", apiKey, "").Code, "a rejected new key cannot authorize a terminal")
	require.Equal(t, 401, call("POST", "/api/admin/client/test-node/terminal/ticket", "", "session", "").Code)

	t.Run("unauthorized_restore_does_not_read_upload", func(t *testing.T) {
		body := &unreadUpload{}
		req := httptest.NewRequest("POST", "/api/admin/upload/backup", body)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: session})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 401, w.Code)
		require.False(t, body.read)
		// A valid OTP reaches the upload handler; an empty body is rejected there.
		require.Equal(t, 400, call("POST", "/api/admin/upload/backup", "", "session", code()).Code)
	})

	t.Run("first_enrollment_endpoint_cannot_replace_factor", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/admin/2fa/enable", strings.NewReader(`{"code":"123456"}`))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: session})
		req.AddCookie(&http.Cookie{Name: "2fa_secret", Value: "JBSWY3DPEHPK3PXP"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 400, w.Code)
		var user models.User
		require.NoError(t, db.First(&user, "uuid = ?", userID).Error)
		require.Equal(t, factor, user.TwoFactor)
	})

	t.Run("creation_rotation_deletion_and_reveal", func(t *testing.T) {
		// A mixed settings update is rejected before *any* setting is written.
		require.NoError(t, config.Set("sitename", "unchanged"))
		w := call("POST", "/api/admin/settings/", fmt.Sprintf(`{"api_key":%q,"sitename":"unauthorized"}`, apiKey), "session", "invalid")
		require.Equal(t, 401, w.Code)
		name, err := config.GetAs[string]("sitename")
		require.NoError(t, err)
		require.Equal(t, "unchanged", name)
		require.Empty(t, keyInDB())
		require.Equal(t, 200, call("POST", "/api/admin/settings/", fmt.Sprintf(`{"api_key":%q}`, apiKey), "session", code()).Code)
		require.Equal(t, apiKey, keyInDB())
		for _, body := range []string{`{"api_key":"replacement-secret"}`, `{"api_key":""}`} {
			require.Equal(t, 401, call("POST", "/api/admin/settings/", body, "session", "").Code)
			require.Equal(t, apiKey, keyInDB())
		}
		for _, auth := range []string{"session", apiKey} {
			w = call("GET", "/api/admin/settings/", "", auth, "")
			require.Equal(t, 200, w.Code)
			var settings struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &settings))
			require.NotContains(t, settings.Data, "api_key")
			require.Equal(t, true, settings.Data["api_key_configured"])
			require.NotContains(t, w.Body.String(), apiKey)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		}
		w = call("POST", "/api/admin/settings/api-key/reveal", "", "session", code())
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), apiKey)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Equal(t, 200, call("POST", "/api/admin/settings/", `{"sitename":"ordinary-change","api_key_configured":false}`, "session", "").Code)
		require.Equal(t, apiKey, keyInDB())
		// JSON OTP compatibility must not persist verification secrets as settings.
		w = call("POST", "/api/admin/settings/", fmt.Sprintf(`{"api_key":"rotated-secret","2fa_code":%q}`, code()), "session", "")
		require.Equal(t, 200, w.Code)
		require.Equal(t, "rotated-secret", keyInDB())
		all, err := config.GetAll()
		require.NoError(t, err)
		require.NotContains(t, all, "2fa_code")
		require.NotContains(t, all, "api_key_configured")
		require.Equal(t, 200, call("POST", "/api/admin/settings/", `{"api_key":""}`, "session", code()).Code)
		require.Empty(t, keyInDB())
	})

	t.Run("backup_requires_otp_and_still_restores_credentials", func(t *testing.T) {
		require.Equal(t, 401, call("GET", "/api/admin/download/backup", "", "session", "invalid").Code)
		w := call("GET", "/api/admin/download/backup", "", "session", code())
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		require.NoError(t, err)
		found := false
		for _, file := range reader.File {
			if file.Name == "komari.db" {
				handle, err := file.Open()
				require.NoError(t, err)
				contents, err := io.ReadAll(handle)
				handle.Close()
				require.NoError(t, err)
				require.True(t, bytes.HasPrefix(contents, []byte("SQLite format 3")))
				require.Contains(t, string(contents), factor)
				found = true
			}
		}
		require.True(t, found)
	})

	t.Run("existing_api_key_automation_is_preserved", func(t *testing.T) {
		require.NoError(t, config.Set(config.ApiKeyKey, apiKey))
		require.Equal(t, 200, call("POST", "/api/admin/client/test-node/terminal/ticket", "", apiKey, "").Code)
		require.Equal(t, 200, call("POST", "/api/admin/settings/api-key/reveal", "", apiKey, "").Code)
		require.Equal(t, 401, call("POST", "/api/admin/2fa/generate", "", apiKey, "").Code)
		require.Equal(t, 401, call("POST", "/api/admin/2fa/disable", "", apiKey, "").Code)
	})

	t.Run("accounts_without_2fa_keep_their_existing_access", func(t *testing.T) {
		require.NoError(t, db.Model(&models.User{}).Where("uuid = ?", userID).Update("two_factor", "").Error)
		require.Equal(t, 200, call("POST", "/api/admin/settings/", `{"api_key":"no-factor-key"}`, "session", "").Code)
		require.Equal(t, "no-factor-key", keyInDB())
		require.Equal(t, 200, call("POST", "/api/admin/settings/api-key/reveal", "", "session", "").Code)
		require.Equal(t, 200, call("GET", "/api/admin/download/backup", "", "session", "").Code)
	})
}
