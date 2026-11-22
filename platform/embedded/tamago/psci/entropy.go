// SPDX-License-Identifier: GPL-3.0-or-later

package psci

import "math/bits"

// ConduitKind selects the hardware SMCCC conduit used by the nosplit runtime
// entropy hooks (runtime/goos.InitRNG and runtime/goos.GetRandomData).
type ConduitKind uint8

const (
	ConduitNone ConduitKind = iota
	ConduitSMC
	ConduitHVC
)

// EntropySource identifies the active entropy backend discovered at boot.
type EntropySource uint8

const (
	EntropyUninitialized EntropySource = iota
	EntropySMCCCTRNG64
	EntropySMCCCTRNG32
	EntropyARMRNDR
	EntropyCounterFallback
)

// String returns a concise diagnostic name for logging and boot probes.
func (s EntropySource) String() string {
	switch s {
	case EntropySMCCCTRNG64:
		return "smccc-trng64"
	case EntropySMCCCTRNG32:
		return "smccc-trng32"
	case EntropyARMRNDR:
		return "arm-rndr"
	case EntropyCounterFallback:
		return "cntpct-fallback"
	default:
		return "uninitialized"
	}
}

// IsHardware reports whether the active entropy source is backed by a true
// hardware/firmware RNG (SMCCC TRNG or ARMv8.5-RNG RNDR) rather than the
// counter-seeded fallback PRNG.
func (s EntropySource) IsHardware() bool {
	return s == EntropySMCCCTRNG64 || s == EntropySMCCCTRNG32 || s == EntropyARMRNDR
}

// xoshiro256** state used as the non-cryptographic counter-mixed fallback when
// neither SMCCC TRNG nor ARMv8.5 RNDR is available (for example QEMU virt
// cortex-a53 without firmware TRNG). Always seeded from CNTPCT_EL0 rather than
// a static constant.
type xoshiroState struct {
	s [4]uint64
}

//go:nosplit
func splitmix64(x *uint64) uint64 {
	*x += 0x9e3779b97f4a7c15
	z := *x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

//go:nosplit
func (x *xoshiroState) seed(v uint64) {
	st := v ^ 0x6a09e667f3bcc909
	x.s[0] = splitmix64(&st)
	x.s[1] = splitmix64(&st)
	x.s[2] = splitmix64(&st)
	x.s[3] = splitmix64(&st)
	if (x.s[0] | x.s[1] | x.s[2] | x.s[3]) == 0 {
		x.s[0] = 0x9e3779b97f4a7c15
	}
}

//go:nosplit
func (x *xoshiroState) mix(v uint64) {
	if (x.s[0] | x.s[1] | x.s[2] | x.s[3]) == 0 {
		x.seed(v)
		return
	}
	st := v
	x.s[0] ^= splitmix64(&st)
	x.s[2] ^= splitmix64(&st)
	if (x.s[0] | x.s[1] | x.s[2] | x.s[3]) == 0 {
		x.s[0] = 0x9e3779b97f4a7c15
	}
}

//go:nosplit
func (x *xoshiroState) next() uint64 {
	result := bits.RotateLeft64(x.s[1]*5, 7) * 9
	t := x.s[1] << 17
	x.s[2] ^= x.s[0]
	x.s[3] ^= x.s[1]
	x.s[1] ^= x.s[2]
	x.s[0] ^= x.s[3]
	x.s[2] ^= t
	x.s[3] = bits.RotateLeft64(x.s[3], 45)
	return result
}

//go:nosplit
func putLE64(dst []byte, v uint64) int {
	n := len(dst)
	if n > 8 {
		n = 8
	}
	for i := 0; i < n; i++ {
		dst[i] = byte(v >> (uint(i) * 8))
	}
	return n
}

//go:nosplit
func putLE32(dst []byte, v uint32) int {
	n := len(dst)
	if n > 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		dst[i] = byte(v >> (uint(i) * 8))
	}
	return n
}

// Package-level nosplit state for runtime/goos.InitRNG and GetRandomData.
// Single-core bare-metal TamaGo calls these early and inside nosplit contexts
// where mutexes, heap allocations and interface dispatch are forbidden.
var (
	hwConduit  ConduitKind
	hwSource   EntropySource
	hwFallback xoshiroState
)

//go:nosplit
func hwCall(conduit ConduitKind, x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	switch conduit {
	case ConduitSMC:
		return callSMC(x0, x1, x2, x3)
	case ConduitHVC:
		return callHVC(x0, x1, x2, x3)
	default:
		return ^uint64(0), 0, 0, 0
	}
}

// InitHardwareEntropy probes hardware entropy sources in preference order
// (SMCCC TRNG 64-bit -> SMCCC TRNG 32-bit -> ARMv8.5 RNDR -> CNTPCT_EL0-seeded
// xoshiro256** fallback). Safe to call from runtime/goos.InitRNG (nosplit).
//
//go:nosplit
func InitHardwareEntropy(conduit ConduitKind) EntropySource {
	hwConduit = conduit
	hwFallback.seed(readCounter())
	if conduit == ConduitSMC || conduit == ConduitHVC {
		r0, _, _, _ := hwCall(conduit, uint64(FuncTRNGVersion), 0, 0, 0)
		if v := int32(r0); v >= 0 && ((uint32(v)>>16)&0x7fff) >= 1 {
			f64, _, _, _ := hwCall(conduit, uint64(FuncTRNGFeatures), uint64(FuncTRNGRnd64), 0, 0)
			if int32(f64) >= 0 {
				hwSource = EntropySMCCCTRNG64
				return hwSource
			}
			f32, _, _, _ := hwCall(conduit, uint64(FuncTRNGFeatures), uint64(FuncTRNGRnd32), 0, 0)
			if int32(f32) >= 0 {
				hwSource = EntropySMCCCTRNG32
				return hwSource
			}
		}
	}
	if hasRNDR() {
		hwSource = EntropyARMRNDR
		return hwSource
	}
	hwSource = EntropyCounterFallback
	return hwSource
}

// HardwareEntropySource returns the entropy backend chosen by InitHardwareEntropy.
//
//go:nosplit
func HardwareEntropySource() EntropySource { return hwSource }

// FillHardwareEntropy fills buf using the active hardware entropy backend,
// falling back to counter-mixed xoshiro256** if hardware temporarily fails to
// produce bits. Safe to call from runtime/goos.GetRandomData (nosplit).
//
//go:nosplit
func FillHardwareEntropy(buf []byte) {
	if len(buf) == 0 {
		return
	}
	if hwSource == EntropyUninitialized {
		InitHardwareEntropy(hwConduit)
	}
	offset := 0
	for offset < len(buf) {
		switch hwSource {
		case EntropySMCCCTRNG64:
			rem := len(buf) - offset
			if rem > 24 {
				rem = 24
			}
			ok := false
			for attempt := 0; attempt < trngMaxRetries; attempt++ {
				r0, r1, r2, r3 := hwCall(hwConduit, uint64(FuncTRNGRnd64), uint64(rem*8), 0, 0)
				if int64(r0) == 0 {
					n := putLE64(buf[offset:offset+rem], r3)
					if n < rem {
						n += putLE64(buf[offset+n:offset+rem], r2)
					}
					if n < rem {
						n += putLE64(buf[offset+n:offset+rem], r1)
					}
					offset += rem
					ok = true
					break
				}
				if int64(r0) != int64(statusNoEntropy) {
					break
				}
			}
			if ok {
				continue
			}
		case EntropySMCCCTRNG32:
			rem := len(buf) - offset
			if rem > 12 {
				rem = 12
			}
			ok := false
			for attempt := 0; attempt < trngMaxRetries; attempt++ {
				r0, r1, r2, r3 := hwCall(hwConduit, uint64(FuncTRNGRnd32), uint64(rem*8), 0, 0)
				if int32(r0) == 0 {
					n := putLE32(buf[offset:offset+rem], uint32(r3))
					if n < rem {
						n += putLE32(buf[offset+n:offset+rem], uint32(r2))
					}
					if n < rem {
						n += putLE32(buf[offset+n:offset+rem], uint32(r1))
					}
					offset += rem
					ok = true
					break
				}
				if int32(r0) != statusNoEntropy {
					break
				}
			}
			if ok {
				continue
			}
		case EntropyARMRNDR:
			ok := false
			for attempt := 0; attempt < trngMaxRetries; attempt++ {
				val, valid := readRNDR()
				if valid {
					offset += putLE64(buf[offset:], val)
					ok = true
					break
				}
			}
			if ok {
				continue
			}
		}
		// Latch source to non-hardware fallback before emitting fallback bytes
		// so HardwareEntropySource().IsHardware() never misreports fallback output.
		hwSource = EntropyCounterFallback
		hwFallback.mix(readCounter())
		offset += putLE64(buf[offset:], hwFallback.next())
	}
}

// EntropyPool mirrors the bare-metal entropy selection and fill logic with
// injectable hardware hooks for deterministic host testing.
type EntropyPool struct {
	source   EntropySource
	conduit  Conduit
	hasRNDR  func() bool
	readRNDR func() (uint64, bool)
	counter  func() uint64
	fallback xoshiroState
}

// NewEntropyPool probes the supplied hooks in the same order as
// InitHardwareEntropy and returns a ready pool.
func NewEntropyPool(conduit Conduit, hasRNDR func() bool, readRNDR func() (uint64, bool), counter func() uint64) *EntropyPool {
	p := &EntropyPool{
		conduit:  conduit,
		hasRNDR:  hasRNDR,
		readRNDR: readRNDR,
		counter:  counter,
	}
	var seed uint64
	if counter != nil {
		seed = counter()
	}
	p.fallback.seed(seed)
	if conduit != nil {
		c := New(conduit)
		if major, _, err := c.TRNGVersion(); err == nil && major >= 1 {
			if _, err := c.TRNGFeatures(FuncTRNGRnd64); err == nil {
				p.source = EntropySMCCCTRNG64
				return p
			}
			if _, err := c.TRNGFeatures(FuncTRNGRnd32); err == nil {
				p.source = EntropySMCCCTRNG32
				return p
			}
		}
	}
	if hasRNDR != nil && readRNDR != nil && hasRNDR() {
		p.source = EntropyARMRNDR
		return p
	}
	p.source = EntropyCounterFallback
	return p
}

// Source returns the entropy source selected when the pool was constructed.
func (p *EntropyPool) Source() EntropySource {
	if p == nil {
		return EntropyUninitialized
	}
	return p.source
}

// Fill populates buf from the selected source, falling back to counter-mixed
// xoshiro256** if hardware returns an error or exhausts retries.
func (p *EntropyPool) Fill(buf []byte) {
	if p == nil || len(buf) == 0 {
		return
	}
	offset := 0
	for offset < len(buf) {
		switch p.source {
		case EntropySMCCCTRNG64, EntropySMCCCTRNG32:
			n, err := New(p.conduit).ReadEntropy(buf[offset:])
			offset += n
			if err == nil {
				return
			}
		case EntropyARMRNDR:
			ok := false
			for attempt := 0; attempt < trngMaxRetries; attempt++ {
				if val, valid := p.readRNDR(); valid {
					offset += putLE64(buf[offset:], val)
					ok = true
					break
				}
			}
			if ok {
				continue
			}
		}
		// Latch source to non-hardware fallback before emitting fallback bytes.
		p.source = EntropyCounterFallback
		var tick uint64
		if p.counter != nil {
			tick = p.counter()
		}
		p.fallback.mix(tick)
		offset += putLE64(buf[offset:], p.fallback.next())
	}
}
