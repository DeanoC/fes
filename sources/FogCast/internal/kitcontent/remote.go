package kitcontent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DeanoC/FogCast/internal/meshcontent"
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
	base   url.URL
	token  string
	client *http.Client
	nodeID string
	abis   []meshcontent.EligibleABI
	auth   MutationAuthorizer
}

type nodeDocument struct {
	NodeID string `json:"node_id"`
	ABIs   []struct {
		ID    string `json:"id"`
		Major int    `json:"major"`
	} `json:"abis"`
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

func (r *Remote) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	if r == nil || id.Validate() != nil {
		return meshcontent.StateMissing
	}
	var doc stateDocument
	if err := r.get(context.Background(), "/v1/mesh/content/slot", url.Values{"id": {id.String()}}, &doc); err != nil {
		return meshcontent.StateMissing
	}
	switch meshcontent.SlotState(doc.State) {
	case meshcontent.StatePresent, meshcontent.StateChecking, meshcontent.StateMissing:
		return meshcontent.SlotState(doc.State)
	default:
		return meshcontent.StateMissing
	}
}

func (r *Remote) SourceAdvertises(id meshcontent.ContentID) bool {
	if r == nil || id.Validate() != nil {
		return false
	}
	var doc sourceDocument
	if err := r.get(context.Background(), "/v1/mesh/content/source", url.Values{"id": {id.String()}}, &doc); err != nil {
		return false
	}
	return doc.Advertises
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
	return r.post(context.Background(), "/v1/mesh/content/link", url.Values{"name": {name}}, body, &struct{}{})
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
		return meshcontent.ErrContentPullFailed
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return meshcontent.ErrContentPullFailed
	}
	if resp.StatusCode != http.StatusOK {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return meshcontent.ErrContentPullFailed
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return meshcontent.ErrContentPullFailed
	}
	return nil
}
