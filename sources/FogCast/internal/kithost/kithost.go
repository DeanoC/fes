// Package kithost prepares fogcast-api's kit mode (#863 slice 4): a host that
// runs on the kit, owns it, and drives only the kit's own agent.
package kithost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/appliancedata"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	// DefaultAgentConfig is the agent's provisioned configuration on FAT.
	DefaultAgentConfig = "/media/fat/fogcast/agent.toml"
	// DefaultTemplate is the image's catalog config, which tenfoot also reads.
	DefaultTemplate = "/usr/share/fogcast/config.toml"
	// DefaultStateRoot holds the kit host's config, catalog and staging on
	// FESDATA3.
	DefaultStateRoot = appliancedata.DefaultMountPoint + "/fogcast-host"
	// FallbackStateRoot is tmpfs, used when FESDATA3 is not durable storage.
	// The kit still runs, as the supervisor does without FESDATA3.
	FallbackStateRoot = "/run/fogcast/kit-host"
	// DefaultAgentWait bounds the startup wait for the agent.
	DefaultAgentWait = 30 * time.Second
)

// Options locates kit mode's inputs. Zero fields take the defaults.
type Options struct {
	AgentConfig  string
	Template     string
	StateRoot    string
	FallbackRoot string
	AgentWait    time.Duration
	// CheckPersistent verifies that a path is FESDATA3 storage before
	// anything is created under it. Prepare checks the state root and the
	// exact state and staging paths. It defaults to
	// appliancedata.CheckPersistent.
	CheckPersistent func(string) error
	HTTPClient      *http.Client
	Logf            func(format string, args ...any)
}

// Prepare writes the kit host's config and returns its path. It reads the
// agent's config (a missing or invalid one is an error), waits up to
// AgentWait for an authenticated agent health to learn the target ID, checks
// that the state root and its state and staging paths are on FESDATA3 or falls
// back to tmpfs, then renders the config from the existing file or the
// template. The FESDATA3 check follows the wait because mister-agent is what
// prepares FESDATA3.
func Prepare(ctx context.Context, options Options) (string, error) {
	o := withDefaults(options)
	agent, err := agentconfig.Load(o.AgentConfig)
	if err != nil {
		return "", fmt.Errorf("kit host: agent config %s: %w", o.AgentConfig, err)
	}
	address, err := AgentAddress(agent.ListenAddress)
	if err != nil {
		return "", err
	}
	template, err := os.ReadFile(o.Template)
	if err != nil {
		return "", fmt.Errorf("kit host: template %s: %w", o.Template, err)
	}
	targetID := agent.TargetID
	started := time.Now()
	observed, err := WaitForAgent(ctx, o.HTTPClient, address, agent.Token, o.AgentWait)
	switch {
	case err != nil && ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		o.Logf("fogcast: kit host did not get the agent's target ID from %s within %s (%v); starting without it", address, o.AgentWait, err)
	default:
		o.Logf("fogcast: kit host reached the agent after %s", time.Since(started).Round(time.Millisecond))
		if targetID != "" && observed != targetID {
			return "", fmt.Errorf("kit host: agent reports target ID %s, its config says %s", observed, targetID)
		}
		targetID = observed
	}
	root := o.StateRoot
	var persistentErr error
	for _, path := range []string{root, filepath.Join(root, "state"), filepath.Join(root, "staging")} {
		if err := o.CheckPersistent(path); err != nil {
			persistentErr = fmt.Errorf("%s: %w", path, err)
			break
		}
	}
	if persistentErr != nil {
		o.Logf("fogcast: kit host state %s is not on FESDATA3 (%v); using tmpfs %s, which a restart loses", root, persistentErr, o.FallbackRoot)
		root = o.FallbackRoot
	}
	if err := privateDirectory(root); err != nil {
		return "", fmt.Errorf("kit host: state root: %w", err)
	}
	path := filepath.Join(root, "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("kit host: existing config: %w", err)
	}
	kit := fogcast.KitHost{
		AgentAddress: address, AgentToken: agent.Token, TargetID: targetID,
		State: filepath.Join(root, "state"), Staging: filepath.Join(root, "staging"),
	}
	rendered, err := fogcast.KitHostConfig(template, existing, kit)
	if err != nil && len(existing) != 0 {
		// A config this host cannot read, perhaps written by a newer bundle,
		// must not stop every start. Keep it beside the new one.
		fresh, freshErr := fogcast.KitHostConfig(template, nil, kit)
		if freshErr == nil {
			aside := fmt.Sprintf("%s.unreadable-%d", path, time.Now().Unix())
			if renameErr := os.Rename(path, aside); renameErr != nil {
				return "", fmt.Errorf("kit host: move aside unreadable config: %w", renameErr)
			}
			o.Logf("fogcast: kit host config was unreadable (%v); kept it as %s and started from the template", err, aside)
			rendered, err = fresh, nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("kit host: %w", err)
	}
	if err := writePrivate(path, rendered); err != nil {
		return "", fmt.Errorf("kit host: write config: %w", err)
	}
	return path, nil
}

// AgentAddress is the loopback origin of an agent listening on listen. A
// wildcard host maps to 127.0.0.1, and a loopback host is kept as given. An
// agent bound to any other address is refused, because the kit host must not
// dial it over the LAN.
func AgentAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("kit host: agent listen_address %q: %w", listen, err)
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("kit host: agent listen_address %q has no valid port", listen)
	}
	switch host {
	case "", "0.0.0.0", "::", "localhost":
		host = "127.0.0.1"
	default:
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("kit host: agent listens only on %s, not on loopback", host)
		}
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// errTokenRejected is an agent that answers health but withholds its target
// ID: the token is wrong, and waiting longer cannot help.
var errTokenRejected = errors.New("agent health has no target ID; the agent rejected the token")

// WaitForAgent polls the agent's health with its bearer until it reports a
// target ID, which it does only to an authenticated caller, or until wait
// elapses.
func WaitForAgent(ctx context.Context, client *http.Client, address, token string, wait time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var last error
	for {
		id, err := agentTargetID(ctx, client, address, token)
		if err == nil {
			return id, nil
		}
		if errors.Is(err, errTokenRejected) {
			return "", err
		}
		last = err
		select {
		case <-ctx.Done():
			return "", last
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func agentTargetID(ctx context.Context, client *http.Client, address, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/v1/health", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agent health returned %s", response.Status)
	}
	var health protocol.Health
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 1<<20)).Decode(&health); err != nil {
		return "", fmt.Errorf("agent health: %w", err)
	}
	if health.TargetID == "" {
		return "", errTokenRejected
	}
	if !discovery.ValidID(health.TargetID) {
		return "", fmt.Errorf("agent health has an invalid target ID")
	}
	return health.TargetID, nil
}

// privateDirectory creates root with mode 0700. A missing parent is created
// with the usual 0755, and the new entry is synced into its parent.
func privateDirectory(root string) error {
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a real directory", root)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writePrivate(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func withDefaults(o Options) Options {
	if o.AgentConfig == "" {
		o.AgentConfig = DefaultAgentConfig
	}
	if o.Template == "" {
		o.Template = DefaultTemplate
	}
	if o.StateRoot == "" {
		o.StateRoot = DefaultStateRoot
	}
	if o.FallbackRoot == "" {
		o.FallbackRoot = FallbackStateRoot
	}
	if o.AgentWait <= 0 {
		o.AgentWait = DefaultAgentWait
	}
	if o.CheckPersistent == nil {
		o.CheckPersistent = appliancedata.CheckPersistent
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return o
}
