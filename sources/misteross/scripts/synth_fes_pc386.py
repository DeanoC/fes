#!/usr/bin/env python3
"""Machine-only Cyclone V synthesis of pc386 (docs/pc386-machine-design.md, phase M2).

Diagnostic synthesis and packing for the design's "Budgets" table: no route,
no timing, no package and no seal. Each block is synthesized on its own
inside a stimulus/signature envelope (a 64-bit LFSR drives every input, every
output folds into one XOR register on LED0, two board clocks; the envelope adds
the LFSR and the fold tree) and packed by nextpnr-mistral without placement,
so the figures are the device cells and ALMs the block needs on its own:

  machine    pc386_machine: everything but the CPU (AT core, VGA, presenter,
             port 2 arbiter, plane bridge, display plane)
  at-core    pc386_at_core: PICs, PIT, DMA, 61h/92h/80h, 8042, RTC, ATA, I/O decoder
  vga        pc386_vga: registers, FIFO, planar logic, coalescer, scanout, DAC, capture
  presenter  pc386_presenter
  port2      pc386_port2 and pc386_plane_bridge
  plane      fes_display_plane (shared, unchanged)

Synthesis permits M10K and DSP cells and forbids LUT RAM, as the CPU's
DSP-permitting recipe does. Tools come from the selected lock's
authenticated slot; the default is the i386 lock, whose Yosys the CPU and
machine are read with. Packing runs on the CPU and routes nothing.

Writes build/synth/fes-pc386/<block>/ and build/synth/fes-pc386/summary.json.

Example:
  python3 scripts/synth_fes_pc386.py --cache-root /abs/out/cache/misteross-toolchains
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_build_common as board
from scripts import sim_fes_pc386 as sim

ROOT = Path(__file__).resolve().parents[1]
DEVICE = '5CSEBA6U23I7'
LOCK = Path('cores/fes-common/rtl/i386/toolchain.lock')
SYNTHESIS = 'synth_intel_alm -family cyclonev -nolutram'
COMMON_PLANE = tuple(path for path in sim.MACHINE_RTL if 'fes-menu' in str(path) or 'fes_display' in str(path))
PC386 = Path('cores/fes-pc386/rtl')
BLOCKS = {
    'machine': ('pc386_machine', sim.MACHINE_RTL),
    'at-core': ('pc386_at_core', sim.AT_RTL),
    'vga': ('pc386_vga', (ROOT / PC386 / 'pc386_rate.sv', ROOT / PC386 / 'pc386_ram.sv',
                          ROOT / PC386 / 'pc386_vga.sv', ROOT / PC386 / 'pc386_vga_scanout.sv')),
    'presenter': ('pc386_presenter', (ROOT / PC386 / 'pc386_ram.sv', ROOT / PC386 / 'pc386_presenter.sv')),
    'port2': ('pc386_port2_pair', (ROOT / PC386 / 'pc386_ram.sv', ROOT / PC386 / 'pc386_port2.sv',
                                   ROOT / PC386 / 'pc386_plane_bridge.sv')),
    'plane': ('fes_display_plane', COMMON_PLANE),
}
# The arbiter and the bridge share a top only so that each is measured with
# the other's ports; it adds no logic.
PAIR = '''module pc386_port2_pair (
    input wire clk, reset, pixel_clk, pixel_reset, ddr_hold,
    output wire ddr_reset, ddr_drained,
    input wire [27:0] plane_address, input wire [7:0] plane_burstcount, input wire plane_read,
    output wire plane_waitrequest, output wire [127:0] plane_readdata, output wire plane_readdatavalid,
    input wire [28:0] present_address, input wire [7:0] present_burstcount, input wire present_read,
    output wire present_waitrequest, present_readdatavalid,
    input wire [28:0] capture_address, input wire [7:0] capture_burstcount, input wire capture_write,
    input wire [63:0] capture_writedata, output wire capture_waitrequest,
    output wire [28:0] address, output wire [7:0] burstcount, output wire read, write,
    output wire [63:0] writedata, output wire [7:0] byteenable,
    input wire waitrequest, input wire [63:0] readdata, input wire readdatavalid
);
    wire [28:0] bridge_address;
    wire [7:0] bridge_burstcount;
    wire bridge_read, bridge_waitrequest, bridge_readdatavalid;
    pc386_plane_bridge bridge (
        .clk(clk), .reset(reset), .pixel_clk(pixel_clk), .pixel_reset(pixel_reset), .ddr_hold(ddr_hold),
        .ddr_reset(ddr_reset), .ddr_drained(ddr_drained), .plane_address(plane_address),
        .plane_burstcount(plane_burstcount), .plane_read(plane_read), .plane_waitrequest(plane_waitrequest),
        .plane_readdata(plane_readdata), .plane_readdatavalid(plane_readdatavalid),
        .port_address(bridge_address), .port_burstcount(bridge_burstcount), .port_read(bridge_read),
        .port_waitrequest(bridge_waitrequest), .port_readdata(readdata), .port_readdatavalid(bridge_readdatavalid));
    pc386_port2 port2 (
        .clk(clk), .reset(reset), .plane_address(bridge_address), .plane_burstcount(bridge_burstcount),
        .plane_read(bridge_read), .plane_waitrequest(bridge_waitrequest), .plane_readdatavalid(bridge_readdatavalid),
        .present_address(present_address), .present_burstcount(present_burstcount), .present_read(present_read),
        .present_waitrequest(present_waitrequest), .present_readdatavalid(present_readdatavalid),
        .capture_address(capture_address), .capture_burstcount(capture_burstcount), .capture_write(capture_write),
        .capture_writedata(capture_writedata), .capture_waitrequest(capture_waitrequest),
        .address(address), .burstcount(burstcount), .read(read), .write(write), .writedata(writedata),
        .byteenable(byteenable), .waitrequest(waitrequest), .readdatavalid(readdatavalid));
endmodule
'''
UTILISATION = re.compile(r'Info:\s+(\w+):\s+(\d+)/\s*(\d+)')
CLOCKS = {'clk': 'clk', 'clk_sys': 'clk', 'pixel_clk': 'pixel_clk'}
RESETS = ('reset', 'pixel_reset')
# Terasic DE10-Nano: FPGA_CLK1_50, FPGA_CLK2_50 and FPGA LED[0].
QSF = '''set_location_assignment PIN_V11 -to FPGA_CLK1_50
set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to FPGA_CLK1_50
set_location_assignment PIN_Y13 -to FPGA_CLK2_50
set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to FPGA_CLK2_50
set_location_assignment PIN_W15 -to LED0
set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to LED0
'''


def envelope(top: str, ports: dict) -> str:
    """An LFSR on every input, every output in one XOR register."""
    lines = ['module pc386_synth_envelope (input wire FPGA_CLK1_50, input wire FPGA_CLK2_50, output reg LED0);',
             '    wire clk = FPGA_CLK1_50, pixel_clk = FPGA_CLK2_50;',
             "    reg [63:0] lfsr = 64'h1;",
             '    always @(posedge clk) lfsr <= {lfsr[62:0], lfsr[63] ^ lfsr[62] ^ lfsr[60] ^ lfsr[59]};',
             "    reg [7:0] boot = 8'd0;",
             '    always @(posedge clk) if (!boot[7]) boot <= boot + 8\'d1;']
    connections, outputs, offset = [], [], 0
    for name, port in ports.items():
        width = len(port['bits'])
        if port['direction'] == 'input':
            if name in CLOCKS:
                value = CLOCKS[name]
            elif name in RESETS:
                value = '!boot[7]'
            else:
                value = '{' + ', '.join(f'lfsr[{(offset + k * 7) % 64}]' for k in reversed(range(width))) + '}'
                offset += 13
            connections.append(f'.{name}({value})')
        else:
            lines.append(f'    wire [{width - 1}:0] out_{name};')
            connections.append(f'.{name}(out_{name})')
            outputs.append(f'out_{name}')
    lines.append(f'    {top} dut (' + ', '.join(connections) + ');')
    lines.append('    always @(posedge clk) LED0 <= ^{' + ', '.join(outputs) + '};')
    lines.append('endmodule')
    return '\n'.join(lines) + '\n'


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--cache-root', type=Path, required=True, help='shared misteross-toolchains cache root')
    parser.add_argument('--lock', type=Path, default=LOCK, help=f'toolchain lock (default: {LOCK})')
    parser.add_argument('--block', action='append', choices=sorted(BLOCKS), help='blocks to synthesize (default: all)')
    args = parser.parse_args()
    if not args.cache_root.is_absolute():
        parser.error('--cache-root must be absolute')
    tools = board._authenticate_tools(ROOT, lock_path=ROOT / args.lock, cache_root=args.cache_root)
    yosys, nextpnr = tools['yosys'], tools['nextpnr-mistral']
    output = ROOT / 'build/synth/fes-pc386'
    output.mkdir(parents=True, exist_ok=True)
    (output / 'pc386_port2_pair.v').write_text(PAIR)
    (output / 'envelope.qsf').write_text(QSF)
    summary = {'format': 1, 'classification': 'diagnostic machine-only synthesis and packing; no route or timing',
               'hardware_acceptance': False, 'device': DEVICE, 'synthesis': SYNTHESIS,
               'lock': args.lock.as_posix(), 'lock_sha256': digest(ROOT / args.lock),
               'tools': {name: tool.identity for name, tool in sorted(tools.items())},
               'created_utc': datetime.now(timezone.utc).isoformat(), 'blocks': {}}
    for block in args.block or list(BLOCKS):
        top, rtl = BLOCKS[block]
        work = output / block
        work.mkdir(exist_ok=True)
        files = [*rtl, *( [output / 'pc386_port2_pair.v'] if block == 'port2' else [])]
        includes = ' '.join(f'-I{path}' for path in (sim.COMMON / 'generated', sim.COMMON / 'vendor/kf8259',
                                                      sim.COMMON / 'vendor/kf8253'))
        read = f'read_verilog -sv {includes} ' + ' '.join(str(path) for path in files)
        with (work / 'ports.log').open('w') as log:
            result = board._run_command([str(yosys.path), '-p', f'{read}; hierarchy -top {top}; proc; write_json ports.json'],
                                        cwd=work, stdout=log, stderr=log)
        if result.returncode:
            raise SystemExit(f'reading {block} failed; see {work / "ports.log"}')
        ports = json.loads((work / 'ports.json').read_text())['modules'][top]['ports']
        (work / 'envelope.v').write_text(envelope(top, ports))
        wrapper = 'pc386_synth_envelope'
        program = (f'{read} {work / "envelope.v"}; hierarchy -top {wrapper}; {SYNTHESIS} -top {wrapper}; '
                   'check -assert; stat; write_json synth.json')
        (work / 'synth.ys').write_text(program + '\n')
        with (work / 'yosys.log').open('w') as log:
            result = board._run_command([str(yosys.path), '-s', 'synth.ys'], cwd=work, stdout=log, stderr=log)
        if result.returncode:
            raise SystemExit(f'synthesis of {block} failed; see {work / "yosys.log"}')
        netlist = json.loads((work / 'synth.json').read_text())
        cells = dict(sorted(Counter(cell['type'] for cell in netlist['modules'][wrapper]['cells'].values()).items()))
        with (work / 'pack.log').open('w') as log:
            result = board._run_command([str(nextpnr.path), '--json', str(work / 'synth.json'), '--device', DEVICE,
                                         '--top', wrapper, '--qsf', str(output / 'envelope.qsf'), '--pack-only'],
                                        cwd=work, stdout=log, stderr=log)
        if result.returncode:
            raise SystemExit(f'packing {block} failed; see {work / "pack.log"}')
        utilisation = {name: {'used': int(used), 'available': int(available)}
                       for name, used, available in UTILISATION.findall((work / 'pack.log').read_text())}
        summary['blocks'][block] = {
            'top': top, 'sources': {str(Path(path).relative_to(ROOT)): digest(Path(path)) for path in files
                                    if Path(path).is_relative_to(ROOT)},
            'cells': cells, 'utilisation': utilisation, 'synth_json_sha256': digest(work / 'synth.json')}
        (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
        print(block, json.dumps({'cells': cells, 'utilisation': utilisation}), flush=True)
    return 0


if __name__ == '__main__':
    sys.exit(main())
