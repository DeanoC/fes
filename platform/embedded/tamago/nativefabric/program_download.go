// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// ProgramSource reads an existing Nano export tree over HTTP. It owns no
// listener, storage lease or peripheral; its supplied transport must honor
// request cancellation. Network cancellation is not a DMA fence.
type ProgramSource struct {
	client http.Client
	base   url.URL
	host   string
}

// NewProgramSource fixes the numeric IPv4 HTTP origin and optional virtual Host
// header. The caller initializes networking independently. Redirects and content
// transformations are refused; the caller's client configuration is not mutated.
func NewProgramSource(client *http.Client, baseURL, host string) (*ProgramSource, error) {
	if client == nil || strings.ContainsAny(host, "\r\n\x00") {
		return nil, errors.New("native download: explicit HTTP client and host required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	address, e := netip.ParseAddr(u.Hostname())
	if e != nil || !address.Is4() || address.IsUnspecified() || address.IsMulticast() || u.Scheme != "http" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.ForceQuery {
		return nil, errors.New("native download: plain HTTP IPv4 export origin required")
	}
	prefix := strings.TrimPrefix(strings.TrimSuffix(u.Path, "/"), "/")
	if prefix == "" && u.Path != "" && u.Path != "/" {
		return nil, errors.New("native download: ambiguous export root")
	}
	if prefix != "" && !fs.ValidPath(prefix) {
		return nil, errors.New("native download: invalid export path prefix")
	}
	u.Path = "/" + prefix
	if prefix == "" {
		u.Path = ""
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.Timeout <= 0 || c.Timeout > 100*time.Second {
		c.Timeout = 100 * time.Second
	}
	return &ProgramSource{client: c, base: *u, host: host}, nil
}

func (s *ProgramSource) read(ctx context.Context, name string, limit int) (data []byte, err error) {
	if ctx == nil || !fs.ValidPath(name) || name == "." || limit < 1 {
		return nil, ErrProgramSession
	}
	u := s.base
	u.Path += "/" + name
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if s.host != "" {
		r.Host = s.host
	}
	r.Header.Set("Accept-Encoding", "identity")
	response, err := s.client.Do(r)
	if err != nil {
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, err
	}
	if response.Body == nil {
		return nil, errors.New("native download: missing response body")
	}
	defer func() {
		if e := response.Body.Close(); e != nil {
			data = nil
			err = errors.Join(err, e)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("native download: %s returned HTTP %d", name, response.StatusCode)
	}
	encoding := response.Header.Values("Content-Encoding")
	if response.Uncompressed || len(encoding) > 1 || len(encoding) == 1 && encoding[0] != "identity" {
		return nil, errors.New("native download: transformed response refused")
	}
	if response.ContentLength > int64(limit) || response.ContentLength < -1 {
		return nil, errors.New("native download: response length exceeds its bound")
	}
	capacity := min(limit+1, 32768)
	if response.ContentLength >= 0 {
		capacity = int(response.ContentLength) + 1
	}
	data = make([]byte, 0, capacity)
	buffer := make([]byte, 32768)
	stalled := 0
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		chunk := buffer[:min(len(buffer), limit+1-len(data))]
		n, e := response.Body.Read(chunk)
		if n < 0 || n > len(chunk) {
			return nil, errors.New("native download: invalid response read count")
		}
		data = append(data, chunk[:n]...)
		if len(data) > limit {
			return nil, errors.New("native download: response exceeds its bound")
		}
		if e != nil && !errors.Is(e, io.EOF) {
			return nil, e
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if n == 0 {
			stalled++
			if stalled >= 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			stalled = 0
		}
		runtime.Gosched()
	}
	if response.ContentLength >= 0 && int64(len(data)) != response.ContentLength {
		return nil, errors.New("native download: response length changed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// Fetch validates and privately retains content using the normal export files.
// It performs no platform admission, SD or firmware IO. The returned selection
// is unbound. Download additionally applies a backend's compiled admission.
func (s *ProgramSource) Fetch(ctx context.Context, id string) (entry ProgramEntry, content *packagestore.InstallPlan, err error) {
	if s == nil || ctx == nil {
		return ProgramEntry{}, nil, ErrProgramSession
	}
	work, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	controls := map[string][]byte{}
	for _, name := range []string{"FES/CORESEL.JSN", "FES/UI.JSON"} {
		data, err := s.read(work, name, 8192)
		if err != nil {
			return ProgramEntry{}, nil, err
		}
		controls[name] = data
	}
	choices, err := decodeProgramSelections(work, controls["FES/CORESEL.JSN"])
	if err != nil {
		return ProgramEntry{}, nil, err
	}
	entries, err := decodeProgramCatalog(controls["FES/UI.JSON"], choices)
	if err != nil {
		return ProgramEntry{}, nil, err
	}
	seen := map[string]bool{}
	var selected *ProgramEntry
	for i, e := range entries {
		if !validProgramEntry(e) || seen[e.ID] {
			return ProgramEntry{}, nil, ErrProgramSession
		}
		seen[e.ID] = true
		if e.ID == id {
			selected = &entries[i]
		}
	}
	if selected == nil {
		return ProgramEntry{}, nil, errors.New("native download: catalogue ID not found")
	}
	r := selected.Request
	limits := map[string]int{}
	var names []string
	add := func(name string, limit int) {
		if old, ok := limits[name]; ok {
			limits[name] = min(old, limit)
			return
		}
		limits[name] = limit
		names = append(names, name)
	}
	add(r.Path, packagestore.MaxInstallBytes)
	add(r.MetadataPath, 16<<10)
	for _, source := range r.Sources {
		add(source.Path, 256<<10)
	}
	if r.Tape != (packagestore.TapeRequest{}) {
		add(r.Tape.Path, packagestore.MaxRetainedTapeBytes)
	}
	files := map[string][]byte{}
	remaining := packagestore.MaxInstallBytes
	for _, name := range names {
		data, err := s.read(work, name, min(limits[name], remaining))
		if err != nil {
			return ProgramEntry{}, nil, err
		}
		files[name] = data
		remaining -= len(data)
	}
	content, err = packagestore.PrepareInstall(work, r, files)
	if err != nil {
		return ProgramEntry{}, nil, err
	}
	entry = *selected
	entry.Request = content.Request()
	return entry, content, nil
}

// Download joins pure content delivery to this backend's compiled admission.
// It does not install, switch devices, or confer network DMA containment.
func (s *ProgramSource) Download(ctx context.Context, backend *ProgramBackend, id string) (*ProgramInstall, error) {
	if ctx == nil || backend == nil {
		return nil, ErrProgramSession
	}
	work, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	entry, content, err := s.Fetch(work, id)
	if err != nil {
		return nil, err
	}
	return backend.prepareInstallContent(work, entry.ID, entry.Title, content)
}
