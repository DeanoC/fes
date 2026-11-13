"""ELF admission of the bounded stack-free AArch64 transition diagnostic."""
import copy,shutil,struct,subprocess,sys,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'platform/embedded/firmware'))
import nano_firmware_mmu as mmu
import nano_firmware_ram as ram

class NanoMMUTransitionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which('cc') or not shutil.which('ld'):raise unittest.SkipTest('host ELF tools required')
        cls.temp=tempfile.TemporaryDirectory();cls.addClassCleanup(cls.temp.cleanup);base=Path(cls.temp.name)
        # GNU host assembly stores these AArch64 instruction words, not host code.
        # The real canonical ARM object and execution are separate integration checks.
        def constant(reg,value,wide):
            return [(0xd2800000 if wide else 0x52800000)|((value&65535)<<5)|reg,
                    (0xf2a00000 if wide else 0x72a00000)|((value>>16)<<5)|reg]
        words=[]
        for reg,value,wide in [(3,0x412fd050,True),(3,0x81000000,True),(10,0x10c02114,True),(13,0x10c02100,True),(11,100000,False)]:words+=constant(reg,value,wide)
        words+=[0x52800000|(384<<5)|17]
        text=['.global fixture\n.text\nfixture:']
        def label(name,offset,code,kind='function'):
            text.extend(['.org '+hex(offset),'.global fes_nano_mmu_transition_'+name,'.type fes_nano_mmu_transition_'+name+',@'+kind,'fes_nano_mmu_transition_'+name+':'])
            text.extend('.long '+hex(word) for word in code)
            text.append('.size fes_nano_mmu_transition_'+name+',.-fes_nano_mmu_transition_'+name)
        # Critical labels are independently declared as overlapping subfunctions.
        inventory=[]
        for name,address in mmu.mmu_status.MMIO:inventory+=constant(0,address,True)+[0xb9400000]
        aliases=[]
        def logging(parent,code,base):
            names=('arm',) if parent=='write' else (parent,)
            for name in names:
                aliases.append(('store_'+name,parent,len(code)*4,16))
                code+=constant(0,0x1c000100,True)+[0x52800001|((8 if name=='arm' else 0)<<5),0xb9000001,0xd5033f9f]
            states=(('arm_before',1),('armed',2)) if parent=='write' else ((parent+'_restoring',3),(parent+'_retired',4))
            for name,value in states:
                aliases.append(('state_'+name,parent,len(code)*4,12))
                delta=0x80007008-(base+len(code)*4)
                code+=[0x10000005|((delta&3)<<29)|(((delta>>2)&0x7ffff)<<5),0x52800001|(value<<5),0xb90000a1,0xd5033f9f]
            return code
        writer=logging('write',words+inventory,0x80000000)
        label('write',0,writer+[0xd51e1000,0xd5033fdf,0xd65f03c0])
        restorer=logging('restore',list(words),0x80000400)
        label('restore',0x400,restorer+[0xd51ec001,0xd5033fdf,0xd53ec003,0xd65f03c0])
        capture=logging('capture',words+inventory+[0xd538411e],0x80000800)
        label('capture',0x800,capture+[0x14000000|((0xc00-0x800-len(capture)*4)//4)])
        label('park',0xc00,[0xd503205f,0x17ffffff])
        label('vectors',0x2000,[word for i in range(16) for word in [0x14000000|(((0x800-0x2000-i*128)//4)&0x3ffffff)]+[0]*31],kind='object')
        label('park_vectors',0x2800,[word for i in range(16) for word in [0x14000000|(((0xc00-0x2800-i*128)//4)&0x3ffffff)]+[0]*31],kind='object')
        text+=['.org 0x4000','.global runtime_exceptions','runtime_exceptions:','.space 2048',
               '.global fes_nano_mmu_transition_sctlr','.set fes_nano_mmu_transition_sctlr,fes_nano_mmu_transition_write+'+str(len(writer)*4),'.type fes_nano_mmu_transition_sctlr,@function','.size fes_nano_mmu_transition_sctlr,8',
               '.global fes_nano_mmu_transition_restore_vbar','.set fes_nano_mmu_transition_restore_vbar,fes_nano_mmu_transition_restore+'+str(len(restorer)*4),'.type fes_nano_mmu_transition_restore_vbar,@function','.size fes_nano_mmu_transition_restore_vbar,12',
               '.data','.global fes_nano_mmu_transition_saved_vbar','.type fes_nano_mmu_transition_saved_vbar,@object','fes_nano_mmu_transition_saved_vbar:','.quad 0','.size fes_nano_mmu_transition_saved_vbar,8',
               '.global fes_nano_mmu_logging_state','.type fes_nano_mmu_logging_state,@object','fes_nano_mmu_logging_state:','.quad 0','.size fes_nano_mmu_logging_state,8','.section .note.GNU-stack,"",@progbits']
        for name,parent,offset,size in aliases:text+=['.global fes_nano_mmu_log_'+name,'.type fes_nano_mmu_log_'+name+',@function','.set fes_nano_mmu_log_'+name+',fes_nano_mmu_transition_'+parent+'+'+str(offset),'.size fes_nano_mmu_log_'+name+','+str(size)]
        for name in ('write','restore','capture'):text+=['.global fes_nano_mmu_uart_init_'+name,'.type fes_nano_mmu_uart_init_'+name+',@function','.set fes_nano_mmu_uart_init_'+name+',fes_nano_mmu_transition_'+name+'+16','.size fes_nano_mmu_uart_init_'+name+',16']
        (base/'fixture.S').write_text('\n'.join(text)+'\n');(base/'fixture.ld').write_text('ENTRY(fixture)\nSECTIONS { . = 0x80000000; .text : { *(.text*) } . = 0x80007000; .data : { *(.data*) } /DISCARD/ : { *(.note*) *(.eh_frame*) } }\n')
        for command in [['cc','-c',str(base/'fixture.S'),'-o',str(base/'fixture.o')],['ld','-T',str(base/'fixture.ld'),str(base/'fixture.o'),'-o',str(base/'fixture.elf')]]:
            p=subprocess.run(command,capture_output=True,text=True,timeout=30)
            if p.returncode:raise AssertionError(p.stderr)
        image=bytearray((base/'fixture.elf').read_bytes());struct.pack_into('<H',image,18,183);cls.image=bytes(image);cls.symbols=ram.symbols(cls.image)
        cls.bounds=dict(ro_end=0x80006000,data_start=0x80007000,data_end=0x80007010)

    def changed(self,name,word,delta=0):
        image=bytearray(self.image);address=self.symbols[name if name.startswith('fes_') else 'fes_nano_mmu_transition_'+name][0]+delta
        phoff=struct.unpack_from('<Q',image,32)[0];stride,count=struct.unpack_from('<HH',image,54)
        for i in range(count):
            kind,flags,off,va,pa,filesz,memsz,align=struct.unpack_from('<IIQQQQQQ',image,phoff+i*stride)
            if kind==1 and va<=address<va+filesz:
                struct.pack_into('<I',image,off+address-va,word);return bytes(image)
        self.fail('missing fixture storage')

    def test_complete_linked_leaf_vectors_bounds_and_one_use_data(self):
        proof=mmu.verify_transition(self.image,self.symbols,self.bounds)
        self.assertEqual(proof['vectors']['bytes'],2048);self.assertEqual(proof['uart_polls'],100000)
        self.assertEqual(proof['saved_vbar'],0x80007000)

    def test_code_writes_barriers_stack_calls_return_and_park_mutations_reject(self):
        for name,word,delta in [('sctlr',0xd503201f,0),('sctlr',0xd503201f,4),('restore_vbar',0xd503201f,0),('restore_vbar',0xd503201f,8),
                                ('capture',0xa9bf7bfd,0),('capture',0x910003ff,0),('capture',0x94000000,0),('capture',0xd65f03c0,0),
                                ('capture',0xd69f03e0,0),('vectors',0xd503201f,0),('park_vectors',0xd503201f,128),('park',0xd65f03c0,0),('saved_vbar',1,0)]:
            with self.subTest(name=name,word=word,delta=delta),self.assertRaises(ValueError):
                mmu.verify_transition(self.changed(name,word,delta),self.symbols,self.bounds)

    def test_architecture_alignment_missing_code_or_private_data_drift_rejects(self):
        image=bytearray(self.image);struct.pack_into('<H',image,18,62)
        with self.assertRaises(ValueError):mmu.verify_transition(bytes(image),self.symbols,self.bounds)
        for name in ('write','capture','restore','vectors','park_vectors','park','saved_vbar'):
            symbols=copy.deepcopy(self.symbols);address,size=symbols['fes_nano_mmu_transition_'+name];symbols['fes_nano_mmu_transition_'+name]=(address+1,size)
            with self.subTest(name=name),self.assertRaises(ValueError):mmu.verify_transition(self.image,symbols,self.bounds)
        symbols=dict(self.symbols);del symbols['runtime_exceptions']
        with self.assertRaises(ValueError):mmu.verify_transition(self.image,symbols,self.bounds)

    def test_logging_control_and_lifetime_targets_values_stores_and_barriers_are_exact(self):
        proof=mmu.verify_transition(self.image,self.symbols,self.bounds)
        for name,site in proof['logging_sites'].items():
            for delta in range(0,site['bytes']+4,4):
                with self.subTest(name=name,delta=delta),self.assertRaises(ValueError):
                    mmu.verify_transition(self.changed(name,0xd503201f,delta),self.symbols,self.bounds)
        with self.assertRaises(ValueError):
            mmu.verify_transition(self.changed('fes_nano_mmu_logging_state',1),self.symbols,self.bounds)

if __name__=='__main__':unittest.main()
