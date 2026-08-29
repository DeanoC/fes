//go:build linux && (amd64 || arm)

package fpgadev

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	fpgaManagerBaseAddress uint64 = 0xff706000

	// These are the only Linux physical register groups reachable during the
	// recovery exception.  Their offsets are kept with the group descriptor,
	// so a caller cannot supply an arbitrary physical address or register.
	bridgeFPGAInterfaceRegisterAddress uint64 = 0xffd08028
	bridgeSDRPortRegisterAddress       uint64 = 0xffc25080
	bridgeModuleResetRegisterAddress   uint64 = 0xffd0501c
	bridgeNIC301RemapRegisterAddress   uint64 = 0xff800000

	bridgeFPGAInterfaceAddress = bridgeFPGAInterfaceRegisterAddress &^ 0xfff
	bridgeSDRPortAddress       = bridgeSDRPortRegisterAddress &^ 0xfff
	bridgeModuleResetAddress   = bridgeModuleResetRegisterAddress &^ 0xfff
	bridgeNIC301RemapAddress   = bridgeNIC301RemapRegisterAddress &^ 0xfff

	bridgeFPGAInterfaceOffset = bridgeFPGAInterfaceRegisterAddress & 0xfff
	bridgeSDRPortOffset       = bridgeSDRPortRegisterAddress & 0xfff
	bridgeModuleResetOffset   = bridgeModuleResetRegisterAddress & 0xfff
	bridgeNIC301RemapOffset   = bridgeNIC301RemapRegisterAddress & 0xfff
)

type bridgeRegisterDescriptor struct {
	group   BridgeGroup
	address uint64
	offset  uint64
}

var bridgeRegisterDescriptors = [...]bridgeRegisterDescriptor{
	{group: BridgeFPGAInterface, address: bridgeFPGAInterfaceAddress, offset: bridgeFPGAInterfaceOffset},
	{group: BridgeSDRPort, address: bridgeSDRPortAddress, offset: bridgeSDRPortOffset},
	{group: BridgeModuleReset, address: bridgeModuleResetAddress, offset: bridgeModuleResetOffset},
	{group: BridgeNIC301Remap, address: bridgeNIC301RemapAddress, offset: bridgeNIC301RemapOffset},
}

type mmioRead32Func func([]byte, uint64) (uint32, error)
type mmioWrite32Func func([]byte, uint64, uint32) error
type mmioOpenFunc func(string, int, uint32) (int, error)
type mmioMapFunc func(int, int64, int, int, int) ([]byte, error)
type mmioUnmapFunc func([]byte) error
type mmioCloseFunc func(int) error
type mmioFstatFunc func(int) (mmioDeviceStat, error)

type mappedPage struct {
	data   []byte
	unmap  mmioUnmapFunc
	owner  *Mapper
	closed bool
}

// mappedResource owns the descriptor and every mmap acquired from it.  It
// serializes close and makes repeated close calls return the same cleanup
// result without invoking unmap/close a second time.
type mappedResource struct {
	mu       sync.Mutex
	fd       int
	closeFD  mmioCloseFunc
	owner    *Mapper
	pages    []*mappedPage
	closed   bool
	closeErr error
}

func (r *mappedResource) add(page *mappedPage) {
	r.mu.Lock()
	r.pages = append(r.pages, page)
	r.mu.Unlock()
}

func (r *mappedResource) close() error {
	return r.closeContext(context.Background())
}

// closeContext always completes every acquired unmap and descriptor close,
// even if the qualification context has expired.  Cancellation is observed
// and returned alongside cleanup errors, but it never permits a leaked page.
func (r *mappedResource) closeContext(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	var cleanup []error
	var contextErr error
	noteContext := func() {
		if contextErr == nil {
			contextErr = contextError(ctx)
		}
	}
	noteContext()
	for index := len(r.pages) - 1; index >= 0; index-- {
		page := r.pages[index]
		if page == nil || page.closed {
			continue
		}
		page.closed = true
		if page.unmap != nil {
			if err := page.unmap(page.data); err != nil {
				cleanup = append(cleanup, fmt.Errorf("unmap MMIO page: %w", err))
			}
		}
		noteContext()
	}
	if r.closeFD != nil {
		if err := r.closeFD(r.fd); err != nil {
			cleanup = append(cleanup, fmt.Errorf("close MMIO descriptor: %w", err))
		}
	}
	noteContext()
	if contextErr != nil {
		cleanup = append(cleanup, contextErr)
	}
	r.closeErr = errors.Join(cleanup...)
	return r.closeErr
}

type linuxRegisters struct {
	resource *mappedResource
	data     []byte
	gpo      uint64
	gpi      uint64
	read32   mmioRead32Func
	write32  mmioWrite32Func

	mu sync.Mutex
}

func (r *linuxRegisters) ReadGPI() (uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureOpen(); err != nil {
		return 0, err
	}
	return r.read32(r.data, r.gpi)
}

func (r *linuxRegisters) WriteGPO(value uint32) error {
	return r.writeGPOContext(context.Background(), value)
}

func (r *linuxRegisters) writeGPOContext(ctx context.Context, value uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if err := r.write32(r.data, r.gpo, value); err != nil {
		return fmt.Errorf("write GPO: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	readback, err := r.read32(r.data, r.gpo)
	if err != nil {
		return fmt.Errorf("read GPO writeback: %w", err)
	}
	if readback != value {
		return fmt.Errorf("GPO readback is %#08x, want %#08x", readback, value)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	return nil
}

func (r *linuxRegisters) ReadGPO() (uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureOpen(); err != nil {
		return 0, err
	}
	return r.read32(r.data, r.gpo)
}

func (r *linuxRegisters) ensureOpen() error {
	if r == nil || r.resource == nil {
		return errors.New("MMIO registers are nil")
	}
	r.resource.mu.Lock()
	closed := r.resource.closed
	r.resource.mu.Unlock()
	if closed {
		return errors.New("MMIO registers are closed")
	}
	return nil
}

func (r *linuxRegisters) Close() error {
	if r == nil || r.resource == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resource.close()
}

func (r *linuxRegisters) CloseContext(ctx context.Context) error {
	if r == nil || r.resource == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resource.closeContext(ctx)
}

func (r *linuxRegisters) ReadGPIContext(ctx context.Context) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	value, err := r.ReadGPI()
	if err != nil {
		return 0, err
	}
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	return value, nil
}

func (r *linuxRegisters) WriteGPOContext(ctx context.Context, value uint32) error {
	return r.writeGPOContext(ctx, value)
}

func (r *linuxRegisters) ReadGPOContext(ctx context.Context) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	value, err := r.ReadGPO()
	if err != nil {
		return 0, err
	}
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	return value, nil
}

type linuxRecoveryRegisters struct {
	*linuxRegisters
	bridges map[BridgeGroup]bridgeWord
}

// WriteGPO on a recovery mapping is intentionally narrower than the
// development Registers surface: qualification may clear inherited state but
// must never send START, ACK, or any other mailbox command before lease
// transfer.
func (r *linuxRecoveryRegisters) WriteGPO(value uint32) error {
	if value != 0 {
		return errors.New("recovery mapping permits only GPO zero")
	}
	return r.linuxRegisters.WriteGPO(value)
}

func (r *linuxRecoveryRegisters) WriteGPOContext(ctx context.Context, value uint32) error {
	if value != 0 {
		return errors.New("recovery mapping permits only GPO zero")
	}
	return r.linuxRegisters.WriteGPOContext(ctx, value)
}

type bridgeWord struct {
	page    []byte
	offset  uint64
	read32  mmioRead32Func
	write32 mmioWrite32Func
}

func (r *linuxRecoveryRegisters) DisableBridges(tuple BridgeTuple) error {
	return r.disableBridgesContext(context.Background(), tuple)
}

func (r *linuxRecoveryRegisters) disableBridgesContext(ctx context.Context, tuple BridgeTuple) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if !tuple.equal(pinnedBridgeTuple) {
		return errors.New("bridge disable tuple is not the pinned tuple")
	}
	values := map[BridgeGroup]uint32{
		BridgeFPGAInterface: tuple.FPGAInterfaceModule,
		BridgeSDRPort:       tuple.SDRPort,
		BridgeModuleReset:   tuple.BridgeModuleReset,
		BridgeNIC301Remap:   tuple.NIC301Remap,
	}
	for _, group := range pinnedBridgeGroups {
		if err := contextError(ctx); err != nil {
			return err
		}
		word, ok := r.bridges[group]
		if !ok {
			return fmt.Errorf("bridge group %q is not mapped", group)
		}
		if err := word.write32(word.page, word.offset, values[group]); err != nil {
			return fmt.Errorf("write bridge group %q: %w", group, err)
		}
		if err := contextError(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *linuxRecoveryRegisters) DisableBridgesContext(ctx context.Context, tuple BridgeTuple) error {
	return r.disableBridgesContext(ctx, tuple)
}

func (r *linuxRecoveryRegisters) ReadBridgeTuple() (BridgeTuple, error) {
	return r.readBridgeTupleContext(context.Background())
}

func (r *linuxRecoveryRegisters) readBridgeTupleContext(ctx context.Context) (BridgeTuple, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return BridgeTuple{}, err
	}
	return r.readBridgeTupleLocked()
}

func (r *linuxRecoveryRegisters) readBridgeTupleLocked() (BridgeTuple, error) {
	if err := r.ensureOpen(); err != nil {
		return BridgeTuple{}, err
	}
	values := make(map[BridgeGroup]uint32, 3)
	for _, group := range [...]BridgeGroup{BridgeFPGAInterface, BridgeSDRPort, BridgeModuleReset} {
		// The context-aware wrapper is used for qualification; the non-context
		// helper remains a synchronous compatibility surface.
		word, ok := r.bridges[group]
		if !ok {
			return BridgeTuple{}, fmt.Errorf("bridge group %q is not mapped", group)
		}
		value, err := word.read32(word.page, word.offset)
		if err != nil {
			return BridgeTuple{}, fmt.Errorf("read bridge group %q: %w", group, err)
		}
		values[group] = value
	}
	return BridgeTuple{
		FPGAInterfaceModule: values[BridgeFPGAInterface],
		SDRPort:             values[BridgeSDRPort],
		BridgeModuleReset:   values[BridgeModuleReset],
	}, nil
}

func (r *linuxRecoveryRegisters) ReadBridgeTupleContext(ctx context.Context) (BridgeTuple, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return BridgeTuple{}, err
	}
	if err := r.ensureOpen(); err != nil {
		return BridgeTuple{}, err
	}
	values := make(map[BridgeGroup]uint32, 3)
	for _, group := range [...]BridgeGroup{BridgeFPGAInterface, BridgeSDRPort, BridgeModuleReset} {
		if err := contextError(ctx); err != nil {
			return BridgeTuple{}, err
		}
		word, ok := r.bridges[group]
		if !ok {
			return BridgeTuple{}, fmt.Errorf("bridge group %q is not mapped", group)
		}
		value, err := word.read32(word.page, word.offset)
		if err != nil {
			return BridgeTuple{}, fmt.Errorf("read bridge group %q: %w", group, err)
		}
		if err := contextError(ctx); err != nil {
			return BridgeTuple{}, err
		}
		values[group] = value
	}
	return BridgeTuple{FPGAInterfaceModule: values[BridgeFPGAInterface], SDRPort: values[BridgeSDRPort], BridgeModuleReset: values[BridgeModuleReset]}, nil
}

func (r *linuxRecoveryRegisters) ReadProgramming() (ProgrammingProof, error) {
	return r.readProgrammingContext(context.Background())
}

func (r *linuxRecoveryRegisters) readProgrammingContext(ctx context.Context) (ProgrammingProof, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return ProgrammingProof{}, err
	}
	if err := r.ensureOpen(); err != nil {
		return ProgrammingProof{}, err
	}
	status, err := r.read32(r.data, fpgaManagerStatusOffset)
	if err != nil {
		return ProgrammingProof{}, fmt.Errorf("read FPGA-manager status: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return ProgrammingProof{}, err
	}
	control, err := r.read32(r.data, fpgaManagerControlOffset)
	if err != nil {
		return ProgrammingProof{}, fmt.Errorf("read FPGA-manager control: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return ProgrammingProof{}, err
	}
	return ProgrammingProof{
		UserMode:                status&programmingUserModeMask == programmingUserModeValue,
		DriveReleased:           control&programmingForbiddenMask == 0,
		EnableClear:             control&programmingEnableMask == 0,
		AXICFGENClear:           control&programmingAXICFGENMask == 0,
		ConfigurationPullsClear: control&programmingPullMask == 0,
		Status:                  status,
		Control:                 control,
		// Process and mapping ownership is not visible in the FPGA-manager
		// register page. The qualification process supplies that independent
		// absence proof; this adapter never infers it.
		NoProgrammingProcess: false,
		NoProgrammingMapping: false,
	}, nil
}

func (r *linuxRecoveryRegisters) ReadProgrammingContext(ctx context.Context) (ProgrammingProof, error) {
	return r.readProgrammingContext(ctx)
}

func (m *Mapper) mapperPath() string {
	if m.devicePath != "" {
		return m.devicePath
	}
	if m.path != "" {
		return m.path
	}
	return "/dev/mem"
}

func (m *Mapper) mapperAddress() uint64 {
	if m.physicalAddress != 0 {
		return m.physicalAddress
	}
	if m.address != 0 {
		return m.address
	}
	return fpgaManagerBaseAddress
}

func (m *Mapper) mapperPageSize() int {
	if m.pageSize != 0 {
		return m.pageSize
	}
	return unix.Getpagesize()
}

func (m *Mapper) mapperLength(pageSize int) int {
	if m.mappingLength != 0 {
		return m.mappingLength
	}
	return pageSize
}

func (m *Mapper) openFD(path string, flags int, mode uint32) (int, error) {
	if m != nil {
		switch f := m.open.(type) {
		case mmioOpenFunc:
			return f(path, flags, mode)
		case func(string, int, uint32) (int, error):
			return f(path, flags, mode)
		case func(string, int) (int, error):
			return f(path, flags)
		case func(string) (int, error):
			return f(path)
		case nil:
		default:
			return -1, errors.New("invalid MMIO opener seam")
		}
	}
	return unix.Open(path, flags, mode)
}

func (m *Mapper) mapRegion(fd int, offset int64, length, prot, flags int) ([]byte, error) {
	if m != nil {
		switch f := m.mapFn.(type) {
		case mmioMapFunc:
			return f(fd, offset, length, prot, flags)
		case func(int, int64, int, int, int) ([]byte, error):
			return f(fd, offset, length, prot, flags)
		case func(uint64, int) ([]byte, error):
			return f(uint64(offset), length)
		case func(int64, int) ([]byte, error):
			return f(offset, length)
		case func(int, int) ([]byte, error):
			return f(int(offset), length)
		case nil:
		default:
			return nil, errors.New("invalid MMIO mapper seam")
		}
	}
	return unix.Mmap(fd, offset, length, prot, flags)
}

func (m *Mapper) unmapRegion(data []byte) error {
	if m != nil {
		m.mu.Lock()
		m.unmapCalls++
		m.mu.Unlock()
		switch f := m.unmap.(type) {
		case mmioUnmapFunc:
			return f(data)
		case func([]byte) error:
			return f(data)
		case func([]byte):
			f(data)
			return nil
		case nil:
		default:
			return errors.New("invalid MMIO unmapper seam")
		}
	}
	return unix.Munmap(data)
}

func (m *Mapper) closeFD(fd int) error {
	if m != nil {
		m.mu.Lock()
		m.closeCalls++
		m.mu.Unlock()
		switch f := m.close.(type) {
		case mmioCloseFunc:
			return f(fd)
		case func(int) error:
			return f(fd)
		case func(int):
			f(fd)
			return nil
		case nil:
		default:
			return errors.New("invalid MMIO closer seam")
		}
	}
	return unix.Close(fd)
}

func (m *Mapper) statFD(fd int) (mmioDeviceStat, error) {
	if m != nil {
		switch f := m.fstat.(type) {
		case mmioFstatFunc:
			return f(fd)
		case func(int) (mmioDeviceStat, error):
			return f(fd)
		case nil:
		default:
			return mmioDeviceStat{}, errors.New("invalid MMIO fstat seam")
		}
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return mmioDeviceStat{}, err
	}
	return mmioDeviceStat{
		mode:  uint32(stat.Mode),
		uid:   uint32(stat.Uid),
		major: uint32(unix.Major(uint64(stat.Rdev))),
		minor: uint32(unix.Minor(uint64(stat.Rdev))),
	}, nil
}

func validateMMIODevice(stat mmioDeviceStat) error {
	if stat.mode&unixSIFMT != unixSIFCHR {
		return errors.New("MMIO device is not a character device")
	}
	if stat.uid != 0 {
		return errors.New("MMIO device is not root-owned")
	}
	if stat.major != 1 || stat.minor != 1 {
		return fmt.Errorf("MMIO device number is %d:%d, want 1:1", stat.major, stat.minor)
	}
	return nil
}

func (m *Mapper) readFunction() mmioRead32Func {
	if m != nil {
		switch f := m.read32.(type) {
		case mmioRead32Func:
			return f
		case func([]byte, uint64) (uint32, error):
			return f
		case func([]byte, int) (uint32, error):
			return func(data []byte, offset uint64) (uint32, error) { return f(data, int(offset)) }
		case nil:
		default:
			return func([]byte, uint64) (uint32, error) { return 0, errors.New("invalid MMIO read seam") }
		}
	}
	return readMMIO32
}

func (m *Mapper) writeFunction() mmioWrite32Func {
	if m != nil {
		switch f := m.write32.(type) {
		case mmioWrite32Func:
			return f
		case func([]byte, uint64, uint32) error:
			return f
		case func([]byte, int, uint32) error:
			return func(data []byte, offset uint64, value uint32) error { return f(data, int(offset), value) }
		case nil:
		default:
			return func([]byte, uint64, uint32) error { return errors.New("invalid MMIO write seam") }
		}
	}
	return writeMMIO32
}

func validateMMIOLayout(address uint64, pageSize, length int, gpoOffset, gpiOffset uint64) error {
	if pageSize <= 0 || pageSize&(pageSize-1) != 0 || pageSize < 4 {
		return errors.New("MMIO page size is invalid")
	}
	if length != pageSize {
		return errors.New("MMIO mapping length must be exactly one page")
	}
	if address%uint64(pageSize) != 0 {
		return errors.New("MMIO physical address is not page aligned")
	}
	if address > math.MaxUint64-uint64(length) {
		return errors.New("MMIO physical address overflows mapping")
	}
	if gpoOffset%4 != 0 || gpiOffset%4 != 0 {
		return errors.New("MMIO register offset is not 32-bit aligned")
	}
	for _, offset := range []uint64{gpoOffset, gpiOffset} {
		if offset > uint64(length)-4 {
			return errors.New("MMIO register offset is outside mapping")
		}
	}
	if address > math.MaxInt64 {
		return errors.New("MMIO mapping offset does not fit Linux off_t")
	}
	return nil
}

func validateMMIOOffset(data []byte, offset uint64) error {
	if offset%4 != 0 {
		return errors.New("MMIO register offset is not 32-bit aligned")
	}
	if offset > uint64(len(data)) || uint64(len(data))-offset < 4 {
		return errors.New("MMIO register offset is outside mapping")
	}
	return nil
}

// readMMIO32 uses sequentially consistent atomics for ARM/Linux compiler and
// CPU ordering, while the byte order is explicitly documented as little
// endian by the Cyclone V manager contract.  The supported Linux target is
// little endian; binary helpers make the representation visible in review.
func readMMIO32(data []byte, offset uint64) (uint32, error) {
	if err := validateMMIOOffset(data, offset); err != nil {
		return 0, err
	}
	ptr, err := mmioWordPointer(data, offset)
	if err != nil {
		return 0, err
	}
	raw := atomic.LoadUint32(ptr)
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], raw)
	runtime.KeepAlive(data)
	return binary.LittleEndian.Uint32(encoded[:]), nil
}

func writeMMIO32(data []byte, offset uint64, value uint32) error {
	if err := validateMMIOOffset(data, offset); err != nil {
		return err
	}
	ptr, err := mmioWordPointer(data, offset)
	if err != nil {
		return err
	}
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	raw := binary.LittleEndian.Uint32(encoded[:])
	atomic.StoreUint32(ptr, raw)
	runtime.KeepAlive(data)
	return nil
}

func mmioWordPointer(data []byte, offset uint64) (*uint32, error) {
	ptr := unsafe.Pointer(&data[int(offset)])
	if uintptr(ptr)%4 != 0 {
		return nil, errors.New("MMIO register address is not 32-bit aligned")
	}
	return (*uint32)(ptr), nil
}

func (m *Mapper) openMappedPage(fd int, address uint64, pageSize, length int, resource *mappedResource) (*mappedPage, error) {
	data, err := m.mapRegion(fd, int64(address), length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		mappingErr := fmt.Errorf("map MMIO page: %w", err)
		if len(data) != 0 {
			if unmapErr := m.unmapRegion(data); unmapErr != nil {
				return nil, errors.Join(mappingErr, fmt.Errorf("unmap partial MMIO page: %w", unmapErr))
			}
		}
		return nil, mappingErr
	}
	page := &mappedPage{data: data, unmap: m.unmapRegion, owner: m}
	if len(data) != length {
		unmapErr := page.unmap(data)
		page.closed = true
		if unmapErr != nil {
			return nil, errors.Join(errors.New("MMIO mapper returned an invalid mapping length"), fmt.Errorf("unmap invalid MMIO page: %w", unmapErr))
		}
		return nil, errors.New("MMIO mapper returned an invalid mapping length")
	}
	resource.add(page)
	return page, nil
}

func (m *Mapper) validateConfig() (uint64, int, int, error) {
	if m == nil {
		return 0, 0, 0, ErrUnsupported
	}
	if m.devicePath != "" && m.path != "" && m.devicePath != m.path {
		return 0, 0, 0, errors.New("MMIO device path aliases conflict")
	}
	if m.physicalAddress != 0 && m.address != 0 && m.physicalAddress != m.address {
		return 0, 0, 0, errors.New("MMIO physical address aliases conflict")
	}
	address := m.mapperAddress()
	if address != fpgaManagerBaseAddress {
		return 0, 0, 0, errors.New("MMIO address is outside the FPGA-manager allowlist")
	}
	pageSize := m.mapperPageSize()
	length := m.mapperLength(pageSize)
	if err := validateMMIOLayout(address, pageSize, length, fpgaManagerGPOOffset, fpgaManagerGPIOffset); err != nil {
		return 0, 0, 0, err
	}
	return address, pageSize, length, nil
}

func (m *Mapper) supportsPlatform() bool {
	if runtime.GOARCH == "arm" {
		return true
	}
	// amd64 is a software-test target only.  Require every descriptor and
	// mapping seam before allowing an operation, so a partial fixture can never
	// fall through to a real /dev/mem open or host mmap.  A production NewMapper
	// has none of these private seams and is therefore unsupported on amd64.
	return runtime.GOARCH == "amd64" && m != nil &&
		m.open != nil && m.mapFn != nil && m.unmap != nil && m.close != nil && m.fstat != nil
}

func (m *Mapper) openFailure(fd int, openErr error) error {
	primary := fmt.Errorf("open MMIO device: %w", openErr)
	if fd < 0 {
		return primary
	}
	if closeErr := m.closeFD(fd); closeErr != nil {
		return errors.Join(primary, fmt.Errorf("close MMIO descriptor after open failure: %w", closeErr))
	}
	return primary
}

// OpenMailbox creates the separate post-transfer development mapping.  It
// maps only one page and leaves ownership of that mapping in Registers.Close.
func (m *Mapper) OpenMailbox() (Registers, error) {
	if m == nil {
		return nil, ErrUnsupported
	}
	if !m.supportsPlatform() {
		return nil, ErrUnsupported
	}
	address, pageSize, length, err := m.validateConfig()
	if err != nil {
		return nil, err
	}
	fd, err := m.openFD(m.mapperPath(), unix.O_RDWR|unix.O_SYNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, m.openFailure(fd, err)
	}
	if fd < 0 {
		return nil, errors.New("open MMIO device returned an invalid descriptor")
	}
	resource := &mappedResource{fd: fd, closeFD: m.closeFD, owner: m}
	stat, err := m.statFD(fd)
	if err != nil {
		closeErr := resource.close()
		if closeErr != nil {
			return nil, errors.Join(fmt.Errorf("fstat MMIO device: %w", err), closeErr)
		}
		return nil, fmt.Errorf("fstat MMIO device: %w", err)
	}
	if err := validateMMIODevice(stat); err != nil {
		closeErr := resource.close()
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	page, err := m.openMappedPage(fd, address, pageSize, length, resource)
	if err != nil {
		closeErr := resource.close()
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	registers := &linuxRegisters{
		resource: resource,
		data:     page.data,
		gpo:      fpgaManagerGPOOffset,
		gpi:      fpgaManagerGPIOffset,
		read32:   m.readFunction(),
		write32:  m.writeFunction(),
	}
	return registers, nil
}

// openRecovery creates the temporary qualification mapping. It has the
// manager page plus exactly four pinned bridge pages.  Every partial-open path
// closes all pages acquired so far and the descriptor exactly once.
func (m *Mapper) openRecovery(ctx context.Context) (recoveryRegisters, error) {
	if m == nil {
		return nil, ErrUnsupported
	}
	if !m.supportsPlatform() {
		return nil, ErrUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	address, pageSize, length, err := m.validateConfig()
	if err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	fd, err := m.openFD(m.mapperPath(), unix.O_RDWR|unix.O_SYNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, m.openFailure(fd, err)
	}
	if fd < 0 {
		return nil, errors.New("open MMIO device returned an invalid descriptor")
	}
	resource := &mappedResource{fd: fd, closeFD: m.closeFD, owner: m}
	if err := contextError(ctx); err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	stat, err := m.statFD(fd)
	if err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(fmt.Errorf("fstat MMIO device: %w", err), closeErr)
		}
		return nil, fmt.Errorf("fstat MMIO device: %w", err)
	}
	if err := contextError(ctx); err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	if err := validateMMIODevice(stat); err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	managerPage, err := m.openMappedPage(fd, address, pageSize, length, resource)
	if err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	read32, write32 := m.readFunction(), m.writeFunction()
	recovery := &linuxRecoveryRegisters{
		linuxRegisters: &linuxRegisters{
			resource: resource,
			data:     managerPage.data,
			gpo:      fpgaManagerGPOOffset,
			gpi:      fpgaManagerGPIOffset,
			read32:   read32,
			write32:  write32,
		},
		bridges: make(map[BridgeGroup]bridgeWord, len(bridgeRegisterDescriptors)),
	}
	for _, descriptor := range bridgeRegisterDescriptors {
		if err := contextError(ctx); err != nil {
			closeErr := resource.closeContext(ctx)
			if closeErr != nil {
				return nil, errors.Join(err, closeErr)
			}
			return nil, err
		}
		if descriptor.address%uint64(pageSize) != 0 {
			err = fmt.Errorf("bridge group %q address is not page aligned", descriptor.group)
			break
		}
		if descriptor.offset%4 != 0 || descriptor.offset > uint64(length)-4 {
			err = fmt.Errorf("bridge group %q register offset is invalid", descriptor.group)
			break
		}
		page, pageErr := m.openMappedPage(fd, descriptor.address, pageSize, length, resource)
		if pageErr != nil {
			err = fmt.Errorf("map bridge group %q: %w", descriptor.group, pageErr)
			break
		}
		recovery.bridges[descriptor.group] = bridgeWord{page: page.data, offset: descriptor.offset, read32: read32, write32: write32}
		if err := contextError(ctx); err != nil {
			closeErr := resource.closeContext(ctx)
			if closeErr != nil {
				return nil, errors.Join(err, closeErr)
			}
			return nil, err
		}
	}
	if err != nil {
		closeErr := resource.closeContext(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return recovery, nil
}

type bridgeDirectoryEntry struct {
	name  string
	isDir bool
}

type linuxBridgeStateObserver struct {
	root     string
	readDir  func(string) ([]bridgeDirectoryEntry, error)
	readFile func(string) ([]byte, error)
}

func newProductionBridgeStateObserver() BridgeVerifier {
	return &linuxBridgeStateObserver{
		root: "/sys/class/fpga_bridge",
		readDir: func(root string) ([]bridgeDirectoryEntry, error) {
			entries, err := os.ReadDir(root)
			if err != nil {
				return nil, err
			}
			out := make([]bridgeDirectoryEntry, 0, len(entries))
			for _, entry := range entries {
				// Class entries are commonly symlinks to the real bridge
				// directories. Stat the fixed-root child so valid class links
				// are accepted while regular-file extras still fail closed.
				info, statErr := os.Stat(filepath.Join(root, entry.Name()))
				if statErr != nil {
					return nil, statErr
				}
				out = append(out, bridgeDirectoryEntry{name: entry.Name(), isDir: info.IsDir()})
			}
			return out, nil
		},
		readFile: os.ReadFile,
	}
}

func (o *linuxBridgeStateObserver) VerifyBridgeViews(ctx context.Context) (BridgeViews, error) {
	if o == nil || o.readDir == nil || o.readFile == nil {
		return BridgeViews{}, errors.New("Linux bridge-state observer is unavailable")
	}
	if err := contextError(ctx); err != nil {
		return BridgeViews{}, err
	}
	entries, err := o.readDir(o.root)
	if err != nil {
		return BridgeViews{}, fmt.Errorf("enumerate Linux bridge entries: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return BridgeViews{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return BridgeViews{}, err
		}
		if !entry.isDir || len(entry.name) < 3 || !strings.HasPrefix(entry.name, "br") {
			return BridgeViews{}, fmt.Errorf("unexpected Linux bridge entry %q", entry.name)
		}
		directoryNumber := entry.name[2:]
		for _, digit := range directoryNumber {
			if digit < '0' || digit > '9' {
				return BridgeViews{}, fmt.Errorf("Linux bridge entry %q is not a dynamic brN directory", entry.name)
			}
		}
		if _, err := strconv.ParseUint(directoryNumber, 10, 32); err != nil {
			return BridgeViews{}, fmt.Errorf("Linux bridge entry %q is not a dynamic brN directory", entry.name)
		}
		nameRaw, err := o.readFile(filepath.Join(o.root, entry.name, "name"))
		if err != nil {
			return BridgeViews{}, fmt.Errorf("read Linux bridge %q name: %w", entry.name, err)
		}
		if err := contextError(ctx); err != nil {
			return BridgeViews{}, err
		}
		if len(nameRaw) == 0 || nameRaw[len(nameRaw)-1] != '\n' {
			return BridgeViews{}, fmt.Errorf("Linux bridge %q name attribute is not newline-terminated", entry.name)
		}
		logical := strings.TrimSuffix(string(nameRaw), "\n")
		if logical == "" || strings.ContainsAny(logical, "\r\n\t ") {
			return BridgeViews{}, fmt.Errorf("Linux bridge %q has invalid logical name", entry.name)
		}
		if _, exists := seen[logical]; exists {
			return BridgeViews{}, fmt.Errorf("duplicate Linux bridge logical name %q", logical)
		}
		seen[logical] = entry.name
		state, err := o.readFile(filepath.Join(o.root, entry.name, "state"))
		if err != nil {
			return BridgeViews{}, fmt.Errorf("read Linux bridge %q state: %w", entry.name, err)
		}
		if err := contextError(ctx); err != nil {
			return BridgeViews{}, err
		}
		if string(state) != "disabled\n" {
			return BridgeViews{}, fmt.Errorf("Linux bridge %q state is not exact disabled token", logical)
		}
	}
	expected := map[string]bool{
		"hps2fpga":   false,
		"lwhps2fpga": false,
		"fpga2hps":   false,
		"fpga2sdram": false,
	}
	if len(seen) != len(expected) {
		return BridgeViews{}, fmt.Errorf("Linux bridge logical inventory has %d entries, want %d", len(seen), len(expected))
	}
	for name := range seen {
		if _, ok := expected[name]; !ok {
			return BridgeViews{}, fmt.Errorf("unexpected Linux bridge logical name %q", name)
		}
		expected[name] = true
	}
	if !expected["hps2fpga"] || !expected["lwhps2fpga"] || !expected["fpga2hps"] || !expected["fpga2sdram"] {
		return BridgeViews{}, errors.New("Linux bridge logical inventory is incomplete")
	}
	if err := contextError(ctx); err != nil {
		return BridgeViews{}, err
	}
	return BridgeViews{
		FPGA2HPSDisabled:   true,
		HPS2FPGADisabled:   true,
		LWHPS2FPGADisabled: true,
		FPGASDRAMDisabled:  true,
		Entries: []BridgeStateView{
			{Name: "hps2fpga", State: "disabled\n"},
			{Name: "lwhps2fpga", State: "disabled\n"},
			{Name: "fpga2hps", State: "disabled\n"},
			{Name: "fpga2sdram", State: "disabled\n"},
		},
	}, nil
}
