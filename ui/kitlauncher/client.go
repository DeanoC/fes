// Package kitlauncher connects the on-kit controller shell to FogCast's host.
package kitlauncher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

type Config struct {
	API          string `json:"api"`
	Token        string `json:"token"`
	TargetID     string `json:"target_id"`
	Framebuffer  string `json:"framebuffer,omitempty"`
	InputProfile string `json:"input_profile,omitempty"`
	Theme        string `json:"theme,omitempty"`
	Shelf        string `json:"shelf,omitempty"`
	AudioChrome  bool   `json:"audio_chrome,omitempty"`
	path         string `json:"-"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, errors.New("launcher configuration unavailable")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8193))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return Config{}, errors.New("invalid launcher configuration")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Config{}, errors.New("invalid launcher configuration")
	}
	u, err := url.Parse(c.API)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !validLauncherToken(c.Token) || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(c.TargetID) {
		return Config{}, errors.New("invalid launcher configuration")
	}
	c.API = strings.TrimRight(c.API, "/")
	c.Shelf = normalizeShelf(c.Shelf)
	c.path = path
	return c, nil
}

func SaveConfig(c Config) error {
	if strings.TrimSpace(c.path) == "" {
		return nil
	}
	out := struct {
		API          string `json:"api"`
		Token        string `json:"token"`
		TargetID     string `json:"target_id"`
		Framebuffer  string `json:"framebuffer,omitempty"`
		InputProfile string `json:"input_profile,omitempty"`
		Theme        string `json:"theme,omitempty"`
		Shelf        string `json:"shelf,omitempty"`
		AudioChrome  bool   `json:"audio_chrome,omitempty"`
	}{
		API:          c.API,
		Token:        c.Token,
		TargetID:     c.TargetID,
		Framebuffer:  c.Framebuffer,
		InputProfile: c.InputProfile,
		Theme:        c.Theme,
		Shelf:        normalizeShelf(c.Shelf),
		AudioChrome:  c.AudioChrome,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return errors.New("invalid launcher configuration")
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return errors.New("launcher configuration unavailable")
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return errors.New("launcher configuration unavailable")
	}
	return nil
}

func validLauncherToken(token string) bool {
	if len(token) < 32 || len(token) > 256 || strings.TrimSpace(token) != token {
		return false
	}
	for _, b := range []byte(token) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

type Client struct {
	config  Config
	HTTP    *http.Client
	Library *hostclient.Client
	Cache   *DiskStore
}
type authenticated struct {
	base   http.RoundTripper
	config Config
}

func (a authenticated) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.Clone(r.Context())
	q.Header.Set("Authorization", "Bearer "+a.config.Token)
	q.Header.Set("X-FogCast-Target-ID", a.config.TargetID)
	return a.base.RoundTrip(q)
}
func NewClient(c Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// Poll and mutation clients carry different whole-request deadlines.
	// A shared header deadline would incorrectly shorten Launch/Stop.
	h := &http.Client{Transport: authenticated{transport, c}, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client := &Client{config: c, HTTP: h, Library: hostclient.NewClient(c.API, h)}
	if root := cacheRoot(c); root != "" {
		if store, err := OpenDiskStore(root); err == nil {
			client.Cache = store
		}
	}
	return client
}

type Session struct {
	State     string `json:"state"`
	GameID    string `json:"game_id"`
	Execution string `json:"execution"`
	Input     struct {
		State     string `json:"state"`
		Ready     bool   `json:"ready"`
		SessionID string `json:"session_id"`
	} `json:"input"`
	CorePackage *CorePackageSession `json:"core_package,omitempty"`
}

type CorePackageSession struct {
	Generation       uint64 `json:"generation"`
	Gamepad          bool   `json:"gamepad"`
	ActiveInterfaces []struct {
		ID    string `json:"id"`
		Major uint16 `json:"major"`
		Minor uint16 `json:"minor"`
	} `json:"active_interfaces"`
}

func (p *CorePackageSession) HasKeyboard() bool {
	if p == nil {
		return false
	}
	for _, contract := range p.ActiveInterfaces {
		if contract.ID == "fes.keyboard" && contract.Major == 1 && contract.Minor == 0 {
			return true
		}
	}
	return false
}

func (c *Client) Session(ctx context.Context) (Session, error) {
	result, err := hostclient.GetSession(ctx, c.HTTP, c.config.API, 1<<20)
	session := adaptSession(result)
	if err == nil {
		return session, nil
	}
	switch {
	case result.HTTPStatus == 0:
		return session, errors.New("host unreachable")
	case result.HTTPStatus == http.StatusOK:
		return session, errors.New("invalid host response")
	default:
		return session, fmt.Errorf("host response %d", result.HTTPStatus)
	}
}

func adaptSession(result hostclient.SessionResult) Session {
	session := Session{State: result.State, GameID: result.GameID, Execution: result.Execution}
	if result.Input != nil {
		session.Input.State = result.Input.State
		session.Input.Ready = result.Input.Ready
		session.Input.SessionID = result.Input.SessionID
	}
	if result.CorePackage == nil {
		return session
	}
	packageSession := &CorePackageSession{
		Generation: result.CorePackage.Generation,
		Gamepad:    result.CorePackage.Gamepad,
	}
	packageSession.ActiveInterfaces = make([]struct {
		ID    string `json:"id"`
		Major uint16 `json:"major"`
		Minor uint16 `json:"minor"`
	}, len(result.CorePackage.ActiveInterfaces))
	for i, contract := range result.CorePackage.ActiveInterfaces {
		packageSession.ActiveInterfaces[i].ID = contract.ID
		packageSession.ActiveInterfaces[i].Major = contract.Major
		packageSession.ActiveInterfaces[i].Minor = contract.Minor
	}
	session.CorePackage = packageSession
	return session
}
