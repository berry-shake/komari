package admin

import (
	"encoding/json"
	"net/http"
	"testing"

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
