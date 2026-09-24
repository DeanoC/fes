package targetclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
)

const KitLeaseHeader = "X-FogCast-Kit-Lease"

var ErrKitLeaseLost = errors.New("kit lease unavailable; inspect target ownership before starting a new session")

type kitLeaseStatus = kitlease.Status
type kitLeaseGrant = kitlease.Grant

// KitLease is owned by one application and shared explicitly with its target
// and input clients. It never takes over another owner or retries mutations.
type KitLease struct {
	mu                        sync.Mutex
	client                    *Client
	owner, purpose, requestID string
	grant                     kitLeaseGrant
	localExpiry               time.Time
	lost, closed              bool
	cancel                    context.CancelFunc
}

func NewKitLease(base *url.URL, bearer string, client *http.Client, owner, purpose string) *KitLease {
	return &KitLease{client: NewClient(base, bearer, client), owner: owner, purpose: purpose}
}
func (l *KitLease) Authorize(r *http.Request, acquire bool) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.lost {
		return ErrKitLeaseLost
	}
	if l.grant.Token == "" {
		if !acquire {
			return ErrKitLeaseLost
		}
		if l.requestID == "" {
			var b [32]byte
			if _, err := rand.Read(b[:]); err != nil {
				return err
			}
			l.requestID = hex.EncodeToString(b[:])
		}
		var grant kitLeaseGrant
		requestStart := time.Now()
		if err := l.client.doJSON(r.Context(), "POST", "/v1/kit/claim", map[string]string{"request_id": l.requestID, "owner": l.owner, "purpose": l.purpose}, &grant); err != nil {
			return err
		}
		if !validKitGrant(grant, requestStart) {
			l.lost = true
			return ErrKitLeaseLost
		}
		l.grant = grant
		l.localExpiry = requestStart.Add(time.Duration(grant.Status.ExpiresInMS) * time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		l.cancel = cancel
		go l.renewLoop(ctx)
	}
	if !time.Now().Before(l.localExpiry) {
		l.invalidateLocked()
		return ErrKitLeaseLost
	}
	r.Header.Set(KitLeaseHeader, l.grant.Token)
	return nil
}
func validKitGrant(g kitLeaseGrant, requestStart time.Time) bool {
	// The kit may boot without an RTC. Only local monotonic time governs client
	// expiry; target wall time is display-only. Charge all round-trip time.
	return g.Token != "" && g.Status.State == "held" && g.Status.Generation != "" &&
		g.Status.ExpiresInMS > 0 && g.Status.ExpiresInMS <= int64((24*time.Hour)/time.Millisecond) &&
		time.Now().Before(requestStart.Add(time.Duration(g.Status.ExpiresInMS)*time.Millisecond))
}
func (l *KitLease) invalidateLocked() {
	l.lost = true
	l.grant = kitLeaseGrant{}
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}
func (l *KitLease) renewLoop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.renew(ctx)
		}
	}
}
func (l *KitLease) renew(parent context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Release cancels the old loop while holding this mutex. A ticker already
	// selected by that loop may only acquire the mutex after a new claim;
	// cancellation must be checked before inspecting or invalidating its grant.
	if parent.Err() != nil || l.closed || l.lost || l.grant.Token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var next kitLeaseGrant
	requestStart := time.Now()
	if err := l.request(ctx, "/v1/kit/renew", l.grant.Token, &next); err != nil || !validKitGrant(next, requestStart) || next.Token != l.grant.Token || next.Status.Generation != l.grant.Status.Generation {
		l.invalidateLocked()
		return
	}
	l.grant = next
	l.localExpiry = requestStart.Add(time.Duration(next.Status.ExpiresInMS) * time.Millisecond)
}
func (l *KitLease) request(ctx context.Context, path, token string, result any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", l.client.endpoint(path, nil).String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.client.token)
	req.Header.Set(KitLeaseHeader, token)
	resp, err := l.client.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, result)
}

// Release relinquishes this application's grant only. A failed renewal never
// reacquires ownership for cleanup. The target performs serialized cleanup.
// A failed release keeps the local grant so the owner can retry; dropping the
// token while the target still holds it would hide the mutator from this client.
func (l *KitLease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	token := l.grant.Token
	if token == "" {
		l.requestID = ""
		return nil
	}
	var status kitLeaseStatus
	err := l.request(ctx, "/v1/kit/release", token, &status)
	if err != nil {
		if !l.closed && !l.lost && l.cancel == nil {
			renewCtx, cancel := context.WithCancel(context.Background())
			l.cancel = cancel
			go l.renewLoop(renewCtx)
		}
		return err
	}
	l.grant = kitLeaseGrant{}
	l.requestID = ""
	return nil
}
func (l *KitLease) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	return l.Release(ctx)
}
func (c *Client) WithKitLease(l *KitLease) *Client { c.kitLease = l; return c }
func (c *Client) KitLease() *KitLease              { return c.kitLease }

func (c *Client) KitLeaseStatus(ctx context.Context) (kitlease.Status, error) {
	var status kitlease.Status
	err := c.doJSON(ctx, http.MethodGet, "/v1/kit/lease", nil, &status)
	return status, err
}

func (l *KitLease) Held() bool { return l.CurrentToken() != "" }

// Ownership reports the grant this session currently holds.
// generation is empty when the session does not hold a current grant.
func (l *KitLease) Ownership() (owned bool, generation string) {
	if l == nil {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.lost || l.grant.Token == "" || l.grant.Status.Generation == "" {
		return false, ""
	}
	if !time.Now().Before(l.localExpiry) {
		return false, ""
	}
	return true, l.grant.Status.Generation
}

// MeshKitLease is the session-owned kit grant Ensure consults.
// A client with no current grant is not an owned binding.
func (c *Client) MeshKitLease() (bool, string) {
	if c == nil {
		return false, ""
	}
	return c.kitLease.Ownership()
}
func (c *Client) authorizeMutation(r *http.Request) error {
	switch r.URL.Path {
	case "/v1/library/core/load", "/v1/library/core/compose", "/v1/library/core/settings", "/v1/launch", "/v2/launch", "/v1/development/rbf", "/v1/development/core", "/v1/cast/start", "/v1/update/stage", "/v1/update/rollback", "/v1/update/confirm":
		return c.kitLease.Authorize(r, true)
	case "/v1/stop", "/v1/development/reboot", "/v1/cast/stop", "/v1/update/activate":
		return c.kitLease.Authorize(r, false)
	}
	return nil
}

func (l *KitLease) CurrentToken() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.grant.Token
}
func (l *KitLease) AuthorizeExisting(r *http.Request, token string) error {
	if l == nil {
		return nil
	}
	if err := l.Authorize(r, false); err != nil {
		return err
	}
	if token == "" || r.Header.Get(KitLeaseHeader) != token {
		return ErrKitLeaseLost
	}
	return nil
}
