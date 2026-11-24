package tenfoot

import (
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

// These types define the native event-loop/App boundary for the delegated
// physical keyboard route. They do not themselves enable that route. Device
// IDs identify open incarnations, never reused fd numbers.
type nativeKeyboardFault uint8

const (
	nativeFaultDesync nativeKeyboardFault = iota + 1
	nativeFaultHotplug
	nativeFaultCapability
	nativeFaultOverflow
	nativeFaultAuthority
)

type nativeUIDisposition uint8

const (
	nativeUISuppress nativeUIDisposition = iota
	nativeUIAllow
)

type nativeGuestDisposition uint8

const (
	nativeGuestQuarantined nativeGuestDisposition = iota
	nativeGuestOrdinary
	nativeGuestBuffered
)

type nativeEdgeDecision struct {
	UI    nativeUIDisposition
	Guest nativeGuestDisposition
	// UIAction is an App-issued single-use token; zero authorizes no action.
	UIAction uint64
}
type nativeRawDevice struct {
	ID   localhid.DeviceID
	FD   int
	Held []uint16
}
type nativeRawBoundary struct {
	Publisher [16]byte
	Sequence  uint64
	Devices   []nativeRawDevice
}
type nativeRawEdge struct {
	Device   localhid.DeviceID
	Sequence uint64
	Code     uint16
	Down     bool
}

// nativeKeyboardBoundary is called synchronously by the event-loop owner; its
// methods must not wait on network, socket I/O, ioctl or a remote ACK. UI and
// guest decisions are separate: a quarantined guest route never degrades into
// an ordinary raw-keyboard route on a transient health or lease failure.
type nativeKeyboardBoundary interface {
	requestNativeKeyboardQuarantine(nativeKeyboardFault)
	nativeKeyboardSnapshotPermit() (generation uint64, permitted bool)
	publishNativeKeyboardBoundary(generation uint64, boundary nativeRawBoundary) error
	recordNativeKeyboardEdge(generation uint64, edge nativeRawEdge) (nativeEdgeDecision, error)
	recordNativeKeyboardRepeat(generation uint64, id localhid.DeviceID, rawCode uint16) (nativeUIDisposition, uint64, error)
	retireNativeKeyboardDevice(generation uint64, id localhid.DeviceID) error
	// handleNativeKeyboardUIOnly consumes every event; its bool reports quit.
	handleNativeKeyboardUIOnly(id localhid.DeviceID, rawCode uint16, name string, usage uint8, down bool, token uint64, now time.Time) (quit bool)
	discardNativeKeyboardUIAction(token uint64) error
	retireNativeKeyboardUIOnly(id localhid.DeviceID)
	quarantineNativeKeyboardUIOnly(id localhid.DeviceID)
	registerNativeKeyboardUIOnly(id localhid.DeviceID, fd int) error
	nativeKeyboardUIOnlySnapshotAllowed() bool
	publishNativeKeyboardUIOnly(id localhid.DeviceID, held []uint16) error
}
