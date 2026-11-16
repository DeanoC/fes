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
              device_alms: int, ram_block: str, host: str = "powerboat",
              status: str = "pass", stage: str | None = None,
              revision: str = "a" * 40, clean: bool = True) -> Path:
    directory.mkdir(parents=True)
    fit = {
        "status": status, "top": "fes_i386_cpu", "device": device, "family": family,
        "real_pins": 1,
        "unconstrained_clock_rows": [["Illegal Clocks", "0", "0"],
                                     ["Unconstrained Clocks", "0", "0"]],
        "seed": 1, "period_ns": 16.276, "host": host,
        "quartus": {"release": "26.3.0", "edition": "pro", "lock": "boards/de25nano/quartus.lock"},
        "classification": "internal virtual-pin characterisation fit", "hardware_acceptance": "not-run",
        "programming_artifact": None,
        "resources": {"alms": {"used": alms, "available": available,
                               "percent_of_device": round(100 * alms / available, 2),
                               "report_label": "Logic utilization"}},
        "timing": {"status": "pass", "corner_count": 4,
                   "corners": [{"corner": f"Corner {i}", "hold_only": False,
                                "analyses": {c: {"coverage": "analyzed"} for c in
                                             ("setup", "hold", "minimum_pulse_width")}}
                               for i in range(4)],
                   "fmax": [{"corner": "Slow", "clocks": []}]},
    }
    if stage is not None:
        fit["resources_stage"] = stage
    (directory / "fit.json").write_text(json.dumps(fit))
    (directory / "inputs.json").write_text(json.dumps({
        "board": "DE25-Nano", "documented_device_alms": device_alms, "ram_block": ram_block,
        "clock_hz": 61_440_000, "real_clocks": ["clk"], "virtual_port_count": 1306,
        "fes_revision": ({"commit": revision, "clean": True} if clean else
                         {"commit": revision, "clean": False,
                          "tracked_diff": "-a\n+b\n", "tracked_diff_sha256": "f" * 64}),
    }))
    (directory / "netlist.json").write_text(json.dumps({"netlist_sha256": "a" * 64}))
    # Four rows, because fit.json above records a four-corner verdict; the
    # collector refuses a table that does not cover every corner.
    # Four corners, each carrying the three checks a closure rests on, because
    # fit.json above records a four-corner verdict and the collector refuses a
    # table truncated in either direction.
    (directory / "timing.tsv").write_text(
        "corner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto\n"
        + "".join(f"Corner {i}\t0\t{c}\tanalyzed\t1\t6.048\tx\ty\n"
                  for i in range(4) for c in ("setup", "hold", "minimum_pulse_width")))
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
            self.assertTrue(rows[1].startswith("cpu-de25\tCorner 0\t"))
            self.assertEqual(len(rows), 13, "header plus four corners of three checks")

    def test_a_partial_fit_names_the_stage_its_counts_came_from(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            partial = write_fit(root / "p", device="A5EB013BB23BE4SCS", family="Agilex 5",
                                alms=1, available=2, device_alms=46800, ram_block="M20K",
                                status="fail", stage="route")
            out = root / "record"
            record.main(["--record", str(out), f"--fit=m-de25={partial}"])
            fit = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertEqual(fit["resources_stage"], "route")

    def test_a_fit_whose_timing_gate_failed_is_not_called_incomplete(self):
        # A missed ladder rung is a complete fit with a failing verdict, and
        # its area is exactly what a reader wants beside the rungs that closed.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            missed = write_fit(root / "m", device="A5EB013BB23BE4SCS", family="Agilex 5",
                               alms=30000, available=46800, device_alms=46800, ram_block="M20K",
                               status="fail")
            fit = json.loads((missed / "fit.json").read_text())
            fit["timing"]["status"] = "fail"
            (missed / "fit.json").write_text(json.dumps(fit))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=cpu-de25-150-MISS={missed}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIn("complete fit", entry["resources_stage"])
            self.assertIn("missed rung", entry["resources_stage"])
            self.assertNotIn("incomplete", entry["resources_stage"])

    def test_a_miss_whose_own_validation_failed_is_not_a_bound(self):
        # A missed rung is only a usable upper bound if its analysis covered
        # every clock. If that check failed, the error must travel with the
        # figures instead of the rung being published as a valid bound.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            bad = write_fit(root / "b", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=30000, available=46800, device_alms=46800, ram_block="M20K",
                            status="fail")
            fit = json.loads((bad / "fit.json").read_text())
            fit["timing"]["status"] = "fail"
            fit["timing_parse_error"] = "TimeQuest reports unconstrained or illegal clocks"
            (bad / "fit.json").write_text(json.dumps(fit))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=cpu-de25-150-MISS={bad}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIn("NOT usable as a timing bound", entry["resources_stage"])
            self.assertIn("timing_parse_error", entry["validation_errors"])
            self.assertNotIn("missed rung", entry["resources_stage"])

    def test_a_miss_with_no_evidence_of_the_checks_is_not_a_bound(self):
        # A fit directory made before those checks existed carries neither the
        # evidence nor an error. The absence of an error is not evidence.
        for drop in ("real_pins", "unconstrained_clock_rows"):
            with self.subTest(missing=drop), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                old = write_fit(root / "o", device="A5EB013BB23BE4SCS", family="Agilex 5",
                                alms=30000, available=46800, device_alms=46800,
                                ram_block="M20K", status="fail")
                fit = json.loads((old / "fit.json").read_text())
                fit["timing"]["status"] = "fail"
                del fit[drop]
                (old / "fit.json").write_text(json.dumps(fit))
                out = root / "record"
                record.main(["--record", str(out), f"--fit=cpu-de25-150-MISS={old}"])
                entry = json.loads((out / "quartus.json").read_text())["fits"][0]
                self.assertIn("NOT usable as a timing bound", entry["resources_stage"])
                self.assertNotIn("missed rung", entry["resources_stage"])

    def test_a_miss_whose_coverage_rows_are_nonzero_is_not_a_bound(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            bad = write_fit(root / "b", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=30000, available=46800, device_alms=46800, ram_block="M20K",
                            status="fail")
            fit = json.loads((bad / "fit.json").read_text())
            fit["timing"]["status"] = "fail"
            fit["unconstrained_clock_rows"] = [["Illegal Clocks", "0", "0"],
                                               ["Unconstrained Clocks", "1", "0"]]
            (bad / "fit.json").write_text(json.dumps(fit))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=cpu-de25-150-MISS={bad}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIn("NOT usable as a timing bound", entry["resources_stage"])

    def test_a_failed_fit_without_a_stage_is_still_qualified(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            partial = write_fit(root / "p", device="A5EB013BB23BE4SCS", family="Agilex 5",
                                alms=1, available=2, device_alms=46800, ram_block="M20K",
                                status="fail")
            out = root / "record"
            record.main(["--record", str(out), f"--fit=m-de25={partial}"])
            fit = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIn("incomplete fit", fit["resources_stage"])

    def test_fits_from_different_revisions_are_named_not_smoothed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            one = write_fit(root / "1", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K",
                            revision="a" * 40)
            two = write_fit(root / "2", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K",
                            revision="b" * 40)
            out = root / "record"
            record.main(["--record", str(out), f"--fit=one={one}", f"--fit=two={two}"])
            host = json.loads((out / "host.json").read_text())
            self.assertIsNone(host["fes_revision"])
            self.assertEqual(host["fit_revisions"], ["a" * 40, "b" * 40])
            self.assertIn("different revisions", host["revision_note"])

    def test_one_revision_across_the_fits_is_reported_as_the_revision(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            one = write_fit(root / "1", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            out = root / "record"
            record.main(["--record", str(out), f"--fit=one={one}"])
            host = json.loads((out / "host.json").read_text())
            self.assertEqual(host["fes_revision"], "a" * 40)
            self.assertTrue(host["collected_at_revision"])
            self.assertEqual(host["fits_from_a_modified_checkout"], [])

    def test_one_fit_with_a_revision_beside_one_without_is_not_agreement(self):
        # Otherwise the known commit is assigned to the record and the note
        # says every fit came from it, attributing the unversioned fit too.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            known = write_fit(root / "k", device="A5EB013BB23BE4SCS", family="Agilex 5",
                              alms=1, available=2, device_alms=46800, ram_block="M20K")
            legacy = write_fit(root / "l", device="A5EB013BB23BE4SCS", family="Agilex 5",
                               alms=1, available=2, device_alms=46800, ram_block="M20K")
            inputs = json.loads((legacy / "inputs.json").read_text())
            del inputs["fes_revision"]
            (legacy / "inputs.json").write_text(json.dumps(inputs))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=known={known}", f"--fit=legacy={legacy}"])
            host = json.loads((out / "host.json").read_text())
            self.assertIsNone(host["fes_revision"])
            self.assertEqual(host["fits_with_no_recorded_revision"], ["legacy"])
            self.assertNotIn("every fit was prepared from one revision", host["revision_note"])

    def test_a_fit_from_a_modified_checkout_is_named(self):
        # The M+ fit is made by editing a tracked file, so its commit alone
        # does not describe the source that ran.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scratch = write_fit(root / "s", device="A5EB013BB23BE4SCS", family="Agilex 5",
                                alms=1, available=2, device_alms=46800, ram_block="M20K",
                                clean=False)
            out = root / "record"
            record.main(["--record", str(out), f"--fit=machineplus-de25={scratch}"])
            host = json.loads((out / "host.json").read_text())
            self.assertEqual(host["fits_from_a_modified_checkout"], ["machineplus-de25"])
            self.assertIn("modified checkout", host["revision_note"])
            fit = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIs(fit["source_clean"], False)
            self.assertIn("tracked_diff", fit["fes_revision"])

    def test_a_passing_fit_without_evidence_is_qualified_too(self):
        # A fit whose VIRTUAL_PIN assignments were ignored would pass while
        # measuring a different envelope, so a pass is no substitute for the
        # evidence either.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            legacy = write_fit(root / "p", device="A5EB013BB23BE4SCS", family="Agilex 5",
                               alms=29114, available=46800, device_alms=46800, ram_block="M20K")
            fit = json.loads((legacy / "fit.json").read_text())
            del fit["real_pins"]
            del fit["unconstrained_clock_rows"]
            (legacy / "fit.json").write_text(json.dumps(fit))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=legacy={legacy}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertEqual(entry["status"], "pass")
            self.assertIn("without its own validation evidence", entry["resources_stage"])
            self.assertIn("real_pins", entry["validation_errors"])
            self.assertIn("clock_coverage", entry["validation_errors"])

    def test_fewer_real_pins_than_clocks_is_a_gap_in_the_collector_too(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            one = write_fit(root / "o", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=35492, available=46800, device_alms=46800, ram_block="M20K")
            inputs = json.loads((one / "inputs.json").read_text())
            inputs["real_clocks"] = ["clk", "pixel_clk"]
            (one / "inputs.json").write_text(json.dumps(inputs))
            out = root / "record"
            record.main(["--record", str(out), f"--fit=machine-de25={one}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertIn("real_pins", entry["validation_errors"])
            self.assertIn("1 real pins against 2 real clocks", entry["validation_errors"]["real_pins"])

    def test_a_passing_fit_with_evidence_is_not_qualified(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            good = write_fit(root / "g", device="A5EB013BB23BE4SCS", family="Agilex 5",
                             alms=29300, available=46800, device_alms=46800, ram_block="M20K")
            out = root / "record"
            record.main(["--record", str(out), f"--fit=good={good}"])
            entry = json.loads((out / "quartus.json").read_text())["fits"][0]
            self.assertNotIn("resources_stage", entry)
            self.assertNotIn("validation_errors", entry)

    def test_a_fit_with_a_timing_result_and_no_rows_is_refused(self):
        # The record would otherwise publish a timing verdict, a corner count
        # and per-corner slacks with none of the evidence behind them.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            (fit / "timing.tsv").unlink()
            self.assertEqual(record.main(["--record", str(root / "r"), f"--fit=x={fit}"]), 1)

    def test_a_timing_file_with_only_a_header_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            (fit / "timing.tsv").write_text("corner\thold_only\tcheck\n")
            self.assertEqual(record.main(["--record", str(root / "r"), f"--fit=x={fit}"]), 1)

    def test_a_truncated_timing_table_is_refused(self):
        # The fixture's verdict covers four corners; a table naming one is as
        # misleading as a missing table, because the verdict is published whole.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            self.assertEqual(record.main(["--record", str(root / "r1"), f"--fit=x={fit}"]), 0,
                             "the fixture's four rows match its four-corner verdict")
            # Truncate to one corner and the same fit must now be refused.
            lines = (fit / "timing.tsv").read_text().splitlines(keepends=True)
            (fit / "timing.tsv").write_text("".join(lines[:4]))
            self.assertEqual(record.main(["--record", str(root / "r2"), f"--fit=x={fit}"]), 1,
                             "one corner of rows against a four-corner verdict must be refused")

    def test_a_corner_missing_a_check_is_refused(self):
        # The other direction of truncation: all four corners present, but one
        # of them has lost its hold row.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            lines = (fit / "timing.tsv").read_text().splitlines(keepends=True)
            kept = [l for l in lines if not (l.startswith("Corner 2\t") and "\thold\t" in l)]
            (fit / "timing.tsv").write_text("".join(kept))
            self.assertEqual(record.main(["--record", str(root / "r"), f"--fit=x={fit}"]), 1)

    def test_a_uniformly_truncated_table_is_refused(self):
        # Every corner consistent with every other, and still missing what the
        # verdict covers: fit.json is the authority, not the table's own
        # internal agreement.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            record_json = json.loads((fit / "fit.json").read_text())
            for entry in record_json["timing"]["corners"]:
                entry["analyses"]["recovery"] = {"coverage": "analyzed"}
            (fit / "fit.json").write_text(json.dumps(record_json))
            # The table still has setup/hold/minimum_pulse_width at all four.
            self.assertEqual(record.main(["--record", str(root / "r"), f"--fit=x={fit}"]), 1)

    def test_a_table_without_the_closure_checks_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fit = write_fit(root / "f", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            (fit / "timing.tsv").write_text(
                "corner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto\n"
                + "".join(f"Corner {i}\t0\trecovery\tanalyzed\t1\t1.0\tx\ty\n"
                          for i in range(4)))
            self.assertEqual(record.main(["--record", str(root / "r"), f"--fit=x={fit}"]), 1)

    def test_a_fit_argument_without_a_name_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            self.assertEqual(record.main(["--record", temporary + "/r", "--fit=/some/dir"]), 1)
            self.assertEqual(record.main(["--record", temporary + "/r", "--fit==/some/dir"]), 1)

    def test_a_repeated_fit_name_is_refused(self):
        # Two identically named entries would be indistinguishable in
        # quartus.json and timing.tsv, and quartus-inputs.json is keyed by name
        # so it would silently keep only the last.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            one = write_fit(root / "1", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=1, available=2, device_alms=46800, ram_block="M20K")
            two = write_fit(root / "2", device="A5EB013BB23BE4SCS", family="Agilex 5",
                            alms=3, available=4, device_alms=46800, ram_block="M20K")
            self.assertEqual(record.main(["--record", str(root / "r"),
                                          f"--fit=same={one}", f"--fit=same={two}"]), 1)


if __name__ == "__main__":
    unittest.main()
