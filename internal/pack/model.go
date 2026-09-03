package pack

import "github.com/DeanoC/mister-packages/internal/hexnum"

const SchemaV1 = "mister-packages.v1"

type PlatformFile struct {
	Schema      string `yaml:"schema"`
	Kind        string `yaml:"kind"`
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Board       string `yaml:"board"`
	SoC         string `yaml:"soc"`
	CPU         string `yaml:"cpu"`
}

type BoardFile struct {
	Schema      string  `yaml:"schema"`
	Kind        string  `yaml:"kind"`
	ID          string  `yaml:"id"`
	Vendor      string  `yaml:"vendor"`
	FPGAFamily  string  `yaml:"fpga_family"`
	FPGADevice  string  `yaml:"fpga_device"`
	Description string  `yaml:"description"`
	Clocks      []Clock `yaml:"clocks"`
	Pins        []Pin   `yaml:"pins"`
}

type Clock struct {
	Name        string        `yaml:"name"`
	Pin         string        `yaml:"pin"`
	Standard    string        `yaml:"standard"`
	FrequencyHz hexnum.Uint64 `yaml:"frequency_hz"`
}

type Pin struct {
	Name      string `yaml:"name"`
	Pin       string `yaml:"pin"`
	Standard  string `yaml:"standard"`
	Direction string `yaml:"direction"`
}

type CPUFile struct {
	Schema         string `yaml:"schema"`
	Kind           string `yaml:"kind"`
	ID             string `yaml:"id"`
	Triple         string `yaml:"triple"`
	Arch           string `yaml:"arch"`
	Width          int    `yaml:"width"`
	CoreCount      int    `yaml:"core_count"`
	MaxAtomicWidth int    `yaml:"max_atomic_width"`
	Description    string `yaml:"description"`
}

type SoCFile struct {
	Schema      string   `yaml:"schema"`
	Kind        string   `yaml:"kind"`
	ID          string   `yaml:"id"`
	Vendor      string   `yaml:"vendor"`
	Family      string   `yaml:"family"`
	Description string   `yaml:"description"`
	Windows     []Window `yaml:"windows"`
	Buses       []Bus    `yaml:"buses"`
	Banks       []Bank   `yaml:"banks"`
}

type Window struct {
	Name       string        `yaml:"name"`
	Base       hexnum.Uint64 `yaml:"base"`
	Size       hexnum.Uint64 `yaml:"size"`
	DataWidth  int           `yaml:"data_width"`
	BaseSymbol string        `yaml:"base_symbol"`
	SizeSymbol string        `yaml:"size_symbol"`
}

type Bus struct {
	Name         string        `yaml:"name"`
	Protocol     string        `yaml:"protocol"`
	Supplier     bool          `yaml:"supplier"`
	Base         hexnum.Uint64 `yaml:"base"`
	DataWidth    int           `yaml:"data_width"`
	AddressWidth int           `yaml:"address_width"`
	Fixed        bool          `yaml:"fixed"`
}

type Bank struct {
	Name       string        `yaml:"name"`
	Base       hexnum.Uint64 `yaml:"base"`
	WindowSize hexnum.Uint64 `yaml:"window_size"`
	Registers  string        `yaml:"registers"`
}

type RegisterBankFile struct {
	Schema      string     `yaml:"schema"`
	Kind        string     `yaml:"kind"`
	ID          string     `yaml:"id"`
	Description string     `yaml:"description"`
	Registers   []Register `yaml:"registers"`
}

type Register struct {
	Name           string        `yaml:"name"`
	Offset         hexnum.Uint64 `yaml:"offset"`
	Access         string        `yaml:"access"`
	Description    string        `yaml:"description"`
	AddressSymbols []string      `yaml:"address_symbols"`
	Writes         []NamedValue  `yaml:"writes"`
	Fields         []Field       `yaml:"fields"`
}

type Field struct {
	Name        string       `yaml:"name"`
	Bits        string       `yaml:"bits"`
	Access      string       `yaml:"access"`
	Description string       `yaml:"description"`
	MaskSymbol  string       `yaml:"mask_symbol"`
	ShiftSymbol string       `yaml:"shift_symbol"`
	Values      []NamedValue `yaml:"values"`
}

type NamedValue struct {
	Symbol    string        `yaml:"symbol"`
	Value     hexnum.Uint64 `yaml:"value"`
	Placement string        `yaml:"placement"`
}

type OracleFile struct {
	Source    OracleSource             `yaml:"source"`
	Constants map[string]hexnum.Uint64 `yaml:"constants"`
}

type OracleSource struct {
	Repository string `yaml:"repository"`
	Commit     string `yaml:"commit"`
}

// Resolved is a loaded platform with register banks attached.
type Resolved struct {
	Root     string
	Platform PlatformFile
	Board    BoardFile
	SoC      SoCFile
	CPU      CPUFile
	Banks    []ResolvedBank
}

type ResolvedBank struct {
	Bank Bank
	File RegisterBankFile
}

type Symbol struct {
	Name  string
	Value uint64
	Kind  string
	From  string
}
