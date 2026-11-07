// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestMenuServiceGapKeepsOriginalPacketDeadline(t *testing.T) {
	c := newPeriodicController()
	h := periodicHost(t, c)
	if data, err := h.pollInput(); err != nil || data != nil || !h.pending.active {
		t.Fatal(data, err, h.pending)
	}
	deadline := h.pending.deadline
	c.advance(600 * time.Millisecond)
	data, err := h.pollInput()
	if err == nil || data != nil || !strings.Contains(err.Error(), "packet timeout") || h.pending.polls != 1 ||
		h.pending.deadline != deadline || h.pending.received || h.pending.done || h.fault == nil {
		t.Fatal("unserviced packet did not retain the captured first-poll timeout", data, err, h.pending)
	}
	accesses := c.accesses
	if _, err = h.pollInput(); err == nil || c.accesses != accesses {
		t.Fatal("failed input retried or touched registers", err)
	}
}

func TestMenuRegularServiceDeliversPressReleasePressDuringLongWork(t *testing.T) {
	c := newPeriodicController()
	h := periodicHost(t, c)
	want := [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}}
	var got [][]byte
	for n := 0; n < 750; n++ {
		if n == 0 || n == 200 || n == 400 {
			c.reports = append(c.reports, want[n/200])
		}
		data, err := h.pollInput()
		if err != nil {
			t.Fatal("regular service failed within original packet bound", n, err)
		}
		if data != nil {
			got = append(got, data)
		}
		c.advance(2 * time.Millisecond)
	}
	if c.elapsed < 1500*time.Millisecond || len(got) != len(want) {
		t.Fatal("long work lost real-driver reports", c.elapsed, got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatal("report order/edges changed", got)
		}
	}
}
