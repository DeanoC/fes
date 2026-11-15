//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
// Host fixture: real tenfoot rendering and the native XRGB copy adapter.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

type memory struct{ data []byte }

func (m memory) Size() uint64 { return uint64(len(m.data)) }
func (m memory) Write(offset uint64, src []byte) error {
	if offset > uint64(len(m.data)) || uint64(len(src)) > uint64(len(m.data))-offset {
		return fmt.Errorf("frame fixture: copy outside window")
	}
	copy(m.data[offset:], src)
	return nil
}
func (memory) Publish() error { return nil }

func run() error {
	output := flag.String("output", "", "fresh host fixture directory")
	stride := flag.Int("stride", 1280*4, "XRGB bytes per row, including padding")
	flag.Parse()
	if *output == "" || *stride < 1280*4 || *stride > 16384 || *stride%16 != 0 {
		return fmt.Errorf("frame fixture: output and bounded aligned stride required")
	}
	if err := os.Mkdir(*output, 0755); err != nil {
		return err
	}
	device, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		return err
	}
	defer device.Close()
	presenter, err := tenfoot.NewPresenter(device)
	if err != nil {
		return err
	}
	defer presenter.Close()
	app := tenfoot.NewApp(nil, 1280, 720, 100)
	window := memory{make([]byte, *stride*720)}
	for i := range window.data {
		window.data[i] = 0xa5 // Padding must never be copied as pixels.
	}
	surface, err := scanout.New(1280, 720, *stride, window)
	if err != nil {
		return err
	}
	var hashes []string
	for index := 0; index < 2; index++ {
		if index == 1 {
			app.HandleCommand(tenfoot.CmdSearch, time.Unix(1, 0))
			app.TypeText("ZX81", time.Unix(1, 0))
		}
		if err := presenter.Draw(app.Snapshot()); err != nil {
			return err
		}
		frame := device.Framebuffer()
		if err := surface.Present(frame); err != nil {
			return err
		}
		for y := 0; y < 720; y++ {
			for x := 0; x < 1280; x++ {
				src := frame.Pix[y*frame.Stride+x*4:]
				dst := window.data[y**stride+x*4:]
				if dst[0] != src[2] || dst[1] != src[1] || dst[2] != src[0] || dst[3] != 255 {
					return fmt.Errorf("frame fixture: native XRGB copy differs at %d,%d", x, y)
				}
			}
			for _, value := range window.data[y**stride+1280*4 : (y+1)**stride] {
				if value != 0xa5 {
					return fmt.Errorf("frame fixture: row padding changed")
				}
			}
		}
		name := filepath.Join(*output, fmt.Sprintf("frame-%d.bin", index))
		if err := os.WriteFile(name, window.data, 0644); err != nil {
			return err
		}
		hash := sha256.Sum256(window.data)
		hashes = append(hashes, hex.EncodeToString(hash[:]))
		preview, err := os.Create(filepath.Join(*output, fmt.Sprintf("frame-%d.png", index)))
		if err != nil {
			return err
		}
		err = png.Encode(preview, frame)
		closeErr := preview.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if hashes[0] == hashes[1] {
		return fmt.Errorf("frame fixture: search did not change the rendered frame")
	}
	report, err := json.MarshalIndent(map[string]any{"width": 1280, "height": 720,
		"row_bytes": 1280 * 4, "stride_bytes": *stride, "sha256": hashes,
		"renderer": "tenfoot.NewApp/NewPresenter", "copy": "scanout.Surface.Present",
		"hardware_access": false}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*output, "frames.json"), append(report, '\n'), 0644)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
