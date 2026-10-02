package pack

import "fmt"

// FabricFile describes an internal, versioned FPGA wire contract. It does not
// declare a host ABI, a GP identity tag, or runtime capability assignments.
type FabricFile struct {
	Schema      string        `yaml:"schema"`
	Kind        string        `yaml:"kind"`
	ID          string        `yaml:"id"`
	Description string        `yaml:"description"`
	Major       uint16        `yaml:"major"`
	Minor       uint16        `yaml:"minor"`
	Constants   []ABIConstant `yaml:"constants"`
}

func LoadFabric(path string) (*FabricFile, error) {
	var fabric FabricFile
	if err := readStrictYAML(path, &fabric); err != nil {
		return nil, err
	}
	if err := checkHeader(fabric.Schema, fabric.Kind, "fabric", path); err != nil {
		return nil, err
	}
	if err := fabric.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &fabric, nil
}

func (f *FabricFile) Validate() error {
	if !validABIIdentifier(f.ID) {
		return fmt.Errorf("invalid fabric identifier %q", f.ID)
	}
	if f.Major == 0 {
		return fmt.Errorf("fabric major must be at least 1")
	}
	if len(f.Constants) == 0 {
		return fmt.Errorf("fabric contract has no constants")
	}
	return validateConstants(f.Constants)
}

func (f *FabricFile) Constant(name string) (uint32, bool) {
	for _, constant := range f.Constants {
		if constant.Name == name {
			return uint32(constant.Value), true
		}
	}
	return 0, false
}
