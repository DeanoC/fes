"""Repeat-load boot selection requires the compiled CPU and firmware closure."""
import copy
import tempfile
import struct
import subprocess
import sys
from pathlib import Path
import unittest

import test_nano_sdm_input_profile as fixture
mode, profile = fixture.mode, fixture.profile


def warm_profile():
    p = profile()
    for name in ('selected_programs', 'tenfoot_serial', 'computer_reload',
                 'computer_storage_return'):
        p[name] = True
    p['window_storage_return']=False  # Paired MENU return derives from its compiled companion.
    native = dict(p, native_program='fpgaload',
                  native_build_sources=mode.rearm.expected_cpu_binding()['sources'])
    p['sdm_cpu_rearm'] = mode.rearm.native_binding(native)
    p['sdm_input_transfer'] = mode.selected(p, True, source_rearm=True)
    return p


class RearmProfileTests(unittest.TestCase):
    def test_build_clis_reject_rearm_without_input_before_creating_output(self):
        for builder in ('build_terasic.py','build_terasic_uboot.py'):
            with tempfile.TemporaryDirectory() as directory:
                output=Path(directory)/'candidate'
                command=[sys.executable,str(mode.HERE/builder),'--reference-repo',directory,
                         '--cross-prefix',directory+'/missing-tool-', '--output',str(output),
                         '--nano-sdm-source-rearm']
                if builder=='build_terasic_uboot.py':command+=['--bl31-build',directory]
                result=subprocess.run(command,capture_output=True,text=True,timeout=20)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('source rearm requires selected SDM input transfer',result.stderr)
                self.assertFalse(output.exists())

    def test_rearm_elf_requires_real_strong_functions(self):
        name='fes_nano_platform_release_source'
        def image(info=0x12,index=1,address=0x1000,size=4,flags=6):
            data=bytearray(1024);data[:6]=b'\x7fELF\x02\x01'
            struct.pack_into('<Q',data,40,64);struct.pack_into('<HH',data,58,64,4)
            strings=b'\0'+name.encode()+b'\0';data[544:544+len(strings)]=strings
            struct.pack_into('<IIQQQQIIQQ',data,128,0,1,flags,0x1000,512,16,0,0,4,0)
            struct.pack_into('<IIQQQQIIQQ',data,192,0,3,0,0,544,len(strings),0,0,1,0)
            struct.pack_into('<IIQQQQIIQQ',data,256,0,2,0,0,600,24,2,0,8,24)
            struct.pack_into('<IBBHQQ',data,600,1,info,0,index,address,size)
            return bytes(data)
        self.assertEqual(mode.strong_function(image(),name),dict(address=0x1000,bytes=4))
        for edits in (dict(info=0x22),dict(info=2),dict(index=0),dict(address=0),dict(size=0),dict(flags=0)):
            with self.subTest(edits=edits),self.assertRaises(ValueError):mode.strong_function(image(**edits),name)

    def test_rearm_is_separate_from_the_retained_first_menu(self):
        self.assertEqual(mode.checked(profile())['format'], 14)
        self.assertIn('FES_NANO_SDM_SOURCE_RELEASE 0\n', mode.header(profile()))
        p = warm_profile()
        self.assertEqual(mode.checked(p), mode.REARM_CONTRACT)
        self.assertEqual(mode.checked(p)['format'], 15)
        self.assertIn('FES_NANO_SDM_SOURCE_RELEASE 1\n', mode.header(p))
        self.assertFalse(mode.checked(p)['retirement_memory_reuse'])
        for flag in (None, 1, {}, 'yes'):
            with self.subTest(flag=flag), self.assertRaises(ValueError):
                mode.selected(p, True, source_rearm=flag)
        with self.assertRaises(ValueError):
            mode.selected(p, False, source_rearm=True)
        with self.assertRaises(ValueError):
            mode.checked(dict(p, sdm_input_transfer=mode.CONTRACT))

    def test_cpu_source_and_each_ownership_selection_are_required(self):
        p = warm_profile()
        native = dict(p, native_program='fpgaload',
                      native_build_sources=copy.deepcopy(p['sdm_cpu_rearm']['sources']))
        self.assertEqual(mode.rearm.native_binding(native), p['sdm_cpu_rearm'])
        for name in ('configuration_retained', 'selected_programs', 'tenfoot_serial',
                     'computer_reload', 'computer_storage_return', 'hps_menu_program'):
            with self.subTest(missing=name), self.assertRaises(ValueError):
                mode.rearm.native_binding(dict(native, **{name: False}))
            with self.assertRaises(ValueError):
                mode.checked(dict(p, **{name: False}))
        for name in mode.rearm.CPU_FILES:
            for digest in (None, '0'*64):
                changed = copy.deepcopy(native)
                changed['native_build_sources'][name] = digest
                with self.subTest(source=name, digest=digest), self.assertRaises(ValueError):
                    mode.rearm.native_binding(changed)
        for name in mode.REARM_CONTRACT:
            changed = copy.deepcopy(p)
            changed['sdm_input_transfer'][name] = 'foreign'
            with self.subTest(contract=name), self.assertRaises(ValueError):
                mode.checked(changed)
        for name in p['sdm_cpu_rearm']['sources']:
            changed = copy.deepcopy(p)
            changed['sdm_cpu_rearm']['sources'][name] = '0'*64
            with self.subTest(binding=name), self.assertRaises(ValueError):
                mode.header(changed)

    def test_export_is_bound_to_the_selected_warm_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            args = fixture.InputProfileTests().fixture(path)
            p = warm_profile()
            (path/mode.HEADER).write_text(mode.header(p)+'#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n')
            binding = mode.install(path, p, *args)
            self.assertEqual(binding['contract'], mode.REARM_CONTRACT)
            mode.verify_installed(path, binding, p)
            with self.assertRaises(ValueError):
                mode.verify_installed(path, binding, profile())
            changed = copy.deepcopy(binding)
            changed['contract'] = mode.CONTRACT
            with self.assertRaises(ValueError):
                mode.verify_installed(path, changed, p)
