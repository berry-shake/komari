package security

import (
	"sync"
	"time"
)

// StateStore atomically consumes short-lived, bounded OAuth/handshake tickets.
type StateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
}
type stateEntry struct {
	value   string
	expires time.Time
}

func (s *StateStore) Put(key, value string, lifetime time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]stateEntry)
	}
	now := time.Now()
	for k, v := range s.entries {
		if !now.Before(v.expires) {
			delete(s.entries, k)
		}
	}
	if len(s.entries) >= 1024 {
		return false
	}
	s.entries[key] = stateEntry{value, now.Add(lifetime)}
	return true
}
func (s *StateStore) Take(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	delete(s.entries, key)
	return entry.value, ok && key != "" && time.Now().Before(entry.expires)
}
func (s *StateStore) Clear() { s.mu.Lock(); s.entries = nil; s.mu.Unlock() }
