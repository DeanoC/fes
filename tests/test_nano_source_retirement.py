"""Sanitized retirement models use active controllers and injected hardware."""
import json
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import sys

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"
FIXTURE = FW / "tests/sdm_retire"


class SourceRetirementTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.output = Path(cls.tmp.name)
        from test_nano_sdm_input_profile import profile as input_profile, mode as input_mode
        selected_header = input_mode.header(input_profile())
        (cls.output / "reader_bound_profile.h").write_text(''.join(
            row + '\n' for row in selected_header.splitlines()
            if row.startswith(("#define FES_NANO_RETAINED_READER_ROUTE ", "#define FES_NANO_READER_ROUTE_SHA "))))
        sys.path.insert(0, str(ROOT / "platform/embedded/nano"))
        import usb_readiness
        project = cls.output / "project"
        handoff = project / "board/hps/project/output_files/hps_bootloader_handoff.bin"
        handoff.parent.mkdir(parents=True)
        data = (ROOT / "tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin").read_bytes()
        handoff.write_bytes(data)
        (project / "board/hps/build.json").write_text(json.dumps({"generated_handoff_and_sdc_sha256": {
            "output_files/hps_bootloader_handoff.bin": hashlib.sha256(data).hexdigest()}}))
        (cls.output / "usb_board_policy.h").write_text(usb_readiness.header(project, {"hps_io_hash": "d" * 64}))
        # The input-enabled vendor export installs this selected pair under
        # the vendor's generic setup names. Keep arm-only sources independent.
        for suffix in ("c", "h"):
            shutil.copyfile(FW / ("sdm_input_probe_setup." + suffix), cls.output / ("probe_setup." + suffix))
        shutil.copyfile(FW / "nano_sdm_source_platform.inc", cls.output / "nano_sdm_source_platform.inc")
        import importlib.util
        spec=importlib.util.spec_from_file_location('fes_recovery_extract',FW/'verify.py')
        extractor=importlib.util.module_from_spec(spec);spec.loader.exec_module(extractor)
        platform=(FW/'nano_bridge_platform.inc').read_text()
        (cls.output/'held_reset_body.inc').write_text(''.join(extractor.function(platform,name) for name in (
            'fes_nano_bridge_platform_lock','fes_nano_bridge_platform_unlock',
            'fes_nano_bridge_platform_invalidate_held','fes_nano_bridge_platform_invalidate',
            'fes_nano_reset_binding','fes_nano_bridge_platform_cold_reset_held')))
        # Compile the real readiness caller, not only configuration_after's
        # completion callback: that was the missing boundary in format 12.
        source = (FW / "nano_cold_platform.c").read_text()
        for signature, filename in (("int fes_nano_platform_window_rearm(uint64_t owner,uint64_t generation)", "window_rearm_test.inc"),
                                    ("static int configuration_return_generation(uint64_t owner,uint64_t generation)", "configuration_return_test.inc"),
                                    ("static int identity_configured(void *context)", "identity_configured_test.inc"),
                                    ("static int window_gate(void)", "window_gate_test.inc"),
                                    ("int fes_nano_platform_window_return(uint64_t *generation)", "window_return_test.inc"),
                                    ("static int window_sd_return(void *context,const struct fes_nano_bridges *b,uint64_t *generation)", "window_sd_return_test.inc")):
            start = source.index(signature)
            at = source.index("{", start)
            depth, end = 1, at + 1
            while depth:
                depth += (source[end] == "{") - (source[end] == "}")
                end += 1
            (cls.output / filename).write_text(source[start:end])

        source = (FW / "sdm_input_vendor_service.c").read_text()
        start = source.index("int fes_nano_sdm_vendor_source_idle(")
        at = source.index("{", start)
        depth, end = 1, at + 1
        while depth:
            depth += (source[end] == "{") - (source[end] == "}")
            end += 1
        (cls.output / "vendor_source_idle_test.inc").write_text(source[start:end])


    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def run_model(self, name, joined=False, arguments=()):
        binary = self.output / name
        sources = [FIXTURE / (name + ".c"), FW / "source_retire.c", FW / "command_queue.c", FW / "input_tables.c"]
        if name in ("warm_probe_test", "setup_validation_test", "setup_batch_cost_test"):
            sources += [self.output / "probe_setup.c"]
        if name == "setup_word_cost_test":
            measured = self.output / "expected_words_probe.c"
            measured.write_text('#define nano_sdm_input_words fixture_nano_sdm_input_words\n'
                                '#include "probe_setup.c"\n')
            sources += [measured]
        if name == "retained_window_gate_test":
            sources += [FW / "nano_smmu.c", FW / "nano_sdm_reader.c"]
        if joined:
            sources += [self.output / "probe_setup.c", FW / "input_transfer.c", FW / "nano_smmu.c", FW / "nano_sdm_reader.c"]
            sources += [FW / (n + ".c") for n in ("nano_cold", "nano_hps_io", "nano_bridges", "nano_boot", "nano_sd", "nano_sd_platform", "nano_usb_platform", "nano_usb_board", "nano_mac", "nano_usb_service", "nano_usb_composition", "nano_configuration")]
        command = [shutil.which("cc") or "cc", "-std=c11", "-O1", "-g", "-Wall", "-Wextra", "-Werror",
                   "-Wno-misleading-indentation", "-Wno-unused-function", "-Wno-unused-variable", "-Wno-sign-compare",
                   "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-DFES_NANO_RETIRE_BATCHED_OBSERVE=1"]
        if joined:
            command += ["-DFES_NANO_RETIRE_TIMING=1", "-DFES_NANO_RETIRE_QUEUE_OBSERVE=1", "-DFES_NANO_SDM_RETIRE_SOURCE=1"]
        command += ["-I" + str(p) for p in (self.output, FW, FW / "tests")]
        if not binary.exists():
            compiled = subprocess.run(command + [str(p) for p in sources] + ["-o", str(binary)], timeout=90, capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
        result = subprocess.run([str(binary), *map(str, arguments)], timeout=180, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout.splitlines()[-1])
        if name == "queue_wire_test":
            sys.path.insert(0, str(ROOT / "platform/embedded/nano/sdm_trace"))
            from queue_observation import parse_queue
            line = next(v for v in result.stdout.splitlines() if v.startswith("FES_NANO_RETIRE_QUEUE "))
            queue = parse_queue(line.encode())
            self.assertEqual((queue["stage"], queue["consumer"], queue["consumer_phase"], queue["consumer_stage"]), (10, 12, 6, 10))
            self.assertEqual((queue["slots"], queue["published"], queue["failed"], queue["error"]), (6, 1, 0, 0))
            self.assertEqual((queue["retained"], queue["reuse"], queue["activated"]), (1, 0, 0))
            self.assertGreater(queue["samples"], 0)
        expected = "passed-startup-then-owned-full-transfer" if name == "joined_test" else "passed"
        self.assertEqual(report["status"], expected)
        self.assertFalse(report["hardware_actions"])
        return report

    def test_repeated_queue_generations_wrap_and_pin_callback_ownership(self):
        report = self.run_model("queue_generation_test")
        self.assertEqual(report["completed_generations"], 384)
        self.assertGreater(report["wraps"], 0)

    def test_retirement_uses_live_setup_cursor_across_generations(self):
        report = self.run_model("retire_generation_test")
        self.assertEqual(report["completed_generations"], 256)
        self.assertGreater(report["wraps"], 0)
        self.assertEqual(report["negative_cases"], 136)

    def test_real_source_release_supplier_requires_returned_owned_source(self):
        report = self.run_model("source_release_test", joined=True)
        self.assertEqual(report["cases"], 66)
        self.assertTrue(report["actual_source_release_supplier"])

    def test_terminal_cold_recovery_uses_live_revoked_scope(self):
        report=self.run_model('terminal_recovery_test',joined=True)
        self.assertEqual(report['cases'],33)
        self.assertTrue(report['actual_terminal_supplier'])

    def test_terminal_warm_recovery_preserves_previous_source(self):
        report=self.run_model('terminal_warm_recovery_test',joined=True)
        self.assertEqual(report['cases'],2)
        self.assertTrue(report['actual_warm_terminal_supplier'])

    def test_real_service_and_supplier_rearm_between_menu_and_computer(self):
        report = self.run_model("warm_service_test", joined=True)
        self.assertEqual(report["completed_generations"], 12)
        self.assertEqual(report["negative_cases"], 40)

    def test_start_negotiation_rejects_foreign_and_late_data_headers(self):
        report = self.run_model("start_header_test", joined=True)
        self.assertEqual(report["cases"], 9)

    def test_setup_failure_reports_retained_cause_without_live_callbacks(self):
        report = self.run_model("setup_failure_test", joined=True)
        self.assertEqual(report["cases"], 8)
        self.assertEqual(report["eight_write_failures"], 3)
        self.assertTrue(report["callbacks_poisoned"])

    def test_warm_expected_table_generation_uses_bounded_chunks_and_keeps_parent_deadline(self):
        rows = {cost: self.run_model("setup_word_cost_test", arguments=(cost,))
                for cost in (0, 80, 140, 256, 1024, 4096)}
        for cost in (0, 80, 140, 256, 1024):
            row = rows[cost]
            self.assertEqual((row["prepared"], row["error"], row["calls"], row["words"], row["largest"]),
                             (1, 0, 480, 15360, 32))
            self.assertTrue(row["previous_unchanged"])
            self.assertLessEqual(row["elapsed_us"], 1000000)
        self.assertEqual((rows[4096]["prepared"], rows[4096]["error"], rows[4096]["register_writes"]),
                         (0, -110, 0))
        self.expected_word_cost_observations = rows

    def test_private_table_batches_cover_all_words_with_finite_cold_and_warm_cost(self):
        report = self.run_model("setup_batch_cost_test")
        self.assertEqual(report["cases"], 12)
        self.assertEqual(report["max_words"], 128)

    def test_preparation_completion_and_final_checks_keep_separate_finite_bounds(self):
        report = self.run_model("setup_validation_test")
        self.assertEqual(report["cases"], 43)

    def test_warm_probe_reuses_owned_queue_without_clearing_abort(self):
        report = self.run_model("warm_probe_test")
        self.assertEqual(report["completed_generations"], 64)

    def test_retained_continuation_requires_live_retired_state(self):
        self.run_model("continue_test")

    def test_sealed_secure_reader_handoff_and_live_failures(self):
        self.run_model("reader_route_test", joined=True)

    def test_reader_gate_stays_closed_after_retained_source_abort(self):
        self.run_model("retained_window_gate_test")

    def test_retained_configuration_control_reply_and_native_readiness(self):
        self.run_model("control_return_test", joined=True)

    def test_native_configuration_checks_still_gate_continuation(self):
        self.run_model("continue_return_test", joined=True)

    def test_portable_failures(self):
        self.run_model("retire_test")

    def test_fresh_prewrite_gates(self):
        self.run_model("write_gate_test")

    def test_joined_launch_retirement_failures(self):
        self.run_model("joined_test", joined=True)

    def test_counter_boundaries(self):
        self.run_model("joined_boundary_test", joined=True)

    def test_batch_callback_and_table_faults(self):
        self.run_model("batch_faults", joined=True)

    def test_command_snapshot_callback_faults(self):
        self.run_model("command_snapshot_test")

    def test_command_progress_callback_and_pointer_failures(self):
        self.run_model("command_progress_test")

    def test_retirement_poll_supplier_faults(self):
        self.run_model("retire_progress_faults")

    def test_transition_batches_reject_faults_before_the_next_write(self):
        report = self.run_model("transition_batch_test")
        self.assertEqual(report["cases"], 884)
        self.assertEqual((report["phases"], report["read_positions"], report["fault_modes"]), (4, 22, 10))

    def test_raw_observations_survive_rejected_guards_without_authority(self):
        self.run_model("retire_observation_test")

    def test_actual_finish_queue_wire_record(self):
        self.run_model("queue_wire_test", joined=True)

    def test_command_completion_and_final_validation_policies(self):
        self.run_model("command_validation_test")

    def test_retirement_staging_publication_completion_and_final_budgets(self):
        result = self.run_model("command_publication_test")
        self.assertEqual(result["cases"], 42)

    def test_joined_retirement_publication_cost_and_expiry(self):
        rows = {}
        for cost in (0, 128, 224, 256, 288, 320, 384, 1000):
            rows[cost] = self.run_model("retire_publication_cost_test", joined=True, arguments=(cost, 0))
        for cost in (0, 128, 224, 256, 288):
            self.assertEqual((rows[cost]["retired"], rows[cost]["continued"],
                              rows[cost]["failed"], rows[cost]["first_error"]), (1, 1, 0, 0))
        self.assertFalse(rows[1000]["continued"])
        clocks = {cost: self.run_model("retire_publication_cost_test", joined=True, arguments=(0, cost))
                  for cost in (1000, 100000)}
        self.assertTrue(clocks[1000]["continued"])
        self.assertFalse(clocks[100000]["continued"])
        self.publication_cost_observations = dict(read_cost_cases=rows, clock_cost_cases=clocks)

    def test_callback_substitution_stops_before_next_operation(self):
        self.run_model("command_callback_test")

    def test_retirement_keeps_its_original_enclosing_deadline(self):
        for delay in (0, 105704, 1000001, 4000001):
            result = self.run_model("retire_validation_delay_test", arguments=(delay,))
            self.assertEqual(bool(result["retired"]), delay < 4000000)
            self.assertEqual(result["writes"], 5 if delay < 4000000 else 4)
            self.assertEqual(result["first_error"], 0 if delay < 4000000 else -110)

    def test_command_snapshot_cost_and_unchanged_deadlines(self):
        baseline = json.loads((ROOT / "tests/fixtures/nano-sdm-input/retirement-cost-v6.json").read_text())
        snapshot_baseline = json.loads((ROOT / "tests/fixtures/nano-sdm-input/retirement-cost-v7.json").read_text())
        guard_baseline = json.loads((ROOT / "tests/fixtures/nano-sdm-input/retirement-cost-v8.json").read_text())
        rows = {}
        for cost in (0, 1, 2, 4, 8, 16, 32, 64, 72, 80, 96, 112, 128, 256, 1000):
            rows[cost] = self.run_model("retire_cost_test", joined=True, arguments=(cost, 0))
        zero = rows[0]
        self.assertTrue(zero["retired"])
        self.assertEqual(zero["writes"], 5)
        self.assertEqual(sum(r[2] for r in zero["phase_counts"]), 25600)
        self.assertLess(zero["phase_counts"][4][0], baseline["phase_counts"][4][0])
        self.assertLess(zero["phase_counts"][4][0], snapshot_baseline["phase_counts"][4][0])
        self.assertLess(zero["phase_counts"][4][5], snapshot_baseline["phase_counts"][4][5])
        self.assertLess(zero["phase_counts"][4][0], guard_baseline["phase_counts"][4][0])
        self.assertLess(zero["phase_counts"][4][5], guard_baseline["phase_counts"][4][5])
        # Phase 6 previously included the fixture's post-terminal retry probe.
        # A native return clears issued/finishes configuration before that
        # probe, so compare earlier phases and the full final table scan.
        self.assertEqual(zero["phase_counts"][6][2], 5120)
        for phase in (0,):
            self.assertEqual(zero["phase_counts"][phase], snapshot_baseline["phase_counts"][phase])
            self.assertEqual(zero["phase_counts"][phase], guard_baseline["phase_counts"][phase])
        for phase in (1, 2, 3, 5):
            current, previous = zero["phase_counts"][phase], guard_baseline["phase_counts"][phase]
            # All control writes, table reads and command stores remain.
            # Each register keeps its ordering barriers. Fresh ownership
            # brackets one bounded poll instead of three nested guard pairs.
            self.assertEqual(current[1:4], previous[1:4])
            self.assertLess(current[0], previous[0])
            self.assertLess(current[5], previous[5])
        self.assertFalse(baseline["eight_us_read_cost_retired"])
        self.assertTrue(rows[8]["retired"])
        self.assertFalse(rows[1000]["retired"])
        finite_clock = self.run_model("retire_cost_test", joined=True, arguments=(0, 1000))
        self.assertTrue(finite_clock["retired"])
        slow_clock = self.run_model("retire_cost_test", joined=True, arguments=(0, 100000))
        self.assertFalse(slow_clock["retired"])
        self.cost_observations = {"read_cost_cases": rows, "slow_clock": slow_clock}
