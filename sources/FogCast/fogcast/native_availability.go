package fogcast

import (
	"context"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

// Catalog entries may execute on the host; FPGA play requires a package entry.
// This check has no target observation or lock dependency.
func (s *Service) nativeCatalogAdmission(ctx context.Context, game catalog.Game) error {
	execution, err := s.resolveExecution(ctx, game)
	if err != nil {
		return err
	}
	if execution == ExecutionHostOnly {
		return nil
	}
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "FPGA launch requires an installed core package", Phase: "admission"}
}
