// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

func TestDeviceStatsTelemetry(t *testing.T) {
	if st := (*Device)(nil).Stats(); st != (Stats{}) {
		t.Fatalf("nil Device Stats=%+v want zero", st)
	}

	// 1. Single-sector PIO read and write accounting.
	hwPIO := newAdvancedCard(256, 0, nil, cap33V)
	dPIO, err := Open(hwPIO, Config{
		InputClockHz: 100_000_000,
		AllowWrites:  true,
	})
	if err != nil {
		t.Fatalf("Open PIO: %v", err)
	}
	readBuf := make([]byte, 1024) // 2 sectors -> 2 single-sector PIO transfers
	if err := dPIO.ReadSectors(0, readBuf); err != nil {
		t.Fatalf("PIO ReadSectors: %v", err)
	}
	if err := dPIO.WriteSectors(4, readBuf); err != nil {
		t.Fatalf("PIO WriteSectors: %v", err)
	}
	stPIO := dPIO.Stats()
	if stPIO.ReadOps != 1 || stPIO.WriteOps != 1 || stPIO.SectorsRead != 2 || stPIO.SectorsWritten != 2 ||
		stPIO.PIOOps != 4 || stPIO.ADMA2Ops != 0 || stPIO.CMD23Ops != 0 || stPIO.Errors != 0 ||
		stPIO.BusWidth != 1 || stPIO.ADMA2Active || stPIO.CMD23Active || stPIO.Quiesced || stPIO.Retired {
		t.Fatalf("unexpected PIO stats: %+v", stPIO)
	}

	// 2. 4-bit MultiBlock + CMD23 + ADMA2 + Quiesce/Resume persistence + Error retirement.
	const dmaBase = uint64(0x1000_0000)
	ram := make([]byte, 128<<10)
	pool, err := dmaops.NewPool(dmaBase, ram, dmaops.Uncached, &testCoherency{line: 64})
	if err != nil {
		t.Fatal(err)
	}
	hwAdv := newAdvancedCard(512, dmaBase, ram, cap33V|capADMA2)
	hwAdv.scr = [8]byte{0x02, 0x35, 0x84, 0x02} // 4-bit + CMD23 supported
	dAdv, err := Open(hwAdv, Config{
		InputClockHz:    100_000_000,
		AllowWrites:     true,
		TransferClockHz: 25_000_000,
		BusWidth4Bit:    true,
		MultiBlock:      true,
		UseCMD23:        true,
		UseADMA2:        true,
		DMAPool:         pool,
	})
	if err != nil {
		t.Fatalf("Open ADMA2+CMD23: %v", err)
	}
	bigBuf := make([]byte, 8*512)
	if err := dAdv.ReadSectors(0, bigBuf); err != nil {
		t.Fatalf("ADMA2 ReadSectors: %v", err)
	}
	if err := dAdv.WriteSectors(16, bigBuf); err != nil {
		t.Fatalf("ADMA2 WriteSectors: %v", err)
	}

	if err := dAdv.Quiesce(); err != nil {
		t.Fatalf("Quiesce: %v", err)
	}
	if st := dAdv.Stats(); !st.Quiesced || st.QuiesceCount != 1 || st.ResumeCount != 0 {
		t.Fatalf("stats after Quiesce=%+v", st)
	}
	if err := dAdv.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	stAdv := dAdv.Stats()
	if stAdv.ReadOps != 1 || stAdv.WriteOps != 1 || stAdv.SectorsRead != 8 || stAdv.SectorsWritten != 8 ||
		stAdv.PIOOps != 0 || stAdv.ADMA2Ops != 2 || stAdv.CMD23Ops != 2 ||
		stAdv.QuiesceCount != 1 || stAdv.ResumeCount != 1 || stAdv.Errors != 0 ||
		stAdv.BusWidth != 4 || stAdv.TransferClockHz != 25_000_000 || stAdv.HighSpeed ||
		!stAdv.ADMA2Active || !stAdv.CMD23Active || stAdv.Quiesced || stAdv.Retired {
		t.Fatalf("unexpected ADMA2+CMD23 stats: %+v", stAdv)
	}

	// Fault injection retires the device and increments Errors exactly once.
	hwAdv.admaFault = true
	if err := dAdv.ReadSectors(0, bigBuf); err == nil {
		t.Fatal("expected ADMA2 fault error")
	}
	if err := dAdv.ReadSectors(0, bigBuf); err == nil {
		t.Fatal("expected ErrRetired after fault")
	}
	stErr := dAdv.Stats()
	if !stErr.Retired || stErr.Errors != 1 {
		t.Fatalf("stats after fault retirement=%+v", stErr)
	}

	// 3. 50 MHz High-Speed reporting in Stats.
	dHS, err := Open(newHighSpeedCard(), Config{
		InputClockHz:    100_000_000,
		TransferClockHz: 50_000_000,
	})
	if err != nil {
		t.Fatalf("Open HighSpeed: %v", err)
	}
	stHS := dHS.Stats()
	if !stHS.HighSpeed || stHS.TransferClockHz != 50_000_000 || stHS.Retired || stHS.Quiesced {
		t.Fatalf("unexpected HighSpeed stats: %+v", stHS)
	}
}
