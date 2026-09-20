import copy
import json
from pathlib import Path
import shutil
import stat
import tempfile
import unittest
from unittest.mock import patch

from scripts import artifact_cache


class ArtifactCacheTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.addCleanup(self.cleanup)
        self.root=Path(self.temp.name)
        self.cache=self.root/'cache'
        self.fields={'format':2,'repository':'repo','revision':'a'*40,'source_path':'sources/misteross',
                     'source_inputs':{'rtl/top.v':'1'*64},'tools':{'yosys':'v1'},
                     'execution':{'seed':0,'argv':['--device','test']},'recipe_sha256':'2'*64}
        self.record=self.root/'build-inputs.json'
        self.record.write_bytes(self.encode(self.fields))
        self.package=self.root/('b'*64)
        self.package.mkdir()
        (self.package/'manifest.toml').write_bytes(b'original manifest')
        (self.package/'core.rbf').write_bytes(b'original bitstream')

    def cleanup(self):
        if hasattr(self,'root'):
            for path in self.root.rglob('*'):
                if not path.is_symlink():
                    path.chmod(0o755 if path.is_dir() else 0o644)
        self.temp.cleanup()

    def encode(self,fields):
        return (json.dumps(fields,sort_keys=True)+'\n').encode()

    def test_key_excludes_only_provenance(self):
        key=artifact_cache.functional_key(self.record.read_bytes())
        for field in ('repository','revision','source_path'):
            changed=copy.deepcopy(self.fields);changed[field]='different'
            self.assertEqual(artifact_cache.functional_key(self.encode(changed)),key)
        for field,value in [('source_inputs',{'rtl/top.v':'3'*64}),('tools',{'yosys':'v2'}),
                            ('execution',{'seed':1}),('recipe_sha256','4'*64),('new_behavior',True)]:
            changed=copy.deepcopy(self.fields);changed[field]=value
            self.assertNotEqual(artifact_cache.functional_key(self.encode(changed)),key)
        self.assertEqual(artifact_cache.functional_key(json.dumps(self.fields,indent=4)),key)
        self.assertIsNone(artifact_cache.functional_key(b'{"format":1}'))
        self.assertIsNone(artifact_cache.functional_key(b'{"format":2,"source_inputs":{}}'))

    def test_publication_is_sealed_complete_and_idempotent(self):
        result=artifact_cache.publish(self.cache,self.record,self.package)
        entry=result.parent
        self.assertEqual({p.name for p in entry.iterdir()},{self.package.name,'build-inputs.json'})
        self.assertEqual({p.name for p in result.iterdir()},{'manifest.toml','core.rbf'})
        self.assertEqual((entry/'build-inputs.json').read_bytes(),self.record.read_bytes())
        for name in ('manifest.toml','core.rbf'):
            self.assertEqual((result/name).read_bytes(),(self.package/name).read_bytes())
        for path in (entry,result,*result.iterdir(),entry/'build-inputs.json'):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode)&0o222,0)
        inode=(result/'core.rbf').stat().st_ino
        self.assertEqual(artifact_cache.publish(self.cache,self.record,self.package),result)
        self.assertEqual((result/'core.rbf').stat().st_ino,inode)
        (self.package/'core.rbf').write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError,'differs'):
            artifact_cache.publish(self.cache,self.record,self.package)
        self.assertEqual((result/'core.rbf').read_bytes(),b'original bitstream')

    def test_original_provenance_cannot_be_relabelled(self):
        result=artifact_cache.publish(self.cache,self.record,self.package)
        original=(result.parent/'build-inputs.json').read_bytes()
        selected=copy.deepcopy(self.fields);selected['revision']='c'*40
        self.record.write_bytes(self.encode(selected))
        with self.assertRaisesRegex(ValueError,'differs'):
            artifact_cache.publish(self.cache,self.record,self.package)
        self.assertEqual((result.parent/'build-inputs.json').read_bytes(),original)

    def test_failed_staging_does_not_publish_partial_entry(self):
        original=Path.write_bytes
        def write(path,data):
            if path.name=='core.rbf' and '.publish-' in str(path):
                raise OSError('simulated write failure')
            return original(path,data)
        with patch.object(Path,'write_bytes',write):
            with self.assertRaisesRegex(OSError,'simulated'):
                artifact_cache.publish(self.cache,self.record,self.package)
        store=artifact_cache.store_for(self.cache,self.record.read_bytes())
        self.assertFalse((store/self.package.name).exists())
        self.assertEqual([p.name for p in store.iterdir()],['.publish.lock'])
        self.assertTrue(artifact_cache.publish(self.cache,self.record,self.package).is_dir())

    def test_rename_exposes_both_record_and_payload_together(self):
        original=Path.rename
        def rename(path,target):
            target=Path(target)
            self.assertFalse(target.exists())
            self.assertEqual((path/'build-inputs.json').read_bytes(),self.record.read_bytes())
            self.assertEqual((path/self.package.name/'core.rbf').read_bytes(),b'original bitstream')
            return original(path,target)
        with patch.object(Path,'rename',rename):
            artifact_cache.publish(self.cache,self.record,self.package)

    def test_rejects_linked_root_and_source_payload(self):
        actual=self.root/'actual';actual.mkdir()
        self.cache.symlink_to(actual,target_is_directory=True)
        with self.assertRaises(ValueError):
            artifact_cache.publish(self.cache,self.record,self.package)
        self.cache.unlink()
        payload=self.package/'core.rbf';payload.unlink();payload.symlink_to(self.record)
        with self.assertRaises(ValueError):
            artifact_cache.publish(self.cache,self.record,self.package)

    def test_existing_linked_package_directory_is_rejected(self):
        result=artifact_cache.publish(self.cache,self.record,self.package)
        result.parent.chmod(0o755)
        result.chmod(0o755)
        shutil.rmtree(result)
        result.symlink_to(self.package,target_is_directory=True)
        with self.assertRaises(ValueError):
            artifact_cache.publish(self.cache,self.record,self.package)

    def test_existing_entry_rejects_extra_members(self):
        result=artifact_cache.publish(self.cache,self.record,self.package)
        result.parent.chmod(0o755)
        (result.parent/'unexpected').write_bytes(b'not authenticated')
        with self.assertRaises(ValueError):
            artifact_cache.publish(self.cache,self.record,self.package)


if __name__=='__main__':
    unittest.main()
