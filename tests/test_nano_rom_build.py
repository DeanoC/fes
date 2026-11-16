"""Actual shared ROM linker and builder snapshots; no board or cross compiler."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('native_rom_builder', ROOT/'platform/embedded/tamago/build.py')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class ROMBuildTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        compiler = os.environ.get('FES_GO_HOST') or shutil.which('go')
        if not compiler:
            raise unittest.SkipTest('host Go compiler unavailable')
        version = subprocess.check_output([compiler, 'version'], text=True)
        if 'go1.26.' not in version and 'go1.27.' not in version:
            raise unittest.SkipTest('host Go 1.26+ required for the shared linker')
        cls.compiler = Path(compiler)

    def fixture(self, size):
        base=ROOT/'sources/misteross/tests/fixtures/core-bundle-v3'
        case=next(c for c in json.loads((base/'cases.json').read_text()) if c['name']==f'valid-nano-{size}')
        # Build the actual three-member USTAR package read by the retained Go parser.
        data=bytearray()
        for name,key in [('manifest.toml','manifest'),('core.rbf','payload'),('rom-map.json','rom_map')]:
            body=(base/case[key]).read_bytes()
            member=tarfile.TarInfo(name);member.size=len(body);member.mode=0o644
            data+=member.tobuf(format=tarfile.USTAR_FORMAT)+body+b'\0'*((-len(body))%512)
        return bytes(data)+b'\0'*1024,case['package_id']

    def test_real_linker_binds_snapshots_and_rejects_foreign_package_and_size(self):
        for size in (4096,8192):
            archive,package_id=self.fixture(size)
            with tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);output=root/'output';output.mkdir()
                rom=root/'rom.bin';image=bytes((i*29+7)&255 for i in range(size));rom.write_bytes(image)
                record=builder.prepare_rom_selection(archive,rom,package_id,size,self.compiler,output)
                self.assertEqual(record['package_id'],package_id)
                self.assertEqual(record['source_sha256'],hashlib.sha256(image).hexdigest())
                self.assertEqual(record['source_size'],size)
                self.assertEqual((output/'FIRMWARE.BIN').read_bytes(),image)
                self.assertEqual((output/'package.fcore').read_bytes(),archive)
                first=record['rom_receipt_sha256']
                changed=bytearray(image);changed[0]^=1;rom.write_bytes(changed)
                second=builder.prepare_rom_selection(archive,rom,package_id,size,self.compiler,output)
                self.assertNotEqual(first,second['rom_receipt_sha256'])
                self.assertEqual(record['source_sha256'],hashlib.sha256(image).hexdigest())
                with self.assertRaises(ValueError):
                    builder.prepare_rom_selection(archive,rom,'f'*64,size,self.compiler,output)
                rom.write_bytes(image[:-1])
                with self.assertRaises(ValueError):
                    builder.prepare_rom_selection(archive,rom,package_id,size,self.compiler,output)


if __name__=='__main__':unittest.main()
