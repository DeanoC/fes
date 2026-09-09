// Package kitlauncher connects the on-kit controller shell to FogCast's host.
// It does not control the FPGA or claim the target lease.
package kitlauncher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/host/tenfoot"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
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
	Library *tenfoot.Client
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
	return &Client{config: c, HTTP: h, Library: tenfoot.NewClient(c.API, h)}
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
}

func (c *Client) Session(ctx context.Context) (Session, error) {
	var s Session
	err := c.get(ctx, "/api/v1/session", &s)
	return s, err
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.API+path, nil)
	if err != nil {
		return errors.New("invalid host request")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("host unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("host response %d", res.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out) != nil {
		return errors.New("invalid host response")
	}
	return nil
}
