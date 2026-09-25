package github

import (
	"github.com/komari-monitor/komari/web/oauth/factory"
	"sync"
	"time"
)

func init() {
	factory.RegisterOidcProvider(func() factory.IOidcProvider {
		return &Github{}
	})
}

type Github struct {
	Addition
	stateMu sync.Mutex
	states  map[string]time.Time
}

type Addition struct {
	ClientId     string `json:"client_id" required:"true"`
	ClientSecret string `json:"client_secret" required:"true"`
}

type GitHubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
}
