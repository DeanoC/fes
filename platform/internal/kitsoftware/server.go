package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
)

// Server is reachable only through a mode-0600 Unix socket. The replaceable agent
// authenticates remote callers before proxying these routes. The same mutex
// serializes queue/cancel with a worker tick so cancellation cannot race acquire.
type Server struct {
	snapshot      atomic.Pointer[Server]
	Mu            sync.Mutex
	Repository    Repository
	Store         *software.Store
	Factory       *software.Release
	Admission     *UnixAdmission
	RestartLedger *ServiceRestartLedger
	Controller    serverController
	BaseIdentity  string
	StorageError  string
	// ExecutionFenced is set when the agent's execution fence may be closed and
	// the durable queue is not yet known to match. That includes a lost /fence
	// reply and a restore that failed after cancellation. Claims stay closed
	// until ReconcileExecutionFence makes the durable and volatile records match.
	ExecutionFenced bool
	Gate            func() error
	// HeldBack and HeldBackReason report the selection this image cannot run.
	// The supervisor copies them from its Engine before publishing status.
	HeldBack, HeldBackReason string
}

type serverController interface {
	Controller
	ServiceFault() string
	HostFault() string
}

func (s *Server) PublishStatus() {
	snapshot := &Server{Repository: s.Repository, Controller: s.Controller, BaseIdentity: s.BaseIdentity, StorageError: s.StorageError, HeldBack: s.HeldBack, HeldBackReason: s.HeldBackReason}
	s.snapshot.Store(snapshot)
}

// ReconcileExecutionFence restores a queued request's claim priority before it
// opens the fence. A lost /fence reply and a failed restore both come through
// here. The fence stays closed when that queue cannot be restored.
func (s *Server) ReconcileExecutionFence(ctx context.Context) error {
	if !s.ExecutionFenced || s.Admission == nil || s.Repository == nil {
		return nil
	}
	state, err := s.Repository.Read()
	if err != nil {
		return err
	}
	if state.Pending != nil && state.Pending.Phase == "queued" {
		if err := s.Admission.Queue(ctx, state.Pending.Request); err != nil {
			return err
		}
	}
	if err := s.Admission.ClearExecutionFence(ctx); err != nil {
		return err
	}
	s.ExecutionFenced = false
	s.StorageError = ""
	return nil
}

// restoreQueuedPriority puts the durable request ahead of new claims, then
// opens the fence. A failed restore or a failed open leaves the fence closed
// for ReconcileExecutionFence.
func (s *Server) restoreQueuedPriority(ctx context.Context, request Request) {
	if err := s.Admission.Queue(ctx, request); err != nil {
		s.ExecutionFenced = true
		s.StorageError = err.Error()
		return
	}
	s.finishFence(ctx)
}

func (s *Server) finishFence(ctx context.Context) {
	if err := s.Admission.ClearExecutionFence(ctx); err != nil {
		s.ExecutionFenced = true
		s.StorageError = err.Error()
		return
	}
	s.ExecutionFenced = false
	s.StorageError = ""
}

func (s *Server) status() (softwarecontrol.Status, error) {
	out := softwarecontrol.Status{BaseIdentity: s.BaseIdentity, StorageError: s.StorageError, HeldBack: s.HeldBack, HeldBackReason: s.HeldBackReason}
	if s.Controller != nil {
		out.ActiveBundle = s.Controller.Active()
		out.ServiceFault = s.Controller.ServiceFault()
		if out.ServiceFault == "" {
			out.ServiceFault = s.Controller.HostFault()
		}
	}
	if s.Repository != nil {
		state, err := s.Repository.Read()
		if err != nil {
			return out, err
		}
		out.State = state
		if out.HeldBack != "" && state.Selected != out.HeldBack {
			// Operator recovery can replace an incompatible selected bundle while
			// this supervisor remains alive. Do not report that old selection as
			// held back after the journal has moved to a compatible release.
			out.HeldBack, out.HeldBackReason = "", ""
		}
	}
	return out, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, err error) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/kit/software" {
		source := s.snapshot.Load()
		if source == nil {
			source = s
		}
		out, err := source.status()
		if err != nil {
			fail(503, err)
			return
		}
		json.NewEncoder(w).Encode(out)
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.Repository == nil || s.Store == nil {
		fail(503, errors.New("persistent software storage unavailable: "+s.StorageError))
		return
	}
	if r.Method == http.MethodPost && r.URL.Path != "/v1/kit/software/cancel" && s.Controller != nil {
		if fault := s.Controller.ServiceFault(); fault != "" {
			fail(503, errors.New(fault))
			return
		}
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/kit/software/releases":
		releases, err := s.Store.List(r.Context())
		if err != nil {
			fail(503, err)
			return
		}
		if s.Factory != nil {
			found := false
			for _, release := range releases {
				if release.ID == s.Factory.ID {
					found = true
					break
				}
			}
			if !found {
				releases = append(releases, *s.Factory)
			}
		}
		json.NewEncoder(w).Encode(releases)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/kit/software/releases":
		if s.Gate != nil {
			if err := s.Gate(); err != nil {
				fail(503, err)
				return
			}
		}

		release, created, err := s.Store.Install(r.Context(), http.MaxBytesReader(w, r.Body, software.MaxBundleSize))
		if err != nil {
			fail(400, err)
			return
		}
		if created {
			w.WriteHeader(201)
		}
		json.NewEncoder(w).Encode(release)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/kit/software/activate":
		var req Request
		if err := decodeRequest(r, &req); err != nil {
			fail(400, err)
			return
		}
		if err := s.Controller.Verify(r.Context(), req.Candidate); err != nil {
			fail(400, err)
			return
		}
		if err := Queue(s.Repository, req); err != nil {
			fail(409, err)
			return
		}
		// The durable request survives a lost queue notification; the worker retries.
		// A completed idempotent request must not recreate a lease reservation.
		state, err := s.Repository.Read()
		if err != nil {
			fail(503, err)
			return
		}
		if state.Pending != nil && state.Pending.Request == req {
			_ = s.Admission.Queue(r.Context(), req)
		}
		out, err := s.status()
		if err != nil {
			fail(503, err)
			return
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/kit/software/cancel":
		var req softwarecontrol.CancelRequest
		if err := decodeRequest(r, &req); err != nil {
			fail(400, err)
			return
		}
		// Close an execution fence, then drop the volatile queue, all before this
		// handler releases the operation mutex. A crash before the durable
		// cancellation leaves the original request; the next start requeues it.
		// The fence is uncertain until /fence returns: a lost reply keeps it
		// closed for reconciliation, and an HTTP rejection means it stayed open.
		// An uncertain /cancel is the same as a failed durable write: restore
		// the queue before opening the fence. A failed restore leaves the fence
		// closed until both records match.
		state, err := s.Repository.Read()
		if err != nil {
			fail(503, err)
			return
		}
		var queued *Operation
		if state.Pending != nil && state.Pending.ID == req.ID && state.Pending.Generation == req.Generation && state.Pending.Phase == "queued" {
			copy := *state.Pending
			queued = &copy
		}
		if queued == nil && state.Pending != nil && state.Pending.ID == req.ID && state.Pending.Generation == req.Generation && state.Pending.Phase == "blocked" {
			copy := *state.Pending
			if err := s.blockedCancelSelection(r.Context(), copy); err != nil {
				fail(409, err)
				return
			}
			if err := cancelBlocked(s.Repository, req.ID, req.Generation, softwarecontrol.ReasonBlockedCancel); err != nil {
				fail(409, err)
				return
			}
			out, err := s.status()
			if err != nil {
				fail(503, err)
				return
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		if queued != nil {
			s.ExecutionFenced = true
			if err := s.Admission.FenceExecution(r.Context()); err != nil {
				if !AdmissionUncertain(err) {
					s.ExecutionFenced = false
					s.StorageError = ""
				} else {
					s.StorageError = err.Error()
				}
				fail(503, err)
				return
			}
			if err := s.Admission.Cancel(r.Context(), req.ID); err != nil {
				if AdmissionUncertain(err) {
					s.restoreQueuedPriority(r.Context(), queued.Request)
				} else {
					s.finishFence(r.Context())
				}
				fail(503, err)
				return
			}
		}
		if err := Cancel(s.Repository, req.ID, req.Generation); err != nil {
			if queued != nil {
				s.restoreQueuedPriority(r.Context(), queued.Request)
			}
			fail(409, err)
			return
		}
		if queued != nil {
			s.finishFence(r.Context())
			if s.ExecutionFenced {
				fail(503, errors.New(s.StorageError))
				return
			}
		}
		out, err := s.status()
		if err != nil {
			fail(503, err)
			return
		}
		json.NewEncoder(w).Encode(out)
	default:
		fail(404, errors.New("unknown software route"))
	}
}

// blockedCancelSelection permits cancellation of exhausted recovery only when
// the exact held maintenance reservation and a healthy compatible factory
// group are both proved. Activation verification remains unchanged and strict.
func (s *Server) blockedCancelSelection(ctx context.Context, op Operation) error {
	if s.Factory == nil || s.Controller == nil || s.Admission == nil {
		return errors.New("blocked software activation has no compatible recovery controller")
	}
	if err := s.Controller.Verify(ctx, op.Previous); !errors.Is(err, software.ErrIncompatible) {
		if err == nil {
			return errors.New("blocked software cancellation requires a held-back selected bundle")
		}
		return err
	}
	factory := s.Factory.ID
	if err := s.Controller.Verify(ctx, factory); err != nil {
		return fmt.Errorf("factory software is not compatible with this base: %w", err)
	}
	if s.Controller.Active() != factory {
		return errors.New("compatible factory software is not the active group")
	}
	grant, err := s.Admission.loadGrant(op.ID)
	if err != nil {
		return fmt.Errorf("failed activation grant is unavailable: %w", err)
	}
	if grant.Token == "" || grant.Status.OperationID != op.ID || grant.Status.Owner != op.Requester || grant.Status.Purpose != "kit-software-activation" || !software.ValidID(grant.Status.Generation) || grant.Status.BootID == "" {
		return errors.New("failed activation grant identity is invalid")
	}
	status, err := s.Admission.Status(ctx)
	if err != nil {
		return err
	}
	maintenance := status.Maintenance
	if maintenance.OperationID != op.ID || maintenance.Generation != grant.Status.Generation || maintenance.BootID != grant.Status.BootID || maintenance.Owner != op.Requester || maintenance.Purpose != "kit-software-activation" || (maintenance.State != "blocked" && maintenance.State != "ready") {
		return errors.New("failed activation no longer owns this boot's blocked maintenance reservation")
	}
	if status.BundleID != factory || !status.RawIdleReady || status.UpdateBlocked {
		return errors.New("compatible factory software is not idle and ready for recovery")
	}
	if err := s.Controller.Healthy(ctx, factory); err != nil {
		return fmt.Errorf("compatible factory software is not healthy: %w", err)
	}
	return nil
}

func decodeRequest(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing request data")
	}
	return nil
}
