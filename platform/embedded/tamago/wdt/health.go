// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"errors"
	"fmt"
)

// HealthProbe names one synchronous liveness check run by CompositeCheck before
// each watchdog kick. Probes must remain fast and must never call Supervisor.Stop.
type HealthProbe struct {
	Name  string
	Check func() error
}

func filterProbes(probes []HealthProbe) []HealthProbe {
	copied := make([]HealthProbe, 0, len(probes))
	for _, p := range probes {
		if p.Check != nil {
			copied = append(copied, p)
		}
	}
	return copied
}

func formatProbeError(idx int, p HealthProbe, err error) error {
	name := p.Name
	if name == "" {
		name = fmt.Sprintf("#%d", idx)
	}
	return fmt.Errorf("wdt: health probe %q failed: %w", name, err)
}

// CompositeCheck returns a SupervisorConfig.Check function that runs probes in
// order and stops on the first failing probe, wrapping its error with the probe name.
func CompositeCheck(probes ...HealthProbe) func() error {
	copied := filterProbes(probes)
	return func() error {
		for i, p := range copied {
			if err := p.Check(); err != nil {
				return formatProbeError(i, p, err)
			}
		}
		return nil
	}
}

// CompositeCheckAll returns a SupervisorConfig.Check function that runs every
// probe in order without short-circuiting and joins all failures via
// errors.Join, preserving every wrapped probe error in Supervisor telemetry.
func CompositeCheckAll(probes ...HealthProbe) func() error {
	copied := filterProbes(probes)
	return func() error {
		var errs []error
		for i, p := range copied {
			if err := p.Check(); err != nil {
				errs = append(errs, formatProbeError(i, p, err))
			}
		}
		return errors.Join(errs...)
	}
}
