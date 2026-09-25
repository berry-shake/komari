package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/accounts"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/config"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/web/api"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

const oldFactor = "JBSWY3DPEHPK3PXP"

type factorFixture struct {
	router        *gin.Engine
	user, session string
}

func newFactorFixture(t *testing.T, factor string) factorFixture {
	t.Helper()
	ensureXtermJSAuditDB()
	db := dbcore.GetDBInstance()
	config.SetDb(db)
	require.NoError(t, config.Set(config.ApiKeyKey, ""))
	id := utils.GenerateRandomString(30)
	session := utils.GenerateRandomString(48)
	require.NoError(t, db.Create(&models.User{UUID: id, Username: id, Passwd: "unused", TwoFactor: factor}).Error)
	require.NoError(t, db.Create(&models.Session{UUID: id, Session: session, Expires: models.LocalTime(time.Now().Add(time.Hour))}).Error)
	t.Cleanup(func() {
		twoFactorEnrollments.mu.Lock()
		twoFactorEnrollments.removeUser(id)
		twoFactorEnrollments.mu.Unlock()
		db.Where("uuid = ?", id).Delete(&models.Session{})
		db.Where("uuid = ?", id).Delete(&models.User{})
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(api.IdentityMiddleware(), api.RequireRole(api.RoleAdmin))
	r.POST("/generate", Generate2FA)
	r.POST("/enable", Enable2FA)
	r.POST("/disable", Disable2FA)
	return factorFixture{r, id, session}
}

func (f factorFixture) call(path, body, code string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "https://komari.test"+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: f.session})
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-2FA-Code", code)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func factorCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	return code
}

func (f factorFixture) generate(t *testing.T, code string) (*http.Cookie, string) {
	t.Helper()
	w := f.call("/generate", "", code)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "2fa_enrollment" {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, "/api/admin/2fa", cookie.Path)
	twoFactorEnrollments.mu.Lock()
	entry := twoFactorEnrollments.entries[cookie.Value]
	twoFactorEnrollments.mu.Unlock()
	require.NotEmpty(t, entry.secret)
	require.NotEqual(t, entry.secret, cookie.Value)
	return cookie, entry.secret
}

func (f factorFixture) stored(t *testing.T) string {
	t.Helper()
	user, err := accounts.GetUserByUUID(f.user)
	require.NoError(t, err)
	return user.TwoFactor
}

func TestTwoFactorRejectsClientControlledSecrets(t *testing.T) {
	for _, factor := range []string{"", oldFactor} {
		t.Run(fmt.Sprint(factor != ""), func(t *testing.T) {
			f := newFactorFixture(t, factor)
			forged, _, err := accounts.Generate2Fa()
			require.NoError(t, err)
			body, _ := json.Marshal(map[string]string{"code": factorCode(t, forged), "secret": forged})
			w := f.call("/enable", string(body), "", &http.Cookie{Name: "2fa_secret", Value: forged})
			require.Equal(t, 400, w.Code)
			require.Equal(t, factor, f.stored(t))
		})
	}
}

func TestTwoFactorEnrollmentAndReplacement(t *testing.T) {
	f := newFactorFixture(t, "")
	cookie, first := f.generate(t, "")
	// Invalid input can be retried without changing the account.
	require.Equal(t, 400, f.call("/enable", `{"code":"invalid"}`, "", cookie).Code)
	require.Empty(t, f.stored(t))
	body := fmt.Sprintf(`{"code":%q}`, factorCode(t, first))
	require.Equal(t, 200, f.call("/enable", body, "", cookie).Code)
	require.Equal(t, first, f.stored(t))
	require.Equal(t, 400, f.call("/enable", body, "", cookie).Code, "grant replay")
	require.Equal(t, 401, f.call("/generate", "", "").Code)
	require.Equal(t, 401, f.call("/generate", "", "invalid").Code)
	require.Equal(t, 401, f.call("/disable", "", "").Code)
	replacement, next := f.generate(t, factorCode(t, first))
	require.Equal(t, first, f.stored(t), "old factor remains active until confirmation")
	require.Equal(t, 200, f.call("/enable", fmt.Sprintf(`{"code":%q}`, factorCode(t, next)), "", replacement).Code)
	require.Equal(t, next, f.stored(t))
	require.Equal(t, 401, f.call("/disable", "", factorCode(t, first)).Code)
	require.Equal(t, 200, f.call("/disable", "", factorCode(t, next)).Code)
	require.Empty(t, f.stored(t))
}

func TestTwoFactorEnrollmentBindingExpiryAndAttempts(t *testing.T) {
	f := newFactorFixture(t, oldFactor)
	for _, scenario := range []string{"different_session", "different_user", "expired", "attempts", "factor_changed", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			cookie, secret := f.generate(t, factorCode(t, oldFactor))
			other := f
			switch scenario {
			case "different_session":
				other.session = utils.GenerateRandomString(48)
				require.NoError(t, dbcore.GetDBInstance().Create(&models.Session{UUID: f.user, Session: other.session, Expires: models.LocalTime(time.Now().Add(time.Hour))}).Error)
			case "different_user":
				other = newFactorFixture(t, oldFactor)
			case "expired":
				twoFactorEnrollments.mu.Lock()
				entry := twoFactorEnrollments.entries[cookie.Value]
				entry.expires = time.Now().Add(-time.Second)
				twoFactorEnrollments.entries[cookie.Value] = entry
				twoFactorEnrollments.mu.Unlock()
			case "attempts":
				for i := 0; i < 5; i++ {
					require.Equal(t, 400, f.call("/enable", `{"code":"invalid"}`, "", cookie).Code)
				}
			case "factor_changed":
				require.NoError(t, dbcore.GetDBInstance().Model(&models.User{}).Where("uuid = ?", f.user).Update("two_factor", "another-factor").Error)
			case "disabled":
				require.Equal(t, 200, f.call("/disable", "", factorCode(t, oldFactor)).Code)
			}
			require.Equal(t, 400, other.call("/enable", fmt.Sprintf(`{"code":%q}`, factorCode(t, secret)), "", cookie).Code)
			require.NotEqual(t, secret, f.stored(t))
			require.NoError(t, dbcore.GetDBInstance().Model(&models.User{}).Where("uuid = ?", f.user).Update("two_factor", oldFactor).Error)
		})
	}
}

func TestTwoFactorEnrollmentIsSingleUseUnderConcurrency(t *testing.T) {
	f := newFactorFixture(t, "")
	cookie, secret := f.generate(t, "")
	code := factorCode(t, secret)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if twoFactorEnrollments.complete(cookie.Value, f.user, accounts.SessionID(f.session), code) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	require.Equal(t, secret, f.stored(t))
}

func TestTwoFactorEnrollmentStoreBounded(t *testing.T) {
	var store enrollmentStore
	for i := 0; i < 1024; i++ {
		_, ok := store.create("user", fmt.Sprint(i), "", "secret")
		require.True(t, ok)
	}
	_, ok := store.create("user", "overflow", "", "secret")
	require.False(t, ok)
	// Reopening in the same session replaces its grant rather than filling the store.
	_, ok = store.create("user", "0", "", "new-secret")
	require.True(t, ok)
}

func TestTwoFactorVerificationIsRateLimited(t *testing.T) {
	f := newFactorFixture(t, oldFactor)
	for i := 0; i < 10; i++ {
		require.Equal(t, 401, f.call("/generate", "", "invalid").Code)
	}
	w := f.call("/generate", "", factorCode(t, oldFactor))
	require.Equal(t, 401, w.Code)
	require.Contains(t, w.Body.String(), "Too many 2FA attempts")
	require.Equal(t, oldFactor, f.stored(t))
}
