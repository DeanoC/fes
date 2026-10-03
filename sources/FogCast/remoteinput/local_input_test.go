package remoteinput

import (
	"bytes"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestLocalPlayerDepartureRoundTripsExistingFrame(t *testing.T) {
	for player := uint8(0); player < 2; player++ {
		event := LocalPlayerDeparture(player)
		frame := protocol.InputFrame{
			Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1},
			Player: event.Player, Device: uint8(event.Device), Kind: uint8(event.Kind),
			Action: uint8(event.Action), Code: uint16(event.Code), Value: event.Value,
		}
		wire, err := protocol.EncodeInputFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := protocol.DecodeInputFrame(bytes.NewReader(wire), 4096)
		if err != nil {
			t.Fatal(err)
		}
		got := Event{Player: decoded.Player, Device: Device(decoded.Device), Kind: Kind(decoded.Kind), Action: Action(decoded.Action), Code: Code(decoded.Code), Value: decoded.Value}
		if got != event || !IsLocalPlayerDeparture(got) {
			t.Fatalf("departure round trip: %+v", got)
		}
	}
}

func TestLocalPlayerDepartureRequiresExactMarker(t *testing.T) {
	marker := LocalPlayerDeparture(0)
	otherEvents := []Event{
		{Player: 2, Device: marker.Device, Kind: marker.Kind, Action: marker.Action},
		{Device: DeviceKeyboard, Kind: marker.Kind, Action: marker.Action},
		{Device: marker.Device, Kind: KindAxis, Action: marker.Action},
		{Device: marker.Device, Kind: marker.Kind, Action: ActionPress},
		{Device: marker.Device, Kind: marker.Kind, Action: marker.Action, Code: ButtonA},
		{Device: marker.Device, Kind: marker.Kind, Action: marker.Action, Value: 1},
	}
	for _, event := range otherEvents {
		if IsLocalPlayerDeparture(event) {
			t.Fatalf("ordinary event mistaken for departure: %+v", event)
		}
	}
}
