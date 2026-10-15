package kitsoftware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type MaintenanceStatus struct {
	State       string `json:"state"`
	OperationID string `json:"operation_id"`
	Generation  string `json:"generation"`
	Owner       string `json:"owner"`
	Purpose     string `json:"purpose"`
	BootID      string `json:"boot_id"`
	Reason      string `json:"reason"`
}
type maintenanceGrant struct {
	Status MaintenanceStatus `json:"status"`
	Token  string            `json:"token"`
}
type AdmissionStatus struct {
	Maintenance   MaintenanceStatus `json:"maintenance"`
	RawIdleReady  bool              `json:"raw_idle_ready"`
	UpdateBlocked bool              `json:"update_blocked"`
	BundleID      string            `json:"bundle_id"`
}
type AdmissionRequest struct {
	RequestID  string `json:"request_id"`
	Owner      string `json:"owner"`
	Purpose    string `json:"purpose"`
	Credential string `json:"credential"`
}
type UnixAdmission struct {
	client  *http.Client
	key     []byte
	keyPath string
	bootID  string
	mu      sync.Mutex
	grants  map[string]maintenanceGrant
}

func NewUnixAdmission(socket, keyPath string) (*UnixAdmission, error) {
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.Write(key)
		err = errors.Join(e, f.Sync(), f.Close())
		if err != nil {
			return nil, err
		}
		d, e := os.Open(filepath.Dir(keyPath))
		if e != nil {
			return nil, e
		}
		err = errors.Join(d.Sync(), d.Close())
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	info, err := os.Lstat(keyPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || len(key) != 32 {
		return nil, errors.New("invalid private software admission key")
	}
	bootBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, err
	}
	return &UnixAdmission{key: key, keyPath: keyPath, bootID: strings.TrimSpace(string(bootBytes)), grants: map[string]maintenanceGrant{}, client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}}, nil
}
func (a *UnixAdmission) Claim(r Request) AdmissionRequest {
	h := hmac.New(sha256.New, a.key)
	h.Write([]byte(r.ID))
	return AdmissionRequest{RequestID: r.ID, Owner: r.Requester, Purpose: "kit-software-activation", Credential: hex.EncodeToString(h.Sum(nil))}
}
func (a *UnixAdmission) call(ctx context.Context, path string, in, out any) error {
	var body io.Reader
	method := http.MethodGet
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		status := &admissionResponseError{Path: path, Status: resp.StatusCode, Body: string(b)}
		if resp.StatusCode == 409 || resp.StatusCode == 423 {
			return &busyAdmissionError{status: status}
		}
		return status
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
	}
	return nil
}

// admissionResponseError is an HTTP status from the agent. The call was rejected
// and the agent's state was left unchanged. A transport failure is not this
// error: the agent may already have applied the call.
type admissionResponseError struct {
	Path   string
	Status int
	Body   string
}

func (e *admissionResponseError) Error() string {
	return fmt.Sprintf("admission %s: HTTP %d: %s", e.Path, e.Status, e.Body)
}

type busyAdmissionError struct {
	status *admissionResponseError
}

func (e *busyAdmissionError) Error() string {
	return ErrBusy.Error() + ": " + e.status.Body
}

func (e *busyAdmissionError) Unwrap() []error {
	return []error{ErrBusy, e.status}
}

// AdmissionUncertain reports that the agent may have applied the call.
// An HTTP status is a definite rejection. A timeout or lost response is not.
func AdmissionUncertain(err error) bool {
	if err == nil {
		return false
	}
	var status *admissionResponseError
	return !errors.As(err, &status)
}

func (a *UnixAdmission) Queue(ctx context.Context, r Request) error {
	return a.call(ctx, "/queue", a.Claim(r), nil)
}

// QueueWhileHeld installs next on this admission while held still owns the
// active maintenance reservation. The boot grant is read from held and is not
// released here.
func (a *UnixAdmission) QueueWhileHeld(ctx context.Context, held *UnixAdmission, heldRequest, next Request) error {
	if held == nil {
		return errors.New("missing boot admission")
	}
	grant, err := held.loadGrant(heldRequest.ID)
	if err != nil {
		return err
	}
	if grant.Token == "" || grant.Status.OperationID != heldRequest.ID {
		return errors.New("missing boot admission credential")
	}
	claim := a.Claim(next)
	return a.call(ctx, "/queue", struct {
		RequestID  string `json:"request_id"`
		Owner      string `json:"owner"`
		Purpose    string `json:"purpose"`
		Credential string `json:"credential"`
		HeldToken  string `json:"held_token"`
	}{
		RequestID:  claim.RequestID,
		Owner:      claim.Owner,
		Purpose:    claim.Purpose,
		Credential: claim.Credential,
		HeldToken:  grant.Token,
	}, nil)
}
func (a *UnixAdmission) FenceExecution(ctx context.Context) error {
	return a.call(ctx, "/fence", struct{}{}, nil)
}
func (a *UnixAdmission) ClearExecutionFence(ctx context.Context) error {
	return a.call(ctx, "/clear-fence", struct{}{}, nil)
}
func (a *UnixAdmission) Cancel(ctx context.Context, id string) error {
	return a.call(ctx, "/cancel", map[string]string{"request_id": id}, nil)
}
func (a *UnixAdmission) Acquire(ctx context.Context, r Request) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		var grant maintenanceGrant
		if err := a.call(ctx, "/acquire", a.Claim(r), &grant); err != nil {
			return err
		}
		if grant.Token == "" || grant.Status.OperationID != r.ID || grant.Status.Generation == "" || grant.Status.BootID != a.bootID {
			return errors.New("invalid maintenance grant identity")
		}
		if err := a.saveGrant(r.ID, grant); err != nil {
			return err
		}
		if grant.Status.State == "ready" {
			return nil
		}
		if grant.Status.State == "blocked" {
			if err := a.call(ctx, "/resume", completion(grant), nil); err != nil {
				return err
			}
		} else if grant.Status.State != "preparing" {
			return fmt.Errorf("unexpected maintenance state %s", grant.Status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func completion(g maintenanceGrant) map[string]string {
	return map[string]string{"credential": g.Token, "operation_id": g.Status.OperationID, "expected_generation": g.Status.Generation}
}
func (a *UnixAdmission) saveGrant(id string, g maintenanceGrant) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	path := a.keyPath + ".grant-" + id
	f, err := os.CreateTemp(filepath.Dir(path), ".grant-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = errors.Join(dir.Sync(), dir.Close())
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.grants[id] = g
	a.mu.Unlock()
	return nil
}
func (a *UnixAdmission) loadGrant(id string) (maintenanceGrant, error) {
	var g maintenanceGrant
	b, err := os.ReadFile(a.keyPath + ".grant-" + id)
	if err != nil {
		return g, err
	}
	err = json.Unmarshal(b, &g)
	return g, err
}
func (a *UnixAdmission) Release(ctx context.Context, r Request) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	g, err := a.loadGrant(r.ID)
	if err != nil || g.Status.BootID != a.bootID {
		if err := a.Acquire(ctx, r); err != nil {
			return err
		}
		g, err = a.loadGrant(r.ID)
		if err != nil {
			return err
		}
	}
	for {
		var status MaintenanceStatus
		if err := a.call(ctx, "/end", completion(g), &status); err != nil {
			return err
		}
		if status.State == "none" || status.State == "completed" {
			return nil
		}
		if status.State != "finishing" {
			return fmt.Errorf("unexpected maintenance completion %s", status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (a *UnixAdmission) Status(ctx context.Context) (AdmissionStatus, error) {
	var s AdmissionStatus
	err := a.call(ctx, "/status", nil, &s)
	return s, err
}
