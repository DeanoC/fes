package protocol

// ControllerPorts reports only exact negotiated controller contracts.
func ControllerPorts(core *CorePackageStatus) (ports, keypad bool) {
	if core == nil {
		return
	}
	for _, contract := range core.ActiveInterfaces {
		if contract.Major != 1 || contract.Minor != 0 {
			continue
		}
		switch contract.ID {
		case "fes.gamepad.ports":
			ports = true
		case "fes.keypad.ports":
			keypad = true
		}
	}
	return ports, ports && keypad
}
