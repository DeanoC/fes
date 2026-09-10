package kitlauncher

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	RefuseNeedsHost = "Needs host"
	RefuseKitInUse  = "Kit in use"

	hostlessLaunchTimeout = 60 * time.Second
)

type hostlessRuntime struct {
	agent *host.Client
	lease *host.KitLease
}

type hostlessRefuse struct{ reason string }

func (e hostlessRefuse) Error() string { return e.reason }

func agentConfigPath(c Config) string {
	if c.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(c.path), "agent.toml")
}

func loopbackAgentURL(listen string) (*url.URL, error) {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, err
	}
	return url.Parse("http://127.0.0.1:" + port)
}

func openHostless(configPath string) (*hostlessRuntime, error) {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	u, err := loopbackAgentURL(cfg.ListenAddress)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: hostlessLaunchTimeout}
	lease := host.NewKitLease(u, cfg.Token, httpClient, kitlease.HostlessOwner, kitlease.HostlessPurpose)
	agent := host.NewClient(u, cfg.Token, httpClient).WithKitLease(lease)
	return &hostlessRuntime{agent: agent, lease: lease}, nil
}

func hostlessEligible(game tenfoot.Game) error {
	if !game.Launchable {
		return hostlessRefuse{RefuseNeedsHost}
	}
	spec, ok := core.DefaultRegistry().Lookup(protocol.System(game.System))
	if !ok || spec.ROMless {
		return hostlessRefuse{RefuseNeedsHost}
	}
	return nil
}

func mapHostlessError(err error) error {
	if err == nil {
		return nil
	}
	var refuse hostlessRefuse
	if errors.As(err, &refuse) {
		return err
	}
	if errors.Is(err, host.ErrKitLeaseLost) {
		return hostlessRefuse{RefuseKitInUse}
	}
	var api *protocol.APIError
	if errors.As(err, &api) {
		switch api.Code {
		case protocol.CodeContentNotCached, protocol.CodeUnsupportedSystem, protocol.CodeBadRequest, protocol.CodeROMNotFound:
			return hostlessRefuse{RefuseNeedsHost}
		case protocol.ErrorCode("KIT_LEASE_BUSY"), protocol.ErrorCode("KIT_LEASE_REQUIRED"), protocol.ErrorCode("KIT_LEASE_BLOCKED"), protocol.ErrorCode("KIT_LEASE_DENIED"):
			return hostlessRefuse{RefuseKitInUse}
		}
	}
	return hostlessRefuse{RefuseNeedsHost}
}

func (h *hostlessRuntime) held() bool {
	return h != nil && h.lease != nil && h.lease.Held()
}

func (h *hostlessRuntime) launch(ctx context.Context, game tenfoot.Game) (protocol.CachedLaunchResponse, error) {
	if err := hostlessEligible(game); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if h == nil || h.agent == nil {
		return protocol.CachedLaunchResponse{}, hostlessRefuse{RefuseNeedsHost}
	}
	status, err := h.agent.KitLeaseStatus(ctx)
	if err != nil {
		return protocol.CachedLaunchResponse{}, hostlessRefuse{RefuseNeedsHost}
	}
	if kitlease.ForeignSession(status) {
		return protocol.CachedLaunchResponse{}, hostlessRefuse{RefuseKitInUse}
	}
	identity, err := h.agent.CachedIdentity(ctx, game.ID)
	if err != nil {
		return protocol.CachedLaunchResponse{}, mapHostlessError(err)
	}
	if !identity.Present || identity.System == nil || identity.Content == nil {
		return protocol.CachedLaunchResponse{}, hostlessRefuse{RefuseNeedsHost}
	}
	if *identity.System != protocol.System(game.System) {
		return protocol.CachedLaunchResponse{}, hostlessRefuse{RefuseNeedsHost}
	}
	response, err := h.agent.LaunchContent(ctx, protocol.CachedLaunchRequest{
		GameID:  game.ID,
		System:  *identity.System,
		Content: *identity.Content,
	})
	if err != nil {
		_ = h.release(ctx)
		return protocol.CachedLaunchResponse{}, mapHostlessError(err)
	}
	return response, nil
}

func (h *hostlessRuntime) stop(ctx context.Context) error {
	if h == nil || h.agent == nil || !h.held() {
		return nil
	}
	_, err := h.agent.Stop(ctx)
	if err != nil {
		return mapHostlessError(err)
	}
	return nil
}

func (h *hostlessRuntime) release(ctx context.Context) error {
	if h == nil || h.lease == nil {
		return nil
	}
	if err := h.lease.Release(ctx); err != nil {
		return mapHostlessError(err)
	}
	return nil
}

func idleHostlessSession() Session {
	return Session{State: string(protocol.StateIdle)}
}

// stopAndRelease stops the hostless program then releases the grant. Stop
// failure still attempts release so a returning host can claim. A failed
// release keeps the local grant (Held stays true) so the kit can retry.
func (h *hostlessRuntime) stopAndRelease(ctx context.Context) (Session, error) {
	if !h.held() {
		return idleHostlessSession(), nil
	}
	stopErr := h.stop(ctx)
	session := Session{}
	if stopErr == nil {
		session = idleHostlessSession()
		if st, stErr := h.status(ctx); stErr == nil {
			session = sessionFromStatus(st)
		}
	}
	relErr := h.release(ctx)
	if stopErr != nil {
		return session, stopErr
	}
	return session, relErr
}

func (h *hostlessRuntime) yield(ctx context.Context) error {
	_, err := h.stopAndRelease(ctx)
	return err
}

func (h *hostlessRuntime) status(ctx context.Context) (protocol.Status, error) {
	if h == nil || h.agent == nil {
		return protocol.Status{}, hostlessRefuse{RefuseNeedsHost}
	}
	return h.agent.Status(ctx)
}

func sessionFromStatus(st protocol.Status) Session {
	s := Session{State: string(st.State)}
	if st.GameID != nil {
		s.GameID = *st.GameID
	}
	if st.State == protocol.StateActive {
		s.Execution = "fpga_native"
	}
	return s
}

func (c *Client) attachHostless() {
	if c == nil {
		return
	}
	path := agentConfigPath(c.config)
	if path == "" {
		return
	}
	h, err := openHostless(path)
	if err != nil {
		return
	}
	c.hostless = h
}

func (c *Client) hostlessHeld() bool {
	return c != nil && c.hostless.held()
}
