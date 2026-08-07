package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("fbbridge", flag.ContinueOnError)
	rtp := f.String("rtp", ":5510", "")
	rcvbuf := f.Int("rtp-recvbuf", 8<<20, "UDP receive buffer size in bytes")
	fbpath := f.String("framebuffer", "/dev/fb0", "")
	nativeCmd := f.String("native-cmd", "/dev/MiSTer_cmd", "native Main_MiSTer command FIFO")
	nativeMode := f.String("native-mode", "8888 1 1920 1080", "native framebuffer mode arguments")
	session := f.String("session", "", "")
	token := f.String("token", "", "")
	noAuth := f.Bool("no-auth", false, "skip control authentication (disposable test only)")
	dumpPath := f.String("dump-annexb", "", "optional Annex-B dump path for decoder-input diagnostics")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *session == "" || *token == "" {
		return errors.New("session and token required")
	}
	if err := activateNativeFramebuffer(*nativeCmd, *nativeMode); err != nil {
		return err
	}
	fb, err := openNativeFramebuffer(*fbpath)
	if err != nil {
		return err
	}
	defer fb.Close()
	fmt.Fprintf(os.Stderr, "fbbridge_framebuffer opened %dx%d stride=%d\n", fb.Width(), fb.Height(), fb.Stride())
	var dump *os.File
	if *dumpPath != "" {
		dump, err = os.OpenFile(*dumpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("open Annex-B dump: %w", err)
		}
		defer dump.Close()
		fmt.Fprintf(os.Stderr, "fbbridge_dump path=%s\n", *dumpPath)
	}
	decoder := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "warning", "-f", "h264", "-i", "pipe:0", "-vf", fmt.Sprintf("scale=%d:%d", fb.Width(), fb.Height()), "-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1")
	decoder.Stderr = os.Stderr
	in, err := decoder.StdinPipe()
	if err != nil {
		return err
	}
	out, err := decoder.StdoutPipe()
	if err != nil {
		return err
	}
	if err = decoder.Start(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "fbbridge_decoder_started")
	var packets, accessUnits, decodeFrames, fbWrites, packetErrors, decoderWrites, dumpBytes atomic.Uint64
	var frameLogged atomic.Bool
	var started atomic.Bool
	var seenSPS atomic.Bool
	var seenPPS atomic.Bool
	var droppedOnGap atomic.Uint64
	var unitsLogged atomic.Uint64
	var nalLogged atomic.Bool

	logUnit := func(u remotemedia.AccessUnit) {
		if unitsLogged.Load() >= 8 || nalLogged.Swap(true) {
			return
		}
		unitsLogged.Add(1)
		for _, nal := range u.NALs {
			if len(nal) > 0 {
				fmt.Fprintf(os.Stderr, "fbbridge_nal type=%d len=%d keyframe=%t\\n", nal[0]&0x1f, len(nal), u.Keyframe)
			}
		}
	}
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fmt.Fprintf(os.Stderr, "fbbridge_stats packets=%d units=%d decoder_writes=%d decoded_frames=%d fb_writes=%d packet_errors=%d dump_bytes=%d\n", packets.Load(), accessUnits.Load(), decoderWrites.Load(), decodeFrames.Load(), fbWrites.Load(), packetErrors.Load(), dumpBytes.Load())
		}
	}()
	unitCh := make(chan []byte, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, fb.Width()*fb.Height()*4)
		for {
			if _, e := io.ReadFull(out, buf); e != nil {
				return
			}
			decodeFrames.Add(1)
			if !frameLogged.Swap(true) {
				var sum uint64
				for i := 0; i < len(buf); i += 4096 {
					sum += uint64(buf[i])
				}
				fmt.Fprintf(os.Stderr, "fbbridge_frame first_frame_bytes=%d sampled_sum=%d first=%02x%02x%02x%02x\n", len(buf), sum, buf[0], buf[1], buf[2], buf[3])
			}
			if e := fb.Write(buf); e != nil {
				return
			}
			fbWrites.Add(1)
		}
	}()
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for payload := range unitCh {
			if dump != nil {
				n, err := dump.Write(payload)
				if err != nil {
					fmt.Fprintf(os.Stderr, "fbbridge_dump_error err=%v\n", err)
					return
				}
				dumpBytes.Add(uint64(n))
			}
			if _, err := in.Write(payload); err != nil {
				return
			}
			decoderWrites.Add(1)
			if dump != nil {
				_ = dump.Sync()
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
	defer func() {
		close(unitCh)
		<-writeDone
		in.Close()
		out.Close()
		<-done
		decoder.Wait()
	}()
	var r *remotemedia.Receiver
	if *noAuth {
		r = remotemedia.NewUnauthenticatedReceiver(remotemedia.ReceiverConfig{Session: *session, Token: *token, Generation: 1})
	} else {
		var err error
		r, err = remotemedia.NewReceiver(remotemedia.ReceiverConfig{Session: *session, Token: *token, Generation: 1})
		if err != nil {
			return err
		}
	}
	defer r.Close()
	a, err := net.ResolveUDPAddr("udp", *rtp)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return err
	}
	defer conn.Close()
	if *rcvbuf > 0 {
		if err := conn.SetReadBuffer(*rcvbuf); err != nil {
			return fmt.Errorf("set RTP receive buffer: %w", err)
		}
	}
	fmt.Printf("fbbridge_bound rtp=%s framebuffer=%s %dx%d\n", conn.LocalAddr(), *fbpath, fb.Width(), fb.Height())
	packet := make([]byte, 64<<10)
	var lastSSRC uint32
	var haveSSRC bool
	for {
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, e := conn.ReadFromUDP(packet)
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return nil
				}
				continue
			}
			return e
		}
		packets.Add(1)
		if len(packet[:n]) >= 12 {
			ssrc := uint32(packet[8])<<24 | uint32(packet[9])<<16 | uint32(packet[10])<<8 | uint32(packet[11])
			if haveSSRC && ssrc != lastSSRC {
				fmt.Fprintf(os.Stderr, "fbbridge_stream_reset old_ssrc=%08x new_ssrc=%08x\n", lastSSRC, ssrc)
			}
			lastSSRC, haveSSRC = ssrc, true
		}
		decodedUnits, e := r.Ingest(packet[:n])
		if e != nil {
			if packetErrors.Load() < 12 {
				fmt.Fprintf(os.Stderr, "fbbridge_packet_error n=%d err=%v\\n", packetErrors.Load()+1, e)
			}
			packetErrors.Add(1)
			continue
		}
		report := r.Report()
		if report.SequenceGaps > droppedOnGap.Load() {
			droppedOnGap.Store(report.SequenceGaps)
			started.Store(false)
			seenSPS.Store(false)
			seenPPS.Store(false)
			fmt.Fprintf(os.Stderr, "fbbridge_gap_reset gaps=%d\\n", report.SequenceGaps)
			continue
		}
		accessUnits.Add(uint64(len(decodedUnits)))
		for _, u := range decodedUnits {
			logUnit(u)
			if !started.Load() {
				hasIDR := false
				for _, nal := range u.NALs {
					if len(nal) == 0 {
						continue
					}
					switch nal[0] & 0x1f {
					case 7:
						seenSPS.Store(true)
					case 8:
						seenPPS.Store(true)
					case 5:
						hasIDR = true
					}
				}
				if !seenSPS.Load() || !seenPPS.Load() || !hasIDR {
					continue
				}
				if fb.Width() != 0 && fb.Height() != 0 {
					fmt.Fprintf(os.Stderr, "fbbridge_keyframe_ready nals=%d\n", len(u.NALs))
				}
				started.Store(true)
			}
			payload := accessUnitAnnexB(u)
			select {
			case unitCh <- payload:
			case <-ctx.Done():
				return nil
			}
		}
	}
}
func accessUnitAnnexB(u remotemedia.AccessUnit) []byte {
	var b []byte
	for _, n := range u.NALs {
		b = append(b, 0, 0, 0, 1)
		b = append(b, n...)
	}
	return b
}

var _ io.Writer
