#!/usr/bin/env python3
"""Private whole-CPU ENTER st2 predicate controls; no emulator oracle."""
from pathlib import Path
import json,subprocess,hashlib
p=Path(__file__).resolve().parent
# target crossing is VGA->RAM in the bench's synthetic map; its final frame
# push is VGA too, so this case checks fallback, not isolated attribution.
cases=[
 ('ram',0,0x5002,0x7000,False,True),
 ('source_page_end',0,0x5000,0x7000,False,True),
 ('source_page_cross',0,0x5001,0x7000,False,False),
 ('target_ram_line_cross',0,0x5002,0x7003,False,True),
 ('source_mmio',0x9800,0x8002,0x7000,False,False),
 ('target_mmio',0x9800,0x7002,0x8006,False,False),
 ('target_mmio_ram_cross',0xb800,0xa002,0x8003,False,False),
 ('prefix_fallback',0,0x5002,0x7000,True,False),
]
s='''bits 16
cpu 286
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
'''
for i,(name,seg,bp,sp,prefix,eligible) in enumerate(cases):
 src=(bp-2)&0xffff; first=(sp-2)&0xffff;copy=(sp-4)&0xffff;last=(sp-6)&0xffff
 s+=f'''
 ; {name}: source ES:{src:04x}, frame targets ES:{first:04x}/{copy:04x}/{last:04x}
 mov ax,0x{seg:04x}
 mov ss,ax
 mov es,ax
 mov sp,0x{sp:04x}
 mov bp,0x{bp:04x}
 mov word [es:0x{src:04x}],0xabcd
 jmp .clean{i}
.clean{i}:
 nop
 nop
 nop
 nop
 mov al,{2*i}
 out 0x80,al
'''+(' db 0x26\n' if prefix else '')+f''' enter 0,2
 mov al,{2*i+1}
 out 0x80,al
 cmp bp,0x{first:04x}
 jne fail
 cmp sp,0x{last:04x}
 jne fail
 cmp word [es:0x{first:04x}],0x{bp:04x}
 jne fail
 cmp word [es:0x{copy:04x}],0xabcd
 jne fail
 cmp word [es:0x{last:04x}],0x{first:04x}
 jne fail
 leave
 cmp bp,0x{bp:04x}
 jne fail
 cmp sp,0x{sp:04x}
 jne fail
'''
s+=''' cli
 xor al,al
 out 0x8f,al
 hlt
fail:
 cli
 mov al,0xee
 out 0x8f,al
 hlt
'''
(p/'controls.asm').write_text(s);(p/'cases.json').write_text(json.dumps(cases,indent=2)+'\n')
subprocess.run(['/home/deano/fes-scratch/u6ref/tools/nasm/nasm','-f','bin','-o',str(p/'controls.bin'),'-l',str(p/'controls.lst'),str(p/'controls.asm')],check=True,timeout=20)
for x in ('controls.asm','controls.bin','cases.json'):print(x,hashlib.sha256((p/x).read_bytes()).hexdigest())
