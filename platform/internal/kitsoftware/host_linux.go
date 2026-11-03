package kitsoftware

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	roleHost = "host"
	// hostAdvertisement is the bundle manifest interface that says
	// bin/fogcast-kit has a `host` mode (#863 slice 5). Older bundles carry
	// the fogcast-kit stub, which exits; the supervisor does not start it.
	hostAdvertisement = "kit-host"
	hostExecutable    = "bin/fogcast-kit"
)

// hostPlan is how the supervisor starts the kit's FogCast host.
type hostPlan struct {
	path string
	args []string
	env  []string
}

// planHostLocked records the host for the bundle that Start just started.
// The host starts on a later tick, once the launcher is ready, so its
// start-up does not compete with the agent and launcher for the board.
func (c *ProcessController) planHostLocked(root string, env []string) {
	c.hostPlan = nil
	c.hostRestart, c.hostDelay = time.Time{}, 0
	if !advertisesHost(root) {
		return
	}
	c.hostPlan = &hostPlan{path: filepath.Join(root, hostExecutable), args: []string{"host"}, env: hostEnv(env)}
}

// advertisesHost reports whether the release manifest advertises kit-host.
// Start runs only on a verified release, so its manifest is authenticated.
func advertisesHost(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Interfaces map[string]string `json:"interfaces"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	version, ok := manifest.Interfaces[hostAdvertisement]
	return ok && strings.HasPrefix(version, "1.")
}

// hostEnv is the group environment without the activation claim, the
// launcher readiness file and the recovery fence. Those belong to the agent
// and the launcher; the host is not part of activation.
func hostEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "FES_SOFTWARE_REQUEST", "FES_SOFTWARE_READY_FILE", "FES_SOFTWARE_RECOVERY_FENCE":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// superviseHostLocked starts the host once the launcher is ready, and
// restarts it alone when it exits, with the launcher's backoff. Its exit
// never touches the agent, the runtime or a running core. The exited
// host's process group is confirmed gone before a replacement starts.
// Called with c.mu held, from RecoverAcceptedExits.
func (c *ProcessController) superviseHostLocked() error {
	if c.active == "" || c.hostPlan == nil {
		return nil
	}
	if c.host != nil {
		select {
		case <-c.host.done:
		default:
			if time.Since(c.host.started) >= launcherStableLife {
				c.hostDelay = launcherRestartMin
			}
			return nil
		}
		if err := c.confirmLauncherGroup(c.host); err != nil {
			return err
		}
	} else if !c.launcherReadyLocked() {
		return nil
	}
	delay := c.hostDelay
	if delay < launcherRestartMin {
		delay = launcherRestartMin
	}
	if c.host != nil && !c.hostRestart.IsZero() && time.Since(c.hostRestart) < delay {
		return nil
	}
	previous := c.host
	if previous != nil {
		log.Printf("restarting host after %s exited: %v", filepath.Base(previous.path), previous.err)
	}
	host, err := c.spawn(c.hostPlan.path, c.hostPlan.args, c.hostPlan.env, roleHost)
	if err != nil {
		return err
	}
	c.host = host
	c.hostRestart = time.Now()
	if previous != nil {
		c.clearReap(previous.cmd.Process.Pid)
		if time.Since(previous.started) < launcherStableLife {
			if c.hostDelay < launcherRestartMin {
				c.hostDelay = launcherRestartMin
			} else if c.hostDelay < launcherRestartMax {
				c.hostDelay *= 2
			}
		} else {
			c.hostDelay = launcherRestartMin
		}
	}
	return nil
}

// launcherReadyLocked reports whether the launcher published readiness for
// the active bundle.
func (c *ProcessController) launcherReadyLocked() bool {
	data, err := os.ReadFile(filepath.Join(c.RunDir, "launcher-ready.json"))
	if err != nil {
		return false
	}
	var ready struct {
		BundleID string `json:"bundle_id"`
	}
	return json.Unmarshal(data, &ready) == nil && ready.BundleID == c.active
}

// withoutHost drops the host from a list of the supervisor's direct
// children, so the service-group proofs see the same three services whether
// or not a host is running or restarting.
func withoutHost(children []int, root string) ([]int, error) {
	host, err := os.Stat(filepath.Join(root, hostExecutable))
	if errors.Is(err, os.ErrNotExist) {
		return children, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(children))
	for _, pid := range children {
		running, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
		if err == nil && os.SameFile(running, host) {
			continue
		}
		out = append(out, pid)
	}
	return out, nil
}
