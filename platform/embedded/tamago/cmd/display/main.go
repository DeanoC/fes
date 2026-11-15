//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
	"image"
	"time"
)

func pattern(frame *image.RGBA, second bool) {
	colors := [][3]byte{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 255}}
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			rgb := colors[x/320]
			if second {
				rgb = [3]byte{255 - rgb[0], 255 - rgb[1], 255 - rgb[2]}
			}
			i := y*frame.Stride + x*4
			frame.Pix[i] = rgb[0]
			frame.Pix[i+1] = rgb[1]
			frame.Pix[i+2] = rgb[2]
			frame.Pix[i+3] = 255
		}
	}
}
func main() {
	memory, err := board.InitDevices()
	if err != nil {
		panic(err)
	}
	screen, err := board.OpenDisplay(1280, 720, memory)
	if err != nil {
		panic(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	pattern(frame, false)
	if err = screen.Present(frame); err != nil {
		panic(err)
	}
	fmt.Printf("FES_DISPLAY_PROBE_READY frame=1 board=%s lifetime=boot drain=unproven\n", board.Name)
	// Keep frame 1 stable for a screenshot or physical inspection; 'n' requests
	// frame 2. No storage, network, USB or FPGA programming is needed.
	for {
		value, ready := serial.Read()
		if ready && value == 'n' {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pattern(frame, true)
	if err = screen.Present(frame); err != nil {
		panic(err)
	}
	fmt.Println("FES_DISPLAY_PROBE PASS frame=2 lifetime=boot drain=unproven")
}
