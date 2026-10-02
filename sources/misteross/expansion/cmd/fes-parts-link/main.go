// fes-parts-link is the host-only Coleco video-part composition diagnostic.
// It selects prebuilt exact-shell assets; it neither compiles nor loads hardware.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/DeanoC/misteross/expansion"
)

func boundedRead(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular non-symlink file", path)
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

// Directories use exactly the same two canonical members as the archive.
func readPart(path string) (expansion.Asset, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return expansion.Asset{}, err
	}
	if !info.IsDir() {
		data, err := boundedRead(path, expansion.MaxArchiveBytes)
		if err != nil {
			return expansion.Asset{}, err
		}
		return expansion.ReadAsset(bytes.NewReader(data))
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return expansion.Asset{}, err
	}
	if len(entries) != 2 || entries[0].Name() != "cart.rbf" || entries[1].Name() != "manifest.json" {
		return expansion.Asset{}, errors.New("part directory requires exactly cart.rbf and manifest.json")
	}
	manifestBytes, err := boundedRead(filepath.Join(path, "manifest.json"), expansion.MaxManifestBytes)
	if err != nil {
		return expansion.Asset{}, err
	}
	cart, err := boundedRead(filepath.Join(path, "cart.rbf"), 32<<20)
	if err != nil {
		return expansion.Asset{}, err
	}
	var manifest expansion.Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return expansion.Asset{}, err
	}
	asset, err := expansion.NewAsset(manifest, cart)
	if err != nil {
		return expansion.Asset{}, err
	}
	if !bytes.Equal(asset.ManifestBytes, manifestBytes) {
		return expansion.Asset{}, errors.New("part directory manifest is not canonical")
	}
	return asset, nil
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("fes-parts-link", flag.ContinueOnError)
	shellPath := flags.String("shell", "", "exact frozen developer shell core.rbf")
	packageID := flags.String("package-id", "", "sealed shell package ID")
	buildID := flags.String("build-id", "", "sealed shell BUILD_ID")
	videoPath := flags.String("video", "", "video part archive or two-member directory")
	expansionPath := flags.String("expansion", "", "optional Coleco v2 CPU expansion archive or directory")
	mapPath := flags.String("map", "", "trusted sealed producer ROM map JSON (optional)")
	romPath := flags.String("rom", "", "exact binary ROM bytes (with -map)")
	outPath := flags.String("output", "", "output RBF, replaced only after successful composition")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *shellPath == "" || *packageID == "" || *buildID == "" ||
		*videoPath == "" || *outPath == "" || (*mapPath == "") != (*romPath == "") {
		return errors.New("require -shell, -package-id, -build-id, -video, -output and paired -map/-rom when used")
	}
	payload, err := boundedRead(*shellPath, 32<<20)
	if err != nil {
		return err
	}
	shell := expansion.PartsShell{PackageID: *packageID, BuildID: *buildID, Payload: payload,
		Layout: expansion.ColecoVideoLayout}
	video, err := readPart(*videoPath)
	if err != nil {
		return fmt.Errorf("video: %w", err)
	}
	if video.Manifest.Slot != expansion.VideoSlot {
		return errors.New("-video requires a video part, not a CPU expansion")
	}
	assets := []expansion.Asset{video}
	if *expansionPath != "" {
		card, err := readPart(*expansionPath)
		if err != nil {
			return fmt.Errorf("expansion: %w", err)
		}
		if card.Manifest.Slot != expansion.ColecoSlot {
			return errors.New("-expansion requires a Coleco CPU expansion")
		}
		assets = append(assets, card)
	}
	var composition expansion.PartsComposition
	var linked []byte
	if *mapPath == "" {
		composition, linked, err = expansion.ComposePartsContext(ctx, shell, assets)
	} else {
		rawMap, readErr := boundedRead(*mapPath, 32<<20)
		if readErr != nil {
			return readErr
		}
		rom, readErr := boundedRead(*romPath, 256<<10)
		if readErr != nil {
			return readErr
		}
		mapping, mapErr := expansion.ParseROMMap(ctx, rawMap, video.Manifest.ShellSHA256, len(rom))
		if mapErr != nil {
			return mapErr
		}
		composition, _, linked, err = expansion.ComposePartsROM(ctx, shell, assets, mapping, rom)
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(*outPath), ".fes-parts-link-*")
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
		Composition expansion.PartsComposition `json:"composition"`
		Programmed  string                     `json:"programmed_sha256"`
	}{composition, composition.PayloadSHA256})
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
