package hostapi

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestPublicSessionRuntimeIdentityIsCoherentAndCopied(t *testing.T) {
	id := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("a", 32), PackageID: strings.Repeat("b", 64), Generation: 7}
	status := protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{PackageID: id.PackageID, Generation: id.Generation}, RuntimeInput: id, InputAgentInstance: strings.Repeat("c", 32)}
	got := publicSession(status, nil)
	if got.RuntimeInput == nil || *got.RuntimeInput != *id || got.InputAgentInstance != status.InputAgentInstance {
		t.Fatal("valid identity was not projected")
	}
	status.InputAgentInstance = "invalid"
	if publicSession(status, nil).InputAgentInstance != "" {
		t.Fatal("invalid agent incarnation projected")
	}
	got.RuntimeInput.Generation++
	if id.Generation != 7 {
		t.Fatal("session projection aliased target identity")
	}
	status.CorePackage.Generation++
	if publicSession(status, nil).RuntimeInput != nil {
		t.Fatal("mismatched package generation projected")
	}
	status = protocol.Status{State: protocol.StateActive, Development: true, RuntimeInput: &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}}
	if got = publicSession(status, nil); got.RuntimeInput == nil || got.RuntimeInput.PackageID != "" {
		t.Fatal("explicit direct-development binding dropped")
	}
	status.Development = false
	if publicSession(status, nil).RuntimeInput != nil {
		t.Fatal("untyped empty-package identity projected")
	}
}
