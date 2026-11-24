package localhid

import (
	"strings"
	"testing"
)

func TestMatrixWitnessValidation(t *testing.T) {
	valid := MatrixWitness{Sink: [16]byte{1}, Sequence: 1, Source: 2, RuntimeInstance: strings.Repeat("a", 32), PackageID: "", Generation: 1}
	if !valid.Valid() {
		t.Fatal("direct-development witness rejected")
	}
	bad := []MatrixWitness{valid, valid, valid, valid, valid, valid}
	bad[0].Sink = [16]byte{}
	bad[1].Sequence = 0
	bad[2].Sequence = ^uint64(0)
	bad[3].Source = 3
	bad[4].RuntimeInstance = strings.Repeat("0", 32)
	bad[5].Generation = 0
	for i, witness := range bad {
		if witness.Valid() {
			t.Fatalf("invalid witness %d accepted", i)
		}
	}
}
