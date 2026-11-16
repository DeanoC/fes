"""Ensure native lifecycle phases neither drop nor repeat existing Go tests."""
import ast
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
TAMAGO = ROOT / "platform/embedded/tamago"
# Read the runner's literal policy without importing build tooling into the
# shared parent-test interpreter or changing its module search path.
tree = ast.parse((TAMAGO / "nativefabric/verify.py").read_text())
PHASES = next(ast.literal_eval(node.value) for node in tree.body
              if isinstance(node, ast.Assign)
              and any(isinstance(target, ast.Name) and target.id == "NATIVE_PHASES" for target in node.targets))


class NativePhaseTests(unittest.TestCase):
    def test_existing_native_cases_belong_to_exactly_one_phase(self):
        directories = ("usbcontrol", "dwc2", "board/arm64map", "nativefabric",
                       "nativefabriccheck", "embeddedcontrol", "fabricmodel", "runtimeadmission")
        cases = []
        for directory in directories:
            for source in (TAMAGO / directory).rglob("*_test.go"):
                for name in re.findall(r"^func (Test\w+)\(t \*testing\.T\)", source.read_text(), re.MULTILINE):
                    cases.append((source, name))
        self.assertGreater(len(cases), 100)
        selected = {label: 0 for label, _ in PHASES}
        for source, name in cases:
            phases = []
            for label, (flag, pattern) in PHASES:
                self.assertIn(flag, ("-run", "-skip"))
                match = re.search(pattern, name) is not None
                if match == (flag == "-run"):
                    phases.append(label)
                    selected[label] += 1
            self.assertEqual(len(phases), 1, (str(source.relative_to(ROOT)), name, phases))
        self.assertTrue(all(selected.values()), selected)


if __name__ == "__main__":
    unittest.main()
