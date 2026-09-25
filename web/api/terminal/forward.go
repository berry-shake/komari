package terminal

import (
	"github.com/gorilla/websocket"
	"github.com/komari-monitor/komari/database/auditlog"
	"time"
)

func ForwardTerminal(id string) {
	session := lookupSession(id)
	if session == nil {
		return
	}
	session.mu.Lock()
	agent := session.Agent
	closed := session.closed
	session.mu.Unlock()
	if closed || agent == nil || session.Browser == nil {
		return
	}
	defer session.close(id)
	auditlog.Log(session.RequesterIp, session.UserUUID, "established, terminal id:"+id, "terminal")
	started := time.Now()
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			kind, data, err := session.Browser.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.TextMessage || len(data) == 0 || data[0] != '{' {
				kind = websocket.BinaryMessage
			}
			agent.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if agent.WriteMessage(kind, data) != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := agent.ReadMessage()
			if err != nil || session.writeBrowser(websocket.BinaryMessage, data) != nil {
				return
			}
		}
	}()
	<-done
	session.close(id)
	<-done
	auditlog.Log(session.RequesterIp, session.UserUUID, "disconnected, terminal id:"+id+", duration:"+time.Since(started).String(), "terminal")
}
