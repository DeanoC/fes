package input

import (
	"errors"
	"reflect"
	"testing"
)

type reportQueue struct {
	reports [][]byte
	err     error
}

func (r *reportQueue) PollKeyboard() ([]byte, error) {
	if len(r.reports) == 0 {
		return nil, r.err
	}
	v := r.reports[0]
	r.reports = r.reports[1:]
	return v, nil
}
func report(modifier, usage byte) []byte { return []byte{modifier, 0, usage, 0, 0, 0, 0, 0} }

func TestUSBRealEdgesAndMapping(t *testing.T) {
	source := &reportQueue{reports: [][]byte{report(0, 79), report(0, 79), report(0, 0), report(0, 79), report(0, 0), report(2, 15), report(0, 0)}}
	keyboard, err := NewUSBKeyboard(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := keyboard.Poll()
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{{Key: "right"}, {Key: "right"}, {Key: "l", Text: "L"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events=%+v want=%+v", got, want)
	}
}
func TestUSBPollBoundAndError(t *testing.T) {
	source := &reportQueue{}
	for i := 0; i < 9; i++ {
		source.reports = append(source.reports, report(0, 0))
	}
	keyboard, _ := NewUSBKeyboard(source)
	if _, err := keyboard.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(source.reports) != 1 {
		t.Fatalf("drained unbounded reports: %d", len(source.reports))
	}
	sentinel := errors.New("disconnected")
	source.err = sentinel
	if _, err := keyboard.Poll(); !errors.Is(err, sentinel) {
		t.Fatalf("error=%v", err)
	}
	if _, err := NewUSBKeyboard(nil); err == nil {
		t.Fatal("nil source accepted")
	}
}
func TestUSBShiftTabAndRollover(t *testing.T) {
	source := &reportQueue{reports: [][]byte{report(2, 43)}}
	keyboard, _ := NewUSBKeyboard(source)
	got, err := keyboard.Poll()
	if err != nil || len(got) != 1 || got[0].Key != "shift-tab" {
		t.Fatalf("events=%v error=%v", got, err)
	}
	source.reports = [][]byte{report(0, 1)}
	if events, err := keyboard.Poll(); err != nil || len(events) != 0 {
		t.Fatalf("rollover produced actions or stopped input: %v %v", events, err)
	}
	source.reports = [][]byte{report(2, 43)}
	got, err = keyboard.Poll()
	if err != nil || len(got) != 0 {
		t.Fatalf("rollover lost prior held state: %v %v", got, err)
	}
}
