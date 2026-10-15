// Package softwareagent is the replaceable agent's adapter to the base-image
// software supervisor. The supervisor alone owns selection and process lifetime.
package softwareagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	contract "github.com/DeanoC/FogCast/kitlease"
)

const Socket = "/run/fogcast/software-admission.sock"

var ErrBlocked = errors.New("kit software activation blocks mutations")

type Request struct {
	contract.ClaimRequest
	Credential string `json:"credential"`
	// HeldToken authorizes installing the next idle queue while the caller
	// still holds the active maintenance reservation.
	HeldToken string `json:"held_token,omitempty"`
}
type Completion struct {
	contract.MaintenanceEndRequest
	Credential string `json:"credential"`
}

// Fence tracks non-execution mutations too (core installs, selections, image
// updates). Execution ownership itself remains in the existing lease manager.
type Fence struct {
	mu         sync.Mutex
	blocked    bool
	operations map[*operation]struct{}
	changed    chan struct{}
}
type operation struct{ cancel context.CancelFunc }

func NewFence(blocked bool) *Fence {
	return &Fence{blocked: blocked, operations: make(map[*operation]struct{}), changed: make(chan struct{})}
}
func (f *Fence) Begin(ctx context.Context) (context.Context, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked {
		return nil, nil, ErrBlocked
	}
	ctx, cancel := context.WithCancel(ctx)
	op := &operation{cancel}
	f.operations[op] = struct{}{}
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			cancel()
			delete(f.operations, op)
			close(f.changed)
			f.changed = make(chan struct{})
		})
	}, nil
}
func (f *Fence) Drain(ctx context.Context) error {
	f.mu.Lock()
	f.blocked = true
	for op := range f.operations {
		op.cancel()
	}
	for len(f.operations) > 0 {
		changed := f.changed
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		f.mu.Lock()
	}
	f.mu.Unlock()
	return nil
}
func (f *Fence) Open() { f.mu.Lock(); f.blocked = false; f.mu.Unlock() }
func (f *Fence) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || strings.HasPrefix(r.URL.Path, "/v1/kit/software") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, done, err := f.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer done()
		stop := context.AfterFunc(ctx, func() { _ = http.NewResponseController(w).SetReadDeadline(time.Now()) })
		defer stop()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type Admission struct {
	mu            sync.Mutex
	startup       *Request
	Leases        *kitlease.Manager
	Fence         *Fence
	Ready         func(context.Context) bool
	UpdateBlocked func(context.Context) bool
	BundleID      string
}

// QueueStartup installs persisted intent before exposing launch paths. The
// startup fence can open once the lease manager holds the queue: it then gives
// maintenance priority while allowing unrelated installs during the wait.
// Cancellation and registration share mu so a cancelled intent cannot reappear.
func (a *Admission) QueueStartup(ctx context.Context, request Request) {
	a.mu.Lock()
	a.startup = &request
	a.mu.Unlock()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			a.mu.Lock()
			if a.startup == nil {
				a.mu.Unlock()
				return
			}
			status, err := a.Leases.MaintenanceStatusChecked()
			if err == nil && status.OperationID == request.RequestID {
				a.startup = nil
				a.mu.Unlock()
				return
			}
			if !a.UpdateBlocked(ctx) && a.Leases.QueueIdleMaintenance(request.ClaimRequest, request.Credential) == nil {
				a.startup = nil
				a.Fence.Open()
				a.mu.Unlock()
				return
			}
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *Admission) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		maintenance, err := a.Leases.MaintenanceStatusChecked()
		if err != nil {
			failure(w, err)
			return
		}
		write(w, struct {
			Maintenance   contract.MaintenanceStatus `json:"maintenance"`
			RawIdleReady  bool                       `json:"raw_idle_ready"`
			UpdateBlocked bool                       `json:"update_blocked"`
			BundleID      string                     `json:"bundle_id"`
		}{maintenance, a.Ready(r.Context()), a.UpdateBlocked(r.Context()), a.BundleID})
	})
	for _, action := range []string{"queue", "acquire", "cancel", "resume", "end", "fence", "clear-fence"} {
		mux.HandleFunc("POST /"+action, func(w http.ResponseWriter, r *http.Request) {
			a.mu.Lock()
			defer a.mu.Unlock()
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if action == "fence" || action == "clear-fence" {
				var body struct{}
				if d.Decode(&body) != nil || d.Decode(&struct{}{}) != io.EOF {
					http.Error(w, "invalid fence", 400)
					return
				}
				if err := a.Leases.SetExecutionFence(action == "fence"); err != nil {
					failure(w, err)
					return
				}
				write(w, struct{}{})
				return
			}
			if action == "resume" || action == "end" {
				var req Completion
				if d.Decode(&req) != nil || d.Decode(&struct{}{}) != io.EOF {
					http.Error(w, "invalid completion", 400)
					return
				}
				var result any
				var err error
				if action == "resume" {
					result, err = a.Leases.ResumeMaintenance(req.Credential, req.MaintenanceEndRequest)
				} else {
					var status contract.MaintenanceStatus
					status, err = a.Leases.EndMaintenance(req.Credential, req.MaintenanceEndRequest)
					result = status
					if err == nil && (status.State == "none" || status.State == "completed") {
						if a.startup != nil && a.startup.RequestID == req.OperationID {
							a.startup = nil
						}
						if a.startup == nil {
							a.Fence.Open()
						}
					}
				}
				if err != nil {
					failure(w, err)
					return
				}
				write(w, result)
				return
			}
			var req Request
			if d.Decode(&req) != nil || d.Decode(&struct{}{}) != io.EOF {
				http.Error(w, "invalid request", 400)
				return
			}
			if action == "cancel" {
				if a.startup != nil && a.startup.RequestID != req.RequestID {
					failure(w, kitlease.ErrLease)
					return
				}
				if err := a.Leases.CancelQueuedMaintenance(req.RequestID); err != nil {
					failure(w, err)
					return
				}
				if a.startup != nil && a.startup.RequestID == req.RequestID {
					a.startup = nil
				}
				status, err := a.Leases.MaintenanceStatusChecked()
				if err != nil {
					failure(w, err)
					return
				}
				if status.State == "none" || status.State == "completed" {
					a.Fence.Open()
				}
				write(w, struct{}{})
				return
			}
			if a.UpdateBlocked(r.Context()) {
				http.Error(w, "appliance update blocks software activation", 409)
				return
			}
			if action == "queue" {
				var err error
				if req.HeldToken != "" {
					err = a.Leases.QueueHeldSuccessor(req.ClaimRequest, req.Credential, req.HeldToken)
				} else {
					err = a.Leases.QueueIdleMaintenance(req.ClaimRequest, req.Credential)
				}
				if err != nil {
					failure(w, err)
					return
				}
				write(w, struct{}{})
				return
			}
			grant, err := a.Leases.StartQueuedMaintenance(req.ClaimRequest, req.Credential)
			if err != nil {
				failure(w, err)
				return
			}
			if grant.Status.State != "ready" {
				write(w, grant)
				return
			}
			if err := a.Fence.Drain(r.Context()); err != nil {
				failure(w, err)
				return
			}
			// An image activation already admitted before the fence could have committed
			// pending state while draining. Never let service replacement cross it.
			if a.UpdateBlocked(r.Context()) {
				http.Error(w, "appliance update blocks software activation", 409)
				return
			}
			write(w, grant)
		})
	}
	return mux
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	code := 409
	if errors.Is(err, kitlease.ErrInvalid) {
		code = 400
	}
	if errors.Is(err, kitlease.ErrBlocked) {
		code = 503
	}
	http.Error(w, err.Error(), code)
}
