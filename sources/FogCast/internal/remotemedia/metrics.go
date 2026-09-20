package remotemedia

import (
	"math"
	"sort"
	"sync"
	"time"
)

type FrameRate struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type TimingSummary struct {
	Count     int     `json:"count"`
	P50Millis float64 `json:"p50_ms"`
	P95Millis float64 `json:"p95_ms"`
	MaxMillis float64 `json:"max_ms"`
}

type DropCounters struct {
	Capture     uint64 `json:"capture"`
	Queue       uint64 `json:"queue"`
	PacketQueue uint64 `json:"packet_queue"`
}

type ErrorCounters struct {
	Capture       uint64 `json:"capture"`
	Encode        uint64 `json:"encode"`
	Packetization uint64 `json:"packetization"`
	UDPSend       uint64 `json:"udp_send"`
}

type MetricsSnapshot struct {
	StartedAt               time.Time     `json:"started_at"`
	At                      time.Time     `json:"at"`
	Resolution              Resolution    `json:"resolution"`
	SourceFPS               float64       `json:"source_fps"`
	EncodedFPS              float64       `json:"encoded_fps"`
	BitrateBPS              float64       `json:"bitrate_bps"`
	EncodedFrames           uint64        `json:"encoded_frames"`
	EncodedBytes            uint64        `json:"encoded_bytes"`
	Packets                 uint64        `json:"packets"`
	Keyframes               uint64        `json:"keyframes"`
	KeyframeRequests        uint64        `json:"keyframe_requests"`
	KeyframeRequestFailures uint64        `json:"keyframe_request_failures"`
	QueueDepth              int           `json:"queue_depth"`
	QueueHighWater          int           `json:"queue_high_water"`
	Drops                   DropCounters  `json:"drops"`
	Errors                  ErrorCounters `json:"errors"`
	RuntimeError            string        `json:"runtime_error,omitempty"`
	EncodeTiming            TimingSummary `json:"encode_timing"`
}

type Metrics struct {
	mu                      sync.Mutex
	startedAt               time.Time
	resolution              Resolution
	sourceFrames            uint64
	sourceFirstNS           int64
	sourceLastNS            int64
	sourceFPSOverride       float64
	encodedFrames           uint64
	encodedBytes            uint64
	packets                 uint64
	keyframes               uint64
	keyframeRequests        uint64
	keyframeRequestFailures uint64
	captureDrops            uint64
	queueDrops              uint64
	packetQueueDrops        uint64
	captureErrors           uint64
	encodeErrors            uint64
	packetizationErrors     uint64
	udpSendErrors           uint64
	runtimeError            string
	captureStatsApplied     bool
	encodeDurations         []time.Duration
	queueDepth              int
	queueHighWater          int
}

func NewMetrics(startedAt time.Time) *Metrics {
	return &Metrics{startedAt: startedAt, encodeDurations: make([]time.Duration, 0, 256)}
}

func (m *Metrics) SetSourceFormat(width, height int, rate FrameRate) {
	m.mu.Lock()
	if width > 0 && height > 0 {
		m.resolution = Resolution{Width: width, Height: height}
	}
	if rate.Numerator > 0 && rate.Denominator > 0 {
		m.sourceFPSOverride = float64(rate.Numerator) / float64(rate.Denominator)
	}
	m.mu.Unlock()
}

func (m *Metrics) SetSourceRate(rate FrameRate) {
	m.mu.Lock()
	if rate.Numerator > 0 && rate.Denominator > 0 {
		m.sourceFPSOverride = float64(rate.Numerator) / float64(rate.Denominator)
	}
	m.mu.Unlock()
}

func (m *Metrics) RecordCapture(captureMonoNS int64) {
	m.mu.Lock()
	m.sourceFrames++
	if m.sourceFrames == 1 {
		m.sourceFirstNS = captureMonoNS
	}
	m.sourceLastNS = captureMonoNS
	m.mu.Unlock()
}

func (m *Metrics) RecordEncodedFrame(encodedBytes int) {
	m.mu.Lock()
	m.encodedFrames++
	if encodedBytes > 0 {
		m.encodedBytes += uint64(encodedBytes)
	}
	m.mu.Unlock()
}

func (m *Metrics) RecordPacket(packetCount int) {
	m.mu.Lock()
	if packetCount > 0 {
		m.packets += uint64(packetCount)
	}
	m.mu.Unlock()
}

func (m *Metrics) RecordKeyframe() {
	m.mu.Lock()
	m.keyframes++
	m.mu.Unlock()
}

func (m *Metrics) RecordKeyframeRequest(failed bool) {
	m.mu.Lock()
	m.keyframeRequests++
	if failed {
		m.keyframeRequestFailures++
	}
	m.mu.Unlock()
}

func (m *Metrics) RecordEncodeDuration(duration time.Duration) {
	m.mu.Lock()
	if len(m.encodeDurations) == cap(m.encodeDurations) {
		copy(m.encodeDurations, m.encodeDurations[1:])
		m.encodeDurations = m.encodeDurations[:len(m.encodeDurations)-1]
	}
	m.encodeDurations = append(m.encodeDurations, duration)
	m.mu.Unlock()
}

func (m *Metrics) RecordCaptureDrop() {
	m.mu.Lock()
	m.captureDrops++
	m.mu.Unlock()
}

func (m *Metrics) RecordCaptureError() {
	m.mu.Lock()
	m.captureErrors++
	m.mu.Unlock()
}

func (m *Metrics) RecordQueueDrop() {
	m.mu.Lock()
	m.queueDrops++
	m.mu.Unlock()
}

func (m *Metrics) RecordEncodeError() {
	m.mu.Lock()
	m.encodeErrors++
	m.mu.Unlock()
}

func (m *Metrics) RecordPacketizationError() {
	m.mu.Lock()
	m.packetizationErrors++
	m.mu.Unlock()
}

func (m *Metrics) RecordUDPSendError() {
	m.mu.Lock()
	m.udpSendErrors++
	m.mu.Unlock()
}

func (m *Metrics) SetQueue(depth, highWater int) {
	m.mu.Lock()
	m.queueDepth = depth
	if highWater > m.queueHighWater {
		m.queueHighWater = highWater
	}
	m.mu.Unlock()
}

func (m *Metrics) ApplyCaptureStats(stats CaptureStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.captureStatsApplied {
		return
	}
	m.captureStatsApplied = true
	if stats.Width > 0 && stats.Height > 0 {
		m.resolution = Resolution{Width: stats.Width, Height: stats.Height}
	}
	if stats.FPS.Numerator > 0 && stats.FPS.Denominator > 0 {
		m.sourceFPSOverride = float64(stats.FPS.Numerator) / float64(stats.FPS.Denominator)
	}
	m.captureDrops += stats.DroppedFrames
	m.packetQueueDrops += stats.PacketQueueDrops
	m.encodeErrors += stats.EncodeErrors
	if stats.RuntimeError != "" {
		m.captureErrors++
		m.runtimeError = stats.RuntimeError
	}
	if stats.QueueDepth > m.queueDepth {
		m.queueDepth = stats.QueueDepth
	}
	if stats.QueueHighWater > m.queueHighWater {
		m.queueHighWater = stats.QueueHighWater
	}
}

func (m *Metrics) Snapshot(at time.Time, queueDepth, queueHighWater int) MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed := at.Sub(m.startedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	sourceFPS := float64(m.sourceFrames) / elapsed
	if m.sourceFPSOverride > 0 {
		sourceFPS = m.sourceFPSOverride
	} else if m.sourceFrames > 1 && m.sourceLastNS > m.sourceFirstNS {
		sourceFPS = float64(m.sourceFrames-1) / float64(m.sourceLastNS-m.sourceFirstNS) * 1e9
	}
	return MetricsSnapshot{
		StartedAt:               m.startedAt,
		At:                      at,
		Resolution:              m.resolution,
		SourceFPS:               sourceFPS,
		EncodedFPS:              float64(m.encodedFrames) / elapsed,
		BitrateBPS:              float64(m.encodedBytes*8) / elapsed,
		EncodedFrames:           m.encodedFrames,
		EncodedBytes:            m.encodedBytes,
		Packets:                 m.packets,
		Keyframes:               m.keyframes,
		KeyframeRequests:        m.keyframeRequests,
		KeyframeRequestFailures: m.keyframeRequestFailures,
		QueueDepth:              maxInt(m.queueDepth, queueDepth),
		QueueHighWater:          maxInt(m.queueHighWater, queueHighWater),
		Drops:                   DropCounters{Capture: m.captureDrops, Queue: m.queueDrops, PacketQueue: m.packetQueueDrops},
		Errors:                  ErrorCounters{Capture: m.captureErrors, Encode: m.encodeErrors, Packetization: m.packetizationErrors, UDPSend: m.udpSendErrors},
		RuntimeError:            m.runtimeError,
		EncodeTiming:            summarizeDurations(m.encodeDurations),
	}
}

func summarizeDurations(durations []time.Duration) TimingSummary {
	if len(durations) == 0 {
		return TimingSummary{}
	}
	values := make([]float64, len(durations))
	for i, duration := range durations {
		values[i] = float64(duration) / float64(time.Millisecond)
	}
	sort.Float64s(values)
	return TimingSummary{
		Count:     len(values),
		P50Millis: percentile(values, 0.50),
		P95Millis: percentile(values, 0.95),
		MaxMillis: values[len(values)-1],
	}
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(values))*fraction)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
