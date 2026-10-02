package targetclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
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
	_, err := l.requestStatus(ctx, path, token, result)
	return err
}

// requestStatus posts a kit-lease mutation and returns the HTTP status.
// The status is zero when the request is not sent.
func (l *KitLease) requestStatus(ctx context.Context, path, token string, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", l.client.endpoint(path, nil).String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+l.client.token)
	req.Header.Set(KitLeaseHeader, token)
	resp, err := l.client.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, decodeResponse(resp, result)
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
	_, err := l.releaseLocked(ctx, false)
	return err
}

// ReleaseGrant relinquishes this application's grant the way Release does.
// A kit that answers the grant is not held — HTTP 404, HTTP 409, or
// KIT_LEASE_REQUIRED (missing, expired, or superseded) — forgets the local
// grant: the token and request id are cleared, renewal is not restarted,
// and lost is not set, so a later claim on this kit still works. notHeld is
// then true. Any other failure keeps the grant and restarts renewal when
// the lease is still open. No token held returns (false, nil) and sends
// no request.
func (l *KitLease) ReleaseGrant(ctx context.Context) (notHeld bool, err error) {
	if l == nil {
		return false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releaseLocked(ctx, true)
}

// releaseLocked posts /v1/kit/release. Caller holds l.mu. forgetNotHeld
// drops a grant the kit reports it does not have, without setting lost and
// without restarting renewal. Release passes false, so that answer stays a
// retryable failure and the local grant is kept.
func (l *KitLease) releaseLocked(ctx context.Context, forgetNotHeld bool) (bool, error) {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	token := l.grant.Token
	if token == "" {
		l.requestID = ""
		return false, nil
	}
	var status kitLeaseStatus
	code, err := l.requestStatus(ctx, "/v1/kit/release", token, &status)
	if err != nil {
		if forgetNotHeld && kitGrantNotHeld(code, err) {
			l.grant = kitLeaseGrant{}
			l.requestID = ""
			return true, nil
		}
		if !l.closed && !l.lost && l.cancel == nil {
			renewCtx, cancel := context.WithCancel(context.Background())
			l.cancel = cancel
			go l.renewLoop(renewCtx)
		}
		return false, err
	}
	l.grant = kitLeaseGrant{}
	l.requestID = ""
	return false, nil
}

// kitGrantNotHeld reports a release the kit rejected because this grant is
// already gone: unknown (404), conflict (409), or missing, expired, or
// superseded (403 KIT_LEASE_REQUIRED).
func kitGrantNotHeld(status int, err error) bool {
	if status == http.StatusNotFound || status == http.StatusConflict {
		return true
	}
	var api *protocol.APIError
	return errors.As(err, &api) && api != nil && string(api.Code) == "KIT_LEASE_REQUIRED"
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

// MeshKitLeaseAbandoned reports a grant this session held and then
// lost, closed, or let expire. A client that never claimed is not
// abandoned.
func (c *Client) MeshKitLeaseAbandoned() bool {
	if c == nil || c.kitLease == nil {
		return false
	}
	return c.kitLease.Abandoned()
}

// Abandoned reports a grant that is no longer usable. A lease that
// never claimed is not abandoned.
func (l *KitLease) Abandoned() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.lost {
		return true
	}
	if l.grant.Token != "" && !time.Now().Before(l.localExpiry) {
		return true
	}
	return false
}
func (c *Client) authorizeMutation(r *http.Request) error {
	gated, acquire := kitMutation(r.URL.Path)
	if !gated {
		return nil
	}
	return c.kitLease.Authorize(r, acquire)
}

// kitMutation reports whether path is a kit-lease mutation and whether
// it may claim. Mesh content paths match a base URL prefix as well as
// the exact route, so a remote rooted under a prefix still carries the
// lease header.
func kitMutation(path string) (gated, acquire bool) {
	switch path {
	case "/v1/library/core/load", "/v1/library/core/compose", "/v1/library/core/parts", "/v1/library/core/settings", "/v1/launch", "/v2/launch", "/v1/development/rbf", "/v1/development/core", "/v1/cast/start", "/v1/update/stage", "/v1/update/rollback", "/v1/update/confirm", "/v1/mesh/content/pull":
		return true, true
	case "/v1/stop", "/v1/development/reboot", "/v1/cast/stop", "/v1/update/activate", "/v1/mesh/content/link":
		return true, false
	}
	if strings.HasSuffix(path, "/v1/mesh/content/pull") {
		return true, true
	}
	if strings.HasSuffix(path, "/v1/mesh/content/link") {
		return true, false
	}
	return false, false
}

// AuthorizeMutation applies the host kit-lease rule for r.
// Content pull acquires the session grant. Content link requires it.
func (c *Client) AuthorizeMutation(r *http.Request) error {
	if c == nil {
		return ErrKitLeaseLost
	}
	return c.authorizeMutation(r)
}

// AcquireContentPullLease claims this session's kit grant the way a
// content pull does. A kit held by another session returns that claim
// error and does not steal. The call does not copy bytes.
func (c *Client) AcquireContentPullLease(ctx context.Context) error {
	if c == nil {
		return ErrKitLeaseLost
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/mesh/content/pull", nil).String(), nil)
	if err != nil {
		return err
	}
	return c.authorizeMutation(request)
}

// ReleaseContentPullLease drops the grant this client holds. A fresh
// mesh ensure that fails after claiming uses it. A grant the session
// already held is not released by that path.
func (c *Client) ReleaseContentPullLease(ctx context.Context) error {
	if c == nil || c.kitLease == nil {
		return ErrKitLeaseLost
	}
	return c.kitLease.Release(ctx)
}

// ReleaseKitGrant releases the session grant on this client.
// A nil client or a client without a lease returns ErrKitLeaseLost.
// notHeld is true when the kit reports that this grant is already gone.
func (c *Client) ReleaseKitGrant(ctx context.Context) (notHeld bool, err error) {
	if c == nil || c.kitLease == nil {
		return false, ErrKitLeaseLost
	}
	return c.kitLease.ReleaseGrant(ctx)
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
