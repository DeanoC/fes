// fes-software installs immutable kit software and queues idle activation.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/targetclient"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "fes-software:", err)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string, out, log io.Writer) error {
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("fes-software", flag.ContinueOnError)
	flags.SetOutput(log)
	configPath := flags.String("config", paths.Config, "private FogCast host configuration")
	targetName := flags.String("target", "", "configured target; defaults to selected_target")
	action := flags.String("action", "status", "status, list, install, activate or cancel")
	bundle := flags.String("bundle", "", "bundle tar file for install")
	candidate := flags.String("candidate", "", "installed bundle ID for activation")
	requestID := flags.String("request-id", "", "activation operation ID; generated for activate, required for cancel")
	requester := flags.String("requester", "fes-software", "operator or task requesting activation")
	expected := flags.Uint64("expected-generation", 0, "expected state generation; defaults to one status read")
	timeout := flags.Duration("timeout", 15*time.Minute, "total operation deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		return errors.New("unexpected arguments or invalid timeout")
	}
	switch *action {
	case "status", "list":
		if *bundle != "" || *candidate != "" || *requestID != "" {
			return errors.New("status and list do not accept mutation inputs")
		}
	case "install":
		if *bundle == "" || *candidate != "" || *requestID != "" {
			return errors.New("install requires --bundle only")
		}
	case "activate":
		if *bundle != "" || !software.ValidID(*candidate) {
			return errors.New("activate requires an installed --candidate ID")
		}
		if *requestID == "" {
			var id [32]byte
			if _, err := rand.Read(id[:]); err != nil {
				return err
			}
			*requestID = hex.EncodeToString(id[:])
		}
	case "cancel":
		if *bundle != "" || *candidate != "" || !software.ValidID(*requestID) {
			return errors.New("cancel requires --request-id only")
		}
	default:
		return errors.New("unknown action")
	}
	if (*action == "activate" || *action == "cancel") && !software.ValidID(*requestID) {
		return errors.New("request ID must be 64 lowercase hex characters")
	}
	cfg, err := fogcast.LoadConfig(*configPath)
	if err != nil {
		return errors.New("could not read FogCast configuration")
	}
	name := *targetName
	if name == "" {
		name = cfg.SelectedTarget
	}
	var target fogcast.TargetConfig
	for _, entry := range cfg.Targets {
		if entry.Name == name {
			target = entry
			break
		}
	}
	if !target.Enabled || target.TargetID == "" {
		return errors.New("target must be enabled with a recorded target_id")
	}
	endpoint, err := url.Parse(target.Address)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("invalid target address")
	}
	client := targetclient.NewClient(endpoint, target.Agent, &http.Client{Timeout: *timeout})
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	health, err := client.Health(ctx)
	if err != nil {
		return err
	}
	if health.TargetID != target.TargetID || health.APIVersion != "v1" {
		return errors.New("configured target identity or API contract does not match")
	}
	var result any
	switch *action {
	case "status":
		result, err = client.KitSoftwareStatus(ctx)
	case "list":
		result, err = client.KitSoftwareReleases(ctx)
	case "install":
		var file *os.File
		file, err = os.Open(*bundle)
		if err != nil {
			return err
		}
		defer file.Close()
		info, statErr := file.Stat()
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > software.MaxBundleSize {
			return errors.New("bundle must be a bounded regular tar file")
		}
		release, inspectErr := software.InspectBundle(file)
		if inspectErr != nil {
			return inspectErr
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		result, err = client.InstallKitSoftware(ctx, info.Size(), file, release.ID)
	case "activate", "cancel":
		provided := false
		flags.Visit(func(flag *flag.Flag) {
			if flag.Name == "expected-generation" {
				provided = true
			}
		})
		if !provided {
			status, statusErr := client.KitSoftwareStatus(ctx)
			if statusErr != nil {
				return statusErr
			}
			*expected = status.Generation
		}
		fmt.Fprintf(log, "software operation %s; expected generation %d\n", *requestID, *expected)
		if *action == "activate" {
			result, err = client.QueueKitSoftware(ctx, softwarecontrol.Request{ID: *requestID, Candidate: *candidate, Requester: *requester, Generation: *expected})
		} else {
			result, err = client.CancelKitSoftware(ctx, softwarecontrol.CancelRequest{ID: *requestID, Generation: *expected})
		}
	}
	if err != nil {
		return fmt.Errorf("%w; inspect --action status and --action list before repeating a mutation", err)
	}
	return json.NewEncoder(out).Encode(result)
}
