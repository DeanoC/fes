"""Guard the source corrections in the v1 firmware licensing research note."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
DOC = (ROOT / "docs/plans/v1-firmware-licensing.md").read_text()


def evidence_line(prefix):
    return next(line for line in DOC.splitlines() if line.startswith(prefix))


def licensing_row(prefix):
    table = DOC.split("## Per-core table", 1)[1].split("\n## Evidence", 1)[0]
    return next(line for line in table.splitlines() if line.startswith(prefix))


class V1FirmwareLicensingDocTests(unittest.TestCase):
    def test_v1_scope_links_to_local_decision_record(self):
        scope = DOC.split("\n\nEvery source", 1)[0]
        self.assertIn("The v1 scope is the four families Deano chose on 1 November", scope)
        self.assertIn("ZX81, Spectrum, C64 and Atari ST", scope)
        match = re.search(r"\[v1 first-release decisions\]\(([^)]+)\)", scope)
        self.assertIsNotNone(match)
        target = match.group(1)
        self.assertFalse(re.match(r"^[A-Za-z][A-Za-z0-9+.-]*:", target))
        self.assertTrue((ROOT / "docs/plans" / target).is_file())

    def test_recorded_deano_decisions_replace_open_questions(self):
        self.assertIn("Deano answered these decisions in the PR discussion", DOC)
        self.assertIn("1. **Ship Amstrad's Spectrum ROMs in the free download? Agreed.**", DOC)
        self.assertIn("2. **Sold or preinstalled hardware. Noted.**", DOC)
        self.assertIn("3. **Which Spectrum ROM versions to recognise? Agreed.**", DOC)
        self.assertIn("4. **Pentagon firmware composition. Agreed.**", DOC)
        self.assertIn("5. **ZX81 family. Deano's decision needed.**", DOC)
        self.assertIn("6. **EmuTOS image and source. Agreed.**", DOC)
        self.assertIn("7. **Commercial C64 licence. Agreed.**", DOC)
        self.assertNotIn("if Deano accepts Decision 1", DOC)
        decision1 = evidence_line("1. **Ship Amstrad's Spectrum ROMs")
        decision6 = evidence_line("6. **EmuTOS image and source.")
        self.assertIn("remove the ROMs if a rights holder objects", decision1)
        self.assertIn("release HIL passes on the PAL core", decision6)

    def test_zx81_rom_bundling_is_recorded_and_deferred_to_deano(self):
        state = evidence_line("Firmware bytes are already in Git:")
        evidence = evidence_line("- **Z0.")
        decision = evidence_line("5. **ZX81 family.")
        self.assertIn("machine.rom", state)
        self.assertIn("core_roms.go", state)
        self.assertIn("build_fes_zx81.py", state)
        self.assertIn("zx8x.mif", evidence)
        self.assertIn("not permission to redistribute", evidence)
        self.assertIn("Deano's decision needed", decision)
        self.assertIn("removing them is a separate PR", decision)
        self.assertIn("Pentagon's entire 48 KiB image", DOC)

    def test_decision_links_use_the_current_heading_anchor(self):
        self.assertIn("## Decisions recorded 1 November 2026", DOC)
        self.assertEqual(DOC.count("#decisions-recorded-1-november-2026"), 3)
        self.assertNotIn("#decisions-for-deano", DOC)

    def test_debian_vice_rom_claim_matches_its_sources(self):
        claim = evidence_line("- **C3.**")
        self.assertIn("copyrighted, and you install them at your own legal risk", claim)
        self.assertIn("have been removed from the Debian source package", claim)
        self.assertNotIn("redistribution is not permitted", claim)

    def test_ts1500_evidence_does_not_claim_a_timex_grant(self):
        row = licensing_row("| `fes.ts1500` |")
        decision = evidence_line("5. **ZX81 family. Deano's decision needed.**")
        self.assertIn("neither establishes TS1500 permission", row)
        self.assertNotIn("Timex permits distribution", row)
        self.assertIn("TS2000 BASIC", evidence_line("- **Z1.**"))
        self.assertIn("write to Nine Tiles Networks to ask permission", decision)
        self.assertIn("TS1500's 61 changed bytes remain unverified", decision)

    def test_emutos_obligations_and_label_match_the_evidence(self):
        top = evidence_line("- **EmuTOS 1.4 for `fes.atari-st`: yes.**")
        row = licensing_row("| `fes.atari-st` |")
        decision = evidence_line("6. **EmuTOS image and source. Agreed.**")
        notes = evidence_line("- `fetch_atari_st_emutos.py`")
        e1 = evidence_line("- **E1.**")
        self.assertIn("full GPL text from `doc/license.txt`", top)
        self.assertIn("same release page as the binary", top)
        self.assertIn("preinstalled on a physical kit", top)
        self.assertIn("corresponding source on the card or provide a valid written offer", top)
        self.assertIn("GPL-2.0-or-later", row)
        self.assertNotIn("GPL-2.0-only", row)
        self.assertIn("upstream tag `VERSION_1_4`", row)
        self.assertNotIn("as in `fetch_atari_st_emutos.py`", row)
        self.assertIn("same release page as the binary", decision)
        self.assertIn("preinstalled card needs the source on the card or a valid written offer", decision)
        self.assertIn("Keep that existing label for the bundle", notes)
        self.assertIn("does not say \"only\"", notes)
        self.assertIn("GPL-2.0-or-later label", e1)
        self.assertIn("does not say \"only\"", e1)
        self.assertNotIn("release-level label", e1)

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
        a6 = evidence_line("- **The Spectrum (Retro Games, 2025)**")
        self.assertNotIn("The Spectrum", practice)
        self.assertIn("Amstrad permitted non-commercial distribution, so this is legal, but RGL can't sell it that way", a6)
        self.assertIn("support page separately says users may load an original ROM image they own", a6)
        self.assertIn("does not establish permission to redistribute ROMs", a6)
        self.assertIn("consumer-emulator practice, not FPGA distribution evidence", a6)

    def test_kit_passed_spectrum_roms_require_firmware_digests(self):
        decision = evidence_line("3. **Which Spectrum ROM versions to recognise?")
        self.assertIn("48K kit record already includes the ROM SHA-256", decision)
        self.assertIn("later 128K, +3 and Pentagon kit checks omit firmware digests", decision)
        self.assertIn("record those missing digests before describing those versions as kit-checked", decision)


if __name__ == "__main__":
    unittest.main()
