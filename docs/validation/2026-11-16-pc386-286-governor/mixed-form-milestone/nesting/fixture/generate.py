#!/usr/bin/env python3
"""Generate independent nested ENTER controls, paired full-loop timing."""
from pathlib import Path
import json,subprocess,hashlib
p=Path(__file__).resolve().parent
n='/home/deano/fes-scratch/u6ref/tools/nasm/nasm'
cases=[(f'L{i}_A0_DEC',i,0,'dec') for i in range(5)]+[('L1_A6_DEC',1,6,'dec'),('L2_A12_DEC',2,12,'dec'),('L3_A32_DEC',3,32,'dec'),('L4_A256_DEC',4,256,'dec'),('L2_A0_LOOP',2,0,'loop')]
def body(i,lev,alloc,scaf):
 return f'.i{i}:\n        enter {alloc},{lev}\n        leave\n'+(f'        dec si\n        jnz .i{i}\n' if scaf=='dec' else f'        loop .i{i}\n')
def control(lev,alloc):
 s='''        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
'''+f'        enter {alloc},{lev}\n'+'''        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
'''+f'        sub ax,{2*(lev+1)+alloc}\n'+'''        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
'''
 for i in range(1,lev):s+=f'        cmp word [ss:bx-{i*2}],0x{[0x1122,0x3344,0x5566][i-1]:04x}\n        jne invalid_memory\n'
 if lev:s+=f'        cmp word [ss:bx-{lev*2}],bx\n        jne invalid_memory\n'
 s+='''        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
'''
 return s
def loopsetup(scaf):return '        mov bp,0x6000\n        mov '+('si' if scaf=='dec' else 'cx')+',1000\n        align 2\n'
base=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev/pc386-gov-heldout/heldout.asm').read_text().replace('cpu 8086','cpu 286')
old='''        mov ax,0x351c
        int 0x21
'''
new='''        mov [saved_orig_sp],sp
        mov [saved_orig_ss],ss
        cli
        mov ax,cs
        mov ss,ax
        mov sp,0x7000
        sti
        mov ax,0x351c
        int 0x21
'''
assert base.count(old)==1;base=base.replace(old,new)
a=base.index('        MEASURE msg_bd, bd');b=base.index('        mov dx,[oldoff]',a)
base=base[:a]+''.join(f'        MEASURE msg_{i}, test_{i}\n' for i in range(len(cases)))+base[b:]
a=base.index('bd:     call t_start');b=base.index('tickisr:',a)
tests=''
for i,(_,lev,alloc,scaf) in enumerate(cases):
 tests+=f'test_{i}:\n'+control(lev,alloc)+'        call t_start\n.block:\n'+loopsetup(scaf)+body(i,lev,alloc,scaf)+'        FINISH\n'
base=base[:a]+tests+'\n'+base[b:]
a=base.index('msg_bd:');b=base.index('        align 2\noldoff:',a)
base=base[:a]+''.join(f"msg_{i}: db '{name}=',0\n" for i,(name,_,_,_) in enumerate(cases))+base[b:]
base=base.replace("hdr:    db", "saved_orig_sp: dw 0\nsaved_orig_ss: dw 0\nsaved_test_sp: dw 0\nmsg_bad: db 'ENTER_CONTROL_FAILED',13,10,0\nhdr:    db",1)
old='''        mov ax,0x4c00
        int 0x21

t_start:'''
new='''        mov ax,cs
        mov ds,ax
        cli
        mov ss,[saved_orig_ss]
        mov sp,[saved_orig_sp]
        sti
        mov ax,0x4c00
        int 0x21
invalid_memory:
        mov sp,[saved_test_sp]
        sti
        mov si,msg_bad
        call puts
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,cs
        mov ds,ax
        cli
        mov ss,[saved_orig_ss]
        mov sp,[saved_orig_sp]
        sti
        mov ax,0x4c01
        int 0x21

t_start:'''
assert old in base;base=base.replace(old,new,1)
rtl='''bits 16
cpu 286
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
'''
for i,(_,lev,alloc,scaf) in enumerate(cases):
 rtl+=f'        call control_{i}\n        mov al,{2*i}\n        out 0x80,al\n'+loopsetup(scaf)+body(i,lev,alloc,scaf)+f'        mov al,{2*i+1}\n        out 0x80,al\n'
rtl+='''        cli
        xor al,al
        out 0x8f,al
        hlt
invalid_memory:
        cli
        mov al,0xee
        out 0x8f,al
        hlt
'''
for i,(_,lev,alloc,_) in enumerate(cases):rtl+=f'control_{i}:\n'+control(lev,alloc)+'        ret\n'
rtl+='saved_test_sp: dw 0\n'
(p/'reference.asm').write_text(base);(p/'rtl.asm').write_text(rtl);(p/'cases.json').write_text(json.dumps(cases,indent=2)+'\n')
for src,dst in [('reference.asm','SURVEY.COM'),('rtl.asm','rtl.bin')]:subprocess.run([n,'-f','bin','-o',str(p/dst),'-l',str(p/(src+'.lst')),str(p/src)],check=True,timeout=20)
for f in ('reference.asm','SURVEY.COM','rtl.asm','rtl.bin','cases.json'):
 print(f,hashlib.sha256((p/f).read_bytes()).hexdigest())
