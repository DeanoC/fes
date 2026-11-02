// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

const HPSFirstDiagnosticProfile = "de25-embedded-hps-first-diagnostic-v1"
const HPSFirstDDRDiagnosticProfile = "de25-embedded-hps-first-ddr-diagnostic-v1"

// HPSFirstPair is the producer's content-bound, unsigned diagnostic receipt.
// Its phase-one hash is useful only when compared with a trusted boot handoff.
type HPSFirstPair struct {
	Format            int    `json:"format"`
	Classification    string `json:"classification"`
	PackageID         string `json:"package_id"`
	BuildID           string `json:"build_id"`
	BuildInputsSHA256 string `json:"build_inputs_sha256"`
	HPSIOHash         string `json:"hps_io_hash"`
	PFGIdentity       string `json:"pfg_identity"`
	QuartusVersion    string `json:"quartus_version"`
	Phase1SHA256      string `json:"phase1_sha256"`
	Phase1Size        int64  `json:"phase1_size"`
	CoreSHA256        string `json:"core_sha256"`
	CoreSize          int64  `json:"core_size"`
	VendorSPLSHA256   string `json:"vendor_spl_sha256"`
}

// HPSFirstHandoff is a platform assertion about the boot that is actually
// running. It cannot be inferred from the producer's sidecar or a package.
type HPSFirstHandoff struct {
	BootOrder, Phase1SHA256, HPSIOHash, PFGIdentity string
}

func validPFGIdentity(s string) bool {
	parts := strings.Split(s, "; binary-sha256:")
	return len(parts) == 2 && strings.HasPrefix(parts[0], "wrapper-sha256:") &&
		nonzeroDigest(strings.TrimPrefix(parts[0], "wrapper-sha256:")) && nonzeroDigest(parts[1])
}

func nonzeroDigest(s string) bool { return validHex(s, 64) && s != strings.Repeat("0", 64) }

func VerifyHPSFirstPair(pair HPSFirstPair, boot HPSFirstHandoff, id fabricmodel.PackageIdentity) error {
	if pair.Format != 1 || pair.Classification != "unqualified-hps-first-diagnostic" ||
		id.Platform != "de25_nano" || id.Device != "A5EB013BB23BE4SCS" || id.ProgrammingProfile != HPSFirstDiagnosticProfile ||
		id.CoreID != "fes.de25-embedded-diagnostic" || id.ABI != "fes.de25-embedded-diagnostic" || id.Major != 1 || id.Minor != 0 || id.ActivationReceiptSHA256 != "" ||
		!nonzeroDigest(pair.PackageID) || !validHex(pair.BuildID, 32) || pair.BuildID == strings.Repeat("0", 32) || !nonzeroDigest(pair.BuildInputsSHA256) ||
		!nonzeroDigest(pair.HPSIOHash) || !nonzeroDigest(pair.Phase1SHA256) || !nonzeroDigest(pair.CoreSHA256) ||
		!nonzeroDigest(pair.VendorSPLSHA256) || pair.Phase1Size <= 0 || pair.CoreSize <= 0 ||
		!validPFGIdentity(pair.PFGIdentity) || pair.QuartusVersion == "" ||
		pair.PackageID != id.PackageID || pair.BuildID != id.BuildID || pair.CoreSHA256 != id.ProgrammedSHA256 ||
		boot.BootOrder != "HPS FIRST" || pair.Phase1SHA256 != boot.Phase1SHA256 ||
		pair.HPSIOHash != boot.HPSIOHash || pair.PFGIdentity != boot.PFGIdentity {
		return errors.New("native fabric: HPS-first package and trusted boot handoff do not match")
	}
	return nil
}
