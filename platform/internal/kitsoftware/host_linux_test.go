package kitsoftware

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeManifest(t *testing.T, root, interfaces string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"interfaces":{`+interfaces+`}}`), 0o444); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyABundleAdvertisingKitHostGetsAHost(t *testing.T) {
	for interfaces, want := range map[string]bool{
		`"runtime-protocol":"2","kit-host":"1.0"`: true,
		`"kit-host":"1.3"`:                        true,
		`"runtime-protocol":"2"`:                  false,
		`"kit-host":"2.0"`:                        false,
	} {
		root := t.TempDir()
		writeManifest(t, root, interfaces)
		c := &ProcessController{}
		c.planHostLocked(root, []string{"FES_SOFTWARE_MANAGED=1"})
		if got := c.hostPlan != nil; got != want {
			t.Fatalf("%s: host planned %v, want %v", interfaces, got, want)
		}
	}
	c := &ProcessController{}
	c.planHostLocked(t.TempDir(), nil)
	if c.hostPlan != nil {
		t.Fatal("planned a host without a manifest")
	}
}

func TestHostEnvironmentLeavesOutActivationAndReadiness(t *testing.T) {
	got := hostEnv([]string{"FES_SOFTWARE_MANAGED=1", "FES_SOFTWARE_BUNDLE_ID=b", "FES_SOFTWARE_READY_FILE=/r", "FES_SOFTWARE_REQUEST={}", "FES_SOFTWARE_RECOVERY_FENCE=1", "XDG_STATE_HOME=/s"})
	if strings.Join(got, " ") != "FES_SOFTWARE_MANAGED=1 FES_SOFTWARE_BUNDLE_ID=b XDG_STATE_HOME=/s" {
		t.Fatalf("env %q", got)
	}
}

func publishLauncherReady(t *testing.T, c *ProcessController, bundle string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(c.RunDir, "launcher-ready.json"), []byte(`{"bundle_id":"`+bundle+`","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The host starts once the launcher is ready and restarts alone; the agent,
// runtime and launcher keep running.
func TestHostStartsAfterLauncherReadinessAndRestartsAlone(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer func() { _ = c.Stop(context.Background()) }()
	startSleepTrio(t, c)
	c.hostPlan = &hostPlan{path: "/bin/sleep", args: []string{"60"}}
	trio := []int{c.children[0].cmd.Process.Pid, c.children[1].cmd.Process.Pid, c.children[2].cmd.Process.Pid}
	if err := c.RecoverAcceptedExits(context.Background(), Request{}, true); err != nil {
		t.Fatal(err)
	}
	if c.host != nil {
		t.Fatal("host started before the launcher was ready")
	}
	publishLauncherReady(t, c, "other")
	if err := c.RecoverAcceptedExits(context.Background(), Request{}, true); err != nil || c.host != nil {
		t.Fatalf("host started for another bundle's readiness: %v", err)
	}
	publishLauncherReady(t, c, "accepted")
	if err := c.RecoverAcceptedExits(context.Background(), Request{}, true); err != nil || c.host == nil {
		t.Fatalf("host not started after readiness: %v", err)
	}
	first := c.host
	if err := first.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-first.done
	if !strings.Contains(c.ServiceFault(), "restarting the host") {
		t.Fatal(c.ServiceFault())
	}
	deadline := time.Now().Add(5 * time.Second)
	for c.host == first && time.Now().Before(deadline) {
		if err := c.RecoverAcceptedExits(context.Background(), Request{}, true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if c.host == first {
		t.Fatal("host was not restarted")
	}
	assertProcessGone(t, first.cmd.Process.Pid)
	if len(c.children) != 3 {
		t.Fatalf("service group has %d members", len(c.children))
	}
	for i, pid := range trio {
		if c.children[i].cmd.Process.Pid != pid {
			t.Fatalf("service %d was replaced by a host restart", i)
		}
	}
	host := c.host.cmd.Process.Pid
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertProcessGone(t, host)
	if c.host != nil || c.hostPlan != nil {
		t.Fatal("Stop kept the host")
	}
}

// A bundle without kit-host keeps today's three services.
func TestNoHostWithoutTheAdvertisement(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer func() { _ = c.Stop(context.Background()) }()
	startSleepTrio(t, c)
	publishLauncherReady(t, c, "accepted")
	if err := c.RecoverAcceptedExits(context.Background(), Request{}, true); err != nil || c.host != nil {
		t.Fatalf("host %v err %v", c.host, err)
	}
}

// The launcher and the host stop together, so four services that each use
// the whole SIGTERM grace still fit three stop slots.
func TestStopStopsTheHostAlongsideTheLauncher(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the stop grace")
	}
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	ignoreTerm := []string{"-c", "trap '' TERM; exec sleep 60"}
	for _, role := range []string{roleRuntime, roleAgent} {
		if err := c.start("/bin/sleep", []string{"60"}, nil, role); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.start("/bin/sh", ignoreTerm, nil, roleLauncher); err != nil {
		t.Fatal(err)
	}
	host, err := c.spawn("/bin/sh", ignoreTerm, nil, roleHost)
	if err != nil {
		t.Fatal(err)
	}
	c.host = host
	time.Sleep(100 * time.Millisecond)
	started := time.Now()
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > StopTermGrace+StopKillGrace+time.Second {
		t.Fatalf("stop took %s; the host was stopped after the launcher", elapsed)
	}
	assertProcessGone(t, host.cmd.Process.Pid)
}

func TestWithoutHostDropsOnlyTheHostExecutable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	sleep, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, hostExecutable), sleep, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &ProcessController{}
	host, err := c.spawn(filepath.Join(root, hostExecutable), []string{"60"}, nil, roleHost)
	if err != nil {
		t.Fatal(err)
	}
	service, err := c.spawn("/bin/sleep", []string{"60"}, nil, roleAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range []*child{host, service} {
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	}()
	got, err := withoutHost([]int{host.cmd.Process.Pid, service.cmd.Process.Pid}, root)
	if err != nil || len(got) != 1 || got[0] != service.cmd.Process.Pid {
		t.Fatalf("withoutHost = %v %v", got, err)
	}
	if got, err := withoutHost([]int{service.cmd.Process.Pid}, t.TempDir()); err != nil || len(got) != 1 {
		t.Fatalf("release without a host: %v %v", got, err)
	}
}
