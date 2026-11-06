#!/usr/bin/env python3
"""Model the actual generated memory sequencer and early-request device reset."""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import signal
import subprocess
import time
import reset_domains

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[3]


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--project',type=Path,required=True)
    p.add_argument('--output',type=Path,required=True)
    args=p.parse_args();project=args.project.resolve();out=args.output.absolute()
    if out.exists() or not out.is_relative_to(ROOT/'out') or any(x.is_symlink() for x in (out,*out.parents)):
        p.error('fresh private output under out/ required')
    if shutil.disk_usage(ROOT).free<30*(1<<30):p.error('30 GiB free required')
    out.mkdir(parents=True)
    controller=project/'qsys_top/altera_reset_controller_1925/synth/altera_reset_controller.v'
    synchronizer=controller.with_name('altera_reset_synchronizer.v')
    rtl=project/'qsys_top/synth/qsys_top.v'
    parameters,_=reset_domains.instance(rtl.read_text(),'altera_reset_controller','rst_controller')
    if parameters!=reset_domains.REQUEST_PARAMETERS:raise ValueError('generated memory reset request parameters changed')
    top=ROOT/'sources/misteross/boards/de25nano/hps/fes_de25_top_reset.v'
    files=[Path(__file__),HERE/'reset_domains.py',HERE/'reset_bridge_probe.cpp',HERE/'fes_de25_reset_bridge.sv',top,controller,synchronizer,rtl]
    sha=lambda f:hashlib.sha256(f.read_bytes()).hexdigest()
    hashes={str(f):sha(f) for f in files}
    wrapper='''module reset_domains_model #(parameter BROKEN_SHARE=0)(input clk,ninit_done,pll_locked,
    output memory_n,request,device_n);
wire qualified_n,memory_reset,owned_n;
fes_de25_top_reset top_reset(.clk(clk),.ninit_done(ninit_done),.pll_locked(pll_locked),.reset_n(qualified_n));
'''
    values=','.join('.'+k+'('+v+')' for k,v in parameters.items())
    ports=dict(clk='clk',reset_in0='!qualified_n',reset_out='memory_reset',reset_req='request',
        **{'reset_in'+str(i):"1'b0" for i in range(1,16)},**{'reset_req_in'+str(i):"1'b0" for i in range(16)})
    wrapper+='altera_reset_controller #('+values+') memory_owner('+','.join('.'+k+'('+v+')' for k,v in ports.items())+');\n'
    wrapper+='''assign memory_n=!memory_reset;
fes_de25_reset_bridge device_owner(.clk(clk),.in_reset_n(memory_n),.in_reset_req(request),.out_reset_n(owned_n));
assign device_n=BROKEN_SHARE ? memory_n : owned_n;
endmodule
'''
    (out/'model.sv').write_text(wrapper);commands=[]
    def run(name,argv,negative=False):
        start=time.monotonic()
        with (out/(name+'.log')).open('xb') as log:
            child=subprocess.Popen(list(map(str,argv)),stdout=log,stderr=subprocess.STDOUT,cwd=out,start_new_session=True)
            try:code=child.wait(timeout=90)
            finally:
                if child.poll() is None:os.killpg(child.pid,signal.SIGKILL);child.wait()
        text=(out/(name+'.log')).read_text()
        if negative:
            cycle=re.search(r'at cycle (\d+)',text)
            if (code!=1 or 'stopped-clock reset request did not contain device owner' not in text
                    or cycle is None or int(cycle[1])<=32):
                raise ValueError('broken reset share did not fail after a live epoch')
        elif code:raise RuntimeError(name+' failed: '+text[-1000:])
        commands.append(dict(name=name,argv=list(map(str,argv)),returncode=code,expected_failure=negative,elapsed=time.monotonic()-start,log_sha256=sha(out/(name+'.log'))))
    for broken in (0,1):
        model=out/('broken' if broken else 'correct')
        # Existing vendor RTL has unnamed generate blocks and nonblocking
        # initializers. These simulator style waivers add no timing exception.
        run('build-'+str(broken),['verilator','--cc','--exe','--build','-j','2','-Wall',
            '-Wno-DECLFILENAME','-Wno-WIDTH','-Wno-UNUSEDSIGNAL','-Wno-UNUSEDPARAM','-Wno-SYNCASYNCNET',
            '-Wno-EOFNEWLINE','-Wno-TIMESCALEMOD','-Wno-GENUNNAMED','-Wno-INITIALDLY',
            '--top-module','reset_domains_model','-GBROKEN_SHARE='+str(broken),'--Mdir',model,
            out/'model.sv',top,controller,synchronizer,HERE/'fes_de25_reset_bridge.sv',HERE/'reset_bridge_probe.cpp'])
        run('execute-'+str(broken),[model/'Vreset_domains_model'],negative=bool(broken))
    assert all(sha(Path(name))==want for name,want in hashes.items())
    result=dict(status='passed',scope='actual generated vendor reset-sequencer and device bridge RTL model; no electrical/metastability or hardware qualification',
        source_sha256=hashes,request_parameters=parameters,commands=commands,short_pulses_and_stopped_clocks=128,
        naive_memory_reset_share_rejected=True,hardware_actions=False)
    (out/'result.json').write_text(json.dumps(result,indent=2)+'\n');print('PASS memory/device reset ownership:',out)


if __name__=='__main__':main()
