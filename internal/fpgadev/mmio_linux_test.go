//go:build linux && (amd64 || arm)

package fpgadev

import (
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMMIOLayoutRejectsInvalidPageAlignmentBoundsAndOverflow(t *testing.T) {
	tests := []struct {
		name      string
		address   uint64
		pageSize  int
		length    int
		gpoOffset uint64
		gpiOffset uint64
	}{
		{name: "zero page", address: fpgaManagerBaseAddress, pageSize: 0, length: 4096, gpoOffset: 0x10, gpiOffset: 0x14},
		{name: "negative page", address: fpgaManagerBaseAddress, pageSize: -4096, length: 4096, gpoOffset: 0x10, gpiOffset: 0x14},
		{name: "unaligned address", address: fpgaManagerBaseAddress + 1, pageSize: 4096, length: 4096, gpoOffset: 0x10, gpiOffset: 0x14},
		{name: "unaligned GPO", address: fpgaManagerBaseAddress, pageSize: 4096, length: 4096, gpoOffset: 0x11, gpiOffset: 0x14},
		{name: "out of bounds GPI", address: fpgaManagerBaseAddress, pageSize: 4096, length: 16, gpoOffset: 0x10, gpiOffset: 0x14},
		{name: "mapping length mismatch", address: fpgaManagerBaseAddress, pageSize: 4096, length: 8192, gpoOffset: 0x10, gpiOffset: 0x14},
		{name: "address overflow", address: ^uint64(0) - 1023, pageSize: 4096, length: 4096, gpoOffset: 0x10, gpiOffset: 0x14},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMMIOLayout(test.address, test.pageSize, test.length, test.gpoOffset, test.gpiOffset); err == nil {
				t.Fatal("invalid MMIO layout was accepted")
			}
		})
	}
}

func TestMMIOUsesLittleEndianOrdered32BitReadWriteAndReadback(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	linuxRegisters := registers.(*linuxRegisters)
	if got := binary.LittleEndian.Uint32(linuxRegisters.data[fpgaManagerGPOOffset:]); got != 0x01020304 {
		t.Fatalf("initial little-endian bytes decode %#x", got)
	}
	if err := registers.WriteGPO(0xaabbccdd); err != nil {
		t.Fatalf("WriteGPO: %v", err)
	}
	if got, err := registers.ReadGPO(); err != nil || got != 0xaabbccdd {
		t.Fatalf("ReadGPO = %#x, %v", got, err)
	}
	if got := linuxRegisters.data[fpgaManagerGPOOffset : fpgaManagerGPOOffset+4]; !reflect.DeepEqual(got, []byte{0xdd, 0xcc, 0xbb, 0xaa}) {
		t.Fatalf("stored GPO bytes = %#v", got)
	}
	if err := registers.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestMMIOWriteReadbackMismatchFailsClosed(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	mapper.read32 = func([]byte, uint64) (uint32, error) { return 0xfeedface, nil }
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	defer registers.Close()
	if err := registers.WriteGPO(0); err == nil {
		t.Fatal("mismatched GPO write readback was accepted")
	}
}

func TestMMIOOpenFailureClosesNoAcquiredResourcesAndMapFailureClosesFD(t *testing.T) {
	openErr := errors.New("open")
	mapper := &Mapper{
		pageSize: 4096,
		open:     func(string, int, uint32) (int, error) { return -1, openErr },
		mapFn:    func(int, int64, int, int, int) ([]byte, error) { return nil, errors.New("unexpected map") },
		unmap:    func([]byte) error { return nil },
		close:    func(int) error { return nil },
		fstat:    func(int) (mmioDeviceStat, error) { return mmioDeviceStat{}, errors.New("unexpected fstat") },
	}
	if _, err := mapper.OpenMailbox(); !errors.Is(err, openErr) {
		t.Fatalf("open error = %v", err)
	}

	var closes []int
	mapper = &Mapper{
		pageSize: 4096,
		open:     func(string, int, uint32) (int, error) { return 8, nil },
		mapFn:    func(int, int64, int, int, int) ([]byte, error) { return nil, errors.New("map") },
		unmap:    func([]byte) error { return nil },
		close: func(fd int) error {
			closes = append(closes, fd)
			return nil
		},
		fstat: func(int) (mmioDeviceStat, error) {
			return mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 1}, nil
		},
	}
	if _, err := mapper.OpenMailbox(); err == nil {
		t.Fatal("map failure was accepted")
	}
	if !reflect.DeepEqual(closes, []int{8}) {
		t.Fatalf("close calls = %v", closes)
	}
}

func TestMMIOOpenErrorWithDescriptorClosesAndAggregatesCleanup(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(*Mapper) error
	}{
		{name: "development mapping", open: func(mapper *Mapper) error {
			_, err := mapper.OpenMailbox()
			return err
		}},
		{name: "recovery mapping", open: func(mapper *Mapper) error {
			_, err := mapper.openRecovery(context.Background())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			openErr := errors.New("open returned descriptor and error")
			closeErr := errors.New("close descriptor")
			closeCalls := 0
			mapper := newAnonymousMapperForTest(t)
			mapper.open = func(string, int, uint32) (int, error) { return 77, openErr }
			mapper.close = func(int) error {
				closeCalls++
				return closeErr
			}
			if err := test.open(mapper); !errors.Is(err, openErr) || !errors.Is(err, closeErr) {
				t.Fatalf("open error = %v, want both causes", err)
			}
			if closeCalls != 1 || mapper.closeCalls != 1 {
				t.Fatalf("descriptor cleanup = callback:%d counter:%d, want one", closeCalls, mapper.closeCalls)
			}
		})
	}
}

func TestMMIOMapErrorWithBytesUnmapsAndAggregatesCleanup(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(*Mapper) error
	}{
		{name: "development mapping", open: func(mapper *Mapper) error {
			_, err := mapper.OpenMailbox()
			return err
		}},
		{name: "recovery mapping", open: func(mapper *Mapper) error {
			_, err := mapper.openRecovery(context.Background())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapErr := errors.New("map returned bytes and error")
			unmapErr := errors.New("unmap partial mapping")
			closeErr := errors.New("close descriptor")
			mapper := newAnonymousMapperForTest(t)
			mapper.mapFn = func(_ int, _ int64, length, _, _ int) ([]byte, error) {
				return make([]byte, length), mapErr
			}
			mapper.unmap = func([]byte) error { return unmapErr }
			mapper.close = func(int) error { return closeErr }
			if err := test.open(mapper); !errors.Is(err, mapErr) || !errors.Is(err, unmapErr) || !errors.Is(err, closeErr) {
				t.Fatalf("map error = %v, want map/unmap/close causes", err)
			}
			if mapper.unmapCalls != 1 || mapper.closeCalls != 1 {
				t.Fatalf("partial mapping cleanup = unmap:%d close:%d, want one each", mapper.unmapCalls, mapper.closeCalls)
			}
		})
	}
}

func TestMMIOUnmapAndCloseErrorsAreAggregatedAndCloseIsIdempotent(t *testing.T) {
	unmapErr := errors.New("unmap")
	closeErr := errors.New("close")
	mapper := newAnonymousMapperForTest(t)
	var unmaps, closes int
	mapper.unmap = func([]byte) error { unmaps++; return unmapErr }
	mapper.close = func(int) error { closes++; return closeErr }
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	first := registers.Close()
	second := registers.Close()
	if !errors.Is(first, unmapErr) || !errors.Is(first, closeErr) || !errors.Is(second, unmapErr) || !errors.Is(second, closeErr) {
		t.Fatalf("close errors first:%v second:%v", first, second)
	}
	if unmaps != 1 || closes != 1 {
		t.Fatalf("cleanup counts unmap:%d close:%d", unmaps, closes)
	}
}

func TestMMIOStaticAllowlistContainsOnlyPinnedBridgeGroups(t *testing.T) {
	if bridgeFPGAInterfaceRegisterAddress != 0xffd08028 || bridgeSDRPortRegisterAddress != 0xffc25080 || bridgeModuleResetRegisterAddress != 0xffd0501c || bridgeNIC301RemapRegisterAddress != 0xff800000 {
		t.Fatalf("source-bound bridge register addresses = %#x %#x %#x %#x", bridgeFPGAInterfaceRegisterAddress, bridgeSDRPortRegisterAddress, bridgeModuleResetRegisterAddress, bridgeNIC301RemapRegisterAddress)
	}
	want := []BridgeGroup{BridgeFPGAInterface, BridgeSDRPort, BridgeModuleReset, BridgeNIC301Remap}
	if !reflect.DeepEqual(pinnedBridgeGroups[:], want) {
		t.Fatalf("pinned bridge groups = %#v, want %#v", pinnedBridgeGroups, want)
	}
	for _, group := range pinnedBridgeGroups {
		if !strings.Contains(string(group), "fpga") && !strings.Contains(string(group), "sdr") && !strings.Contains(string(group), "reset") && !strings.Contains(string(group), "nic") {
			t.Fatalf("unexpected bridge group %q", group)
		}
	}
	wantDescriptors := [...]bridgeRegisterDescriptor{
		{group: BridgeFPGAInterface, address: 0xffd08000, offset: 0x28},
		{group: BridgeSDRPort, address: 0xffc25000, offset: 0x80},
		{group: BridgeModuleReset, address: 0xffd05000, offset: 0x1c},
		{group: BridgeNIC301Remap, address: 0xff800000, offset: 0x00},
	}
	if !reflect.DeepEqual(bridgeRegisterDescriptors, wantDescriptors) {
		t.Fatalf("bridge register descriptors = %#v, want %#v", bridgeRegisterDescriptors, wantDescriptors)
	}
	for _, descriptor := range bridgeRegisterDescriptors {
		if descriptor.address+descriptor.offset != map[BridgeGroup]uint64{
			BridgeFPGAInterface: bridgeFPGAInterfaceRegisterAddress,
			BridgeSDRPort:       bridgeSDRPortRegisterAddress,
			BridgeModuleReset:   bridgeModuleResetRegisterAddress,
			BridgeNIC301Remap:   bridgeNIC301RemapRegisterAddress,
		}[descriptor.group] {
			t.Fatalf("descriptor %q is not source-bound: %#x+%#x", descriptor.group, descriptor.address, descriptor.offset)
		}
	}
}

func TestProductionQualifierUsesFixedBridgeObserver(t *testing.T) {
	qualifier := NewQualifier(NewMapper(), NewStaticPolicy(), QualificationObservers{})
	if _, ok := qualifier.implementation.dependencies.Bridge.(*linuxBridgeStateObserver); !ok {
		t.Fatalf("production bridge observer type = %T, want fixed Linux observer", qualifier.implementation.dependencies.Bridge)
	}
}

func TestMMIORecoveryWritesOnlyPinnedBridgeTupleAndViews(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	recovery, err := mapper.openRecovery(context.Background())
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	defer recovery.Close()
	if err := recovery.DisableBridges(PinnedBridgeDisable()); err != nil {
		t.Fatalf("DisableBridges: %v", err)
	}
	readback, err := recovery.ReadBridgeTuple()
	if err != nil {
		t.Fatalf("ReadBridgeTuple: %v", err)
	}
	if readback != (BridgeTuple{FPGAInterfaceModule: 0, SDRPort: 0, BridgeModuleReset: 7}) {
		t.Fatalf("bridge readback = %#v", readback)
	}
	if err := recovery.DisableBridges(BridgeTuple{}); err == nil {
		t.Fatal("non-pinned bridge tuple was accepted")
	}
	if err := recovery.WriteGPO(0xac100000); err == nil {
		t.Fatal("recovery mapping accepted a nonzero mailbox command")
	}
}

func TestMMIORecoveryProgrammingProofNeverInfersProcessOrMappingAbsence(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	mapper.mapFn = func(_ int, offset int64, length, _, _ int) ([]byte, error) {
		page := make([]byte, length)
		if uint64(offset) == fpgaManagerBaseAddress {
			binary.LittleEndian.PutUint32(page[fpgaManagerStatusOffset:], 0x4)
			binary.LittleEndian.PutUint32(page[fpgaManagerControlOffset:], 0)
		}
		return page, nil
	}
	recovery, err := mapper.openRecovery(context.Background())
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	defer recovery.Close()
	proof, err := recovery.ReadProgramming()
	if err != nil {
		t.Fatalf("ReadProgramming: %v", err)
	}
	if !proof.UserMode || !proof.DriveReleased || proof.NoProgrammingProcess || proof.NoProgrammingMapping {
		t.Fatalf("programming proof = %#v", proof)
	}
}

func TestMMIORecoveryProgrammingRequiresEveryOwnershipAndPullControlBitClear(t *testing.T) {
	for _, test := range []struct {
		name    string
		control uint32
	}{
		{name: "enable", control: 1 << 0},
		{name: "axi configuration", control: 1 << 8},
		{name: "nCONFIG pull", control: 1 << 2},
		{name: "nSTATUS pull", control: 1 << 3},
		{name: "CONF_DONE pull", control: 1 << 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			mapper.mapFn = func(_ int, offset int64, length, _, _ int) ([]byte, error) {
				page := make([]byte, length)
				if uint64(offset) == fpgaManagerBaseAddress {
					binary.LittleEndian.PutUint32(page[fpgaManagerStatusOffset:], programmingUserModeValue)
					binary.LittleEndian.PutUint32(page[fpgaManagerControlOffset:], test.control)
				}
				return page, nil
			}
			recovery, err := mapper.openRecovery(context.Background())
			if err != nil {
				t.Fatalf("OpenRecovery: %v", err)
			}
			defer recovery.Close()
			proof, err := recovery.ReadProgramming()
			if err != nil {
				t.Fatalf("ReadProgramming: %v", err)
			}
			if proof.UserMode && proof.DriveReleased {
				t.Fatalf("ownership/pull control %#x was accepted: %#v", test.control, proof)
			}
		})
	}
}

func TestMMIORecoveryProgrammingRequiresUserMode(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	mapper.mapFn = func(_ int, offset int64, length, _, _ int) ([]byte, error) {
		page := make([]byte, length)
		if uint64(offset) == fpgaManagerBaseAddress {
			binary.LittleEndian.PutUint32(page[fpgaManagerStatusOffset:], 0)
			binary.LittleEndian.PutUint32(page[fpgaManagerControlOffset:], 0)
		}
		return page, nil
	}
	recovery, err := mapper.openRecovery(context.Background())
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	defer recovery.Close()
	proof, err := recovery.ReadProgramming()
	if err != nil {
		t.Fatalf("ReadProgramming: %v", err)
	}
	if proof.UserMode {
		t.Fatalf("non-USERMODE status was accepted: %#v", proof)
	}
}

func TestMMIORecoveryProgrammingRejectsEachRegisterReadFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		call int
	}{
		{name: "status", call: 1},
		{name: "control", call: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			calls := 0
			readErr := errors.New("programming register read failure")
			mapper.read32 = func(data []byte, offset uint64) (uint32, error) {
				calls++
				if calls == test.call {
					return 0, readErr
				}
				return readMMIO32(data, offset)
			}
			recovery, err := mapper.openRecovery(context.Background())
			if err != nil {
				t.Fatalf("OpenRecovery: %v", err)
			}
			defer recovery.Close()
			if _, err := recovery.ReadProgramming(); !errors.Is(err, readErr) {
				t.Fatalf("%s read error = %v, want %v", test.name, err, readErr)
			}
			if calls != test.call {
				t.Fatalf("programming reads before %s failure = %d, want %d", test.name, calls, test.call)
			}
		})
	}
}

func TestMMIORecoveryWritesL3RemapWithoutReadingWriteOnlyRegister(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	var writes, reads []struct {
		address uint64
		value   uint32
	}
	mapper.write32 = func(data []byte, offset uint64, value uint32) error {
		writes = append(writes, struct {
			address uint64
			value   uint32
		}{offset, value})
		return writeMMIO32(data, offset, value)
	}
	mapper.read32 = func(data []byte, offset uint64) (uint32, error) {
		reads = append(reads, struct {
			address uint64
			value   uint32
		}{offset, 0})
		return readMMIO32(data, offset)
	}
	recovery, err := mapper.openRecovery(context.Background())
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	defer recovery.Close()
	if err := recovery.DisableBridges(PinnedBridgeDisable()); err != nil {
		t.Fatalf("DisableBridges: %v", err)
	}
	if len(writes) != len(pinnedBridgeGroups) {
		t.Fatalf("bridge writes = %d, want %d", len(writes), len(pinnedBridgeGroups))
	}
	if writes[len(writes)-1].value != 1 {
		t.Fatalf("L3 remap write = %#x, want 1", writes[len(writes)-1].value)
	}
	// DisableBridges itself must not read any bridge word; the only reads in
	// this operation would be evidence that the write-only remap was faked.
	if len(reads) != 0 {
		t.Fatalf("DisableBridges performed %d bridge reads", len(reads))
	}
}

func TestMMIORecoveryRejectsEachPinnedBridgeWriteFailure(t *testing.T) {
	for index, group := range pinnedBridgeGroups {
		t.Run(string(group), func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			calls := 0
			writeErr := errors.New("bridge write failure")
			mapper.write32 = func(data []byte, offset uint64, value uint32) error {
				calls++
				if calls == index+1 {
					return writeErr
				}
				return writeMMIO32(data, offset, value)
			}
			recovery, err := mapper.openRecovery(context.Background())
			if err != nil {
				t.Fatalf("OpenRecovery: %v", err)
			}
			defer recovery.Close()
			if err := recovery.DisableBridges(PinnedBridgeDisable()); !errors.Is(err, writeErr) {
				t.Fatalf("%s write error = %v, want %v", group, err, writeErr)
			}
			if calls != index+1 {
				t.Fatalf("writes before %s failure = %d, want %d", group, calls, index+1)
			}
		})
	}
}

func TestMMIORecoveryRejectsEachReadableBridgeReadFailure(t *testing.T) {
	for index, group := range []BridgeGroup{BridgeFPGAInterface, BridgeSDRPort, BridgeModuleReset} {
		t.Run(string(group), func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			calls := 0
			readErr := errors.New("bridge read failure")
			mapper.read32 = func(data []byte, offset uint64) (uint32, error) {
				calls++
				if calls == index+1 {
					return 0, readErr
				}
				return readMMIO32(data, offset)
			}
			recovery, err := mapper.openRecovery(context.Background())
			if err != nil {
				t.Fatalf("OpenRecovery: %v", err)
			}
			defer recovery.Close()
			if _, err := recovery.ReadBridgeTuple(); !errors.Is(err, readErr) {
				t.Fatalf("%s read error = %v, want %v", group, err, readErr)
			}
			if calls != index+1 {
				t.Fatalf("reads before %s failure = %d, want %d", group, calls, index+1)
			}
		})
	}
}

func TestMMIOOpenRequiresNoFollowAndRootOwnedCharDevice(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	var gotFlags int
	mapper.open = func(_ string, flags int, _ uint32) (int, error) {
		gotFlags = flags
		return 41, nil
	}
	mapper.fstat = func(int) (mmioDeviceStat, error) {
		return mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 1}, nil
	}
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	defer registers.Close()
	if gotFlags&mmioONOFOLLOW == 0 {
		t.Fatalf("open flags %#x omit O_NOFOLLOW", gotFlags)
	}
}

func TestMMIORejectsEveryInvalidDeviceIdentityAndClosesDescriptor(t *testing.T) {
	for _, test := range []struct {
		name string
		stat mmioDeviceStat
	}{
		{name: "regular file", stat: mmioDeviceStat{mode: 0o100600, uid: 0, major: 1, minor: 1}},
		{name: "non-root owner", stat: mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 1000, major: 1, minor: 1}},
		{name: "wrong major", stat: mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 8, minor: 1}},
		{name: "wrong minor", stat: mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			closes := 0
			mapper.fstat = func(int) (mmioDeviceStat, error) { return test.stat, nil }
			mapper.close = func(int) error {
				closes++
				return nil
			}
			if _, err := mapper.OpenMailbox(); err == nil {
				t.Fatal("invalid MMIO device identity was accepted")
			}
			if closes != 1 || mapper.closeCalls != 1 {
				t.Fatalf("descriptor cleanup = callback:%d counter:%d, want one", closes, mapper.closeCalls)
			}
		})
	}
}

func TestMMIOFstatFailureClosesDescriptorForEachMappingKind(t *testing.T) {
	fstatErr := errors.New("fstat failure")
	for _, test := range []struct {
		name string
		open func(*Mapper) error
	}{
		{name: "mailbox", open: func(mapper *Mapper) error {
			_, err := mapper.OpenMailbox()
			return err
		}},
		{name: "recovery", open: func(mapper *Mapper) error {
			_, err := mapper.openRecovery(context.Background())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapper := newAnonymousMapperForTest(t)
			mapper.fstat = func(int) (mmioDeviceStat, error) { return mmioDeviceStat{}, fstatErr }
			if err := test.open(mapper); !errors.Is(err, fstatErr) {
				t.Fatalf("fstat error = %v, want %v", err, fstatErr)
			}
			if mapper.closeCalls != 1 || mapper.unmapCalls != 0 {
				t.Fatalf("fstat cleanup = close:%d unmap:%d, want 1/0", mapper.closeCalls, mapper.unmapCalls)
			}
		})
	}
}

func TestMMIOConcurrentCloseIsIdempotent(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	const callers = 16
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { results <- registers.Close() }()
	}
	for i := 0; i < callers; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent close: %v", err)
		}
	}
	if mapper.closeCalls != 1 || mapper.unmapCalls != 1 {
		t.Fatalf("concurrent cleanup counts = close:%d unmap:%d", mapper.closeCalls, mapper.unmapCalls)
	}
}

func TestMapperRecoveryAndDevelopmentMappingsAreDistinct(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	recovery, err := mapper.openRecovery(context.Background())
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	development, err := mapper.OpenMailbox()
	if err != nil {
		recovery.Close()
		t.Fatalf("OpenMailbox: %v", err)
	}
	if recovery.(*linuxRecoveryRegisters).resource == development.(*linuxRegisters).resource {
		t.Fatal("recovery and development mappings share a resource")
	}
	if err := recovery.Close(); err != nil {
		t.Fatalf("recovery close: %v", err)
	}
	if err := development.Close(); err != nil {
		t.Fatalf("development close: %v", err)
	}
	if mapper.closeCalls != 2 || mapper.unmapCalls != 6 {
		t.Fatalf("mapping cleanup = closes:%d unmaps:%d, want 2/6", mapper.closeCalls, mapper.unmapCalls)
	}
}

func TestMMIORecoveryCancellationAfterManagerMapClosesAcquiredResources(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	maps := 0
	mapper.mapFn = func(_ int, _ int64, length, _, _ int) ([]byte, error) {
		maps++
		if maps == 1 {
			cancel()
		}
		return make([]byte, length), nil
	}
	if _, err := mapper.openRecovery(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-map cancellation error = %v, want context.Canceled", err)
	}
	if maps != 1 || mapper.unmapCalls != 1 || mapper.closeCalls != 1 {
		t.Fatalf("post-map cleanup = maps:%d unmaps:%d closes:%d, want 1/1/1", maps, mapper.unmapCalls, mapper.closeCalls)
	}
}

func TestLinuxBridgeStateObserverRequiresExactDynamicInventoryAndDisabledTokens(t *testing.T) {
	root := "/anonymous/bridge-root"
	entries := []bridgeDirectoryEntry{
		{name: "br7", isDir: true},
		{name: "br2", isDir: true},
		{name: "br9", isDir: true},
		{name: "br4", isDir: true},
	}
	logical := map[string]string{
		"br2": "hps2fpga\n",
		"br4": "lwhps2fpga\n",
		"br7": "fpga2hps\n",
		"br9": "fpga2sdram\n",
	}
	reads := 0
	observer := &linuxBridgeStateObserver{
		root: root,
		readDir: func(string) ([]bridgeDirectoryEntry, error) {
			return append([]bridgeDirectoryEntry(nil), entries...), nil
		},
		readFile: func(path string) ([]byte, error) {
			reads++
			for entry, name := range logical {
				if path == filepath.Join(root, entry, "name") {
					return []byte(name), nil
				}
				if path == filepath.Join(root, entry, "state") {
					return []byte("disabled\n"), nil
				}
			}
			return nil, errors.New("unexpected fixture path")
		},
	}
	views, err := observer.VerifyBridgeViews(context.Background())
	if err != nil {
		t.Fatalf("VerifyBridgeViews: %v", err)
	}
	if !views.allDisabled() || reads != 8 {
		t.Fatalf("views/read count = %#v/%d, want all disabled and eight reads", views, reads)
	}

	for _, hostile := range []struct {
		name     string
		entries  []bridgeDirectoryEntry
		logical  map[string]string
		state    string
		dirErr   error
		readErr  error
		stateErr bool
	}{
		{name: "directory read error", dirErr: errors.New("bridge directory read")},
		{name: "duplicate logical name", entries: []bridgeDirectoryEntry{{name: "br0", isDir: true}, {name: "br1", isDir: true}}, logical: map[string]string{"br0": "hps2fpga\n", "br1": "hps2fpga\n"}, state: "disabled\n"},
		{name: "unexpected logical name", entries: []bridgeDirectoryEntry{{name: "br0", isDir: true}}, logical: map[string]string{"br0": "other\n"}, state: "disabled\n"},
		{name: "name missing newline", entries: []bridgeDirectoryEntry{{name: "br0", isDir: true}}, logical: map[string]string{"br0": "hps2fpga"}, state: "disabled\n"},
		{name: "missing newline", entries: []bridgeDirectoryEntry{{name: "br0", isDir: true}}, logical: map[string]string{"br0": "hps2fpga\n"}, state: "disabled"},
		{name: "malformed dynamic entry", entries: []bridgeDirectoryEntry{{name: "brx", isDir: true}}, logical: map[string]string{"brx": "hps2fpga\n"}, state: "disabled\n"},
		{name: "non-directory dynamic entry", entries: []bridgeDirectoryEntry{{name: "br5", isDir: false}}, logical: map[string]string{"br5": "hps2fpga\n"}, state: "disabled\n"},
		{name: "name attribute read error", entries: []bridgeDirectoryEntry{{name: "br5", isDir: true}}, readErr: errors.New("name attribute read")},
		{name: "state attribute read error", entries: []bridgeDirectoryEntry{{name: "br5", isDir: true}}, logical: map[string]string{"br5": "hps2fpga\n"}, state: "disabled\n", readErr: errors.New("state attribute read"), stateErr: true},
		{name: "logical whitespace", entries: []bridgeDirectoryEntry{{name: "br5", isDir: true}}, logical: map[string]string{"br5": "hps2fpga \n"}, state: "disabled\n"},
		{name: "enabled state", entries: []bridgeDirectoryEntry{{name: "br5", isDir: true}}, logical: map[string]string{"br5": "hps2fpga\n"}, state: "enabled\n"},
	} {
		t.Run(hostile.name, func(t *testing.T) {
			candidate := &linuxBridgeStateObserver{
				root: root,
				readDir: func(string) ([]bridgeDirectoryEntry, error) {
					if hostile.dirErr != nil {
						return nil, hostile.dirErr
					}
					return hostile.entries, nil
				},
				readFile: func(path string) ([]byte, error) {
					if hostile.readErr != nil && (!hostile.stateErr || strings.HasSuffix(path, "/state")) {
						return nil, hostile.readErr
					}
					for entry, name := range hostile.logical {
						if path == filepath.Join(root, entry, "name") {
							return []byte(name), nil
						}
						if path == filepath.Join(root, entry, "state") {
							return []byte(hostile.state), nil
						}
					}
					return nil, errors.New("unexpected fixture path")
				},
			}
			if _, err := candidate.VerifyBridgeViews(context.Background()); err == nil {
				t.Fatal("hostile bridge inventory was accepted")
			}
		})
	}
}

func TestMMIORejectsConflictingPhysicalAddressAliases(t *testing.T) {
	mapper := newAnonymousMapperForTest(t)
	mapper.physicalAddress = fpgaManagerBaseAddress
	mapper.address = fpgaManagerBaseAddress + 4096
	if _, err := mapper.OpenMailbox(); err == nil {
		t.Fatal("conflicting physical address aliases were accepted")
	}
}

func TestMMIORecoveryPartialMapFailuresCloseEveryAcquiredPage(t *testing.T) {
	for failAt := 0; failAt < 5; failAt++ {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			maps, unmaps, closes := 0, 0, 0
			mapErr := errors.New("map failure")
			mapper := &Mapper{
				pageSize: 4096,
				open:     func(string, int, uint32) (int, error) { return 19, nil },
				mapFn: func(_ int, _ int64, length, _, _ int) ([]byte, error) {
					call := maps
					maps++
					if call == failAt {
						return nil, mapErr
					}
					return make([]byte, length), nil
				},
				unmap: func([]byte) error { unmaps++; return nil },
				close: func(int) error { closes++; return nil },
				fstat: func(int) (mmioDeviceStat, error) {
					return mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 1}, nil
				},
			}
			if _, err := mapper.openRecovery(context.Background()); !errors.Is(err, mapErr) {
				t.Fatalf("OpenRecovery error = %v, want map failure", err)
			}
			if maps != failAt+1 || unmaps != failAt || closes != 1 {
				t.Fatalf("cleanup after map %d = maps:%d unmaps:%d closes:%d", failAt, maps, unmaps, closes)
			}
		})
	}
}

func TestMMIOInvalidMappingLengthUnmapsAndCloses(t *testing.T) {
	unmaps, closes := 0, 0
	mapper := &Mapper{
		pageSize: 4096,
		open:     func(string, int, uint32) (int, error) { return 23, nil },
		mapFn:    func(_ int, _ int64, length, _, _ int) ([]byte, error) { return make([]byte, length-1), nil },
		unmap:    func([]byte) error { unmaps++; return nil },
		close:    func(int) error { closes++; return nil },
		fstat: func(int) (mmioDeviceStat, error) {
			return mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 1}, nil
		},
	}
	if _, err := mapper.OpenMailbox(); err == nil {
		t.Fatal("invalid mapping length was accepted")
	}
	if unmaps != 1 || closes != 1 {
		t.Fatalf("invalid mapping cleanup = unmaps:%d closes:%d", unmaps, closes)
	}
}
