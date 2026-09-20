package pack

import (
	"fmt"
	"sort"

	"github.com/DeanoC/mister-packages/internal/bitfield"
)

func (r *Resolved) Symbols() ([]Symbol, error) {
	var out []Symbol
	add := func(name, kind, from string, value uint64) error {
		if name == "" {
			return nil
		}
		for _, existing := range out {
			if existing.Name == name {
				if existing.Value != value {
					return fmt.Errorf("symbol %s defined as 0x%x at %s and 0x%x at %s",
						name, existing.Value, existing.From, value, from)
				}
				return nil
			}
		}
		out = append(out, Symbol{Name: name, Value: value, Kind: kind, From: from})
		return nil
	}

	for _, window := range r.SoC.Windows {
		from := "window " + window.Name
		if err := add(window.BaseSymbol, "address", from, uint64(window.Base)); err != nil {
			return nil, err
		}
		if err := add(window.SizeSymbol, "size", from, uint64(window.Size)); err != nil {
			return nil, err
		}
	}

	for _, bank := range r.Banks {
		base := uint64(bank.Bank.Base)
		for _, reg := range bank.File.Registers {
			from := bank.Bank.Name + "." + reg.Name
			address := base + uint64(reg.Offset)
			for _, name := range reg.AddressSymbols {
				if err := add(name, "address", from, address); err != nil {
					return nil, err
				}
			}
			for _, write := range reg.Writes {
				if err := add(write.Symbol, "value", from, uint64(write.Value)); err != nil {
					return nil, err
				}
			}
			for _, field := range reg.Fields {
				if field.Bits == "" {
					return nil, fmt.Errorf("%s.%s: missing bits", from, field.Name)
				}
				bits, err := bitfield.Parse(field.Bits)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", from, field.Name, err)
				}
				fieldFrom := from + "." + field.Name
				if err := add(field.MaskSymbol, "mask", fieldFrom, bits.Mask()); err != nil {
					return nil, err
				}
				if err := add(field.ShiftSymbol, "shift", fieldFrom, bits.Shift()); err != nil {
					return nil, err
				}
				for _, value := range field.Values {
					placed := uint64(value.Value)
					if value.Placement == "register" {
						placed, err = bits.Place(uint64(value.Value))
						if err != nil {
							return nil, fmt.Errorf("%s: %w", fieldFrom, err)
						}
					} else if value.Placement != "" && value.Placement != "field" {
						return nil, fmt.Errorf("%s: unknown placement %q", fieldFrom, value.Placement)
					}
					if err := add(value.Symbol, "value", fieldFrom, placed); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Resolved) SymbolMap() (map[string]uint64, error) {
	symbols, err := r.Symbols()
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint64, len(symbols))
	for _, symbol := range symbols {
		out[symbol.Name] = symbol.Value
	}
	return out, nil
}

func DiffOracle(got map[string]uint64, oracle *OracleFile) []string {
	var problems []string
	names := make([]string, 0, len(oracle.Constants))
	for name := range oracle.Constants {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := uint64(oracle.Constants[name])
		have, ok := got[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing %s (want 0x%x)", name, want))
			continue
		}
		if have != want {
			problems = append(problems, fmt.Sprintf("%s got 0x%x want 0x%x", name, have, want))
		}
	}
	return problems
}
