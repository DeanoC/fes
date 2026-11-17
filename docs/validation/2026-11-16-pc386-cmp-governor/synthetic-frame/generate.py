#!/usr/bin/env python3
"""Prepare paired fault/trap timing programs; generation does not run either CPU."""
import hashlib
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
NASM = Path('/home/deano/fes-scratch/u6ref/tools/nasm/nasm')
TEMPLATE = ROOT / 'out/dev/pc386-gov-heldout/heldout.asm'
CPU = ROOT / 'out/dev/1902-wide-zero-v11/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu'
CPU_MANIFEST = ROOT / 'out/dev/1902-wide-zero-v11/manifest.json'
COUNT = 1000

CASES = [(f'{kind.upper()}_P{n}',kind,n,'entry') for n in (0, 8) for kind in ('full','reduced')]

HANDLERS = '''
control_shifts:
 mov si,0x8000
 mov word [si],0x5aa5
 xor cx,cx
 xor dx,dx
 stc
 shr word [si],cl
 pushf
 pop ax
 and ax,0x41
 cmp ax,0x41
 jne failed
 cmp word [si],0x5aa5
 jne failed
 xor dx,dx
 stc
 shr word [si],0
 pushf
 pop ax
 and ax,0x41
 cmp ax,0x41
 jne failed
 cmp word [si],0x5aa5
 jne failed
 ret
handler_de:
 push bp
 mov bp,sp
 push ax
 push bx
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 mov al,[0x8016]
 cmp [bx],al
 jne .bad
 mov al,[0x8017]
 cmp [bx+1],al
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 xor ax,ax
 mov al,[0x8018]
 add [ss:bp+2],ax
 pop ds
 pop bx
 pop ax
 pop bp
 iret
handler_db:
 push bp
 mov bp,sp
 push ax
 push bx
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 cmp byte [bx],0x4d
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x141
 cmp ax,0x140
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 and word [ss:bp+6],0xfeff
 pop ds
 pop bx
 pop ax
 pop bp
 iret
'''
_start=HANDLERS.index('handler_de:')
_split=HANDLERS.index('handler_db:')
_fault=HANDLERS[_start:_split]
_reduced=_fault.replace(' push ds\n push cs\n pop ds\n','').replace(' pop ds\n','')
HANDLERS=(HANDLERS[:_start]+_fault.replace('handler_de:','handler_a:')
 +_fault.replace('handler_de:','handler_b:\n times 8 nop')
 +_reduced.replace('handler_de:','handler_c:')
 +_reduced.replace('handler_de:','handler_d:\n times 8 nop')+HANDLERS[_split:])

def body(kind, n, mode):
    return '.inner:\n xor ax,ax\n xor dx,dx\n clc\n pushf\n push cs\n push si\n jmp di\n.resume:\n dec bp\n jnz .inner\n'

SETUP = ''' mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov word [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
'''
CHECK = ''' cmp word [0x8006],1000
 jne failed
 cmp word [0x8008],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
'''

def nasm(src, dst):
    subprocess.run([str(NASM), '-f', 'bin', '-o', str(dst), str(src)], check=True, timeout=20)

def digest(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()

def main():
    ref = TEMPLATE.read_text().replace('cpu 8086', 'cpu 286')
    ref = ref.replace('        mov si,hdr\n', '''        mov ax,0x3500
        int 0x21
        mov [de_oldoff],bx
        mov [de_oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,handler_a
        mov ax,0x2500
        int 0x21
        mov ax,0x3501
        int 0x21
        mov [db_oldoff],bx
        mov [db_oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,handler_db
        mov ax,0x2501
        int 0x21
        call control_shifts
        mov si,hdr
''', 1)
    a, b = ref.index('        MEASURE msg_bd, bd'), ref.index('        mov dx,[oldoff]')
    ref = ref[:a] + ''.join(f'        MEASURE msg_{i}, test_{i}\n' for i in range(len(CASES))) + '''        mov si,okmsg
        call puts
cleanup:
        mov dx,[de_oldoff]
        mov ax,[de_oldseg]
        mov ds,ax
        mov ax,0x2500
        int 0x21
        mov ax,cs
        mov ds,ax
        mov dx,[db_oldoff]
        mov ax,[db_oldseg]
        mov ds,ax
        mov ax,0x2501
        int 0x21
        mov ax,cs
        mov ds,ax
''' + ref[b:]
    a, b = ref.index('bd:     call t_start'), ref.index('tickisr:')
    tests = ''
    for i, (_, kind, n, mode) in enumerate(CASES):
        expected = '.resume'
        first,second,skip=(0x4d,0x75,0)
        handler = {('full',0):'handler_a',('full',8):'handler_b',('reduced',0):'handler_c',('reduced',8):'handler_d'}[(kind,n)]
        tests += f'test_{i}: mov dx,{handler}\n mov ax,0x2500\n int 0x21\n call t_start\n.block:\n mov word [0x8014],{expected}\n mov byte [0x8016],{first}\n mov byte [0x8017],{second}\n mov byte [0x8018],{skip}\n' + SETUP + f' mov si,.resume\n mov di,{handler}\n' + body(kind,n,mode) + CHECK + '        FINISH\n'
    ref = ref[:a] + tests + '''failed:
        mov si,failmsg
        call puts
        jmp cleanup
''' + HANDLERS + '\n' + ref[b:]
    a, b = ref.index('msg_bd:'), ref.index('        align 2\noldoff:')
    ref = ref[:a] + ''.join(f"msg_{i}: db '{name}=',0\n" for i,(name,*_) in enumerate(CASES)) + '''okmsg: db 'EVENT_CONTROL_VERIFIED',13,10,0
failmsg: db 'EVENT_CONTROL_FAILED',13,10,0
de_oldoff: dw 0
de_oldseg: dw 0
db_oldoff: dw 0
db_oldseg: dw 0
''' + ref[b:]
    rtl = '''bits 16
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
 mov word [0],handler_a
 mov word [2],0
 mov word [4],handler_db
 mov word [6],0
 call control_shifts
'''
    for i, (_, kind, n, mode) in enumerate(CASES):
        expected = '.resume'
        first,second,skip=(0x4d,0x75,0)
        handler = {('full',0):'handler_a',('full',8):'handler_b',('reduced',0):'handler_c',('reduced',8):'handler_d'}[(kind,n)]
        rtl += f'rtl_case_{i}:\n mov word [0],{handler}\n mov al,{2*i}\n out 0x80,al\n mov word [0x8014],{expected}\n mov byte [0x8016],{first}\n mov byte [0x8017],{second}\n mov byte [0x8018],{skip}\n' + SETUP + f' mov si,.resume\n mov di,{handler}\n' + body(kind,n,mode) + CHECK + f' mov al,{2*i+1}\n out 0x80,al\n'
    rtl += ''' mov al,0
 out 0x8f,al
 hlt
failed:
 mov al,0xff
 out 0x8e,al
 hlt
''' + HANDLERS
    for name,data in [('reference.asm',ref),('rtl.asm',rtl)]:
        (HERE/name).write_text(data)
    nasm(HERE/'reference.asm',HERE/'SURVEY.COM')
    nasm(HERE/'rtl.asm',HERE/'rtl.bin')
    ref_bytes=(HERE/'SURVEY.COM').read_bytes()
    rtl_bytes=(HERE/'rtl.bin').read_bytes()
    cases=[]
    for i,(name,kind,n,mode) in enumerate(CASES):
        source=HERE/f'body-{i}.asm'
        binary=HERE/f'body-{i}.bin'
        source.write_text('bits 16\ncpu 286\norg 0\n'+body(kind,n,mode))
        nasm(source,binary)
        data=binary.read_bytes()
        assert ref_bytes.count(data)==4, (name, 'reference', ref_bytes.count(data))
        assert rtl_bytes.count(data)==4, (name, 'rtl', rtl_bytes.count(data))
        index=i
        ref_offsets=[i for i in range(len(ref_bytes)) if ref_bytes.startswith(data,i)]
        rtl_offsets=[i for i in range(len(rtl_bytes)) if rtl_bytes.startswith(data,i)]
        cases.append(dict(name=name, kind=kind, count=n, mode=mode, body_sha256=digest(binary), body_hex=data.hex(), reference_offset=ref_offsets[index], rtl_offset=rtl_offsets[index]))
    cpu_manifest = json.loads(CPU_MANIFEST.read_text())
    assert digest(CPU) == cpu_manifest['binary_sha256']
    for path, expected_hash in cpu_manifest['sources'].items():
        assert digest(Path(path)) == expected_hash, path
    files=[Path(__file__),TEMPLATE,NASM,CPU,CPU_MANIFEST,HERE/'reference.asm',HERE/'rtl.asm',HERE/'SURVEY.COM',HERE/'rtl.bin']
    manifest={'git_head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'cpu_diagnostic_enabled':True,'files':{str(p):digest(p) for p in files},'cases':cases}
    (HERE/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print(f'Prepared {len(cases)} exact byte-matched loops; manifest: {HERE / "manifest.json"}')

if __name__=='__main__':
    main()
