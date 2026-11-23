//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/storagepolicy"
)

var expectedBootContext, expectedControlMap, expectedConfigurationMap, expectedReaderMemoryMap string
var expectedDDRReadMap string
var expectedMenuPresent, expectedMenuReadMap string
var expectedComputerMap, expectedComputerReload string
var expectedROMMap string
var expectedNativeComputer string
var expectedInitialTape string
var expectedComputerStorageReturn string
var expectedProgramPlatform, expectedSelectedPrograms string
var expectedCompiledMenuProgram, expectedCompiledSpectrum48, expectedSharedCoreShell string
var expectedTenfootSerial string
var expectedWritableStoragePlan, expectedSDTransferClock string
var programWritableStorageSelected bool
var programService *nativefabric.ProgramService
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
	if expectedSharedCoreShell != "" && (expectedSharedCoreShell != "1" || expectedCompiledSpectrum48 != "" ||
		expectedCompiledMenuProgram == "" || expectedSelectedPrograms != "1" || expectedTenfootSerial != "1") {
		panic("shared core shell requires the paired native platform")
	}
	var writablePlan *storagepolicy.Plan
	if expectedWritableStoragePlan != "" {
		if expectedSDTransferClock != "25000000" || expectedSelectedPrograms != "1" || expectedTenfootSerial != "1" {
			panic("writable storage requires selected native Tenfoot and 25 MHz transport")
		}
		p, err := storagepolicy.ParsePlan(decoded(expectedWritableStoragePlan, 8192))
		if err != nil {
			panic(err)
		}
		writablePlan = &p
	} else if expectedSDTransferClock != "" {
		panic("native transfer clock requires trusted writable storage plan")
	}
	if expectedMenuPresent != "" || expectedMenuReadMap != "" {
		if expectedMenuPresent != "1" || expectedMenuReadMap == "" ||
			expectedDDRReadMap != "" || expectedComputerMap != "" || expectedROMMap != "" || expectedNativeComputer != "" ||
			expectedComputerReload != "" || expectedComputerStorageReturn != "" || expectedSelectedPrograms != "" || expectedProgramPlatform != "" || expectedCompiledSpectrum48 != "" ||
			expectedTenfootSerial != "" || expectedUSBControl != "" || expectedCompiledMenuProgram != "" {
			panic("menu presentation requires its complete separate compiled boot selection")
		}
	}
	if (expectedTenfootSerial != "") != tenfootSerialLinked || expectedTenfootSerial != "" && (expectedTenfootSerial != "1" || expectedSelectedPrograms != "1") ||
		expectedSelectedPrograms != "" && expectedTenfootSerial != "1" {
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
	if expectedInitialTape != "" && (expectedInitialTape != "1" || expectedNativeComputer != "1" ||
		expectedROMMap == "" || expectedSelectedPrograms != "1" || expectedTenfootSerial != "1" ||
		expectedUSBControl != "owned-shared-usb-pio-v1") {
		panic("initial tape requires matched native computer, shared USB and selected serial Tenfoot")
	}
	cpu, err := de25nano.ProgramCPUTransport(expectedInitialTape == "1")
	if err != nil {
		panic(err)
	}
	control, configuration := decoded(expectedControlMap, 8192), decoded(expectedConfigurationMap, 32768)
	var v *nativefabric.HPSContextVerifier
	if expectedNativeComputer != "" && (expectedNativeComputer != "1" || expectedROMMap == "") {
		panic("native computer requires its explicitly selected complete held-ROM resources")
	}
	if expectedMenuPresent == "1" {
		v, err = nativefabric.NewMenuHPSContextVerifier(cpu, decoded(expectedBootContext, 8192), control,
			decoded(expectedReaderMemoryMap, 8192), decoded(expectedMenuReadMap, 8192))
	} else if expectedROMMap != "" {
		if expectedDDRReadMap != "" || expectedSelectedPrograms != "1" || expectedComputerMap == "" {
			panic("held ROM requires selected computer programs and its complete compiled resources")
		}
		if expectedNativeComputer == "1" {
			v, err = nativefabric.NewComputerHPSContextVerifier(cpu, decoded(expectedBootContext, 8192), control, decoded(expectedComputerMap, 8192), decoded(expectedROMMap, 8192))
		} else {
			v, err = nativefabric.NewROMHPSContextVerifier(cpu, decoded(expectedBootContext, 8192), control, decoded(expectedComputerMap, 8192), decoded(expectedROMMap, 8192))
		}
	} else {
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
		if expectedCompiledMenuProgram != "" {
			var companion nativefabric.CompiledMenuProgram
			companion, err = nativefabric.ParseCompiledMenuProgram(decoded(expectedCompiledMenuProgram, 65536))
			if err == nil {
				loader, err = nativefabric.NewProgramLoaderWithMenu(v, decoded(expectedProgramPlatform, 16384), control, configuration, decoded(expectedComputerMap, 8192), companion)
			}
		} else {
			loader, err = nativefabric.NewProgramLoader(v, decoded(expectedProgramPlatform, 16384), control, configuration, decoded(expectedComputerMap, 8192))
		}
		if err == nil && expectedCompiledSpectrum48 != "" {
			loader, err = loader.WithSpectrum48(decoded(expectedCompiledSpectrum48, 16384))
		}
		if err == nil && expectedSharedCoreShell == "1" {
			loader, err = loader.WithSharedCoreShell()
		}
		if err != nil {
			panic(err)
		}
	} else if expectedProgramPlatform != "" || expectedCompiledMenuProgram != "" || expectedCompiledSpectrum48 != "" {
		panic("program platform provided without selected program mode")
	}
	if err := prepareProgramNetwork(v); err != nil {
		panic(err)
	}
	if err := openProgramUSB(v); err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("verifier_ready")
	de25nano.StartupCheckpoint("sd_open_begin")
	var disk block.Device
	if writablePlan != nil {
		disk, err = de25nano.OpenWritableStorageAtClock(writablePlan.Identity(), 25000000)
	} else {
		disk, err = board.OpenStorage()
	}
	if err != nil {
		panic(err)
	}
	de25nano.StartupCheckpoint("sd_open_end")
	lease, ok := disk.(*sdcontrol.Session)
	if !ok {
		panic(errors.New("FPGA load requires owned SD storage"))
	}
	if err := lease.EnableCache(block.CacheConfig{Capacity: 128}); err != nil {
		panic(err)
	}
	var volume fs.FS
	if writablePlan != nil {
		check, done := context.WithTimeout(context.Background(), 100*time.Second)
		volume, err = storagepolicy.Mount(check, lease, *writablePlan)
		done()
	} else {
		volume, err = fat.Mount(disk)
	}
	if err != nil {
		panic(err)
	}
	programWritableStorageSelected = writablePlan != nil
	de25nano.StartupCheckpoint("fat_mount_end")
	store, err := packagestore.New(volume, lease)
	if err != nil {
		panic(err)
	}
	// The selected frontend is a build-tag constant. Keep the other diagnostic
	// paths out of this image; the startup gate above requires matching flags.
	if tenfootSerialLinked {
		if loader == nil {
			panic("serial Tenfoot requires the owned program loader")
		}
		serveProgramTenfoot(loader, store, lease)
		return
	}
	if loader != nil {
		panic("selected programs require serial Tenfoot")
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
	} else if expectedMenuPresent == "1" {
		memory, err := v.ReserveMenuMemory(de25nano.ReserveMenuMemory)
		if err != nil {
			panic(err)
		}
		if memory.Source.Bridges.Owner != closed.Owner || memory.Source.Bridges.Generation != closed.Generation {
			panic("menu publication differs from storage containment")
		}
		fmt.Printf("FES_NATIVE_FPGA_MENU_MEMORY PINNED cpu_base=%x bytes=%d slots=%d slot_bytes=%d map_sha256=%x initial_sha256=%x owner=%x generation=%d fpga_access=held lifetime=until-reboot\n",
			memory.CPUBase, memory.ArenaBytes, memory.Slots, memory.SlotBytes, memory.MapSHA256, memory.InitialSHA256, closed.Owner, closed.Generation)
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
	fmt.Printf("FES_NATIVE_CPU_MAPPING PASS el=%d sctlr=%x tcr=%x root=%x mair=%x mmfr0=%x mpidr=%x ram_base=%x ram_bytes=%d control_base=%x control_bytes=%d dma_base=%x dma_bytes=%d reader_base=%x reader_bytes=%d firmware_base=%x firmware_bytes=%d reads=%d translations=%d bytes=%d descriptor_sha256=%x owner=%x generation=%d data_held=1 runtime_admission=unavailable\n",
		cpuObservation.CPU.EL>>2, cpuObservation.CPU.SCTLR, cpuObservation.CPU.TCR, cpuObservation.CPU.TTBR0, cpuObservation.CPU.MAIR, cpuObservation.CPU.MMFR0, cpuObservation.CPU.MPIDR,
		resources.RAMBase, resources.RAMBytes, resources.ControlBase, resources.ControlBytes, resources.DMABase, resources.DMABytes,
		resources.ReaderBase, resources.ReaderBytes, resources.FirmwareBase, resources.FirmwareBytes, cpuObservation.TableReads, cpuObservation.Translations, cpuObservation.Bytes, cpuObservation.DescriptorSHA256, source.Bridges.Owner, source.Bridges.Generation)
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
	if expectedMenuPresent == "1" {
		if err := presentMenuFrames(v, control, decoded(expectedMenuReadMap, 8192)); err != nil {
			panic(err)
		}
		return
	}
	var observed nativefabric.FPGAIdentityObservation
	if id.ProgrammingProfile == nativefabric.HPSFirstDDRDiagnosticProfile {
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
	if id.ProgrammingProfile == nativefabric.HPSFirstDDRDiagnosticProfile {
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
