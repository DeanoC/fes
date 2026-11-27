from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
DRIVER = ROOT / "scripts/powerboat/hil-de25-core.sh"
RESTORE_MARKER = 'RESTORE_TOOL="$FES_SOURCE/scripts/de25_restore.py"'


class De25CoreRestoreTests(unittest.TestCase):
    def run_restore_epilogue(
        self, verifier_rc: int, ocr: str = "Home\n\n= FES", *, first_ocr: str = "", capture_rc: int = 0
    ) -> tuple[subprocess.CompletedProcess[str], str, str, str]:
        source = DRIVER.read_text()
        epilogue = source[source.index(RESTORE_MARKER) :]

        with tempfile.TemporaryDirectory(prefix="fes-de25-restore-") as temporary:
            run_dir = Path(temporary)
            earlier_capture = run_dir / "out/restore-menu.previous"
            earlier_capture.mkdir(parents=True)
            (earlier_capture / "1.png").write_text("stale frame\n")
            (run_dir / "logs").mkdir()
            status = run_dir / "kit-status.md"
            status.write_text("## KIT D25 IN USE\n")

            harness = "\n".join(
                (
                    "set -euo pipefail",
                    'D="${D:?}"',
                    'STATUS="${STATUS:?}"',
                    'FES_SOURCE="$D/source"',
                    'ROOT="$D"',
                    'BASE="$D/baseline.json"',
                    'PR=2246',
                    'HEAD=0123456789abcdef',
                    'PASS=1',
                    'RESTORE=0',
                    'PKGS=(/tmp/test.fcore)',
                    'PKG_SHAS=(deadbeef)',
                    'fail() { printf "%s\\n" "$*" >> "$D/logs/driver.log"; }',
                    'python3() { return "$VERIFY_RC"; }',
                    'sleep() { :; }',
                    'timeout() { shift 3; "$@"; }',
                    'ffmpeg() {',
                    '  [[ " $* " == *" -i /dev/v4l/by-id/fes-ms2109-hil "* ]] || return 2',
                    '  echo capture >> "$D/logs/driver.log"',
                    '  [ "$CAPTURE_RC" = 0 ] || return "$CAPTURE_RC"',
                    '  echo frame > "${@: -1}"',
                    '}',
                    'tesseract() {',
                    '  echo OCR >> "$D/logs/driver.log"',
                    '  if [[ -n "$FIRST_OCR" && "$1" = */1.png ]]; then',
                    '    printf "%s\\n" "$FIRST_OCR"',
                    '  else',
                    '    printf "%s\\n" "$OCR"',
                    '  fi',
                    '}',
                    epilogue,
                )
            )
            environment = os.environ.copy()
            environment.update(
                {
                    "D": str(run_dir),
                    "STATUS": str(status),
                    "VERIFY_RC": str(verifier_rc),
                    "CAPTURE_RC": str(capture_rc),
                    "FIRST_OCR": first_ocr,
                    "OCR": ocr,
                }
            )
            result = subprocess.run(
                ["bash", "-c", harness],
                check=False,
                capture_output=True,
                text=True,
                env=environment,
            )

            ledger = status.read_text()
            log_path = run_dir / "logs/driver.log"
            if not log_path.exists():
                self.fail(
                    "restore epilogue did not write its driver log; "
                    f"rc={result.returncode}, stdout={result.stdout!r}, stderr={result.stderr!r}"
                )
            log = log_path.read_text()
            verdict = (run_dir / "verdict").read_text()
            return result, ledger, log, verdict

    def test_failed_verification_keeps_kit_claimed_for_recovery(self) -> None:
        result, ledger, log, verdict = self.run_restore_epilogue(verifier_rc=1)

        self.assertEqual(result.returncode, 1)
        self.assertNotIn("KIT D25 FREE (baseline verified)", ledger)
        self.assertIn("KIT D25 RESTORE_INCOMPLETE", ledger)
        self.assertIn("DE25 restore verification failed", log)
        self.assertNotIn("capture", log)
        self.assertNotIn("restore verified; J8G released FREE", log)
        self.assertEqual(verdict, "FAIL\n")

    def test_successful_verification_and_visible_menu_release_kit(self) -> None:
        for ocr in ("Home\n\n= FES", "FES | Library\nB home GUIDE settings", "Welcome to FES"):
            with self.subTest(ocr=ocr):
                result, ledger, log, verdict = self.run_restore_epilogue(verifier_rc=0, ocr=ocr)

                self.assertEqual(result.returncode, 0)
                self.assertIn("KIT D25 FREE (baseline verified)", ledger)
                self.assertNotIn("KIT D25 RESTORE_INCOMPLETE", ledger)
                self.assertIn("restored MENU verified in", log)
                self.assertEqual(verdict, "PASS\n")

    def test_retry_waits_for_menu_after_room_error(self) -> None:
        result, ledger, log, verdict = self.run_restore_epilogue(
            verifier_rc=0, first_ocr="Home\n= FES\nRoom failed: Home"
        )

        self.assertEqual(result.returncode, 0)
        self.assertIn("KIT D25 FREE (baseline verified)", ledger)
        self.assertEqual(log.splitlines().count("capture"), 2)
        self.assertIn("/2.png", log)
        self.assertEqual(verdict, "PASS\n")

    def test_non_menu_frames_keep_kit_claimed(self) -> None:
        for ocr in ("", "FES | Now playing\nB home", "Home\n= FES\nRoom\nfailed: Home"):
            with self.subTest(ocr=ocr):
                result, ledger, log, verdict = self.run_restore_epilogue(verifier_rc=0, ocr=ocr)

                self.assertEqual(result.returncode, 1)
                self.assertNotIn("KIT D25 FREE (baseline verified)", ledger)
                self.assertIn("KIT D25 RESTORE_INCOMPLETE", ledger)
                self.assertIn("DE25 restored MENU not verified", log)
                self.assertEqual(log.splitlines().count("capture"), 12)
                self.assertEqual(verdict, "FAIL\n")

    def test_capture_failure_cannot_reuse_stale_menu(self) -> None:
        result, ledger, log, verdict = self.run_restore_epilogue(verifier_rc=0, capture_rc=124)

        self.assertEqual(result.returncode, 1)
        self.assertIn("KIT D25 RESTORE_INCOMPLETE", ledger)
        self.assertNotIn("KIT D25 FREE (baseline verified)", ledger)
        self.assertNotIn("OCR", log)
        self.assertEqual(verdict, "FAIL\n")


if __name__ == "__main__":
    unittest.main()
