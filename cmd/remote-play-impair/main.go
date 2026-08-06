package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"time"
)

type report struct {
	Mode      string `json:"mode"`
	Forwarded uint64 `json:"forwarded"`
	Dropped   uint64 `json:"dropped"`
	Received  uint64 `json:"received"`
	Reordered uint64 `json:"reordered"`
	Malformed uint64 `json:"malformed"`
	Shutdown  string `json:"shutdown,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "remote-play-impair: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr *os.File) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil
	}
	flags := flag.NewFlagSet("remote-play-impair", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:5604", "UDP listen address")
	forward := flags.String("forward", "127.0.0.1:5504", "UDP destination")
	dropEvery := flags.Uint64("drop-every", 0, "drop every Nth packet; zero disables dropping")
	reorderWindow := flags.Uint("reorder-window", 0, "hold this many packets and forward them in reverse order")
	quiet := flags.Bool("quiet", false, "suppress the final JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dropEvery == 1 {
		return errors.New("--drop-every must be zero or greater than one")
	}
	if *reorderWindow > 64 {
		return errors.New("--reorder-window must be <= 64")
	}
	listenAddr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		return fmt.Errorf("resolve listen address: %w", err)
	}
	destination, err := net.ResolveUDPAddr("udp", *forward)
	if err != nil {
		return fmt.Errorf("resolve forward address: %w", err)
	}
	in, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("bind listen address: %w", err)
	}
	defer in.Close()
	out, err := net.DialUDP("udp", nil, destination)
	if err != nil {
		return fmt.Errorf("open forward socket: %w", err)
	}
	defer out.Close()
	state := report{Mode: "impairment"}
	buffer := make([]byte, 64<<10)
	queue := make([][]byte, 0, int(*reorderWindow))
	flush := func() {
		for i := len(queue) - 1; i >= 0; i-- {
			if _, err := out.Write(queue[i]); err == nil {
				state.Forwarded++
				state.Reordered++
			}
		}
		queue = queue[:0]
	}
	for {
		if err := in.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return err
		}
		n, _, err := in.ReadFromUDP(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					state.Shutdown = "context canceled"
					break
				}
				if len(queue) > 0 {
					flush()
				}
				continue
			}
			return fmt.Errorf("read UDP packet: %w", err)
		}
		if n < 12 {
			state.Malformed++
			continue
		}
		state.Received++
		drop := *dropEvery != 0 && state.Received%*dropEvery == 0
		if drop {
			state.Dropped++
		}
		if !drop {
			packet := append([]byte(nil), buffer[:n]...)
			if *reorderWindow == 0 {
				if _, err := out.Write(packet); err != nil {
					return fmt.Errorf("forward UDP packet: %w", err)
				}
				state.Forwarded++
			} else {
				queue = append(queue, packet)
				if len(queue) >= int(*reorderWindow) {
					flush()
				}
			}
		}
	}
	encoded, _ := json.MarshalIndent(state, "", "  ")
	if !*quiet && stdout != nil {
		_, _ = stdout.Write(append(encoded, '\n'))
	}
	return nil
}
