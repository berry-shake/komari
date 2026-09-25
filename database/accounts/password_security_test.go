package accounts

import (
	"strings"
	"testing"

	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
)

func TestLegacyPasswordsUpgradeAfterSuccessfulLogin(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:password_security?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	password := strings.Repeat("long-password", 10)
	u := models.User{UUID: "legacy-user", Username: "legacy-user", Passwd: legacyHashPasswd(password), SSOID: "github_test"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := CheckPassword(u.Username, "incorrect"); ok {
		t.Fatal("accepted wrong password")
	}
	var before models.User
	db.First(&before, "uuid = ?", u.UUID)
	if before.Passwd != u.Passwd {
		t.Fatal("invalid login changed password")
	}
	if id, ok := CheckPassword(u.Username, password); !ok || id != u.UUID {
		t.Fatal("legacy login failed")
	}
	var after models.User
	if err := db.First(&after, "uuid = ?", u.UUID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(after.Passwd, passwordHashPrefix) || after.SSOID != u.SSOID {
		t.Fatal("migration failed or changed SSO")
	}
	if _, ok := CheckPassword(u.Username, password); !ok {
		t.Fatal("upgraded login failed")
	}
	if _, ok := CheckPassword(u.Username, password+"different"); ok {
		t.Fatal("long password suffix ignored")
	}
	other, err := CreateAccount("new-user", password)
	if err != nil {
		t.Fatal(err)
	}
	if other.Passwd == after.Passwd || !verifyPassword(password, other.Passwd) {
		t.Fatal("password hashes are not independently salted")
	}
	if err := ForceResetPassword(u.Username, "reset-password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := CheckPassword(u.Username, password); ok {
		t.Fatal("old password accepted after reset")
	}
	if _, ok := CheckPassword(u.Username, "reset-password"); !ok {
		t.Fatal("reset password rejected")
	}
}
