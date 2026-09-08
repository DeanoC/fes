// fes-update is the operator client for the existing target lease/update API.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/discovery"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "fes-update:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, log io.Writer) error {
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("fes-update", flag.ContinueOnError)
	flags.SetOutput(log)
	configPath := flags.String("config", paths.Config, "existing private FogCast host configuration")
	targetName := flags.String("target", "", "named target (defaults to selected_target)")
	action := flags.String("action", "status", "status, update or rollback")
	releaseDir := flags.String("release", "", "release directory containing release.json and rootfs.img")
	timeout := flags.Duration("timeout", 6*time.Minute, "total upload/reboot/confirmation deadline")
	if err = flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *timeout <= 0 || (*action != "status" && *action != "update" && *action != "rollback") {
		return errors.New("invalid action, deadline or extra arguments")
	}
	config, err := fogcast.LoadConfig(*configPath)
	if err != nil {
		return errors.New("could not read FogCast configuration")
	}
	name := *targetName
	if name == "" {
		name = config.SelectedTarget
	}
	var target fogcast.TargetConfig
	for _, candidate := range config.Targets {
		if candidate.Name == name {
			target = candidate
			break
		}
	}
	if !target.Enabled || target.TargetID == "" {
		return errors.New("selected target must be enabled and have a recorded target_id")
	}
	base, err := url.Parse(target.Address)
	if err != nil {
		return errors.New("invalid target address")
	}
	transport := &http.Client{Timeout: 5 * time.Minute}
	lease := host.NewKitLease(base, target.Agent, transport, "fes-update", "appliance "+*action)
	client := host.NewClient(base, target.Agent, transport).WithKitLease(lease)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lease.Close(cleanup)
	}()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	var result host.ApplianceStatus
	switch *action {
	case "status":
		result, err = client.InspectAppliance(ctx, target.TargetID, discovery.Resolve)
	case "update":
		manifest, image, openErr := openRelease(*releaseDir)
		if openErr != nil {
			return openErr
		}
		defer image.Close()
		fmt.Fprintln(log, "Uploading release; activation will stop the runtime and reboot the kit.")
		result, err = client.UpdateAppliance(ctx, target.TargetID, manifest, image, discovery.Resolve)
	case "rollback":
		fmt.Fprintln(log, "Selecting the previous release; waiting for its new boot and confirmation.")
		result, err = client.RollbackAppliance(ctx, target.TargetID, discovery.Resolve)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

func openRelease(dir string) (appliance.Manifest, *os.File, error) {
	var manifest appliance.Manifest
	if dir == "" {
		return manifest, nil, errors.New("update requires --release directory")
	}
	f, err := os.Open(filepath.Join(dir, "release.json"))
	if err != nil {
		return manifest, nil, err
	}
	manifest, err = appliance.DecodeManifest(f)
	f.Close()
	if err != nil {
		return manifest, nil, err
	}
	image, err := os.Open(filepath.Join(dir, "rootfs.img"))
	if err != nil {
		return manifest, nil, err
	}
	fail := func(err error) (appliance.Manifest, *os.File, error) { image.Close(); return manifest, nil, err }
	info, err := image.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() != manifest.ImageSize {
		return fail(errors.New("release image size/type differs from manifest"))
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, image); err != nil {
		return fail(err)
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != manifest.ImageSHA256 {
		return fail(errors.New("release image SHA-256 differs from manifest"))
	}
	if _, err = image.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return manifest, image, nil
}
