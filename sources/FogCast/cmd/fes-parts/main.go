// fes-parts prepares a private developer composition without contacting a kit.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fes-parts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pkgPath := flags.String("package", "", "sealed format-2 developer .fcore archive")
	videoPath := flags.String("video", "", "video part .fexp archive")
	cardPath := flags.String("expansion", "", "optional Coleco bus-2 .fexp archive")
	output := flags.String("out", "", "private parts transport file to create")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *pkgPath == "" || *videoPath == "" || *output == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: fes-parts -package shell.fcore -video video.fexp [-expansion card.fexp] -out parts.tar")
		return 2
	}
	pkg, err := os.ReadFile(*pkgPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var assets []expansion.Asset
	for _, selected := range []struct{ path, slot string }{
		{*videoPath, expansion.VideoSlot}, {*cardPath, expansion.ColecoSlot},
	} {
		if selected.path == "" {
			continue
		}
		file, err := os.Open(selected.path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		asset, err := expansion.ReadAsset(file)
		file.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if asset.Manifest.Slot != selected.slot && !(selected.slot == expansion.VideoSlot && asset.Manifest.Slot == expansion.NativeVideoSlot) {
			fmt.Fprintf(stderr, "selected part must provide %s\n", selected.slot)
			return 1
		}
		assets = append(assets, asset)
	}
	bundle, err := corepackage.ComposePartsArchive(ctx, pkg, assets)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	transport, err := bundle.Write(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, writeErr := file.Write(transport)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(*output)
		fmt.Fprintln(stderr, "cannot publish developer parts transport")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(bundle.Composition); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }
