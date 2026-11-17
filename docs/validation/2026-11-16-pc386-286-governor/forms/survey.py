#!/usr/bin/env python3
"""Private diagnostic: generate equal instruction loops for DOS and FES RTL.

Reference numbers measure full loops through BIOS timer sampling, not individual
instruction cycles. Fixed reference ceiling and coarse quantization apply.
No kit access; no automatic promotion or hardware acceptance.
"""
import hashlib,json,os,subprocess,sys
from pathlib import Path
P=Path(__file__).resolve().parent
N=Path('/home/deano/fes-scratch/u6ref/tools/nasm/nasm')
SOURCE=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev/pc386-gov-heldout')
CASES=[('CMP_WORD_SI','cmp dx,[si]'), ('CMP_BYTE_SI','cmp dh,[si]'), ('FLAGS_OTHER','pushf\n pop si'), ('SHORT_JMP','jmp short .jump_done\n nop\n.jump_done: nop'), ('NEAR_JMP','jmp near .near_done\n nop\n.near_done: nop'), ('CALL_RET_NEAR','call .callee\n jmp near .call_done\n.callee: inc dx\n ret\n.call_done: nop')]

COUNT=1000

def body(i,ins):
 return f'.i{i}:\n  {ins}\n  dec bp\n  jnz .i{i}\n'

def gen():
 ref=SOURCE.joinpath('heldout.asm').read_text()
 ref=ref.replace('        mov si,hdr\n', '        mov word [0x8000],1\n        mov word [0x8002],0\n        mov si,hdr\n', 1)
 a=ref.index('        MEASURE msg_bd, bd'); b=ref.index('        mov dx,[oldoff]',a)
 ref=ref[:a]+''.join(f'        MEASURE msg_{i}, test_{i}\n' for i in range(len(CASES)))+ref[b:]
 a=ref.index('bd:     call t_start'); b=ref.index('tickisr:',a)
 tests=''
 for i,(name,ins) in enumerate(CASES):
  tests+=f'test_{i}: call t_start\n.block: mov bp,{COUNT}\n        mov bx,0x8000\n        mov si,0x8000\n        mov dx,1\n        mov cx,1\n        align 2\n'+body(i,ins)+'        FINISH\n'
 ref=ref[:a]+tests+'\n'+ref[b:]
 a=ref.index('msg_bd:'); b=ref.index('        align 2\noldoff:',a)
 ref=ref[:a]+''.join(f"msg_{i}: db '{name}=',0\n" for i,(name,_) in enumerate(CASES))+ref[b:]
 rtl='bits 16\ncpu 286\norg 0x1000\nstart:\n cli\n cld\n xor ax,ax\n mov ds,ax\n mov es,ax\n mov ss,ax\n mov sp,0x7000\n mov word [0x8000],1\n mov word [0x8002],0\n'
 for i,(name,ins) in enumerate(CASES):
  rtl+=f' mov bp,{COUNT}\n mov al,{2*i}\n out 0x80,al\n mov bx,0x8000\n mov si,0x8000\n mov dx,1\n mov cx,1\n align 2\n'+body(i,ins)+f' mov al,{2*i+1}\n out 0x80,al\n'
 rtl+=' mov al,0\n out 0x8f,al\n hlt\n'
 for name,data in [('reference.asm',ref),('rtl.asm',rtl)]: (P/name).write_text(data)
 for src,dst in [('reference.asm','SURVEY.COM'),('rtl.asm','rtl.bin')]:
  subprocess.run([str(N),'-f','bin','-o',str(P/dst),str(P/src)],check=True,timeout=20)
 (P/'cases.json').write_text(json.dumps(dict(CASES),indent=2)+'\n')

def run_rtl():
 tb=P/'candidate/Vfes_i386_cpu'
 env=dict(os.environ,FES_I386_MODE='governor',FES_I386_LATENCY='0',FES_I386_PROGRAM=str(P/'rtl.bin'),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_IRQ_PERIOD='0')
 r=subprocess.run([str(tb)],env=env,capture_output=True,text=True,timeout=120)
 (P/'rtl.log').write_text(r.stdout+'\nSTDERR\n'+r.stderr)
 if r.returncode not in (0,1): raise ValueError('CPU benchmark exited unexpectedly')
 marks={}
 for l in r.stdout.splitlines():
  fields=l.split()
  if len(fields)==5 and fields[0]=='MARK' and int(fields[1],16)==0x80:
   k=int(fields[2]);
   if k in marks: raise ValueError('duplicate mark')
   marks[k]=tuple(map(int,fields[3:]))
 if set(marks)!=set(range(2*len(CASES))) or 'halted=1' not in r.stdout: raise ValueError('missing mark or halted')
 data={name:{'charged':(marks[2*i+1][1]-marks[2*i][1])/COUNT,'cycles':marks[2*i+1][0]-marks[2*i][0]} for i,(name,_) in enumerate(CASES)}
 (P/'rtl.json').write_text(json.dumps(data,indent=2)+'\n')
 print(json.dumps(data,indent=2),flush=True)

if __name__=='__main__':
 if sys.argv[1:] == ['generate']: gen()
 elif sys.argv[1:] == ['rtl']: run_rtl()
 else: raise SystemExit('use generate or rtl')
