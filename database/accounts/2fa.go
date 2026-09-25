package accounts

import (
	"image"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/pquerna/otp/totp"
)

var (
	TwoFactorIssuer = "Komari Monitor"
)

func Generate2Fa() (string, image.Image, error) {
	otp, err := totp.Generate(totp.GenerateOpts{
		Issuer:      TwoFactorIssuer,
		AccountName: "komari",
	})
	if err != nil {
		return "", nil, err
	}
	img, err := otp.Image(250, 250)
	if err != nil {
		return "", nil, err
	}
	return otp.Secret(), img, nil
}

// Replace2Fa changes only the factor that was verified by the caller. A stale
// enrollment must not overwrite a factor enabled or replaced in another request.
func Replace2Fa(uuid, previous, secret string) (bool, error) {
	db := dbcore.GetDBInstance()
	result := db.Model(&models.User{}).
		Where("uuid = ? AND COALESCE(two_factor, '') = ?", uuid, previous).
		Update("two_factor", secret)
	return result.RowsAffected == 1, result.Error
}

func Verify2Fa(uuid, code string) (bool, error) {
	db := dbcore.GetDBInstance()
	var user models.User
	err := db.Where("uuid = ?", uuid).First(&user).Error
	if err != nil {
		return false, err
	}

	if user.TwoFactor == "" {
		return false, nil // 用户未启用2FA
	}

	valid := totp.Validate(code, user.TwoFactor)
	if !valid {
		return false, nil
	}

	return true, nil
}
