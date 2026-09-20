package remotemedia

import (
	"strings"
	"testing"
)

func TestAudioFormatRejectsNonPCM16OrInvalidLayout(t *testing.T) {
	for _, format := range []AudioFormat{
		{SampleRate: 44_100, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240},
		{SampleRate: 48_000, Channels: 3, Encoding: AudioEncodingPCM16LE, FrameSamples: 240},
		{SampleRate: 48_000, Channels: 2, Encoding: "aac", FrameSamples: 240},
		{SampleRate: 48_000, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 0},
	} {
		if err := ValidateAudioFormat(format); err == nil {
			t.Fatalf("ValidateAudioFormat(%+v) succeeded", format)
		}
	}
	if err := ValidateAudioFormat(AudioFormat{SampleRate: 48_000, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240}); err != nil {
		t.Fatalf("valid format: %v", err)
	}
}

func TestAudioConfigRejectsUnknownSourceAndMissingIdentity(t *testing.T) {
	valid := testAudioConfig()
	for _, mutate := range []func(*AudioConfig){
		func(c *AudioConfig) { c.Source.Kind = AudioSourceKind("unknown") },
		func(c *AudioConfig) { c.Source.EndpointUID = "" },
		func(c *AudioConfig) { c.Source.EndpointDigest = "" },
	} {
		config := valid
		mutate(&config)
		if err := ValidateAudioConfig(config); err == nil {
			t.Fatalf("ValidateAudioConfig(%+v) succeeded", config)
		}
	}
}

func TestAudioConfigAllowsDisabledZeroValue(t *testing.T) {
	if err := ValidateAudioConfig(AudioConfig{}); err != nil {
		t.Fatalf("zero AudioConfig: %v", err)
	}
}

func TestAudioConfigValidatesHostOutputDisplayIdentity(t *testing.T) {
	persistent := AudioDisplayIdentity{HardwareUUID: "display-1", EDIDVendor: "ACME", EDIDModel: "Panel", EDIDSerial: "serial-1"}
	persistentDigest, err := CanonicalAudioDisplayDigest(persistent)
	if err != nil {
		t.Fatal(err)
	}
	config := testHostOutputAudioConfig(persistent, persistentDigest)
	if err := ValidateAudioConfig(config); err != nil {
		t.Fatalf("persistent host output config: %v", err)
	}
	for _, mutate := range []func(*AudioConfig){
		func(c *AudioConfig) { c.Source.DisplayDigest = "" },
		func(c *AudioConfig) { c.Source.DisplayDigest = "sha256:wrong" },
	} {
		invalid := config
		mutate(&invalid)
		if err := ValidateAudioConfig(invalid); err == nil {
			t.Fatalf("invalid host output config accepted: %+v", invalid)
		}
	}
}

func TestAudioConfigRequiresSelectionContextForDisplayFingerprint(t *testing.T) {
	fingerprint := AudioDisplayIdentity{EDIDVendor: "ACME", EDIDModel: "Panel", ExplicitSelection: true}
	if _, err := CanonicalAudioDisplayDigest(fingerprint); err == nil {
		t.Fatal("empty explicit selection context accepted")
	}
	fingerprint.SelectionContext = "operator:selected-main-display"
	digest, err := CanonicalAudioDisplayDigest(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAudioConfig(testHostOutputAudioConfig(fingerprint, digest)); err != nil {
		t.Fatalf("explicit display fingerprint config: %v", err)
	}
}

func TestAudioSampleRejectsPartialPCMFrame(t *testing.T) {
	format := AudioFormat{SampleRate: 48_000, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240}
	sample := AudioSample{Format: format, Frames: 2, PCM16: make([]byte, 7)}
	if err := ValidateAudioSample(sample); err == nil {
		t.Fatal("partial PCM frame accepted")
	}
	sample.PCM16 = make([]byte, 8)
	if err := ValidateAudioSample(sample); err != nil {
		t.Fatalf("complete PCM frame rejected: %v", err)
	}
	copy := sample.Clone()
	copy.PCM16[0] = 1
	if sample.PCM16[0] != 0 {
		t.Fatal("AudioSample.Clone did not defensively copy PCM")
	}
}

func TestAudioSampleAndFrameRejectOverflowingPCMSize(t *testing.T) {
	format := AudioFormat{SampleRate: 48_000, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240}
	frames := int(^uint(0) >> 1)
	if err := ValidateAudioSample(AudioSample{Format: format, Frames: frames}); err == nil {
		t.Fatal("overflowing audio sample accepted")
	}
	if err := ValidateAudioFrame(AudioFrame{Format: format, Frames: frames}); err == nil {
		t.Fatal("overflowing audio frame accepted")
	}
}

func TestAudioFrameCloneDefensivelyCopiesPCM(t *testing.T) {
	frame := AudioFrame{PCM16: []byte{0, 1, 2, 3}}
	copy := frame.Clone()
	copy.PCM16[0] = 9
	if frame.PCM16[0] != 0 {
		t.Fatal("AudioFrame.Clone did not defensively copy PCM")
	}
}

func TestAudioStatsRecordsNonZeroPCM16Deterministically(t *testing.T) {
	count, err := CountNonZeroPCM16([]byte{0, 0, 1, 0, 0xff, 0xff, 0, 0})
	if err != nil || count != 2 {
		t.Fatalf("CountNonZeroPCM16 = %d, %v; want 2, nil", count, err)
	}
	stats := AudioStats{}
	if err := stats.RecordPCM16([]byte{0, 0, 1, 0, 0xff, 0xff, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if stats.Callbacks != 1 || stats.NonZeroSamples != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	stats.RecordRuntimeError(AudioRuntimeErrorSourceUnavailable)
	if stats.RuntimeError != AudioRuntimeErrorSourceUnavailable {
		t.Fatalf("runtime error = %q", stats.RuntimeError)
	}
	if _, err := CountNonZeroPCM16([]byte{0}); err == nil {
		t.Fatal("partial PCM16 sample accepted")
	}
}

func TestAudioEndpointDigestIsCanonicalAndStable(t *testing.T) {
	endpoint := AudioEndpoint{UID: "uid-e\u0301", DisplayName: "  ShadowCast\t UAC  ", SampleRate: 48_000, Channels: 2}
	digest, err := CanonicalAudioEndpointDigest(endpoint)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	const want = "sha256:891ac7b4defb2a95bbf534edd1676958e13e5c2afc689f02ad1623014ccb54b4"
	if digest != want {
		t.Fatalf("digest = %q, want %q", digest, want)
	}
	equivalent, err := CanonicalAudioEndpointDigest(AudioEndpoint{UID: "uid-é", DisplayName: "ShadowCast UAC", SampleRate: 48_000, Channels: 2})
	if err != nil {
		t.Fatalf("equivalent digest: %v", err)
	}
	if digest != equivalent {
		t.Fatalf("equivalent endpoint digest = %q, want %q", equivalent, digest)
	}
}

func TestAudioDisplayDigestHasSeparateDomainAndGoldenVector(t *testing.T) {
	display := AudioDisplayIdentity{HardwareUUID: "uuid-e\u0301", EDIDVendor: "ACME", EDIDModel: "Panel  1", EDIDSerial: ""}
	digest, err := CanonicalAudioDisplayDigest(display)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	const want = "sha256:1b5350b602e7ad021384efaf10b649b5a25f92cbdcda9817b4048f9d4b267fec"
	if digest != want {
		t.Fatalf("digest = %q, want %q", digest, want)
	}
	endpoint, err := CanonicalAudioEndpointDigest(AudioEndpoint{UID: display.HardwareUUID, DisplayName: display.EDIDModel, SampleRate: 48_000, Channels: 2})
	if err != nil {
		t.Fatalf("endpoint digest: %v", err)
	}
	if digest == endpoint || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("display digest is not separately domain-separated: %q", digest)
	}
}

func testAudioConfig() AudioConfig {
	endpoint := AudioEndpoint{UID: "device-1", DisplayName: "ShadowCast", SampleRate: 48_000, Channels: 2}
	digest, _ := CanonicalAudioEndpointDigest(endpoint)
	return AudioConfig{
		Enabled:   true,
		Source:    AudioSourceConfig{Kind: AudioSourceShadowCastUAC, EndpointName: endpoint.DisplayName, EndpointUID: endpoint.UID, EndpointDigest: digest, SampleRate: endpoint.SampleRate, Channels: endpoint.Channels, FrameSamples: 240, Enabled: true},
		Transport: AudioTransportConfig{RTPDestination: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", SSRC: 1, MTU: 1200, FormatCapabilityVersion: 1},
	}
}

func testHostOutputAudioConfig(display AudioDisplayIdentity, digest string) AudioConfig {
	return AudioConfig{
		Enabled: true,
		Source:  AudioSourceConfig{Kind: AudioSourceHostOutput, Display: display, DisplayDigest: digest, SampleRate: 48_000, Channels: 2, FrameSamples: 240, Enabled: true},
	}
}
