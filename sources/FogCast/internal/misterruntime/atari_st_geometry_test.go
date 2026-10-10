package misterruntime

import (
	"bytes"
	"context"
	"encoding/binary"
	"sort"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func geometrySTResponse(t *testing.T, bound bool) Protocol2Response {
	r := writableSTResponse(t, bound)
	iface := protocol.AtariStFloppyGeometryInterface()
	r.ActivePackage.Descriptor.Interfaces = append(r.ActivePackage.Descriptor.Interfaces, corepackage.Interface{ID: iface.ID, Major: 1, Required: true})
	for _, list := range []*[]Protocol2Interface{&r.Capabilities.ActiveInterfaces, &r.Capabilities.ABIs[0].Interfaces} {
		*list = append(*list, Protocol2Interface{ID: iface.ID, Major: 1})
		sort.Slice(*list, func(i, j int) bool { return (*list)[i].ID < (*list)[j].ID })
	}
	r.Capabilities.MediaUnits[0].MinBytes = uint32(protocol.AtariStFloppyGeometryMinBytes)
	r.Capabilities.MediaUnits[0].MaxBytes = uint32(protocol.AtariStFloppyGeometryMaxBytes)
	return r
}
func TestAtariStGeometryStatusRequiresNegotiatedRequiredExtension(t *testing.T) {
	r := geometrySTResponse(t, true)
	if _, err := decodeProtocol2Response([]byte(responseLine(t, r))); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Protocol2Response){
		func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.Interfaces[len(r.ActivePackage.Descriptor.Interfaces)-1].Required = false
		},
		func(r *Protocol2Response) {
			for n, i := range r.Capabilities.ActiveInterfaces {
				if i.ID == protocol.AtariStFloppyGeometryInterface().ID {
					r.Capabilities.ActiveInterfaces = append(r.Capabilities.ActiveInterfaces[:n], r.Capabilities.ActiveInterfaces[n+1:]...)
					break
				}
			}
		},
		func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].MaxBytes++ },
	} {
		bad := geometrySTResponse(t, true)
		change(&bad)
		if _, err := decodeProtocol2Response([]byte(responseLine(t, bad))); err == nil {
			t.Fatal("unnegotiated widened status admitted")
		}
	}
}
func TestAtariStGeometryLibraryStageRejectsBadBPBBeforeDispatch(t *testing.T) {
	for _, size := range []int{409600, 839680} {
		for _, bad := range []bool{false, true} {
			before, after := geometrySTResponse(t, false), geometrySTResponse(t, true)
			c := &durableMediaControl{computerMediaControl: &computerMediaControl{statuses: []Protocol2Response{before}}, response: after}
			r := NewRuntime(c, "", 0, 0)
			b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: before.ActivePackage.PackageID, Generation: 7}, GameID: "st-desktop", BaseMediaID: after.Capabilities.MediaUnits[0].Persistence.BaseMediaID}
			disk := make([]byte, size)
			binary.LittleEndian.PutUint16(disk[11:13], 512)
			binary.LittleEndian.PutUint16(disk[19:21], uint16(size/512))
			binary.LittleEndian.PutUint16(disk[24:26], 10)
			heads := uint16(1)
			if size == 839680 {
				heads = 2
			}
			binary.LittleEndian.PutUint16(disk[26:28], heads)
			if bad {
				disk[24] ^= 1
			}
			units, err := r.InsertLibraryMedia(context.Background(), context.Background(), int64(size), bytes.NewReader(disk), b)
			if bad {
				if err == nil || c.libraryCalls != 0 {
					t.Fatal("invalid immutable BPB dispatched")
				}
			} else if err != nil || c.libraryCalls != 1 || len(units) != 1 || !bytes.Equal(c.bytes, disk) {
				t.Fatal("valid variable disk not staged exactly", err)
			}
		}
	}
}
