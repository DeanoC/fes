package agent_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type libraryPartsRuntime struct {
	packageRuntime
	id           string
	libraryCalls int
}

func (r *libraryPartsRuntime) LoadLibraryPartsCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	r.libraryCalls++
	r.id = id
	return r.packageRuntime.LoadCoreOwned(admission, observation, owner, size, body)
}
func TestLibraryPartsCoordinatorUsesExplicitLibraryOperationAndClonesReceipt(t *testing.T) {
	id := strings.Repeat("a", 64)
	receipt := expansion.PartsComposition{PackageID: id, Layout: expansion.ColecoVideoLayout, Parts: []expansion.PartSelection{{Role: "video", PartID: strings.Repeat("b", 64)}}, ShellSHA256: strings.Repeat("c", 64), PayloadSHA256: strings.Repeat("d", 64), PayloadSize: 40408}
	receipt.ID, _ = expansion.PartsCompositionID(receipt.PackageID, receipt.Layout, receipt.Parts, receipt.PayloadSHA256)
	runtime := &libraryPartsRuntime{packageRuntime: packageRuntime{attempted: true, activation: misterruntime.CoreActivation{PackageID: id, Generation: 7, PersistenceMode: "volatile", Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.application", Major: 1}}, PartsComposition: &receipt}}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	if _, err := coordinator.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), ""); err == nil || runtime.libraryCalls != 0 || runtime.calls != 0 {
		t.Fatal("missing library identity reached a load operation")
	}
	status, err := coordinator.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), id)
	if err != nil || runtime.libraryCalls != 1 || runtime.id != id || !reflect.DeepEqual(status.CorePackage.PartsComposition, &receipt) || status.CorePackage.PersistenceMode != "volatile" {
		t.Fatalf("status=%#v error=%v calls=%d id=%s", status, err, runtime.libraryCalls, runtime.id)
	}
	status.CorePackage.PartsComposition.Parts[0].PartID = strings.Repeat("e", 64)
	if coordinator.Status().CorePackage.PartsComposition.Parts[0].PartID != strings.Repeat("b", 64) {
		t.Fatal("status aliases parts selection")
	}
}
