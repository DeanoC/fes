//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/framefixture"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// This initial cold menu uses the actual presenter and independent secure
// closure. The boot-pinned arena and failed transfers stay rooted until reboot.
// It does not resume SD or claim a complete menu/core/menu application cycle.
func presentMenuFrames(v *nativefabric.HPSContextVerifier, control, access []byte) (err error) {
	ctx := context.Background()
	limits := scanout.ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: func() { time.Sleep(time.Millisecond) }}
	started := time.Now()
	step := func(index int, phase string) {
		fmt.Printf("FES_NATIVE_FPGA_MENU_PRESENT STEP index=%d phase=%s elapsed_us=%d\n", index+1, phase, time.Since(started)/time.Microsecond)
	}
	step(0, "open_begin")
	session, err := v.OpenMenuSession(ctx, control, access, retained, limits)
	if err != nil {
		return err // OpenMenuSession already attempts its known independent closure.
	}
	step(0, "open_end")
	var last scanout.MenuFrame
	defer func() {
		step(3, "return_begin")
		returned, closeErr := session.Close()
		err = errors.Join(err, closeErr)
		if err != nil {
			// The error and closure carry cached state; do not read a failed CSR.
			var failure *scanout.MenuFailure
			if errors.As(err, &failure) {
				fmt.Printf("FES_NATIVE_FPGA_MENU_PRESENT FAILURE operation=%s stage=%s polls=%d flags=%x copied=%d attempted=%t memory_retained=1\n",
					failure.Operation, failure.Stage, failure.Polls, failure.Flags, failure.CopiedBytes, failure.CommandAttempted)
			}
			return
		}
		if returned.Frames != 3 || returned.LastFrame != last || returned.Memory.ArenaBytes != scanout.MenuArenaBytes {
			err = errors.New("native menu: incomplete frame/return result")
			return
		}
		step(3, "return_end")
		id := v.Identity()
		fmt.Printf("FES_NATIVE_FPGA_MENU_PRESENT PASS package_id=%s build_id=%s owner=%x source_generation=%d closed_generation=%d frames=%d last_sequence=%d last_slot=%d access_map_sha256=%x data_closed=1 memory_retained=1 runtime_admission=unavailable\n",
			id.PackageID, id.BuildID, returned.After.Owner, returned.Source.Bridges.Generation, returned.After.Generation,
			returned.Frames, last.Sequence, last.Slot, returned.AccessMapSHA256)
	}()
	err = framefixture.Render(3, func(index int, frame *image.RGBA) error {
		observed, err := session.PresentFrame(ctx, frame)
		if err != nil {
			return err
		}
		last = observed
		id := v.Identity()
		fmt.Printf("FES_NATIVE_FPGA_MENU_PRESENT FRAME index=%d package_id=%s build_id=%s reader_generation=%d sequence=%d submitted=%d slot=%d underflows=%d memory_retained=1\n",
			index+1, id.PackageID, id.BuildID, observed.Generation, observed.Sequence, observed.Submitted, observed.Slot, observed.Underflows)
		return nil
	}, step)
	return err
}
