#!/usr/bin/env python3
"""Join the complete secure stop callback to both actual FPGA reader models."""
import hashlib
import json
from pathlib import Path
import subprocess
import uuid

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]

def main():
    output=ROOT/'out'/('nano-fabric-stop-'+uuid.uuid4().hex)
    captured=output/'source';captured.mkdir(parents=True)
    inputs={name:HERE/name for name in ('nano_fabric_stop.c','nano_fabric_stop.h','nano_probe_stop.c','nano_probe_stop.h',
        'nano_ddr_read.c','nano_ddr_read.h','generated/fes_embedded_control.h','generated/fes_de25_ddr_probe.h',
        'tests/nano_fabric_stop_test.cpp','tests/nano_ddr_read_test.inc')}
    board=ROOT/'sources/misteross/boards/de25nano/hps'
    inputs.update({name:board/name for name in ('fes_de25_ddr_probe.v','generated/fes_de25_ddr_probe.vh',
                                              'generated/fes_de25_ddr_probe.hpp','probe_tb.cpp')})
    inputs.update({'fes_de25_embedded.sv':ROOT/'sources/misteross/prototypes/de25_board_control/fes_de25_embedded.sv',
        'fes_embedded_control.sv':ROOT/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
        'fes_embedded_control.vh':ROOT/'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh'})
    hashes={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs.values()}
    for name,p in inputs.items():
        target=captured/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(p.read_bytes())
    commands=[]
    def run(label,args):
        argv=[str(x) for x in args];commands.append(argv)
        result=subprocess.run(argv,cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
        (output/(label+'.log')).write_bytes(result.stdout)
        if result.returncode:raise RuntimeError(label+' failed: '+str(output/(label+'.log')))
        return result.stdout
    objects=[]
    for name in ('nano_fabric_stop','nano_probe_stop','nano_ddr_read'):
        obj=output/(name+'.o');objects.append(obj)
        run(name,['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                  '-I'+str(captured),'-c',captured/(name+'.c'),'-o',obj])
    probe=output/'probe'
    run('probe',['verilator','--cc','--build','--top-module','fes_de25_ddr_probe','-Wall',
                 '--Mdir',probe,'-I'+str(captured),captured/'fes_de25_ddr_probe.v'])
    model=output/'reader'
    # This read-only DDR wrapper intentionally does not use write responses or
    # its diagnostic reader data in DDR mode, as in the existing DDR bench.
    run('reader',['verilator','--cc','--exe','--build','--top-module','fes_de25_embedded','-Wall','-Wno-UNUSEDSIGNAL',
        '--Mdir',model,'-I'+str(captured),'-GBUILD_ID=128\'h00112233445566778899aabbccddeeff',
        '-GMEMORY_BACKEND=1','-GDDR_BASE=32\'h95000000','-GDDR_BYTES=8192',
        '-CFLAGS','-std=c++17 -O2 -fsanitize=undefined -fno-sanitize-recover=all -I'+str(captured)+' -I'+str(probe),
        '-LDFLAGS',' '.join(str(x) for x in objects)+' '+str(probe/'Vfes_de25_ddr_probe__ALL.a')+' -fsanitize=undefined',
        captured/'fes_de25_embedded.sv',captured/'fes_embedded_control.sv',captured/'tests/nano_fabric_stop_test.cpp'])
    observed=json.loads(run('cases',[model/'Vfes_de25_embedded']))
    if (observed['status']!='passed' or observed['cases']<50 or observed['read_failure_points']<88 or
        observed['ddr_read_cases']<50 or observed['ddr_read_failure_points']<56):
        raise RuntimeError('incomplete complete-master stop checks')
    if hashes!={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs.values()}:
        raise RuntimeError('fabric stop sources changed during checks')
    result={'status':'passed','classification':'actual C and both actual reader RTL models; injected ownership and memory; no hardware acceptance',
            'commands':commands,'source_sha256':hashes,'observed':observed}
    (output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
    print('PASS:',output)

if __name__=='__main__':main()
