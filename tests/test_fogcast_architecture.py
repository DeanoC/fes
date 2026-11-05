from pathlib import Path
import re
import unittest


ARCHITECTURE = (
    Path(__file__).resolve().parents[1]
    / "sources/FogCast/docs/ARCHITECTURE.md"
)


class FogCastArchitectureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.architecture = re.sub(r"\s+", " ", ARCHITECTURE.read_text())

    def test_target_agent_memory_budget_is_documented(self):
        self.assertIn("the target agent defaults to 64 MiB", self.architecture)
        self.assertIn("`GOMEMLIMIT` overrides either default", self.architecture)
        self.assertIn("they are not hard RSS caps", self.architecture)

    def test_current_spooled_launch_paths_and_filesystem_requirement_are_documented(self):
        for path in (
            "`corepackage.StageROMInputSpooled`",
            "`corepackage.StageROMInputV2Spooled`",
            "`corepackage.StagePcxtInitialInputSpooled`",
            "`corepackage.StageCompositionBytes`",
        ):
            with self.subTest(path=path):
                self.assertIn(path, self.architecture)
        self.assertIn("closes the mapping before the runtime load", self.architecture)
        self.assertIn("requires hard-link support in the staging-root", self.architecture)
        self.assertIn("FAT does not support the required", self.architecture)


if __name__ == "__main__":
    unittest.main()
