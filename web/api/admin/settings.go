package admin

import (
	"github.com/komari-monitor/komari/database/auditlog"
	"github.com/komari-monitor/komari/database/records"
	"github.com/komari-monitor/komari/database/tasks"
	"github.com/komari-monitor/komari/pkg/config"
	"github.com/komari-monitor/komari/web/api"

	"github.com/gin-gonic/gin"
)

// GetSettings 获取自定义配置
func GetSettings(c *gin.Context) {
	cst, err := config.GetAll()
	if err != nil {
		api.RespondError(c, 500, "Failed to get settings: "+err.Error())
		return
	}
	cst["api_key_configured"] = cst[config.ApiKeyKey] != nil && cst[config.ApiKeyKey] != ""
	delete(cst, config.ApiKeyKey)
	c.Header("Cache-Control", "no-store")
	api.RespondSuccess(c, cst)
}

// RevealAPIKey is deliberately separate from the routine settings read.
func RevealAPIKey(c *gin.Context) {
	if err := api.VerifySensitive2FA(c); err != nil {
		api.RespondError(c, 401, err.Error())
		return
	}
	key, err := config.GetAs[string](config.ApiKeyKey, "")
	if err != nil {
		api.RespondError(c, 500, "Failed to read API key")
		return
	}
	c.Header("Cache-Control", "no-store")
	api.RespondSuccess(c, gin.H{"api_key": key})
}

// EditSettings 更新自定义配置
func EditSettings(c *gin.Context) {
	cfg := make(map[string]interface{})
	if err := c.ShouldBindJSON(&cfg); err != nil {
		api.RespondError(c, 400, "Invalid or missing request body: "+err.Error())
		return
	}
	// Verification may inspect JSON. BindJSON has consumed it, so preserve the
	// submitted OTP in the context, then remove all verification/response metadata.
	for _, key := range []string{"2fa_code", "two_factor_code", "otp"} {
		if value, ok := cfg[key].(string); ok && value != "" && c.GetString("2fa_code") == "" {
			c.Set("2fa_code", value)
		}
		delete(cfg, key)
	}
	delete(cfg, "api_key_configured")
	if value, exists := cfg[config.ApiKeyKey]; exists {
		if err := api.VerifySensitive2FA(c); err != nil {
			api.RespondError(c, 401, err.Error())
			return
		}
		key, ok := value.(string)
		if !ok || (key != "" && len(key) < 12) {
			api.RespondError(c, 400, "API key must be empty or a string of at least 12 characters")
			return
		}
	}

	for _, key := range config.RemovedSettingKeys() {
		if _, exists := cfg[key]; exists {
			api.RespondError(c, 400, "Setting is no longer supported: "+key)
			return
		}
	}

	if err := config.SetMany(cfg); err != nil {
		api.RespondError(c, 500, "Failed to update settings: "+err.Error())
		return
	}

	uuid, _ := c.Get("uuid")
	message := "update settings: "
	for key := range cfg {
		message += key + ", "
	}
	if len(message) > 2 {
		message = message[:len(message)-2]
	}
	auditlog.Log(c.ClientIP(), uuid.(string), message, "info")
	api.RespondSuccess(c, nil)
}

func ClearAllRecords(c *gin.Context) {
	records.DeleteAll()
	tasks.DeleteAllPingRecords()
	uuid, _ := c.Get("uuid")
	auditlog.Log(c.ClientIP(), uuid.(string), "clear all records", "info")
	api.RespondSuccess(c, nil)
}
