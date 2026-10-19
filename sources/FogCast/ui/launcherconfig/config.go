// Package launcherconfig reads the kit's provisioned launcher.json: the host
// API origin, the per-kit launcher bearer and the kit's target ID.
package launcherconfig

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	API      string `json:"api"`
	Token    string `json:"token"`
	TargetID string `json:"target_id"`
}

var targetIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Load reads path. Keys left by the removed grid launcher (kit_ui, theme,
// shelf, menu_display and similar) are ignored, so older cards still load.
func Load(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, errors.New("launcher configuration unavailable")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8193))
	if d.Decode(&c) != nil {
		return Config{}, errors.New("invalid launcher configuration")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Config{}, errors.New("invalid launcher configuration")
	}
	u, err := url.Parse(c.API)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !validToken(c.Token) || !targetIDPattern.MatchString(c.TargetID) {
		return Config{}, errors.New("invalid launcher configuration")
	}
	c.API = strings.TrimRight(c.API, "/")
	return c, nil
}

func validToken(token string) bool {
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
