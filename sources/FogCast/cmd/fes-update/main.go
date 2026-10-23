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
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/ext4image"
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
	var imageBase software.Compatibility
	if *action == "update" {
		manifest, image, err = openRelease(*releaseDir, log)
		if err != nil {
			return err
		}
		defer image.Close()
		if imageBase, err = imageSoftwareBase(image); err != nil {
			return err
		}
	} else if *action == "confirm" && *releaseDir != "" {
		manifest, err = readReleaseManifest(*releaseDir, log)
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
		if err = requireSelectedSoftware(ctx, client, target.TargetID, imageBase, log); err == nil {
			result, err = client.UpdateAppliance(ctx, target.TargetID, manifest, image, discovery.Resolve)
		}
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

// imageSoftwareBase reads the software base record the image's supervisor
// enforces. openRelease has already matched the image to its manifest.
func imageSoftwareBase(image io.ReaderAt) (software.Compatibility, error) {
	var base software.Compatibility
	raw, err := ext4image.ReadFile(image, "/etc/fes/software-base.json", 64<<10)
	if err == nil {
		err = json.Unmarshal(raw, &base)
	}
	if err != nil {
		return base, fmt.Errorf("release image software base: %w", err)
	}
	return base, nil
}

// requireSelectedSoftware refuses, before anything is uploaded, an image whose
// menu interfaces the kit's selected software bundle does not accept. The
// image would hold that bundle back and run its own factory software, so a
// bundle that accepts them has to go first.
func requireSelectedSoftware(ctx context.Context, client *targetclient.Client, targetID string, base software.Compatibility, log io.Writer) error {
	if _, err := client.InspectAppliance(ctx, targetID, discovery.Resolve); err != nil {
		return err
	}
	status, err := client.KitSoftwareStatus(ctx)
	if err != nil {
		return fmt.Errorf("read the kit's selected software: %w", err)
	}
	releases, err := client.KitSoftwareReleases(ctx)
	if err != nil {
		return fmt.Errorf("read the kit's software releases: %w", err)
	}
	notice, err := selectedSoftwareRuns(base, status, releases)
	if notice != "" {
		fmt.Fprintf(log, "%s %s\n", time.Now().Local().Format(time.RFC3339), notice)
	}
	return err
}

// selectedSoftwareRuns checks the selected bundle against the image's base
// record. A bundle built for another base cannot be activated before the
// image, so that update proceeds with a notice and the image runs its factory
// software until a bundle built for it is activated.
func selectedSoftwareRuns(base software.Compatibility, status softwarecontrol.Status, releases []software.Release) (string, error) {
	if status.Selected == "" {
		return "kit reports no software selection; the image runs its own factory software", nil
	}
	for _, release := range releases {
		if release.ID != status.Selected {
			continue
		}
		contract := base
		contract.Requires = nil
		if err := release.Manifest.Compatible(contract); err != nil {
			if !errors.Is(err, software.ErrIncompatible) {
				return "", err
			}
			return fmt.Sprintf("selected kit software %s was built for another base (%v); the image holds it back and runs its own factory software until a bundle built for the image is activated", status.Selected, err), nil
		}
		if err := release.Manifest.Accepts(base.Requires); err != nil {
			return "", fmt.Errorf("selected kit software %s cannot run this image (%w); install and activate a bundle that accepts it, then update the image", status.Selected, err)
		}
		return "", nil
	}
	return "", fmt.Errorf("selected kit software %s is not in the kit's release inventory", status.Selected)
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

func readReleaseManifest(dir string, log io.Writer) (appliance.Manifest, error) {
	f, err := os.Open(filepath.Join(dir, "release.json"))
	if err != nil {
		return appliance.Manifest{}, err
	}
	defer f.Close()
	manifest, ignored, err := appliance.DecodeManifest(f)
	if err != nil {
		return manifest, err
	}
	if len(ignored) > 0 && log != nil {
		fmt.Fprintf(log, "ignoring unknown release manifest fields: %s (manifest_format %s)\n", strings.Join(ignored, ","), manifest.ManifestFormat)
	}
	return manifest, nil
}

func openRelease(dir string, log io.Writer) (appliance.Manifest, *os.File, error) {
	var manifest appliance.Manifest
	if dir == "" {
		return manifest, nil, errors.New("update requires --release directory")
	}
	manifest, err := readReleaseManifest(dir, log)
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
