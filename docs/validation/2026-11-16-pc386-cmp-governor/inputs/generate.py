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
CPU = ROOT / 'out/dev/1902-event-combined-v2/iret-control/Vfes_i386_cpu'
CPU_MANIFEST = ROOT / 'out/dev/1902-event-combined-v2/manifest.json'
COUNT = 1000

CASES = [
 ('MEM8_BX','cmp [bx],al',0,'dec'),
 ('MEM16_BX','cmp [bx],ax',0,'dec'),
 ('REG8_BX','cmp al,[bx]',0,'dec'),
 ('REG16_BX','cmp ax,[bx]',0,'dec'),
 ('MEM8_DISP','cmp [bx+2],al',0,'dec'),
 ('MEM16_INDEX','cmp [bx+si],ax',0,'dec'),
 ('REG8_DIRECT','cmp al,[0x8004]',0,'dec'),
 ('REG16_DISP','cmp ax,[bx+2]',0,'dec'),
 ('MEM8_DS','db 0x3e\n cmp [bx],al',0,'dec'),
 ('MEM16_ES','db 0x26\n cmp [bx+si],ax',0,'dec'),
 ('REG8_DS','db 0x3e\n cmp al,[bx+2]',0,'dec'),
 ('REG16_ES','db 0x26\n cmp ax,[0x8004]',0,'dec'),
 ('MEM8_LOOP','cmp [bx],al',0,'loop'),
 ('MEM16_LOOP','cmp [bx],ax',0,'loop'),
 ('REG8_LOOP','cmp al,[bx]',0,'loop'),
 ('REG16_LOOP','cmp ax,[bx]',0,'loop'),
 ('MEM8_NE','cmp [bx],al',1,'dec-ne'),
 ('MEM16_NE','cmp [bx],ax',1,'dec-ne'),
 ('REG8_NE','cmp al,[bx]',1,'dec-ne'),
 ('REG16_NE','cmp ax,[bx]',1,'dec-ne'),
]

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
 nop
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
 cmp byte [bx],0xf6
 jne .bad
 cmp byte [bx+1],0x37
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
 add word [ss:bp+2],2
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

def body(kind, n, mode):
    tail = 'loop .inner' if mode == 'loop' else 'dec bp\n jnz .inner'
    return '.inner:\n '+('nop\n ' * n)+kind+'\n pushf\n pop di\n '+tail+'\n'

SETUP = ''' mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
'''
CHECK = ''' and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
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

def case_setup(mode):
    return SETUP.replace('0x1234\n mov word [0x8006],0x1234', '0x1235\n mov word [0x8006],0x1235') if mode == 'dec-ne' else SETUP

def case_check(mode, kind):
    check = CHECK
    if mode == 'dec-ne':
        expected = '0x95' if kind.startswith('cmp a') else '0'
        check = check.replace('cmp di,0x44', 'cmp di,' + expected)
        check = check.replace('cmp word [0x8004],0x1234','cmp word [0x8004],0x1235').replace('cmp word [0x8006],0x1234','cmp word [0x8006],0x1235')
    counters = (' cmp bp,1000\n jne failed\n cmp cx,0\n jne failed\n' if mode == 'loop' else ' cmp bp,0\n jne failed\n cmp cx,1000\n jne failed\n')
    return counters + check

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
        mov dx,handler_de
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
        mov ax,cs
        mov es,ax
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
        expected = '.inner'
        tests += f'test_{i}: call t_start\n.block:\n mov word [0x8014],{expected}\n' + case_setup(mode) + body(kind,n,mode) + case_check(mode,kind) + '        FINISH\n'
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
 mov word [0],handler_de
 mov word [2],0
 mov word [4],handler_db
 mov word [6],0
 call control_shifts
'''
    for i, (_, kind, n, mode) in enumerate(CASES):
        expected = '.inner'
        rtl += f'rtl_case_{i}:\n mov al,{2*i}\n out 0x80,al\n mov word [0x8014],{expected}\n' + case_setup(mode) + body(kind,n,mode) + case_check(mode,kind) + f' mov al,{2*i+1}\n out 0x80,al\n'
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
        assert ref_bytes.count(data)==1, (name, 'reference', ref_bytes.count(data))
        assert rtl_bytes.count(data)==1, (name, 'rtl', rtl_bytes.count(data))
        cases.append(dict(name=name, kind=kind, count=n, mode=mode, body_sha256=digest(binary), body_hex=data.hex(), reference_offset=ref_bytes.index(data), rtl_offset=rtl_bytes.index(data)))
    cpu_manifest = json.loads(CPU_MANIFEST.read_text())
    assert digest(CPU) == '4414d541cb6d386ab7332e7d8f6b0b8a105686d3881945d6a4873bae6576556f'
    for path, expected_hash in cpu_manifest['sources'].items():
        assert digest(Path(path)) == expected_hash, path
    files=[Path(__file__),TEMPLATE,NASM,CPU,CPU_MANIFEST,HERE/'reference.asm',HERE/'rtl.asm',HERE/'SURVEY.COM',HERE/'rtl.bin']
    manifest={'git_head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'cpu_diagnostic_enabled':True,'files':{str(p):digest(p) for p in files},'cases':cases}
    (HERE/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print(f'Prepared {len(cases)} exact byte-matched loops; manifest: {HERE / "manifest.json"}')

if __name__=='__main__':
    main()
