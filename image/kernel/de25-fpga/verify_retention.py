#!/usr/bin/env python3
"""Execute the pinned manager lifecycle with injected service failures."""
import argparse
import json
from pathlib import Path
import subprocess
from verify_write import patched_source, digest, LANE


def functions(source):
    text=source.decode()
    result=[]
    for name,end in [('s10_ops_write_init','\n/*'),('s10_ops_write','\nstatic int s10_ops_write_complete('),
                     ('s10_ops_write_complete','\nstatic const struct fpga_manager_ops')]:
        start=text.index('static int '+name+'(')
        result.append(text[start:text.index(end,start)])
    return '\n'.join(result)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source',required=True,type=Path)
    parser.add_argument('--output',required=True,type=Path)
    parser.add_argument('--cc',default='cc')
    args=parser.parse_args()
    name='drivers/fpga/stratix10-soc.c'
    identity,original,patched=patched_source(args.source,name)
    args.output.mkdir(parents=True,exist_ok=False)
    unit=args.output/'retention-functions.c';unit.write_text(functions(patched))
    harness=args.output/'retention-test.c';harness.write_bytes((LANE/'retention_test.c').read_bytes())
    # Match the kernel's unused callback-parameter policy; preserve C signatures.
    command=[args.cc,'-std=c11','-O2','-Wall','-Wextra','-Werror','-Wno-unused-parameter',str(harness),'-o']
    results={}
    for label,flags in [('enabled',[]),('disabled',['-DCONFIG_FES_DE25_DMA_PLATFORM=0'])]:
        binary=args.output/('retention-'+label)
        subprocess.run([*command,str(binary),*flags],check=True)
        r=subprocess.run([str(binary.resolve())],check=True,capture_output=True,timeout=10)
        (args.output/(label+'.log')).write_bytes(r.stdout+r.stderr)
        results[label]=r.stdout.decode().splitlines()
    # The original callback bodies are an independent retry regression control.
    unit.write_text(functions(original))
    control=args.output/'retention-control'
    subprocess.run([*command,str(control)],check=True)
    rejected=subprocess.run([str(control.resolve()),'retry-control'],capture_output=True,timeout=5)
    (args.output/'control.log').write_bytes(rejected.stdout+rejected.stderr)
    if rejected.returncode==0 or b'failed load must refuse a new caller' not in rejected.stderr:
        raise ValueError('original manager did not reproduce unsafe retry')
    unit.write_text(functions(patched))
    receipt={'classification':'actual init/write/complete functions; injected service and clock',
             'kernel_revision':identity['kernel_revision'],'manager':identity['files'][name],
             'patch_sha256':identity['patch_sha256'],'compiler_command':command,'cases':results,
             'control':'original manager permits retry after failed init; expected assertion',
             'hardware_access':False,'dma_fence':False,'secure_call_return':'not-established',
             'sha256':{p.name:digest(p.read_bytes()) for p in args.output.iterdir() if p.is_file()}}
    (args.output/'checks.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print('\n'.join(results['enabled']))


if __name__=='__main__':
    main()
