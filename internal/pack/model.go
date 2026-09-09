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
	Repository string   `yaml:"repository"`
	Commit     string   `yaml:"commit"`
	Files      []string `yaml:"files"`
}

type SystemFile struct {
	Schema       string          `yaml:"schema"`
	Kind         string          `yaml:"kind"`
	ID           string          `yaml:"id"`
	Description  string          `yaml:"description"`
	ExpectedCore string          `yaml:"expected_core"`
	RBF          RBFRef          `yaml:"rbf"`
	Media        []MediaRule     `yaml:"media"`
	Settings     []SettingRule   `yaml:"settings"`
	Core         CoreRecipe      `yaml:"core"`
	Input        InputRecipe     `yaml:"input"`
	CoreSource   *CoreSourceFile `yaml:"-"`
}

type RBFRef struct {
	Role     string `yaml:"role"`
	Artifact string `yaml:"artifact"`
	Source   string `yaml:"source"`
}

type CoreSourceFile struct {
	Schema      string   `yaml:"schema"`
	Kind        string   `yaml:"kind"`
	ID          string   `yaml:"id"`
	Description string   `yaml:"description"`
	Repository  string   `yaml:"repository"`
	Commit      string   `yaml:"commit"`
	RBFPath     string   `yaml:"rbf_path"`
	RBFSHA256   string   `yaml:"rbf_sha256"`
	RBFSize     uint64   `yaml:"rbf_size"`
	Project     string   `yaml:"project"`
	Submodules  []GitPin `yaml:"submodules"`
}

type GitPin struct {
	Path       string `yaml:"path"`
	Repository string `yaml:"repository"`
	Commit     string `yaml:"commit"`
}

type CoreSourceOracleFile struct {
	Source OracleSource     `yaml:"source"`
	Pin    CoreSourceOracle `yaml:"pin"`
}

type CoreSourceOracle struct {
	Repository string `yaml:"repository"`
	Commit     string `yaml:"commit"`
	RBFPath    string `yaml:"rbf_path"`
	RBFSHA256  string `yaml:"rbf_sha256"`
	RBFSize    uint64 `yaml:"rbf_size"`
	Project    string `yaml:"project"`
}

type MediaRule struct {
	Role        string        `yaml:"role"`
	Index       int           `yaml:"index"`
	Required    bool          `yaml:"required"`
	Extensions  []string      `yaml:"extensions"`
	MaximumSize hexnum.Uint64 `yaml:"maximum_size"`
	Transform   string        `yaml:"transform"`
}

type SettingRule struct {
	Name          string   `yaml:"name"`
	AllowedValues []string `yaml:"allowed_values"`
}

type CoreRecipe struct {
	ResetAssertWord   hexnum.Uint64 `yaml:"reset_assert_word"`
	InitialStatusWord hexnum.Uint64 `yaml:"initial_status_word"`
	ResetReleaseWord  hexnum.Uint64 `yaml:"reset_release_word"`
	FileWire          string        `yaml:"file_wire"`
}

type InputRecipe struct {
	PlayerCount   int           `yaml:"player_count"`
	PlayerCommand hexnum.Uint64 `yaml:"player_command"`
	Up            hexnum.Uint64 `yaml:"up"`
	Down          hexnum.Uint64 `yaml:"down"`
	Left          hexnum.Uint64 `yaml:"left"`
	Right         hexnum.Uint64 `yaml:"right"`
	A             hexnum.Uint64 `yaml:"a"`
	B             hexnum.Uint64 `yaml:"b"`
	C             hexnum.Uint64 `yaml:"c"`
	Start         hexnum.Uint64 `yaml:"start"`
	X             hexnum.Uint64 `yaml:"x"`
	Y             hexnum.Uint64 `yaml:"y"`
	L             hexnum.Uint64 `yaml:"l"`
	R             hexnum.Uint64 `yaml:"r"`
	Select        hexnum.Uint64 `yaml:"select"`
}

type SystemOracleFile struct {
	Source  OracleSource `yaml:"source"`
	Profile SystemOracle `yaml:"profile"`
}

type SystemOracle struct {
	System       string      `yaml:"system"`
	ExpectedCore string      `yaml:"expected_core"`
	RBF          RBFRef      `yaml:"rbf"`
	Media        []MediaRule `yaml:"media"`
	Core         CoreRecipe  `yaml:"core"`
	Input        InputRecipe `yaml:"input"`
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

// ABIFile declares one versioned FPGA software contract. It describes wire
// constants and advertised interfaces; it never contains MMIO or reset recipes.
type ABIFile struct {
	Schema      string         `yaml:"schema"`
	Kind        string         `yaml:"kind"`
	ID          string         `yaml:"id"`
	Description string         `yaml:"description"`
	Major       uint16         `yaml:"major"`
	Minor       uint16         `yaml:"minor"`
	Tag         uint16         `yaml:"tag"`
	Constants   []ABIConstant  `yaml:"constants"`
	Interfaces  []ABIInterface `yaml:"interfaces"`
}

type ABIConstant struct {
	Name  string        `yaml:"name"`
	Value hexnum.Uint64 `yaml:"value"`
}

type ABIInterface struct {
	ID            string `yaml:"id"`
	Major         uint16 `yaml:"major"`
	Minor         uint16 `yaml:"minor"`
	CapabilityBit uint8  `yaml:"capability_bit"`
}

// ProgrammingProfilesFile is the platform registry of approved ABI-major
// pairings. Profiles are declarative names; runtime owns the actual lifecycle.
type ProgrammingProfilesFile struct {
	Schema      string               `yaml:"schema"`
	Kind        string               `yaml:"kind"`
	ID          string               `yaml:"id"`
	Description string               `yaml:"description"`
	Platform    string               `yaml:"platform"`
	Device      string               `yaml:"device"`
	Profiles    []ProgrammingProfile `yaml:"profiles"`
}

type ProgrammingProfile struct {
	ID             string     `yaml:"id"`
	DiagnosticOnly bool       `yaml:"diagnostic_only"`
	ABIs           []ABIMajor `yaml:"abis"`
}

type ABIMajor struct {
	ID    string `yaml:"id"`
	Major uint16 `yaml:"major"`
}
