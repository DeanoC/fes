//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

var expectedBootContext, expectedControlMap, expectedConfigurationMap, expectedReaderMemoryMap string
var expectedDDRReadMap string
var expectedWindowMemory string
var expectedWindowReadMap string
var expectedWindowPresent, expectedWindowStorageReturn, expectedWindowReload string
var expectedWindowService string
var expectedComputerMap, expectedComputerReload string
var expectedROMMap string
var expectedComputerStorageReturn string
var expectedProgramPlatform, expectedSelectedPrograms string
var expectedTenfootSerial string
var retained *nativefabric.ConfigurationTransfer

func decoded(s string, limit int) []byte {
	if len(s) == 0 || len(s) > limit {
		panic("missing or oversized compiled FPGA source")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	de25nano.StartupCheckpoint("go_main")
	if expectedWindowPresent != "" && (expectedWindowPresent != "1" || expectedWindowReadMap == "" || expectedWindowMemory != "1") {
		panic("window presentation requires the selected full-window read path")
	}
	if expectedWindowStorageReturn != "" && (expectedWindowStorageReturn != "1" || expectedWindowPresent != "1") {
		panic("window storage return requires the selected presentation diagnostic")
	}
	if expectedWindowReload != "" && (expectedWindowReload != "1" || expectedWindowStorageReturn != "1") {
		panic("window reload requires the selected window SD-return diagnostic")
	}
	if expectedWindowService != "" && (expectedWindowService != "1" || expectedWindowStorageReturn != "1" || expectedWindowReload != "") {
		panic("window service requires the complete SD-return path and a separate selection from bounded reload")
	}
	if expectedTenfootSerial != "" && (expectedTenfootSerial != "1" || expectedSelectedPrograms != "1" || !tenfootSerialLinked) {
		panic("serial Tenfoot requires selected native programs")
	}
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 {
		panic("unexpected FPGA configuration CPU/source contract")
	}
	if expectedComputerReload != "" && (expectedComputerReload != "1" || expectedComputerMap == "" || expectedDDRReadMap != "") {
		panic("computer reload requires the selected held-data computer diagnostic")
	}
	if expectedComputerStorageReturn != "" && (expectedComputerStorageReturn != "1" || expectedComputerReload != "1") {
		panic("storage return requires the selected two-load computer diagnostic")
	}
	cpu := de25nano.FabricCPUTransport{}
	control, configuration := decoded(expectedControlMap, 8192), decoded(expectedConfigurationMap, 32768)
	var v *nativefabric.HPSContextVerifier
	var err error
	if expectedWindowMemory != "" {
		if expectedWindowMemory != "1" || expectedROMMap != "" || expectedDDRReadMap != "" || expectedComputerMap != "" || expectedComputerReload != "" || expectedProgramPlatform != "" || expectedSelectedPrograms != "" || expectedComputerStorageReturn != "" {
			panic("window memory requires its separate contained frame diagnostic")
		}
		v, err = nativefabric.NewWindowHPSContextVerifier(cpu, decoded(expectedBootContext, 8192), control, decoded(expectedReaderMemoryMap, 8192))
	} else if expectedROMMap != "" {
		if expectedWindowReadMap != "" || expectedDDRReadMap != "" || expectedSelectedPrograms != "1" || expectedComputerMap == "" {
			panic("held ROM requires selected computer programs and its complete compiled resources")
		}
		v, err = nativefabric.NewROMHPSContextVerifier(cpu, decoded(expectedBootContext, 8192), control, decoded(expectedComputerMap, 8192), decoded(expectedROMMap, 8192))
	} else {
		if expectedWindowReadMap != "" {
			panic("window reads require the complete compiled reservation")
		}
		v, err = nativefabric.NewHPSContextVerifier(cpu, decoded(expectedBootContext, 8192))
	}
	if err != nil {
		panic(err)
	}
	id := v.Identity()
	var loader *nativefabric.ProgramLoader
	if expectedSelectedPrograms != "" {
		if expectedSelectedPrograms != "1" || expectedComputerMap == "" || expectedDDRReadMap != "" || expectedComputerReload != "1" || expectedComputerStorageReturn != "1" {
			panic("selected programs require contained computer control, rearming and owned SD return")
		}
		loader, err = nativefabric.NewProgramLoader(v, decoded(expectedProgramPlatform, 16384), control, configuration, decoded(expectedComputerMap, 8192))
		if err != nil {
			panic(err)
		}
	} else if expectedProgramPlatform != "" {
		panic("program platform provided without selected program mode")
	}
	if err := openProgramUSB(v); err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("verifier_ready")
	de25nano.StartupCheckpoint("sd_open_begin")
	disk, err := board.OpenStorage()
	if err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("sd_open_end")
	lease, ok := disk.(*sdcontrol.Session)
	if !ok {
		panic(errors.New("FPGA load requires owned SD storage"))
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("fat_mount_end")
	store, err := packagestore.New(volume, lease)
	if err != nil {
		panic(err)
	}
	if loader != nil {
		if expectedTenfootSerial == "1" {
			serveProgramTenfoot(loader, store, volume)
		} else {
			servePrograms(loader, store, volume)
		}
		return
	}
	request := packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: id.PackageID}
	de25nano.StartupCheckpoint("retain_begin")
	selection, err := store.Retain(context.Background(), request)
	if err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("retain_end")
	plan, err := v.PreflightStoredConfiguration(control, configuration, selection)
	if err != nil {
		panic(err)
	}
	id = plan.Identity()
	handoff, err := store.Contain(selection)
	if err != nil {
		panic(err)
	}
	closed := handoff.Receipt()
	fmt.Printf("FES_NANO_LOAD_PACKAGE CONTAINED package_id=%s owner=%x generation=%d source=sd storage_active=0\n", id.PackageID, closed.Owner, closed.Generation)
	if id.ProgrammingProfile == nativefabric.HPSFirstDDRDiagnosticProfile {
		memory, err := v.ReserveReaderMemory(decoded(expectedReaderMemoryMap, 8192), de25nano.ReserveReaderMemory)
		if err != nil {
			panic(err)
		}
		if memory.Source.Bridges.Owner != closed.Owner || memory.Source.Bridges.Generation != closed.Generation {
			panic("DDR reader reservation differs from storage containment")
		}
		fmt.Printf("FES_NATIVE_FPGA_READER_MEMORY PINNED cpu_base=%x bus_base=%x bytes=%d arena_bytes=%d map_sha256=%x pattern_sha256=%x owner=%x generation=%d fpga_access=unqualified\n",
			memory.CPUBase, memory.BusBase, memory.Bytes, memory.ArenaBytes, memory.MapSHA256, memory.PatternSHA256, closed.Owner, closed.Generation)
	} else if expectedWindowMemory == "1" {
		frame, err := windowFrame()
		if err != nil {
			panic(err)
		}
		memory, err := v.ReserveWindowMemory(frame, de25nano.ReserveWindowMemory)
		if err != nil {
			panic(err)
		}
		if memory.Source.Bridges.Owner != closed.Owner || memory.Source.Bridges.Generation != closed.Generation {
			panic("window publication differs from storage containment")
		}
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_MEMORY PINNED cpu_base=%x bus_base=%x bytes=%d frame_bytes=%d row_bytes=%d stride=%d rows=%d map_sha256=%x frame_sha256=%x crc32=%08x owner=%x generation=%d fpga_access=held lifetime=until-reboot renderer=tenfoot\n",
			memory.CPUBase, memory.Geometry.Base, memory.ArenaBytes, memory.FrameBytes, memory.Geometry.RowBytes, memory.Geometry.Stride, memory.Geometry.Rows,
			memory.MapSHA256, memory.FrameSHA256, memory.CRC32, closed.Owner, closed.Generation)
	} else if expectedReaderMemoryMap != "" {
		panic("on-chip reader cannot supply a DDR memory map")
	}
	var source nativefabric.HPSBootObservation
	retained, source, err = plan.NewTransfer(handoff, de25nano.ReserveConfiguration)
	if err != nil {
		panic(err) // Board reservation remains pinned, including constructor errors.
	}
	resources, err := de25nano.ObserveFabricResources()
	if err != nil {
		panic(err)
	}
	afterResources, err := v.Verify(v.Identity())
	if err != nil {
		panic(err)
	}
	if source != afterResources {
		panic("CPU resource observation changed secure source ownership")
	}
	cpuObservation := resources.Mapping
	fmt.Printf("FES_NATIVE_CPU_MAPPING PASS el=%d sctlr=%x tcr=%x root=%x mair=%x mmfr0=%x mpidr=%x ram_base=%x ram_bytes=%d control_base=%x control_bytes=%d dma_base=%x dma_bytes=%d reader_base=%x reader_bytes=%d reads=%d translations=%d bytes=%d descriptor_sha256=%x owner=%x generation=%d data_held=1 runtime_admission=unavailable\n",
		cpuObservation.CPU.EL>>2, cpuObservation.CPU.SCTLR, cpuObservation.CPU.TCR, cpuObservation.CPU.TTBR0, cpuObservation.CPU.MAIR, cpuObservation.CPU.MMFR0, cpuObservation.CPU.MPIDR,
		resources.RAMBase, resources.RAMBytes, resources.ControlBase, resources.ControlBytes, resources.DMABase, resources.DMABytes,
		resources.ReaderBase, resources.ReaderBytes, cpuObservation.TableReads, cpuObservation.Translations, cpuObservation.Bytes, cpuObservation.DescriptorSHA256, source.Bridges.Owner, source.Bridges.Generation)
	if expectedComputerMap != "" {
		if expectedDDRReadMap != "" {
			panic("computer control cannot share an active DDR diagnostic")
		}
		mapping := decoded(expectedComputerMap, 8192)
		count := 1
		if expectedComputerReload == "1" {
			count = 2
		}
		for cycle := 1; cycle <= count; cycle++ {
			if count == 2 {
				fmt.Printf("FES_NATIVE_FPGA_CYCLE BEGIN cycle=%d count=2 owner=%x generation=%d\n", cycle, source.Bridges.Owner, source.Bridges.Generation)
			}
			program(v, configuration, source)
			if err := probeComputer(v, control, mapping); err != nil {
				panic(err)
			}
			if count == 2 {
				fmt.Printf("FES_NATIVE_FPGA_CYCLE END cycle=%d count=2\n", cycle)
			}
			if cycle < count {
				if expectedComputerStorageReturn == "1" {
					returned, err := v.Verify(v.Identity())
					if err != nil {
						panic(err)
					}
					fmt.Printf("FES_NATIVE_STORAGE_RETURN BEGIN owner=%x generation=%d package_id=%s data_held=1\n",
						returned.Bridges.Owner, returned.Bridges.Generation, id.PackageID)
					if err := v.ResumeStorageAfterReturn(store); err != nil {
						panic(err)
					}
					active, valid := lease.LastReceipt()
					if !valid || !active.Active || !active.Prepared || active.Owner != returned.Bridges.Owner || active.Generation != returned.Bridges.Generation+2 {
						panic("storage resume lost the owned return")
					}
					fmt.Printf("FES_NATIVE_STORAGE_RETURN ACTIVE owner=%x generation=%d return_generation=%d writes=disabled\n",
						active.Owner, active.Generation, returned.Bridges.Generation)
					request = packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: id.PackageID}
					selection, err = store.Retain(context.Background(), request)
					if err != nil {
						panic(err)
					}
					plan, err = v.PreflightStoredConfiguration(control, configuration, selection)
					if err != nil {
						panic(err)
					}
					id = plan.Identity()
					handoff, err = store.Contain(selection)
					if err != nil {
						panic(err)
					}
					held := handoff.Receipt()
					if held.Owner != active.Owner || held.Generation != active.Generation+1 || handoff.ReturnedFrom().Generation != returned.Bridges.Generation {
						panic("fresh package containment lost its core return")
					}
					fmt.Printf("FES_NATIVE_STORAGE_RETURN CONTAINED owner=%x generation=%d return_generation=%d package_id=%s source=sd writes=disabled\n",
						held.Owner, held.Generation, returned.Bridges.Generation, selection.Inspection().PackageID)
				} else {
					plan, err = v.PreflightStoredConfiguration(control, configuration, selection)
					if err != nil {
						panic(err)
					}
				}
				retained, source, err = plan.NewReconfigurationTransfer(handoff, de25nano.ReserveConfiguration)
				if err != nil {
					panic(err)
				}
			}
		}
		return
	}
	program(v, configuration, source)
	if expectedWindowReadMap != "" {
		limits := scanout.ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: func() { time.Sleep(time.Millisecond) }}
		if expectedWindowService == "1" {
			if err := serveWindows(v, store, request, control, configuration, decoded(expectedWindowReadMap, 8192), limits); err != nil {
				reportWindowFailure(err)
				panic(err)
			}
			return
		}
		if expectedWindowPresent == "1" {
			session, err := presentWindowFrames(v, control, decoded(expectedWindowReadMap, 8192), limits, 12)
			if err != nil {
				reportWindowFailure(err)
				panic(err)
			}
			if expectedWindowStorageReturn == "1" {
				fresh, err := verifyWindowStorageReturn(session, store, handoff, request)
				if err != nil {
					reportWindowFailure(err)
					panic(err)
				}
				if expectedWindowReload == "1" {
					if err := reloadWindow(v, control, configuration, decoded(expectedWindowReadMap, 8192), session, fresh, limits); err != nil {
						reportWindowFailure(err)
						panic(err)
					}
				}
			}
			return
		}
		observation, err := v.ProbeWindowReader(context.Background(), control, decoded(expectedWindowReadMap, 8192), retained, limits)
		if err != nil {
			reportWindowFailure(err)
			panic(err)
		}
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_READ PASS package_id=%s build_id=%s owner=%x source_generation=%d closed_generation=%d reader_generation=%d cookie=%d bytes=%d crc32=%08x frame_sha256=%x access_map_sha256=%x data_closed=1 memory_retained=1 runtime_admission=unavailable\n",
			id.PackageID, id.BuildID, observation.After.Owner, observation.Source.Bridges.Generation, observation.After.Generation,
			observation.Frame.Generation, observation.Frame.Cookie, observation.Frame.DeliveredBytes, observation.Frame.CRC32,
			observation.Memory.FrameSHA256, observation.AccessMapSHA256)
		return
	}
	var observed nativefabric.FPGAIdentityObservation
	if id.ProgrammingProfile == nativefabric.HPSFirstDDRDiagnosticProfile || id.ProgrammingProfile == nativefabric.HPSFirstWindowDiagnosticProfile {
		if expectedDDRReadMap != "" {
			observed, err = v.ProbeDDRReader(control, decoded(expectedReaderMemoryMap, 8192), decoded(expectedDDRReadMap, 8192))
		} else {
			observed, err = v.ProbeIdentity(control)
		}
	} else {
		observed, err = v.ProbeReader(control)
	}
	if err != nil {
		panic(err)
	}
	if observed.Source != source {
		panic("FPGA identity changed source context")
	}
	fmt.Printf("FES_NATIVE_FPGA_IDENTITY PASS package_id=%s build_id=%s abi=%x base=%x map_sha256=%s owner=%x generation=%d data_held=1 runtime_programming=unavailable\n",
		id.PackageID, id.BuildID, observed.Identity.ABI, observed.CPUBase, hex.EncodeToString(observed.ControlMapSHA256[:]), observed.After.Owner, observed.After.Generation)
	if id.ProgrammingProfile == nativefabric.HPSFirstWindowDiagnosticProfile {
		fmt.Printf("FES_NATIVE_FPGA_WINDOW_READER BLOCKED owner=%x generation=%d data_held=1 translation=unqualified runtime_programming=unavailable\n", observed.After.Owner, observed.After.Generation)
	} else if id.ProgrammingProfile == nativefabric.HPSFirstDDRDiagnosticProfile {
		if expectedDDRReadMap != "" {
			fmt.Printf("FES_NATIVE_FPGA_DDR_READER PASS owner=%x generation=%d reader_generation=1 cookie=7 bytes=32 data_closed=1 runtime_programming=unavailable\n", observed.After.Owner, observed.After.Generation)
		} else {
			fmt.Printf("FES_NATIVE_FPGA_DDR_READER BLOCKED owner=%x generation=%d data_held=1 translation=unqualified drain=unqualified runtime_programming=unavailable\n", observed.After.Owner, observed.After.Generation)
		}
	} else {
		fmt.Printf("FES_NATIVE_FPGA_READER PASS owner=%x generation=%d reader_generation=1 cookie=7 data_held=1 runtime_programming=unavailable\n",
			observed.After.Owner, observed.After.Generation)
	}
}

// One fixed transfer keeps the original deadline and retirement behavior.
// The global transfer and board arena retain uncertain source/DMA ownership.
func program(v *nativefabric.HPSContextVerifier, configuration []byte, source nativefabric.HPSBootObservation) {
	id, ok := retained.ProgramIdentity()
	if !ok {
		panic("configuration lacks its retained program identity")
	}
	size := retained.Status().RetainedBytes
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%d runtime_programming=unavailable\n",
		hex.EncodeToString(source.Digest[:]), id.PackageID, id.BuildID, source.Bridges.Owner, source.Bridges.Generation)
	defer func() {
		failure := recover()
		if failure == nil {
			return
		}
		if status := retained.Status(); status.Failed && status.Outstanding > 0 {
			var observation nativefabric.ConfigurationRetirement
			var err error
			observation, err = v.RetireConfiguration(retained, source, configuration)
			if err != nil {
				fmt.Printf("FES_NATIVE_FPGA_RETIRE REJECT error=%v retained=%d\n", err, status.RetainedBytes)
			} else {
				fmt.Printf("FES_NATIVE_FPGA_RETIRE ACK owner=%x generation=%d status=%d retained=%d activation=unavailable\n",
					observation.Owner, observation.Generation, observation.Status, status.RetainedBytes)
			}
		}
		panic(failure)
	}()
	started := time.Now()
	deadline := started.Add(20 * time.Second)
	for {
		phase := retained.Status().Phase
		if !time.Now().Before(deadline) {
			panic(retained.Cancel(fmt.Errorf("FPGA configuration deadline")))
		}
		done, busy, err := retained.Advance()
		if !time.Now().Before(deadline) {
			_ = retained.Cancel(fmt.Errorf("late FPGA firmware return"))
			status := retained.Status()
			panic(fmt.Sprintf("late FPGA firmware return phase=%s elapsed_us=%d accepted=%d completed=%d outstanding=%d retained=%d",
				phase, time.Since(started).Microseconds(), status.AcceptedBytes, status.CompletedBytes, status.Outstanding, status.RetainedBytes))
		}
		if err != nil {
			panic(err)
		}
		if done {
			break
		}
		if busy {
			time.Sleep(time.Millisecond)
		}
	}
	status := retained.Status()
	after, err := v.Verify(v.Identity())
	if err != nil || after != source || status.AcceptedBytes != size || status.CompletedBytes != size || status.Outstanding != 0 || !status.Done || status.Failed {
		panic("FPGA configuration did not retain closed source ownership")
	}
	fmt.Printf("FES_NATIVE_FPGA_PROGRAMMED PASS package_id=%s core_sha256=%s bytes=%d owner=%x generation=%d data_held=1 activation=unavailable\n",
		id.PackageID, id.ProgrammedSHA256, size, source.Bridges.Owner, source.Bridges.Generation)
}

func probeComputer(v *nativefabric.HPSContextVerifier, control, mapping []byte) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	limits := computer.Limits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: runtime.Gosched}
	var s *nativefabric.ComputerSession
	s, err = v.OpenComputer(ctx, control, mapping, retained, limits)
	id, valid := retained.ProgramIdentity()
	if !valid {
		return errors.New("computer lacks its selected program identity")
	}
	if err != nil {
		return err
	}
	defer func() {
		observed, cleanup := s.Close(context.Background())
		err = errors.Join(err, cleanup)
		if err == nil && observed.HoldAcknowledged {
			fmt.Printf("FES_NATIVE_COMPUTER_CONTROL PASS package_id=%s build_id=%s base=%x map_sha256=%x owner=%x generation=%d held_ack=1 data_held=1 execution_observed=1 runtime_admission=unavailable\n",
				id.PackageID, id.BuildID, observed.CPUBase, observed.MapSHA256,
				observed.After.Owner, observed.After.Generation)
		}
	}()
	c := s.Computer()
	sample := func(phase string) (computer.FetchActivity, error) {
		a, err := c.Activity(ctx)
		if err == nil {
			fmt.Printf("FES_NATIVE_COMPUTER_ACTIVITY phase=%s fetches=%d address=%x\n", phase, a.Cycles, a.Address)
		}
		return a, err
	}
	held := func(first, last string) (computer.FetchActivity, error) {
		if err := c.Hold(ctx); err != nil {
			return computer.FetchActivity{}, err
		}
		// Allow clock-enabled CPU reset to settle. The observation is never
		// cleared or masked by Hold: actual instruction fetches must stop.
		time.Sleep(time.Millisecond)
		a, err := sample(first)
		if err != nil {
			return a, err
		}
		time.Sleep(20 * time.Millisecond)
		b, err := sample(last)
		if err == nil && a != b {
			err = errors.New("computer fetch bus continued while held")
		}
		return b, err
	}
	run := func(phase string, before computer.FetchActivity) (computer.FetchActivity, error) {
		if err := c.Run(ctx); err != nil {
			return computer.FetchActivity{}, err
		}
		time.Sleep(20 * time.Millisecond)
		after, err := sample(phase)
		delta := after.Cycles - before.Cycles
		if err == nil && (delta == 0 || delta >= 1<<31) {
			err = errors.New("computer fetch bus did not advance within its configured lifetime")
		}
		return after, err
	}
	initial, err := held("initial", "initial-held")
	if err != nil {
		return err
	}
	active, err := run("run", initial)
	if err != nil {
		return err
	}
	if err = c.Keyboard(ctx, [9]uint16{}); err != nil {
		return err
	}
	returned, err := held("return", "return-held")
	if err != nil {
		return err
	}
	if returned.Cycles-active.Cycles >= 1<<31 {
		return errors.New("computer fetch observation reset on return")
	}
	relaunched, err := run("relaunch", returned)
	if err != nil {
		return err
	}
	final, err := held("final", "final-held")
	if err != nil {
		return err
	}
	if final.Cycles-relaunched.Cycles >= 1<<31 {
		return errors.New("computer fetch observation reset on final Hold")
	}
	return nil // Close acknowledges Hold, retires MMIO and verifies reclosure.
}
