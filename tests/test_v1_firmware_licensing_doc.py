"""Guard the source corrections in the v1 firmware licensing research note."""
from pathlib import Path
import unittest


DOC = (Path(__file__).resolve().parents[1] / "docs/plans/v1-firmware-licensing.md").read_text()


def evidence_line(prefix):
    return next(line for line in DOC.splitlines() if line.startswith(prefix))


def licensing_row(prefix):
    table = DOC.split("## Per-core table", 1)[1].split("\n## Evidence", 1)[0]
    return next(line for line in table.splitlines() if line.startswith(prefix))


class V1FirmwareLicensingDocTests(unittest.TestCase):
    def test_v1_scope_links_to_the_decision_record(self):
        scope = DOC.split("\n\nEvery source", 1)[0]
        self.assertIn("docs/plans/v1-first-release-first-boot-setup.md", scope)

    def test_debian_vice_rom_claim_matches_its_sources(self):
        claim = evidence_line("- **C3.**")
        self.assertIn("copyrighted, and you install them at your own legal risk", claim)
        self.assertIn("have been removed from the Debian source package", claim)
        self.assertNotIn("redistribution is not permitted", claim)

    def test_ts1500_evidence_does_not_claim_a_timex_grant(self):
        row = licensing_row("| `fes.ts1500` |")
        decision = evidence_line("5. **Ask Nine Tiles and Timex.**")
        self.assertIn("neither establishes TS1500 permission", row)
        self.assertNotIn("Timex permits distribution", row)
        self.assertIn("TS2000 BASIC", evidence_line("- **Z1.**"))
        self.assertIn("Nine Tiles grant alone would not clear the TS1500-specific bytes", decision)
        self.assertNotIn("all four ZX81-family cores could ship", decision)

    def test_emutos_uses_upstream_license_and_same_page_source_access(self):
        top = evidence_line("- **EmuTOS 1.4 for `fes.atari-st`: yes.**")
        row = licensing_row("| `fes.atari-st` |")
        decision = evidence_line("6. **EmuTOS image and where its source goes.**")
        notes = evidence_line("- `fetch_atari_st_emutos.py`")
        self.assertIn("same release page as the binary", top)
        self.assertIn("not be embedded in an image or sealed package", top)
        self.assertIn("explicit release-level statement", row)
        self.assertIn("same release page as the binary", decision)
        self.assertIn("explicit statement that the project is distributed under GPL version 2", notes)
        self.assertNotIn("absence of \"or later\"", row)

    def test_emutos_turrican_sources_are_reported_as_conflicting(self):
        claim = evidence_line("- **E6.**")
        self.assertIn("Turrican", claim)
        self.assertIn("Hatari", claim)
        self.assertIn("list Turrican among commercial games that work fine", claim)
        self.assertIn("These sources conflict", claim)

    def test_the_spectrum_is_consumer_emulator_evidence(self):
        practice = DOC.split("**FPGA practice (A4–A5).**", 1)[1].split(
            "**Consumer emulator practice (A6).**", 1
        )[0]
        self.assertNotIn("The Spectrum", practice)
        self.assertIn("The Spectrum (Retro Games, 2025)** is an Arm/Linux emulator", DOC)
        self.assertIn("consumer-emulator practice, not FPGA distribution evidence", DOC)
        self.assertIn("users may load an original ROM image they own", DOC)

    def test_kit_passed_spectrum_roms_require_firmware_digests(self):
        decision = evidence_line("3. **Which Spectrum ROM versions to recognise.**")
        self.assertIn("package and RBF hashes but not firmware digests", decision)
        self.assertIn("#1344/#1353 record each ROM's SHA-256", decision)
        self.assertIn("before describing a version as having passed a kit check", decision)


if __name__ == "__main__":
    unittest.main()
