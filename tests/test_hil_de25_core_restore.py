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
    def run_restore_epilogue(self, verifier_rc: int) -> tuple[subprocess.CompletedProcess[str], str, str, str]:
        source = DRIVER.read_text()
        epilogue = source[source.index(RESTORE_MARKER) :]

        with tempfile.TemporaryDirectory(prefix="fes-de25-restore-") as temporary:
            run_dir = Path(temporary)
            (run_dir / "out").mkdir()
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
                    epilogue,
                )
            )
            environment = os.environ.copy()
            environment.update(
                {
                    "D": str(run_dir),
                    "STATUS": str(status),
                    "VERIFY_RC": str(verifier_rc),
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
        self.assertNotIn("restore verified; J8G released FREE", log)
        self.assertEqual(verdict, "FAIL\n")

    def test_successful_verification_still_releases_kit(self) -> None:
        result, ledger, log, verdict = self.run_restore_epilogue(verifier_rc=0)

        self.assertEqual(result.returncode, 0)
        self.assertIn("KIT D25 FREE (baseline verified)", ledger)
        self.assertNotIn("KIT D25 RESTORE_INCOMPLETE", ledger)
        self.assertIn("restore verified; J8G released FREE", log)
        self.assertEqual(verdict, "PASS\n")


if __name__ == "__main__":
    unittest.main()
