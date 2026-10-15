package kitsoftware

import (
	"encoding/json"
	"errors"
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
	snapshot     atomic.Pointer[Server]
	Mu           sync.Mutex
	Repository   Repository
	Store        *software.Store
	Factory      *software.Release
	Admission    *UnixAdmission
	Controller   *ProcessController
	BaseIdentity string
	StorageError string
	Gate         func() error
}

func (s *Server) PublishStatus() {
	snapshot := &Server{Repository: s.Repository, Controller: s.Controller, BaseIdentity: s.BaseIdentity, StorageError: s.StorageError}
	s.snapshot.Store(snapshot)
}

func (s *Server) status() (softwarecontrol.Status, error) {
	out := softwarecontrol.Status{BaseIdentity: s.BaseIdentity, StorageError: s.StorageError}
	if s.Controller != nil {
		out.ActiveBundle = s.Controller.Active()
		out.ServiceFault = s.Controller.ServiceFault()
	}
	if s.Repository != nil {
		state, err := s.Repository.Read()
		if err != nil {
			return out, err
		}
		out.State = state
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
		// Release the volatile queue first under the same operation mutex; a crash
		// before durable cancellation merely leaves the original queued request.
		state, err := s.Repository.Read()
		if err != nil {
			fail(503, err)
			return
		}
		if state.Pending != nil && state.Pending.ID == req.ID && state.Pending.Generation == req.Generation && state.Pending.Phase == "queued" {
			if err := s.Admission.Cancel(r.Context(), req.ID); err != nil {
				fail(503, err)
				return
			}
		}
		if err := Cancel(s.Repository, req.ID, req.Generation); err != nil {
			fail(409, err)
			return
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
