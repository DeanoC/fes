"""The Linux-free build requires a deliberate selection, independent of BOARD."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("embedded_experiment", ROOT / "platform/embedded/experiment.py")
experiment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(experiment)


class EmbeddedOptInTests(unittest.TestCase):
    def test_only_explicit_one_enables_builds(self):
        for env in ({}, {"BOARD": "de25-nano"}, {experiment.VARIABLE: ""},
                    {experiment.VARIABLE: "0"}, {experiment.VARIABLE: "true"}):
            with self.subTest(env=env), self.assertRaisesRegex(ValueError, experiment.VARIABLE):
                experiment.require_experiment(env)
        experiment.require_experiment({experiment.VARIABLE: "1"})
