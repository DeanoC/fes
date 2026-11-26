// SPDX-License-Identifier: GPL-3.0-or-later

package psci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

type mockConduit struct {
	fn    func(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64)
	calls int
}

func (m *mockConduit) Call(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	m.calls++
	return m.fn(x0, x1, x2, x3)
}

func TestPSCIVersionFeaturesAndReset(t *testing.T) {
	// Nil client and nil conduit return ErrUnsupported cleanly.
	var nilClient *Client
	if _, _, err := nilClient.Version(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil Version=%v", err)
	}
	empty := New(nil)
	if empty.Supports(FuncSystemOff) {
		t.Fatal("nil conduit reported support")
	}
	if err := empty.SystemOff(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil SystemOff=%v", err)
	}
	if err := empty.SystemReset(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil SystemReset=%v", err)
	}
	if err := empty.SystemReset2(0, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil SystemReset2=%v", err)
	}

	var gotResetType uint32
	var gotCookie uint64
	conduit := &mockConduit{fn: func(x0, x1, x2, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncVersion:
			return 0x00010001, 0, 0, 0 // PSCI 1.1
		case FuncFeatures:
			switch uint32(x1) {
			case FuncSystemOff, FuncSystemReset:
				return 0, 0, 0, 0
			case FuncSystemReset2:
				return 1, 0, 0, 0
			default:
				return ^uint64(0), 0, 0, 0
			}
		case FuncSystemOff:
			return 0, 0, 0, 0 // Unexpected return must become ErrReturned.
		case FuncSystemReset:
			return uint64(^uint32(2)), 0, 0, 0 // -3 = DENIED
		case FuncSystemReset2:
			gotResetType = uint32(x1)
			gotCookie = x2
			return uint64(^uint32(5)), 0, 0, 0 // -6 = INTERNAL_FAILURE
		default:
			return ^uint64(0), 0, 0, 0
		}
	}}
	c := New(conduit)
	maj, min, err := c.Version()
	if err != nil || maj != 1 || min != 1 {
		t.Fatalf("Version=(%d.%d, %v), want 1.1", maj, min, err)
	}
	if !c.Supports(FuncSystemOff) || !c.Supports(FuncSystemReset) || c.Supports(0x840000ff) {
		t.Fatal("unexpected PSCI_FEATURES results")
	}
	if flags, err := c.Features(FuncSystemReset2); err != nil || flags != 1 {
		t.Fatalf("Features(RESET2)=(%#x, %v)", flags, err)
	}
	if err := c.SystemOff(); !errors.Is(err, ErrReturned) {
		t.Fatalf("SystemOff=%v, want ErrReturned", err)
	}
	if err := c.SystemReset(); !errors.Is(err, ErrDenied) {
		t.Fatalf("SystemReset=%v, want ErrDenied", err)
	}
	if err := c.SystemReset2(0x80000001, 0xdeadbeef); !errors.Is(err, ErrInternalFailure) || gotResetType != 0x80000001 || gotCookie != 0xdeadbeef {
		t.Fatalf("SystemReset2=(%v, type=%#x, cookie=%#x)", err, gotResetType, gotCookie)
	}

	// Cover remaining PSCI error mappings.
	if !errors.Is(psciError(statusInvalidParameters), ErrInvalidParameters) {
		t.Fatal("invalid parameters mapping failed")
	}
	if psciError(-99) == nil {
		t.Fatal("unknown PSCI error returned nil")
	}
}

func TestSMCCCTRNG64And32ExtractionOrder(t *testing.T) {
	// DEN0098 §2.5.2: TRNG_RND64 packs bits [63:0] in X3, [127:64] in X2, [191:128] in X1.
	t.Run("trng64", func(t *testing.T) {
		retries := 0
		c := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
			switch uint32(x0) {
			case FuncTRNGVersion:
				return 0x00010000, 0, 0, 0
			case FuncTRNGFeatures:
				if uint32(x1) == FuncTRNGRnd64 {
					return 0, 0, 0, 0
				}
				return ^uint64(0), 0, 0, 0
			case FuncTRNGRnd64:
				if retries == 0 {
					retries++
					return ^uint64(2), 0, 0, 0 // -3 = NO_ENTROPY on first attempt
				}
				// Distinct bytes across X3, X2, X1 to verify extraction order.
				return 0, 0x1112131415161718, 0x2122232425262728, 0x3132333435363738
			default:
				return ^uint64(0), 0, 0, 0
			}
		}})
		maj, min, err := c.TRNGVersion()
		if err != nil || maj != 1 || min != 0 {
			t.Fatalf("TRNGVersion=(%d.%d, %v)", maj, min, err)
		}
		if n, err := c.ReadEntropy(nil); n != 0 || err != nil {
			t.Fatalf("empty ReadEntropy=(%d, %v)", n, err)
		}
		buf := make([]byte, 27) // 24 + 3 bytes across 2 calls
		n, err := c.ReadEntropy(buf)
		if err != nil || n != 27 {
			t.Fatalf("ReadEntropy=(%d, %v)", n, err)
		}
		if binary.LittleEndian.Uint64(buf[0:8]) != 0x3132333435363738 ||
			binary.LittleEndian.Uint64(buf[8:16]) != 0x2122232425262728 ||
			binary.LittleEndian.Uint64(buf[16:24]) != 0x1112131415161718 {
			t.Fatalf("unexpected TRNG64 word order: %x", buf[:24])
		}
		if !bytes.Equal(buf[24:27], []byte{0x38, 0x37, 0x36}) {
			t.Fatalf("unexpected TRNG64 tail: %x", buf[24:27])
		}
	})

	t.Run("trng32", func(t *testing.T) {
		c := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
			switch uint32(x0) {
			case FuncTRNGVersion:
				return 0x00010000, 0, 0, 0
			case FuncTRNGFeatures:
				if uint32(x1) == FuncTRNGRnd32 {
					return 0, 0, 0, 0
				}
				return ^uint64(0), 0, 0, 0
			case FuncTRNGRnd32:
				return 0, 0xaaaaaaaa, 0xbbbbbbbb, 0xcccccccc
			default:
				return ^uint64(0), 0, 0, 0
			}
		}})
		buf := make([]byte, 15) // 12 + 3 bytes
		n, err := c.ReadEntropy(buf)
		if err != nil || n != 15 {
			t.Fatalf("TRNG32 ReadEntropy=(%d, %v)", n, err)
		}
		if binary.LittleEndian.Uint32(buf[0:4]) != 0xcccccccc ||
			binary.LittleEndian.Uint32(buf[4:8]) != 0xbbbbbbbb ||
			binary.LittleEndian.Uint32(buf[8:12]) != 0xaaaaaaaa {
			t.Fatalf("unexpected TRNG32 word order: %x", buf[:12])
		}
		if !bytes.Equal(buf[12:15], []byte{0xcc, 0xcc, 0xcc}) {
			t.Fatalf("unexpected TRNG32 tail: %x", buf[12:15])
		}
	})

	t.Run("no-entropy-exhausted", func(t *testing.T) {
		c := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
			switch uint32(x0) {
			case FuncTRNGVersion:
				return 0x00010000, 0, 0, 0
			case FuncTRNGFeatures:
				return 0, 0, 0, 0
			case FuncTRNGRnd64:
				return ^uint64(2), 0, 0, 0 // always NO_ENTROPY
			default:
				return ^uint64(0), 0, 0, 0
			}
		}})
		if _, err := c.ReadEntropy(make([]byte, 8)); !errors.Is(err, ErrNoEntropy) {
			t.Fatalf("exhausted TRNG=%v, want ErrNoEntropy", err)
		}
		if !errors.Is(trngError(int64(statusInvalidParameters)), ErrInvalidParameters) {
			t.Fatal("TRNG invalid params mapping failed")
		}
		if trngError(-99) == nil {
			t.Fatal("unknown TRNG error returned nil")
		}
	})
}

func TestEntropyPoolSelectionAndFallback(t *testing.T) {
	// Verify String() and IsHardware() across all EntropySource values.
	if EntropyUninitialized.String() != "uninitialized" || EntropyUninitialized.IsHardware() {
		t.Fatal("bad uninitialized metadata")
	}
	if EntropySMCCCTRNG64.String() != "smccc-trng64" || !EntropySMCCCTRNG64.IsHardware() {
		t.Fatal("bad trng64 metadata")
	}
	if EntropySMCCCTRNG32.String() != "smccc-trng32" || !EntropySMCCCTRNG32.IsHardware() {
		t.Fatal("bad trng32 metadata")
	}
	if EntropyARMRNDR.String() != "arm-rndr" || !EntropyARMRNDR.IsHardware() {
		t.Fatal("bad rndr metadata")
	}
	if EntropyCounterFallback.String() != "cntpct-fallback" || EntropyCounterFallback.IsHardware() {
		t.Fatal("bad fallback metadata")
	}
	var nilPool *EntropyPool
	if nilPool.Source() != EntropyUninitialized {
		t.Fatal("nil pool source non-zero")
	}
	nilPool.Fill(make([]byte, 8))

	// 1. SMCCC TRNG64 selected over RNDR when both exist.
	tick := uint64(100)
	nextTick := func() uint64 { tick += 17; return tick }
	trng64 := &mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncTRNGVersion:
			return 0x00010000, 0, 0, 0
		case FuncTRNGFeatures:
			return 0, 0, 0, 0
		case FuncTRNGRnd64:
			return 0, 0x0102030405060708, 0x1112131415161718, 0x2122232425262728
		default:
			return ^uint64(0), 0, 0, 0
		}
	}}
	p64 := NewEntropyPool(trng64, func() bool { return true }, func() (uint64, bool) { return 1, true }, nextTick)
	if p64.Source() != EntropySMCCCTRNG64 {
		t.Fatalf("source=%s, want smccc-trng64", p64.Source())
	}
	b64 := make([]byte, 16)
	p64.Fill(b64)
	if binary.LittleEndian.Uint64(b64[:8]) != 0x2122232425262728 {
		t.Fatalf("trng64 pool fill=%x", b64)
	}

	// 2. SMCCC TRNG32 selected when only 32-bit is supported.
	trng32 := &mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncTRNGVersion:
			return 0x00010000, 0, 0, 0
		case FuncTRNGFeatures:
			if uint32(x1) == FuncTRNGRnd32 {
				return 0, 0, 0, 0
			}
			return ^uint64(0), 0, 0, 0
		case FuncTRNGRnd32:
			return 0, 0x11111111, 0x22222222, 0x33333333
		default:
			return ^uint64(0), 0, 0, 0
		}
	}}
	p32 := NewEntropyPool(trng32, nil, nil, nextTick)
	if p32.Source() != EntropySMCCCTRNG32 {
		t.Fatalf("source=%s, want smccc-trng32", p32.Source())
	}
	b32 := make([]byte, 8)
	p32.Fill(b32)
	if binary.LittleEndian.Uint32(b32[:4]) != 0x33333333 {
		t.Fatalf("trng32 pool fill=%x", b32)
	}

	// 3. ARMv8.5 RNDR selected when TRNG is absent, with transient retry and fallback on exhaustion.
	rndrCalls := 0
	pRNDR := NewEntropyPool(nil, func() bool { return true }, func() (uint64, bool) {
		rndrCalls++
		if rndrCalls == 1 {
			return 0, false // transient Z=1 retry
		}
		if rndrCalls == 2 {
			return 0xfeedfacecafebeef, true
		}
		return 0, false // exhaust retries -> fallback
	}, nextTick)
	if pRNDR.Source() != EntropyARMRNDR {
		t.Fatalf("source=%s, want arm-rndr", pRNDR.Source())
	}
	bRNDR := make([]byte, 16)
	pRNDR.Fill(bRNDR)
	if binary.LittleEndian.Uint64(bRNDR[:8]) != 0xfeedfacecafebeef {
		t.Fatalf("rndr first word=%x", bRNDR[:8])
	}
	if bytes.Equal(bRNDR[8:16], make([]byte, 8)) {
		t.Fatal("rndr fallback produced all zeros")
	}
	if pRNDR.Source() != EntropyCounterFallback || pRNDR.Source().IsHardware() {
		t.Fatalf("rndr fallback did not latch non-hardware source: %s", pRNDR.Source())
	}

	// 3b. SMCCC TRNG runtime failure also latches non-hardware fallback.
	trngFails := false
	trngDegrade := &mockConduit{fn: func(x0, _, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncTRNGVersion:
			return 0x00010000, 0, 0, 0
		case FuncTRNGFeatures:
			return 0, 0, 0, 0
		case FuncTRNGRnd64:
			if trngFails {
				return ^uint64(2), 0, 0, 0 // NO_ENTROPY
			}
			return 0, 1, 2, 3
		default:
			return ^uint64(0), 0, 0, 0
		}
	}}
	pDegrade := NewEntropyPool(trngDegrade, nil, nil, nextTick)
	if !pDegrade.Source().IsHardware() {
		t.Fatalf("expected hardware source, got %s", pDegrade.Source())
	}
	trngFails = true
	bDegrade := make([]byte, 16)
	pDegrade.Fill(bDegrade)
	if bytes.Equal(bDegrade, make([]byte, 16)) {
		t.Fatal("trng degrade fallback produced all zeros")
	}
	if pDegrade.Source() != EntropyCounterFallback || pDegrade.Source().IsHardware() {
		t.Fatalf("trng degrade did not latch non-hardware source: %s", pDegrade.Source())
	}

	// 4. Counter fallback produces distinct non-zero blocks across seeds and calls.
	pFall1 := NewEntropyPool(nil, nil, nil, func() uint64 { return 12345 })
	pFall2 := NewEntropyPool(nil, nil, nil, func() uint64 { return 67890 })
	if pFall1.Source() != EntropyCounterFallback {
		t.Fatalf("source=%s, want cntpct-fallback", pFall1.Source())
	}
	a, b, c := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	pFall1.Fill(a)
	pFall1.Fill(b)
	pFall2.Fill(c)
	zero := make([]byte, 32)
	if bytes.Equal(a, zero) || bytes.Equal(b, zero) || bytes.Equal(a, b) || bytes.Equal(a, c) {
		t.Fatalf("degenerate fallback output a=%x b=%x c=%x", a, b, c)
	}
}

func TestHardwareEntropyNosplitStubs(t *testing.T) {
	// Exercise package-level nosplit helpers with host stubs.
	for _, kind := range []ConduitKind{ConduitNone, ConduitSMC, ConduitHVC} {
		src := InitHardwareEntropy(kind)
		if src != EntropyCounterFallback || HardwareEntropySource() != EntropyCounterFallback {
			t.Fatalf("kind=%d src=%s", kind, src)
		}
	}
	// Verify FillHardwareEntropy degrades cleanly to fallback when hardware
	// modes fail on the host stub (also covering putLE32 and readRNDR).
	var tmp [4]byte
	if n := putLE32(tmp[:3], 0x11223344); n != 3 || !bytes.Equal(tmp[:3], []byte{0x44, 0x33, 0x22}) {
		t.Fatalf("putLE32=%d %x", n, tmp[:3])
	}
	for _, src := range []EntropySource{EntropyUninitialized, EntropySMCCCTRNG64, EntropySMCCCTRNG32, EntropyARMRNDR, EntropyCounterFallback} {
		hwSource = src
		var buf [32]byte
		FillHardwareEntropy(buf[:])
		if bytes.Equal(buf[:], make([]byte, 32)) {
			t.Fatalf("src=%s produced all zeros", src)
		}
		if HardwareEntropySource() != EntropyCounterFallback || HardwareEntropySource().IsHardware() {
			t.Fatalf("src=%s did not latch non-hardware fallback: got %s", src, HardwareEntropySource())
		}
	}
	var a, b [32]byte
	FillHardwareEntropy(nil)
	FillHardwareEntropy(a[:])
	FillHardwareEntropy(b[:])
	var zero [32]byte
	if bytes.Equal(a[:], zero[:]) || bytes.Equal(a[:], b[:]) {
		t.Fatalf("degenerate nosplit output a=%x b=%x", a, b)
	}
}

func TestSMCCCVersionAffinityInfoAndProbe(t *testing.T) {
	empty := New(nil)
	if _, _, err := empty.SMCCCVersion(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil SMCCCVersion=%v", err)
	}
	if _, err := empty.AffinityInfo(0, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil AffinityInfo=%v", err)
	}
	if _, err := empty.Probe(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil Probe=%v", err)
	}

	if empty.SupportsWarmReset() {
		t.Fatal("nil conduit reported SupportsWarmReset")
	}
	if AffinityOn.String() != "on" || AffinityOff.String() != "off" ||
		AffinityOnPending.String() != "on-pending" || AffinityState(9).String() != "AffinityState(9)" {
		t.Fatalf("AffinityState.String mismatch: %q %q %q %q",
			AffinityOn.String(), AffinityOff.String(), AffinityOnPending.String(), AffinityState(9).String())
	}

	// PSCI 1.1 + SMCCC 1.2 + Reset2 + AffinityInfo + TRNG64.
	var gotAffinity uint64
	var gotLevel uint32
	affinityRet := uint64(0)
	c11 := New(&mockConduit{fn: func(x0, x1, x2, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncVersion:
			return 0x00010001, 0, 0, 0 // PSCI 1.1
		case FuncSMCCCVersion:
			return 0x00010002, 0, 0, 0 // SMCCC 1.2
		case FuncFeatures:
			switch uint32(x1) {
			case FuncSMCCCVersion, FuncSystemOff, FuncSystemReset, FuncSystemReset2, FuncAffinityInfo:
				return 0, 0, 0, 0
			default:
				return ^uint64(0), 0, 0, 0
			}
		case FuncAffinityInfo:
			gotAffinity = x1
			gotLevel = uint32(x2)
			return affinityRet, 0, 0, 0
		case FuncTRNGVersion:
			return 0x00010000, 0, 0, 0 // TRNG 1.0
		case FuncTRNGFeatures:
			if uint32(x1) == FuncTRNGRnd64 {
				return 0, 0, 0, 0
			}
			return ^uint64(0), 0, 0, 0
		default:
			return ^uint64(0), 0, 0, 0
		}
	}})
	smMaj, smMin, err := c11.SMCCCVersion()
	if err != nil || smMaj != 1 || smMin != 2 {
		t.Fatalf("SMCCCVersion=(%d.%d, %v), want 1.2", smMaj, smMin, err)
	}
	// Per SMCCC §7.2, NOT_SUPPORTED (-1) from SMCCC_VERSION maps to SMCCC 1.0,
	// and Probe skips TRNG probing unless SMCCC >= 1.1 is established (DEN0098 §2.1.3).
	c10SMCCC := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncVersion:
			return 0x00010000, 0, 0, 0 // PSCI 1.0
		case FuncFeatures:
			if uint32(x1) == FuncSMCCCVersion {
				return 0, 0, 0, 0 // SMCCC_VERSION supported, returns 1.0 (-1)
			}
			return ^uint64(0), 0, 0, 0
		case FuncSMCCCVersion:
			return ^uint64(0), 0, 0, 0 // -1 = NOT_SUPPORTED -> SMCCC 1.0
		case FuncTRNGVersion:
			t.Fatalf("Probe called TRNG_VERSION on SMCCC 1.0")
		}
		return ^uint64(0), 0, 0, 0
	}})
	sm10Maj, sm10Min, err := c10SMCCC.SMCCCVersion()
	if err != nil || sm10Maj != 1 || sm10Min != 0 {
		t.Fatalf("SMCCC_VERSION NOT_SUPPORTED=(%d.%d, %v), want 1.0 nil", sm10Maj, sm10Min, err)
	}
	if _, _, err := New(&mockConduit{fn: func(uint64, uint64, uint64, uint64) (uint64, uint64, uint64, uint64) {
		return uint64(^uint32(1)), 0, 0, 0 // -2 = INVALID_PARAMETERS
	}}).SMCCCVersion(); !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("SMCCC_VERSION error err=%v, want ErrInvalidParameters", err)
	}
	caps10, err := c10SMCCC.Probe()
	if err != nil || !caps10.HasSMCCC || caps10.SMCCCMajor != 1 || caps10.SMCCCMinor != 0 || caps10.TRNGMajor != 0 {
		t.Fatalf("PSCI 1.0 SMCCC 1.0 Probe=(%+v, %v)", caps10, err)
	}
	cNoSMCCCFeat := New(&mockConduit{fn: func(x0, _, _, _ uint64) (uint64, uint64, uint64, uint64) {
		if uint32(x0) == FuncVersion {
			return 0x00010000, 0, 0, 0
		}
		if uint32(x0) == FuncSMCCCVersion || uint32(x0) == FuncTRNGVersion {
			t.Fatalf("Probe called %#x without discovery", uint32(x0))
		}
		return ^uint64(0), 0, 0, 0
	}})
	if capsNo, err := cNoSMCCCFeat.Probe(); err != nil || capsNo.HasSMCCC || capsNo.TRNGMajor != 0 {
		t.Fatalf("PSCI 1.0 without SMCCC_VERSION feature Probe=(%+v, %v)", capsNo, err)
	}
	for _, want := range []AffinityState{AffinityOn, AffinityOff, AffinityOnPending} {
		affinityRet = uint64(want)
		got, err := c11.AffinityInfo(0x80000102, 1)
		if err != nil || got != want || gotAffinity != 0x80000102 || gotLevel != 1 {
			t.Fatalf("AffinityInfo=(%v, %v, aff=%#x, lvl=%d), want %v", got, err, gotAffinity, gotLevel, want)
		}
	}
	affinityRet = uint64(^uint32(1)) // -2 = INVALID_PARAMETERS
	if _, err := c11.AffinityInfo(0xff, 0); !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("AffinityInfo invalid params err=%v", err)
	}
	affinityRet = 5 // unknown positive state
	if _, err := c11.AffinityInfo(0, 0); err == nil {
		t.Fatal("unknown affinity state succeeded")
	}

	caps, err := c11.Probe()
	if err != nil {
		t.Fatalf("Probe(1.1): %v", err)
	}
	wantCaps := Capabilities{
		PSCIMajor:    1,
		PSCIMinor:    1,
		SMCCCMajor:   1,
		SMCCCMinor:   2,
		HasSMCCC:     true,
		SystemOff:    true,
		SystemReset:  true,
		SystemReset2: true,
		AffinityInfo: true,
		TRNGMajor:    1,
		TRNGMinor:    0,
		TRNG64:       true,
		TRNG32:       false,
	}
	if caps != wantCaps {
		t.Fatalf("Probe(1.1)=%+v, want %+v", caps, wantCaps)
	}
	if !c11.SupportsWarmReset() {
		t.Fatal("PSCI 1.1 with SYSTEM_RESET2 reported SupportsWarmReset=false")
	}
	if c10SMCCC.SupportsWarmReset() {
		t.Fatal("PSCI 1.0 reported SupportsWarmReset=true")
	}
	c11NoReset2 := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		if uint32(x0) == FuncVersion {
			return 0x00010001, 0, 0, 0
		}
		if uint32(x0) == FuncFeatures && uint32(x1) == FuncSystemReset2 {
			return ^uint64(0), 0, 0, 0
		}
		return ^uint64(0), 0, 0, 0
	}})
	if c11NoReset2.SupportsWarmReset() {
		t.Fatal("PSCI 1.1 without SYSTEM_RESET2 reported SupportsWarmReset=true")
	}

	// PSCI 0.2 without PSCI_FEATURES skips SMCCC_VERSION and reports mandatory 0.2 calls.
	c02 := New(&mockConduit{fn: func(x0, _, _, _ uint64) (uint64, uint64, uint64, uint64) {
		if uint32(x0) == FuncVersion {
			return 0x00000002, 0, 0, 0
		}
		if uint32(x0) == FuncSMCCCVersion || uint32(x0) == FuncFeatures || uint32(x0) == FuncTRNGVersion {
			t.Fatalf("Probe(0.2) unexpectedly invoked %#x", uint32(x0))
		}
		return ^uint64(0), 0, 0, 0
	}})
	if c02.SupportsWarmReset() {
		t.Fatal("PSCI 0.2 reported SupportsWarmReset=true")
	}
	caps02, err := c02.Probe()
	if err != nil {
		t.Fatalf("Probe(0.2): %v", err)
	}
	if caps02.PSCIMajor != 0 || caps02.PSCIMinor != 2 || caps02.HasSMCCC ||
		!caps02.SystemOff || !caps02.SystemReset || !caps02.AffinityInfo || caps02.SystemReset2 ||
		caps02.TRNG64 || caps02.TRNG32 {
		t.Fatalf("Probe(0.2) mismatch: %+v", caps02)
	}
}
