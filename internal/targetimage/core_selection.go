package targetimage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"io"
	"path/filepath"
)

// extraCoreRecipe keeps image admission bounded to the explicitly supported
// source-built additions. Mega Drive retains its existing upstream selector.
func extraCoreRecipe(system string) (string, string, error) {
	switch system {
	case "pong":
		return "scripts/build_pong.py", "https://github.com/DeanoC/misteross", nil
	case "snes":
		return "scripts/rebuild_core.py", "https://github.com/MiSTer-devel/SNES_MiSTer", nil
	case "nes":
		return "scripts/rebuild_core.py", "https://github.com/MiSTer-devel/NES_MiSTer", nil
	default:
		return "", "", fmt.Errorf("additional core must be pong, snes or nes")
	}
}

func validateExtraCoreManifest(m MegaDriveBundleManifest, system string) error {
	recipe, repository, err := extraCoreRecipe(system)
	if err != nil {
		return err
	}
	if err := validateCoreManifest(m, system, recipe); err != nil {
		return err
	}
	if m.Repository != repository {
		return fmt.Errorf("unexpected %s source repository", system)
	}
	if system == "snes" && m.Revision != "93d359e6f23c734ae3928984e88bed1d9b53cbac" {
		return fmt.Errorf("unexpected SNES source revision")
	}
	if system == "nes" && m.Revision != "9a63821173b6da4d6e95dcbe2e2a322ec8171144" {
		return fmt.Errorf("unexpected NES source revision")
	}
	return nil
}

// PrepareCoreSelection verifies the closed sealed bundle and reuses the same
// staged artifact/selection pair publication as the Mega Drive path.
func PrepareCoreSelection(system, bundle, cache, output string) (MegaDriveSelection, error) {
	var selection MegaDriveSelection
	if _, _, err := extraCoreRecipe(system); err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(cache, "cache"); err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(output, "output"); err != nil {
		return selection, err
	}
	if filepath.Clean(output) == filepath.Join(filepath.Clean(cache), system+".rbf") {
		return selection, fmt.Errorf("output must not replace cached artifact")
	}
	if err := validateCoreBundleDirectory(bundle, system); err != nil {
		return selection, err
	}
	file, _, err := openRegularNoFollow(filepath.Join(bundle, system+"-rbf.toml"), true)
	if err != nil {
		return selection, err
	}
	var manifest MegaDriveBundleManifest
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&manifest)
	closeErr := file.Close()
	if err != nil {
		return selection, err
	}
	if closeErr != nil {
		return selection, closeErr
	}
	if err := validateExtraCoreManifest(manifest, system); err != nil {
		return selection, err
	}
	selection = MegaDriveSelection{Format: 1, Origin: "source-built", ABI: manifest.ABI, System: manifest.System, Repository: manifest.Repository, Revision: manifest.Revision, Artifact: manifest.Artifact, SHA256: manifest.SHA256, Size: manifest.Size, InstallPath: "/usr/share/mister-runtime/cores/" + system + ".rbf", Recipe: manifest.Recipe, RecipeSHA256: manifest.RecipeSHA256, Toolchain: manifest.Toolchain, Label: manifest.Label}
	err = installMegaDriveSelection(filepath.Join(bundle, system+".rbf"), selection.SHA256, selection.Size, cache, output, true, selection)
	return selection, err
}

// VerifyCoreSelection validates an externally supplied expected selection and
// compares the artifact bytes. The image never supplies its own expected set.
func VerifyCoreSelection(system, artifact, record string) error {
	file, _, err := openRegularNoFollow(record, true)
	if err != nil {
		return err
	}
	var s MegaDriveSelection
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&s)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if s.Origin != "source-built" || s.InstallPath != "/usr/share/mister-runtime/cores/"+system+".rbf" {
		return fmt.Errorf("invalid source-built selection")
	}
	m := MegaDriveBundleManifest{Format: s.Format, ABI: s.ABI, System: s.System, Artifact: s.Artifact, SHA256: s.SHA256, Size: s.Size, Repository: s.Repository, Revision: s.Revision, Recipe: s.Recipe, RecipeSHA256: s.RecipeSHA256, Toolchain: s.Toolchain, Label: s.Label}
	if err := validateExtraCoreManifest(m, system); err != nil {
		return err
	}
	// Installed images conventionally use 0644; expected selection remains sealed.
	file, info, err := openRegularNoFollow(artifact, false)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if info.Size() != s.Size || size != s.Size || hex.EncodeToString(hash.Sum(nil)) != s.SHA256 {
		return fmt.Errorf("artifact differs from selection")
	}
	return nil
}
