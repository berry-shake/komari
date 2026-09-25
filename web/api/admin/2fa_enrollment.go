package admin

import (
	"errors"
	"sync"
	"time"

	"github.com/komari-monitor/komari/database/accounts"
	"github.com/komari-monitor/komari/utils"
	"github.com/pquerna/otp/totp"
)

type enrollment struct {
	user, session, previous, secret string
	expires                         time.Time
	attempts                        int
}

// Enrollment secrets are server-generated and never accepted from cookies or
// request bodies. Only a random grant ID goes to the browser. Grants are bounded,
// expire after five minutes and can succeed exactly once in their login session.
type enrollmentStore struct {
	mu      sync.Mutex
	entries map[string]enrollment
}

func (s *enrollmentStore) create(user, session, previous, secret string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]enrollment)
	}
	now := time.Now()
	for id, entry := range s.entries {
		if !now.Before(entry.expires) || (entry.user == user && entry.session == session) {
			delete(s.entries, id)
		}
	}
	if len(s.entries) >= 1024 {
		return "", false
	}
	id := utils.GenerateRandomString(48)
	if id == "" {
		return "", false
	}
	s.entries[id] = enrollment{user: user, session: session, previous: previous, secret: secret, expires: now.Add(5 * time.Minute)}
	return id, true
}

func (s *enrollmentStore) complete(id, user, session, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok || entry.user != user || entry.session != session || !time.Now().Before(entry.expires) {
		return errors.New("Invalid or expired 2FA enrollment; generate a new QR code")
	}
	entry.attempts++
	if !totp.Validate(code, entry.secret) {
		if entry.attempts >= 5 {
			delete(s.entries, id)
			return errors.New("Too many 2FA attempts; generate a new QR code")
		}
		s.entries[id] = entry
		return errors.New("Invalid new authenticator code")
	}
	delete(s.entries, id)
	changed, err := accounts.Replace2Fa(user, entry.previous, entry.secret)
	if err != nil {
		return errors.New("Failed to update 2FA")
	}
	if !changed {
		return errors.New("2FA changed; verify the current authenticator and start again")
	}
	s.removeUser(user)
	return nil
}

// Caller holds mu. A successful factor change invalidates every pending grant
// for the account, including grants created in other sessions.
func (s *enrollmentStore) removeUser(user string) {
	for id, entry := range s.entries {
		if entry.user == user {
			delete(s.entries, id)
		}
	}
}
