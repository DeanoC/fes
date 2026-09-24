package kitcontent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// errLauncherConfig is static so a bad launcher file cannot echo its token.
var errLauncherConfig = errors.New("mesh content source configuration is invalid")

const sourceProbeTimeout = 5 * time.Second

// launcherSource is the kit's content Source. It reads the host, which
// is the content node, with the provisioned launcher credential. Pull's
// body stays empty; this type only answers Advertises and Open.
type launcherSource struct {
	api      string
	token    string
	targetID string
	client   *http.Client
}

type launcherFile struct {
	API      string `json:"api"`
	Token    string `json:"token"`
	TargetID string `json:"target_id"`
}

// OpenLauncherSource reads launcher.json. A missing file is os.ErrNotExist
// so the agent can keep a nil source. Extra keys are ignored. An invalid
// file returns errLauncherConfig and does not include the token.
func OpenLauncherSource(path string) (*launcherSource, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var raw launcherFile
	decoder := json.NewDecoder(io.LimitReader(file, 8192))
	if err := decoder.Decode(&raw); err != nil {
		return nil, errLauncherConfig
	}
	endpoint, err := url.Parse(strings.TrimSpace(raw.API))
	if err != nil || endpoint.User != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errLauncherConfig
	}
	token := raw.Token
	if token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n") || !discovery.ValidID(raw.TargetID) {
		return nil, errLauncherConfig
	}
	return &launcherSource{
		api:      strings.TrimRight(strings.TrimSpace(raw.API), "/"),
		token:    token,
		targetID: raw.TargetID,
		client:   &http.Client{},
	}, nil
}

func (s *launcherSource) Advertises(id meshcontent.ContentID) bool {
	if s == nil || id.Validate() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceProbeTimeout)
	defer cancel()
	req, err := s.request(ctx, "/api/v1/mesh/content/source", id)
	if err != nil {
		return false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	var doc struct {
		Advertises bool `json:"advertises"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return false
	}
	return doc.Advertises
}

func (s *launcherSource) Open(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if s == nil || id.Validate() != nil {
		return nil, errLauncherConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := s.request(ctx, "/api/v1/mesh/content/object", id)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errLauncherConfig
	}
	if resp.StatusCode != http.StatusOK || !octetStream(resp.Header.Get("Content-Type")) {
		resp.Body.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errLauncherConfig
	}
	return resp.Body, nil
}

func (s *launcherSource) request(ctx context.Context, path string, id meshcontent.ContentID) (*http.Request, error) {
	endpoint, err := url.Parse(s.api + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("id", id.String())
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("X-FogCast-Target-ID", s.targetID)
	return req, nil
}

func octetStream(header string) bool {
	media := strings.TrimSpace(header)
	if i := strings.Index(media, ";"); i >= 0 {
		media = strings.TrimSpace(media[:i])
	}
	return strings.EqualFold(media, "application/octet-stream")
}
