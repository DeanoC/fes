package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/DeanoC/fes/platform/internal/kitsoftware"
)

func acknowledge(args []string) error {
	request, err := operatorRequest("acknowledge", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := kitsoftware.AcknowledgeRunningPrior(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func preparePowerCycle(args []string) error {
	request, err := operatorRequest("prepare-power-cycle", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := kitsoftware.PreparePowerCycle(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func selectFactory(args []string) error {
	request, err := operatorRequest("select-factory", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := kitsoftware.SelectFactory(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func operatorRequest(command string, args []string) (kitsoftware.AcknowledgeRequest, error) {
	var request kitsoftware.AcknowledgeRequest
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	operation := flags.String("operation", "", "exact blocked software operation ID")
	generation := flags.String("generation", "", "expected software selection generation")
	selected := flags.String("selected", "", "expected committed previous bundle ID")
	base := flags.String("base", "", "expected running base image SHA-256")
	boot := flags.String("boot", "", "expected current boot ID")
	action := flags.String("action", "", "fresh operator action ID (64 lowercase hex)")
	if err := flags.Parse(args); err != nil {
		return request, err
	}
	if flags.NArg() != 0 || *generation == "" || *operation == "" || *selected == "" || *base == "" || *boot == "" || *action == "" {
		return request, errors.New(command + " requires --operation, --generation, --selected, --base, --boot and --action; no positional arguments")
	}
	value, err := strconv.ParseUint(*generation, 10, 64)
	if err != nil {
		return request, errors.New(command + " generation must be an unsigned decimal integer")
	}
	return kitsoftware.AcknowledgeRequest{
		Operation: *operation, Generation: value, Selected: *selected,
		Base: *base, Boot: *boot, Action: *action,
	}, nil
}
