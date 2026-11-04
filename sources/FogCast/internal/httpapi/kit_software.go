package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/protocol"
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

func kitSoftwareHealth(parent context.Context, socket string) *protocol.Identity {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	result := &protocol.Identity{ActiveBundle: os.Getenv("FES_SOFTWARE_BUNDLE_ID"), BaseIdentity: os.Getenv("FES_SOFTWARE_BASE_IDENTITY")}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://software/v1/kit/software", nil)
	response, err := softwareSupervisorClient(socket).Do(request)
	if err != nil {
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result
	}
	identity, ok := decodeSoftwareHealth(response.Body)
	if !ok {
		return result
	}
	return &identity
}

// decodeSoftwareHealth checks the complete status envelope before publishing
// the selected bundle. The supervisor validates its repository state before
// serving it; health does not build or independently validate its receipt map.
// Only compact identity fields leave the agent.
func decodeSoftwareHealth(body io.Reader) (protocol.Identity, bool) {
	var identity protocol.Identity
	// The JSON decoder buffers one value at a time. The bounded UTF-8 reader
	// catches invalid byte sequences even in fields this projection ignores.
	input := &statusUTF8Reader{reader: io.LimitReader(body, maxSoftwareControlResponse+1)}
	decoder := json.NewDecoder(input)
	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return identity, false
	}
	known := []string{"version", "generation", "selected", "previous", "pending", "unreleased", "receipts", "active_bundle", "base_identity", "service_fault", "storage_error", "held_back", "held_back_reason"}
	seen := make(map[string]bool, len(known))
	version := 0
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return protocol.Identity{}, false
		}
		field := ""
		for _, candidate := range known {
			if strings.EqualFold(name, candidate) {
				field = candidate
				break
			}
		}
		if field != "" {
			// Reject aliases as well as duplicates: encoding/json treats Unicode
			// case-fold equivalents as the same struct member.
			if field != name || seen[field] {
				return protocol.Identity{}, false
			}
			seen[field] = true
		}
		if field == "" {
			// Unknown additive fields remain forward compatible. The decoder
			// checks syntax but need not allocate a representation of the value.
			if err := decoder.Decode(new(discardStatusValue)); err != nil {
				return protocol.Identity{}, false
			}
			continue
		}
		switch field {
		case "version":
			var value *int
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
			version = *value
		case "generation":
			var value *uint64
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
			identity.Generation = *value
		case "selected":
			var value *string
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
			identity.Selected = *value
		case "active_bundle":
			var value *string
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
			identity.ActiveBundle = *value
		case "base_identity":
			var value *string
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
			identity.BaseIdentity = *value
		case "storage_error":
			var storageError *string
			if decoder.Decode(&storageError) != nil || storageError == nil || *storageError != "" {
				return protocol.Identity{}, false
			}
		case "receipts", "pending", "unreleased":
			// The JSON decoder checks the entire value's syntax without
			// allocating receipt structures; the supervisor checks invariants.
			if decoder.Decode(new(statusObject)) != nil {
				return protocol.Identity{}, false
			}
		case "previous", "service_fault", "held_back", "held_back_reason":
			var value *string
			if decoder.Decode(&value) != nil || value == nil {
				return protocol.Identity{}, false
			}
		}
	}
	if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
		return protocol.Identity{}, false
	}
	if decoder.Decode(new(discardStatusValue)) != io.EOF || input.invalid || input.total > maxSoftwareControlResponse {
		return protocol.Identity{}, false
	}
	for _, field := range []string{"version", "generation", "selected", "receipts", "active_bundle", "base_identity"} {
		if !seen[field] {
			return protocol.Identity{}, false
		}
	}
	if version != 1 || !software.ValidID(identity.Selected) {
		return protocol.Identity{}, false
	}
	return identity, true
}

// Decoder checks the entire JSON value before calling UnmarshalJSON. These
// sinks avoid allocating or parsing a receipt map a second time.
type discardStatusValue struct{}

func (*discardStatusValue) UnmarshalJSON([]byte) error { return nil }

type statusObject struct{}

func (*statusObject) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || data[0] != '{' {
		return errors.New("status member is not an object")
	}
	return nil
}

// encoding/json replaces invalid UTF-8. Keep at most three bytes from a
// partial trailing rune so that malformed bytes anywhere in the stream fail
// closed, including ignored fields and read-boundary splits.
type statusUTF8Reader struct {
	reader  io.Reader
	pending []byte
	total   int
	invalid bool
}

func (r *statusUTF8Reader) Read(dst []byte) (int, error) {
	n, err := r.reader.Read(dst)
	r.total += n
	data := dst[:n]
	if len(r.pending) != 0 {
		for len(data) != 0 && !utf8.FullRune(r.pending) {
			r.pending = append(r.pending, data[0])
			data = data[1:]
		}
		if utf8.FullRune(r.pending) {
			if ch, size := utf8.DecodeRune(r.pending); ch == utf8.RuneError && size == 1 {
				r.invalid = true
			}
			r.pending = nil
		}
	}
	if len(data) != 0 {
		last := len(data) - 1
		for last > 0 && !utf8.RuneStart(data[last]) {
			last--
		}
		if !utf8.FullRune(data[last:]) {
			r.pending = append(r.pending[:0], data[last:]...)
			data = data[:last]
		}
		if !utf8.Valid(data) {
			r.invalid = true
		}
	}
	if err == io.EOF && len(r.pending) != 0 {
		r.invalid = true
	}
	return n, err
}
