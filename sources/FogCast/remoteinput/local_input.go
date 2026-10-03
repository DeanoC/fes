package remoteinput

// LocalPlayerDeparture reports that a physical gamepad left its kit-local
// controller slot. Code zero is not a gamepad control. This marker travels
// only on the local input socket; it must not enter a host input stream.
func LocalPlayerDeparture(player uint8) Event {
	return Event{Player: player, Device: DeviceGamepad, Kind: KindButton, Action: ActionRelease}
}

// IsLocalPlayerDeparture distinguishes topology from an ordinary released
// button. Keyboards do not own controller slots.
func IsLocalPlayerDeparture(e Event) bool {
	return e.Player < 2 && e.Device == DeviceGamepad && e.Kind == KindButton &&
		e.Action == ActionRelease && e.Code == 0 && e.Value == 0
}
