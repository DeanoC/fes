// SPDX-License-Identifier: GPL-3.0-or-later

// Package psci implements ARM PSCI (DEN0022D/E) system power/reset controls
// and ARM SMCCC TRNG (DEN0098 v1.0) hardware entropy queries over an SMC or
// HVC conduit.
package psci

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ARM PSCI and SMCCC function IDs (DEN0022D/E, DEN0028).
const (
	FuncSMCCCVersion uint32 = 0x80000000 // SMCCC_VERSION (SMC32/HVC32)
	FuncVersion      uint32 = 0x84000000 // PSCI_VERSION (SMC32/HVC32)
	FuncSystemOff    uint32 = 0x84000008 // PSCI_SYSTEM_OFF (SMC32/HVC32)
	FuncSystemReset  uint32 = 0x84000009 // PSCI_SYSTEM_RESET (SMC32/HVC32)
	FuncFeatures     uint32 = 0x8400000a // PSCI_FEATURES (SMC32/HVC32)
	FuncAffinityInfo uint32 = 0xc4000004 // PSCI_AFFINITY_INFO (SMC64/HVC64)
	FuncSystemReset2 uint32 = 0xc4000012 // PSCI_SYSTEM_RESET2 (SMC64/HVC64)
)

// ARM SMCCC TRNG function IDs (DEN0098 v1.0).
const (
	FuncTRNGVersion  uint32 = 0x84000050 // TRNG_VERSION (SMC32/HVC32)
	FuncTRNGFeatures uint32 = 0x84000051 // TRNG_FEATURES (SMC32/HVC32)
	FuncTRNGRnd32    uint32 = 0x84000053 // TRNG_RND (SMC32/HVC32, up to 96 bits)
	FuncTRNGRnd64    uint32 = 0xc4000053 // TRNG_RND (SMC64/HVC64, up to 192 bits)
)

// PSCI and SMCCC TRNG signed status codes.
const (
	statusSuccess           int32 = 0
	statusNotSupported      int32 = -1
	statusInvalidParameters int32 = -2
	statusDenied            int32 = -3
	statusNoEntropy         int32 = -3
	statusInternalFailure   int32 = -6
)

const trngMaxRetries = 64

var (
	ErrUnsupported       = errors.New("psci: operation not supported")
	ErrInvalidParameters = errors.New("psci: invalid parameters")
	ErrDenied            = errors.New("psci: operation denied")
	ErrInternalFailure   = errors.New("psci: internal firmware failure")
	ErrNoEntropy         = errors.New("psci: TRNG entropy unavailable")
	ErrReturned          = errors.New("psci: terminal power/reset call returned unexpectedly")
)

// Conduit executes a single synchronous ARM SMCCC call (SMC #0 or HVC #0).
type Conduit interface {
	Call(x0, x1, x2, x3 uint64) (r0, r1, r2, r3 uint64)
}

// Client wraps an SMCCC conduit for PSCI power management and SMCCC TRNG calls.
type Client struct {
	conduit Conduit
}

// New creates a PSCI/TRNG client bound to conduit. A nil conduit is valid and
// causes all calls to return ErrUnsupported.
func New(conduit Conduit) *Client {
	return &Client{conduit: conduit}
}

func (c *Client) call(fid uint32, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64, error) {
	if c == nil || c.conduit == nil {
		return 0, 0, 0, 0, ErrUnsupported
	}
	r0, r1, r2, r3 := c.conduit.Call(uint64(fid), x1, x2, x3)
	return r0, r1, r2, r3, nil
}

func psciError(code int32) error {
	switch code {
	case statusSuccess:
		return nil
	case statusNotSupported:
		return ErrUnsupported
	case statusInvalidParameters:
		return ErrInvalidParameters
	case statusDenied:
		return ErrDenied
	case statusInternalFailure:
		return ErrInternalFailure
	default:
		return fmt.Errorf("psci: firmware error %d", code)
	}
}

func trngError(code int64) error {
	switch code {
	case int64(statusSuccess):
		return nil
	case int64(statusNotSupported):
		return ErrUnsupported
	case int64(statusInvalidParameters):
		return ErrInvalidParameters
	case int64(statusNoEntropy):
		return ErrNoEntropy
	default:
		return fmt.Errorf("psci: TRNG firmware error %d", code)
	}
}

// AffinityState reports the power state returned by PSCI_AFFINITY_INFO.
type AffinityState uint8

const (
	AffinityOn        AffinityState = 0
	AffinityOff       AffinityState = 1
	AffinityOnPending AffinityState = 2
)

// String returns the human-readable PSCI affinity state name.
func (s AffinityState) String() string {
	switch s {
	case AffinityOn:
		return "on"
	case AffinityOff:
		return "off"
	case AffinityOnPending:
		return "on-pending"
	default:
		return fmt.Sprintf("AffinityState(%d)", uint8(s))
	}
}

// Capabilities summarizes the PSCI, SMCCC, and SMCCC TRNG features advertised
// by firmware on a Client's conduit.
type Capabilities struct {
	PSCIMajor    uint16
	PSCIMinor    uint16
	SMCCCMajor   uint16
	SMCCCMinor   uint16
	HasSMCCC     bool
	SystemOff    bool
	SystemReset  bool
	SystemReset2 bool
	AffinityInfo bool
	TRNGMajor    uint16
	TRNGMinor    uint16
	TRNG64       bool
	TRNG32       bool
}

// Version queries PSCI_VERSION and returns (major, minor).
func (c *Client) Version() (major, minor uint16, err error) {
	r0, _, _, _, err := c.call(FuncVersion, 0, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, 0, psciError(v)
	}
	return uint16((uint32(v) >> 16) & 0x7fff), uint16(uint32(v) & 0xffff), nil
}

// SMCCCVersion queries SMCCC_VERSION (0x80000000) and returns (major, minor).
func (c *Client) SMCCCVersion() (major, minor uint16, err error) {
	r0, _, _, _, err := c.call(FuncSMCCCVersion, 0, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, 0, psciError(v)
	}
	return uint16((uint32(v) >> 16) & 0x7fff), uint16(uint32(v) & 0xffff), nil
}

// AffinityInfo queries PSCI_AFFINITY_INFO for targetAffinity at lowestAffinityLevel.
func (c *Client) AffinityInfo(targetAffinity uint64, lowestAffinityLevel uint32) (AffinityState, error) {
	r0, _, _, _, err := c.call(FuncAffinityInfo, targetAffinity, uint64(lowestAffinityLevel), 0)
	if err != nil {
		return 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, psciError(v)
	}
	if v > int32(AffinityOnPending) {
		return 0, fmt.Errorf("psci: unknown affinity state %d", v)
	}
	return AffinityState(v), nil
}

// Probe discovers the firmware PSCI, SMCCC, and TRNG versions and supported
// functions on c's conduit without triggering any state transitions.
func (c *Client) Probe() (Capabilities, error) {
	psciMaj, psciMin, err := c.Version()
	if err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{
		PSCIMajor: psciMaj,
		PSCIMinor: psciMin,
	}
	if smcccMaj, smcccMin, err := c.SMCCCVersion(); err == nil {
		caps.HasSMCCC = true
		caps.SMCCCMajor = smcccMaj
		caps.SMCCCMinor = smcccMin
	}
	if psciMaj >= 1 {
		caps.SystemOff = c.Supports(FuncSystemOff)
		caps.SystemReset = c.Supports(FuncSystemReset)
		caps.SystemReset2 = c.Supports(FuncSystemReset2)
		caps.AffinityInfo = c.Supports(FuncAffinityInfo)
	} else if psciMaj == 0 && psciMin >= 2 {
		// PSCI 0.2 requires SYSTEM_OFF, SYSTEM_RESET, and AFFINITY_INFO without PSCI_FEATURES.
		caps.SystemOff = true
		caps.SystemReset = true
		caps.AffinityInfo = true
	}
	if trngMaj, trngMin, err := c.TRNGVersion(); err == nil && trngMaj >= 1 {
		caps.TRNGMajor = trngMaj
		caps.TRNGMinor = trngMin
		_, err64 := c.TRNGFeatures(FuncTRNGRnd64)
		caps.TRNG64 = err64 == nil
		_, err32 := c.TRNGFeatures(FuncTRNGRnd32)
		caps.TRNG32 = err32 == nil
	}
	return caps, nil
}

// Features queries PSCI_FEATURES for a given PSCI function ID.
func (c *Client) Features(funcID uint32) (uint32, error) {
	r0, _, _, _, err := c.call(FuncFeatures, uint64(funcID), 0, 0)
	if err != nil {
		return 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, psciError(v)
	}
	return uint32(v), nil
}

// Supports reports whether PSCI_FEATURES advertises funcID as supported.
func (c *Client) Supports(funcID uint32) bool {
	_, err := c.Features(funcID)
	return err == nil
}

// SystemOff invokes PSCI_SYSTEM_OFF. On hardware/QEMU this call does not return
// on success; any return yields a non-nil error.
func (c *Client) SystemOff() error {
	r0, _, _, _, err := c.call(FuncSystemOff, 0, 0, 0)
	if err != nil {
		return err
	}
	if err := psciError(int32(r0)); err != nil {
		return err
	}
	return ErrReturned
}

// SystemReset invokes PSCI_SYSTEM_RESET (cold system reboot). On hardware/QEMU
// this call does not return on success; any return yields a non-nil error.
func (c *Client) SystemReset() error {
	r0, _, _, _, err := c.call(FuncSystemReset, 0, 0, 0)
	if err != nil {
		return err
	}
	if err := psciError(int32(r0)); err != nil {
		return err
	}
	return ErrReturned
}

// SystemReset2 invokes PSCI_SYSTEM_RESET2 (PSCI >= 1.1, e.g. architectural warm
// reset with resetType=0). On success this call does not return.
func (c *Client) SystemReset2(resetType uint32, cookie uint64) error {
	r0, _, _, _, err := c.call(FuncSystemReset2, uint64(resetType), cookie, 0)
	if err != nil {
		return err
	}
	if err := psciError(int32(r0)); err != nil {
		return err
	}
	return ErrReturned
}

// TRNGVersion queries SMCCC TRNG_VERSION (DEN0098) and returns (major, minor).
func (c *Client) TRNGVersion() (major, minor uint16, err error) {
	r0, _, _, _, err := c.call(FuncTRNGVersion, 0, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, 0, trngError(int64(v))
	}
	return uint16((uint32(v) >> 16) & 0x7fff), uint16(uint32(v) & 0xffff), nil
}

// TRNGFeatures queries SMCCC TRNG_FEATURES for a given TRNG function ID.
func (c *Client) TRNGFeatures(funcID uint32) (uint32, error) {
	r0, _, _, _, err := c.call(FuncTRNGFeatures, uint64(funcID), 0, 0)
	if err != nil {
		return 0, err
	}
	v := int32(r0)
	if v < 0 {
		return 0, trngError(int64(v))
	}
	return uint32(v), nil
}

// ReadEntropy fills buf from SMCCC TRNG_RND64 (or TRNG_RND32 if 64-bit is not
// supported by firmware). Per DEN0098 §2.5.2, returned entropy bits are packed
// starting from x3/w3, then x2/w2, then x1/w1.
func (c *Client) ReadEntropy(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	major, _, err := c.TRNGVersion()
	if err != nil {
		return 0, err
	}
	if major < 1 {
		return 0, ErrUnsupported
	}
	use64 := false
	if _, err := c.TRNGFeatures(FuncTRNGRnd64); err == nil {
		use64 = true
	} else if _, err := c.TRNGFeatures(FuncTRNGRnd32); err != nil {
		return 0, ErrUnsupported
	}

	written := 0
	for written < len(buf) {
		rem := len(buf) - written
		var chunk [24]byte
		var chunkLen int
		if use64 {
			chunkLen = min(rem, 24)
			r1, r2, r3, err := c.trngCall(FuncTRNGRnd64, uint64(chunkLen*8), true)
			if err != nil {
				return written, err
			}
			binary.LittleEndian.PutUint64(chunk[0:8], r3)
			binary.LittleEndian.PutUint64(chunk[8:16], r2)
			binary.LittleEndian.PutUint64(chunk[16:24], r1)
		} else {
			chunkLen = min(rem, 12)
			r1, r2, r3, err := c.trngCall(FuncTRNGRnd32, uint64(chunkLen*8), false)
			if err != nil {
				return written, err
			}
			binary.LittleEndian.PutUint32(chunk[0:4], uint32(r3))
			binary.LittleEndian.PutUint32(chunk[4:8], uint32(r2))
			binary.LittleEndian.PutUint32(chunk[8:12], uint32(r1))
		}
		copy(buf[written:written+chunkLen], chunk[:chunkLen])
		written += chunkLen
	}
	return written, nil
}

func (c *Client) trngCall(fid uint32, nbits uint64, is64 bool) (uint64, uint64, uint64, error) {
	for range trngMaxRetries {
		r0, r1, r2, r3, err := c.call(fid, nbits, 0, 0)
		if err != nil {
			return 0, 0, 0, err
		}
		var status int64
		if is64 {
			status = int64(r0)
		} else {
			status = int64(int32(r0))
		}
		if status == int64(statusSuccess) {
			return r1, r2, r3, nil
		}
		if status == int64(statusNoEntropy) {
			continue
		}
		return 0, 0, 0, trngError(status)
	}
	return 0, 0, 0, ErrNoEntropy
}
