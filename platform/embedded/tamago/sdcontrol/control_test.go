// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"errors"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"sync"
	"testing"
)

type callFunc func([7]uint64) ([4]uint64, error)

func (f callFunc) Invoke(a [7]uint64) ([4]uint64, error) { return f(a) }
func wireReply(flags uint32, generation uint64) [4]uint64 {
	return [4]uint64{0, uint64(g.FesEmbeddedSdTag)<<32 | uint64(flags), 17, generation}
}
func TestReceiptMismatchRetainsLastOwnership(t *testing.T) {
	tests := map[string]func(*[4]uint64){
		"tag":               func(r *[4]uint64) { r[1] ^= 1 << 63 },
		"owner":             func(r *[4]uint64) { r[2]++ },
		"generation-stale":  func(r *[4]uint64) { r[3]-- },
		"generation-skip":   func(r *[4]uint64) { r[3]++ },
		"reserved":          func(r *[4]uint64) { r[1] |= 1 << 31 },
		"contained-active":  func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedSdContained) },
		"prepared-too-soon": func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedSdPrepared) },
		"error-state":       func(r *[4]uint64) { r[0] = 4 },
		"unknown-status":    func(r *[4]uint64) { *r = [4]uint64{7, uint64(g.FesEmbeddedSdTag) << 32} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c, err := NewClient(callFunc(func(a [7]uint64) ([4]uint64, error) {
				calls++
				if calls == 1 {
					if a != [7]uint64{uint64(g.FesEmbeddedSdCall), 0, 0, uint64(g.FesEmbeddedSdCookie)} {
						t.Fatalf("query %#v", a)
					}
					return wireReply(0x39, 1), nil
				}
				if a != [7]uint64{uint64(g.FesEmbeddedSdCall), 1, 17, uint64(g.FesEmbeddedSdCookie), 1} {
					t.Fatalf("resume %#v", a)
				}
				r := wireReply(0x33, 2)
				mutate(&r)
				return r, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Resume(); !errors.Is(err, ErrRetired) {
				t.Fatal(err)
			}
			r, valid := c.LastReceipt()
			if valid || r != (Receipt{Owner: 17, Generation: 1}) {
				t.Fatalf("%+v %v", r, valid)
			}
			if !errors.Is(c.Stop(), ErrRetired) || !errors.Is(c.Resume(), ErrRetired) || calls != 2 {
				t.Fatalf("retired calls=%d", calls)
			}
		})
	}
}
func TestQueryRejectsUnreadyOrActiveService(t *testing.T) {
	for _, r := range [][4]uint64{
		{5, uint64(g.FesEmbeddedSdTag) << 32}, wireReply(0x33, 2),
		{5, uint64(g.FesEmbeddedSdTag) << 32, 17, 1}, wireReply(0x3d, 1),
		{0, uint64(g.FesEmbeddedSdTag)<<32 | 0x39, 0, 1},
	} {
		c, err := NewClient(callFunc(func([7]uint64) ([4]uint64, error) { return r, nil }))
		if err == nil || c != nil {
			t.Fatalf("adopted %#v", r)
		}
	}
}
func TestConcurrentResumeIssuesOneGrant(t *testing.T) {
	calls := 0
	c, err := NewClient(callFunc(func(a [7]uint64) ([4]uint64, error) {
		calls++
		if a[1] == 0 {
			return wireReply(0x39, 1), nil
		}
		return wireReply(0x33, 2), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			err := c.Resume()
			if err != nil && !errors.Is(err, ErrTransition) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls != 2 {
		t.Fatal(calls)
	}
	if !errors.Is(c.Stop(), ErrRetired) {
		t.Fatal("incorrect Stop receipt accepted")
	}
}
