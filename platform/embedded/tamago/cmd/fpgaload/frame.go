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

// The renderer publishes only to private CPU storage. The later boot-pinned
// copy still grants no FPGA data access or permission to recycle the arena.
type windowPixels []byte

func (p windowPixels) Size() uint64 { return uint64(len(p)) }
func (p windowPixels) Write(offset uint64, src []byte) error {
	if offset > uint64(len(p)) || uint64(len(src)) > uint64(len(p))-offset {
		return errors.New("window frame copy exceeds private pixels")
	}
	copy(p[offset:], src)
	return nil
}
func (windowPixels) Publish() error { return nil }

func windowFrame() ([]byte, error) {
	pixels := make(windowPixels, 5120*720)
	surface, err := scanout.New(1280, 720, 5120, pixels)
	if err != nil {
		return nil, err
	}
	if err := framefixture.Render(1, func(_ int, frame *image.RGBA) error { return surface.Present(frame) }, nil); err != nil {
		return nil, err
	}
	return pixels, nil
}

// Report the driver's cached failure; diagnostics must not read a failed or
// reset aperture after session.Close has attempted global containment.
func reportWindowFailure(err error) {
	var failure *scanout.ReaderFailure
	if errors.As(err, &failure) {
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_FAILURE operation=%s stage=%s limit=%s polls=%d reads=%d writes=%d start_attempted=%t snapshot_complete=%t sample_poll=%d flags=%x sample_generation=%d sample_cookie=%d requested=%d delivered=%d crc32=%08x elapsed_us=%d memory_retained=1 runtime_admission=unavailable\n",
			failure.Operation, failure.Stage, failure.Limit, failure.Polls, failure.Reads, failure.Writes, failure.StartAttempted,
			failure.SnapshotComplete, failure.SamplePoll, failure.Flags, failure.Sample.Generation, failure.Sample.Cookie,
			failure.Sample.RequestedBytes, failure.Sample.DeliveredBytes, failure.Sample.CRC32, failure.Elapsed/time.Microsecond)
	}
}

func presentWindowFrames(v *nativefabric.HPSContextVerifier, control, contract []byte, limits scanout.ReaderLimits) (*nativefabric.WindowSession, error) {
	ctx := context.Background()
	session, err := v.OpenWindowSession(ctx, control, contract, retained, limits)
	if err != nil {
		return nil, err
	}
	seen := map[uint32]bool{}
	var last nativefabric.WindowPresentation
	started := time.Now()
	step := func(index int, phase string) {
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_PRESENT STEP index=%d phase=%s elapsed_us=%d\n", index+1, phase, time.Since(started)/time.Microsecond)
	}
	err = framefixture.Render(12, func(index int, frame *image.RGBA) error {
		observed, e := session.PresentFrame(ctx, frame)
		if e != nil {
			return e
		}
		if seen[observed.Frame.CRC32] {
			return errors.New("native window presentation did not change pixels")
		}
		seen[observed.Frame.CRC32] = true
		last = observed
		id := v.Identity()
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_PRESENT FRAME index=%d package_id=%s build_id=%s owner=%x source_generation=%d reader_generation=%d cookie=%d bytes=%d published_crc32=%08x read_crc32=%08x frame_sha256=%x drain_generation=%d drain_cookie=%d memory_retained=1\n",
			index+1, id.PackageID, id.BuildID, observed.Memory.Source.Bridges.Owner, observed.Memory.Source.Bridges.Generation,
			observed.Frame.Generation, observed.Frame.Cookie, observed.Frame.DeliveredBytes, observed.Memory.CRC32, observed.Frame.CRC32,
			observed.Memory.FrameSHA256, observed.Drain.Generation, observed.Drain.Cookie)
		return nil
	}, step)
	step(12, "return_begin")
	returned, closeErr := session.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return nil, err
	}
	if returned.Frames != 12 || returned.LastFrame != last.Frame || returned.Memory != last.Memory {
		return nil, errors.New("native window presentation return differs")
	}
	step(12, "return_end")
	id := v.Identity()
	fmt.Printf("FES_NATIVE_FPGA_WINDOW_PRESENT PASS package_id=%s build_id=%s owner=%x source_generation=%d closed_generation=%d frames=%d last_reader_generation=%d last_cookie=%d bytes=%d crc32=%08x frame_sha256=%x access_map_sha256=%x data_closed=1 memory_retained=1 runtime_admission=unavailable\n",
		id.PackageID, id.BuildID, returned.After.Owner, returned.Source.Bridges.Generation, returned.After.Generation, returned.Frames,
		last.Frame.Generation, last.Frame.Cookie, last.Frame.DeliveredBytes, last.Frame.CRC32, last.Memory.FrameSHA256, returned.AccessMapSHA256)
	return session, nil
}
