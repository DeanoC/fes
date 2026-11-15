from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

from scripts import build_fes_zx81_quartus as producer
from scripts import quartus_vo_evidence
from scripts.fes_build_common import BuildError
from scripts.fes_de10nano_evidence import HPS_DDR_CFG_PORTS, hps_ddr_layout
from tests.quartus_timing_fixture import MISS_REPORT, PASSING_REPORT

ROOT = Path(__file__).resolve().parents[1]
BUILD_ID = "0123456789abcdef0123456789abcdef"


def clocks_table(system_period="19.144", audio_period="81.364", pixel_period="13.464") -> str:
    rows = [("FPGA_CLK1_50", "Base", "20.000"),
            (producer.CLOCKS["system"][0], "Generated", system_period),
            (producer.CLOCKS["audio"][0], "Generated", audio_period),
            (producer.CLOCKS["pixel"][0], "Generated", pixel_period)]
    rule = "+------------+-----------+--------+\n"
    return ("; Clocks ;\n" + rule + "; Clock Name ; Type ; Period ;\n" + rule
            + "".join(f"; {name} ; {kind} ; {period} ;\n" for name, kind, period in rows) + rule)


def pll_report(system="52.224", audio="12.288", pixel="74.249999") -> str:
    out = ""
    for label, generated in (("system", system), ("audio", audio), ("pixel", pixel)):
        out += f";         -- {producer.CLOCKS[label][1]} ;    ;\n"
        out += f";             -- Output Clock Frequency          ; {generated} MHz ;\n"
    return out


def fit_report(model="zx81", *, mlab="0", dsp="3", lane_location=None, lane_mif=None, extra_ram=None,
               pin_standard="3.3-V LVTTL") -> str:
    machine = producer.model_for(model)
    rule = "+------+------+-------------+-----+----------+\n"
    rams = []
    for lane, location in enumerate(producer.lane_locations(machine)):
        if lane == 0 and lane_location:
            location = lane_location
        mif = lane_mif if lane == 0 and lane_mif else f"zx81_rom_lane{lane}.mif"
        rams.append((f"zx81_machine:machine|zx81_rom_link:rom|zx81_rom_lane_quartus:lane{lane}|altsyncram:rom|ALTSYNCRAM",
                     "ROM", "1", mif, location))
    rams.append(("zx81_machine:machine|zx81_dpram:save_ram|altsyncram:a|ALTSYNCRAM", "True Dual Port", "2",
                 "None", extra_ram or "M10K_X41_Y76_N0, M10K_X55_Y73_N0"))
    ram_table = ("; Fitter RAM Summary ;\n" + rule + "; Name ; Mode ; M10K blocks ; MIF ; Location ;\n" + rule
                 + "".join(f"; {n} ; {m} ; {b} ; {f} ; {loc} ;\n" for n, m, b, f, loc in rams) + rule)
    pin_rule = "+----------+------------+----------+----------+--------+-------------+\n"
    pins = ("; All Package Pins ;\n" + pin_rule
            + "; Location ; Pad Number ; I/O Bank ; Pin Name/Usage ; Dir. ; I/O Standard ;\n" + pin_rule
            + "".join(f"; {location} ; 1 ; 3B ; {name} ; output ; {pin_standard} ;\n"
                      for name, location in producer.AUDIO_PINS.items()) + pin_rule)
    return (f"; Total MLAB memory bits                                      ; {mlab}                     ;       ;\n"
            f"; Total DSP Blocks                                            ; {dsp} / 112               ; 3 %   ;\n"
            + ram_table + "\n" + pins + "\n" + pll_report())


def vo_netlist(**overrides) -> str:
    layout = hps_ddr_layout(ROOT)
    ports = {}
    for port, (_, width) in HPS_DDR_CFG_PORTS.items():
        bits = [("vcc" if layout[port] >> index & 1 else "gnd") for index in reversed(range(width))]
        ports[port] = "{" + ",".join(bits) + "}"
    ports.update({f"cmd_valid_{n}": "gnd" for n in range(1, 6)})
    ports.update({f"wr_valid_{n}": "gnd" for n in range(4)})
    ports["cmd_valid_0"] = "\\memory|port0|m_read~0_combout "
    ports["cmd_data_0"] = "{gnd,\\memory|port0|a~1_combout ,gnd,\\memory|port0|m_read~0_combout }"
    ports.update(overrides)
    body = ",\n".join(f"\t.{port}({value})" for port, value in ports.items())
    return f"// post-fit\nmodule top();\ncyclonev_hps_interface_fpga2sdram \\memory|f2sdram (\n{body});\nendmodule\n"


class ModelTests(unittest.TestCase):
    def test_models_match_the_oss_producer(self) -> None:
        from scripts import build_fes_zx81_oss as oss
        self.assertEqual(set(producer.MODELS), set(oss.MODELS))
        for key, model in producer.MODELS.items():
            other = oss.MODELS[key]
            for field in ("core_id", "name", "description", "version", "define", "rom_id", "rom_lanes",
                          "rom_bytes", "lane_rows"):
                self.assertEqual(getattr(model, field), getattr(other, field), (key, field))

    def test_unknown_model_is_refused(self) -> None:
        with self.assertRaisesRegex(BuildError, "model must be one of"):
            producer.model_for("zx82")


class ProjectTests(unittest.TestCase):
    def qsf(self, model="zx81", seed=1) -> str:
        return producer.project_qsf(ROOT, ROOT / "build/test-project", BUILD_ID, seed, producer.model_for(model))

    def test_project_selects_the_quartus_rom_link_and_no_socket(self) -> None:
        text = self.qsf()
        self.assertIn(producer.ROM_LINK, text)
        self.assertNotIn("cores/fes-zx81/rtl/zx81_rom_link.v", text)
        self.assertNotIn("zx81_expansion_socket", text)
        self.assertIn("set_parameter -name EXPANSION_SOCKET 0", text)
        self.assertIn(f"FES_ZX81_BUILD_ID=128'h{BUILD_ID}", text)
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', text)
        self.assertIn('VERILOG_MACRO "FES_ZX81_ROM_LINK=1"', text)
        self.assertIn("SEED 1\n", text)
        self.assertIn(producer.I2C_SITE, text)

    def test_each_model_pins_its_rom_lanes(self) -> None:
        for key, model in producer.MODELS.items():
            text = self.qsf(key)
            for lane in range(8):
                assignment = (f"set_location_assignment M10K_X5_Y{73 + lane}_N0 -to "
                              f'"*|zx81_rom_link:rom|zx81_rom_lane_quartus:lane{lane}|*"')
                self.assertEqual(assignment in text, lane < model.rom_lanes, (key, lane))
            if model.define:
                self.assertIn(f'VERILOG_MACRO "{model.define}=1"', text)
            else:
                self.assertNotIn("ZX81_MODEL_", text)

    def test_only_declared_seeds_and_valid_build_ids(self) -> None:
        with self.assertRaisesRegex(BuildError, "not a declared sign-off seed"):
            self.qsf(seed=4)
        with self.assertRaisesRegex(BuildError, "32 lowercase hexadecimal"):
            producer.project_qsf(ROOT, ROOT / "build/x", "XYZ", 1, producer.model_for("zx81"))

    def test_quartus_rom_link_keeps_the_lane_interface(self) -> None:
        text = (ROOT / producer.ROM_LINK).read_text()
        self.assertIn("module zx81_rom_link (", text)
        self.assertIn("`ifdef ZX81_ROM_4K", text)
        for lane in range(8):
            self.assertIn(f'.INIT_FILE("zx81_rom_lane{lane}.mif")) lane{lane}', text)
        self.assertNotIn("MISTRAL_", text)

    def test_blank_and_pattern_mifs(self) -> None:
        self.assertIn("[0..3FF] : 000;", producer.blank_mif())
        data = producer.pattern_bytes(8192)
        self.assertEqual(len(data), 8192)
        self.assertEqual(data, producer.pattern_bytes(8192))
        self.assertGreater(len(set(data)), 200)
        mif = producer.pattern_mif(data[:1024])
        self.assertIn(f"3FF : {data[1023]:03X};", mif)


class EvidenceTests(unittest.TestCase):
    def sta(self, **periods) -> str:
        return PASSING_REPORT + "\n" + clocks_table(**periods)

    def test_clock_evidence_accepts_the_measured_fit(self) -> None:
        evidence = producer.clock_evidence(self.sta(), fit_report())
        self.assertEqual(evidence["system"]["generated_mhz"], 52.224)
        self.assertEqual(evidence["pixel"]["analysed_period_ns"], 13.464)

    def test_clock_analysed_slower_than_required_fails(self) -> None:
        with self.assertRaisesRegex(BuildError, "analyses the system clock at 19.2"):
            producer.clock_evidence(self.sta(system_period="19.2"), fit_report())

    def test_pll_frequency_mismatch_fails(self) -> None:
        fit = fit_report().replace("; 12.288 MHz", "; 12.29 MHz")
        with self.assertRaisesRegex(BuildError, "audio PLL output is 12.29 MHz"):
            producer.clock_evidence(self.sta(), fit)

    def test_timing_miss_fails_the_gate(self) -> None:
        with self.assertRaises(BuildError):
            producer.quartus_timing_summary.validate_timing_report(MISS_REPORT, BuildError)

    def test_fit_evidence_accepts_pinned_blank_lanes(self) -> None:
        for key in producer.MODELS:
            evidence = producer.fit_evidence(fit_report(key), producer.model_for(key))
            self.assertEqual(len(evidence["rom_lanes"]), producer.model_for(key).rom_lanes)

    def test_fit_evidence_refusals(self) -> None:
        machine = producer.model_for("zx81")
        cases = {
            "forbids MLAB": fit_report(mlab="640"),
            "display-plane DSP": fit_report(dsp="4"),
            "ROM lane 0 must be": fit_report(lane_location="M10K_X6_Y73_N0"),
            "ROM lane 0 must be one blank": fit_report(lane_mif="pattern.mif"),
            "shares a ROM lane block": fit_report(extra_ram="M10K_X5_Y80_N0"),
            "audio output HDMI_MCLK": fit_report(pin_standard="2.5 V"),
        }
        for message, report in cases.items():
            with self.subTest(message), self.assertRaisesRegex(BuildError, message):
                producer.fit_evidence(report, machine)

    def test_zx80_builds_only_four_lanes(self) -> None:
        report = fit_report("zx81")
        with self.assertRaisesRegex(BuildError, "more ROM lanes than it declares"):
            producer.fit_evidence(report, producer.model_for("zx80"))


class NetlistTests(unittest.TestCase):
    def test_post_fit_layout_and_session_display(self) -> None:
        evidence = quartus_vo_evidence.session_display_evidence(vo_netlist(), ROOT)
        layout = hps_ddr_layout(ROOT)
        self.assertEqual(evidence["cfg"], {port: f"0x{value:x}" for port, value in layout.items()})
        self.assertTrue(evidence["read_only"])

    def test_bit_order_is_least_significant_first(self) -> None:
        _, connections = quartus_vo_evidence.instance_connections(vo_netlist())
        self.assertEqual(connections["cmd_data_0"][0], "\\memory|port0|m_read~0_combout")
        self.assertEqual(connections["cmd_data_0"][1], "gnd")

    def test_layout_mismatch_fails(self) -> None:
        port, (_, width) = next(iter(HPS_DDR_CFG_PORTS.items()))
        netlist = vo_netlist(**{port: "{" + ",".join(["vcc"] * width) + "}"})
        with self.assertRaisesRegex(BuildError, f"fpga2sdram {port} is 0x"):
            quartus_vo_evidence.session_display_evidence(netlist, ROOT)

    def test_a_net_never_passes_for_a_constant(self) -> None:
        for port, value in (("cmd_valid_1", "\\memory|port1|go~q "),
                            ("cmd_data_0", "{gnd,gnd,\\x~q ,\\memory|port0|m_read~0_combout }"),
                            ("cfg_axi_mm_select", "{gnd,gnd,gnd,gnd,gnd,\\x~q }")):
            with self.subTest(port), self.assertRaises(BuildError):
                quartus_vo_evidence.session_display_evidence(vo_netlist(**{port: value}), ROOT)

    def test_exactly_one_instance(self) -> None:
        netlist = vo_netlist()
        with self.assertRaisesRegex(BuildError, "exactly one"):
            quartus_vo_evidence.instance_connections(netlist + netlist.replace("\\memory|f2sdram", "\\other"))
        with self.assertRaisesRegex(BuildError, "exactly one"):
            quartus_vo_evidence.instance_connections("module top(); endmodule")


class JobTests(unittest.TestCase):
    def test_step_failure_and_time_cap(self) -> None:
        import tempfile
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            producer._run(["/bin/true"], work, {"PATH": "/usr/bin:/bin"}, work / "ok.log")
            with self.assertRaisesRegex(BuildError, "false failed \\(exit 1\\)"):
                producer._run(["/bin/false"], work, {"PATH": "/usr/bin:/bin"}, work / "fail.log")
            with patch.object(producer, "HEARTBEAT_S", 0.05), patch.object(producer, "COMPILE_TIMEOUT_S", 0.1), \
                    patch("sys.stdout"):
                with self.assertRaisesRegex(BuildError, "sleep exceeded"):
                    producer._run(["/bin/sleep", "5"], work, {"PATH": "/usr/bin:/bin"}, work / "slow.log")

    def test_time_cap_kills_the_step_children(self) -> None:
        import os
        import tempfile
        import time
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            child = work / "child.pid"
            script = f"sleep 30 & echo $! > {child}; wait"
            with patch.object(producer, "HEARTBEAT_S", 0.1), patch.object(producer, "COMPILE_TIMEOUT_S", 0.5), \
                    patch("sys.stdout"):
                with self.assertRaisesRegex(BuildError, "bash exceeded"):
                    producer._run(["/bin/bash", "-c", script], work, {"PATH": "/usr/bin:/bin"}, work / "c.log")
            pid = int(child.read_text())
            for _ in range(50):
                if not _running(pid):
                    break
                time.sleep(0.05)
            else:
                os.kill(pid, 9)
                self.fail("the step's child outlived the time cap")

    def test_sigterm_kills_the_step_children(self) -> None:
        import os
        import signal
        import subprocess
        import tempfile
        import time
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            child = work / "child.pid"
            program = (
                "import sys; sys.path.insert(0, '.'); from pathlib import Path\n"
                "from scripts import build_fes_zx81_quartus as p\n"
                "p.install_termination_handler()\n"
                "try:\n"
                f"    p._run(['/bin/bash', '-c', 'sleep 30 & echo $! > {child}; wait'], Path({str(work)!r}),\n"
                "           {'PATH': '/usr/bin:/bin'}, Path(" + repr(str(work / "t.log")) + "))\n"
                "except p.Terminated as stop:\n"
                "    sys.exit(128 + stop.signum)\n")
            producer_process = subprocess.Popen([sys.executable, "-I", "-c", program], cwd=ROOT,
                                                stdout=subprocess.DEVNULL)
            for _ in range(100):
                if child.exists() and child.read_text().strip():
                    break
                time.sleep(0.05)
            pid = int(child.read_text())
            producer_process.send_signal(signal.SIGTERM)
            self.assertEqual(producer_process.wait(timeout=10), 128 + signal.SIGTERM)
            for _ in range(50):
                if not _running(pid):
                    break
                time.sleep(0.05)
            else:
                os.kill(pid, 9)
                self.fail("the step's child outlived SIGTERM to the producer")

    def test_interrupt_reports_failed(self) -> None:
        from unittest.mock import patch
        import io
        with patch.object(producer, "build", side_effect=KeyboardInterrupt), \
                patch("sys.stderr", new=io.StringIO()) as stderr, \
                patch.object(producer.signal, "signal"):
            self.assertEqual(producer.main(["--model", "zx80"]), 130)
        self.assertIn("build-fes-zx81-quartus: FAILED zx80: interrupted", stderr.getvalue())

    def test_disk_preflight(self) -> None:
        from unittest.mock import patch
        import collections
        usage = collections.namedtuple("usage", "total used free")
        with patch.object(producer.shutil, "disk_usage", return_value=usage(1, 0, 29 * 10**9)):
            with self.assertRaisesRegex(BuildError, "need 30 GB"):
                producer.require_disk(ROOT)


class RecordTests(unittest.TestCase):
    def test_source_roots_cover_every_loaded_producer_module(self) -> None:
        loaded = sorted("scripts/" + name.split(".", 1)[1].replace(".", "/") + ".py"
                        for name in _producer_modules() if name != "scripts")
        self.assertEqual([path for path in loaded if path not in producer.SOURCE_ROOTS], [])
        for path in producer.SOURCE_ROOTS:
            self.assertTrue((ROOT / path).exists(), path)

    def test_record_requires_quartus_execution(self) -> None:
        with self.assertRaisesRegex(BuildError, "controlled functional execution"):
            producer.create_build_record(ROOT, "x", "0" * 40, {}, execution={"toolchain": "oss"})

    def test_manifest_matches_the_oss_interfaces_without_the_socket(self) -> None:
        record = json.dumps({"repository": "https://github.com/DeanoC/fes", "revision": "0" * 40,
                             "recipe_sha256": "0" * 64, "tools": {"quartus": "q"},
                             "parameters": {"core_id": "fes.zx80", "model": "zx80"}}).encode()
        payload = ROOT / producer.ROM_LINK
        from scripts.core_package import _decode_manifest
        rom = {"id": "zx80-rom", "role": "firmware", "source_size": 4096, "file": "rom-map.json",
               "size": 1, "sha256": "0" * 64}
        try:
            fields = _decode_manifest(producer.manifest(record, payload, rom, producer.model_for("zx80")), payload.read_bytes())
        except Exception as error:  # noqa: BLE001 - schema details belong to core_package
            self.fail(f"manifest rejected: {error}")
        interfaces = [item["id"] for item in fields["interfaces"]]
        self.assertNotIn("fes.media.zx81-tape-write", interfaces)
        self.assertNotIn("fes.expansion.zx81-bus", interfaces)
        self.assertEqual(fields["rom"]["id"], "zx80-rom")


def _running(pid: int) -> bool:
    """True while pid exists and is not a zombie: a killed orphan may wait for
    PID 1 to reap it, and os.kill(pid, 0) still succeeds on a zombie."""
    try:
        stat = Path(f"/proc/{pid}/stat").read_text()
    except (FileNotFoundError, ProcessLookupError):
        # Reaped before or while the entry was read (read() can raise ESRCH).
        return False
    return stat[stat.rindex(")") + 2] != "Z"


def _producer_modules() -> set[str]:
    import subprocess
    program = ("import sys; sys.path.insert(0, '.'); import scripts.build_fes_zx81_quartus; "
               "print('\\n'.join(n for n in sys.modules if n.startswith('scripts.')))")
    return set(subprocess.check_output([sys.executable, "-I", "-c", program], cwd=ROOT, text=True).split())


if __name__ == "__main__":
    unittest.main()
