package admin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestEditSettingsRejectsRetiredIntegrationsAtomically(t *testing.T) {
	for _, key := range []string{"nezha_compat_enabled", "nezha_compat_listen", "cloudflare_tunnel_token"} {
		t.Run(key, func(t *testing.T) {
			db, _ := setupXtermJSTestDB(t)
			require.NoError(t, config.Set(config.SitenameKey, "unchanged"))
			router := gin.New()
			router.POST("/settings", EditSettings)
			body, err := json.Marshal(map[string]any{key: "obsolete", config.SitenameKey: "must-not-save"})
			require.NoError(t, err)
			response := performXtermJSRequest(t, router, http.MethodPost, "/settings", body)
			require.Equal(t, http.StatusBadRequest, response.Code)
			name, err := config.GetAs[string](config.SitenameKey)
			require.NoError(t, err)
			require.Equal(t, "unchanged", name)
			var count int64
			require.NoError(t, db.Model(&config.ConfigItem{}).Where("key = ?", key).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestGenerateTemporaryShareTokenUsesServerEntropyAndBounds(t *testing.T) {
	_, _ = setupXtermJSTestDB(t)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("uuid", "temporary-share-user")
		c.Next()
	})
	router.POST("/settings/tempory-share", GenerateTemporaryShareToken)

	first := performXtermJSRequest(t, router, http.MethodPost, "/settings/tempory-share", []byte(`{"hours":1}`))
	require.Equal(t, http.StatusOK, first.Code)
	var firstBody struct {
		Data struct {
			Token    string `json:"token"`
			ExpireAt int64  `json:"expire_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	require.Len(t, firstBody.Data.Token, 43)
	require.Regexp(t, regexp.MustCompile(`^[0-9A-Za-z]{43}$`), firstBody.Data.Token)
	require.InDelta(t, time.Now().Add(time.Hour).Unix(), firstBody.Data.ExpireAt, 3)

	second := performXtermJSRequest(t, router, http.MethodPost, "/settings/tempory-share", []byte(`{"hours":1}`))
	require.Equal(t, http.StatusOK, second.Code)
	var secondBody struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondBody))
	require.NotEqual(t, firstBody.Data.Token, secondBody.Data.Token)

	for _, body := range []string{`{"hours":0}`, `{"hours":721}`} {
		response := performXtermJSRequest(t, router, http.MethodPost, "/settings/tempory-share", []byte(body))
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
}

func TestGenerateTemporaryShareTokenIsRateLimited(t *testing.T) {
	_, _ = setupXtermJSTestDB(t)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("uuid", "temporary-share-rate-limit-user")
		c.Next()
	})
	router.POST("/settings/tempory-share", GenerateTemporaryShareToken)

	for i := 0; i < 5; i++ {
		response := performXtermJSRequest(t, router, http.MethodPost, "/settings/tempory-share", []byte(`{"hours":1}`))
		require.Equal(t, http.StatusOK, response.Code)
	}
	response := performXtermJSRequest(t, router, http.MethodPost, "/settings/tempory-share", []byte(`{"hours":1}`))
	require.Equal(t, http.StatusTooManyRequests, response.Code)
}
