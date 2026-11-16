# SPDX-License-Identifier: MIT
"""scripts/record_i386_device_fits.py collects fits into a record. It copies
figures and computes none, and it keeps each device's figures with that
device's own totals."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts import record_i386_device_fits as record


def write_fit(directory: Path, *, device: str, family: str, alms: float, available: float,
              device_alms: int, ram_block: str, host: str = "powerboat") -> Path:
    directory.mkdir(parents=True)
    (directory / "fit.json").write_text(json.dumps({
        "status": "pass", "top": "fes_i386_cpu", "device": device, "family": family,
        "seed": 1, "period_ns": 16.276, "host": host, "real_pins": 1,
        "quartus": {"release": "26.3.0", "edition": "pro", "lock": "boards/de25nano/quartus.lock"},
        "classification": "internal virtual-pin characterisation fit", "hardware_acceptance": "not-run",
        "programming_artifact": None,
        "resources": {"alms": {"used": alms, "available": available,
                               "percent_of_device": round(100 * alms / available, 2),
                               "report_label": "Logic utilization"}},
        "timing": {"status": "pass", "corner_count": 4, "fmax": [{"corner": "Slow", "clocks": []}]},
    }))
    (directory / "inputs.json").write_text(json.dumps({
        "board": "DE25-Nano", "documented_device_alms": device_alms, "ram_block": ram_block,
        "clock_hz": 61_440_000, "real_clocks": ["clk"], "virtual_port_count": 1306,
    }))
    (directory / "netlist.json").write_text(json.dumps({"netlist_sha256": "a" * 64}))
    (directory / "timing.tsv").write_text(
        "corner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto\n"
        "Slow fix4 0C\t0\tsetup\tanalyzed\t1\t6.048\tx\ty\n")
    return directory


class Collect(unittest.TestCase):
    def test_each_fit_keeps_its_own_device_totals_and_no_ratio_is_computed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            agilex = write_fit(root / "a", device="A5EB013BB23BE4SCS", family="Agilex 5",
                               alms=29114, available=46800, device_alms=46800, ram_block="M20K")
            cyclone = write_fit(root / "c", device="5CSEBA6U23I7", family="Cyclone V",
                                alms=19870, available=41910, device_alms=41910, ram_block="M10K")
            out = root / "record"
            self.assertEqual(record.main(["--record", str(out),
                                          f"--fit=cpu-de25={agilex}", f"--fit=cpu-de10={cyclone}"]), 0)
            fits = {fit["fit"]: fit for fit in json.loads((out / "quartus.json").read_text())["fits"]}
            self.assertEqual(fits["cpu-de25"]["resources"]["alms"]["available"], 46800)
            self.assertEqual(fits["cpu-de10"]["resources"]["alms"]["available"], 41910)
            self.assertEqual(fits["cpu-de25"]["ram_block"], "M20K")
            self.assertEqual(fits["cpu-de10"]["ram_block"], "M10K")
            # Nothing in the record divides one device's figure by the other's.
            text = (out / "quartus.json").read_text()
            self.assertNotIn("ratio", text)
            host = json.loads((out / "host.json").read_text())
            self.assertIn("different units", host["comparison_note"])
            self.assertEqual(host["hosts"], ["powerboat"])

    def test_timing_rows_are_labelled_by_fit(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            agilex = write_fit(root / "a", device="A5EB013BB23BE4SCS", family="Agilex 5",
                               alms=1, available=2, device_alms=46800, ram_block="M20K")
            out = root / "record"
            record.main(["--record", str(out), f"--fit=cpu-de25={agilex}"])
            rows = (out / "timing.tsv").read_text().splitlines()
            self.assertEqual(rows[0].split("\t")[0], "fit")
            self.assertTrue(rows[1].startswith("cpu-de25\tSlow fix4 0C\t"))

    def test_a_fit_argument_without_a_name_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            self.assertEqual(record.main(["--record", temporary + "/r", "--fit=/some/dir"]), 1)


if __name__ == "__main__":
    unittest.main()
