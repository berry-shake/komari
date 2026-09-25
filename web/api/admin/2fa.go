package admin

import (
	"image/png"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/accounts"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/web/api"
)

var twoFactorEnrollments enrollmentStore

func twoFactorUser(c *gin.Context) (*models.User, bool) {
	// Personal factors belong to a live browser session, never to a global API key.
	if c.GetString("session") == "" || c.GetString("uuid") == "" {
		api.RespondError(c, http.StatusUnauthorized, "A login session is required")
		return nil, false
	}
	user, err := accounts.GetUserByUUID(c.GetString("uuid"))
	if err != nil {
		api.RespondError(c, http.StatusUnauthorized, "Invalid login session")
		return nil, false
	}
	return &user, true
}

func enrollmentCookie(c *gin.Context, id string, age int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("2fa_enrollment", id, age, "/api/admin/2fa", "", utils.GetScheme(c) == "https", true)
	// Retire the legacy client-controlled secret cookie.
	c.SetCookie("2fa_secret", "", -1, "/", "", utils.GetScheme(c) == "https", true)
	c.Header("Cache-Control", "no-store")
}

func Generate2FA(c *gin.Context) {
	user, ok := twoFactorUser(c)
	if !ok {
		return
	}
	// First enrollment has no old factor. Replacement must verify the existing
	// factor before issuing a short-lived, session-bound enrollment grant.
	if err := api.VerifySensitiveFactor(c, user.UUID, user.TwoFactor); err != nil {
		api.RespondError(c, http.StatusUnauthorized, err.Error())
		return
	}
	secret, img, err := accounts.Generate2Fa()
	if err != nil {
		api.RespondError(c, 500, "Failed to generate 2FA")
		return
	}
	id, ok := twoFactorEnrollments.create(user.UUID, accounts.SessionID(c.GetString("session")), user.TwoFactor, secret)
	if !ok {
		api.RespondError(c, 429, "Too many pending 2FA enrollments")
		return
	}
	enrollmentCookie(c, id, 300)
	c.Header("Content-Type", "image/png")
	_ = png.Encode(c.Writer, img)
}

func Enable2FA(c *gin.Context) {
	user, ok := twoFactorUser(c)
	if !ok {
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Code == "" {
		api.RespondError(c, 400, "A new authenticator code is required")
		return
	}
	id, _ := c.Cookie("2fa_enrollment")
	if err := twoFactorEnrollments.complete(id, user.UUID, accounts.SessionID(c.GetString("session")), input.Code); err != nil {
		api.RespondError(c, 400, err.Error())
		return
	}
	enrollmentCookie(c, "", -1)
	api.RespondSuccess(c, "2FA enabled successfully")
}

func Disable2FA(c *gin.Context) {
	user, ok := twoFactorUser(c)
	if !ok {
		return
	}
	if err := api.VerifySensitiveFactor(c, user.UUID, user.TwoFactor); err != nil {
		api.RespondError(c, http.StatusUnauthorized, err.Error())
		return
	}
	// Serialize with enrollment completion and invalidate outstanding grants.
	twoFactorEnrollments.mu.Lock()
	defer twoFactorEnrollments.mu.Unlock()
	changed, err := accounts.Replace2Fa(user.UUID, user.TwoFactor, "")
	if err != nil {
		api.RespondError(c, 500, "Failed to disable 2FA")
		return
	}
	if !changed {
		api.RespondError(c, 409, "2FA changed; verify the current authenticator again")
		return
	}
	twoFactorEnrollments.removeUser(user.UUID)
	enrollmentCookie(c, "", -1)
	api.RespondSuccess(c, "")
}
