//go:build linux

package menudisplay

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestStatusAndSealedPresentation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		if _, _, err = readLine(conn); err != nil {
			done <- err
			return
		}
		_, _, err = conn.WriteMsgUnix([]byte(`{"protocol":2,"ok":true,"menu_display":{"available":true,"generation":7,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"displayed_sequence":3,"underflows":0}}`+"\n"), nil, nil)
		conn.Close()
		if err != nil {
			done <- err
			return
		}
		conn, err = listener.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		begin, fds, err := readLine(conn)
		if err != nil || len(fds) != 0 {
			done <- err
			return
		}
		var request map[string]any
		if err = json.Unmarshal(begin, &request); err != nil {
			done <- err
			return
		}
		if request["operation"] != "menu_frame_begin" || request["expected_generation"] != float64(7) {
			done <- errUnexpected
			return
		}
		fd, err := unix.MemfdCreate("test-menu", unix.MFD_ALLOW_SEALING)
		if err != nil {
			done <- err
			return
		}
		file := os.NewFile(uintptr(fd), "test-menu")
		defer file.Close()
		if err = file.Truncate(FrameBytes); err != nil {
			done <- err
			return
		}
		staging := []byte(`{"protocol":2,"ok":true,"menu_frame":{"generation":7,"byte_count":3686400,"staging_format":"rgba8888","displayed_sequence":null,"underflows":0}}` + "\n")
		_, _, err = conn.WriteMsgUnix(staging[:60], unix.UnixRights(fd), nil)
		if err == nil {
			_, _, err = conn.WriteMsgUnix(staging[60:], nil, nil)
		}
		if err != nil {
			done <- err
			return
		}
		commit, fds, err := readLine(conn)
		if err != nil || len(fds) != 1 {
			done <- errUnexpected
			return
		}
		defer unix.Close(fds[0])
		if err = json.Unmarshal(commit, &request); err != nil {
			done <- err
			return
		}
		if request["operation"] != "menu_frame_commit" || request["generation"] != float64(7) {
			done <- errUnexpected
			return
		}
		seals, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GET_SEALS, 0)
		if err != nil {
			done <- err
			return
		}
		if seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL {
			done <- errUnexpected
			return
		}
		buf := make([]byte, 4)
		if _, err = file.ReadAt(buf, 0); err != nil || string(buf) != "RGBA" {
			done <- errUnexpected
			return
		}
		_, _, err = conn.WriteMsgUnix([]byte(`{"protocol":2,"ok":true,"menu_frame":{"generation":7,"byte_count":3686400,"displayed_sequence":4,"underflows":0}}`+"\n"), nil, nil)
		done <- err
	}()
	client := New(path)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil || !status.Available || status.Generation != 7 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	pix := make([]byte, FrameBytes)
	copy(pix, "RGBA")
	result, err := client.Present(ctx, status.Generation, pix)
	if err != nil || result.DisplayedSequence != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPresentRejectsInvalidFrame(t *testing.T) {
	client := New(filepath.Join(t.TempDir(), "missing"))
	if _, err := client.Present(context.Background(), 0, make([]byte, FrameBytes)); err == nil {
		t.Fatal("zero generation accepted")
	}
	if _, err := client.Present(context.Background(), 1, make([]byte, 1)); err == nil {
		t.Fatal("short pixels accepted")
	}
}

var errUnexpected = &unexpected{}

type unexpected struct{}

func (*unexpected) Error() string { return "unexpected protocol value" }

func readLine(conn *net.UnixConn) ([]byte, []int, error) {
	var line []byte
	var fds []int
	for {
		buf := make([]byte, 4096)
		oob := make([]byte, unix.CmsgSpace(4))
		n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
		if err != nil {
			return nil, nil, err
		}
		line = append(line, buf[:n]...)
		if oobn > 0 {
			msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
			if err != nil {
				return nil, nil, err
			}
			for _, msg := range msgs {
				got, err := unix.ParseUnixRights(&msg)
				if err != nil {
					return nil, nil, err
				}
				fds = append(fds, got...)
			}
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			return line[:len(line)-1], fds, nil
		}
		if len(line) > 65536 {
			return nil, nil, &unexpected{}
		}
	}
}
