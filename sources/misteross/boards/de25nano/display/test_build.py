"""Exercise locked display preparation and source/parameter gates without Quartus."""
import importlib.util
import io
import json
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import zipfile

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_display_builder", HERE / "build.py")
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class DisplayBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-de25-display-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.files = {name: b"vendor input\n" for name in B.VENDOR_FILES}
        pins = '\n'.join(f'set_location_assignment PIN_TEST{n} -to {port}\n'
                         f'set_instance_assignment -name IO_STANDARD "3.3-V LVCMOS" -to {port}'
                         for n, port in enumerate(sorted(B.PORTS))) + '\n'
        self.files["golden_top.qsf"] = ('set_global_assignment -name DEVICE A5EB013BB23BE4SR1\n' + pins).encode()
        self.baseline = ('set_global_assignment -name DEVICE ' + B.DEVICE + '\n' + pins).encode()
        params = {"system_info_device_component": "A5EB013BB23BE4SR1", "gui_reference_clock_frequency": "50.0",
                  "gui_output_clock_frequency0": "74.25", "gui_number_of_clocks": "1", "gui_use_locked": "true",
                  "gui_phase_shift0": "0.0", "gui_duty_cycle0": "50.0"}
        self.files["sys_pll.ip"] = ('<ipxact:component xmlns:ipxact="' + B.NS['i'] + '" xmlns:altera="' + B.NS['a'] + '">'
            '<altera:altera_module_parameters><ipxact:parameters>' + ''.join(
                f'<ipxact:parameter parameterId="{key}"><ipxact:value>{value}</ipxact:value></ipxact:parameter>'
                for key, value in params.items()) + '</ipxact:parameters></altera:altera_module_parameters>'
            '<ipxact:value>A5EB013BB23BE4SR1</ipxact:value></ipxact:component>').encode()
        self.files['agilex_reset_release.ip'] = ('<ipxact:component xmlns:ipxact="' + B.NS['i'] + '" xmlns:altera="' + B.NS['a'] + '">'
            '<altera:altera_module_parameters><ipxact:parameters><ipxact:parameter parameterId="DEVICE_FAMILY">'
            '<ipxact:value>Agilex 5</ipxact:value></ipxact:parameter></ipxact:parameters></altera:altera_module_parameters>'
            '<ipxact:value>A5EB013BB23BE4SR1</ipxact:value></ipxact:component>').encode()
        self.archive = self.root / "vendor.zip"

    def prepare(self, mode="idle", output=None, baseline=None):
        sources = B.capture_sources()
        lock = sources["vendor.lock.toml"].decode()
        prefix = "Demonstration/FPGA/HDMI_ASx4/"
        with zipfile.ZipFile(self.archive, 'w') as z:
            for name, data in self.files.items():
                z.writestr(prefix + name, data)
            z.writestr(B.SCS_GOLDEN, baseline if baseline is not None else self.baseline)
        original_hash = B.tomllib.loads(lock)["archive"]["sha256"]
        sources["vendor.lock.toml"] = lock.replace(original_hash, B.sha(self.archive.read_bytes())).encode()
        with mock.patch.object(B, "capture_sources", return_value=sources):
            return B.prepare(self.archive, mode, output or self.root / mode)

    def test_prepares_real_source_modes_with_vendor_pins_and_only_device_retarget(self):
        for mode in ("idle", "menu-pattern"):
            with self.subTest(mode=mode):
                output = self.prepare(mode)
                project = output / "project"
                qsf = (project / (B.PROJECT + '.qsf')).read_text()
                self.assertIn('NUM_PARALLEL_PROCESSORS 2', qsf)
                self.assertIn('set_parameter -name MENU_PATTERN ' + str(int(mode == "menu-pattern")), qsf)
                self.assertEqual('VERILOG_FILE fes_splash_core.v' in qsf, mode == "idle")
                self.assertEqual('VERILOG_FILE fes_menu_reader.v' in qsf, mode == "menu-pattern")
                self.assertNotIn('fes_menu_ddr', qsf)
                self.assertEqual((project / "I2C_HDMI_Config.v").read_bytes(), self.files["HDMI_I2C/I2C_HDMI_Config.v"])
                self.assertEqual((output / "vendor/sys_pll.ip").read_bytes(), self.files["sys_pll.ip"])
                self.assertEqual((project / "sys_pll.ip").read_bytes(),
                    self.files["sys_pll.ip"].replace(b'A5EB013BB23BE4SR1', B.DEVICE.encode()))
                self.assertEqual((project / 'agilex_reset_release.ip').read_bytes(),
                    self.files['agilex_reset_release.ip'].replace(b'A5EB013BB23BE4SR1', B.DEVICE.encode()))
                inputs = json.loads((output / 'inputs.json').read_text())
                self.assertFalse(inputs['hps'] or inputs['ddr'] or inputs['runtime_menu'])
                self.assertEqual(inputs['source_classification'], 'working-copy-diagnostic')
                self.assertEqual(inputs['source_sha256']['top.v'], B.sha((project / 'top.v').read_bytes()))

    def test_rejects_incomplete_or_disagreeing_scs_pin_contract(self):
        missing = self.baseline.replace(b'set_location_assignment', b'comment_location_assignment', 1)
        with self.assertRaisesRegex(ValueError, 'assignment set changed'):
            self.prepare(baseline=missing)
        other = self.baseline.replace(b'PIN_TEST0', b'PIN_DIFFERENT')
        with self.assertRaisesRegex(ValueError, 'pins differ'):
            self.prepare(baseline=other)

    def test_rejects_changed_pll_frequency_or_metadata(self):
        original = self.files['sys_pll.ip']
        for before, after in ((b'>74.25<', b'>75.0<'), (b'>true<', b'>false<'), (b'A5EB013BB23BE4SR1', b'OTHER')):
            self.files['sys_pll.ip'] = original.replace(before, after)
            with self.subTest(before=before), self.assertRaisesRegex(ValueError, 'PLL contract changed'):
                self.prepare()

    def test_refuses_existing_output_and_unlocked_archive(self):
        output = self.prepare()
        with self.assertRaises(FileExistsError):
            self.prepare(output=output)
        with self.assertRaisesRegex(ValueError, 'matching vendor.lock'):
            B.prepare(self.archive, 'idle', self.root / 'unlocked')

    def test_archive_replacement_after_hash_cannot_change_selected_entries(self):
        self.prepare()
        sources = B.capture_sources()
        original = self.archive.read_bytes()
        lock = sources['vendor.lock.toml'].decode()
        sources['vendor.lock.toml'] = lock.replace(B.tomllib.loads(lock)['archive']['sha256'], B.sha(original)).encode()
        read_bytes = Path.read_bytes
        def replace_after_read(path):
            data = read_bytes(path)
            if path == self.archive:
                path.write_bytes(b'replaced archive must never be reopened')
            return data
        with mock.patch.object(B, 'capture_sources', return_value=sources), mock.patch.object(Path, 'read_bytes', replace_after_read):
            output = B.prepare(self.archive, 'idle', self.root / 'bound-archive')
        self.assertEqual((output / 'vendor/sys_pll.ip').read_bytes(), self.files['sys_pll.ip'])

    def test_changed_reset_release_family_is_rejected(self):
        self.files['agilex_reset_release.ip'] = self.files['agilex_reset_release.ip'].replace(b'Agilex 5', b'Cyclone V')
        with self.assertRaisesRegex(ValueError, 'reset-release device/family'):
            self.prepare()

    def test_captured_git_revision_ignores_later_working_tree_bytes(self):
        repository = self.root / 'repository'
        repository.mkdir()
        source = repository / 'top.v'
        source.write_bytes(b'frozen RTL')
        def git(*args):
            return subprocess.check_output(['git', '-C', str(repository), *args], stderr=subprocess.PIPE)
        git('init', '-q'); git('config', 'user.name', 'FES test'); git('config', 'user.email', 'test@example.invalid')
        git('add', '.'); git('commit', '-qm', 'frozen source')
        revision = git('rev-parse', 'HEAD').decode().strip()
        source.write_bytes(b'later RTL')
        with mock.patch.object(B, 'ROOT', repository), mock.patch.object(B, 'SOURCE_PATHS', {'top.v': source}):
            self.assertEqual(B.capture_sources(revision)['top.v'], b'frozen RTL')

    def test_dirty_build_stops_before_preparation(self):
        identity = {'base_revision': 'a' * 40, 'source_classification': 'working-copy-diagnostic'}
        with mock.patch.object(B, 'source_identity', return_value=identity), mock.patch.object(B, 'prepare') as prepare, redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                B.main(['build', '--mode', 'idle', '--vendor-archive', str(self.archive)])
            prepare.assert_not_called()

    def test_ip_generation_cannot_silently_change_original_pll_parameters(self):
        output = self.prepare()
        def mutate(command, project, log):
            path = project / 'sys_pll.ip'
            path.write_bytes(path.read_bytes().replace(b'>74.25<', b'>75.0<'))
        with mock.patch.object(B, 'run', side_effect=mutate), self.assertRaisesRegex(ValueError, 'generation changed'):
            B.compile_project(output, self.root / 'quartus_sh', 'test')
        self.assertFalse((output / 'build.json').exists())
        self.assertIn('gui_output_clock_frequency0', json.loads((output / 'ip-parameter-review.json').read_text())['sys_pll.ip'])

    def test_ip_generation_cannot_add_unreviewed_reset_parameters(self):
        output = self.prepare()
        def mutate(command, project, log):
            path = project / 'agilex_reset_release.ip'
            path.write_bytes(path.read_bytes().replace(b'</ipxact:parameters>',
                b'<ipxact:parameter parameterId="NEW_RESET_MODE"><ipxact:value>new</ipxact:value>'
                b'</ipxact:parameter></ipxact:parameters>'))
        with mock.patch.object(B, 'run', side_effect=mutate), self.assertRaisesRegex(ValueError, 'generation changed'):
            B.compile_project(output, self.root / 'quartus_sh', 'test')
        review = json.loads((output / 'ip-parameter-review.json').read_text())
        self.assertEqual(review['agilex_reset_release.ip']['NEW_RESET_MODE'], {'before': None, 'after': 'new'})

    def test_source_change_during_preparation_stops_before_ip_generation(self):
        good = {'fes_revision': 'a' * 40, 'source_classification': 'committed'}
        changed = {'base_revision': 'a' * 40, 'source_classification': 'working-copy-diagnostic'}
        with mock.patch.object(B, 'source_identity', side_effect=[good, changed]), \
             mock.patch.object(B.TOOLS, 'locate_quartus', return_value=(self.root / 'quartus_sh', 'test')), \
             mock.patch.object(B, 'prepare', return_value=self.root), mock.patch.object(B, 'compile_project') as compile, \
             redirect_stderr(io.StringIO()), redirect_stdout(io.StringIO()):
            with self.assertRaises(SystemExit):
                B.main(['build', '--mode', 'idle', '--vendor-archive', str(self.archive)])
            compile.assert_not_called()
