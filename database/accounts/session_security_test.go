package accounts

import (
	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"testing"
	"time"
)

func TestSessionIdentifierCannotAuthenticateAndCanRevoke(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:session-security?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	user := models.User{UUID: "session-security-user", Username: "session-security-user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := "private-session-token-security-test"
	row := models.Session{UUID: user.UUID, Session: token, Expires: models.FromTime(time.Now().Add(time.Hour))}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	id := SessionID(token)
	if id == token || len(id) != 64 {
		t.Fatal("unsafe identifier")
	}
	if _, err := GetUserBySession(id); err == nil {
		t.Fatal("identifier authenticates")
	}
	if _, err := GetUserBySession(token); err != nil {
		t.Fatal("live session rejected")
	}
	if err := DeleteSessionByID(id); err != nil {
		t.Fatal(err)
	}
	if _, err := GetUserBySession(token); err == nil {
		t.Fatal("revocation failed")
	}
	row.Expires = models.FromTime(time.Now().Add(-time.Second))
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetUserBySession(token); err == nil {
		t.Fatal("expired session authenticates")
	}
}
