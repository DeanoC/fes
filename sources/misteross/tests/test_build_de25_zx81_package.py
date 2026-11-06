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
from scripts import build_de25_zx81_static_package as static
from scripts.export_core_package import build_identity,verify_record_source_at_revision

ROOT=Path(__file__).resolve().parents[1]


class ZX81ProducerTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='de25-zx81-producer-test-')
        self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        data={producer.RECIPE:b'# fixture recipe\n',producer.ABI:b'fixture ABI',
              'cores/fes-zx81/rtl/machine.v':b'fixture machine',
              'cores/fes-common/rtl/z80/engine.sv':b'fixture CPU',
              'boards/de25nano/fixture.v':b'fixture board',
              'cores/fes-menu/rtl/fixture.v':b'fixture reader',
              'cores/fes-splash/rtl/fixture.v':b'fixture display'}
        data.update({name:b'fixture shared library' for name in producer.COMMON_LIBRARY_INPUTS})
        data.update({name:b'# fitted review fixture\n' for name in producer.REVIEW_INPUTS})
        data.update({name:b'# static review fixture\n' for name in static.REVIEW_INPUTS})
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

    def test_static_record_has_independent_control_identity_and_no_constant_pcm_policy(self):
        def record(controller='1' * 32, placement='X2 Y7 X26 Y12'):
            return static.create_build_record(self.root, 'https://github.com/DeanoC/fes', self.revision,
                self.tools, self.execution, self.captured, control_build_id=controller,
                placement=placement, routing='X1 Y6 X27 Y13')
        raw = record(); fields = verify_record_source_at_revision(self.root, raw)
        p = fields['parameters']
        self.assertEqual(p['programming_profile'], static.PROGRAMMING_PROFILE)
        self.assertTrue(p['expansion_socket'])
        self.assertEqual(p['static_default_persona_id'], 0xc001a001)
        self.assertEqual(p['signoff_policy'], static.SIGNOFF_POLICY)
        self.assertTrue(set(static.REVIEW_INPUTS) <= set(fields['source_inputs']))
        self.assertNotEqual(build_identity(raw), p['static_control_build_id'])
        for name in ('audio_clock_reference_policy', 'hdmi_output_policy', 'internal_ram_kib'):
            self.assertNotIn(name, p)
        self.assertNotEqual(build_identity(raw), build_identity(record('2' * 32)))
        self.assertNotEqual(build_identity(raw), build_identity(record(placement='X3 Y7 X26 Y12')))
        with self.assertRaises(producer.BuildError): record('not-an-id')
        with self.assertRaises(producer.BuildError): record(placement='X2 Y7 X26 Y12; ignored')

    def test_static_manifest_post_fit_seals_do_not_change_the_functional_id(self):
        record = static.create_build_record(self.root, 'https://github.com/DeanoC/fes', self.revision,
            self.tools, self.execution, self.captured, control_build_id='1' * 32,
            placement='X2 Y7 X26 Y12', routing='X1 Y6 X27 Y13')
        base = self.root / 'core.rbf'; base.write_bytes(b'synthetic base, never deploy')
        persona = self.root / 'persona.rbf'; persona.write_bytes(b'synthetic empty persona, never deploy')
        shell = dict(control_build_id='1' * 32, root_qdb_sha256='2' * 64,
            interface_sha256='3' * 64, clock_reset_sha256='4' * 64, vendor_pof={'hash': '5' * 64})
        first = static.manifest(record, base, persona, {'shell': shell})
        fields = tomllib.loads(first.decode())
        self.assertEqual(fields['format'], 5)
        self.assertEqual(fields['build']['id'], build_identity(record))
        self.assertEqual(fields['static_expansion']['control_build_id'], '1' * 32)
        self.assertEqual(fields['target']['programming_profile'], static.PROGRAMMING_PROFILE)
        shell['root_qdb_sha256'] = '6' * 64
        second = static.manifest(record, base, persona, {'shell': shell})
        self.assertNotEqual(first, second)
        self.assertEqual(tomllib.loads(second.decode())['build']['id'], fields['build']['id'])

    def test_static_selection_requires_explicit_regions_before_any_build(self):
        with mock.patch.object(static, 'builder', side_effect=AssertionError('builder ran')):
            with self.assertRaisesRegex(producer.BuildError, 'placement and --routing'):
                producer.build(quartus_root='', vendor_archive=Path('unopened.zip'), static_expansion=True)
        with self.assertRaisesRegex(producer.BuildError, 'require --static-expansion'):
            producer.build(quartus_root='', vendor_archive=None, placement='X2 Y7 X26 Y12')

    def test_capture_contains_the_real_profile_and_hdmi_helpers(self):
        captured=producer.computer(ROOT).capture_standard_board()
        for name in ('zx81-hps/managed_bram.py','scanout/hdmi_inputs.py','scanout/build.py','hps/build.py',
                     'zx81-hps/fes_de25_zx81_standard.v','zx81-hps/fes_de25_audio.v','zx81-hps/standard-clocks.sdc'):
            key='sources/misteross/boards/de25nano/'+name
            self.assertEqual(captured[key],(ROOT/'boards/de25nano'/name).read_bytes())


if __name__=='__main__':unittest.main()
