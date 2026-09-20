package pack

import (
	"fmt"
	"io"
	"strings"

	"github.com/DeanoC/mister-packages/internal/hexnum"
)

func (r *Resolved) Report(w io.Writer) error {
	fmt.Fprintf(w, "platform %s\n", r.Platform.ID)
	fmt.Fprintf(w, "board    %s  vendor=%s  part=%s\n", r.Board.ID, r.Board.Vendor, r.Board.FPGADevice)
	fmt.Fprintf(w, "soc      %s\n", r.SoC.ID)
	fmt.Fprintf(w, "cpu      %s  triple=%s  %d-bit  %d cores\n",
		r.CPU.ID, r.CPU.Triple, r.CPU.Width, r.CPU.CoreCount)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "windows:")
	for _, window := range r.SoC.Windows {
		fmt.Fprintf(w, "  %-20s base=%-12s size=%s\n",
			window.Name, hexnum.Format(uint64(window.Base)), hexnum.Format(uint64(window.Size)))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "banks:")
	for _, bank := range r.Banks {
		fmt.Fprintf(w, "  %-16s base=%s  %s\n",
			bank.Bank.Name, hexnum.Format(uint64(bank.Bank.Base)), bank.File.ID)
		for _, reg := range bank.File.Registers {
			addr := uint64(bank.Bank.Base) + uint64(reg.Offset)
			syms := strings.Join(reg.AddressSymbols, ", ")
			if syms == "" {
				syms = "-"
			}
			fmt.Fprintf(w, "    %-16s +0x%x = %s  %s\n",
				reg.Name, uint64(reg.Offset), hexnum.Format(addr), syms)
		}
	}
	symbols, err := r.Symbols()
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "symbols (%d):\n", len(symbols))
	for _, symbol := range symbols {
		fmt.Fprintf(w, "  %-42s %s  (%s)\n", symbol.Name, hexnum.Format(symbol.Value), symbol.Kind)
	}
	return nil
}
