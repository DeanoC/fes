package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/protocol"
)

type ApplianceStatus struct {
	BootID       string `json:"boot_id"`
	ImageSHA256  string `json:"image_sha256"`
	Trial        bool   `json:"trial"`
	RawIdleReady bool   `json:"raw_idle_ready"`
	Good         string `json:"good"`
	Previous     string `json:"previous"`
	Pending      string `json:"pending"`
	TrialBootID  string `json:"trial_boot_id"`
	TrialImage   string `json:"trial_image"`
	Corrupt      bool   `json:"corrupt"`
}

func (c *Client) ApplianceStatus(ctx context.Context) (ApplianceStatus, error) {
	var status ApplianceStatus
	err := c.doJSON(ctx, http.MethodGet, "/v1/update", nil, &status)
	return status, err
}
func (c *Client) StageAppliance(ctx context.Context, m appliance.Manifest, content io.Reader) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if content == nil {
		return errors.New("missing release image")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/update/stage", nil).String(), readOnlyReader{Reader: content})
	if err != nil {
		return err
	}
	req.ContentLength = m.ImageSize
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-FogCast-Release-Manifest", base64.StdEncoding.EncodeToString(raw))
	if err = c.authorizeMutation(req); err != nil {
		return err
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result ApplianceStatus
	return decodeResponse(response, &result)
}
func (c *Client) ConfirmAppliance(ctx context.Context, bootID, image string) error {
	var ack struct {
		Confirmed bool `json:"confirmed"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/v1/update/confirm", map[string]string{"boot_id": bootID, "image_sha256": image}, &ack)
	if err == nil && !ack.Confirmed {
		return errors.New("missing update confirmation acknowledgement")
	}
	return err
}

type ResolveAppliance func(context.Context, string) ([]string, error)

// InspectAppliance resolves a changed address using the recorded authenticated
// identity. It performs only reads and also works while a trial is unconfirmed.
func (c *Client) InspectAppliance(ctx context.Context, targetID string, resolve ResolveAppliance) (ApplianceStatus, error) {
	if targetID == "" {
		return ApplianceStatus{}, errors.New("missing target identity")
	}
	endpoint, status, err := c.findApplianceBoot(ctx, targetID, resolve)
	if err != nil {
		return status, err
	}
	if endpoint.String() != c.EndpointURL().String() {
		_, err = c.AdoptEndpoint(ctx, endpoint, false)
	}
	return status, err
}

// UpdateAppliance uploads once and activates once, then observes the changed
// authenticated boot before obtaining a new lease and confirming that trial.
// The caller supplies a bounded context and serializes use of this client.
func (c *Client) UpdateAppliance(ctx context.Context, targetID string, m appliance.Manifest, content io.Reader, resolve ResolveAppliance) (ApplianceStatus, error) {
	if err := m.Validate(); err != nil {
		return ApplianceStatus{}, err
	}
	before, err := c.applianceBefore(ctx, targetID, resolve)
	if err != nil {
		return before, err
	}
	if err = c.StageAppliance(ctx, m, content); err != nil {
		return before, err
	}
	var accepted ApplianceStatus
	err = c.doJSON(ctx, http.MethodPost, "/v1/update/activate", map[string]string{"image_sha256": m.ImageSHA256}, &accepted)
	return c.finishAppliance(ctx, targetID, before, m.ImageSHA256, err, resolve)
}

func (c *Client) RollbackAppliance(ctx context.Context, targetID string, resolve ResolveAppliance) (ApplianceStatus, error) {
	before, err := c.applianceBefore(ctx, targetID, resolve)
	if err != nil {
		return before, err
	}
	if !appliance.ValidHash(before.Previous) {
		return before, errors.New("no previous release to roll back to")
	}
	var accepted ApplianceStatus
	err = c.doJSON(ctx, http.MethodPost, "/v1/update/rollback", nil, &accepted)
	return c.finishAppliance(ctx, targetID, before, before.Previous, err, resolve)
}

func (c *Client) applianceBefore(ctx context.Context, targetID string, resolve ResolveAppliance) (ApplianceStatus, error) {
	status, err := c.InspectAppliance(ctx, targetID, resolve)
	if err != nil {
		return status, err
	}
	if status.Trial || status.Corrupt || status.Pending != "" {
		return status, errors.New("target release is not in a stable update state")
	}
	return status, nil
}

func (c *Client) finishAppliance(ctx context.Context, targetID string, before ApplianceStatus, expected string, activationErr error, resolve ResolveAppliance) (ApplianceStatus, error) {
	var apiErr *protocol.APIError
	// An explicit rejection must not turn into a reboot wait or a second request.
	if errors.As(activationErr, &apiErr) {
		return before, activationErr
	}
	// The activation may have committed even if its response was lost. Stop the
	// pre-reboot renewal loop immediately; observation below makes no mutations.
	c.InvalidateKitSession()
	var (
		lastErr                 = activationErr
		candidateBootID         string
		confirmationEndpoint    string
		confirmationBootID      string
		confirmationReady       bool
		confirmationAttempted   bool
		nextConfirmationAttempt time.Time
	)
	for {
		if err := ctx.Err(); err != nil {
			return before, fmt.Errorf("update outcome unconfirmed; inspect target status: %w", errors.Join(err, lastErr))
		}
		endpoint, status, err := c.findApplianceBoot(ctx, targetID, resolve)
		if err != nil {
			lastErr = err
			if !waitAppliancePoll(ctx) {
				return before, fmt.Errorf("update outcome unconfirmed; inspect target status: %w", errors.Join(ctx.Err(), lastErr))
			}
			continue
		}
		if status.BootID == before.BootID {
			if !waitAppliancePoll(ctx) {
				return before, fmt.Errorf("update outcome unconfirmed; inspect target status: %w", errors.Join(ctx.Err(), lastErr))
			}
			continue
		}
		if status.ImageSHA256 != expected {
			return status, errors.New("target booted a fallback or unexpected image; candidate was not confirmed")
		}
		if status.Corrupt {
			return status, errors.New("target boot selection state is corrupt")
		}
		if candidateBootID == "" {
			candidateBootID = status.BootID
		} else if status.BootID != candidateBootID {
			return status, errors.New("target boot identity changed before confirmation")
		}
		// A status read may race a successful confirmation, so accept the exact
		// expected image once it is durably known-good without requiring a second
		// confirmation request.
		if status.RawIdleReady && !status.Trial && status.Good == expected && status.Pending == "" {
			return status, nil
		}
		if status.Trial && status.RawIdleReady {
			if !confirmationReady || confirmationEndpoint != endpoint.String() || confirmationBootID != status.BootID {
				ownership, adoptErr := c.AdoptEndpoint(ctx, endpoint, true)
				if adoptErr != nil {
					lastErr = adoptErr
				} else if ownership.State != "free" {
					lastErr = fmt.Errorf("target ownership is %s", ownership.State)
				} else {
					confirmationEndpoint = endpoint.String()
					confirmationBootID = status.BootID
					confirmationReady = true
					confirmationAttempted = false
					nextConfirmationAttempt = time.Time{}
				}
			}
			if confirmationReady && confirmationEndpoint == endpoint.String() && confirmationBootID == status.BootID && (!confirmationAttempted || !time.Now().Before(nextConfirmationAttempt)) {
				confirmErr := c.ConfirmAppliance(ctx, status.BootID, expected)
				confirmationAttempted = true
				nextConfirmationAttempt = time.Now().Add(time.Second)
				if confirmErr != nil {
					lastErr = confirmErr
				}
			}
		}
		if !waitAppliancePoll(ctx) {
			return before, fmt.Errorf("update outcome unconfirmed; inspect target status: %w", errors.Join(ctx.Err(), lastErr))
		}
	}
}

func waitAppliancePoll(ctx context.Context) bool {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Client) findApplianceBoot(ctx context.Context, targetID string, resolve ResolveAppliance) (*url.URL, ApplianceStatus, error) {
	probe := func(base *url.URL) (ApplianceStatus, error) {
		short, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		peer := c.Peer(base)
		health, err := peer.Health(short)
		if err != nil {
			return ApplianceStatus{}, err
		}
		if health.TargetID != targetID || health.BootID == "" {
			return ApplianceStatus{}, errors.New("discovered target identity mismatch")
		}
		status, err := peer.ApplianceStatus(short)
		if err != nil {
			return status, err
		}
		if status.BootID != health.BootID || !appliance.ValidHash(status.ImageSHA256) {
			return status, errors.New("inconsistent boot identity")
		}
		return status, nil
	}
	base := c.EndpointURL()
	if status, err := probe(base); err == nil {
		return base, status, nil
	}
	if resolve == nil {
		return nil, ApplianceStatus{}, errors.New("target unavailable")
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	addresses, err := resolve(lookup, targetID)
	cancel()
	if err != nil {
		return nil, ApplianceStatus{}, err
	}
	var matched *url.URL
	var result ApplianceStatus
	seen := map[string]bool{}
	for _, address := range addresses {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		status, err := probe(u)
		if err != nil {
			continue
		}
		if matched != nil {
			return nil, ApplianceStatus{}, errors.New("multiple endpoints claim the same target identity")
		}
		matched, result = u, status
	}
	if matched == nil {
		return nil, result, errors.New("target unavailable after discovery")
	}
	return matched, result, nil
}
