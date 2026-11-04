"""Synthetic operator USB traces. These tests never open a kit or grant HIL."""
import importlib.util
import hashlib
from pathlib import Path
import sys
import unittest

from test_nano_hps_io import io_trace
from test_nano_native_trace import BUILD, SEED, OWNER, trace

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_usb_trace', ROOT/'platform/embedded/nano/usb_trace.py')
usb = importlib.util.module_from_spec(spec)
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
try:spec.loader.exec_module(usb)
finally:sys.path.pop(0)
SETUP = 'NOTICE:  FES_NANO_USB_SETUP status=0 board_error=0 platform_error=0 data=held\r\n'


def fixture(reports=()):
    raw, context, mapping = io_trace()
    raw = raw.replace('NOTICE:  FES_NANO_OWNED_BOOT', SETUP+'NOTICE:  FES_NANO_OWNED_BOOT')
    phases = ('loader_begin','decode_begin','inflate_end','checksum_end','decode_end','copy_end',
              'handoff','go_hwinit','go_vectors')
    steps = []
    for i, phase in enumerate(phases):
        state = 0 if phase in ('loader_begin','handoff') else 0x1005
        steps.append(f'FES_BOOT_STEP phase={phase} ticks={i:016x} hz={400000000:016x} el={2 if i<7 else 1} sctlr={state:016x}\r\n')
    raw = raw.replace('FES_NATIVE_HPS_CONTEXT', ''.join(steps)+'FES_NATIVE_HPS_CONTEXT')
    raw += usb.READY+'\r\n'
    raw += ''.join('FES_NATIVE_USB REPORT '+r+'\r\n' for r in reports)
    raw += f'FES_NATIVE_USB STOP owner={OWNER:x} generation=3 data=held\r\n'+usb.PASS+'\r\n'
    return raw, context, mapping


class USBTraceTests(unittest.TestCase):
    def decode(self, raw):
        _, context, mapping = fixture()
        return usb.decode(raw, BUILD, SEED, hps_context=context, hps_io=mapping)

    def test_idle_success_does_not_claim_physical_input(self):
        raw, _, _ = fixture()
        result = self.decode(raw)
        self.assertTrue(result['complete'], result['errors'])
        self.assertTrue(result['boot']['loader_ready'])
        self.assertEqual(result['startup']['last_checkpoint'], 'go_vectors')
        self.assertFalse(result['input_observed'])
        self.assertFalse(result['core_load_acceptance'])
        self.assertFalse(self.decode(fixture(['0000000000000000'])[0])['input_observed'])

    def test_press_release_and_modifier_reports_are_separate_evidence(self):
        raw, _, _ = fixture(['0200040000000000', '0000000000000000'])
        result = self.decode(raw)
        self.assertTrue(result['complete'], result['errors'])
        self.assertTrue(result['input_observed'])
        self.assertEqual((result['key_presses'], result['key_releases']), (2,2))

    def test_setup_and_every_terminal_marker_are_required_once_in_order(self):
        raw, _, _ = fixture()
        for marker in (SETUP, usb.READY+'\r\n', f'FES_NATIVE_USB STOP owner={OWNER:x} generation=3 data=held\r\n', usb.PASS+'\r\n'):
            for changed in (raw.replace(marker,''),raw.replace(marker,marker+marker),marker+raw):
                with self.subTest(marker=marker):
                    self.assertFalse(self.decode(changed)['complete'])

    def test_failure_owner_and_generation_cannot_pass(self):
        raw, _, _ = fixture()
        for old, new in (('status=0 board_error=0','status=-1 board_error=0'),
                         ('board_error=0','board_error=-12'),('platform_error=0','platform_error=-7'),
                         ('generation=3', 'generation=2'),('generation=3', 'generation=18446744073709551616'),
                         (f'STOP owner={OWNER:x}',f'STOP owner={OWNER+2:x}'),
                         ('transport=pio','transport=dma'),('data=held','data=active')):
            with self.subTest(new=new):self.assertFalse(self.decode(raw.replace(old,new))['complete'])
        for line in ('FES_NATIVE_USB FAIL enumeration timed out','panic: failure','FES_NANO_SD READ bytes=512',
                     'FES_NATIVE_USB REPORT 0000000000000000','FES_NATIVE_USB PASS scope=usb-only-pio',
                     'FES_NATIVE_USB READY transport=pio input=boot-keyboard data=held'):
            self.assertFalse(self.decode(raw+line+'\n')['complete'])

    def test_fragmented_records_never_grant_completion(self):
        raw, _, _ = fixture()
        end = raw.index(usb.PASS)
        for n in range(end, len(raw)-1):
            self.assertFalse(self.decode(raw[:n])['complete'])
        partial = self.decode(raw[:raw.index('FES_NATIVE_USB STOP')])
        self.assertTrue(partial['ready']);self.assertFalse(partial['stopped'])
        self.assertEqual(partial['errors'],[])

    def test_bare_loader_prompt_is_ready_without_completing_usb(self):
        raw, _, _ = fixture()
        prompt = raw[:raw.index('SOCFPGA_AGILEX5 # ')]+'SOCFPGA_AGILEX5 # '
        result = self.decode(prompt)
        self.assertTrue(result['boot']['loader_ready'],result['errors'])
        self.assertFalse(result['complete']);self.assertFalse(result['ready'])
        self.assertEqual(result['errors'],[])
        self.assertFalse(self.decode(prompt[:-1])['boot']['loader_ready'])

    def test_arm64_terminal_proof_is_program_pass_not_generic_runtime_output(self):
        raw, _, _ = fixture()
        # CPU.Init installs arm64.exit before runtime.exit's generic print.
        # That hook parks silently. It is not observable as a UART halt proof.
        halt = 'exit with code 0 halting\r\n'
        self.assertTrue(self.decode(raw)['complete'])
        for changed in (halt+raw,raw+halt,raw+halt.replace('code 0','code 1'),
                        raw.replace(usb.PASS,halt+usb.PASS)):
            self.assertFalse(self.decode(changed)['complete'])
        self.assertFalse(self.decode(raw+halt.rstrip('\r\n'))['complete'])

    def test_report_validation_and_order(self):
        for value in ('0000010000000000','0000040400000000','0000e00000000000','00000000','gg00000000000000'):
            with self.subTest(value=value):self.assertFalse(self.decode(fixture([value])[0])['complete'])
        raw, _, _ = fixture(['0000040000000000'])
        marker = 'FES_NATIVE_USB REPORT 0000040000000000\r\n'
        self.assertFalse(self.decode(raw.replace(marker,'').replace(usb.READY,marker+usb.READY))['complete'])

    def test_actual_operator_startup_check_rejects_missing_or_later_phases(self):
        raw, _, _ = fixture()
        for line in raw.splitlines(keepends=True):
            if not line.startswith('FES_BOOT_STEP'):continue
            for changed in (raw.replace(line,''),raw.replace(line,line+line)):
                self.assertFalse(usb.startup_checkpoints(changed)['complete'])
                self.assertFalse(self.decode(changed)['complete'])
        self.assertFalse(self.decode(raw.replace('phase=go_vectors','phase=fat_mount_end'))['complete'])
        self.assertFalse(self.decode(raw.replace('el=1 sctlr=0000000000001005','el=1 sctlr=0000000000000000'))['complete'])

    def test_foreign_early_usb_and_missing_hps_proof_fail(self):
        raw, context, mapping = fixture()
        ready = usb.READY+'\r\n'
        self.assertFalse(self.decode(ready+raw.replace(ready,''))['complete'])
        line = next(line for line in raw.splitlines(keepends=True) if line.startswith('FES_NATIVE_HPS_IO PASS'))
        self.assertFalse(self.decode(raw.replace(line,''))['complete'])
        prefix = raw[:raw.index(usb.READY)]
        self.assertFalse(trace.decode_native_boot(prefix,BUILD,SEED,hps_context=context,hps_io=mapping)['complete'])
        self.assertTrue(trace.decode_native_boot(prefix,BUILD,SEED,hps_context=context,hps_io=mapping,usb_control=True)['complete'])
        for selection in (None,1,'true'):
            with self.assertRaises(ValueError):trace.decode_native_boot(b'',BUILD,SEED,usb_control=selection)
        with self.assertRaises(ValueError):trace.decode_native_boot(b'',BUILD,SEED,usb_control=True)
        with self.assertRaises(ValueError):
            trace.decode_native_boot(b'',BUILD,SEED,usb_control=True,hps_context=context,hps_io=mapping,capture_trace=False)
        self.assertFalse(self.decode(b'x'*(usb.MAX_BYTES+1))['complete'])


PAD_SCHEMA='05010905a101150026ff007508950209300931810205091901290c150025017501950c8102750495018103c0'
def gamepad_fixture(reports=()):
    raw,context,mapping=fixture()
    data=bytes.fromhex(PAD_SCHEMA)
    descriptor=f'FES_NATIVE_USB DESCRIPTOR sha256={hashlib.sha256(data).hexdigest()} bytes={len(data)} data={PAD_SCHEMA}\r\n'
    ready='FES_NATIVE_USB READY transport=pio input=gamepad data=held\r\n'
    raw=raw.replace(usb.READY+'\r\n',descriptor+ready+''.join('FES_NATIVE_USB GAMEPAD '+r+'\r\n' for r in reports))
    return raw,context,mapping

class GamepadTraceTests(unittest.TestCase):
    def decode(self,raw,**kwargs):
        _,context,mapping=gamepad_fixture()
        return usb.decode(raw,BUILD,SEED,hps_context=context,hps_io=mapping,usb_input='gamepad',**kwargs)

    def test_input_selection_is_independent_and_required(self):
        raw,context,mapping=gamepad_fixture()
        good=self.decode(raw)
        self.assertTrue(good['complete'],good['errors'])
        self.assertFalse(good['input_observed'])
        self.assertFalse(good['core_load_acceptance'])
        self.assertFalse(usb.decode(raw,BUILD,SEED,hps_context=context,hps_io=mapping)['complete'])
        self.assertFalse(self.decode(fixture()[0])['complete'])
        for value in (None,True,'mouse'):
            with self.assertRaises(ValueError):usb.decode(raw,BUILD,SEED,hps_context=context,hps_io=mapping,usb_input=value)

    def test_changes_are_separate_from_idle_startup(self):
        idle='report=80800000 axes=128,128,0,0,0,0 buttons=00000000 hat=-1'
        changed='report=ff000100 axes=32767,-32767,0,0,0,0 buttons=00000001 hat=-1'
        held=self.decode(gamepad_fixture([idle,idle])[0])
        self.assertTrue(held['complete'],held['errors']);self.assertFalse(held['input_observed'])
        result=self.decode(gamepad_fixture([idle,changed,idle])[0])
        self.assertTrue(result['complete'],result['errors']);self.assertTrue(result['input_observed'])
        self.assertEqual(result['gamepad_changes'],2)
        self.assertEqual(result['key_presses'],0)

    def test_descriptor_must_be_once_before_ready_and_match_its_hash(self):
        raw,_,_=gamepad_fixture()
        marker=next(line for line in raw.splitlines(keepends=True) if 'USB DESCRIPTOR' in line)
        ready='FES_NATIVE_USB READY transport=pio input=gamepad data=held\r\n'
        for changed in (raw.replace(marker,''),raw.replace(marker,marker+marker),
                        raw.replace(marker,'').replace(ready,ready+marker),
                        raw.replace('data='+PAD_SCHEMA,'data=00'+PAD_SCHEMA),
                        raw.replace('bytes='+str(len(bytes.fromhex(PAD_SCHEMA))),'bytes=0'),
                        raw.replace('sha256='+hashlib.sha256(bytes.fromhex(PAD_SCHEMA)).hexdigest(),'sha256='+'0'*64)):
            self.assertFalse(self.decode(changed)['complete'])

    def test_malformed_reports_and_foreign_protocol_fail(self):
        row='report=80800000 axes=128,128,0,0,0,0 buttons=00000000 hat=-1'
        raw,_,_=gamepad_fixture([row])
        for old,new in (('report=80800000','report=808'),('axes=128,128','axes=32768,128'),
                        ('hat=-1','hat=8'),('buttons=00000000','buttons=0'),
                        ('GAMEPAD '+row,'REPORT 0000000000000000')):
            with self.subTest(new=new):self.assertFalse(self.decode(raw.replace(old,new))['complete'])
        marker='FES_NATIVE_USB GAMEPAD '+row+'\r\n'
        for changed in (marker+raw.replace(marker,''),raw+marker,raw.replace('generation=3','generation=2'),
                        raw.replace(usb.PASS,'FES_NATIVE_USB FAIL stop')):
            self.assertFalse(self.decode(changed)['complete'])

    def test_fragments_and_failed_schema_preserve_descriptor_without_completion(self):
        raw,_,_=gamepad_fixture()
        end=raw.index(usb.PASS)
        for n in range(end,len(raw)-1):self.assertFalse(self.decode(raw[:n])['complete'])
        failed=raw[:raw.index('FES_NATIVE_USB READY')]+ 'FES_NATIVE_USB FAIL unsupported HID layout\r\n'
        result=self.decode(failed)
        self.assertIsNotNone(result['descriptor'])
        self.assertFalse(result['ready']);self.assertFalse(result['complete'])


if __name__ == '__main__':unittest.main()
