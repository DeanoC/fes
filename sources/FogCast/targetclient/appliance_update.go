package targetclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
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
	c.reportProgress("upload start", fmt.Sprintf("%d bytes", m.ImageSize))
	upload := &applianceUploadProgress{reader: content, size: m.ImageSize, report: c.reportProgress, last: time.Now()}
	// The transport may still read the body after Do returns (an early reply).
	// Closing the wrapper first keeps every progress call on this goroutine's
	// side of the return, so hooks never race later phases or the caller.
	defer upload.finish()
	content = upload
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
	upload.finish()
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result ApplianceStatus
	if err := decodeResponse(response, &result); err != nil {
		return err
	}
	c.reportProgress("staged", m.ImageSHA256)
	return nil
}

type applianceUploadProgress struct {
	mu          sync.Mutex
	done        bool
	reader      io.Reader
	size, sent  int64
	last        time.Time
	lastPercent int64
	report      func(string, string)
}

// finish stops further reads and progress reports. It is idempotent.
func (p *applianceUploadProgress) finish() {
	p.mu.Lock()
	p.done = true
	p.mu.Unlock()
}

func (p *applianceUploadProgress) Read(b []byte) (int, error) {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done {
		return 0, io.ErrClosedPipe
	}
	// The source read is not under the lock, so a blocked read cannot stall finish.
	n, err := p.reader.Read(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return n, err
	}
	p.sent += int64(n)
	percent := p.sent * 100 / p.size
	if p.sent == p.size || (p.sent > 0 && (time.Since(p.last) >= 10*time.Second || percent-p.lastPercent >= 10)) {
		p.report("upload", fmt.Sprintf("%d/%d bytes (%d%%)", p.sent, p.size, percent))
		p.last = time.Now()
		p.lastPercent = percent
	}
	return n, err
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
	if err == nil {
		c.reportProgress("activation accepted", m.ImageSHA256)
	} else {
		c.reportProgress("activation response lost", err.Error())
	}
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
	if err == nil {
		c.reportProgress("activation accepted", before.Previous)
	} else {
		c.reportProgress("activation response lost", err.Error())
	}
	return c.finishAppliance(ctx, targetID, before, before.Previous, err, resolve)
}

func (c *Client) applianceBefore(ctx context.Context, targetID string, resolve ResolveAppliance) (ApplianceStatus, error) {
	status, err := c.InspectAppliance(ctx, targetID, resolve)
	if err != nil {
		return status, err
	}
	c.reportProgress("inspect/before", fmt.Sprintf("boot=%s image=%s good=%s", status.BootID, status.ImageSHA256, status.Good))
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
	return c.waitApplianceConfirmation(ctx, targetID, expected, before.BootID, false, activationErr, resolve)
}

// ConfirmApplianceTrial resumes confirmation of an expected release without staging or activation.
func (c *Client) ConfirmApplianceTrial(ctx context.Context, targetID, expected string, resolve ResolveAppliance) (ApplianceStatus, error) {
	if !appliance.ValidHash(expected) {
		return ApplianceStatus{}, errors.New("expected image must be a 64-character lowercase SHA-256")
	}
	return c.waitApplianceConfirmation(ctx, targetID, expected, "", true, nil, resolve)
}

func (c *Client) waitApplianceConfirmation(ctx context.Context, targetID, expected, beforeBoot string, resume bool, initialErr error, resolve ResolveAppliance) (ApplianceStatus, error) {
	var (
		lastErr                 = initialErr
		candidateBootID         string
		confirmationEndpoint    string
		confirmationBootID      string
		confirmationReady       bool
		confirmationAttempted   bool
		nextConfirmationAttempt time.Time
		lastStatus              ApplianceStatus
		lastPhase               string
		lastWait                time.Time
		seenBoot                string
		started                 = time.Now()
	)
	event := func(phase, detail string) {
		c.reportProgress(phase, detail)
		lastPhase = phase
		lastWait = time.Now()
	}
	progress := func(phase, detail string) {
		if phase != lastPhase {
			event(phase, detail)
		} else if time.Since(lastWait) >= 30*time.Second {
			event(phase, fmt.Sprintf("still waiting after %s; %s", time.Since(started).Round(time.Second), detail))
		}
	}
	expired := func() (ApplianceStatus, error) {
		return lastStatus, fmt.Errorf("update outcome unconfirmed (last phase: %s); run fes-update --action confirm: %w", lastPhase, errors.Join(ctx.Err(), lastErr))
	}
	for {
		if err := ctx.Err(); err != nil {
			return expired()
		}
		endpoint, status, err := c.findApplianceBoot(ctx, targetID, resolve)
		if err != nil {
			lastErr = err
			progress("old boot gone", "target unreachable")
			if !waitAppliancePoll(ctx) {
				return expired()
			}
			continue
		}
		lastStatus = status
		if !resume && status.BootID == beforeBoot {
			progress("waiting for new boot", "activation may still be pending")
			if !waitAppliancePoll(ctx) {
				return expired()
			}
			continue
		}
		if resume && status.BootID != candidateBootID {
			candidateBootID = status.BootID
			confirmationReady = false
		}
		if seenBoot != status.BootID {
			event("new boot seen", fmt.Sprintf("boot=%s image=%s trial=%t", status.BootID, status.ImageSHA256, status.Trial))
			seenBoot = status.BootID
		}
		if status.Corrupt {
			return status, errors.New("target boot selection state is corrupt")
		}
		if status.ImageSHA256 == expected && !status.Trial && status.Good == expected && status.Pending == "" && (resume || status.RawIdleReady) {
			event("confirmed", fmt.Sprintf("boot=%s image=%s", status.BootID, expected))
			return status, nil
		}
		if resume && !status.Trial && status.Pending == expected && status.ImageSHA256 != expected && status.Good == status.ImageSHA256 {
			progress("waiting for new boot", "expected image pending activation")
			if !waitAppliancePoll(ctx) {
				return expired()
			}
			continue
		}
		if status.ImageSHA256 != expected {
			return status, fmt.Errorf("target booted unexpected image %s; expected %s; candidate was not confirmed", status.ImageSHA256, expected)
		}
		if !resume && candidateBootID == "" {
			candidateBootID = status.BootID
		} else if !resume && status.BootID != candidateBootID {
			return status, errors.New("target boot identity changed before confirmation")
		}
		if !status.Trial {
			return status, errors.New("expected image is running without a confirmable trial or durable good state")
		}
		if !status.RawIdleReady {
			progress("waiting for raw_idle_ready", fmt.Sprintf("boot=%s", status.BootID))
		} else {
			if !confirmationReady || confirmationEndpoint != endpoint.String() || confirmationBootID != status.BootID {
				ownership, adoptErr := c.AdoptEndpoint(ctx, endpoint, true)
				if adoptErr != nil {
					lastErr = adoptErr
					progress("waiting for lease", adoptErr.Error())
				} else if ownership.State != "free" {
					lastErr = fmt.Errorf("target ownership is %s", ownership.State)
					progress("waiting for lease", lastErr.Error())
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
				if c.kitLease != nil && c.kitLease.Held() && !confirmationAttempted {
					event("lease claimed", fmt.Sprintf("boot=%s", status.BootID))
				}
				event("confirm sent", fmt.Sprintf("boot=%s image=%s", status.BootID, expected))
				confirmationAttempted = true
				nextConfirmationAttempt = time.Now().Add(time.Second)
				if confirmErr != nil {
					lastErr = confirmErr
					event("confirm result", confirmErr.Error())
				} else {
					event("confirm result", "acknowledged; verifying durable state")
				}
			}
		}
		if !waitAppliancePoll(ctx) {
			return expired()
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
