#!/usr/bin/env python3
"""Exercise the secure probe-stop helper with the actual read-only Avalon RTL."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import uuid

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
PROBE=ROOT/'sources/misteross/boards/de25nano/hps'

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--probe-source',type=Path,default=PROBE)
    p.add_argument('--output',type=Path,default=ROOT/'out'/('nano-probe-stop-'+uuid.uuid4().hex))
    args=p.parse_args()
    args.probe_source=args.probe_source.absolute()
    output=args.output.absolute()
    if output.exists() or '..' in output.parts or not output.is_relative_to(ROOT/'out') or any(x.is_symlink() for x in (output,*output.parents)):
        p.error('output must be a fresh private directory below out/')
    output.mkdir(parents=True)
    captured=output/'source';(captured/'generated').mkdir(parents=True)
    inputs={name:HERE/name for name in ('nano_probe_stop.c','nano_probe_stop.h','tests/nano_probe_stop_test.cpp','generated/fes_de25_ddr_probe.h')}
    inputs.update({name:args.probe_source/name for name in ('fes_de25_ddr_probe.v','generated/fes_de25_ddr_probe.vh')})
    inputs['probe_tb.cpp']=PROBE/'probe_tb.cpp'
    inputs['generated/fes_de25_ddr_probe.hpp']=PROBE/'generated/fes_de25_ddr_probe.hpp'
    hashes={str(path.relative_to(ROOT)):sha(path) for path in inputs.values()}
    for name,path in inputs.items():
        target=captured/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(path.read_bytes())
    commands=[]
    def run(label,argv):
        command=[str(x) for x in argv];commands.append(command)
        r=subprocess.run(command,cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
        (output/(label+'.log')).write_bytes(r.stdout)
        if r.returncode:raise RuntimeError(label+' failed: '+str(output/(label+'.log')))
        return r.stdout.decode()
    obj=output/'stop.o'
    run('compile-helper',['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(captured),'-c',captured/'nano_probe_stop.c','-o',obj])
    run('build-model',['verilator','--cc','--exe','--build','--top-module','fes_de25_ddr_probe','-Wall','--Mdir',output/'obj','-I'+str(captured),
        '-CFLAGS','-std=c++17 -O2 -fsanitize=undefined -fno-sanitize-recover=all -I'+str(captured),
        '-LDFLAGS',str(obj)+' -fsanitize=undefined',captured/'fes_de25_ddr_probe.v',captured/'tests/nano_probe_stop_test.cpp'])
    observed=json.loads(run('cases',[output/'obj/Vfes_de25_ddr_probe']))
    if observed['status']!='passed' or observed['cases']<20 or observed['read_failure_points']<18:raise RuntimeError('incomplete stop checks')
    if hashes!={str(path.relative_to(ROOT)):sha(path) for path in inputs.values()}:raise RuntimeError('source changed during probe stop checks')
    report={'status':'passed','classification':'actual C helper and Avalon RTL, injected ownership/memory responses; no physical drain or complete master containment',
            'source_sha256':hashes,'commands':commands,'observed':observed}
    (output/'result.json').write_text(json.dumps(report,indent=2)+'\n')
    print('PASS:',output)

if __name__=='__main__':main()
