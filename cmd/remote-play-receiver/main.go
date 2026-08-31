package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/internal/remotemedia"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "remote-play-receiver: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil
	}
	flags := flag.NewFlagSet("remote-play-receiver", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rtpAddress := flags.String("rtp", "127.0.0.1:5004", "RTP/H.264 UDP listen address")
	controlAddress := flags.String("control", "", "optional authenticated control TCP listen address")
	session := flags.String("session", "", "required session identifier")
	generation := flags.Uint64("generation", 1, "required session generation")
	token := flags.String("token", "", "required session token")
	ssrc := flags.Uint("ssrc", 0, "optional expected RTP SSRC; normally learned from MEDIA_HELLO")
	decode := flags.String("decode", "none", "decoder backend (none is the explicit unsupported boundary)")
	metricsPath := flags.String("metrics", "", "optional final receiver JSON report path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *session == "" {
		return errors.New("--session is required")
	}
	if *token == "" {
		return errors.New("--token is required")
	}
	if *ssrc > 0xffffffff {
		return errors.New("--ssrc must fit in uint32")
	}
	if *decode != "none" && *decode != "ffplay" {
		result := map[string]any{"status": "decode_not_configured", "decode_backend": *decode, "runtime": runtime.GOOS}
		encoded, _ := json.Marshal(result)
		encoded = append(encoded, '\n')
		if stdout != nil {
			_, _ = stdout.Write(encoded)
		}
		return errors.New("decode/display is not configured; supported backend is ffplay")
	}
	if *decode == "ffplay" {
		if _, err := exec.LookPath("ffplay"); err != nil {
			return fmt.Errorf("decode/display backend ffplay is unavailable: %w", err)
		}
	}
	if *decode == "none" {
		result := map[string]any{"status": "decode_not_configured", "decode_backend": *decode, "runtime": runtime.GOOS}
		encoded, _ := json.Marshal(result)
		encoded = append(encoded, '\n')
		if stdout != nil {
			_, _ = stdout.Write(encoded)
		}
		return errors.New("decode/display is not configured; receiver only provides validated Annex-B access units")
	}
	r, err := remotemedia.NewReceiver(remotemedia.ReceiverConfig{Session: *session, Generation: *generation, Token: *token, SSRC: uint32(*ssrc)})
	if err != nil {
		return err
	}
	defer writeReceiverReport(*metricsPath, *session, *generation, r, *decode == "ffplay")
	defer r.Close()
	var decoder *exec.Cmd
	var decoderInput io.WriteCloser
	var decoderQueue chan []byte
	var decoderDone chan struct{}
	if *decode == "ffplay" {
		decoder = exec.CommandContext(ctx, "ffplay", "-loglevel", "warning", "-fflags", "nobuffer", "-flags", "low_delay", "-f", "h264", "-i", "pipe:0")
		decoder.Stderr = stderr
		decoderInput, err = decoder.StdinPipe()
		if err != nil {
			return fmt.Errorf("open ffplay input: %w", err)
		}
		if err := decoder.Start(); err != nil {
			return fmt.Errorf("start ffplay: %w", err)
		}
		decoderQueue = make(chan []byte, 8)
		decoderDone = make(chan struct{})
		go func() {
			defer close(decoderDone)
			for frame := range decoderQueue {
				if _, err := decoderInput.Write(frame); err != nil {
					return
				}
			}
		}()
		defer func() {
			close(decoderQueue)
			<-decoderDone
			_ = decoderInput.Close()
			_ = decoder.Wait()
		}()
	}
	rtpListenAddress, err := net.ResolveUDPAddr("udp", *rtpAddress)
	if err != nil {
		return fmt.Errorf("resolve RTP UDP address: %w", err)
	}
	conn, err := net.ListenUDP("udp", rtpListenAddress)
	if err != nil {
		return fmt.Errorf("bind RTP UDP socket: %w", err)
	}
	defer conn.Close()
	var control net.Listener
	if *controlAddress != "" {
		control, err = net.Listen("tcp", *controlAddress)
		if err != nil {
			return fmt.Errorf("bind control TCP socket: %w", err)
		}
		defer control.Close()
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "receiver_bound rtp=%s control=%s decoder=%s\n", conn.LocalAddr(), listenerAddress(control), *decode)
	}
	if control != nil {
		go acceptControl(ctx, control, r)
	}
	buffer := make([]byte, 64<<10)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return err
		}
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read RTP packet: %w", err)
		}
		units, err := r.Ingest(buffer[:n])
		if err != nil && stdout != nil {
			_, _ = fmt.Fprintf(stdout, "packet_rejected: %v\n", err)
		}
		if err == nil && decoderQueue != nil {
			for _, unit := range units {
				annexB := accessUnitAnnexB(unit)
				select {
				case decoderQueue <- annexB:
				default:
					r.RecordDecodeDrop()
				}
			}
		}
	}
}

func accessUnitAnnexB(unit remotemedia.AccessUnit) []byte {
	var out []byte
	for _, nal := range unit.NALs {
		out = append(out, 0, 0, 0, 1)
		out = append(out, nal...)
	}
	return out
}

func listenerAddress(l net.Listener) string {
	if l == nil {
		return ""
	}
	return l.Addr().String()
}

func writeReceiverReport(path, session string, generation uint64, receiver *remotemedia.Receiver, decodeConfigured bool) {
	if path == "" || receiver == nil {
		return
	}
	report := struct {
		Mode             string                     `json:"mode"`
		Session          string                     `json:"session"`
		Generation       uint64                     `json:"generation"`
		PhysicalCapture  bool                       `json:"physical_capture"`
		DecodeConfigured bool                       `json:"decode_configured"`
		Receiver         remotemedia.ReceiverReport `json:"receiver"`
	}{Mode: "receiver", Session: session, Generation: generation, DecodeConfigured: decodeConfigured, Receiver: receiver.Report()}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func acceptControl(ctx context.Context, listener net.Listener, r *remotemedia.Receiver) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go func() {
			defer conn.Close()
			for {
				message, err := remotemedia.ReadControlMessage(conn)
				if err != nil {
					return
				}
				if err := r.AcceptControl(message); err != nil {
					return
				}
				switch message.Type {
				case remotemedia.ControlMediaHello:
					if err := remotemedia.WriteControlMessage(conn, remotemedia.ControlMessage{Type: remotemedia.ControlMediaWelcome, Session: message.Session, Generation: message.Generation, Token: message.Token}); err != nil {
						return
					}
				case remotemedia.ControlPing:
					if err := remotemedia.WriteControlMessage(conn, remotemedia.ControlMessage{Type: remotemedia.ControlPong, Session: message.Session, Generation: message.Generation, Token: message.Token, Body: message.Body}); err != nil {
						return
					}
				case remotemedia.ControlStop:
					return
				}
			}
		}()
	}
}
