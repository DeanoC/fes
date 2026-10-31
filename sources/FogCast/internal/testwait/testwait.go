// Package testwait holds the shared ceiling for tests that poll for an
// asynchronous change. The race-enabled CI host job runs several times slower
// than a dev machine, so a 2 s ceiling that never fails locally times out there
// now and then (#297). Tests keep polling for the condition, so the ceiling
// only matters when something is actually wrong.
package testwait

import "time"

// Timeout is how long a test waits for an expected change before it fails.
const Timeout = 10 * time.Second
