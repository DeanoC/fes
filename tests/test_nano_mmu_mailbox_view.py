"""Fixed read views, exact hardware guards and terminal parser behaviour."""
import copy
import json
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/firmware')]
import mmu_mailbox_view as view
import nano_firmware_mmu as mmu
from test_nano_mmu_boot import base_trace,decode,transition,mailbox_rows,row,step_rows,vector_row,status_rows


class ScopeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which('cc') or not shutil.which('ld'):raise unittest.SkipTest('host ELF tools required')
        cls.temp=tempfile.TemporaryDirectory();cls.addClassCleanup(cls.temp.cleanup);base=Path(cls.temp.name)
        fixture=json.loads((ROOT/'tests/fixtures/nano-mmu-mailbox/scope-guard.json').read_text())
        original=[int(w,16) for w in fixture['words']]
        cls.root=0x80030000;cls.park=0x80004000;position=0;labels={};lines=['.text','.global writer','writer:']
        def add(name,words):
            nonlocal position
            full='fes_nano_mmu_mailbox_'+name
            labels[full]=(0x80000000+position,4*len(words))
            lines.extend(['.global '+full,'.type '+full+',@function',full+':'])
            lines.extend('.long '+hex(w) for w in words);lines.append('.size '+full+',.-'+full);position+=4*len(words)
        def guard(address):
            words=original.copy()
            for i in view.SCOPE['external_branch_indices']:
                w=words[i]
                if w&0xfc000000==0x14000000:mask,shift=0x03ffffff,0
                elif w&0x7e000000==0x36000000:mask,shift=0x0007ffe0,5
                else:mask,shift=0x00ffffe0,5
                words[i]=(w&~mask)|((((cls.park-address-4*i)//4)<<shift)&mask)
            for i in view.SCOPE['root_indices']:
                delta=cls.root-address-4*i;words[i]=(words[i]&~0x60ffffe0)|((delta&3)<<29)|(((delta>>2)&0x7ffff)<<5)
            return words
        for phase,target in enumerate((0x10a30000,0x10a30000,0x90000)):
            add('scope_before_'+str(phase),guard(0x80000000+position))
            packed=sum(value<<(9*i) for i,value in enumerate((0,8,9,1,288,11)))
            setup=[0xd2800006|((packed&65535)<<5)]
            setup += [0xf2800006|((shift//16)<<21)|(((packed>>shift)&65535)<<5) for shift in (16,32,48)]
            setup += [0xd2800005|((target&65535)<<5),0xf2a00005|((target>>16)<<5),0x52800004]
            add('setup_'+str(phase),setup)
            add('reads_'+str(phase),[0xd2800120,0x9b007c80,0x9ac024c0,0x92402000,0x8b0008a0,0xb9400001])
            read=labels['fes_nano_mmu_mailbox_reads_'+str(phase)][0]
            delta=(read-(0x80000000+position)-8)//4
            add('loop_'+str(phase),[0x11000484,0x7100189f,0x54000003|((delta&0x7ffff)<<5)])
            add('scope_after_'+str(phase),guard(0x80000000+position))
            if phase==0:
                lines+=['.global fes_nano_mmu_transition_sctlr','fes_nano_mmu_transition_sctlr:','.long 0xd51e1000,0xd5033fdf'];position+=8
        lines+=['.global fes_nano_mmu_transition_write','.type fes_nano_mmu_transition_write,@function','.set fes_nano_mmu_transition_write,writer','.size fes_nano_mmu_transition_write,'+str(position),
                '.org 0x4000','.global fes_nano_mmu_transition_park','fes_nano_mmu_transition_park:','.long 0xd503205f,0x17ffffff','.section .note.GNU-stack,"",@progbits']
        (base/'fixture.S').write_text('\n'.join(lines)+'\n');(base/'fixture.ld').write_text('ENTRY(writer)\nSECTIONS { . = 0x80000000; .text : { *(.text*) } /DISCARD/ : { *(.note*) *(.eh_frame*) } }\n')
        for command in (['cc','-c',str(base/'fixture.S'),'-o',str(base/'fixture.o')],['ld','-T',str(base/'fixture.ld'),str(base/'fixture.o'),'-o',str(base/'fixture.elf')]):
            p=subprocess.run(command,capture_output=True,text=True,timeout=30)
            if p.returncode:raise AssertionError(p.stderr)
        cls.image=(base/'fixture.elf').read_bytes();cls.symbols=mmu.ram.symbols(cls.image);cls.bounds=dict(tables=dict(root=cls.root));cls.labels=labels
        cls.original=bytes().join(struct.pack('<I',w) for w in original);cls.fixture=fixture

    def test_guard_matches_original_and_actual_elf_has_exact_eighteen_read_views(self):
        f=self.fixture
        self.assertEqual(view.scope_fingerprint(self.original,f['scope_address'],f['park'],f['root']),view.SCOPE)
        proof=view.verify(self.image,self.symbols,self.bounds,mmu.file_bytes)
        self.assertEqual(proof['maximum_reads'],18);self.assertFalse(proof['hardware_authority'])

    def test_each_guard_or_read_instruction_mutation_is_rejected(self):
        for name,(address,size) in self.labels.items():
            raw,_=mmu.file_bytes(self.image,address,size);offset=self.image.find(raw)
            self.assertGreaterEqual(offset,0)
            for delta in range(0,size,4):
                bad=bytearray(self.image);bad[offset+delta]^=1
                with self.subTest(site=name,delta=delta),self.assertRaises(ValueError):
                    view.verify(bytes(bad),self.symbols,self.bounds,mmu.file_bytes)

    def test_missing_reordered_outside_or_truncated_sites_never_verify(self):
        for name in self.labels:
            for change in ('missing','size','unaligned','outside'):
                symbols=copy.deepcopy(self.symbols);address,size=symbols[name]
                if change=='missing':del symbols[name]
                elif change=='size':symbols[name]=(address,size+4)
                elif change=='unaligned':symbols[name]=(address+1,size)
                else:symbols[name]=(0x80010000,size)
                with self.subTest(name=name,change=change),self.assertRaises(ValueError):
                    view.verify(self.image,symbols,self.bounds,mmu.file_bytes)


class FormatterTests(unittest.TestCase):
    def test_only_the_exact_local_formatter_and_preserved_lr_call_are_admitted(self):
        data=json.loads((ROOT/'tests/fixtures/nano-mmu-mailbox/hex-formatter.json').read_text())
        raw=struct.pack('<24I',*[int(w,16) for w in data['raw_words']]);base=data['address']
        symbols={'fes_nano_mmu_hex_formatter':(base,96),'fes_nano_mmu_transition_park':(data['park'],8)}
        bounds={'ro_end':base+4096}
        def bytes_at(image,address,size):
            self.assertEqual(address,base);self.assertEqual(size,96);return image,0
        proof=view.verify_hex(raw,symbols,bounds,bytes_at)
        self.assertEqual(proof['contract'],view.HEX)
        for index in range(24):
            bad=bytearray(raw);bad[index*4]^=1
            with self.subTest(index=index),self.assertRaises(ValueError):view.verify_hex(bytes(bad),symbols,bounds,bytes_at)
        for register in (0,1,7,18,19,29,30):
            call_address=base+0x108;delta=(base-call_address)//4
            words=[0xaa0003ee|(register<<16),0xaa1e03e8,0x94000000|(delta&0x3ffffff),0xaa0803fe]
            view.verify_hex_call(words,2,call_address,proof)
            for index in range(4):
                broken=words.copy();broken[index]^=1
                with self.subTest(register=register,index=index),self.assertRaises(ValueError):view.verify_hex_call(broken,2,call_address,proof)
        with self.assertRaises(ValueError):view.verify_hex_call([0,0,0x94000000,0],2,base,None)


class TraceTests(unittest.TestCase):
    def test_three_different_views_are_observations_and_never_device_authority(self):
        state=view.state()
        for phase in range(3):
            for line in mailbox_rows(phase).replace('0000000000000000',format(phase+1,'016x')).splitlines():
                self.assertIsNone(view.observe(state,line,phase+1))
        self.assertTrue(state['complete']);self.assertFalse(state['hardware_authority'])
        self.assertEqual(state['records'][2]['values']['cin'],3)

    def test_complete_boot_requires_each_view_once_and_in_its_exact_phase(self):
        original=base_trace().replace('U-Boot 2025.01-fes-nano-',transition()+'U-Boot 2025.01-fes-nano-',1)
        self.assertTrue(decode(original)['firmware_mmu_trace']['complete'])
        for phase in range(3):
            event=mailbox_rows(phase)
            for replacement in ('',event*2,event.replace('phase='+str(phase),'phase=3'),
                                event.replace('values=0000000000000000,','values=0000000100000000,'),
                                event.replace(' BEGIN',' begin'),event.splitlines(keepends=True)[1]):
                with self.subTest(phase=phase,replacement=replacement):
                    self.assertFalse(decode(original.replace(event,replacement))['loader_ready'])
        self.assertFalse(decode(original.replace(mailbox_rows(2),'').replace(row('before'),mailbox_rows(2)+row('before')))['loader_ready'])

    def test_fault_after_begin_preserves_prefix_and_never_admits_boot(self):
        prefix=base_trace().split('U-Boot 2025.01-fes-nano-',1)[0]+row('before')+''.join(step_rows()[:15])+status_rows()
        event=vector_row('enabled');event=event.split(mailbox_rows(2),1)[0]+mailbox_rows(2).splitlines(keepends=True)[0]
        fault='FES_NANO_EL3_MMU_FAULT '+' '.join(name+'='+format(0x2f000010 if name=='esr' else 0,'016x') for name in __import__('mmu_trace').FAULT_FIELDS)+'\r\n'
        result=decode(prefix+event+fault)
        self.assertFalse(result['loader_ready']);self.assertEqual(result['firmware_mmu_trace']['mailbox_views']['pending']['phase'],2)
        self.assertEqual(len(result['firmware_mmu_trace']['faults']),1)

if __name__=='__main__':unittest.main()
