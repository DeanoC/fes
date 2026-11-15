"""Actual firmware scope and source-derived computer contract; no hardware."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
spec=importlib.util.spec_from_file_location('computer_context',ROOT/'platform/embedded/nano/hps_context.py')
context=importlib.util.module_from_spec(spec);spec.loader.exec_module(context)
spec=importlib.util.spec_from_file_location('computer_cold',ROOT/'platform/embedded/nano/cold_profile.py')
cold=importlib.util.module_from_spec(spec);spec.loader.exec_module(cold)


class ComputerTests(unittest.TestCase):
    def test_actual_cold_scope_and_owned_codec(self):
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'computer'
            compiled=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                            '-fno-sanitize-recover=all','-I'+str(FW),str(FW/'nano_bridges.c'),
                            str(FW/'nano_bridge_sip.c'),str(FW/'tests/nano_computer_control_test.c'),'-o',str(binary)],
                           capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in range(21):
                with self.subTest(mutation=mutation):
                    p=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True,timeout=5)
                    self.assertEqual(p.returncode,0,p.stderr+p.stdout)
                    self.assertIn('PASS',p.stdout)

    def test_computer_receipt_requires_exact_source_and_no_active_ddr(self):
        m=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-computer.json').read_text())
        record={'program':'fpgaload','hps_project':'fixture','hps_computer':m,'hps_computer_sha256':context.digest(m)}
        with patch.object(context,'checked_configuration') as checked,patch.object(context,'computer',return_value=m):
            self.assertEqual(context.checked_computer(record),m)
            checked.assert_called_once_with(record,'program')
            self.assertEqual(context.checked_computer(dict(record,computer_reload=True)),m)
            self.assertEqual(context.checked_computer(dict(record,computer_reload=True,computer_storage_return=True)),m)
            with self.assertRaises(ValueError):context.checked_computer(dict(record,computer_storage_return=True))
            for storage_return in (None,0,1,'true'):
                with self.subTest(storage_return=storage_return),self.assertRaises(ValueError):
                    context.checked_computer(dict(record,computer_reload=True,computer_storage_return=storage_return))
            for flag in (None,0,1,'true'):
                with self.subTest(reload=flag),self.assertRaises(ValueError):
                    context.checked_computer(dict(record,computer_reload=flag))
            for bad in (dict(record,program='tenfoot'),dict(record,hps_ddr_read={}),
                        dict(record,hps_computer_sha256='0'*64),dict(record,hps_computer=None)):
                with self.assertRaises(ValueError):context.checked_computer(bad)
        self.assertIsNone(context.checked_computer({}))
        with self.assertRaises(ValueError):context.checked_computer({'computer_reload':True})

    def test_cold_header_grants_computer_only_with_sealed_contract(self):
        data=ROOT/'platform/embedded/tamago/nativefabric/testdata'
        hps=json.loads((data/'hps-context.json').read_text());control=json.loads((data/'hps-control.json').read_text())
        computer=json.loads((data/'hps-computer.json').read_text())
        p={'sd_pio':True,'chain_sha256':'1'*64,'owner_seed':17,'entry_image_sha256':'2'*64,'entry_image_size':128,
           'hps_context':hps,'hps_context_sha256':context.digest(hps),
           'hps_control':control,'hps_control_sha256':context.digest(control),
           'hps_configuration':{'base':0x94800000,'size':8<<20,'core_size':128,'chunk_bytes':64,'core_sha384':'3'*96,'chunk_sha384':['4'*96,'5'*96]},
           'hps_computer':computer,'hps_computer_sha256':context.digest(computer)}
        p['cpu_mapping']=context.cpu_mapping(control,p['hps_configuration'],None)
        p['cpu_mapping_sha256']=context.digest(p['cpu_mapping'])
        self.assertIn('#define FES_NANO_COLD_COMPUTER 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(p)} ))
        self.assertIn('#define FES_NANO_COMPUTER_RELOAD 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,computer_reload=True))} ))
        self.assertIn('#define FES_NANO_COMPUTER_STORAGE_RETURN 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,computer_reload=True,computer_storage_return=True))} ))
        with self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,computer_storage_return=True))} )
        for flag in (None,0,1,'true'):
            with self.subTest(reload=flag),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,computer_reload=flag))} )
        with self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,hps_computer=None,computer_reload=True))} )
        self.assertIn('#define FES_NANO_COLD_COMPUTER 0',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,hps_computer=None))} ))
        for key,value in (('cpu_base',0),('span',True),('abi_minor',1),('capabilities',1),('hps_master',True),
                          ('endpoint_sha256','0'*64),('control_map_sha256','3'*64),('wrapper_sha256','bad')):
            bad=dict(computer,**{key:value})
            with self.subTest(key=key),self.assertRaises(ValueError):
                cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,hps_computer=bad,hps_computer_sha256=context.digest(bad)))} )
        with self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,hps_configuration=None))} )


if __name__=='__main__':unittest.main()
