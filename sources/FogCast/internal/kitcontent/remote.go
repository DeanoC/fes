package kitcontent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// remoteReadTimeout bounds a slot, source, node, or slots read when the
// caller did not set a deadline. remoteLinkTimeout bounds a link the
// same way. remoteSnapshotTTL is how long ReadyHere may reuse a
// successful slots snapshot. Ensure reads one id and does not use that
// snapshot. Pull and link drop it.
var (
	remoteReadTimeout = 5 * time.Second
	remoteLinkTimeout = 15 * time.Second
	remoteSnapshotTTL = time.Second
)

// MutationAuthorizer admits one host mutation with the session kit lease.
// Pull acquires that grant. Link requires the grant already held.
type MutationAuthorizer interface {
	AuthorizeMutation(*http.Request) error
}

// Remote is a host-side meshcontent.Executor for one kit content store.
// Ensure calls it. The kit holds the bytes. This type does not program
// the FPGA. Pull and link ask the mutation authorizer, when one is set,
// before the request is sent.
type Remote struct {
	base     url.URL
	token    string
	client   *http.Client
	nodeID   string
	abis     []meshcontent.EligibleABI
	packages []string
	auth     MutationAuthorizer
	snap     snapCache
}

type snapCache struct {
	mu    sync.Mutex
	key   string
	at    time.Time
	facts map[string]meshcontent.SlotFact
}

type nodeDocument struct {
	NodeID string `json:"node_id"`
	ABIs   []struct {
		ID    string `json:"id"`
		Major int    `json:"major"`
	} `json:"abis"`
	Packages []string `json:"packages"`
}

type stateDocument struct {
	State string `json:"state"`
}

type sourceDocument struct {
	Advertises bool `json:"advertises"`
}

// Dial reads the kit's node id and eligible ABIs. The returned executor
// drives that kit. A kit whose node id is not the session's bound node
// is refused by Ensure.
func Dial(ctx context.Context, endpoint *url.URL, token string, client *http.Client) (*Remote, error) {
	if endpoint == nil || strings.TrimSpace(token) == "" {
		return nil, errors.New("mesh content remote requires an endpoint and token")
	}
	if client == nil {
		client = http.DefaultClient
	}
	remote := &Remote{base: *endpoint, token: token, client: client}
	ctx, cancel := remote.bound(ctx, remoteReadTimeout)
	defer cancel()
	var doc nodeDocument
	if err := remote.get(ctx, "/v1/mesh/content/node", nil, &doc); err != nil {
		return nil, err
	}
	if strings.TrimSpace(doc.NodeID) == "" {
		return nil, errors.New("mesh content kit did not report a node id")
	}
	remote.nodeID = doc.NodeID
	remote.abis = make([]meshcontent.EligibleABI, 0, len(doc.ABIs))
	for _, abi := range doc.ABIs {
		remote.abis = append(remote.abis, meshcontent.EligibleABI{ID: abi.ID, Major: abi.Major})
	}
	remote.packages = normalizePackageIDs(doc.Packages)
	return remote, nil
}

// SetMutationAuthorizer installs the host kit-lease check for pull and
// link. Node, slot, and source reads do not use it. Set it before Ensure.
func (r *Remote) SetMutationAuthorizer(auth MutationAuthorizer) {
	if r == nil {
		return
	}
	r.auth = auth
}

func (r *Remote) NodeID() string {
	if r == nil {
		return ""
	}
	return r.nodeID
}

func (r *Remote) EligibleABIs() []meshcontent.EligibleABI {
	if r == nil {
		return nil
	}
	return append([]meshcontent.EligibleABI(nil), r.abis...)
}

// Packages returns described package ids the kit reported. An empty
// list is not eligibility. Executor method signatures are unchanged;
// ReadyHere reads this through PackageHolder.
func (r *Remote) Packages() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.packages...)
}

func (r *Remote) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	state, err := r.ReadSlot(context.Background(), id)
	if err != nil {
		return meshcontent.StateMissing
	}
	return state
}

// ReadSlot reports id. A transport failure is ErrContentUnreachable,
// not Missing. The read has a deadline when ctx does not.
func (r *Remote) ReadSlot(ctx context.Context, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if r == nil || id.Validate() != nil {
		return meshcontent.StateMissing, nil
	}
	ctx, cancel := r.bound(ctx, remoteReadTimeout)
	defer cancel()
	var doc stateDocument
	if err := r.get(ctx, "/v1/mesh/content/slot", url.Values{"id": {id.String()}}, &doc); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	switch meshcontent.SlotState(doc.State) {
	case meshcontent.StatePresent, meshcontent.StateChecking, meshcontent.StateMissing:
		return meshcontent.SlotState(doc.State), nil
	default:
		return "", meshcontent.ErrContentUnreachable
	}
}

func (r *Remote) SourceAdvertises(id meshcontent.ContentID) bool {
	ok, err := r.ReadSource(context.Background(), id)
	return err == nil && ok
}

// ReadSource reports whether a source advertises id. A transport
// failure is an error, not a missing source.
func (r *Remote) ReadSource(ctx context.Context, id meshcontent.ContentID) (bool, error) {
	if r == nil || id.Validate() != nil {
		return false, nil
	}
	ctx, cancel := r.bound(ctx, remoteReadTimeout)
	defer cancel()
	var doc sourceDocument
	if err := r.get(ctx, "/v1/mesh/content/source", url.Values{"id": {id.String()}}, &doc); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	return doc.Advertises, nil
}

// Snapshot reads ids in one batch request per chunk. A fresh successful
// snapshot for the same ids is reused until remoteSnapshotTTL. A
// transport failure is not Missing.
func (r *Remote) Snapshot(ctx context.Context, ids []meshcontent.ContentID) (map[string]meshcontent.SlotFact, error) {
	if r == nil {
		return nil, meshcontent.ErrContentUnreachable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make([]meshcontent.ContentID, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id.Validate() != nil {
			continue
		}
		if _, ok := seen[id.String()]; ok {
			continue
		}
		seen[id.String()] = struct{}{}
		wanted = append(wanted, id)
	}
	key := snapshotKey(wanted)
	if facts, ok := r.cachedSnapshot(key); ok {
		return facts, nil
	}
	facts := make(map[string]meshcontent.SlotFact, len(wanted))
	for start := 0; start < len(wanted); start += meshcontent.MaxSlotBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + meshcontent.MaxSlotBatch
		if end > len(wanted) {
			end = len(wanted)
		}
		chunk, err := r.fetchSlots(ctx, wanted[start:end])
		if err != nil {
			return nil, err
		}
		for id, fact := range chunk {
			facts[id] = fact
		}
	}
	r.storeSnapshot(key, facts)
	return cloneFacts(facts), nil
}

func (r *Remote) Pull(ctx context.Context, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if r == nil {
		return "", meshcontent.ErrContentPullFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := id.Validate(); err != nil {
		return "", err
	}
	var doc stateDocument
	err := r.post(ctx, "/v1/mesh/content/pull", url.Values{"id": {id.String()}}, nil, &doc)
	r.dropSnapshot()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	state := meshcontent.SlotState(doc.State)
	switch state {
	case meshcontent.StatePresent, meshcontent.StateChecking:
		return state, nil
	default:
		return "", meshcontent.ErrContentPullFailed
	}
}

func (r *Remote) LinkExpansion(name string, id meshcontent.ContentID) error {
	if r == nil {
		return errors.New("mesh content remote is missing")
	}
	body, err := json.Marshal(map[string]string{"content_id": id.String()})
	if err != nil {
		return err
	}
	ctx, cancel := r.bound(context.Background(), remoteLinkTimeout)
	defer cancel()
	err = r.post(ctx, "/v1/mesh/content/link", url.Values{"name": {name}}, body, &struct{}{})
	r.dropSnapshot()
	return err
}

func (r *Remote) get(ctx context.Context, path string, query url.Values, dest any) error {
	return r.do(ctx, http.MethodGet, path, query, nil, dest)
}

func (r *Remote) post(ctx context.Context, path string, query url.Values, body []byte, dest any) error {
	return r.do(ctx, http.MethodPost, path, query, body, dest)
}

func (r *Remote) do(ctx context.Context, method, path string, query url.Values, body []byte, dest any) error {
	endpoint := r.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.auth != nil && method == http.MethodPost {
		if err := r.auth.AuthorizeMutation(req); err != nil {
			return err
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return r.transportErr(method, path)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return r.transportErr(method, path)
	}
	if resp.StatusCode != http.StatusOK {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lease := leaseDenied(payload); lease != nil {
			return lease
		}
		return r.transportErr(method, path)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return r.transportErr(method, path)
	}
	return nil
}

func (r *Remote) transportErr(method, path string) error {
	if method == http.MethodGet {
		return meshcontent.ErrContentUnreachable
	}
	if strings.HasSuffix(path, "/link") {
		return meshcontent.ErrContentLinkFailed
	}
	return meshcontent.ErrContentPullFailed
}

func (r *Remote) bound(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, limit)
}

func (r *Remote) fetchSlots(ctx context.Context, ids []meshcontent.ContentID) (map[string]meshcontent.SlotFact, error) {
	ctx, cancel := r.bound(ctx, remoteReadTimeout)
	defer cancel()
	query := url.Values{}
	for _, id := range ids {
		query.Add("id", id.String())
	}
	var doc struct {
		Slots []struct {
			ID         string `json:"id"`
			State      string `json:"state"`
			Advertises bool   `json:"advertises"`
		} `json:"slots"`
	}
	if err := r.get(ctx, "/v1/mesh/content/slots", query, &doc); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	out := make(map[string]meshcontent.SlotFact, len(doc.Slots))
	for _, slot := range doc.Slots {
		state := meshcontent.SlotState(slot.State)
		switch state {
		case meshcontent.StatePresent, meshcontent.StateChecking, meshcontent.StateMissing:
		default:
			return nil, meshcontent.ErrContentUnreachable
		}
		out[slot.ID] = meshcontent.SlotFact{State: state, Advertises: slot.Advertises}
	}
	for _, id := range ids {
		if _, ok := out[id.String()]; !ok {
			return nil, meshcontent.ErrContentUnreachable
		}
	}
	return out, nil
}

func (r *Remote) cachedSnapshot(key string) (map[string]meshcontent.SlotFact, bool) {
	if r == nil || remoteSnapshotTTL <= 0 {
		return nil, false
	}
	r.snap.mu.Lock()
	defer r.snap.mu.Unlock()
	if r.snap.facts == nil || r.snap.key != key || time.Since(r.snap.at) >= remoteSnapshotTTL {
		return nil, false
	}
	return cloneFacts(r.snap.facts), true
}

func (r *Remote) storeSnapshot(key string, facts map[string]meshcontent.SlotFact) {
	if r == nil || remoteSnapshotTTL <= 0 {
		return
	}
	r.snap.mu.Lock()
	r.snap.key = key
	r.snap.at = time.Now()
	r.snap.facts = cloneFacts(facts)
	r.snap.mu.Unlock()
}

func (r *Remote) dropSnapshot() {
	if r == nil {
		return
	}
	r.snap.mu.Lock()
	r.snap.at = time.Time{}
	r.snap.facts = nil
	r.snap.mu.Unlock()
}

func snapshotKey(ids []meshcontent.ContentID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func cloneFacts(in map[string]meshcontent.SlotFact) map[string]meshcontent.SlotFact {
	out := make(map[string]meshcontent.SlotFact, len(in))
	for id, fact := range in {
		out[id] = fact
	}
	return out
}

func leaseDenied(payload []byte) error {
	var doc struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &doc) != nil || doc.Error.Code == "" {
		return nil
	}
	switch doc.Error.Code {
	case "KIT_LEASE_REQUIRED", "KIT_LEASE_BUSY", "KIT_LEASE_DENIED", "KIT_LEASE_BLOCKED":
		return &meshcontent.LeaseDeniedError{Code: doc.Error.Code}
	default:
		return nil
	}
}
