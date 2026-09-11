package zx81keys

import (
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMatrixClearsJAndQuote(t *testing.T) {
	neutral := Matrix(nil)
	if neutral != Neutral {
		t.Fatalf("empty matrix=%#x", neutral)
	}
	j := Matrix(map[remoteinput.Code]bool{Letter('J'): true})
	if j&(1<<(6*5+3)) != 0 {
		t.Fatalf("J should be down: %#x", j)
	}
	quote := Matrix(map[remoteinput.Code]bool{KeyShift: true, Letter('P'): true})
	if quote&(1<<0) != 0 || quote&(1<<(5*5)) != 0 {
		t.Fatalf("SHIFT+P should be down: %#x", quote)
	}
	enter := Matrix(map[remoteinput.Code]bool{KeyEnter: true})
	if enter&(1<<(6*5)) != 0 {
		t.Fatalf("ENTER should be down: %#x", enter)
	}
}

func TestNameRoundTripLettersDigitsAndNamedKeys(t *testing.T) {
	t.Parallel()
	if got, ok := Name(Letter('S')); !ok || got != "s" {
		t.Fatalf("S name = %q ok=%v", got, ok)
	}
	if got, ok := Name(Digit(7)); !ok || got != "7" {
		t.Fatalf("7 name = %q ok=%v", got, ok)
	}
	if got, ok := Name(KeyEnter); !ok || got != "return" {
		t.Fatalf("enter name = %q ok=%v", got, ok)
	}
	if _, ok := Name(200); ok {
		t.Fatal("axis codes are not ZX81 names")
	}
}
