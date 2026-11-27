//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
// stage-installer writes a fresh host staging directory, never a block device.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

type selections []string

func (s *selections) String() string      { return fmt.Sprint([]string(*s)) }
func (s *selections) Set(id string) error { *s = append(*s, id); return nil }
func run() error {
	var ids selections
	export := flag.String("export", "", "Nano export directory")
	menu := flag.String("menu-export", "", "optional single-menu Nano export; prepare fresh launcher files and preallocated preferences")
	agentConfig := flag.String("agent-config", "", "private JSON file with this launcher's FogCast target_id, token and port")
	output := flag.String("output", "", "new staging directory (must not exist)")
	flag.Var(&ids, "id", "export entry ID to offer; repeat for multiple packages")
	flag.Parse()
	if *export == "" || *output == "" || len(ids) == 0 || flag.NArg() != 0 {
		return fmt.Errorf("--export, --output and at least one --id are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	var files map[string][]byte
	var err error
	if *menu != "" {
		files, err = nativefabric.BuildLauncherStaging(ctx, os.DirFS(*export), os.DirFS(*menu), ids)
	} else {
		files, err = nativefabric.BuildStagedCatalog(ctx, os.DirFS(*export), ids)
	}
	if err != nil {
		return err
	}
	if *agentConfig != "" {
		if *menu == "" {
			return fmt.Errorf("--agent-config requires --menu-export")
		}
		info, err := os.Stat(*agentConfig)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("agent configuration must be a regular file")
		}
		src, err := os.Open(*agentConfig)
		if err != nil {
			return err
		}
		defer src.Close()
		opened, err := src.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return fmt.Errorf("agent configuration changed while opening")
		}
		raw, err := io.ReadAll(io.LimitReader(src, agentapi.MaxProvisioningBytes+1))
		if err != nil {
			return err
		}
		if _, err := agentapi.ParseProvisioning(raw); err != nil {
			return fmt.Errorf("invalid FogCast agent provisioning")
		}
		files[agentapi.ProvisioningPath] = raw
	}
	if err = os.Mkdir(*output, 0700); err != nil {
		return err
	}
	var names []string
	for name := range files {
		if name != "FES/INSTALL.JSN" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	names = append(names, "FES/INSTALL.JSN")
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(*output, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(files[name])
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Printf("Prepared %d staged packages; no install or hardware action\n", len(ids))
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
