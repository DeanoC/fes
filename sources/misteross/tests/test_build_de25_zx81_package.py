"""Canonical standard ZX81 identity/contract and fail-closed profile selection.

Temporary Git/ELF/report markers below exercise source and manifest boundaries;
they are deliberately unqualified and never stand in for a compiled package.
"""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock
from scripts import build_de25_zx81_package as producer
from scripts.export_core_package import build_identity,verify_record_source_at_revision

ROOT=Path(__file__).resolve().parents[1]


class ZX81ProducerTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='de25-zx81-producer-test-')
        self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        data={producer.RECIPE:b'fixture recipe',producer.ABI:b'fixture ABI',
              'cores/fes-zx81/rtl/machine.v':b'fixture machine',
              'cores/fes-common/rtl/z80/engine.sv':b'fixture CPU',
              'boards/de25nano/fixture.v':b'fixture board',
              'cores/fes-menu/rtl/fixture.v':b'fixture reader',
              'cores/fes-splash/rtl/fixture.v':b'fixture display'}
        data.update({name:b'fixture shared library' for name in producer.COMMON_LIBRARY_INPUTS})
        for name,value in data.items():
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(value)
        for command in (['git','init','-q'],['git','add','.'],
                        ['git','-c','user.name=Test','-c','user.email=test@example.invalid','commit','-qm','fixture']):
            subprocess.run(command,cwd=self.root,check=True)
        self.revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=self.root,text=True).strip()
        self.captured={'sources/misteross/'+name:value for name,value in data.items()}
        self.tools={name:'explicit fixture '+name for name in ('quartus','qsys','ipgenerate','pfg')}
        self.execution={'toolchain':'quartus','fixture':True}

    def record(self):
        return producer.create_build_record(self.root,'https://github.com/DeanoC/fes',self.revision,
                                             self.tools,self.execution,self.captured)

    def test_record_binds_standard_machine_and_allocated_memory_bridge(self):
        record=self.record();value=verify_record_source_at_revision(self.root,record)
        self.assertEqual(value['parameters']['core_id'],'fes.zx81')
        self.assertEqual(value['parameters']['fabric_initiator_policy'],producer.FABRIC)
        self.assertEqual((value['parameters']['f2s_data_width'],value['parameters']['f2sdram_data_width']),(0,256))
        self.assertEqual(value['parameters']['hps_initialization'],'HPS FIRST')
        self.assertTrue(value['parameters']['standard_core'])
        self.assertEqual(value['parameters']['programming_profile'],'de25-linux-computer-v1')
        self.assertEqual(value['parameters']['internal_ram_kib'],16)
        self.assertFalse(value['parameters']['expansion_socket'])
        self.assertEqual(value['parameters']['reset_timing_policy'],producer.common.TIMED_RESET_RELEASE)
        changed=dict(self.tools,quartus='changed actual compiler')
        other=producer.create_build_record(self.root,'https://github.com/DeanoC/fes',self.revision,changed,self.execution,self.captured)
        self.assertNotEqual(build_identity(record),build_identity(other))
        name='sources/misteross/cores/fes-common/rtl/z80/engine.sv';self.captured[name]=b'changed'
        with self.assertRaisesRegex(producer.BuildError,'differs from canonical closure'):self.record()

    def test_manifest_matches_standard_allocated_session_contract(self):
        payload=self.root/'core.rbf';payload.write_bytes(b'unqualified payload fixture')
        manifest=tomllib.loads(producer.manifest(self.record(),payload).decode())
        self.assertEqual(manifest['format'],2)
        self.assertEqual(manifest['target'],{'platform':'de25_nano','device':producer.DEVICE,'programming_profile':'de25-linux-computer-v1'})
        self.assertEqual(manifest['abi'],{'id':'fes.computer','major':1,'minor':0})
        self.assertEqual({v['id'] for v in manifest['interfaces']},
                         {'fes.video.fixed-720p60','fes.keyboard.hid','fes.gamepad.ports',
                          'fes.audio.pcm-s16-stereo-48k','fes.media.zx81-tape','fes.media.zx81-tape-write',
                          'fes.video.session-display','fes.fabric.video.display-plane','fes.fabric.de25-scanout'})
        self.assertNotIn('fes.memory.hps-ddr',{i['id'] for i in manifest['interfaces']})
        self.assertEqual(next(i['minor'] for i in manifest['interfaces'] if i['id']=='fes.fabric.de25-scanout'),1)
        self.assertNotIn('roms',manifest)

    def test_cli_and_wrong_pinned_inputs_refuse_before_any_build(self):
        with mock.patch.object(producer,'computer',side_effect=AssertionError('builder ran')):
            with self.assertRaisesRegex(producer.BuildError,'requires --vendor-archive'):
                producer.build(quartus_root='',vendor_archive=None)
            with self.assertRaisesRegex(producer.BuildError,'pinned recipe'):
                producer._require_clean_source(self.root,pinned_inputs=('foreign','ABI'))

    def test_capture_contains_the_real_profile_and_hdmi_helpers(self):
        captured=producer.computer(ROOT).capture_standard_board()
        for name in ('zx81-hps/managed_bram.py','scanout/hdmi_inputs.py','scanout/build.py','hps/build.py',
                     'zx81-hps/fes_de25_zx81_standard.v','zx81-hps/fes_de25_audio.v','zx81-hps/standard-clocks.sdc'):
            key='sources/misteross/boards/de25nano/'+name
            self.assertEqual(captured[key],(ROOT/'boards/de25nano'/name).read_bytes())


if __name__=='__main__':unittest.main()
