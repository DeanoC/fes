package remotemedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type SenderConfig struct {
	RTPAddress        string
	ControlAddress    string
	Session           string
	Generation        uint64
	Token             string
	SSRC              uint32
	InitialSequence   uint16
	RTPBaseTimestamp  uint32
	MTU               int
	Bitrate           int
	PeriodicKeyframes int
	KeyframeInterval  time.Duration
}

type Sender struct {
	config     SenderConfig
	source     CaptureSource
	metrics    *Metrics
	clock      *RTPClock
	mu         sync.Mutex
	closed     bool
	runCancel  context.CancelFunc
	stats      CaptureStats
	statsReady bool
	closeOnce  sync.Once
	closeErr   error
}

func NewSender(config SenderConfig, source CaptureSource, metrics *Metrics) (*Sender, error) {
	if source == nil {
		return nil, errors.New("capture source is required")
	}
	if config.RTPAddress == "" {
		return nil, errors.New("RTP address is required")
	}
	if config.Session == "" || config.Token == "" {
		return nil, errors.New("session and token are required")
	}
	if config.SSRC == 0 {
		return nil, errors.New("SSRC must be nonzero")
	}
	if config.MTU != 0 && config.MTU < RTPHeaderSize+3 {
		return nil, fmt.Errorf("RTP MTU %d is too small", config.MTU)
	}
	if config.MTU == 0 {
		config.MTU = DefaultRTPMTU
	}
	if config.PeriodicKeyframes <= 0 {
		config.PeriodicKeyframes = 30
	}
	if config.KeyframeInterval <= 0 {
		config.KeyframeInterval = 500 * time.Millisecond
	}
	if metrics == nil {
		metrics = NewMetrics(time.Now())
	}
	return &Sender{
		config:  config,
		source:  source,
		metrics: metrics,
		clock:   NewRTPClock(config.RTPBaseTimestamp),
	}, nil
}

func (s *Sender) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("sender is closed")
	}
	if s.runCancel != nil {
		s.mu.Unlock()
		return errors.New("sender is already running")
	}
	s.runCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.runCancel = nil
		s.mu.Unlock()
	}()
	if err := s.source.Start(); err != nil {
		s.finalizeCapture()
		return fmt.Errorf("start capture source: %w", err)
	}

	var control *controlWriter
	var controlErrMu sync.Mutex
	var controlErr error
	setControlErr := func(err error) {
		if err == nil {
			return
		}
		controlErrMu.Lock()
		if controlErr == nil {
			controlErr = err
		}
		controlErrMu.Unlock()
	}
	getControlErr := func() error {
		controlErrMu.Lock()
		defer controlErrMu.Unlock()
		return controlErr
	}
	var workers sync.WaitGroup
	defer func() {
		cancel()
		if control != nil {
			_ = control.Close()
		}
		workers.Wait()
		s.finalizeCapture()
	}()

	initialStats := s.source.Stats()
	s.metrics.SetSourceFormat(initialStats.Width, initialStats.Height, initialStats.FPS)

	if s.config.ControlAddress != "" {
		conn, err := (&net.Dialer{}).DialContext(runCtx, "tcp", s.config.ControlAddress)
		if err != nil {
			return fmt.Errorf("dial media control destination: %w", err)
		}
		control = &controlWriter{conn: conn}
		if err := s.WriteMediaHello(control.conn, initialStats.Width, initialStats.Height, initialStats.FPS, s.config.Bitrate); err != nil {
			return fmt.Errorf("send MEDIA_HELLO: %w", err)
		}
		workers.Add(2)
		go func() {
			defer workers.Done()
			err := s.controlLoop(runCtx, control)
			setControlErr(err)
			cancel()
		}()
		go func() {
			defer workers.Done()
			err := s.reportLoop(runCtx, control)
			setControlErr(err)
			cancel()
		}()
	}

	conn, err := (&net.Dialer{}).DialContext(runCtx, "udp", s.config.RTPAddress)
	if err != nil {
		return fmt.Errorf("dial RTP destination: %w", err)
	}
	defer conn.Close()

	packetizer := NewRTPPacketizer(s.config.MTU, s.config.SSRC, s.config.InitialSequence)
	for {
		select {
		case <-runCtx.Done():
			controlFailure := getControlErr()
			if controlFailure != nil && !errors.Is(controlFailure, context.Canceled) {
				return controlFailure
			}
			return runCtx.Err()
		default:
		}
		sample, err := s.source.Next(runCtx)
		if err != nil {
			if runCtx.Err() != nil {
				if controlFailure := getControlErr(); controlFailure != nil && !errors.Is(controlFailure, context.Canceled) {
					return controlFailure
				}
				return runCtx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if controlFailure := getControlErr(); controlFailure != nil && !errors.Is(controlFailure, context.Canceled) {
					return controlFailure
				}
				return err
			}
			return fmt.Errorf("capture source: %w", err)
		}
		s.metrics.RecordCapture(sample.CaptureMonoNS)
		stats := s.source.Stats()
		if sample.Width > 0 && sample.Height > 0 {
			s.metrics.SetSourceFormat(sample.Width, sample.Height, stats.FPS)
		}
		timestamp, err := s.clock.Timestamp(sample.CaptureMonoNS)
		if err != nil {
			s.metrics.RecordCaptureError()
			return fmt.Errorf("capture timestamp: %w", err)
		}
		annexB, err := (EncodedAccessUnit{
			AVCC:          sample.AVCC,
			SPS:           sample.SPS,
			PPS:           sample.PPS,
			NALLengthSize: sample.NALLengthSize,
			Keyframe:      sample.Keyframe,
		}).AnnexB()
		if err != nil {
			s.metrics.RecordPacketizationError()
			return fmt.Errorf("convert encoded access unit: %w", err)
		}
		nals, err := ParseAnnexBNALs(annexB)
		if err != nil {
			s.metrics.RecordPacketizationError()
			return fmt.Errorf("parse encoded access unit: %w", err)
		}
		packets, err := packetizer.Packetize(AccessUnit{NALs: nals, Timestamp: timestamp, Keyframe: sample.Keyframe})
		if err != nil {
			s.metrics.RecordPacketizationError()
			return fmt.Errorf("packetize encoded access unit: %w", err)
		}
		for _, packet := range packets {
			if _, err := conn.Write(packet.Marshal()); err != nil {
				s.metrics.RecordUDPSendError()
				return fmt.Errorf("send RTP packet: %w", err)
			}
		}
		s.metrics.RecordEncodedFrame(len(sample.AVCC))
		s.metrics.RecordPacket(len(packets))
		s.metrics.RecordEncodeDuration(sample.EncodeDuration)
		if sample.Keyframe {
			s.metrics.RecordKeyframe()
		}
	}
}

func (s *Sender) finalizeCapture() {
	stats := s.source.Stats()
	s.mu.Lock()
	s.stats = stats
	s.statsReady = true
	s.mu.Unlock()
	s.metrics.ApplyCaptureStats(stats)
	_ = s.closeSource()
}

func (s *Sender) closeSource() error {
	s.closeOnce.Do(func() { s.closeErr = s.source.Close() })
	return s.closeErr
}

func (s *Sender) Close() error {
	s.mu.Lock()
	s.closed = true
	cancel := s.runCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		return s.closeSource()
	}
	return s.closeSource()
}

func (s *Sender) Metrics() *Metrics { return s.metrics }

func (s *Sender) Config() SenderConfig { return s.config }

func (s *Sender) CaptureStats() (CaptureStats, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats, s.statsReady
}

func (s *Sender) WriteMediaHello(w io.Writer, width, height int, rate FrameRate, bitrate int) error {
	body, err := json.Marshal(map[string]any{
		"width":                    width,
		"height":                   height,
		"fps":                      rate,
		"bitrate":                  bitrate,
		"payload_type":             RTPPayloadTypeH264,
		"clock_rate":               RTPClockRate,
		"ssrc":                     s.config.SSRC,
		"mtu":                      s.config.MTU,
		"b_frames":                 false,
		"keyframe_request":         "unsupported_periodic_idr",
		"keyframe_interval_frames": s.config.PeriodicKeyframes,
		"keyframe_interval":        s.config.KeyframeInterval.String(),
	})
	if err != nil {
		return err
	}
	return WriteControlMessage(w, ControlMessage{Type: ControlMediaHello, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: body})
}

func (s *Sender) controlLoop(ctx context.Context, control *controlWriter) error {
	for {
		message, err := ReadControlMessage(control.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return fmt.Errorf("media control connection closed: %w", err)
			}
			return fmt.Errorf("read media control message: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := ValidateControlMessage(message, s.config.Session, s.config.Generation, s.config.Token); err != nil {
			return fmt.Errorf("validate media control message: %w", err)
		}
		switch message.Type {
		case ControlKeyframeRequest:
			failed := true
			if requester, ok := s.source.(KeyframeRequester); ok {
				failed = requester.RequestKeyframe() != nil
			}
			s.metrics.RecordKeyframeRequest(failed)
		case ControlPing:
			if err := control.Write(ControlMessage{Type: ControlPong, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: message.Body}); err != nil {
				return fmt.Errorf("write media control PONG: %w", err)
			}
		case ControlStop:
			return nil
		}
	}
}

func (s *Sender) reportLoop(ctx context.Context, control *controlWriter) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			stats := s.source.Stats()
			s.metrics.SetSourceFormat(stats.Width, stats.Height, stats.FPS)
			snapshot := s.metrics.Snapshot(now, stats.QueueDepth, stats.QueueHighWater)
			body, err := json.Marshal(struct {
				Metrics MetricsSnapshot `json:"metrics"`
				Capture CaptureStats    `json:"capture"`
			}{Metrics: snapshot, Capture: stats})
			if err != nil {
				return fmt.Errorf("marshal media report: %w", err)
			}
			if err := control.Write(ControlMessage{Type: ControlMediaReport, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: body}); err != nil {
				return fmt.Errorf("write media report: %w", err)
			}
		}
	}
}

type controlWriter struct {
	conn net.Conn
	mu   sync.Mutex
}

func (w *controlWriter) Write(message ControlMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WriteControlMessage(w.conn, message)
}

func (w *controlWriter) Close() error {
	return w.conn.Close()
}
