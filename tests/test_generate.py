from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from scripts import generate


class GenerateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        for module in generate.MODULES:
            self.file(f'sources/{module}/README', b'module')
        self.file('sources/mister-packages/definition.yaml', b'definition')
        self.file('sources/mister-packages/fixtures/current.json', b'canonical')
        self.file('sources/libmister-runtime/canonical.json', b'runtime canonical')
        self.file('sources/FogCast/generated.go', b'old')
        self.file('sources/FogCast/fixtures/removed.json', b'stale')
        subprocess.run(['git', '-C', str(self.root), 'add', '.'], check=True)
        mappings = {
            'GENERATED': (('emit-go', 'definition.yaml', 'FogCast', 'generated.go'),),
            'COPIED_TREES': (('fixtures', 'FogCast', 'fixtures'),),
            'COPIED_FILES': (('fixtures/current.json', 'libmister-runtime', 'copy.json'),),
            'COMPONENT_FIXTURES': (('libmister-runtime', 'canonical.json', 'FogCast', 'runtime.json'),),
            'CORE_SOURCES': (),
        }
        for name,value in mappings.items():
            patcher=patch.object(generate.consistency,name,value)
            patcher.start(); self.addCleanup(patcher.stop)

    def file(self, relative, data):
        target=self.root/relative
        target.parent.mkdir(parents=True,exist_ok=True)
        target.write_bytes(data)
        return target

    def emit(self, root, command, source):
        return b'generated\n' if command=='emit-go' else b''

    def snapshot(self):
        return {str(p.relative_to(self.root)):p.read_bytes() for p in self.root.rglob('*')
                if p.is_file() and '.git' not in p.parts}

    def test_write_is_idempotent_preserves_sources_and_deletes_stale_members(self):
        before=self.snapshot()
        result=generate.regenerate(self.root,True,self.emit)
        self.assertEqual(result['deleted'],1)
        self.assertEqual((self.root/'sources/FogCast/generated.go').read_bytes(),b'generated\n')
        self.assertEqual((self.root/'sources/FogCast/fixtures/current.json').read_bytes(),b'canonical')
        self.assertEqual((self.root/'sources/FogCast/runtime.json').read_bytes(),b'runtime canonical')
        for name,data in before.items():
            if name.startswith('sources/mister-packages/') or name.endswith('/canonical.json'):
                self.assertEqual((self.root/name).read_bytes(),data)
        again=generate.regenerate(self.root,True,self.emit)
        self.assertEqual((again['updated'],again['deleted']),(0,0))

    def test_check_reports_mismatch_without_mutation(self):
        before=self.snapshot()
        with patch.object(generate.consistency,'_run',side_effect=self.emit):
            with self.assertRaisesRegex(ValueError,'generated .* differs'):
                generate.regenerate(self.root)
        self.assertEqual(self.snapshot(),before)
        generate.regenerate(self.root,True,self.emit)
        with patch.object(generate.consistency,'_run',side_effect=self.emit):
            self.assertEqual(generate.regenerate(self.root)['generated_files'],1)

    def test_all_emission_completes_before_any_mutation(self):
        before=self.snapshot()
        def fail(*args):
            raise ValueError('emission failed')
        with self.assertRaisesRegex(ValueError,'emission failed'):
            generate.regenerate(self.root,True,fail)
        self.assertEqual(self.snapshot(),before)

    def test_rejects_gitlink_even_if_directory_exists(self):
        subprocess.run(['git','-C',str(self.root),'rm','-r','--cached','-q','sources/FogCast'],check=True)
        subprocess.run(['git','-C',str(self.root),'update-index','--add','--cacheinfo',
                        '160000','1'*40,'sources/FogCast'],check=True)
        with self.assertRaisesRegex(ValueError,'tracked modules'):
            generate.regenerate(self.root,True,self.emit)

    def test_symlink_and_escape_rejected_before_writes(self):
        target=self.root/'sources/FogCast/generated.go'
        target.unlink();target.symlink_to(self.root/'sources/libmister-runtime/canonical.json')
        for write in (False,True):
            with self.assertRaisesRegex(ValueError,'symlink'):
                generate.regenerate(self.root,write,self.emit)
        target.unlink()
        with patch.object(generate.consistency,'GENERATED',(('emit-go','definition.yaml','FogCast','../../escape'),)):
            with self.assertRaisesRegex(ValueError,'unsafe generated path'):
                generate.regenerate(self.root,True,self.emit)

    def test_stale_fixture_symlink_is_not_followed_or_deleted(self):
        target=self.root/'sources/FogCast/fixtures/link'
        target.symlink_to(self.root/'sources/mister-packages/fixtures',target_is_directory=True)
        with self.assertRaisesRegex(ValueError,'symlink'):
            generate.regenerate(self.root,True,self.emit)
        self.assertTrue(target.is_symlink())

    def test_file_ancestor_conflict_fails_before_mutation(self):
        self.file('sources/FogCast/conflict', b'not a directory')
        before=self.snapshot()
        with patch.object(generate.consistency,'COPIED_FILES',(('fixtures/current.json','FogCast','conflict/member'),)):
            with self.assertRaisesRegex(ValueError,'ancestor is not a directory'):
                generate.regenerate(self.root,True,self.emit)
        self.assertEqual(self.snapshot(),before)



if __name__=='__main__':
    unittest.main()
