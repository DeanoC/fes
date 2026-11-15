"""Compiled HPS I/O handoff parsing and read-only secure-scope failures."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from test_nano_native_trace import trace,fixture,BUILD,SEED,OWNER

ROOT=Path(__file__).resolve().parents[1]
NANO=ROOT/'platform/embedded/nano'
FW=ROOT/'platform/embedded/firmware'

def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result)
    return result

pinmux=module('test_nano_pinmux',NANO/'hps_io.py')
context=module('test_io_hps_context',NANO/'hps_context.py')
cold=module('test_io_cold',NANO/'cold_profile.py')
FIXTURE=ROOT/'tests/fixtures/nano/hps-io-handoff.bin'
NATIVE=ROOT/'platform/embedded/tamago/nativefabric/testdata'

def io_trace(firmware=None):
    source=json.loads((NATIVE/'hps-context.json').read_bytes())
    mapping=json.loads((NATIVE/'hps-io.json').read_bytes())
    expected={'sha256':context.digest(source),'package_id':source['package_id'],'build_id':source['build_id']}
    marker=(f"FES_NATIVE_HPS_CONTEXT PASS sha256={expected['sha256']} package_id={source['package_id']} build_id={source['build_id']} "
            f'owner={OWNER:x} generation=1 programming=unavailable')
    prefix=fixture().replace('FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano',marker)
    lines=[]
    if firmware is None:
        for part in range(2):
            lines.append(f'NOTICE:  FES_NANO_HPS_IO_BEGIN part={part} owner={OWNER:x} generation=1 count=164 busy=0')
            if not part:
                lines.extend(f"NOTICE:  FES_NANO_HPS_IO_ROW pass={p} index={i} offset={r['offset']:x} expected={r['value']:x} observed={r['value']:x}"
                             for p in range(2) for i,r in enumerate(mapping['registers']))
            lines.append(f'NOTICE:  FES_NANO_HPS_IO_END part={part} status=0 reads=328 error=0 owner={OWNER:x} generation=1 data=held')
    else:
        # These are the actual compiled strong-provider printf records with
        # injected MMIO, normalized only to the surrounding synthetic owner.
        import re
        lines=[re.sub(r'owner=[0-9a-f]+',f'owner={OWNER:x}',line) for line in firmware.splitlines() if line.startswith('FES_NANO_HPS_IO_')]
    lines.append(f'FES_NATIVE_HPS_IO PASS map_sha256={context.digest(mapping)} count=164 reads=328 owner={OWNER:x} generation=1 data_held=1 runtime_admission=unavailable')
    return prefix+'\r\n'.join(lines)+'\r\n',expected,mapping


class HPSIOTraceTests(unittest.TestCase):
    def test_all_rows_both_ordered_samples_and_native_result_are_required(self):
        raw,expected,mapping=io_trace()
        decode=lambda data:trace.decode_native_boot(data,BUILD,SEED,hps_context=expected,hps_io=mapping)
        result=decode(raw)
        self.assertTrue(result['complete'],result['errors'])
        self.assertEqual(result['hps_io_observation']['reads'],328)
        self.assertFalse(result['core_load_acceptance'])
        self.assertFalse(result['hps_io_observation']['runtime_admission'])
        self.assertFalse(decode(raw.rstrip('\r\n'))['complete'])
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,hps_context=expected)['complete'])
        lines=raw.splitlines(keepends=True)
        for line in lines:
            if 'FES_NANO_HPS_IO_' not in line and 'FES_NATIVE_HPS_IO' not in line:continue
            for changed in (raw.replace(line,''),raw.replace(line,line+line)):
                result=decode(changed)
                self.assertFalse(result['complete'])
        for old,new in (('offset=1000','offset=14c0'),('observed=7500','observed=7501'),
                        ('expected=7500','expected=7501'),('part=0 status=0','part=0 status=4'),
                        ('part=1 status=0','part=1 status=1'),('reads=328 error=0','reads=327 error=0'),
                        ('error=0','error=-5'),('count=164 busy=0','count=164 busy=1'),
                        ('map_sha256='+context.digest(mapping),'map_sha256='+'a'*64),
                        ('data_held=1','data_held=0'),('runtime_admission=unavailable','runtime_admission=available')):
            with self.subTest(old=old):self.assertFalse(decode(raw.replace(old,new))['complete'])
        for line in lines:
            if 'HPS_IO_BEGIN' in line or 'HPS_IO_END' in line or 'FES_NATIVE_HPS_IO' in line:
                with self.subTest(line=line):
                    self.assertFalse(decode(raw.replace(line,line.replace(f'owner={OWNER:x}',f'owner={OWNER+2:x}')))['complete'])
                    self.assertFalse(decode(raw.replace(line,line.replace('generation=1','generation=2')))['complete'])
        with self.assertRaises(ValueError):trace.decode_native_boot(raw,BUILD,SEED,hps_io=mapping)
        partial=raw[:raw.index('FES_NANO_HPS_IO_ROW pass=1 index=163')]
        self.assertFalse(decode(partial)['complete'])
        self.assertEqual(decode(partial)['hps_io_observation']['reads'],327)


class HPSIOTests(unittest.TestCase):
    def setUp(self):
        self.raw=FIXTURE.read_bytes()
        self.context=json.loads((NATIVE/'hps-context.json').read_bytes())
        self.mapping=json.loads((NATIVE/'hps-io.json').read_bytes())

    def test_real_compiled_handoff_has_exact_rows_and_excludes_non_agilex5_offsets(self):
        provenance=json.loads(FIXTURE.with_suffix('.json').read_text())
        self.assertEqual(hashlib.sha256(self.raw).hexdigest(),provenance['handoff_sha256'])
        self.assertEqual(len(self.raw),provenance['size'])
        rows=pinmux.parse_handoff(self.raw)
        self.assertEqual(rows,self.mapping['registers'])
        self.assertEqual(len(rows),164)
        self.assertEqual([len(pinmux.MUX),len(pinmux.CTRL),len(pinmux.FPGA),len(pinmux.DELAY)],[48,48,20,48])
        self.assertEqual(next(r['value'] for r in rows if r['offset']==0x1430),0x7500)
        self.assertTrue(all(r['value']==0x34 for r in rows if 0x1130<=r['offset']<=0x124c))
        self.assertTrue({0x14c0,0x14c4,0x1324,0x134c,0x1358}.isdisjoint(r['offset'] for r in rows))
        self.assertEqual(context.encode(self.mapping),(NATIVE/'hps-io.json').read_bytes())
        self.assertEqual(pinmux.validate(self.mapping,self.context),self.mapping)

    def test_malformed_headers_offsets_order_generic_words_and_selections_reject(self):
        candidates=[self.raw[:0x57f],self.raw+bytes(4096)]
        for index in range(16):
            changed=bytearray(self.raw);changed[index]^=1;candidates.append(bytes(changed))
        for position,magic,base,offsets in pinmux.GROUPS:
            for relative in (0,4,8,15,16,16+8*(len(offsets)-1)):
                changed=bytearray(self.raw);changed[position+relative]^=1;candidates.append(bytes(changed))
        for position in (0x10+20,0x330+20,0x330+16+20*8+4,0x330+16+21*8+4):
            changed=bytearray(self.raw);struct.pack_into('>I',changed,position,16);candidates.append(bytes(changed))
        for changed in candidates:
            with self.subTest(size=len(changed)),self.assertRaises(ValueError):pinmux.parse_handoff(changed)

    def test_configuration_covers_all_boot_reads_but_not_the_unconsumed_suffix(self):
        expected=pinmux.configuration_bytes(self.raw)
        self.assertEqual(len(expected),0x658)
        self.assertEqual(expected,self.raw[:0x658])
        # Every consumed byte must either change compatibility or reject.
        for offset in range(0x658):
            changed=bytearray(self.raw);changed[offset]^=1
            try:actual=pinmux.configuration_bytes(bytes(changed))
            except ValueError:continue
            with self.subTest(offset=hex(offset)):self.assertNotEqual(actual,expected)
        for offset in range(0x658,len(self.raw)):
            changed=bytearray(self.raw);changed[offset]^=1
            self.assertEqual(pinmux.configuration_bytes(bytes(changed)),expected)
            self.assertNotEqual(hashlib.sha256(changed).digest(),hashlib.sha256(self.raw).digest())
        self.assertEqual(expected[0x644:0x658],self.raw[0x644:0x658])
        for position in (0x580,0x620,0x634):
            for relative in range(16):
                changed=bytearray(self.raw);changed[position+relative]^=1
                with self.subTest(position=hex(position+relative)),self.assertRaises(ValueError):
                    pinmux.configuration_bytes(bytes(changed))
        for raw in (self.raw[:0x580],self.raw[:0x647],self.raw[:0x657]):
            with self.assertRaises(ValueError):pinmux.configuration_bytes(raw)

    def test_compiled_receipt_digest_and_no_symlink_gate_precede_map(self):
        with tempfile.TemporaryDirectory() as temp:
            project=Path(temp);hps=project/'board/hps';handoff=hps/'project/output_files/hps_bootloader_handoff.bin'
            handoff.parent.mkdir(parents=True);handoff.write_bytes(self.raw)
            receipt=hps/'build.json';receipt.write_text(json.dumps({'generated_handoff_and_sdc_sha256':{'output_files/hps_bootloader_handoff.bin':hashlib.sha256(self.raw).hexdigest()}}))
            with patch.object(context,'_derive',return_value=self.context) as check:
                self.assertEqual(context._hps_io(project),self.mapping);check.assert_called_once_with(project)
            configuration=pinmux.configuration_sha256(project,self.context)
            changed=bytearray(self.raw);changed[-2]^=1;handoff.write_bytes(changed)
            with self.assertRaisesRegex(ValueError,'actual compiled'):pinmux.configuration_sha256(project,self.context)
            receipt.write_text(json.dumps({'generated_handoff_and_sdc_sha256':{'output_files/hps_bootloader_handoff.bin':hashlib.sha256(changed).hexdigest()}}))
            self.assertEqual(pinmux.configuration_sha256(project,self.context),configuration)
            self.assertNotEqual(pinmux.derive(project,self.context)['handoff_sha256'],self.mapping['handoff_sha256'])
            changed=bytearray(self.raw);changed[0x10+20]^=1;handoff.write_bytes(changed)
            with self.assertRaisesRegex(ValueError,'actual compiled'):pinmux.derive(project,self.context)
            with self.assertRaisesRegex(ValueError,'actual compiled'):pinmux.configuration_sha256(project,self.context)
            handoff.unlink();handoff.symlink_to(FIXTURE)
            with self.assertRaisesRegex(ValueError,'symlink'):pinmux.derive(project,self.context)
            with self.assertRaisesRegex(ValueError,'symlink'):pinmux.configuration_sha256(project,self.context)

    def test_map_and_native_receipt_cannot_omit_mask_redirect_or_grant_another_profile(self):
        record={'program':'hpsio','hps_project':'sealed','hps_context':self.context,
                'hps_context_sha256':context.digest(self.context),'hps_io':self.mapping,'hps_io_sha256':context.digest(self.mapping)}
        with patch.object(context,'derive',return_value=self.context),patch.object(context,'hps_io',return_value=self.mapping):
            self.assertEqual(context.checked_hps_io(record),self.mapping)
            self.assertEqual(context.checked_hps_io(dict(record,program='usbcheck')),self.mapping)
            for key in ('hps_io','hps_io_sha256'):
                changed=dict(record);changed.pop(key)
                with self.subTest(key=key),self.assertRaises(ValueError):context.checked_hps_io(changed)
            for program in ('probe','bootcontext','fpgaload','tenfoot'):
                with self.subTest(program=program),self.assertRaises(ValueError):context.checked_hps_io(dict(record,program=program))
            with self.assertRaises(ValueError):context.checked_hps_io(dict(record,hps_io_sha256='0'*64))
        profile={'sd_pio':False,'chain_sha256':'a'*64,'owner_seed':1,'entry_image_sha256':'b'*64,'entry_image_size':64,
                 'hps_context':self.context,'hps_context_sha256':context.digest(self.context),
                 'hps_io':self.mapping,'hps_io_sha256':context.digest(self.mapping)}
        header=cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(profile)} )
        self.assertIn('#define FES_NANO_HPS_IO_OBSERVE 1',header)
        self.assertIn('#define FES_NANO_CONTROL_PROBE 0',header)
        self.assertNotIn('0x14c0U',header)
        for name in ('offset','mask','omit','duplicate','value','extra','format','hash'):
            changed=copy.deepcopy(self.mapping)
            if name=='offset':changed['registers'][0]['offset']=0x14c4
            if name=='mask':changed['registers'][0]['mask']=15
            if name=='omit':changed['registers'].pop()
            if name=='duplicate':changed['registers'][1]=changed['registers'][0]
            if name=='value':changed['registers'][0]['value']=16
            if name=='extra':changed['registers'][0]['write']=True
            if name=='format':changed['format']=True
            if name=='hash':changed['hps_io_hash']='f'*64
            with self.subTest(name=name),self.assertRaises(ValueError):
                cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,hps_io=changed,hps_io_sha256=context.digest(changed)))} )
        for key,value in (('sd_pio',True),('hps_control',{'cpu_base':0x20010400}),('hps_io_sha256','f'*64),('hps_context',None)):
            with self.subTest(key=key),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,**{key:value}))} )
        usb=cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,usb_pio=True,usb_input='boot-keyboard'))} )
        self.assertIn('#define FES_NANO_CONTROLLED_USB 1',usb)
        self.assertIn('#define FES_NANO_CONTROLLED_SD 0',usb)
        self.assertIn('#define FES_NANO_CONTROLLED_USB 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,usb_pio=True,usb_input='gamepad'))} ))
        for value in (None,True,'mouse'):
            with self.subTest(input=value),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,usb_pio=True,usb_input=value))} )
        for key,value in (('usb_pio',1),('usb_pio','true'),('sd_pio',True),('hps_io',None),('hps_control',{})):
            with self.subTest(usb_field=key),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,usb_pio=True,**{key:value}) if key!='usb_pio' else dict(profile,usb_pio=value))} )
        self.assertIn('#define FES_NANO_HPS_IO_OBSERVE 0',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **({k:v for k,v in profile.items() if k not in ('hps_io','hps_io_sha256')})} ))

    def test_actual_c_double_sample_and_every_read_or_ownership_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            binary=Path(temp)/'hps-io'
            compiled=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-I'+str(FW),str(FW/'nano_hps_io.c'),str(FW/'tests/nano_hps_io_test.c'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertIn('984 read/ownership failure positions',result.stdout)


if __name__=='__main__':unittest.main()
