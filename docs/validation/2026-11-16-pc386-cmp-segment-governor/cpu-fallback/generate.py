#!/usr/bin/env python3
"""Freeze whole-CPU governor differential stimuli; no reference VM."""
from pathlib import Path
import hashlib,json,subprocess
P=Path(__file__).resolve().parent
NASM=Path('/home/deano/fes-scratch/u6ref/tools/nasm/nasm')
CASES=[
 ('BASE8','cmp [bx],al'),('BASE16','cmp [bx],ax'),
 ('FS_DS8','db 0x64,0x3e\n cmp [bx],al'),('FS_DS16','db 0x64,0x3e\n cmp [bx],ax'),
 ('GS_DS8','db 0x65,0x3e\n cmp [bx],al'),('GS_DS16','db 0x65,0x3e\n cmp [bx],ax'),
 ('OPSIZE8','db 0x66,0x3e\n cmp [bx],al'),('OPSIZE32','db 0x3e\n cmp [bx],eax'),
 ('ADDRSIZE8','db 0x3e\n cmp [ebx],al'),('ADDRSIZE16','db 0x3e\n cmp [ebx],ax'),
 ('EA3_8','db 0x3e\n cmp [bx+si+2],al'),('EA3_16','db 0x3e\n cmp [bx+si+2],ax'),
 ('SEG8','db 0x3e\n cmp [bx],al'),('SEG16','db 0x3e\n cmp [bx],ax')]
SETUP='''mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
'''
CHECK='''and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
'''
prog='''bits 16
cpu 386
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
for i,(name,ins) in enumerate(CASES):
 prog+=f'case_{i}:\n mov al,{2*i}\n out 0x80,al\n'+SETUP+f'.inner:\n {ins}\n pushf\n pop di\n dec bp\n jnz .inner\n'+CHECK+f' mov al,{2*i+1}\n out 0x80,al\n'
prog+='''mov al,0
 out 0x8f,al
 hlt
failed:
 mov al,0xff
 out 0x8e,al
 hlt
'''
(P/'rtl.asm').write_text(prog)
subprocess.run([str(NASM),'-f','bin','-o',str(P/'rtl.bin'),str(P/'rtl.asm')],check=True,timeout=20)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
CPUS=[Path('out/dev/1902-cmp-milestone-v1/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu').resolve(),Path('out/dev/1902-cmp-segment-model-v3/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu').resolve()]
manifest={'cases':[{'name':n,'instruction':s} for n,s in CASES],'files':{str(p):sha(p) for p in [Path(__file__).resolve(),NASM,P/'rtl.asm',P/'rtl.bin',*CPUS]}}
(P/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print('Frozen',len(CASES),'cases; input',sha(P/'rtl.bin'))
