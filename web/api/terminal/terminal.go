package terminal

import (
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

type TerminalSession struct {
	UUID         string
	UserUUID     string
	Browser      *websocket.Conn
	Agent        *websocket.Conn
	RequesterIp  string
	mu           sync.Mutex
	browserWrite sync.Mutex
	closed       bool
	connecting   bool
}

var TerminalSessionsMutex sync.Mutex
var TerminalSessions = make(map[string]*TerminalSession)

func lookupSession(id string) *TerminalSession {
	TerminalSessionsMutex.Lock()
	defer TerminalSessionsMutex.Unlock()
	return TerminalSessions[id]
}

func (s *TerminalSession) writeBrowser(kind int, data []byte) error {
	s.browserWrite.Lock()
	defer s.browserWrite.Unlock()
	s.Browser.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return s.Browser.WriteMessage(kind, data)
}

func (s *TerminalSession) close(id string) {
	s.mu.Lock()
	s.closed = true
	agent := s.Agent
	s.mu.Unlock()
	// Close is safe concurrently with reads/writes and releases blocked forwarding.
	if agent != nil {
		agent.Close()
	}
	if s.Browser != nil {
		s.Browser.Close()
	}
	TerminalSessionsMutex.Lock()
	if TerminalSessions[id] == s {
		delete(TerminalSessions, id)
	}
	TerminalSessionsMutex.Unlock()
}
