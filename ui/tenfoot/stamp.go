package tenfoot

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	headerClientTsUTC  = "X-FogCast-Client-Ts-Utc"
	headerClientMonoMS = "X-FogCast-Client-Mono-Ms"
	headerFlightID     = "X-FogCast-Flight-Id"
)

var processStart = time.Now()

// ClientStamp is the sofa wall + monotonic clock captured at a user action.
type ClientStamp struct {
	TsUTC    string
	MonoMS   int64
	FlightID string
}

// UIEvent is one debug ingest row posted to POST /api/v1/debug/ui-events.
type UIEvent struct {
	TSUTC    string            `json:"ts_utc"`
	MonoMS   int64             `json:"mono_ms"`
	FlightID string            `json:"flight_id,omitempty"`
	Layer    string            `json:"layer"`
	Kind     string            `json:"kind"`
	Severity string            `json:"severity,omitempty"`
	Detail   map[string]string `json:"detail,omitempty"`
}

// ClientStampNow records UTC wall time and process-monotonic milliseconds.
func ClientStampNow() ClientStamp {
	return ClientStamp{
		TsUTC:  time.Now().UTC().Format(time.RFC3339Nano),
		MonoMS: time.Since(processStart).Milliseconds(),
	}
}

func (s ClientStamp) withFlight(id string) ClientStamp {
	s.FlightID = strings.TrimSpace(id)
	return s
}

func applyClientStamp(req *http.Request, stamp ClientStamp) {
	if req == nil {
		return
	}
	if stamp.TsUTC == "" && stamp.MonoMS == 0 && stamp.FlightID == "" {
		stamp = ClientStampNow()
	}
	if stamp.TsUTC != "" {
		req.Header.Set(headerClientTsUTC, stamp.TsUTC)
	}
	req.Header.Set(headerClientMonoMS, strconv.FormatInt(stamp.MonoMS, 10))
	if id := strings.TrimSpace(stamp.FlightID); id != "" {
		req.Header.Set(headerFlightID, id)
	}
}

func validClientFlightID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if r >= '0' && r <= '9' {
				continue
			}
			if r >= 'a' && r <= 'f' {
				continue
			}
			if r >= 'A' && r <= 'F' {
				return false
			}
			return false
		}
	}
	return value[14] == '4'
}

type flightMemory struct {
	mu sync.Mutex
	id string
}

func (m *flightMemory) remember(id string) {
	if m == nil || !validClientFlightID(id) {
		return
	}
	m.mu.Lock()
	m.id = id
	m.mu.Unlock()
}

func (m *flightMemory) current() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id
}

func (m *flightMemory) stamp() ClientStamp {
	return ClientStampNow().withFlight(m.current())
}
