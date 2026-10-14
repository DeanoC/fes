package main

import (
	"os"
	"strings"

	"github.com/DeanoC/FogCast/internal/kitlease"
)

func maintenanceLeaseOptions(dependencies runDependencies) ([]kitlease.Option, error) {
	if dependencies.maintenanceFile == "" {
		return nil, nil
	}
	bootID, err := os.ReadFile(dependencies.maintenanceBootIDFile)
	if err != nil {
		return nil, err
	}
	return []kitlease.Option{kitlease.WithMaintenanceFile(dependencies.maintenanceFile, dependencies.maintenanceLock, strings.TrimSpace(string(bootID)))}, nil
}
