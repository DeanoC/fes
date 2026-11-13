"""Actual producer metadata and identity; synthetic payloads, no FPGA tools."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import uuid

ROOT = Path(__file__).resolve().parents[1]
PRODUCER = ROOT / 'sources/misteross/prototypes/de25_board_control'
PREAMBLE = '''
import copy,hashlib,json,sys
from pathlib import Path
sys.path.insert(0,str(Path('sources/misteross/prototypes/de25_board_control').resolve()))
import seal,rom_transport,reader_memory
def inputs(model='zx81'):
 return dict(device=seal.DEVICE,diagnostic_only=True,
  functional_snapshot_sha256={'synthetic-source': '1'*64},
  board_fes_revision=rom_transport.BOARD_REVISION,overlay_source_state='committed',
  compile_processors=2,vendor_archive_sha256='2'*64,computer_model=model,
  reader_memory=reader_memory.selection('hps-ddr',0x14800000,0x800000),
  computer={'hps_initialization':'HPS FIRST'},
  rom_transport=rom_transport.selection(True,model),package_contract='fes.computer')
def record(i):
 return seal.make_record(i,'1'*40,'https://github.com/DeanoC/fes',{'quartus-version':'synthetic fixture; no Quartus execution'})
'''


class NanoComputerPackageTests(unittest.TestCase):
    def run_source(self, source, *args):
        result = subprocess.run([sys.executable, '-c', PREAMBLE + source, *map(str, args)],
                                cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                text=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_actual_metadata_and_pre_synthesis_identity(self):
        with tempfile.TemporaryDirectory(prefix='nano-computer-packages-') as temporary:
            destination = Path(os.environ.get('FES_NANO_COMPUTER_FIXTURE_OUTPUT', temporary))
            result = self.run_source('''
destination=Path(sys.argv[1]);destination.mkdir(parents=True,exist_ok=True)
out=[]
for model in ('zx80','zx81'):
 i=inputs(model);r=record(i);fields=json.loads(r)
 diagnostic=copy.deepcopy(i);del diagnostic['package_contract'];old=record(diagnostic)
 assert seal.build_identity(r)!=seal.build_identity(old)
 assert fields['abi_definition']==seal.COMPUTER_DEFINITION
 assert seal.COMPUTER_REGISTRY in fields['source_inputs']
 assert 'sources/mister-packages/packages/abi/fes_computer.yaml' in fields['source_inputs']
 assert fields['parameters']['package_contract']=='fes.computer'
 data=bytes(range(128));tools={'quartus-version':'synthetic fixture; no Quartus execution'}
 manifest,mapping=seal.package_metadata(i,data,r,tools)
 directory=destination/model;directory.mkdir()
 for name,value in [('manifest.toml',manifest),('core.rbf',data),('rom-map.json',mapping)]:
  (directory/name).write_bytes(value)
 package=seal.read_package(directory)
 assert package.fields['core']['id']=='fes.'+model
 assert package.fields['abi']=={'id':'fes.computer','major':1,'minor':0}
 assert len(package.fields['interfaces'])==4
 assert package.fields['target']['programming_profile']==seal.COMPUTER_PROFILE
 assert package.fields['rom']['source_size']==(4096 if model=='zx80' else 8192)
 assert package.payload_bytes==data
 for selection,wrong_record in [(i,old),(diagnostic,r)]:
  try:seal.package_metadata(selection,data,wrong_record,tools)
  except ValueError:pass
  else:raise AssertionError('post-synthesis package relabelling accepted')
 out.append(dict(model=model,package_id=package.package_id,build_id=seal.build_identity(r),
                 diagnostic_build_id=seal.build_identity(old),record_sha256=seal.sha(r)))
print(json.dumps(out))
''', destination)
            self.assertEqual([v['model'] for v in result], ['zx80', 'zx81'])
            self.assertNotEqual(result[0]['build_id'], result[1]['build_id'])
            # Exercise the generated native registry, real archive parser and
            # retained ROM linker against the producer's actual output.
            env = dict(os.environ, GOWORK='off', FES_NANO_COMPUTER_FIXTURES=str(destination))
            for key in ('GOOS', 'GOARCH', 'GOARM', 'GOFLAGS', 'GOOSPKG'):
                env.pop(key, None)
            consumer = subprocess.run(['go', '-C', str(ROOT / 'platform/embedded/tamago'), 'test', '-count=1',
                                       '-run', '^TestNanoComputerProducerMetadataAndROMConsumption$', './runtimeadmission'],
                                      cwd=ROOT, env=env, capture_output=True, text=True, timeout=90)
            self.assertEqual(consumer.returncode, 0, consumer.stdout + consumer.stderr)
            if os.environ.get('FES_NANO_COMPUTER_FIXTURE_OUTPUT'):
                (destination.parent / 'producer-fixtures.json').write_text(json.dumps(result, indent=2) + '\n')

    def test_wrong_source_variants_cannot_select_computer_package(self):
        result = self.run_source('''
bad=[]
for change in [lambda i:i.update(package_contract='unknown'),lambda i:i.update(rom_transport=None),
 lambda i:i.update(board_fes_revision='0'*40),
 lambda i:i.update(reader_memory=reader_memory.selection('onchip',0,0)),
 lambda i:i['computer'].update(hps_initialization='AFTER INIT_DONE')]:
 i=inputs();change(i)
 try:seal.computer_package(i)
 except ValueError:bad.append(True)
 else:raise AssertionError('unreviewed computer variant accepted')
print(json.dumps(bad))
''')
        self.assertEqual(result, [True] * 5)

    def test_build_options_reject_before_creating_a_project(self):
        for extra, message in [
            ([], 'explicit held-ROM'),
            (['--external-rom', '--hps-first', '--reader-memory', 'hps-ddr', '--ddr-bytes', '4096', '--allow-dirty'], 'committed source'),
            (['--sealed-diagnostic'], 'not allowed with argument'),
        ]:
            with self.subTest(extra=extra):
                destination = ROOT / 'out' / ('nano-computer-reject-' + uuid.uuid4().hex)
                result = subprocess.run([sys.executable, PRODUCER / 'build.py', 'build', '--sealed-computer',
                                         '--board-revision', '79d616e85e19d65f0d645b67a85ffe7e142dcdfc',
                                         '--vendor-archive', '/nonexistent/nano-vendor.zip', '--output', destination, *extra],
                                        cwd=ROOT, capture_output=True, text=True, timeout=15)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertFalse(destination.exists())


if __name__ == '__main__':
    unittest.main()
