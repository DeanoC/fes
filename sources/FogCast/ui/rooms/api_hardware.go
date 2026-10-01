package rooms

import (
	"context"
	"errors"
	"strconv"

	"github.com/DeanoC/FogCast/hostclient"
	lua "github.com/yuin/gopher-lua"
)

// HardwareServices is an optional, typed extension to library services.
// Admission and durable selection stay on the host, never in room storage.
type HardwareServices interface {
	Hardware(context.Context) (hostclient.HardwareSnapshot, error)
	SelectCoreEntryExpansion(context.Context, string, string, string, string) (hostclient.CoreEntryExpansion, error)
}

var errNoHardware = errors.New("hardware setups are not available from this host")

func (r *Instance) installHardware() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"setup": func(L *lua.LState) int { r.actions = append(r.actions, Action{Kind: ActionHardwareSetup}); return 0 },
		"open_tapes": func(L *lua.LState) int {
			r.actions = append(r.actions, hardwareSetupAction(L, ActionHardwareTapes))
			return 0
		},
		"import_expansion": func(L *lua.LState) int {
			r.actions = append(r.actions, hardwareSetupAction(L, ActionHardwareImportExpansion))
			return 0
		},
		"read": func(L *lua.LState) int {
			cb := L.CheckFunction(1)
			s, ok := r.opts.Services.(HardwareServices)
			if !ok {
				r.deliver(asyncResult{cb: cb, err: errNoHardware})
				return 0
			}
			if r.hardwareReading {
				r.deliver(asyncResult{cb: cb, err: errors.New("hardware refresh already in progress")})
				return 0
			}
			r.hardwareReading = true
			go func() {
				v, err := s.Hardware(r.ctx)
				r.deliver(asyncResult{cb: cb, hardware: &v, err: err, hardwareRead: true})
			}()
			return 0
		},
		"select_expansion": func(L *lua.LState) int {
			opts, cb := L.CheckTable(1), L.CheckFunction(2)
			s, ok := r.opts.Services.(HardwareServices)
			if !ok {
				r.deliver(asyncResult{cb: cb, err: errNoHardware})
				return 0
			}
			if r.hardwareSaving {
				r.deliver(asyncResult{cb: cb, err: errors.New("a setup is already being saved")})
				return 0
			}
			game, pkg := optString(opts, "game_id"), optString(opts, "package_id")
			if game == "" || pkg == "" {
				L.ArgError(1, "game_id and package_id are required")
			}
			// Require an explicit expected value, including the empty socket.
			if _, ok := opts.RawGetString("expected_expansion_id").(lua.LString); !ok {
				L.ArgError(1, "expected_expansion_id is required")
			}
			if _, ok := opts.RawGetString("expansion_id").(lua.LString); !ok {
				L.ArgError(1, "expansion_id is required (empty removes)")
			}
			expected, id := optString(opts, "expected_expansion_id"), optString(opts, "expansion_id")
			r.hardwareSaving = true
			go func() {
				v, err := s.SelectCoreEntryExpansion(r.ctx, game, pkg, expected, id)
				r.deliver(asyncResult{cb: cb, selection: &v, err: err, hardwareSave: true})
			}()
			return 0
		},
	})
	return t
}

func (r *Instance) hardwareTable(v hostclient.HardwareSnapshot) *lua.LTable {
	L := r.L
	out, machines := L.NewTable(), L.NewTable()
	for _, m := range v.Machines {
		row := L.NewTable()
		for k, s := range map[string]string{"game_id": m.GameID, "title": m.Title, "core_id": m.CoreID, "package_id": m.PackageID, "unavailable_reason": m.UnavailableReason, "draft_expansion_id": m.DraftExpansionID, "media_id": m.MediaID, "media_name": m.MediaName} {
			row.RawSetString(k, lua.LString(s))
		}
		for k, b := range map[string]bool{"package_ready": m.PackageReady, "firmware_ready": m.FirmwareReady, "ready": m.Ready} {
			row.RawSetString(k, lua.LBool(b))
		}
		socket := L.NewTable()
		socket.RawSetString("id", lua.LString(m.Socket.ID))
		socket.RawSetString("label", lua.LString(m.Socket.Label))
		socket.RawSetString("supported", lua.LBool(m.Socket.Supported))
		row.RawSetString("socket", socket)
		choices := L.NewTable()
		for _, c := range m.Choices {
			choice := L.NewTable()
			for k, s := range map[string]string{"expansion_id": c.ExpansionID, "label": c.Label, "description": c.Description, "unavailable_reason": c.UnavailableReason} {
				choice.RawSetString(k, lua.LString(s))
			}
			choice.RawSetString("ready", lua.LBool(c.Ready))
			choice.RawSetString("in_progress", lua.LBool(c.InProgress))
			choices.Append(choice)
		}
		row.RawSetString("choices", choices)
		machines.Append(row)
	}
	out.RawSetString("machines", machines)
	out.RawSetString("session_error", lua.LString(v.SessionError))
	if s := v.Session; s != nil {
		session := L.NewTable()
		r.SetLiveControls(!r.opts.SessionDisplayRequired || hostclient.SessionDisplayCapable(s.CorePackage))
		_, tapeErr := hostclient.LiveMediaBinding(*s)
		session.RawSetString("tape_available", lua.LBool(tapeErr == nil && (!r.opts.SessionDisplayRequired || hostclient.SessionDisplayCapable(s.CorePackage))))
		known := s.CorePackage != nil && s.CorePackage.Composition != nil && s.CorePackage.Composition.PackageID == s.CorePackage.PackageID
		session.RawSetString("hardware_known", lua.LBool(known))
		for k, str := range map[string]string{"id": s.ID, "game_id": s.GameID, "state": s.State, "target": s.Target, "target_id": s.TargetID, "flight_id": s.FlightID} {
			session.RawSetString(k, lua.LString(str))
		}
		if c := s.CorePackage; c != nil {
			session.RawSetString("generation", lua.LString(strconv.FormatUint(c.Generation, 10)))
			session.RawSetString("package_id", lua.LString(c.PackageID))
			id := ""
			if known {
				id = c.Composition.ExpansionID
			}
			session.RawSetString("expansion_id", lua.LString(id))
		}
		out.RawSetString("session", session)
	}
	return out
}

func hardwareSetupAction(L *lua.LState, kind ActionKind) Action {
	opts := L.CheckTable(1)
	game, pkg := optString(opts, "game_id"), optString(opts, "package_id")
	if game == "" || pkg == "" {
		L.ArgError(1, "game_id and package_id are required")
	}
	if kind == ActionHardwareTapes {
		if _, ok := opts.RawGetString("expected_media_id").(lua.LString); !ok {
			L.ArgError(1, "expected_media_id is required")
		}
	}
	return Action{Kind: kind, GameID: game, PackageID: pkg, ExpectedMediaID: optString(opts, "expected_media_id")}
}
