"""Build one pinned freestanding Agilex 5 CPU-jitter object without a network dependency."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
VENDOR = HERE.parent / 'nano_jitter_vendor'
COMMIT = 'fbbe36ed459a9f4eeb6caecfe66efe581a638fba'
FLAGS = ('-std=c11','-O0','-ffreestanding','-fno-builtin','-fno-pie','-fno-stack-protector',
         '-fno-lto','-fno-common','-ffunction-sections','-fdata-sections','-fstack-usage')
ARM = ('-mcpu=cortex-a55','-mgeneral-regs-only','-mstrict-align','-mno-outline-atomics',
       '-DAARCH64_NSTIME_REGISTER="cntpct_el0"')
OPTS = {'seed':['fes_nano_jitter_seed'],
        'capture':['fes_nano_jitter_record_open','fes_nano_jitter_record_sample','fes_nano_jitter_record_close',
                   'fes_nano_jitter_record_limit']}
PIN = '51ba9f6d26f42259aa145eee0874979df32e1601f8b38e80e66793a633567ba1'


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def produce(cc, output, mode, *, host_fixture=False, evidence_dir=None):
    if mode not in OPTS or sha(VENDOR/'sources.json') != PIN:
        raise ValueError('unexpected mode or upstream manifest')
    manifest=json.loads((VENDOR/'sources.json').read_bytes())
    if manifest['commit']!=COMMIT or any(sha(VENDOR/k)!=v for k,v in manifest['files'].items()):
        raise ValueError('different jitterentropy source')
    cc=shutil.which(cc)
    if not cc:
        raise ValueError('missing compiler')
    def run(args, **kwargs):
        return subprocess.run(args,check=True,**kwargs)
    machine=subprocess.check_output([cc,'-dumpmachine'],text=True).strip()
    if not machine.startswith('aarch64-') and not host_fixture:
        raise ValueError('AArch64 cross compiler required')
    cpu=['-DFES_JITTER_HOST_FIXTURE'] if host_fixture else list(ARM)
    if mode=='capture':
        cpu+=['-DFES_JITTER_RECORD','-DJENT_RAW_COLLECTOR']
    all_flags=[*FLAGS,*cpu,'-I'+str(HERE/'include'),'-I'+str(VENDOR),'-I'+str(VENDOR/'src')]
    if evidence_dir is not None:
        all_flags.append('-fcallgraph-info=su')
        evidence_dir.mkdir(parents=True,exist_ok=True)
    inputs=[VENDOR/k for k in manifest['files'] if k.endswith('.c')]
    inputs += [HERE/'adapter.c',HERE/'arena.c']
    # GCC's linker/assembler selection follows its own cross compiler.
    output=Path(output).absolute()
    output.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(dir=output.parent,prefix='jent-') as tmp:
        tmp=Path(tmp)
        def compile_one(src):
            flags=list(all_flags)
            if src.name=='jitterentropy-arch-timer.c':
                flags.append('-Djent_get_nstime=fes_jent_clock_raw')
            o=tmp/(src.stem+'.o');run([cc,*flags,'-c',str(src),'-o',str(o)])
            return o
        with ThreadPoolExecutor(max_workers=4) as workers:
            objects=list(workers.map(compile_one,inputs))
        script=tmp/'data.ld'
        # Include a PROGBITS anchor for all upstream/adapter static zero state;
        # the platform's split NOBITS region has no room for this allocator.
        script.write_text('SECTIONS { .data.fes_jent : ALIGN(16) { BYTE(0); *(.data .data.* .bss .bss.* COMMON) } }\n')
        link_args=['-nostdlib','-no-pie','-Wl,-r,--gc-sections,--build-id=none,-T,'+str(script)]
        run([cc,*link_args,*(f'-Wl,-u,{name}' for name in OPTS[mode]),*objects,'-o',str(tmp/'complete.o')])
        if evidence_dir is not None:
            for ext in ('*.su','*.ci'):
                for p in tmp.glob(ext): shutil.copyfile(p,evidence_dir/p.name)
        os.replace(tmp/'complete.o',output)


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--cc',required=True);p.add_argument('--output',required=True,type=Path)
    p.add_argument('--mode',required=True,choices=OPTS)
    a=p.parse_args();produce(a.cc,a.output,a.mode)
