package remotemedia

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type senderTestSource struct {
	mu       sync.Mutex
	started  bool
	closed   bool
	startErr error
	frames   []EncodedSample
	stats    CaptureStats
}

func (s *senderTestSource) Start() error {
	s.mu.Lock()
	s.started = true
	err := s.startErr
	s.mu.Unlock()
	return err
}
func (s *senderTestSource) Next(ctx context.Context) (EncodedSample, error) {
	for {
		s.mu.Lock()
		if len(s.frames) > 0 {
			frame := s.frames[0]
			s.frames = s.frames[1:]
			s.mu.Unlock()
			return frame, nil
		}
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return EncodedSample{}, errors.New("source closed")
		}
		select {
		case <-ctx.Done():
			return EncodedSample{}, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
func (s *senderTestSource) Stats() CaptureStats { s.mu.Lock(); defer s.mu.Unlock(); return s.stats }
func (s *senderTestSource) Close() error        { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }

func TestSenderWritesMediaHelloWithCodecContract(t *testing.T) {
	source := &senderTestSource{}
	sender, err := NewSender(SenderConfig{RTPAddress: "127.0.0.1:1", Session: "session", Generation: 2, Token: "token", SSRC: 9}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	var wire bytes.Buffer
	if err := sender.WriteMediaHello(&wire, 1280, 720, FrameRate{Numerator: 60000, Denominator: 1001}, 8_000_000); err != nil {
		t.Fatalf("write media hello: %v", err)
	}
	message, err := ReadControlMessage(&wire)
	if err != nil {
		t.Fatalf("read media hello: %v", err)
	}
	if message.Type != ControlMediaHello || message.Session != "session" || message.Generation != 2 {
		t.Fatalf("message = %#v", message)
	}
	var body struct {
		KeyframeIntervalFrames int    `json:"keyframe_interval_frames"`
		KeyframeInterval       string `json:"keyframe_interval"`
	}
	if err := json.Unmarshal(message.Body, &body); err != nil {
		t.Fatalf("decode MEDIA_HELLO body: %v", err)
	}
	if body.KeyframeIntervalFrames != 30 || body.KeyframeInterval != (500*time.Millisecond).String() {
		t.Fatalf("keyframe contract = %#v", body)
	}
}

func TestNewSenderRejectsRTPMTUTooSmall(t *testing.T) {
	_, err := NewSender(SenderConfig{RTPAddress: "127.0.0.1:1", Session: "session", Token: "token", SSRC: 1, MTU: RTPHeaderSize + 2}, &senderTestSource{}, nil)
	if err == nil || !strings.Contains(err.Error(), "MTU") {
		t.Fatalf("small MTU error = %v", err)
	}
}

func TestSenderSendsRTPWithCaptureTimestampAndStopsCleanly(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	defer udp.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &senderTestSource{frames: []EncodedSample{{CaptureMonoNS: 1_000_000_000, AVCC: []byte{0, 0, 0, 2, 0x65, 0x01}, SPS: []byte{0x67, 0x42}, PPS: []byte{0x68, 0xce}, NALLengthSize: 4, Keyframe: true, Width: 640, Height: 480}}}
	sender, err := NewSender(SenderConfig{RTPAddress: udp.LocalAddr().String(), Session: "session", Generation: 1, Token: "token", SSRC: 0x1234, InitialSequence: 7, RTPBaseTimestamp: 99}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- sender.Run(ctx) }()
	udp.SetReadDeadline(time.Now().Add(time.Second))
	wire := make([]byte, 1500)
	n, _, err := udp.ReadFromUDP(wire)
	if err != nil {
		t.Fatalf("read RTP: %v", err)
	}
	if n < RTPHeaderSize || binary.BigEndian.Uint16(wire[2:4]) != 7 || binary.BigEndian.Uint32(wire[4:8]) != 99 {
		t.Fatalf("RTP header = %x", wire[:n])
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("sender result = %v", err)
	}
	if !source.closed {
		t.Fatal("sender did not close capture source")
	}
}

func TestSenderMediaControlSendsHelloAndStops(t *testing.T) {
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen control: %v", err)
	}
	defer controlListener.Close()
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	defer udp.Close()
	source := &senderTestSource{frames: []EncodedSample{{CaptureMonoNS: 1, AVCC: []byte{0, 0, 0, 2, 0x65, 1}, SPS: []byte{0x67, 0x42}, PPS: []byte{0x68, 0xce}, NALLengthSize: 4, Keyframe: true}}}
	sender, err := NewSender(SenderConfig{RTPAddress: udp.LocalAddr().String(), ControlAddress: controlListener.Addr().String(), Session: "session", Generation: 2, Token: "token", SSRC: 11, Bitrate: 1000}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := controlListener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sender.Run(ctx) }()
	var controlConn net.Conn
	select {
	case controlConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("control connection was not established")
	}
	defer controlConn.Close()
	controlConn.SetReadDeadline(time.Now().Add(time.Second))
	hello, err := ReadControlMessage(controlConn)
	if err != nil {
		t.Fatalf("read MEDIA_HELLO: %v", err)
	}
	if hello.Type != ControlMediaHello {
		t.Fatalf("first control message = %#v", hello)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("sender result = %v", err)
	}
}

func TestSenderCloseCancelsActiveRunBeforeClosingSource(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	defer udp.Close()
	source := &senderTestSource{}
	sender, err := NewSender(SenderConfig{RTPAddress: udp.LocalAddr().String(), Session: "session", Generation: 1, Token: "token", SSRC: 1}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- sender.Run(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		source.mu.Lock()
		started := source.started
		source.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sender did not start capture source")
		}
		time.Sleep(time.Millisecond)
	}
	if err := sender.Close(); err != nil {
		t.Fatalf("close sender: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("sender result = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("sender did not stop after Close")
	}
	source.mu.Lock()
	closed := source.closed
	source.mu.Unlock()
	if !closed {
		t.Fatal("sender did not close capture source")
	}
}

func TestSenderSnapshotsAndClosesSourceWhenStartFails(t *testing.T) {
	source := &senderTestSource{startErr: errors.New("capture start failed"), stats: CaptureStats{Width: 1280, Height: 720}}
	sender, err := NewSender(SenderConfig{RTPAddress: "127.0.0.1:1", Session: "session", Generation: 1, Token: "token", SSRC: 1}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if err := sender.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "capture start failed") {
		t.Fatalf("sender result = %v", err)
	}
	stats, ready := sender.CaptureStats()
	source.mu.Lock()
	closed := source.closed
	source.mu.Unlock()
	if !ready || stats.Width != 1280 || !closed {
		t.Fatalf("capture stats/source close = %#v, %v, %v", stats, ready, closed)
	}
}

func TestSenderStopsWhenControlConnectionCloses(t *testing.T) {
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen control: %v", err)
	}
	defer controlListener.Close()
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	defer udp.Close()
	source := &senderTestSource{}
	sender, err := NewSender(SenderConfig{RTPAddress: udp.LocalAddr().String(), ControlAddress: controlListener.Addr().String(), Session: "session", Generation: 1, Token: "token", SSRC: 1}, source, nil)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := controlListener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	done := make(chan error, 1)
	go func() { done <- sender.Run(context.Background()) }()
	var controlConn net.Conn
	select {
	case controlConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("control connection was not established")
	}
	if _, err := ReadControlMessage(controlConn); err != nil {
		t.Fatalf("read MEDIA_HELLO: %v", err)
	}
	if err := controlConn.Close(); err != nil {
		t.Fatalf("close control connection: %v", err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "media control connection closed") {
			t.Fatalf("sender result = %v, want control connection failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("sender continued after control connection closed")
	}
}
