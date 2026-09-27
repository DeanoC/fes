// fes-slot-link is a host/kit diagnostic that composes independently built
// slot cards (and optionally a trusted producer ROM map and ROM) onto one
// sealed multi-socket shell, exactly as a library launch does. It does not
// admit packages or program hardware. The composition identity is printed as
// JSON on success.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/DeanoC/misteross/expansion"
)

type cardList []string

func (c *cardList) String() string     { return strings.Join(*c, ",") }
func (c *cardList) Set(v string) error { *c = append(*c, v); return nil }

func boundedRead(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds input limit", path)
	}
	return data, nil
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("fes-slot-link", flag.ContinueOnError)
	shellPath := flags.String("shell", "", "sealed shell core.rbf")
	packageID := flags.String("package-id", "", "sealed shell package ID")
	buildID := flags.String("build-id", "", "sealed shell BUILD_ID")
	slot := flags.String("slot", expansion.Apple2Slot, "shell expansion interface")
	major := flags.Int("slot-major", 1, "shell expansion interface major")
	mapPath := flags.String("map", "", "trusted producer ROM map JSON (optional)")
	romPath := flags.String("rom", "", "exact binary ROM bytes (with -map)")
	outPath := flags.String("output", "", "output RBF (atomically replaced on success)")
	var cards cardList
	flags.Var(&cards, "card", "card expansion archive (repeat for several slots)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *shellPath == "" || *packageID == "" || *buildID == "" || *outPath == "" ||
		(*mapPath == "") != (*romPath == "") || (len(cards) == 0 && *mapPath == "") {
		return errors.New("require -shell, -package-id, -build-id, -output and cards and/or -map with -rom")
	}
	payload, err := boundedRead(*shellPath, 32<<20)
	if err != nil {
		return err
	}
	shell := expansion.Shell{PackageID: *packageID, BuildID: *buildID, Payload: payload,
		Slot: *slot, SlotMajor: *major}
	var assets []expansion.Asset
	for _, path := range cards {
		data, err := boundedRead(path, expansion.MaxArchiveBytes)
		if err != nil {
			return err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		assets = append(assets, asset)
	}
	var composition expansion.SlotComposition
	var linked []byte
	if *mapPath != "" {
		rawMap, err := boundedRead(*mapPath, 32<<20)
		if err != nil {
			return err
		}
		rom, err := boundedRead(*romPath, 256<<10)
		if err != nil {
			return err
		}
		mapping, err := expansion.ParseROMMap(ctx, rawMap, fmt.Sprintf("%x", sha256.Sum256(payload)), len(rom))
		if err != nil {
			return err
		}
		composition, _, linked, err = expansion.ComposeSlotsROM(ctx, shell, assets, mapping, rom)
		if err != nil {
			return err
		}
	} else {
		composition, linked, err = expansion.ComposeSlotsContext(ctx, shell, assets)
		if err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(*outPath), ".fes-slot-link-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(linked); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), *outPath); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(struct {
		Composition expansion.SlotComposition `json:"composition"`
		Programmed  string                    `json:"programmed_sha256"`
	}{composition, fmt.Sprintf("%x", sha256.Sum256(linked))})
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
