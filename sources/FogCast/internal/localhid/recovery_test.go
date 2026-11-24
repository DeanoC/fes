package localhid

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRecoveryRequestAndReplyBindCompleteIdentity(t *testing.T) {
	old := strings.Repeat("a", 32)
	request := Request{Version: 2, Op: "recover", Request: 1, AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"}
	wire, err := EncodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadRequest(bufio.NewReader(bytes.NewReader(wire)))
	if err != nil || decoded.AgentInstance != old || decoded.TargetID != "kit-b" || decoded.BootID != "boot-1" {
		t.Fatalf("request %+v %v", decoded, err)
	}
	for _, bad := range []string{
		strings.Replace(string(wire), `"boot_id":"boot-1"`, `"boot_id":null`, 1),
		strings.Replace(string(wire), `"agent_instance":"`+old+`"`, `"agent_instance":"`+strings.Repeat("0", 32)+`"`, 1),
		strings.Replace(string(wire), `"target_id":"kit-b"`, `"target_id":"kit-b","target_id":"kit-b"`, 1),
	} {
		if _, err := ReadRequest(bufio.NewReader(strings.NewReader(bad))); !errors.Is(err, ErrRequest) {
			t.Fatalf("accepted malformed recovery %v", err)
		}
	}
	reply := Reply{Version: 2, Op: "recover", Request: 1, AgentInstance: old, TargetID: "kit-b", BootID: "boot-1", Status: "reader-drained"}
	framed, err := EncodeReply(reply)
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := ReadIncoming(bytes.NewReader(framed))
	if err != nil || incoming.Reply == nil {
		t.Fatalf("reply %v", err)
	}
	if err := CorrelateRecoveryReply(*incoming.Reply, request); err != nil {
		t.Fatal(err)
	}
	wrong := reply
	wrong.TargetID = "kit-a"
	if CorrelateRecoveryReply(wrong, request) == nil {
		t.Fatal("wrong target accepted")
	}
	wrong = reply
	wrong.BootID = "boot-2"
	if CorrelateRecoveryReply(wrong, request) == nil {
		t.Fatal("wrong boot accepted")
	}
	wrong = reply
	wrong.AgentInstance = strings.Repeat("b", 32)
	if CorrelateRecoveryReply(wrong, request) == nil {
		t.Fatal("changed agent claimed same-instance drain")
	}
	wrong.Status = "startup-retired"
	if err := CorrelateRecoveryReply(wrong, request); err != nil {
		t.Fatal(err)
	}
	wrong.AgentInstance = old
	if CorrelateRecoveryReply(wrong, request) == nil {
		t.Fatal("same agent claimed restart retirement")
	}
}
func TestRecoveryTrackerDoesNotGrantUnavailable(t *testing.T) {
	old := strings.Repeat("a", 32)
	var tracker ReplyTracker
	id, err := tracker.StartRecovery(old, "kit-b", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(Reply{Version: 2, Op: "recover", Request: id, AgentInstance: old, TargetID: "kit-b", BootID: "boot-1", Status: "unavailable"}); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("unavailable accepted: %v", err)
	}
	if _, err := tracker.Start("begin", ""); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("quarantine lost: %v", err)
	}
}
