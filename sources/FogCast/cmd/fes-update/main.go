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
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/targetclient"
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
	action := flags.String("action", "status", "status, update, rollback or confirm")
	releaseDir := flags.String("release", "", "release directory (release.json; rootfs.img required for update)")
	imageSHA := flags.String("image-sha256", "", "expected image SHA-256 for confirm")
	timeout := flags.Duration("timeout", 0, "explicit total deadline (default depends on action and image size)")
	if err = flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*action != "status" && *action != "update" && *action != "rollback" && *action != "confirm") {
		return errors.New("invalid action, deadline or extra arguments")
	}
	explicitTimeout := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "timeout" {
			explicitTimeout = true
		}
	})
	if explicitTimeout && *timeout <= 0 {
		return errors.New("--timeout must be greater than zero")
	}
	if err := validateActionFlags(*action, *releaseDir, *imageSHA); err != nil {
		return err
	}
	var manifest appliance.Manifest
	var image *os.File
	if *action == "update" {
		manifest, image, err = openRelease(*releaseDir)
		if err != nil {
			return err
		}
		defer image.Close()
	} else if *action == "confirm" && *releaseDir != "" {
		manifest, err = readReleaseManifest(*releaseDir)
		if err != nil {
			return err
		}
		*imageSHA = manifest.ImageSHA256
	}
	deadline, basis := chooseDeadline(*action, explicitTimeout, *timeout, manifest.ImageSize)
	fmt.Fprintf(log, "%s deadline %s (%s)\n", time.Now().Local().Format(time.RFC3339), deadline, basis)
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
	transport := &http.Client{Timeout: deadline}
	lease := targetclient.NewKitLease(base, target.Agent, transport, "fes-update", "appliance "+*action)
	lastPhase := "starting"
	client := targetclient.NewClient(base, target.Agent, transport).WithKitLease(lease).WithProgress(func(phase, detail string) {
		lastPhase = phase
		fmt.Fprintf(log, "%s %s: %s\n", time.Now().Local().Format(time.RFC3339), phase, detail)
	})
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lease.Close(cleanup)
	}()
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	var result targetclient.ApplianceStatus
	switch *action {
	case "status":
		result, err = client.InspectAppliance(ctx, target.TargetID, discovery.Resolve)
	case "update":
		result, err = client.UpdateAppliance(ctx, target.TargetID, manifest, image, discovery.Resolve)
	case "rollback":
		result, err = client.RollbackAppliance(ctx, target.TargetID, discovery.Resolve)
	case "confirm":
		result, err = client.ConfirmApplianceTrial(ctx, target.TargetID, *imageSHA, discovery.Resolve)
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fmt.Fprintf(log, "%s deadline expired; last phase: %s; run fes-update --action confirm with --release or --image-sha256\n", time.Now().Local().Format(time.RFC3339), lastPhase)
		}
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

func validateActionFlags(action, releaseDir, imageSHA string) error {
	if action == "confirm" {
		if (releaseDir == "") == (imageSHA == "") {
			return errors.New("confirm requires exactly one of --release or --image-sha256")
		}
		if imageSHA != "" && !appliance.ValidHash(imageSHA) {
			return errors.New("--image-sha256 must be 64 lowercase hex characters")
		}
	} else if imageSHA != "" {
		return errors.New("--image-sha256 requires --action confirm")
	}
	if action == "update" && releaseDir == "" {
		return errors.New("update requires --release directory")
	}
	return nil
}

func chooseDeadline(action string, explicit bool, timeout time.Duration, imageSize int64) (time.Duration, string) {
	if explicit {
		return timeout, "explicit --timeout"
	}
	switch action {
	case "update":
		mib := (imageSize + (1 << 20) - 1) / (1 << 20)
		return ((8*time.Minute + time.Duration(mib)*6*time.Second + time.Minute - 1) / time.Minute) * time.Minute, fmt.Sprintf("8m activation/reboot/confirm + 6s per MiB (%d MiB)", mib)
	case "rollback":
		return 10 * time.Minute, "rollback default"
	case "confirm":
		return 15 * time.Minute, "confirm default"
	default:
		return time.Minute, "status default"
	}
}

func readReleaseManifest(dir string) (appliance.Manifest, error) {
	f, err := os.Open(filepath.Join(dir, "release.json"))
	if err != nil {
		return appliance.Manifest{}, err
	}
	defer f.Close()
	return appliance.DecodeManifest(f)
}

func openRelease(dir string) (appliance.Manifest, *os.File, error) {
	var manifest appliance.Manifest
	if dir == "" {
		return manifest, nil, errors.New("update requires --release directory")
	}
	manifest, err := readReleaseManifest(dir)
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
