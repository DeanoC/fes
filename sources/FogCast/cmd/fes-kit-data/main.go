// fes-kit-data is part of the base image, not the replaceable software bundle.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/appliancedata"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fes-kit-data status|mount|prepare")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "identity":
		var identity applianceupdate.BootIdentity
		identity, err = applianceupdate.ReadBootIdentity(appliance.DefaultRoot+"/boot.json", "/proc/sys/kernel/random/boot_id")
		if errors.Is(err, os.ErrNotExist) {
			if _, factoryErr := os.Stat("/.fes-bootstrap/etc/fes/factory.json"); errors.Is(factoryErr, os.ErrNotExist) {
				_, err = fmt.Fprintln(os.Stdout, "direct-root")
			}
		} else if err == nil {
			_, err = fmt.Fprintln(os.Stdout, identity.ImageSHA256)
		}
	case "status":
		var gate appliancedata.Gate
		gate, err = appliancedata.ReadProductionUpdateGate()
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(struct {
				Trial   bool `json:"trial"`
				Pending bool `json:"pending"`
				Corrupt bool `json:"corrupt"`
			}{gate.Trial, gate.Pending, gate.Corrupt})
		}
	case "mount", "prepare":
		var result appliancedata.Result
		cfg := appliancedata.ProductionConfig()
		if os.Args[1] == "mount" {
			result, err = appliancedata.Mount(cfg)
		} else {
			result, err = appliancedata.Prepare(cfg)
		}
		if err == nil && result.Skipped != "" {
			fmt.Fprintf(os.Stderr, "data unavailable: %s\n", result.Skipped)
			switch result.Skipped {
			case appliancedata.SkipAbsent:
				os.Exit(3)
			case appliancedata.SkipTrial, appliancedata.SkipPending:
				os.Exit(4)
			default:
				os.Exit(1)
			}
		}
	default:
		err = fmt.Errorf("unknown operation %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
