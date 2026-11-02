package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
)

const maxSoftwareControlResponse = 32 << 20

// WithKitSoftware exposes the stable supervisor's software API through the
// agent's existing bearer authentication. The fixed Unix socket is target-local;
// callers cannot select an upstream service or filesystem path.
func WithKitSoftware(socket string) Option {
	return func(options *serverOptions) { options.kitSoftwareSocket = socket }
}

func softwareSupervisorClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: true,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func registerKitSoftwareRoutes(mux *http.ServeMux, token, socket string) {
	client := softwareSupervisorClient(socket)
	for _, route := range []string{
		"GET /v1/kit/software", "GET /v1/kit/software/releases",
		"POST /v1/kit/software/releases", "POST /v1/kit/software/activate", "POST /v1/kit/software/cancel",
	} {
		mux.Handle(route, authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				writeBadRequest(w, r, "software control does not accept query parameters")
				return
			}
			limit := int64(4096)
			if r.Method == http.MethodPost {
				contentType := "application/json"
				if r.URL.Path == "/v1/kit/software/releases" {
					contentType = "application/octet-stream"
					limit = software.MaxBundleSize
				}
				if !exactContentType(r, contentType) || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > limit {
					writeBadRequest(w, r, "software control requires a bounded body with the declared content type")
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			upstream, err := http.NewRequestWithContext(r.Context(), r.Method, "http://software"+r.URL.Path, r.Body)
			if err != nil {
				softwareProxyError(w, http.StatusBadGateway, "software request could not be created")
				return
			}
			upstream.ContentLength = r.ContentLength
			upstream.Header.Set("Content-Type", r.Header.Get("Content-Type"))
			response, err := client.Do(upstream)
			if err != nil {
				softwareProxyError(w, http.StatusServiceUnavailable, "kit software supervisor is unavailable; inspect software status before retrying a mutation")
				return
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, maxSoftwareControlResponse+1))
			if err != nil || len(data) > maxSoftwareControlResponse {
				softwareProxyError(w, http.StatusBadGateway, "software supervisor response is incomplete")
				return
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				var failure struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(data, &failure) != nil || failure.Error == "" {
					failure.Error = "software supervisor rejected the operation"
				}
				softwareProxyError(w, response.StatusCode, failure.Error)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(response.StatusCode)
			_, _ = w.Write(data)
		})))
	}
}

func softwareProxyError(w http.ResponseWriter, status int, message string) {
	code := "KIT_SOFTWARE_UNAVAILABLE"
	if status == http.StatusConflict {
		code = "KIT_SOFTWARE_CONFLICT"
	} else if status >= 400 && status < 500 {
		code = "KIT_SOFTWARE_BAD_REQUEST"
	}
	writeError(w, status, code, message)
}

func kitSoftwareHealth(parent context.Context, socket string) *softwarecontrol.Status {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	result := &softwarecontrol.Status{ActiveBundle: os.Getenv("FES_SOFTWARE_BUNDLE_ID"), BaseIdentity: os.Getenv("FES_SOFTWARE_BASE_IDENTITY")}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://software/v1/kit/software", nil)
	response, err := softwareSupervisorClient(socket).Do(request)
	if err != nil {
		result.StorageError = "software supervisor unavailable"
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result.StorageError = "software supervisor unavailable"
		return result
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSoftwareControlResponse+1))
	if err != nil || len(data) > maxSoftwareControlResponse {
		result.StorageError = "software supervisor status is incomplete"
		return result
	}
	status, ok := decodeSoftwareStatus(data)
	if !ok {
		result.StorageError = "software supervisor status is incomplete"
		return result
	}
	return &status
}

// A partially decoded response must never publish a selection: host health
// intentionally omits the supervisor's diagnostic error text.
func decodeSoftwareStatus(data []byte) (softwarecontrol.Status, bool) {
	var status softwarecontrol.Status
	if len(data) > maxSoftwareControlResponse {
		return status, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return status, false
	}
	fields := make(map[string]json.RawMessage)
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return status, false
		}
		name, ok := key.(string)
		if !ok {
			return status, false
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return status, false
		}
		seen[folded] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return status, false
		}
		fields[name] = value
	}
	if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
		return status, false
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return status, false
	}
	for _, name := range []string{"version", "generation", "selected", "receipts", "active_bundle", "base_identity"} {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return status, false
		}
	}
	if json.Unmarshal(data, &status) != nil || status.Version != 1 || !software.ValidID(status.Selected) ||
		status.Receipts == nil || status.StorageError != "" {
		return softwarecontrol.Status{}, false
	}
	return status, true
}
