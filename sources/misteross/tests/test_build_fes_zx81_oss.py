from __future__ import annotations

import subprocess
import json
import os
import shutil
import tempfile
import tomllib
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_zx81_oss, toolchain_cache
from scripts.build_fes_zx81_oss import (
    OUTPUT_RELATIVE,
    PLACER_SEEDS,
    PLACER_TIMING_WEIGHT,
    PLACER_CRITICALITY_EXPONENT,
    RTL_SOURCES,
    BuildError,
    _manifest,
    build_commands,
)
from tests.test_build_fes_pong import publish_shared_toolchain


ROOT = Path(__file__).resolve().parents[1]


class BuildFesZx81OssTests(unittest.TestCase):
    def test_nmos_cpu_sources_enter_functional_record(self):
        from scripts.export_core_package import build_identity
        name = 'cores/fes-common/rtl/z80/fes_z80_nmos.sv'
        self.assertIn(name, build_fes_zx81_oss.PINNED_INPUTS)
        record = build_fes_zx81_oss.create_build_record(ROOT, 'https://example.invalid/fes', 'a'*40,
                                                       {'yosys': 'x'}, execution=EXECUTION)
        path = ROOT / name
        original = path.read_bytes()
        try:
            path.write_bytes(original+b'\n// changed functional input\n')
            changed = build_fes_zx81_oss.create_build_record(ROOT, 'https://example.invalid/fes', 'a'*40,
                                                            {'yosys': 'x'}, execution=EXECUTION)
            self.assertNotEqual(build_identity(record), build_identity(changed))
        finally:
            path.write_bytes(original)
        program = build_commands(ROOT, ROOT/OUTPUT_RELATIVE, '0'*32,
            {'yosys': Path('/auth/yosys'), 'nextpnr-mistral': Path('/auth/nextpnr')})[0][-1]
        self.assertIn('fes_z80_nmos.sv', program)
        self.assertNotIn('tv80_core.v', program)

    def test_shared_audio_sources_replace_local_serializer(self):
        self.assertNotIn('cores/fes-common/rtl/fes_audio_pll.v', RTL_SOURCES)
        self.assertIn('cores/fes-common/rtl/fes_audio_output.v', RTL_SOURCES)
        self.assertIn('cores/fes-common/rtl/fes_audio_i2s.v', RTL_SOURCES)
        self.assertNotIn('cores/fes-zx81/rtl/zx81_hdmi_i2s.v', RTL_SOURCES)
        self.assertEqual(build_fes_zx81_oss.REQUIRED_RESOURCES['altera_pll'], 2)

    def test_build_record_declares_audio_clock(self):
        record = build_fes_zx81_oss.create_build_record(
            ROOT, 'https://github.com/DeanoC/misteross.git', 'a' * 40,
            {'yosys': 'x'}, execution=EXECUTION)
        self.assertEqual(json.loads(record)['parameters']['audio_clock_hz'], 12_288_000)
        self.assertEqual(json.loads(record)['parameters']['sys_clock_hz'], 52_224_000)

    def test_audio_clock_and_pad_evidence_rejects_unrouted_output(self):
        fmax = {'audio_clk': {'constraint': 12.288, 'achieved': 12.9}}
        self.assertEqual(build_fes_zx81_oss._audio_timing(fmax)[2], 12.9)
        fmax['audio_clk']['achieved'] = 12.0
        with self.assertRaisesRegex(BuildError, 'audio clock timing achieved'):
            build_fes_zx81_oss._audio_timing(fmax)
        ports = {}
        netnames = {'audio_clk': {'bits': [201]}, 'audio.locked': {'bits': [202]},
                    'audio.sclk': {'bits': [203]}, 'audio.lrclk': {'bits': [204]},
                    'audio.sdata': {'bits': [205]}, 'audio.lock_sync': {'bits': [206]}}
        cells = {'system_clock.pll': {'type': 'altera_pll', 'parameters': {
            'reference_clock_frequency': '50.0 MHz',
            'output_clock_frequency0': '52.224 MHz',
            'output_clock_frequency1': '12.288 MHz',
        }, 'connections': {'locked': [202]}},
            'audio_clock_buffer': {'type': 'MISTRAL_CLKBUF', 'connections': {'Q': [201]}},
            'audio.lock_sync_MISTRAL_FF_Q': {'type': 'MISTRAL_FF',
                                              'connections': {'Q': [206], 'CLK': [201], 'ACLR': [202]}},
            'audio.serializer.sample_tick': {'type': 'MISTRAL_FF',
                'attributes': {'src': 'cores/fes-common/rtl/fes_audio_i2s.v:23.5-41.8|dff_map.v:6.16-6.121'},
                'connections': {'CLK': [201], 'ACLR': [206]}},
        }
        sources = {'HDMI_MCLK': 201, 'HDMI_SCLK': 203,
                   'HDMI_LRCLK': 204, 'HDMI_I2S0': 205}
        for index, (port, pin) in enumerate({
            'HDMI_MCLK': 'PIN_U11', 'HDMI_SCLK': 'PIN_T12',
            'HDMI_LRCLK': 'PIN_T11', 'HDMI_I2S0': 'PIN_T13',
        }.items(), 1):
            ports[port] = {'direction': 'output', 'bits': [index]}
            cells[port] = {'type': 'MISTRAL_OB', 'connections': {'PAD': [index], 'I': [sources[port]]},
                           'attributes': {'LOC': pin, 'IO_STANDARD': '3.3-V LVTTL',
                                          'NEXTPNR_BEL': f'MISTRAL_IO.1.1.{index}'}}
        routed = {'modules': {'top': {'ports': ports, 'cells': cells, 'netnames': netnames}}}
        build_fes_zx81_oss._audio_evidence(routed)
        renamed = json.loads(json.dumps(routed))
        del renamed['modules']['top']['netnames']['audio.lrclk']
        renamed['modules']['top']['cells']['audio.lrclk_MISTRAL_FF_Q'] = {
            'type': 'MISTRAL_FF', 'connections': {'Q': [204]}}
        build_fes_zx81_oss._audio_evidence(renamed)
        renamed['modules']['top']['cells']['audio.lrclk_MISTRAL_FF_Q']['connections']['Q'] = [999]
        with self.assertRaisesRegex(BuildError, 'audio output HDMI_LRCLK has no routed audio.lrclk signal'):
            build_fes_zx81_oss._audio_evidence(renamed)
        # ABC can name the unreset CDC synchronizer after sample_tick. Its
        # inactive ACLR must not be mistaken for a missing serializer reset.
        cells['audio.serializer.sample_tick_MISTRAL_FF_Q_DATAIN_MISTRAL_FF_Q'] = {
            'type': 'MISTRAL_FF',
            'attributes': {'src': 'cores/fes-common/rtl/fes_audio_output.v:31.5-40.8'},
            'connections': {'CLK': [201], 'ACLR': []},
        }
        build_fes_zx81_oss._audio_evidence(routed)
        cells['HDMI_I2S0']['connections']['I'] = [999]
        with self.assertRaisesRegex(BuildError, 'HDMI_I2S0'):
            build_fes_zx81_oss._audio_evidence(routed)
        cells['HDMI_I2S0']['connections']['I'] = [205]
        cells['system_clock.pll']['connections']['locked'] = [999]
        with self.assertRaisesRegex(BuildError, 'lock'):
            build_fes_zx81_oss._audio_evidence(routed)
        cells['system_clock.pll']['connections']['locked'] = [202]
        cells['audio.serializer.sample_tick']['connections']['ACLR'] = ['1']
        with self.assertRaisesRegex(BuildError, 'serializer'):
            build_fes_zx81_oss._audio_evidence(routed)
        cells['audio.serializer.sample_tick']['connections']['ACLR'] = [206]
        serializer = cells.pop('audio.serializer.sample_tick')
        cells['renamed_serializer_ff'] = serializer
        serializer['connections']['CLK'] = [999]
        with self.assertRaisesRegex(BuildError, 'serializer'):
            build_fes_zx81_oss._audio_evidence(routed)
        serializer['connections']['CLK'] = [201]
        build_fes_zx81_oss._audio_evidence(routed)
        del cells['renamed_serializer_ff']
        with self.assertRaisesRegex(BuildError, 'serializer'):
            build_fes_zx81_oss._audio_evidence(routed)
        cells['renamed_serializer_ff'] = serializer
        cells['HDMI_I2S0']['attributes']['LOC'] = 'PIN_BAD'
        with self.assertRaisesRegex(BuildError, 'HDMI_I2S0'):
            build_fes_zx81_oss._audio_evidence(routed)

    def test_synthesized_audio_clock_comes_from_second_pll_output(self):
        pll = {'type': 'altera_pll', 'connections': {'outclk': [10, 11], 'locked': [12]}}
        buffer = {'type': 'MISTRAL_CLKBUF', 'connections': {'A': [11], 'Q': [13]}}
        top = {'cells': {'system_clock.pll': pll, 'audio_buffer': buffer},
               'netnames': {'audio_clk': {'bits': [13]}, 'audio.locked': {'bits': [12]}}}
        design = {'modules': {'top': top}}
        build_fes_zx81_oss._audio_synthesis_evidence(design)
        buffer['connections']['A'] = [99]
        with self.assertRaisesRegex(BuildError, 'second PLL output'):
            build_fes_zx81_oss._audio_synthesis_evidence(design)

    def test_signoff_rejects_failed_arc_even_with_normal_footer(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'synth.json').write_text('{"modules":{"top":{}}}')
            (output / 'routed.json').write_text('{"modules":{"top":{}}}')
            (output / 'nextpnr.log').write_text(
                "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
                "Info: PLL 'system_clock.pll': 50 MHz -> 52 MHz\n"
                "ERROR: Failed to route arc 6.0 of net 'cpu.NMI_n'\n"
                "Info: Program finished normally.\n"
            )
            with patch.object(build_fes_zx81_oss, '_i2c_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_synthesis_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_evidence'), patch.object(
                build_fes_zx81_oss, '_plane_dsp_evidence'), patch.object(
                build_fes_zx81_oss, '_cell_counts', return_value={
                    **build_fes_zx81_oss.REQUIRED_RESOURCES,
                    'MISTRAL_M10K': 1,
                }
            ):
                with self.assertRaisesRegex(BuildError, 'failed arc'):
                    build_fes_zx81_oss.validate_build_evidence(output)

    def test_signoff_rejects_a_blocked_plug_addr_exit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'synth.json').write_text('{"modules":{"top":{}}}')
            from tests.test_build_fes_zx81_expansion import legal_socket_design
            routed = legal_socket_design(routes={
                "video.vsync": "WIRE.24.2.COMBOUT[19].GIN.24.2.38",
            })
            (output / 'routed.json').write_text(json.dumps(routed))
            (output / 'nextpnr.log').write_text("Info: Program finished normally.\n")
            with patch.object(build_fes_zx81_oss, '_i2c_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_synthesis_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_evidence'), patch.object(
                build_fes_zx81_oss, '_plane_dsp_evidence'), patch.object(
                build_fes_zx81_oss, '_cell_counts', return_value={
                    **build_fes_zx81_oss.REQUIRED_RESOURCES,
                    'MISTRAL_M10K': 1,
                }
            ):
                with self.assertRaisesRegex(BuildError, r'plug_addr\[39\] exit used by video.vsync'):
                    build_fes_zx81_oss.validate_build_evidence(output)

    def test_signoff_rejects_shell_logic_on_the_plug_column(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'synth.json').write_text('{"modules":{"top":{}}}')
            from tests.test_build_fes_zx81_expansion import legal_socket_design
            routed = legal_socket_design(extra_cells={
                "video.vsync": {"type": "MISTRAL_COMB",
                                "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.24.2.0"}},
            })
            (output / 'routed.json').write_text(json.dumps(routed))
            (output / 'nextpnr.log').write_text("Info: Program finished normally.\n")
            with patch.object(build_fes_zx81_oss, '_i2c_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_synthesis_evidence'), patch.object(
                build_fes_zx81_oss, '_audio_evidence'), patch.object(
                build_fes_zx81_oss, '_plane_dsp_evidence'), patch.object(
                build_fes_zx81_oss, '_cell_counts', return_value={
                    **build_fes_zx81_oss.REQUIRED_RESOURCES,
                    'MISTRAL_M10K': 1,
                }
            ):
                with self.assertRaisesRegex(BuildError, r'video\.vsync at MISTRAL_COMB\.24\.2\.0'):
                    build_fes_zx81_oss.validate_build_evidence(output)

    def test_build_exports_sealed_map_and_cleans_failed_database_recheck(self):
        import hashlib
        from types import SimpleNamespace
        from scripts.core_package import read_package
        from scripts.cyclonev_rbf import SX120F
        from tests.test_rom_map import mux_text, routed_rom
        from tests.test_build_fes_zx81_expansion import ZX81SocketProducerTests
        producer = build_fes_zx81_oss
        database = {
            'data/m10k-mux.txt': mux_text().encode(),
            'libmistral/cyclonev.h': b'y = 2 + 86 * pos2y(pos);',
            'libmistral/cvd-sx120f.cc': ('7605, 7024, // cram size\n// x to bit x\n{' +
                ','.join(map(str, SX120F.x_to_bx)) + '}\n// column types\n{' +
                ','.join(['T_EMPTY']*5+['T_M10K']) + '}\n' +
                'sx120f_bel_spans_info[] = {1, 9, 1, 73, 80, 0xff};').encode(),
        }
        pins = {name: hashlib.sha256(data).hexdigest() for name, data in database.items()}
        cram = bytearray(b'\xff')*((SX120F.cram_sx*SX120F.cram_sy+7)//8)
        output = ROOT / OUTPUT_RELATIVE
        tools = {name: SimpleNamespace(path=ROOT/'build/toolchain/install/bin'/name, identity=name)
                 for name in ('yosys', 'nextpnr-mistral', 'mistral')}
        database_root = ROOT/'build/toolchain/src/mistral'
        for name, data in database.items():
            path = database_root/name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)

        def synth(*args, **kwargs):
            design = ZX81SocketProducerTests().fixture()
            (output/'synth.json').write_text(json.dumps(design))

        def route(**kwargs):
            design = routed_rom()
            for cell in design['modules']['top']['cells'].values():
                if cell.get('type') == 'MISTRAL_M10K':
                    cell['parameters']['CFG_ASYNC_READ'] = '0'
            (output/'routed.json').write_text(json.dumps(design))
            (output/'core.rbf').write_bytes(b'compiler fixture RBF')
            return SimpleNamespace(seed=10, weight=1000)

        def evidence(*args, **kwargs):
            return {'route': {}, 'rbf': {'size': 20, 'sha256': hashlib.sha256(b'compiler fixture RBF').hexdigest()}}

        with (patch.object(producer, 'ROM_DATABASE_SHA256', pins),
              patch.object(producer, '_authenticate_tools', return_value=tools),
              patch.object(producer, 'FunctionalInvocation', FakeInvocation),
              patch.object(producer, '_run_tool', side_effect=synth),
              patch.object(producer, 'route_after_synth', side_effect=route),
              patch.object(producer, 'validate_build_evidence', side_effect=evidence),
              patch('scripts.rom_map.rbf_load', return_value=SimpleNamespace(cram=cram))):
            package_path = producer.build(ROOT)
            self.addCleanup(package_path.chmod, 0o755)
            package = read_package(package_path)
            self.assertEqual(package.fields['format'], 3)
            self.assertEqual(package.fields['rom']['id'], 'machine-rom')
            self.assertEqual(package.fields['rom']['role'], 'firmware')
            self.assertEqual(package.fields['rom']['source_size'], 8192)
            self.assertEqual(package.manifest_bytes, (output/'manifest.toml').read_bytes())
            self.assertEqual(package.rom_map_bytes, (output/'rom-map.json').read_bytes())
            self.assertEqual(read_package(package_path.with_suffix('.fcore')), package)
            self.assertEqual(producer.build(ROOT), package_path)

            def tamper(*args, **kwargs):
                (database_root/'data/m10k-mux.txt').write_bytes(b'changed during compilation')
                return evidence()
            with patch.object(producer, 'validate_build_evidence', side_effect=tamper):
                with self.assertRaisesRegex(ValueError, 'database digest'):
                    producer.build(ROOT)
            for name in ('rom-map.json', 'manifest.toml', 'core.rbf', 'build-summary.json'):
                self.assertFalse((output/name).exists(), name)
            self.assertEqual(read_package(package_path), package)

    def test_record_seals_rom_database_and_format(self):
        record = json.loads(build_fes_zx81_oss.create_build_record(
            ROOT, 'https://example.invalid/fes.git', 'a'*40, {'yosys': 'test'}, execution=EXECUTION))
        self.assertEqual(record['parameters'].get('rom_id'), 'machine-rom')
        self.assertEqual(record['parameters'].get('package_format'), 3)
        self.assertIn('rom_database_sha256', record['parameters'])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root/OUTPUT_RELATIVE
            output.mkdir(parents=True)
            (output/'rom-map.json').write_bytes(b'stale map')
            build_fes_zx81_oss._prepare_output(root)
            self.assertFalse((output/'rom-map.json').exists())

    def test_record_seals_effective_search_policy(self) -> None:
        for mode in ("first-pass-paired", "staged"):
            with self.subTest(mode=mode):
                weights, budget = build_fes_zx81_oss.placement_policy(mode)
                record = json.loads(build_fes_zx81_oss.create_build_record(
                    ROOT, "https://github.com/DeanoC/misteross.git", "a" * 40,
                    {"yosys": "test"}, qor_mode=mode,
                 execution=EXECUTION))
                parameters = record["parameters"]
                self.assertEqual(parameters["placer_heap_timingweights"], ",".join(map(str, weights)))
                self.assertEqual(parameters["placer_qor_budget"], budget)
                self.assertEqual(parameters["expansion_socket"], "zx81-bus-v2")
        with self.assertRaises(BuildError):
            build_fes_zx81_oss.placement_policy("unknown")

    def test_product_search_tries_clock_aware_seed_1_first(self) -> None:
        from scripts.search_placer_qor import search
        from tests.test_search_placer_qor import _candidate
        weights, budget = build_fes_zx81_oss.placement_policy("first-pass-paired")
        calls = []

        def route(seed, weight):
            calls.append((seed, weight))
            return _candidate(seed, weight, 54.8)

        ranked = search(
            nextpnr=Path("unused"), fixture=Path("unused"), output=Path("unused"),
            device=build_fes_zx81_oss.TARGET, qsf=Path("unused"), sdc=None, freq=None,
            seeds=PLACER_SEEDS, weights=weights, critexp=PLACER_CRITICALITY_EXPONENT,
            budget=budget, mode="first-pass-paired", extra=(), timeout=1, run_one=route,
        )
        self.assertEqual(calls, [(1, 300)])
        self.assertEqual(PLACER_SEEDS, (1, 3, 10))
        self.assertGreater(len(PLACER_SEEDS), 1)
        self.assertGreater(len(weights), 1)
        self.assertTrue(ranked[0].passing)
        self.assertEqual((ranked[0].seed, ranked[0].weight), (1, 300))
        self.assertEqual(budget, len(PLACER_SEEDS) * len(weights))

    def test_make_entrypoint_uses_the_oss_recipe(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-zx81"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_zx81_oss.py --root "{ROOT}"',
        )
        phony = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(phony, r"\.PHONY:.*\bbuild-fes-zx81\b")

    def test_oss_auth_uses_verified_shared_tools_directly(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, manifest = publish_shared_toolchain(
                base, lock_path=ROOT / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK, gpu_router="HIP")
            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
                clear=True,
            ), patch.object(
                toolchain_cache,
                "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                authenticated = build_fes_zx81_oss._authenticate_tools(
                    ROOT, cache_root=request.cache_root
                )

            self.assertEqual(authenticated["yosys"].path, manifest.install / "bin/yosys")
            self.assertEqual(
                authenticated["nextpnr-mistral"].path,
                manifest.install / "bin/nextpnr-mistral",
            )

    def test_oss_commands_use_nmos_z80_and_not_tv80(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OUTPUT_RELATIVE,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("fes_z80_nmos.sv", program)
        self.assertNotIn("tv80_core.v", program)
        self.assertNotIn("t80pa.v", program)
        self.assertIn("-DFES_ZX81_ROM_LINK=1", program)
        self.assertIn("zx81_rom_link.v", program)
        self.assertNotIn("zx8x.hex", program)
        self.assertNotIn("cores/fes-zx81/rtl/zx8x.hex", build_fes_zx81_oss.PINNED_INPUTS)
        self.assertIn("cores/fes-zx81/rtl/zx81_rom_link.v", build_fes_zx81_oss.PINNED_INPUTS)
        self.assertNotIn("-DFES_ZX81_OSS=1", program)
        self.assertIn("synth_intel_alm -nolutram -top top", program)
        self.assertNotIn("-nodsp", program)
        self.assertNotIn("-nobram", program)
        self.assertNotIn("T80pa.vhd", program)
        self.assertIn("cores/fes-zx81/rtl/top.v", program)
        self.assertIn("chparam -set EXPANSION_SOCKET 1 top", program)
        self.assertIn("--freq", nextpnr)
        self.assertIn("74.25", nextpnr)
        self.assertIn("--seed", nextpnr)
        self.assertIn(str(PLACER_SEEDS[0]), nextpnr)
        self.assertIn("--placer-heap-timingweight", nextpnr)
        self.assertIn(str(PLACER_TIMING_WEIGHT), nextpnr)
        self.assertIn("--placer-heap-critexp", nextpnr)
        self.assertIn(str(PLACER_CRITICALITY_EXPONENT), nextpnr)
        self.assertIn("--router", nextpnr)
        self.assertIn("gpu", nextpnr)
        self.assertNotIn("router1", nextpnr)
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("build/fes-zx81-oss/socket.qsf", joined)
        self.assertIn("cores/fes-zx81/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-zx81/clocks.sdc", joined)
        self.assertNotIn("cores/fes-zx81/constraints-oss.qsf", joined)

    def test_standard_authentication_uses_the_socket_toolchain(self) -> None:
        with patch.object(build_fes_zx81_oss, "_authenticate_oss_tools") as authenticate:
            build_fes_zx81_oss._authenticate_tools(ROOT, cache_root=Path("/cache"))
        authenticate.assert_called_once()
        kwargs = authenticate.call_args.kwargs
        self.assertEqual(kwargs["lock_path"], ROOT / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK)
        self.assertEqual(kwargs["toolchain_root"], ROOT / "build/toolchain/zx81-expansion")

    def test_oss_top_uses_mistral_io_without_quartus(self) -> None:
        top = (ROOT / "cores/fes-zx81/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("MISTRAL_IO hdmi_scl_pad", top)
        self.assertIn('BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0"', top)
        self.assertIn("`ifdef QUARTUS", top)
        self.assertIn("hdmi_scl_low ? 1'b0 : 1'bz", top)
        dpram = (ROOT / "cores/fes-zx81/rtl/zx81_dpram.v").read_text(encoding="utf-8")
        self.assertNotIn("FES_ZX81_OSS", dpram)
        self.assertIn('ramstyle = "M10K"', dpram)
        self.assertIn("q_a_r <= wren_a ? data_a : ram[address_a]", dpram)
        self.assertNotIn("assign q_a = ram[address_a]", dpram)
        sys_pll = (ROOT / "cores/fes-zx81/rtl/sys_pll.v").read_text(encoding="utf-8")
        self.assertIn('.output_clock_frequency0("52.224 MHz")', sys_pll)
        self.assertNotIn('.output_clock_frequency0("50.0 MHz")', sys_pll)

    def test_model_selects_family_core_rom_and_yosys(self) -> None:
        tools = {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")}
        default_yosys = build_commands(ROOT, ROOT / OUTPUT_RELATIVE, "0" * 32, tools)[0][2]
        self.assertIn("-DFES_ZX81_ROM_LINK=1 -I", default_yosys)
        self.assertNotIn("ZX81_MODEL", default_yosys)
        zx80 = build_fes_zx81_oss.model_for("zx80")
        self.assertEqual(zx80.core_id, "fes.zx80")
        self.assertEqual(zx80.rom_bytes, 4096)
        self.assertEqual(zx80.lane_rows, tuple(range(73, 77)))
        yosys = build_commands(
            ROOT, ROOT / zx80.output, "0" * 32, tools, model="zx80"
        )[0][2]
        self.assertIn("-DZX81_MODEL_ZX80", yosys)
        self.assertIn("-DFES_ZX81_ROM_LINK=1", yosys)
        self.assertIn("build/fes-zx80-oss/synth.json", yosys)
        with self.assertRaises(BuildError):
            build_fes_zx81_oss.model_for("ts2068")
        record = json.loads(build_fes_zx81_oss.create_build_record(
            ROOT, "https://example.invalid/fes.git", "a" * 40, {"yosys": "test"},
            execution=EXECUTION, model="zx80"))
        parameters = record["parameters"]
        self.assertEqual(parameters["core_id"], "fes.zx80")
        self.assertEqual(parameters["model"], "zx80")
        self.assertEqual(parameters["model_define"], "ZX81_MODEL_ZX80")
        self.assertEqual(parameters["rom_id"], "zx80-rom")
        self.assertEqual(parameters["rom_source_size"], 4096)
        default = json.loads(build_fes_zx81_oss.create_build_record(
            ROOT, "https://example.invalid/fes.git", "a" * 40, {"yosys": "test"},
            execution=EXECUTION))
        self.assertEqual(default["parameters"]["core_id"], "fes.zx81")
        self.assertEqual(default["parameters"]["model"], "zx81")
        self.assertEqual(default["parameters"]["model_define"], "none")
        self.assertEqual(default["parameters"]["rom_source_size"], 8192)
        top = (ROOT / "cores/fes-zx81/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("`ifdef ZX81_MODEL_ZX80\n        .ENABLE_ZX81_TAPE_WRITE(0),", top)
        self.assertIn(".ENABLE_ZX81_TAPE_WRITE(1),", top)

    def test_oss_rejects_wrong_output_directory(self) -> None:
        with self.assertRaises(BuildError):
            build_commands(
                ROOT,
                ROOT / "build/other",
                "00112233445566778899aabbccddeeff",
                {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
            )

    def test_oss_manifest_carries_source_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
            "rom": dict(id="machine-rom", role="firmware", source_size=8192, file="rom-map.json", size=100, sha256="f"*64),
        }
        tools = {
            "mistral": "commit=" + "f" * 40 + "; sha256=" + "1" * 64,
            "nextpnr-mistral": "commit=" + "2" * 40 + "; sha256=" + "3" * 64,
            "yosys": "commit=" + "4" * 40 + "; sha256=" + "5" * 64,
        }
        manifest = _manifest(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            tools,
        )
        self.assertIn(b"repository = ", manifest)
        self.assertIn(b"revision = \"" + (b"a" * 40) + b"\"", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_socket_manifest_exposes_bus_not_a_cart_type(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{"expansion_socket":"zx81-bus-v2"}}'
        )
        manifest = _manifest(
            record,
            {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64},
             "rom": dict(id="machine-rom", role="firmware", source_size=8192, file="rom-map.json", size=100, sha256="f"*64)},
            "https://github.com/DeanoC/misteross.git", "a" * 40,
            {"mistral": "m", "nextpnr-mistral": "n", "yosys": "y"},
        )
        fields = tomllib.loads(manifest.decode())
        self.assertEqual(fields["core"]["version"], "1.7.0")
        self.assertEqual(fields["format"], 3)
        self.assertEqual(fields["rom"]["id"], "machine-rom")
        self.assertEqual(fields["rom"]["source_size"], 8192)
        interfaces = {item["id"] for item in fields["interfaces"]}
        self.assertIn("fes.expansion.zx81-bus", interfaces)
        self.assertIn("fes.audio.pcm-s16-stereo-48k", interfaces)
        self.assertTrue(next(item['required'] for item in fields['interfaces']
                             if item['id'] == 'fes.audio.pcm-s16-stereo-48k'))
        self.assertNotIn("fes.expansion.zx81-ram", interfaces)
        self.assertEqual(fields["abi"]["id"], "fes.computer")
        self.assertIn("fes.keyboard.hid", interfaces)
        self.assertIn("fes.media.zx81-tape", interfaces)
        self.assertIn("fes.media.zx81-tape-write", interfaces)
        self.assertNotIn("fes.media.blob", interfaces)
        self.assertIn("fes.memory.hps-ddr", interfaces)
        self.assertIn("fes.video.session-display", interfaces)
        for item in fields["interfaces"]:
            if item["id"] in ("fes.memory.hps-ddr", "fes.video.session-display"):
                self.assertTrue(item["required"])
        # Software without the shared plane refuses the package at admission,
        # before programming. The declared version is the generated contract's.
        header = (build_fes_zx81_oss.ROOT / "cores/fes-common/generated/fes_display_plane.vh").read_text()
        major, minor = (int(header.split(f"`define FES_DISPLAY_PLANE_{part} 32'h")[1].split()[0], 16)
                        for part in ("MAJOR", "MINOR"))
        plane = [item for item in fields["interfaces"] if item["id"] == "fes.fabric.video.display-plane"]
        self.assertEqual(plane, [{"id": "fes.fabric.video.display-plane", "major": major,
                                  "minor": minor, "required": True}])

    def test_family_manifest_save_follows_the_zx81_rom(self) -> None:
        def manifest_for(model: str) -> dict:
            record = (
                b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
                b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
                b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
                b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
                b'"tools":{},"parameters":{"model":"' + model.encode() + b'"}}'
            )
            raw = _manifest(
                record,
                {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64},
                 "rom": dict(id="rom", role="firmware", source_size=8192, file="rom-map.json", size=100, sha256="f" * 64)},
                "https://github.com/DeanoC/misteross.git", "a" * 40,
                {"mistral": "m", "nextpnr-mistral": "n", "yosys": "y"},
            )
            return tomllib.loads(raw.decode())

        zx81 = manifest_for("zx81")
        self.assertEqual(zx81["core"], {
            "id": "fes.zx81", "name": "Zx81",
            "description": "ZX81 on the FES computer ABI", "version": "1.7.0",
        })
        zx81_ids = {item["id"] for item in zx81["interfaces"]}
        self.assertIn("fes.media.zx81-tape-write", zx81_ids)
        zx80 = manifest_for("zx80")
        self.assertEqual(zx80["core"]["id"], "fes.zx80")
        self.assertEqual(zx80["core"]["version"], "0.1.0")
        zx80_ids = {item["id"] for item in zx80["interfaces"]}
        self.assertIn("fes.media.zx81-tape", zx80_ids)
        self.assertNotIn("fes.media.zx81-tape-write", zx80_ids)
        for model, core_id in (("ts1000", "fes.ts1000"), ("ts1500", "fes.ts1500")):
            fields = manifest_for(model)
            self.assertEqual(fields["core"]["id"], core_id)
            self.assertEqual(fields["core"]["version"], "0.1.0")
            ids = {item["id"] for item in fields["interfaces"]}
            self.assertIn("fes.media.zx81-tape-write", ids)


class Zx81PlaneDspTests(unittest.TestCase):
    @staticmethod
    def design(*cells):
        return {"modules": {"top": {"cells": {
            name: {"type": kind, "parameters": {"A_SIGNED": signed, "B_SIGNED": signed}}
            for name, kind, signed in cells}}}}

    def test_exactly_three_unsigned_plane_products(self) -> None:
        products = [(f"display.{colour}_product", "MISTRAL_MUL9X9", "0")
                    for colour in ("red", "green", "blue")]
        build_fes_zx81_oss._plane_dsp_evidence(self.design(*products))
        self.assertEqual(build_fes_zx81_oss.DSP_RESOURCES & build_fes_zx81_oss.FORBIDDEN_RESOURCES, frozenset())
        self.assertIn("MISTRAL_MUL18X19", build_fes_zx81_oss.FORBIDDEN_RESOURCES)
        for cells in (products[:2], [*products, ("machine.alu", "MISTRAL_MUL9X9", "0")],
                      [*products[:2], ("machine.alu", "MISTRAL_MUL9X9", "0")],
                      [*products[:2], ("display.blue_product", "MISTRAL_MUL9X9", "1")],
                      [*products[:2], ("display.blue_product", "MISTRAL_MUL9X9", "x")]):
            with self.subTest(cells=cells), self.assertRaises(build_fes_zx81_oss.BuildError):
                build_fes_zx81_oss._plane_dsp_evidence(self.design(*cells))


class Zx81SyncM10kTests(unittest.TestCase):
    def test_rom_lanes_and_cart_tdps_are_synchronous(self) -> None:
        root = build_fes_zx81_oss.ROOT
        rtl = (root / "cores/fes-zx81/rtl/zx81_rom_link.v").read_text()
        machine = (root / "cores/fes-zx81/rtl/zx81_machine.sv").read_text()
        self.assertEqual(rtl.count(".CFG_ASYNC_READ(0)"), 8)
        self.assertNotIn("CFG_ASYNC_READ(1)", rtl)
        self.assertEqual(rtl.count(".B1EN(1'b1)"), 8)
        self.assertIn("bank_d <= address[12:10]", rtl)
        self.assertIn(".clk(clk_sys)", machine)
        producer = (root / "scripts/build_fes_zx81_oss.py").read_text()
        self.assertIn("expected_async_read=0", producer)
        self.assertIn("reject_async_m10k_reads", producer)
        pack = (root / "cores/fes-zx81/rtl/zx81_ram_pack.v").read_text()
        chars = (root / "cores/fes-zx81/expansions/qs_chrs.v").read_text()
        self.assertNotIn("CFG_ASYNC_READ(1)", pack)
        self.assertNotIn("CFG_ASYNC_READ(1)", chars)
        self.assertIn("read_bank_q", pack)
        self.assertIn("peek_bank_q", pack)
        self.assertIn("romcs_q", chars)
        self.assertIn("dsel_q", chars)
        contract = "describe the same request"
        bus = (root / "cores/fes-zx81/rtl/zx81_bus_pack.vh").read_text()
        socket = (root / "cores/fes-zx81/rtl/zx81_expansion_socket.v").read_text()
        self.assertIn(contract, bus)
        self.assertIn(contract, socket)
        self.assertIn("at most 6 clk_sys", bus)
        self.assertIn("at most 6 clk_sys", socket)
        for name in ("scripts/build_zx81_bus_validation_cart.py", "scripts/hip_zx81_bus_carts.py"):
            text = (root / name).read_text()
            self.assertIn("reject_async_m10k_reads", text)
            self.assertIn("cart.json", text)
            self.assertIn("cart-routed.json", text)


def _yosys_binary() -> str | None:
    named = os.environ.get("YOSYS")
    if named and Path(named).is_file() and os.access(named, os.X_OK):
        return named
    return shutil.which("yosys")


@unittest.skipUnless(_yosys_binary(), "Yosys is required to check the ZX81 M10K netlist")
class Zx81YosysM10kTests(unittest.TestCase):
    def test_mapped_shell_has_no_async_m10k(self) -> None:
        yosys = _yosys_binary()
        assert yosys is not None
        sources = " ".join(build_fes_zx81_oss.RTL_SOURCES)
        program = (
            f"read_verilog -lib {build_fes_zx81_oss.DDR_ATOM}; "
            "read_verilog -sv -DFES_ZX81_ROM_LINK=1 "
            "-I cores/fes-zx81/generated -I cores/fes-common/generated -I cores/fes-zx81/rtl "
            f"{sources}; "
            "chparam -set BUILD_ID 128'h" + "0" * 32 + " top; "
            "chparam -set EXPANSION_SOCKET 1 top; "
            "synth_intel_alm -nolutram -top top -run :map_luts; "
            "autoname; select -module top; "
            "log ---SDP---; select -count t:MISTRAL_M10K; "
            "log ---TDP---; select -count t:MISTRAL_M10K_TDP; "
            "log ---ASYNC---; "
            "select -list t:MISTRAL_M10K r:CFG_ASYNC_READ=1 %i; "
            "select -list t:MISTRAL_M10K_TDP r:CFG_ASYNC_READ=1 %i; "
            "log ---LANES---; select -list t:MISTRAL_M10K r:CFG_ASYNC_READ=0 %i; "
            "log ---END---"
        )
        with tempfile.TemporaryDirectory() as directory:
            log_path = Path(directory) / "yosys.log"
            result = subprocess.run([yosys, "-l", str(log_path), "-p", program],
                                    cwd=build_fes_zx81_oss.ROOT,
                                    text=True, capture_output=True, check=False)
            log = log_path.read_text(encoding="utf-8", errors="replace")
        self.assertEqual(result.returncode, 0, (result.stderr or log)[-4000:])
        sections: dict[str, list[str]] = {}
        current = None
        for line in log.splitlines():
            if line.startswith("---") and line.endswith("---") and line.strip("-"):
                current = line.strip("-")
                sections[current] = []
            elif current is not None and line.strip():
                sections[current].append(line.strip())
        async_cells = [line for line in sections["ASYNC"] if line.startswith("top/")]
        lanes = sorted(line.removeprefix("top/") for line in sections["LANES"] if "machine.rom.lane" in line)
        self.assertEqual(async_cells, [])
        self.assertEqual(lanes, sorted(f"machine.rom.lane{index}" for index in range(8)))


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
