package tenfoot

import (
	"net/http"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/hostclient"
)

// hostChanges tracks the library changes this launcher has sent and not yet
// had answered. A kit that is its own host can take half a minute over one
// (a new Atari ST disk took 27 s), and a status read sent meanwhile can time
// out. A room then keeps the last hardware snapshot it read rather than
// reporting the computer unavailable because of a change of our own.
type hostChanges struct {
	mu       sync.Mutex
	inFlight int
	started  uint64
	hardware *hostclient.HardwareSnapshot
}

// hostChangeMark is the change state when a status read starts.
type hostChangeMark struct {
	inFlight bool
	started  uint64
}

func (c *hostChanges) begin() {
	c.mu.Lock()
	c.inFlight++
	c.started++
	c.mu.Unlock()
}

func (c *hostChanges) end() {
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
}

func (c *hostChanges) mark() hostChangeMark {
	if c == nil {
		return hostChangeMark{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return hostChangeMark{inFlight: c.inFlight > 0, started: c.started}
}

func (c *hostChanges) keepHardware(snap hostclient.HardwareSnapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.hardware = &snap
	c.mu.Unlock()
}

// hardwareDuring is the last good snapshot when a change of ours overlapped
// the failed read that started at m: in flight when it started, started
// since, or still in flight now.
func (c *hostChanges) hardwareDuring(m hostChangeMark) (hostclient.HardwareSnapshot, bool) {
	if c == nil {
		return hostclient.HardwareSnapshot{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	overlapped := m.inFlight || c.inFlight > 0 || c.started != m.started
	if !overlapped || c.hardware == nil {
		return hostclient.HardwareSnapshot{}, false
	}
	return *c.hardware, true
}

// hostChangeTransport counts library changes while the host handles them.
// Reads, session control and UI events are not library changes.
type hostChangeTransport struct {
	base    http.RoundTripper
	changes *hostChanges
}

func (t hostChangeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isLibraryChange(req) {
		return t.base.RoundTrip(req)
	}
	t.changes.begin()
	defer t.changes.end()
	return t.base.RoundTrip(req)
}

func isLibraryChange(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	path := req.URL.Path
	for _, prefix := range []string{"/api/v1/library/", "/api/v1/core-media", "/api/v1/core-expansions", "/api/v1/core-catalog"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// trackHostChanges returns a copy of httpClient whose transport counts
// library changes into changes. The caller's settings are kept.
func trackHostChanges(httpClient *http.Client, changes *hostChanges) *http.Client {
	var out http.Client
	if httpClient != nil {
		out = *httpClient
	} else {
		out.Timeout = hostDefaultTimeout
	}
	base := out.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	out.Transport = hostChangeTransport{base: base, changes: changes}
	return &out
}
