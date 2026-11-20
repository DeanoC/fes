"""The flash packager can publish only committed producer bytes."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
from boot_flash_image import clean_head, committed_sources, composition_inputs


class FlashProducerSourceTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.path = self.root/'platform/embedded/nano/producer.py'
        self.dependency = self.path.with_name('fit_layout.py')
        self.git('init', '-q')
        self.path.parent.mkdir(parents=True)
        self.path.write_bytes(b'# pinned producer\n')
        self.dependency.write_bytes(b'# indirect dependency\n')
        self.git('add', 'platform/embedded')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                 'commit', '-qm', 'fixture')

    def git(self, *args):
        subprocess.run(['git', *args], cwd=self.root, check=True, capture_output=True)

    def test_pins_committed_bytes_and_rejects_dirty_sources(self):
        head = clean_head(self.root)
        expected = {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in (self.path, self.dependency)}
        self.assertEqual(committed_sources(self.root, head), expected)

        self.path.write_bytes(b'# edited producer\n')
        with self.assertRaisesRegex(ValueError, 'clean committed checkout'):
            clean_head(self.root)

    def test_git_status_alone_cannot_hide_changed_indirect_dependency(self):
        head = clean_head(self.root)
        self.git('update-index', '--assume-unchanged', str(self.dependency.relative_to(self.root)))
        self.dependency.write_bytes(b'# hidden edit\n')
        self.assertEqual(clean_head(self.root), head)
        with self.assertRaisesRegex(ValueError, 'source differs from its commit: .*fit_layout.py'):
            committed_sources(self.root, head)

    def composition_fixture(self):
        origin = self.root/'origin'

        def write(name, value, *, source=False):
            selected = self.root/name
            retained = origin/name
            for path in (retained, selected) if source else (retained,):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(value)
            if source:
                self.git('add', name)
            return retained

        producer = write('platform/embedded/nano/hps_boot.py', '# compose', source=True)
        builder = write('platform/embedded/firmware/build_terasic_uboot.py', '# build', source=True)
        driver = write('platform/embedded/firmware/nano_sd.c', 'int sd;', source=True)
        sdk = write('platform/embedded/entry/entry.S', 'entry:', source=True)
        rtl = write('sources/misteross/core.v', 'module core; endmodule', source=True)
        entry = write('out/entry/entry.img', 'entry payload')
        spl = write('out/uboot/build/spl/u-boot-spl-dtb.hex', 'spl payload')
        sof = write('out/board/hps/project/output_files/golden_top.sof', 'SOF payload')
        config = write('out/uboot/build/.config', 'CONFIG_BOOT=y')

        def sha(path):
            return hashlib.sha256(path.read_bytes()).hexdigest()

        def source_map(path):
            return {str(path.relative_to(origin)): sha(path)}

        entry_receipt = write('out/entry/result.json', json.dumps({'sources': source_map(sdk), 'native_build_sources': source_map(driver)}))
        uboot_receipt = write('out/uboot/result.json', json.dumps({'native_handoff': {'producer_sources': source_map(driver)},
                                                                  'producer_sha256': sha(builder)}))
        fpga_receipt = write('out/board/hps/build-inputs.json', json.dumps({'source_inputs': source_map(rtl)}))
        tool = write('quartus_pfg', 'converter')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'composition')
        inventory = {str(p): sha(p) for p in (producer, entry, spl, sof, config, entry_receipt, uboot_receipt, fpga_receipt, tool)}

        def check(head):
            return composition_inputs(self.root, head, inventory, (tool,), sha=sha,
                                      read_record=lambda path: json.loads(path.read_bytes()))

        return check, driver, rtl, config

    def test_retained_composition_checks_nested_firmware_rtl_and_generated_inputs(self):
        check, driver, rtl, config = self.composition_fixture()
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=self.root, text=True).strip()
        self.assertTrue({driver, rtl, config} <= check(head).keys())
        config.write_text('CONFIG_BOOT=n')
        with self.assertRaisesRegex(ValueError, r'recorded digest: .*\.config'):
            check(head)

    def test_retained_bytes_reject_a_later_commit_with_changed_firmware(self):
        check, driver, _, _ = self.composition_fixture()
        relative = driver.relative_to(self.root/'origin')
        (self.root/relative).write_text('int changed_sd;')
        self.git('add', str(relative))
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'changed C input')
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=self.root, text=True).strip()
        with self.assertRaisesRegex(ValueError, 'selected commit: .*nano_sd.c'):
            check(head)


if __name__ == '__main__':
    unittest.main()
