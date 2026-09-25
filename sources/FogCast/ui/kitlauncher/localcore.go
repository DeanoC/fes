package kitlauncher

import "context"

// readLocalCore reports whether the runtime on this kit has a core bound.
// Play input follows this bit. It does not read launcher.json or the host
// session. fogcast-kit installs the probe; without one, play input stays off
// and this package does not open the runtime socket.
func (c *Client) readLocalCore(ctx context.Context) (bool, error) {
	if c == nil || c.localCore == nil {
		return false, nil
	}
	return c.localCore(ctx)
}
