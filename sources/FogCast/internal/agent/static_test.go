package agent_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type staticPackageRuntime struct{ packageRuntime }

func (r *staticPackageRuntime) LoadComposedCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	return r.LoadCoreOwned(admission, observation, owner, size, body)
}
func TestStaticCoordinatorRetainsAndOwnsReceipt(t *testing.T) {
	receipt := expansion.StaticComposition{ID: strings.Repeat("a", 64), PackageID: strings.Repeat("b", 64), HardwareID: expansion.RAM16KPersonaID}
	runtime := &staticPackageRuntime{packageRuntime: packageRuntime{attempted: true, activation: misterruntime.CoreActivation{PackageID: receipt.PackageID, Generation: 7, PersistenceMode: "volatile", StaticComposition: &receipt}}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	status, err := coordinator.LoadComposedCore(context.Background(), 5, strings.NewReader("parts"), receipt.PackageID)
	if err != nil || status.CorePackage == nil || status.CorePackage.StaticComposition == nil || *status.CorePackage.StaticComposition != receipt {
		t.Fatal("coordinator lost static identity", err)
	}
	status.CorePackage.StaticComposition.HardwareID = expansion.EmptyPersonaID
	if coordinator.Status().CorePackage.StaticComposition.HardwareID != expansion.RAM16KPersonaID {
		t.Fatal("caller mutation changed coordinator's static receipt")
	}
}
