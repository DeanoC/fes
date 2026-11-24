package localhid

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

type fragmentReader struct{ r io.Reader }

func (f fragmentReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return f.r.Read(p)
}
func frameJSON(s string) []byte {
	var b bytes.Buffer
	b.Write(replyMarker[:])
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(s)))
	b.WriteString(s)
	return b.Bytes()
}
func TestReplyReaderHandlesFragmentedAndCoalescedPong(t *testing.T) {
	token := strings.Repeat("a", 64)
	raw := `{"version":2,"op":"barrier","request":3,"token":"` + token + `","status":"drained"}`
	pong, err := protocol.EncodeInputFrame(protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePong, Session: 1}, ClientMonoNS: 9, ServerMonoNS: 10})
	if err != nil {
		t.Fatal(err)
	}
	combined := append(append([]byte{}, pong...), frameJSON(raw)...)
	r := fragmentReader{bytes.NewReader(combined)}
	first, err := ReadIncoming(r)
	if err != nil || first.Pong == nil || first.Reply != nil {
		t.Fatalf("pong=%+v %v", first, err)
	}
	second, err := ReadIncoming(r)
	if err != nil || second.Reply == nil || second.Pong != nil {
		t.Fatalf("reply=%+v %v", second, err)
	}
	if err := CorrelateReply(*second.Reply, 3, "barrier", token); err != nil {
		t.Fatal(err)
	}
	if err := CorrelateReply(*second.Reply, 3, "barrier", strings.Repeat("b", 64)); !errors.Is(err, ErrReply) {
		t.Fatalf("wrong token accepted %v", err)
	}
	if err := CorrelateReply(*second.Reply, 4, "barrier", token); !errors.Is(err, ErrReply) {
		t.Fatalf("wrong request accepted %v", err)
	}
}
func TestControlReplyStrictFramingAndJSON(t *testing.T) {
	token := strings.Repeat("a", 64)
	tests := []string{
		`{"version":2,"op":"begin","request":1,"token":"` + token + `","status":"ready","extra":1}`,
		`{"version":2,"version":2,"op":"begin","request":1,"token":"` + token + `","status":"ready"}`,
		`{"version":2,"op":"begin","request":1,"token":"` + token + `","status":"ready"}{}`,
		`{"version":2,"op":"reset","request":1,"token":"` + token + `","status":"ready"}`,
		`{"version":2,"op":"begin","request":0,"token":"` + token + `","status":"ready"}`,
		`{"version":2,"op":"begin","request":1,"token":"bad","status":"ready"}`,
		`{"version":2,"op":"begin","request":1,"token":null,"status":"quarantined"}`,
	}
	for _, input := range tests {
		if _, err := ReadIncoming(bytes.NewReader(frameJSON(input))); !errors.Is(err, ErrReply) {
			t.Fatalf("accepted invalid response %s: %v", input, err)
		}
	}
	oversized := append([]byte{}, replyMarker[:]...)
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], MaxReplyBytes+1)
	oversized = append(oversized, n[:]...)
	if _, err := ReadIncoming(bytes.NewReader(oversized)); !errors.Is(err, ErrReply) {
		t.Fatalf("oversize %v", err)
	}
	if _, err := ReadIncoming(bytes.NewReader([]byte("xxxx"))); !errors.Is(err, ErrReply) {
		t.Fatalf("marker %v", err)
	}
	if _, err := ReadIncoming(bytes.NewReader(frameJSON(`{"version":2}`)[:9])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated %v", err)
	}
}
func TestBeginAndResetCorrelation(t *testing.T) {
	token := strings.Repeat("c", 64)
	begin := Reply{Version: 2, Op: "begin", Request: 1, Token: token, Status: "ready"}
	if err := CorrelateReply(begin, 1, "begin", ""); err != nil {
		t.Fatal(err)
	}
	if err := CorrelateReply(begin, 1, "begin", token); !errors.Is(err, ErrReply) {
		t.Fatalf("begin with old token accepted %v", err)
	}
	reset := Reply{Version: 2, Op: "reset", Request: 2, Token: token, Status: "neutral-confirmed"}
	if err := CorrelateReply(reset, 2, "reset", token); err != nil {
		t.Fatal(err)
	}
	if err := CorrelateReply(reset, 2, "barrier", token); !errors.Is(err, ErrReply) {
		t.Fatalf("wrong op accepted %v", err)
	}
}

func TestReplyTrackerQuarantinesDuplicateStaleAndDisconnect(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, which := range []string{"duplicate", "stale", "wrong-token", "disconnect"} {
		t.Run(which, func(t *testing.T) {
			var tracker ReplyTracker
			trackerAdmit(t, &tracker, token)
			id, err := tracker.Start("reset", token)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tracker.Start("reset", token); !errors.Is(err, ErrPending) {
				t.Fatalf("parallel control %v", err)
			}
			r := Reply{Version: 2, Op: "reset", Request: id, Token: token, Status: "neutral-confirmed"}
			switch which {
			case "duplicate":
				if err := tracker.Accept(r); err != nil {
					t.Fatal(err)
				}
			case "stale":
				r.Request++
			case "wrong-token":
				r.Token = strings.Repeat("b", 64)
			case "disconnect":
				tracker.Disconnect()
			}
			err = tracker.Accept(r)
			if which == "disconnect" {
				if !errors.Is(err, ErrQuarantined) {
					t.Fatalf("disconnect %v", err)
				}
			} else if !errors.Is(err, ErrReply) {
				t.Fatalf("accepted bad ack %v", err)
			}
			if _, err := tracker.Start("begin", ""); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("tracker revived: %v", err)
			}
		})
	}
}

func TestReplyTrackerUnconfirmedResultStaysQuarantined(t *testing.T) {
	token := strings.Repeat("a", 64)
	var tracker ReplyTracker
	trackerAdmit(t, &tracker, token)
	id, err := tracker.Start("reset", token)
	if err != nil {
		t.Fatal(err)
	}
	r := Reply{Version: 2, Op: "reset", Request: id, Token: token, Status: "unconfirmed"}
	if err := tracker.Accept(r); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("unconfirmed reset %v", err)
	}
	if _, err := tracker.Start("begin", ""); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("unconfirmed state revived %v", err)
	}
}

func trackerAdmit(t *testing.T, tracker *ReplyTracker, token string) {
	t.Helper()
	id, err := tracker.Start("begin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "begin", Request: id, Token: token, Status: "ready"}); err != nil {
		t.Fatal(err)
	}
}
func TestReplyTrackerRequiresItsOwnAdmittedSource(t *testing.T) {
	var tracker ReplyTracker
	token := strings.Repeat("a", 64)
	if _, err := tracker.Start("reset", token); !errors.Is(err, ErrReply) {
		t.Fatalf("unadmitted reset %v", err)
	}
	trackerAdmit(t, &tracker, token)
	if _, err := tracker.Start("barrier", strings.Repeat("b", 64)); !errors.Is(err, ErrReply) {
		t.Fatalf("other source barrier %v", err)
	}
	id, err := tracker.Start("reset", token)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "reset", Request: id, Token: token, Status: "neutral-confirmed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Start("barrier", token); !errors.Is(err, ErrReply) {
		t.Fatalf("retired source barrier %v", err)
	}
}

func TestQuarantinedEdgeAllowsOnlySameSourceConfirmedReset(t *testing.T) {
	token := strings.Repeat("a", 64)
	var tracker ReplyTracker
	trackerAdmit(t, &tracker, token)
	id, err := tracker.Start("edge", token)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "edge", Request: id, Token: token, Status: "quarantined"}); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("quarantined edge %v", err)
	}
	for _, op := range []string{"begin", "edge", "barrier"} {
		tok := token
		if op == "begin" {
			tok = ""
		}
		if _, err := tracker.Start(op, tok); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("%s permitted during neutral-only: %v", op, err)
		}
	}
	if _, err := tracker.Start("reset", strings.Repeat("b", 64)); !errors.Is(err, ErrReply) {
		t.Fatalf("wrong-token reset %v", err)
	}
	id, err = tracker.Start("reset", token)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "reset", Request: id, Token: token, Status: "neutral-confirmed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Start("begin", ""); err != nil {
		t.Fatalf("fresh source after neutral refused: %v", err)
	}
}
func TestEdgeAckMustCorrelateAndDrain(t *testing.T) {
	token := strings.Repeat("a", 64)
	var tracker ReplyTracker
	trackerAdmit(t, &tracker, token)
	id, err := tracker.Start("edge", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Start("reset", token); !errors.Is(err, ErrPending) {
		t.Fatalf("reset overtook edge: %v", err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "edge", Request: id, Token: token, Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Start("edge", token); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeReplyRoundTripAndRejectsOversizedFields(t *testing.T) {
	valid := Reply{Version: 2, Op: "begin", Request: 1, Token: strings.Repeat("a", 64), Status: "ready"}
	wire, err := EncodeReply(valid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadIncoming(bytes.NewReader(wire))
	if err != nil || got.Reply == nil || *got.Reply != valid {
		t.Fatalf("reply changed %+v %v", got, err)
	}
	valid.Token = strings.Repeat("a", 1<<20)
	if _, err := EncodeReply(valid); !errors.Is(err, ErrReply) {
		t.Fatalf("oversized token accepted %v", err)
	}
}
