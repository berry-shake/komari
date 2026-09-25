package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/accounts"
	"github.com/komari-monitor/komari/web/security"
	"github.com/pquerna/otp/totp"
)

var sensitiveAttempts = security.NewAttemptLimiter(10, time.Minute)

func RequireSensitive2FA() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := VerifySensitive2FA(c); err != nil {
			RespondError(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}

func VerifySensitive2FA(c *gin.Context) error {
	if _, ok := c.Get("api_key"); ok {
		return nil
	}
	uuid, ok := c.Get("uuid")
	if !ok {
		return err2FARequired()
	}
	uuidString, ok := uuid.(string)
	if !ok || uuidString == "" {
		return err2FARequired()
	}
	user, err := accounts.GetUserByUUID(uuidString)
	if err != nil {
		return err
	}
	return VerifySensitiveFactor(c, uuidString, user.TwoFactor)
}

// VerifySensitiveFactor verifies the exact factor snapshot that a caller will
// use for a compare-and-swap update. It never bypasses verification for API keys.
func VerifySensitiveFactor(c *gin.Context, uuid, secret string) error {
	if secret == "" {
		return nil
	}
	code := get2FACode(c)
	if code == "" {
		return err2FARequired()
	}
	if !sensitiveAttempts.Allow(uuid) {
		return &sensitive2FAError{"Too many 2FA attempts; try again in one minute"}
	}
	if !totp.Validate(code, secret) {
		return err2FAInvalid()
	}
	return nil
}

func get2FACode(c *gin.Context) string {
	if code, ok := c.Get("2fa_code"); ok {
		if codeString, ok := code.(string); ok && codeString != "" {
			return codeString
		}
	}
	if code := c.GetHeader("X-2FA-Code"); code != "" {
		return code
	}
	if code := c.GetHeader("X-Two-Factor-Code"); code != "" {
		return code
	}
	for _, key := range []string{"2fa_code", "two_factor_code", "otp"} {
		if code := c.Query(key); code != "" {
			return code
		}
	}
	if c.Request.Body == nil || c.Request.Method == http.MethodGet {
		return ""
	}
	// Multipart backups carry the OTP in a header. Do not buffer an entire
	// upload merely to look for a JSON verification field.
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return ""
	}
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bodyBytes) == 0 {
		return ""
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return ""
	}
	for _, key := range []string{"2fa_code", "two_factor_code", "otp"} {
		if value, ok := body[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func err2FARequired() error {
	return &sensitive2FAError{"2FA code is required"}
}

func err2FAInvalid() error {
	return &sensitive2FAError{"Invalid 2FA code"}
}

type sensitive2FAError struct {
	message string
}

func (e *sensitive2FAError) Error() string {
	return e.message
}
