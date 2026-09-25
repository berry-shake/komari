package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/web/oauth/factory"
)

func init() {

}

func (g *Github) GetName() string {
	return "github"
}
func (g *Github) GetConfiguration() factory.Configuration {
	return &g.Addition
}

func (g *Github) GetAuthorizationURL(_ string) (string, string) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	now := time.Now()
	if g.states == nil {
		g.states = make(map[string]time.Time)
	}
	for key, expires := range g.states {
		if !now.Before(expires) {
			delete(g.states, key)
		}
	}
	if len(g.states) >= 1024 {
		return "", ""
	}
	state := utils.GenerateRandomString(32)

	// 构建GitHub OAuth授权URL
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&state=%s&scope=user:email",
		url.QueryEscape(g.Addition.ClientId),
		url.QueryEscape(state),
	)
	g.states[state] = now.Add(5 * time.Minute)
	return authURL, state
}
func (g *Github) OnCallback(ctx *gin.Context, state string, query map[string]string, _ string) (factory.OidcCallback, error) {
	code := query["code"]

	if !g.consumeState(state) {
		return factory.OidcCallback{}, fmt.Errorf("invalid or expired state")
	}

	// 获取code
	//code := c.Query("code")
	if code == "" {
		return factory.OidcCallback{}, fmt.Errorf("no code provided")
	}

	// 获取访问令牌
	tokenURL := "https://github.com/login/oauth/access_token"
	data := url.Values{
		"client_id":     {g.Addition.ClientId},
		"client_secret": {g.Addition.ClientSecret},
		"code":          {code},
	}

	req, _ := http.NewRequestWithContext(ctx.Request.Context(), "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return factory.OidcCallback{}, fmt.Errorf("failed to get access token: %s", utils.DataMasking(err.Error(), []string{g.Addition.ClientSecret, g.Addition.ClientId}))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return factory.OidcCallback{}, fmt.Errorf("GitHub token exchange failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokenResp); err != nil {
		return factory.OidcCallback{}, fmt.Errorf("failed to parse access token response: %s", utils.DataMasking(err.Error(), []string{g.Addition.ClientSecret, g.Addition.ClientId}))
	}

	if tokenResp.AccessToken == "" {
		return factory.OidcCallback{}, fmt.Errorf("GitHub returned an empty access token")
	}

	// 获取用户信息
	userReq, _ := http.NewRequestWithContext(ctx.Request.Context(), "GET", "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userReq.Header.Set("Accept", "application/json")

	userResp, err := client.Do(userReq)
	if err != nil {
		return factory.OidcCallback{}, fmt.Errorf("failed to get user info: %v", err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		return factory.OidcCallback{}, fmt.Errorf("GitHub user lookup failed: HTTP %d", userResp.StatusCode)
	}

	var githubUser GitHubUser
	if err := json.NewDecoder(io.LimitReader(userResp.Body, 1<<20)).Decode(&githubUser); err != nil {
		return factory.OidcCallback{}, fmt.Errorf("failed to parse user info response: %v", err)
	}

	if githubUser.ID <= 0 {
		return factory.OidcCallback{}, fmt.Errorf("GitHub returned an invalid user ID")
	}
	return factory.OidcCallback{UserId: fmt.Sprintf("%d", githubUser.ID)}, nil
}
func (g *Github) Init() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.states = make(map[string]time.Time)
	return nil
}
func (g *Github) Destroy() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.states = nil
	return nil
}

var _ factory.IOidcProvider = (*Github)(nil)

func (g *Github) consumeState(state string) bool {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	expires, ok := g.states[state]
	delete(g.states, state)
	return state != "" && ok && time.Now().Before(expires)
}
