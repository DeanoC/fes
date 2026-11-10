package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/catalogcheckpoint"
)

// catalogMain is an offline helper for the software supervisor. The caller
// proves the host process group stopped before invoking any database command.
func catalogMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "fogcast-kit catalog: expected schema, inspect, checkpoint or restore")
		return 2
	}
	if args[0] == "schema" && len(args) == 1 {
		if err := json.NewEncoder(stdout).Encode(struct {
			Schema int `json:"schema"`
		}{catalog.SupportedSchemaVersion}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet("fogcast-kit catalog "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	database := flags.String("database", "", "stopped host's absolute catalog path")
	var snapshot, retain string
	expected := -1
	switch args[0] {
	case "inspect":
	case "checkpoint":
		flags.StringVar(&snapshot, "snapshot", "", "new absolute snapshot directory")
	case "restore":
		flags.StringVar(&snapshot, "snapshot", "", "absolute prior snapshot directory")
		flags.StringVar(&retain, "retain", "", "absolute retained current-catalog directory")
		flags.IntVar(&expected, "expected-current-schema", -1, "expected source schema")
	default:
		fmt.Fprintln(stderr, "fogcast-kit catalog: unknown command")
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *database == "" || (args[0] != "inspect" && snapshot == "") || (args[0] == "restore" && (retain == "" || expected < 0)) {
		fmt.Fprintln(stderr, "fogcast-kit catalog: missing or invalid arguments")
		return 2
	}
	var result catalogcheckpoint.Metadata
	var err error
	switch args[0] {
	case "inspect":
		result, err = catalogcheckpoint.Inspect(ctx, *database)
	case "checkpoint":
		result, err = catalogcheckpoint.Checkpoint(ctx, *database, snapshot)
	case "restore":
		result, err = catalogcheckpoint.Restore(ctx, *database, snapshot, retain, expected)
	}
	if err != nil {
		fmt.Fprintf(stderr, "fogcast-kit catalog %s: %v\n", args[0], err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
