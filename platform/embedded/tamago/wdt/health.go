// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import "fmt"

// HealthProbe names one synchronous liveness check run by CompositeCheck before
// each watchdog kick. Probes must remain fast and must never call Supervisor.Stop.
type HealthProbe struct {
	Name  string
	Check func() error
}

// CompositeCheck returns a SupervisorConfig.Check function that runs probes in
// order and stops on the first failing probe, wrapping its error with the probe name.
func CompositeCheck(probes ...HealthProbe) func() error {
	copied := make([]HealthProbe, 0, len(probes))
	for _, p := range probes {
		if p.Check == nil {
			continue
		}
		copied = append(copied, p)
	}
	return func() error {
		for i, p := range copied {
			if err := p.Check(); err != nil {
				name := p.Name
				if name == "" {
					name = fmt.Sprintf("#%d", i)
				}
				return fmt.Errorf("wdt: health probe %q failed: %w", name, err)
			}
		}
		return nil
	}
}
