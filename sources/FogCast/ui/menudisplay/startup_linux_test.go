//go:build linux

package menudisplay

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/testsock"
)

func TestStartupStatusPreservesPlaneAndRejectsUncertain(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, menu string
		wantErr    bool
	}{
		{"focused", fmt.Sprintf(`{"available":true,"session":true,"package_id":%q,"core_generation":7,"focus":"launcher","generation":2}`, pkg), false},
		{"shown", fmt.Sprintf(`{"available":true,"session":true,"package_id":%q,"core_generation":7,"focus":"machine","generation":2}`, pkg), false},
		{"closed", fmt.Sprintf(`{"available":false,"session":true,"package_id":%q,"core_generation":7,"focus":"machine","generation":0}`, pkg), false},
		{"missing availability", fmt.Sprintf(`{"session":true,"package_id":%q,"core_generation":7,"focus":"machine"}`, pkg), true},
		{"missing focus", fmt.Sprintf(`{"available":true,"session":true,"package_id":%q,"core_generation":7}`, pkg), true},
		{"hidden launcher focus", fmt.Sprintf(`{"available":false,"session":true,"package_id":%q,"core_generation":7,"focus":"launcher"}`, pkg), true},
		{"runtime display error", fmt.Sprintf(`{"available":false,"session":true,"package_id":%q,"core_generation":7,"focus":"machine","error":{"code":"io_failed"}}`, pkg), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testsock.Path(t, "status.sock")
			l, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, e := l.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				_, _ = bufio.NewReader(c).ReadString('\n')
				fmt.Fprintf(c, `{"ok":true,"state":"running_development","generation":7,"active_package":{"package_id":%q},"menu_display":%s}`+"\n", pkg, tc.menu)
			}()
			got, err := New(path).StartupStatus(context.Background())
			<-done
			if (err != nil) != tc.wantErr {
				t.Fatalf("status=%+v err=%v", got, err)
			}
			if err == nil && (got.State != "running_development" || got.PackageID != pkg || got.Generation != 7 || got.Plane == nil || got.Plane.Focus == "" || got.RuntimePID != os.Getpid() || got.RuntimeStart == "") {
				t.Fatalf("status=%+v", got)
			}
		})
	}
}

func TestStartupStatusRejectsRuntimeFailure(t *testing.T) {
	for _, body := range []string{`{"ok":false,"state":"idle","error":{"code":"io_failed","message":"unknown state"}}`, `{"ok":true,"state":"idle","error":{"message":"ambiguous failure"}}`} {
		path := testsock.Path(t, "failure.sock")
		l, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			c, e := l.Accept()
			if e != nil {
				return
			}
			defer c.Close()
			_, _ = bufio.NewReader(c).ReadString('\n')
			_, _ = fmt.Fprintln(c, body)
		}()
		_, err = New(path).StartupStatus(context.Background())
		<-done
		_ = l.Close()
		if err == nil {
			t.Fatal("runtime error accepted as idle")
		}
	}
}
