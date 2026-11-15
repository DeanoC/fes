"""Reader provenance and bank separation without physical qualification."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/firmware'),str(ROOT/'platform/embedded/nano')]
import nano_sdm_reader_profile as reader
from test_nano_sdm_input_profile import profile

class ReaderProfileTests(unittest.TestCase):
    def test_exact_role_and_route_binding_rejects_mutated_or_legacy_records(self):
        p=profile();p['sdm_reader_route']=reader._expected(p)
        self.assertEqual(reader.checked(p),p['sdm_reader_route'])
        for key in reader.CONTRACT:
            q=copy.deepcopy(p);q['sdm_reader_route']['contract'][key]='changed'
            with self.subTest(key=key), self.assertRaises(ValueError):reader.checked(q)
        for key in p['sdm_reader_route']:
            q=copy.deepcopy(p);del q['sdm_reader_route'][key]
            with self.subTest(missing=key), self.assertRaises(ValueError):reader.checked(q)
        for key in ('hps_context','hps_menu_program','hps_program_platform','sdm_reader_route'):
            q=copy.deepcopy(p);del q[key]
            with self.subTest(profile=key), self.assertRaises(ValueError):reader.checked(q)
        q=copy.deepcopy(p);q['hps_menu_program']['context']['package_id']='9'*64
        with self.assertRaises(ValueError):reader.checked(q)
        q=copy.deepcopy(p);q['hps_menu_program']['platform']['platform']['hps_io_hash']='9'*64
        with self.assertRaises(ValueError):reader.checked(q)
        for key in ('source_retained','physical_qualification','register_writes','memory_reuse'):
            q=copy.deepcopy(p);q['sdm_reader_route']['contract'][key]=int(q['sdm_reader_route']['contract'][key])
            with self.subTest(boolean=key), self.assertRaises(ValueError):reader.checked(q)

    def test_complete_reviewed_files_not_matching_strings_supply_route(self):
        with tempfile.TemporaryDirectory(dir=ROOT/'out',prefix='reader-source-') as directory:
            project=Path(directory);base=project/'board/hps/project'
            ip=base/reader.IP;wrapper=base/reader.WRAPPER
            ip.parent.mkdir(parents=True);wrapper.parent.mkdir(parents=True)
            def xml(values):
                return ('<r xmlns:i="http://www.accellera.org/XMLSchema/IPXACT/1685-2014">'+''.join(
                    '<i:parameter parameterId="'+k+'"><i:value>'+v+'</i:value></i:parameter>' for k,v in values.items())+'</r>').encode()
            ip.write_bytes(xml(reader.PARAMETERS));original=b'module reviewed_wrapper; endmodule\n';wrapper.write_bytes(original)
            fixture=dict(reader.CONTRACT,ip_sha256=hashlib.sha256(ip.read_bytes()).hexdigest(),wrapper_sha256=hashlib.sha256(original).hexdigest())
            with patch.object(reader,'CONTRACT',fixture):
                p=profile();p['sdm_reader_route']=reader.derive(project,project,p)
                self.assertEqual(reader.checked(p),p['sdm_reader_route'])
                for changed in (original+b"// 17'h3; f2sdram_aruser\n", original.replace(b'endmodule',b'wire other_driver; endmodule')):
                    wrapper.write_bytes(changed)
                    with self.assertRaisesRegex(ValueError,'complete retained reader source'):reader.derive(project,project,p)
                wrapper.write_bytes(original);wrapper.rename(wrapper.with_suffix('.saved'));wrapper.symlink_to(wrapper.with_suffix('.saved'))
                with self.assertRaisesRegex(ValueError,'private bounded'):reader.derive(project,project,p)
                wrapper.unlink();wrapper.write_bytes(original)
                for key in reader.PARAMETERS:
                    changed=dict(reader.PARAMETERS);changed[key]='foreign';ip.write_bytes(xml(changed))
                    with self.subTest(parameter=key),self.assertRaises(ValueError):reader.derive(project,project,p)
                ip.write_bytes(xml(reader.PARAMETERS))
                with self.assertRaisesRegex(ValueError,'private retained reader project'):reader.derive(ROOT,project,p)
                with self.assertRaisesRegex(ValueError,'private retained reader project'):reader.derive(project/'..',project,p)
                linked=project/'linked';linked.symlink_to(project,target_is_directory=True)
                with self.assertRaisesRegex(ValueError,'private bounded'):reader.derive(linked,project,p)

    def test_reviewed_payload_still_requires_the_exact_parameter_semantics(self):
        with tempfile.TemporaryDirectory(dir=ROOT/'out',prefix='reader-semantics-') as directory:
            project=Path(directory);base=project/'board/hps/project'
            ip=base/reader.IP;wrapper=base/reader.WRAPPER
            ip.parent.mkdir(parents=True);wrapper.parent.mkdir(parents=True);wrapper.write_bytes(b'reviewed fixture')
            rows=''.join('<i:parameter parameterId="'+k+'"><i:value>'+v+'</i:value></i:parameter>' for k,v in reader.PARAMETERS.items())
            for payload in (rows.replace('<i:value>false</i:value>','<i:value>true</i:value>'), rows+rows):
                ip.write_text('<r xmlns:i="http://www.accellera.org/XMLSchema/IPXACT/1685-2014">'+payload+'</r>')
                fixture=dict(reader.CONTRACT,ip_sha256=hashlib.sha256(ip.read_bytes()).hexdigest(),wrapper_sha256=hashlib.sha256(wrapper.read_bytes()).hexdigest())
                with patch.object(reader,'CONTRACT',fixture),self.assertRaises(ValueError):reader.derive(project,project,profile())
