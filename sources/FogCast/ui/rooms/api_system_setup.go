package rooms

import lua "github.com/yuin/gopher-lua"

func (r *Instance) openSystemExpansions(L *lua.LState) int {
	r.actions = append(r.actions, hardwareSetupAction(L, ActionHardwareExpansions))
	return 0
}
