package localhid

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func validBegin() string {
	return `{"version":2,"op":"begin","request":1,"publisher":"` + strings.Repeat("1", 32) + `","agent_instance":"` + strings.Repeat("e", 32) + `","queue_epoch":1,"lease_generation":"` + strings.Repeat("a", 64) + `","runtime_instance":"` + strings.Repeat("d", 32) + `","package_id":"` + strings.Repeat("b", 64) + `","core_generation":7,"snapshot_seq":0,"devices":[{"device":"` + strings.Repeat("2", 32) + `","held":[4,5]}]}`
}
func TestRequestFragmentationPreservesFollowingBinary(t *testing.T) {
	expected := []byte{48, 0, 0, 0, 77, 83, 84, 82}
	stream := append([]byte(validBegin()+"\n"), expected...)
	r := bufio.NewReaderSize(fragmentReader{bytes.NewReader(stream)}, 32)
	got, err := ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Op != "begin" || got.CoreGeneration != 7 || len(got.Devices) != 1 || len(got.Devices[0].Held) != 2 {
		t.Fatalf("bad request %+v", got)
	}
	remain, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(remain, expected) {
		t.Fatalf("following frame %x %v", remain, err)
	}
}
func TestRequestStrictNestedValidation(t *testing.T) {
	valid := validBegin()
	device := strings.Repeat("2", 32)
	invalid := []string{
		strings.Replace(valid, `"version":2`, `"version":2,"version":2`, 1),
		strings.Replace(valid, `"held":[4,5]`, `"held":[4,4]`, 1),
		strings.Replace(valid, `"held":[4,5]`, `"held":"BAU="`, 1),
		strings.Replace(valid, `"held":[4,5]`, `"held":[4,null]`, 1),
		strings.Replace(valid, `"held":[4,5]`, `"held":null`, 1),
		strings.Replace(valid, `"device":"`+device+`"`, `"device":"`+device+`","device":"`+device+`"`, 1),
		strings.Replace(valid, `"held":[4,5]`, `"held":[4,5],"extra":0`, 1),
		strings.Replace(valid, `"queue_epoch":1`, `"queue_epoch":18446744073709551615`, 1),
		strings.Replace(valid, `"publisher":"`+strings.Repeat("1", 32)+`"`, `"publisher":"`+strings.Repeat("A", 32)+`"`, 1),
		strings.Replace(valid, `,"runtime_instance":"`+strings.Repeat("d", 32)+`"`, ``, 1),
		strings.Replace(valid, `"runtime_instance":"`+strings.Repeat("d", 32)+`"`, `"runtime_instance":"`+strings.Repeat("0", 32)+`"`, 1),
		strings.Replace(valid, `"runtime_instance":"`+strings.Repeat("d", 32)+`"`, `"runtime_instance":null`, 1),
		strings.Replace(valid, `"version":2`, `"version":1`, 1),
		valid + `{}`,
	}
	one := `{"device":"` + device + `","held":[4,5]}`
	invalid = append(invalid, strings.Replace(valid, one, one+","+one, 1))
	for _, input := range invalid {
		if _, err := ReadRequest(bufio.NewReader(strings.NewReader(input + "\n"))); !errors.Is(err, ErrRequest) {
			t.Fatalf("accepted invalid request %s: %v", input, err)
		}
	}
}

func TestRequestExplicitEmptyPackageForDirectDevelopment(t *testing.T) {
	request := strings.Replace(validBegin(), `"package_id":"`+strings.Repeat("b", 64)+`"`, `"package_id":""`, 1)
	got, err := ReadRequest(bufio.NewReader(strings.NewReader(request + "\n")))
	if err != nil || got.PackageID != "" {
		t.Fatalf("explicit empty package: %+v %v", got, err)
	}
}
func TestRequestBoundAndMissingNewline(t *testing.T) {
	if _, err := ReadRequest(bufio.NewReader(strings.NewReader(strings.Repeat("x", MaxRequestBytes+1) + "\n"))); !errors.Is(err, ErrRequest) {
		t.Fatalf("oversize %v", err)
	}
	if _, err := ReadRequest(bufio.NewReader(strings.NewReader(validBegin()))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("unterminated %v", err)
	}
}
func TestRequestEdgeAndResetValidation(t *testing.T) {
	token := strings.Repeat("c", 64)
	device := strings.Repeat("2", 32)
	edge := `{"version":2,"op":"edge","request":2,"token":"` + token + `","event_seq":3,"device":"` + device + `","usage":4,"down":false}`
	got, err := ReadRequest(bufio.NewReader(strings.NewReader(edge + "\n")))
	if err != nil || got.Down || got.EventSeq != 3 {
		t.Fatalf("edge %+v %v", got, err)
	}
	for _, bad := range []string{strings.Replace(edge, `"down":false`, `"down":null`, 1), strings.Replace(edge, `"request":2`, `"request":0`, 1), strings.Replace(edge, `"event_seq":3`, `"event_seq":18446744073709551615`, 1)} {
		if _, err := ReadRequest(bufio.NewReader(strings.NewReader(bad + "\n"))); !errors.Is(err, ErrRequest) {
			t.Fatalf("bad edge accepted %v", err)
		}
	}
	reset := `{"version":2,"op":"reset","request":3,"token":"` + token + `","last_event_seq":3}`
	if _, err := ReadRequest(bufio.NewReader(strings.NewReader(reset + "\n"))); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeRequestRoundTripsEmptyAndNonemptySnapshot(t *testing.T) {
	publisher := strings.Repeat("1", 32)
	lease := strings.Repeat("a", 64)
	pkg := strings.Repeat("b", 64)
	id := device(2)
	begin := Request{Version: 2, Op: "begin", Request: 1, Publisher: publisher, AgentInstance: strings.Repeat("e", 32), QueueEpoch: 1, LeaseGeneration: lease, RuntimeInstance: strings.Repeat("d", 32), PackageID: pkg, CoreGeneration: 7, Devices: []DeviceSnapshot{{ID: id, Held: []uint8{4, 5}}, {ID: device(3), Held: nil}}}
	wire, err := EncodeRequest(begin)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(bufio.NewReader(bytes.NewReader(wire)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 2 || len(got.Devices[0].Held) != 2 || got.Devices[1].Held == nil || len(got.Devices[1].Held) != 0 {
		t.Fatalf("snapshot changed %+v", got)
	}
	edge := Request{Version: 2, Op: "edge", Request: 2, Token: strings.Repeat("c", 64), EventSeq: 1, Device: id, Usage: 4, Down: false}
	wire, err = EncodeRequest(edge)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ReadRequest(bufio.NewReader(bytes.NewReader(wire)))
	if err != nil || got.Down || got.Device != id {
		t.Fatalf("edge changed %+v %v", got, err)
	}
	edge.Request = 0
	if _, err := EncodeRequest(edge); !errors.Is(err, ErrRequest) {
		t.Fatalf("invalid outgoing edge %v", err)
	}
}

func TestEncodeRejectsOversizedInputsBeforeEncoding(t *testing.T) {
	r := Request{Version: 2, Op: "begin", Request: 1, Publisher: strings.Repeat("1", 32), AgentInstance: strings.Repeat("e", 32), QueueEpoch: 1, LeaseGeneration: strings.Repeat("a", 64), RuntimeInstance: strings.Repeat("d", 32), PackageID: strings.Repeat("b", 64), CoreGeneration: 1}
	r.Devices = make([]DeviceSnapshot, MaxDevices+1)
	if _, err := EncodeRequest(r); !errors.Is(err, ErrRequest) {
		t.Fatalf("excess devices %v", err)
	}
	r.Devices = []DeviceSnapshot{{ID: device(1), Held: make([]uint8, 257)}}
	if _, err := EncodeRequest(r); !errors.Is(err, ErrRequest) {
		t.Fatalf("excess usages %v", err)
	}
	r.Devices = []DeviceSnapshot{{ID: device(1)}}
	r.Publisher = strings.Repeat("a", 1<<20)
	if _, err := EncodeRequest(r); !errors.Is(err, ErrRequest) {
		t.Fatalf("oversized identity %v", err)
	}
	edge := Request{Version: 2, Op: "edge", Request: 1, Token: strings.Repeat("a", 1<<20), EventSeq: 1, Device: device(1), Usage: 4}
	if _, err := EncodeRequest(edge); !errors.Is(err, ErrRequest) {
		t.Fatalf("oversized token %v", err)
	}
}
