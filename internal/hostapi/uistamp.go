package hostapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	headerClientTsUTC  = "X-FogCast-Client-Ts-Utc"
	headerClientMonoMS = "X-FogCast-Client-Mono-Ms"
	headerFlightID     = "X-FogCast-Flight-Id"

	uiEventRingCapacity = 4096
	maxUIEventsPerPost  = 8
	maxUIDetailKeys     = 16
	maxUIDetailChars    = 256
)

var allowedUIKinds = map[string]struct{}{
	"ui.launch": {},
	"ui.stop":   {},
	"ui.focus":  {},
	"ui.nav":    {},
}

var droppedUIDetailKeys = map[string]struct{}{
	"token":         {},
	"agent":         {},
	"authorization": {},
	"password":      {},
	"secret":        {},
	"bearer":        {},
	"cookie":        {},
}

// clientStamp is an optional sofa/tenfoot clock captured at the user action.
// Invalid values are ignored so a debug field cannot block launch or stop.
type clientStamp struct {
	TsUTC    string
	MonoMS   int64
	MonoSet  bool
	FlightID string
}

func (c clientStamp) present() bool {
	return c.TsUTC != "" || c.MonoSet
}

func parseClientStamp(r *http.Request, bodyTs string, bodyMono *int64, bodyFlight string) clientStamp {
	stamp := clientStamp{}
	ts := strings.TrimSpace(bodyTs)
	if ts == "" && r != nil {
		ts = strings.TrimSpace(r.Header.Get(headerClientTsUTC))
	}
	if parsed, ok := parseClientUTC(ts); ok {
		stamp.TsUTC = parsed
	}
	switch {
	case bodyMono != nil && *bodyMono >= 0:
		stamp.MonoMS = *bodyMono
		stamp.MonoSet = true
	case r != nil:
		raw := strings.TrimSpace(r.Header.Get(headerClientMonoMS))
		if raw != "" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
				stamp.MonoMS = n
				stamp.MonoSet = true
			}
		}
	}
	flight := strings.TrimSpace(bodyFlight)
	if flight == "" && r != nil {
		flight = strings.TrimSpace(r.Header.Get(headerFlightID))
	}
	if validHostFlightID(flight) {
		stamp.FlightID = flight
	}
	return stamp
}

func parseClientUTC(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), true
	}
	return "", false
}

func validHostFlightID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4 && parsed.String() == value
}

type stampBody struct {
	ClientTsUTC  string `json:"client_ts_utc"`
	ClientMonoMS *int64 `json:"client_mono_ms"`
	FlightID     string `json:"flight_id"`
}

func decodeOptionalStopStamp(w http.ResponseWriter, r *http.Request) (clientStamp, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "stop request body must be empty or one JSON object")
		return clientStamp{}, err
	}
	body = bytes.TrimSpace(body)
	var parsed stampBody
	if len(body) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&parsed); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "stop request body must be empty or one JSON object")
			return clientStamp{}, err
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "stop request body must contain exactly one JSON object")
			return clientStamp{}, errors.New("trailing JSON")
		}
	}
	return parseClientStamp(r, parsed.ClientTsUTC, parsed.ClientMonoMS, parsed.FlightID), nil
}

// uiEvent is one sofa/tenfoot action in the debug ingest ring. It is not a
// session mutation and does not claim the kit.
type uiEvent struct {
	Sequence uint64         `json:"sequence"`
	TSUTC    string         `json:"ts_utc"`
	MonoMS   int64          `json:"mono_ms"`
	FlightID string         `json:"flight_id,omitempty"`
	Layer    string         `json:"layer"`
	Kind     string         `json:"kind"`
	Severity string         `json:"severity"`
	Detail   map[string]any `json:"detail,omitempty"`
}

type uiEventRing struct {
	mu       sync.Mutex
	capacity int
	sequence uint64
	events   []uiEvent
}

func newUIEventRing(capacity int) *uiEventRing {
	if capacity <= 0 {
		capacity = uiEventRingCapacity
	}
	return &uiEventRing{capacity: capacity}
}

func (r *uiEventRing) append(events []uiEvent) []uiEvent {
	if r == nil || len(events) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uiEvent, 0, len(events))
	for _, event := range events {
		r.sequence++
		event.Sequence = r.sequence
		if event.Detail == nil {
			event.Detail = map[string]any{}
		}
		r.events = append(r.events, event)
		out = append(out, event)
	}
	if len(r.events) > r.capacity {
		keep := r.events[len(r.events)-r.capacity:]
		r.events = append([]uiEvent(nil), keep...)
	}
	return out
}

func (r *uiEventRing) after(after uint64) []uiEvent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uiEvent, 0)
	for _, event := range r.events {
		if event.Sequence > after {
			cloned := event
			cloned.Detail = cloneUIDetail(event.Detail)
			out = append(out, cloned)
		}
	}
	return out
}

func registerDebugUIRoutes(mux *http.ServeMux, ring *uiEventRing) {
	mux.HandleFunc("GET /api/v1/debug/ui-events", func(w http.ResponseWriter, r *http.Request) {
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "after must be a non-negative sequence")
				return
			}
			after = n
		}
		writeJSON(w, http.StatusOK, struct {
			Events []uiEvent `json:"events"`
		}{Events: ring.after(after)})
	})
	mux.HandleFunc("POST /api/v1/debug/ui-events", func(w http.ResponseWriter, r *http.Request) {
		events, err := decodeUIEvents(w, r)
		if err != nil {
			return
		}
		stored := ring.append(events)
		writeJSON(w, http.StatusOK, struct {
			Events []uiEvent `json:"events"`
			Count  int       `json:"count"`
		}{Events: stored, Count: len(stored)})
	})
}

func decodeUIEvents(w http.ResponseWriter, r *http.Request) ([]uiEvent, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ui event body is invalid")
		return nil, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ui event body is invalid")
		return nil, errors.New("empty ui event body")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ui event body is invalid")
		return nil, err
	}
	rawEvents := []json.RawMessage{body}
	if blob, ok := probe["events"]; ok {
		var list []json.RawMessage
		if err := json.Unmarshal(blob, &list); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ui event list is invalid")
			return nil, err
		}
		rawEvents = list
	}
	if len(rawEvents) == 0 || len(rawEvents) > maxUIEventsPerPost {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ui event list is invalid")
		return nil, errors.New("ui event count")
	}
	out := make([]uiEvent, 0, len(rawEvents))
	for _, raw := range rawEvents {
		event, err := normalizeUIEvent(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func normalizeUIEvent(raw json.RawMessage) (uiEvent, error) {
	var incoming struct {
		TSUTC    string         `json:"ts_utc"`
		MonoMS   *int64         `json:"mono_ms"`
		FlightID string         `json:"flight_id"`
		Layer    string         `json:"layer"`
		Kind     string         `json:"kind"`
		Severity string         `json:"severity"`
		Detail   map[string]any `json:"detail"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&incoming); err != nil {
		return uiEvent{}, errors.New("ui event is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return uiEvent{}, errors.New("ui event is invalid")
	}
	kind := strings.TrimSpace(incoming.Kind)
	if _, ok := allowedUIKinds[kind]; !ok {
		return uiEvent{}, errors.New("ui event kind is invalid")
	}
	layer := strings.TrimSpace(incoming.Layer)
	if layer == "" {
		layer = "ui"
	}
	if layer != "ui" {
		return uiEvent{}, errors.New("ui event layer is invalid")
	}
	severity := strings.TrimSpace(incoming.Severity)
	if severity == "" {
		severity = "ok"
	}
	if severity != "ok" && severity != "warn" && severity != "error" {
		return uiEvent{}, errors.New("ui event severity is invalid")
	}
	ts, ok := parseClientUTC(incoming.TSUTC)
	if !ok {
		return uiEvent{}, errors.New("ui event timestamp is invalid")
	}
	mono := int64(0)
	if incoming.MonoMS != nil {
		if *incoming.MonoMS < 0 {
			return uiEvent{}, errors.New("ui event timestamp is invalid")
		}
		mono = *incoming.MonoMS
	}
	flight := strings.TrimSpace(incoming.FlightID)
	if flight != "" && !validHostFlightID(flight) {
		flight = ""
	}
	return uiEvent{
		TSUTC:    ts,
		MonoMS:   mono,
		FlightID: flight,
		Layer:    layer,
		Kind:     kind,
		Severity: severity,
		Detail:   sanitizeUIDetail(incoming.Detail),
	}, nil
}

func sanitizeUIDetail(detail map[string]any) map[string]any {
	if len(detail) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(detail))
	for key, value := range detail {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, drop := droppedUIDetailKeys[strings.ToLower(key)]; drop {
			continue
		}
		if len(out) >= maxUIDetailKeys {
			break
		}
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			out[key] = clipUIDetailString(typed)
		case float64:
			out[key] = typed
		case bool:
			out[key] = typed
		case json.Number:
			out[key] = typed.String()
		default:
			out[key] = clipUIDetailString(stringifyUIDetail(typed))
		}
	}
	return out
}

func clipUIDetailString(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if utf8.RuneCountInString(value) <= maxUIDetailChars {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxUIDetailChars])
}

func stringifyUIDetail(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func cloneUIDetail(detail map[string]any) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(detail))
	for key, value := range detail {
		out[key] = value
	}
	return out
}
