#!/usr/bin/env python3
"""Compare the ROM variant's actual capture RTL with its frozen predecessor."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import os
import signal
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
REFERENCE_REVISION = '79d616e85e19d65f0d645b67a85ffe7e142dcdfc'
REFERENCE_PATH = 'sources/misteross/boards/de25nano/zx81-hps/video.v'


def control_module(source, name, previous):
    """Extract the actual writer control; abstract only the returned bank token.

    The proof covers observable writer state, write enables/addresses and publication
    under arbitrary blank, Hold, CE, reset and acknowledgement inputs. Pixel
    timing/RAM and asynchronous clock behavior are checked by the full RTL pair.
    """
    start = source.index('    reg writer =')
    middle = source.index('    reg [6:0] lane;', start)
    reset = 'sys_release[1]' if previous else 'reset_n'
    block = source.index('    always @(posedge clk_sys or negedge '+reset+') begin', middle)
    end = source.index('    reg [10:0] horizontal', block)
    fragment = source[start:middle] + source[block:end]
    # Fail rather than extracting a different state machine after a source edit.
    if fragment.count('always @') != 1 or 'request <= !request;' not in fragment:
        raise ValueError('capture writer control shape changed')
    return f'''module {name}(input clk_sys, reset_n, held, ce_6m5, zx_pixel, hblank, vblank, ack,
        output [50:0] observation);
    localparam WIDTH=328, HEIGHT=270, STRIDE=41, WORDS=11070;
    wire [1:0] sys_release = {{2{{reset_n}}}};
    wire sys_reset = !reset_n;
    wire [1:0] acknowledge_sync = {{2{{ack}}}};
    reg request = 0;
{fragment}
    assign observation = {{request, (capturing ? writer : 1'b0), capturing, line_open, bad, old_hblank,
        x, rows, published_rows, sample, (sample ? write_address : 15'd0), {'armed' if previous else 'old_vblank'}}};
endmodule
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--yosys', type=Path, required=True)
    args = parser.parse_args()
    output = args.output.absolute()
    if (output.exists() or not output.is_relative_to(ROOT/'out') or '..' in output.parts or
            any(p.is_symlink() for p in (output, *output.parents))):
        parser.error('fresh private output beneath out/ required')
    if shutil.disk_usage(ROOT).free < 30*(1 << 30):
        parser.error('30 GiB free required')
    output.mkdir(parents=True)
    original = subprocess.check_output(['git', '-C', str(ROOT), 'show', REFERENCE_REVISION+':'+REFERENCE_PATH])
    candidate = (HERE/'video.v').read_bytes()
    files = [Path(__file__), HERE/'video.v', HERE/'video_pair.cpp']
    hashes = {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
    reference = original.decode().replace('module fes_de25_zx81_video (', 'module reference_video (', 1)
    (output/'reference.v').write_text(reference)
    formal = control_module(original.decode(), 'reference_control', True) + control_module(candidate.decode(), 'candidate_control', False)
    formal += '''module capture_equivalence(input clk_sys, reset_n, held, ce_6m5, zx_pixel, hblank, vblank, ack);
wire [50:0] old_state, new_state;
reference_control reference_inst(.*,.observation(old_state));
candidate_control candidate_inst(.*,.observation(new_state));
always @* assert(old_state == new_state);
endmodule
'''
    (output/'control.sv').write_text(formal)
    ports = '.clk_sys(clk_sys),.pixel_clk(pixel_clk),.held(held),.ce_6m5(ce_6m5),.zx_pixel(zx_pixel),.hblank(hblank),.vblank(vblank)'
    signals = '{red,green,blue,de,hsync,vsync,frame_tick}'
    internals = '{request,acknowledge,capturing,line_open,bad,old_hblank,x,rows,published_rows,front,front_valid,front_rows,sample}'
    wrapper = 'module video_pair(input clk_sys,pixel_clk,reset_n,held,ce_6m5,zx_pixel,hblank,vblank,output mismatch,output [3:0] events);\n'
    # Reset qualification now belongs to the caller. Compare complete video
    # behavior at the same qualified releases as the old internal chains.
    # This does not claim the enclosing computer retains the old extra two
    # system cycles: its Platform Designer reset is already qualified.
    wrapper += '''reg [1:0] sys_qualified=0, pixel_qualified=0;
always @(posedge clk_sys or negedge reset_n)
    if (!reset_n) sys_qualified<=0; else sys_qualified<={sys_qualified[0],1'b1};
always @(posedge pixel_clk or negedge reset_n)
    if (!reset_n) pixel_qualified<=0; else pixel_qualified<={pixel_qualified[0],1'b1};
'''
    for prefix, module in [('a', 'reference_video'), ('b', 'fes_de25_zx81_video')]:
        wrapper += f'wire [7:0] {prefix}_red,{prefix}_green,{prefix}_blue; wire {prefix}_de,{prefix}_hsync,{prefix}_vsync,{prefix}_frame_tick;\n'
        outputs = ','.join(f'.{s}({prefix}_{s})' for s in ('red','green','blue','de','hsync','vsync','frame_tick'))
        resets = '.reset_n(reset_n)' if prefix=='a' else '.reset_n(sys_qualified[1]),.pixel_reset_n(pixel_qualified[1])'
        wrapper += f'{module} {prefix}({ports},{resets},{outputs});\n'
    def select(expression, prefix):
        return re.sub(r'\b[a-z_]+\b', lambda m: prefix+'.'+m[0], expression)
    # RAM write data matters only on an actual whole-byte write. The two
    # uninitialised lane registers are deliberately not compared while idle.
    wrapper += 'assign mismatch = ('+select(signals,'a')+' != '+select(signals,'b')+') || ('+select(internals,'a')+' != '+select(internals,'b')+') || (a.capturing && a.writer != b.writer) || (a.sample && a.write_address != b.write_address) || (a.armed != b.old_vblank) || (a.sample && a.x[2:0]==7 && ({zx_pixel,a.lane} != {zx_pixel,b.lane}));\n'
    wrapper += "assign events = {a.acknowledge,a.request,a.sample && a.x[2:0]==7,a.frame_tick};\nendmodule\n"
    (output/'pair.sv').write_text(wrapper)
    commands = []

    def run(name, argv, timeout=90, expected=0):
        started = time.monotonic()
        with (output/(name+'.log')).open('xb') as stream:
            child = subprocess.Popen(list(map(str, argv)), cwd=output, stdout=stream,
                                     stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = child.wait(timeout=timeout)
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGTERM)
                    try:
                        child.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(child.pid, signal.SIGKILL)
                        child.wait()
            if code != expected:
                raise subprocess.CalledProcessError(code, argv)
        commands.append(dict(name=name, argv=list(map(str, argv)), elapsed=time.monotonic()-started, returncode=code, expected_returncode=expected,
                             log_sha256=hashlib.sha256((output/(name+'.log')).read_bytes()).hexdigest()))

    proof = ('prep -top capture_equivalence -flatten; async2sync; opt; '
        'sat -seq 8 -tempinduct -prove-asserts -verify -enable_undef -set-def-inputs -set-init-def -maxsteps 32 '
        '-dump_vcd counterexample.vcd -show-inputs -show reference_inst.observation -show candidate_inst.observation')
    run('formal', [args.yosys.resolve(), '-Q', '-T', '-p', 'read_verilog -formal -sv control.sv; '+proof])
    if 'Induction step proven: SUCCESS!' not in (output/'formal.log').read_text():
        raise ValueError('unbounded control equivalence was not proved')
    mutation = formal.replace('capturing <= !pending;', 'capturing <= pending;', 1)
    if mutation == formal:
        raise ValueError('negative control did not modify the candidate')
    (output/'mutated.sv').write_text(mutation)
    run('formal-negative', [args.yosys.resolve(), '-Q', '-T', '-p',
        'read_verilog -formal -sv mutated.sv; '+proof], expected=1)
    if 'proof did fail' not in (output/'formal-negative.log').read_text():
        raise ValueError('negative control did not produce a proof counterexample')
    run('build', ['verilator', '--cc', '--exe', '--build', '-j', '2', '-Wall',
        '-Wno-DECLFILENAME', '-Wno-WIDTH', '-Wno-UNUSEDSIGNAL', '-Wno-SYNCASYNCNET', '--top-module', 'video_pair',
        '--Mdir', output/'model', output/'pair.sv', output/'reference.v', HERE/'video.v', HERE/'video_pair.cpp'])
    run('execute', [output/'model/Vvideo_pair'])
    assert all(hashlib.sha256((ROOT/p).read_bytes()).hexdigest() == digest for p, digest in hashes.items())
    result = dict(status='passed', classification='unbounded actual writer-control equivalence with arbitrary inputs; full actual dual-clock RTL pair simulation; no physical timing or metastability proof',
        reference_revision=REFERENCE_REVISION, reference_path=REFERENCE_PATH, reference_sha256=hashlib.sha256(original).hexdigest(),
        source_sha256=hashes, yosys_sha256=hashlib.sha256(args.yosys.resolve().read_bytes()).hexdigest(),
        simulator_sha256=hashlib.sha256((output/'model/Vvideo_pair').read_bytes()).hexdigest(), commands=commands,
        hardware_actions=False, physical_timing_signoff=False)
    (output/'result.json').write_text(json.dumps(result, indent=2)+'\n')
    print('PASS capture control induction and full dual-clock RTL pair:', output)


if __name__ == '__main__':
    main()
