"""A composed native USB image needs explicit matching scope and linker proof."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

from entry_payload_fixture import bind_payload
ROOT=Path(__file__).resolve().parents[1]
DATA=ROOT/'platform/embedded/tamago/nativefabric/testdata'
sys.path.insert(0,str(ROOT/'platform/embedded/firmware'))
try:
    import nano_firmware_mmu  # noqa: F401 -- cold_profile.header imports it by module name
finally:
    sys.path.pop(0)
def module(name,path):
    s=importlib.util.spec_from_file_location(name,ROOT/path)
    m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
selection=module('shared_usb_selection','platform/embedded/nano/usb_selection.py')
entry=module('shared_usb_entry','platform/embedded/entry/build.py')
cold=module('shared_usb_cold','platform/embedded/nano/cold_profile.py')
SHARED_FLAGS=dict(tenfoot_serial=True,selected_programs=True,computer_reload=True,computer_storage_return=True)
class SharedUSBSelectionTests(unittest.TestCase):
    def record(self,protocol='gamepad'):
        return {'program':'fpgaload','usb_control':True,'usb_shared':True,'usb_input':protocol,
                'tenfoot_serial':True,'selected_programs':True,'computer_reload':True,'computer_storage_return':True,
                'command':['go','build','-ldflags','-X main.expectedUSBControl=owned-shared-usb-pio-v1 -X main.expectedUSBInput='+protocol,'./cmd/fpgaload']}
    def test_explicit_shared_scope_and_exact_native_link_match(self):
        for protocol in ('boot-keyboard','gamepad','multi'):
            r=self.record(protocol)
            self.assertEqual(selection.mode(r),(True,True))
            self.assertTrue(entry.usb_control_option(r))
            for key in ('usb_control','usb_shared','tenfoot_serial','selected_programs','computer_reload','computer_storage_return'):
                for bad in (False,0,1,None):
                    with self.subTest(protocol=protocol,key=key,bad=bad),self.assertRaises(ValueError):
                        entry.usb_control_option(dict(r,**{key:bad}))
    def test_shared_mode_cannot_adopt_another_program_or_memory_scope(self):
        r=self.record()
        for patch in ({'program':'usbcheck'},{'program':'tenfoot'},{'usb_input':None},{'usb_input':'mouse'},
                      {'program':'usbcheck','usb_shared':False,'usb_input':'multi'},
                      {'smmu_observe':True},{'hps_ddr_read':{}}):
            with self.subTest(patch=patch),self.assertRaises(ValueError):selection.mode(dict(r,**patch))
        for flags in ('','-X main.expectedUSBControl=owned-usb-pio-v1 -X main.expectedUSBInput=gamepad',
                      '-X main.expectedUSBControl=owned-shared-usb-pio-v1',
                      '-X main.expectedUSBControl=owned-shared-usb-pio-v1 -X main.expectedUSBInput=boot-keyboard'):
            changed=copy.deepcopy(r);changed['command'][3]=flags
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.usb_control_option(changed)
        for marker in ('main.expectedUSBControl','main.expectedUSBInput'):
            changed=copy.deepcopy(r);changed['command'][3]+=' -X '+marker+'=gamepad'
            with self.subTest(marker=marker),self.assertRaises(ValueError):entry.usb_control_option(changed)
        changed=copy.deepcopy(r);changed['command'][-1]='./cmd/usbcheck'
        with self.assertRaises(ValueError):entry.usb_control_option(changed)
    def test_protocol_sets_have_one_source(self):
        self.assertEqual(selection.protocols(False),('boot-keyboard','gamepad'))
        self.assertEqual(selection.protocols(True),('boot-keyboard','gamepad','multi'))
        for shared in (None,0,1,'true'):
            with self.subTest(shared=shared):self.assertNotIn('multi',selection.protocols(shared))


class SharedUSBBootProducerTests(unittest.TestCase):
    """prepare_uboot, package and hps_boot admit the entry and emit the SPL/BL31
    header only through cold_profile: the shared lease's multi input must pass
    both, while contained (unshared) USB keeps the single-device protocols."""

    def write_entry(self,directory,protocol,**change):
        receipt=dict(status='built',target='de25-nano',entry_image_address=0x96080000,
            ram_start=0x84000000,ram_end=0x94000000,uart_base=0x10c02100,native_program='fpgaload',
            firmware_profile_sha256=cold.sha(ROOT/'platform/embedded/nano/sd-profile.json'),sources={},
            native_build_sources={'platform/embedded/tamago/build.py':cold.sha(ROOT/'platform/embedded/tamago/build.py')},
            usb_control=True,usb_shared=True,usb_input=protocol,**SHARED_FLAGS)
        receipt.update(change)
        bind_payload(directory,receipt)
        (directory/'result.json').write_text(json.dumps(receipt))

    def test_entry_admits_multi_only_on_the_shared_lease(self):
        hps=cold.hps_context
        # Sealed-project derivations are injected; the USB scope checks are real.
        with tempfile.TemporaryDirectory() as temp,\
                patch.object(hps,'checked',return_value={'sealed':True}),\
                patch.object(hps,'checked_hps_io',return_value={'pins':True}),\
                patch.object(hps,'checked_control',return_value={'control':True}),\
                patch.object(hps,'checked_configuration',return_value=None),\
                patch.object(hps,'checked_reader_memory',return_value=None),\
                patch.object(hps,'checked_cpu_mapping',return_value=None),\
                patch.object(hps,'checked_computer',return_value={'computer':True}),\
                patch.object(hps,'checked_program_platform',return_value={'platform':True}):
            directory=Path(temp)
            for protocol in selection.protocols(True):
                self.write_entry(directory,protocol)
                with self.subTest(protocol=protocol):
                    self.assertEqual(cold.entry(directory)[0],directory/'entry.img')
            for change in ({'usb_shared':False},{'native_program':'usbcheck','usb_shared':False},
                           {'usb_control':False},{'usb_input':None},{'usb_input':'mouse'}):
                self.write_entry(directory,'multi',**change)
                with self.subTest(change=change),self.assertRaises(ValueError):cold.entry(directory)

    def shared_profile(self,protocol):
        digest=cold.hps_context.digest
        memory=json.loads((DATA/'hps-reader-memory.json').read_bytes())
        hps=dict(json.loads((DATA/'hps-context.json').read_bytes()),programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1',
                 control_abi=0x10203041,package_id=memory['package_id'],build_id=memory['build_id'])
        control=dict(json.loads((DATA/'hps-control.json').read_bytes()),fabric_stop=dict(scope='sealed-ddr-master-stop-v1',
                     probe_cpu_base=0x20010100,probe_span=128,maintenance='held-reset-from-configuration'))
        computer=dict(json.loads((DATA/'hps-computer.json').read_bytes()),package_id=hps['package_id'],build_id=hps['build_id'],
                      control_map_sha256=digest(control),handoff_sha256=control['handoff_sha256'])
        io=json.loads((DATA/'hps-io.json').read_bytes())
        configuration=dict(base=0x94800000,size=8<<20,core_size=128,chunk_bytes=65536,core_sha384='d'*96,chunk_sha384=['d'*96])
        cpu=cold.hps_context.cpu_mapping(control,configuration,memory)
        return dict(boot_fit=dict(cold.fit_layout.CONTRACT),sd_pio=True,usb_pio=True,usb_shared=True,usb_input=protocol,
            chain_sha256='a'*64,owner_seed=1,entry_image_sha256='b'*64,entry_image_size=128,**SHARED_FLAGS,
            hps_context=hps,hps_context_sha256=digest(hps),hps_io=io,hps_io_sha256=digest(io),
            hps_control=control,hps_control_sha256=digest(control),hps_configuration=configuration,
            hps_reader_memory=memory,hps_reader_memory_sha256=digest(memory),
            hps_computer=computer,hps_computer_sha256=digest(computer),cpu_mapping=cpu,cpu_mapping_sha256=digest(cpu))

    def test_cold_header_admits_multi_only_on_the_shared_lease(self):
        headers={protocol:cold.header(self.shared_profile(protocol)) for protocol in selection.protocols(True)}
        for line in ('#define FES_NANO_CONTROLLED_USB 1\n','#define FES_NANO_SHARED_USB 1\n','#define FES_NANO_COLD_COMPUTER 1\n'):
            self.assertIn(line,headers['multi'])
        # The SPL/BL31 chain is protocol-agnostic; the native ELF owns the input stack.
        self.assertEqual(headers['multi'],headers['boot-keyboard'])
        for change in ({'usb_input':None},{'usb_input':'mouse'},{'usb_shared':1},{'usb_shared':'true'}):
            with self.subTest(change=change),self.assertRaises(ValueError):
                cold.header(dict(self.shared_profile('multi'),**change))
        source=json.loads((DATA/'hps-context.json').read_bytes());io=json.loads((DATA/'hps-io.json').read_bytes())
        contained=dict(boot_fit=dict(cold.fit_layout.CONTRACT),sd_pio=False,usb_pio=True,chain_sha256='a'*64,owner_seed=1,
            entry_image_sha256='b'*64,entry_image_size=64,hps_context=source,hps_context_sha256=cold.hps_context.digest(source),
            hps_io=io,hps_io_sha256=cold.hps_context.digest(io))
        for protocol in selection.protocols(False):
            with self.subTest(contained=protocol):
                self.assertIn('#define FES_NANO_SHARED_USB 0\n',cold.header(dict(contained,usb_input=protocol)))
        with self.assertRaisesRegex(ValueError,'USB boot input'):cold.header(dict(contained,usb_input='multi'))
