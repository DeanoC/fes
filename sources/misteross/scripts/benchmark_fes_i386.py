#!/usr/bin/env python3
"""Route the i386 timing spikes on the DE10-Nano; never generate or deploy RBFs.

A CPU-only routed harness like the RISC-V QoR one (cores/fes-riscv/qor.md):
each spike is a block of fes_i386_cpu inside a stimulus/signature envelope
(cores/fes-common/sim/i386/spike) with one clock, KEY0 and LED0 on pads.

  frontend  F1; F2 with a 4-way L1I model (M10K tags and data, compares
            against a registered ITLB page, way select); L1, the 16 length
            decoders; L2, each byte's lengths and next starts; the fetch
            buffer; and Q
  dag       D's address operands (addressing decode, register file,
            forwarding by tag, index scaling, segment select) and AG (one
            late operand, the 3:2 rows, the adders, the 16-bit-wrap select)
  m2        the M2 stage: M10K tag and data reads, four tag compares against
            a registered page number, way select
  cpu       fes_i386_cpu as built so far, F1 to WB with its L1I, L1D and
            bus interface, the machine's ports answered by the stimulus
  rom       a registered case-table ROM synthesized without proc -norom, so
            it maps to an M10K: checks that it places and routes

Each route reports the clock, its critical path, and a figure for every
pipeline stage: the worst setup slack among the registers the stage writes,
from nextpnr's per-endpoint timing, plus the envelope's worst endpoints so a
mislabelled register shows. Each synthesis reports every stage's deepest
logic path in the netlist before placement: LUT levels, carry cells and the
named nets along it.

Tools come from the selected toolchain lock's authenticated slot (default: the
i386 lock, cores/fes-common/rtl/i386/toolchain.lock, whose Yosys reads the RTL
as written), built for the GPU router --gpu-router names: HIP (Powerboat's) or
CUDA (a CUDA host's; its route results match HIP's). Synthesis permits M10K and DSP cells and forbids
LUT RAM, as the design's rules do. Every route uses the GPU router through
gpu-run, with CPU fallback rejected. Timing is allowed to fail so a miss is
measured; nothing here is a signoff.

Example:
  python3 scripts/benchmark_fes_i386.py --cache-root /abs/out/cache/misteross-toolchains \\
      --spike dag --seed 1 --seed 2

Results describe the spike envelope. They are diagnostic place-and-route
measurements, not packages or board acceptance.
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_build_common as board

ROOT = Path(__file__).resolve().parents[1]
RTL = Path('cores/fes-common/rtl/i386')
SPIKE = Path('cores/fes-common/sim/i386/spike')
DEVICE = '5CSEBA6U23I7'
# The i386 RTL's own lock: the shared toolchain.lock's Yosys misreads it.
LOCK = RTL / 'toolchain.lock'
FRONTEND_RTL = ('fes_i386_pkg.sv', 'fes_i386_oplen.sv', 'fes_i386_lendec.sv', 'fes_i386_lstage.sv',
                'fes_i386_fetchbuf.sv', 'fes_i386_qalign.sv', 'fes_i386_frontend.sv')
DAG_RTL = ('fes_i386_pkg.sv', 'fes_i386_amode.sv', 'fes_i386_dfwd.sv', 'fes_i386_agu.sv')
CPU_RTL = (*FRONTEND_RTL, 'fes_i386_amode.sv', 'fes_i386_dfwd.sv', 'fes_i386_agu.sv', 'fes_i386_ucode_pkg.sv', 'fes_i386_ucode_rom.sv', 'fes_i386_decode.sv', 'fes_i386_seq.sv',
           'fes_i386_dops.sv', 'fes_i386_shift.sv', 'fes_i386_shflags.sv', 'fes_i386_exctl.sv', 'fes_i386_muldiv.sv',
           'fes_i386_ex.sv', 'fes_i386_l1i.sv', 'fes_i386_l1d.sv', 'fes_i386_biu.sv', 'fes_i386_cpu.sv')
SPIKES = {
    'frontend': ('spike_frontend', FRONTEND_RTL),
    'dag': ('spike_dag', DAG_RTL),
    'm2': ('spike_m2', ()),
    'cpu': ('spike_cpu', CPU_RTL),
    'rom': ('spike_rom', ()),
}
# Which stage writes each register, by the RTL signals around it. After
# flattening a register's cell takes the name of any one port it drives, and
# Yosys may drop the register's own name (the F2 register can come out named
# only as L1 decoder inputs). So register_labels labels a register from every
# public name of its output net in the synthesized netlist and its cell name;
# if none matches, from the names of its data input. The first stage whose
# pattern matches any name wins; a path belongs to the stage that writes its
# end. The envelope's own registers are matched first, since some take a
# stage's port name or have a stage's net at their input; the envelope's
# redirect_linear register can also take Q's port name, so Q excludes it.
STAGES = {
    'frontend': (
        ('envelope', r'(.*\.)?(signature|noise|LED0|redirect|code32|d_ready)$|redirect_linear$|stimulus\.'),
        ('F1', r'(.*\.)?(f2_block|fetch_block|active|itlb_page)$|l1i_'),
        # A decoder's opcode table can map to an M10K that holds F2's bytes.
        ('F2', r'(.*\.)?(l1_valid|l1_block|l1_bytes|mem_data)$|.*\.oplen\.(one|two)_byte'),
        # Only the L1/L2 register feeds L2's sums, so any name inside them is it.
        ('L1', r'(.*\.)?(l2_\w+|half|fill_bytes|fill_data)$|dut\.fetch\.sums\.'),
        ('L2', r'(.*\.)?(bytes|half_block|dec|avail)$'),
        ('Q', r'(.*\.)?(half_valid|tail_ok)$|dut\.align\.(?!redirect_linear$)'),
    ),
    'dag': (
        # D writes AG's operands, the producer entering AG, and the forwarding
        # table for the next uop.
        ('D', r'(.*\.)?ag_|agu\.(row|last|disp|late|late_hi|seg_term|seg_hi|seg_hi_plus1|addr32)$'
              r'|dfwd\.(ready|lt|wt|src|\w+_next)$|(.*\.)?prod_\w+$'),
        ('AG', r'(.*\.)?(m1_valid|m1_linear|m1_offset|m1_result|late_m1)$'),
    ),
    'm2': (('M2', r'(.*\.)?load_'),),
    # The whole CPU: the front end's labels with any hierarchy prefix, then
    # each pipeline register by its stage prefix. The architectural registers
    # (WB) and the stage records come before D, whose forwarding tables take
    # the register file and the records as ports.
    'cpu': (
        ('envelope', r'(.*\.)?(signature|observed|noise|LED0|io_ack|io_rdata|intr|nmi|inta_ack|inta_vector'
                     r'|init_\w+|a20m|ddr_waitrequest|ddr_readdata|ddr_readdatavalid|mmio_ready|mmio_rvalid'
                     r'|mmio_rdata|dma_valid|dma_write|dma_address|dma_wdata|flush_request)$|stimulus\.'),
        # The memory system's own state: the bus interface, the fill and
        # slow-path sequencers, and the valid bits fills, stores and DMA
        # write. Each cache's arrays and read registers take the stage that
        # loads their address: F1 for the L1I's, M1 for the L1D's data and AG
        # for its lookup tags, EX for the copies a store reads as it enters WB.
        # M1 also registers the L1D's hit, the set's valid bits and its wait.
        ('MEM', r'(.*\.)?biu\.|(.*\.)?l1i\.(fill|f_line|victim|rr|beat_n|spoilt|valid|si_\w+)$'
                r'|(.*\.)?l1d\.(state|part|fill_it|beat_n|spoilt|rr|row|lbuf|sbuf|victim|d_mmio|d_addr|d_be'
                r'|valid_bits)$'),
        ('EX', r'(.*\.)?(st_wait|wb_p0|wb_p1)$|(.*\.)?l1i\.snoop|(.*\.)?l1d\.probe'),
        ('M1', r'(.*\.)?l1d\.(q_\w+|ev_q|od_q|ev_wd|od_wd|hit|vm2|stall)$|(.*\.)?l1d\.(ev|od)\d(\.|_|$)'),
        ('AG', r'(.*\.)?l1d\.tag$|(.*\.)?l1d\.tag\d(\.|_|$)'),
        ('F1', r'(.*\.)?(f2_block|fetch_block|active|itlb_page)$|l1i_|(.*\.)?l1i\.(blk|req|page|p_new|at|way|tag)$'
               r'|(.*\.)?l1i\.(data|tag)\d(\.|_|$)'),
        ('F2', r'(.*\.)?(l1_valid|l1_block|l1_bytes|mem_data)$|.*\.oplen\.(one|two)_byte'),
        ('L1', r'(.*\.)?(l2_\w+|half|fill_bytes|fill_data)$|(.*\.)?fetch\.sums\.'),
        ('L2', r'(.*\.)?(bytes|half_block|dec|avail)$'),
        # Q's output register may keep only the names of DC's inputs: the
        # decoder's (decode.q in Yosys, decode.by in Quartus) or dc_q's.
        ('Q', r'(.*\.)?(half_valid|tail_ok)$|(.*\.)?align\.(?!redirect_linear$)|(.*\.)?(decode\.(q|by)|dc_q)$'),
        ('WB', r'(.*\.)?(gpr|eflags|eip|seg_\w+|halted|t0|t1|trace_\w+|start|start_linear|arch_\w+|rt_\w+|exc_\w+|io_data'
               r'|hlt|int_take|int_nmi|int_vec|int_wait|nmi_q|nmi_pend|nmi_block|movss_sh|serial)$'),
        # A stage's output record passes most fields through, so they name
        # the register feeding it: ag_out's are ag_s (D), m1_out's m1_s (AG),
        # ex_out's ex_s (M2). ex.flags_in is EX's ex_flags. WB's decisions
        # load with wb_s. The multiply and divide unit works in EX, and
        # md_busy is its wait; its output registers may keep only the CPU's
        # net names (md_result, md_hi) or EX's port names.
        ('EX', r'(.*\.)?(wb_s|wb_flushes|wb_redir|wb_deliver|io_wait|ex_flags|md_busy|md_de|md_hi|md_result)$'
               r'|(.*\.)?ex\.(flags_in|shifter\.cf_in|md_\w+)$|(.*\.)?muldiv\.'),
        ('M2', r'(.*\.)?(ex_s|ex_in|ex_out)$|(.*\.)?ex\.u$'),
        # M1 also writes the L1D model's address registers and the store
        # distances M2 decodes.
        ('M1', r'(.*\.)?(m2_s|m2_load|m2_de|m2_dw|m2_dr|m2_ve|m2_vw|m2_vr|even_q|odd_q|odd_first)$|l1d_'),
        ('AG', r'(.*\.)?(m1_s|m1_out|late_m1)$'),
        # DC's buffer before D, since dfwd and dops take its fields as ports.
        ('DC', r'(.*\.)?(dc|dc0|dc_skid|dc_valid|dc_skid_valid|rom_ok)$'),
        # D also keeps the string engine's copies and waits.
        ('D', r'(.*\.)?(d_eip|d_stop|d_seg_busy|t_busy|d_out|ag_\w+|r_src|r_dst|r_fp|r_cnt|r_c0|r_c1|r_c2'
              r'|s_end1|s_kill1|s_tgt|s_down|str_busy'
              r'|df_busy|io_busy|ser_wait)$|(.*\.)?(dops|dfwd)\.'),
    ),
    'rom': (('ROM', r'(.*\.)?q$'),),
}
# M10K and DSP allowed, LUT RAM forbidden (docs/i386-cpu-design.md, platform
# rules). proc -norom keeps case tables in logic, by choice: the length
# decoders' opcode tables would otherwise become M10K ROMs that take in F2's
# register, putting an M10K read in front of L1's decode (rule 7). That
# costs F2 about 15% on the corrected model.
SYNTHESIS = 'proc -norom; synth_intel_alm -family cyclonev -nolutram'
# The ROM spike checks that a case-table ROM maps to an M10K and routes, so it
# keeps proc's ROM inference.
SPIKE_SYNTHESIS = {'rom': 'synth_intel_alm -family cyclonev -nolutram'}


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path: Path, value) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def sources(spike: str) -> list[Path]:
    top, rtl = SPIKES[spike]
    return [*(RTL / name for name in rtl), SPIKE / 'spike_stimulus.sv', SPIKE / f'{top}.sv']


_CELL_SUFFIX = re.compile(r'_MISTRAL_.*$')


def _owner(cell: str) -> str:
    """The RTL signal a mapped cell name starts from."""
    return _CELL_SUFFIX.sub('', cell)


def path_summary(path: dict) -> dict:
    """Compress one nextpnr critical path to what a reader needs."""
    segments = path.get('path', [])
    logic = [segment for segment in segments if segment['type'] == 'logic']
    routing = sum(segment['delay'] for segment in segments if segment['type'] == 'routing')
    total = sum(segment['delay'] for segment in segments)
    cells = [segment['to']['cell'] for segment in logic]
    owners = []
    for cell in [segments[0]['from']['cell'] if segments else '', *cells,
                 segments[-1]['to']['cell'] if segments else '']:
        owner = _owner(cell)
        if owner and (not owners or owners[-1] != owner):
            owners.append(owner)
    return {
        'from': segments[0]['from']['cell'] if segments else None,
        'to': segments[-1]['to']['cell'] if segments else None,
        'delay_ns': round(total, 3), 'routing_ns': round(routing, 3), 'logic_ns': round(total - routing, 3),
        'logic_cells': len(logic),
        'carry_cells': sum('ALUT_ARITH' in cell for cell in cells),
        # An M10K launches from its data-out ports (A1DATA, B1DATA).
        'm10k_source': any(segment['type'] == 'clk-to-q'
                           and segment['from'].get('port', '').startswith(('A1DATA', 'B1DATA'))
                           for segment in segments),
        'signals': owners,
    }


def register_labels(netlist: dict, top: str, stages) -> dict[str, str]:
    """Each register and M10K cell's stage, from every public name of its output."""
    module = netlist['modules'][top]
    aliases: dict[int, list[str]] = {}
    for net, info in module['netnames'].items():
        # Yosys's generated names spell out the cells a net feeds, not RTL signals.
        if not info.get('hide_name') and '_MISTRAL_' not in net:
            for bit in info['bits']:
                if isinstance(bit, int):
                    aliases.setdefault(bit, []).append(net)
    def match(names: list[str]) -> str | None:
        return next((label for label, pattern in stages
                     if any(re.match(pattern, candidate) for candidate in names)), None)

    def named(bits) -> list[str]:
        return [net for bit in bits if isinstance(bit, int) for net in aliases.get(bit, [])]

    labels = {}
    for name, cell in module['cells'].items():
        if cell['type'] not in ('MISTRAL_FF', 'MISTRAL_M10K'):
            continue
        connections = cell['connections']
        outputs = connections.get('Q', []) if cell['type'] == 'MISTRAL_FF' else []
        labels[name] = (match([_owner(name), *named(outputs)])
                        or (cell['type'] == 'MISTRAL_FF' and match(named(connections.get('DATAIN', []))))
                        or 'envelope')
    return labels


def cells_by_stage(netlist: dict, top: str, labels: dict[str, str]) -> dict[str, int]:
    """Count the labeled FF and M10K cells by stage."""
    module = netlist['modules'][top]
    return dict(sorted(Counter(labels[name] for name, cell in module['cells'].items()
                               if cell['type'] in ('MISTRAL_FF', 'MISTRAL_M10K')).items()))


def route_record(run: dict, identity: dict, source_fingerprint: str, synthesis: dict,
                 version: str) -> dict:
    """Join one route with the synthesis and tool identities for a portable JSONL row."""
    cells = synthesis['cells']
    nextpnr_identity = identity['tools']['nextpnr-mistral']
    nextpnr_commit = re.search(r'\bcommit=([0-9a-f]{8,})', nextpnr_identity)
    if not nextpnr_commit:
        raise ValueError('nextpnr tool identity has no commit')
    critical_paths = run.get('critical_paths') or []
    return {
        'argv': run['argv'],
        'cells_by_stage': synthesis['cells_by_stage'],
        'comb': sum(count for cell_type, count in cells.items() if cell_type.startswith('MISTRAL_ALUT')),
        'critical_path': critical_paths[0] if critical_paths else None,
        'elapsed_seconds': run['elapsed_seconds'],
        'envelope_worst': run.get('envelope_worst', []),
        'ff': cells.get('MISTRAL_FF', 0),
        'fmax_mhz': run.get('fmax_mhz'),
        'gpu_backend': run.get('gpu_backend'),
        'gpu_device_granted': run.get('gpu_device_granted', []),
        'lock': identity['lock'],
        'lock_sha256': identity['lock_sha256'],
        'logic_depth': synthesis['logic_depth'],
        'm10k': cells.get('MISTRAL_M10K', 0),
        'nextpnr': nextpnr_commit.group(1)[:8],
        'seed': run['seed'],
        'source_fingerprint': source_fingerprint,
        'sources': identity['sources'],
        'spike': run['spike'],
        'stages': run.get('stages', {}),
        'synth_json_sha256': synthesis['synth_json_sha256'],
        'synthesis': synthesis['synthesis'],
        'tools': identity['tools'],
        'version': version,
    }


def _label(cell: str, stages, labels) -> str:
    if labels and cell in labels:
        return labels[cell]
    owner = _owner(cell)
    return next((label for label, pattern in stages if re.match(pattern, owner)), 'envelope')


def stage_figures(data: dict, stages, target_mhz: float, labels=None) -> dict:
    """Each stage's worst setup slack and the clock it alone would allow.

    Registers no pattern names (the stimulus and signature envelope) are
    reported as 'envelope'. labels maps cell names to stages (register_labels).
    """
    period = 1000.0 / target_mhz
    worst: dict[str, tuple[float, str]] = {}
    for net in data.get('detailed_net_timings', []):
        for endpoint in net.get('endpoints', []):
            if not endpoint.get('setup_checked') or 'setup_slack_ns' not in endpoint:
                continue
            owner = _owner(endpoint['cell'])
            label = _label(endpoint['cell'], stages, labels)
            slack = float(endpoint['setup_slack_ns'])
            if label not in worst or slack < worst[label][0]:
                worst[label] = (slack, owner)
    return {label: {'setup_slack_ns': round(slack, 3), 'fmax_mhz': round(1000.0 / (period - slack), 2),
                    'endpoint': owner}
            for label, (slack, owner) in sorted(worst.items())}


def envelope_endpoints(data: dict, stages, labels=None, count: int = 5) -> list[dict]:
    """The envelope's worst setup endpoints, one per register name."""
    worst: dict[str, float] = {}
    for net in data.get('detailed_net_timings', []):
        for endpoint in net.get('endpoints', []):
            if not endpoint.get('setup_checked') or 'setup_slack_ns' not in endpoint:
                continue
            owner = _owner(endpoint['cell'])
            if _label(endpoint['cell'], stages, labels) != 'envelope':
                continue
            slack = float(endpoint['setup_slack_ns'])
            worst[owner] = min(slack, worst.get(owner, slack))
    return [{'endpoint': owner, 'setup_slack_ns': round(slack, 3)}
            for owner, slack in sorted(worst.items(), key=lambda item: item[1])[:count]]


def timing_summary(report: Path, stages=(), target_mhz: float = 100.0, labels=None) -> dict:
    data = json.loads(report.read_text())
    clocks = data.get('fmax', {})
    achieved = [float(record['achieved']) for record in clocks.values() if 'achieved' in record]
    if not achieved:
        raise ValueError(f'{report} has no achieved clock frequency')
    return {'fmax_mhz': min(achieved), 'clocks': clocks, 'utilization': data.get('utilization', {}),
            'stages': stage_figures(data, stages, target_mhz, labels),
            'envelope_worst': envelope_endpoints(data, stages, labels),
            'critical_paths': [path_summary(path) for path in data.get('critical_paths', [])]}


_SOURCES = ('MISTRAL_FF', 'MISTRAL_M10K', 'MISTRAL_IB', 'MISTRAL_CLKBUF')
_PASS = ('MISTRAL_NOT', 'MISTRAL_BUF')


def logic_depth(netlist: dict, top: str, stages, labels=None) -> dict:
    """Each stage's deepest logic path in the synthesized netlist.

    A level is one LUT. A carry chain counts one level where its operands
    enter, and its carry cells are counted separately. Registers, M10K
    outputs and pads start a path; a path ends at a register or M10K input and
    belongs to the stage that writes it. The path lists the named nets it
    passes, from its source.
    """
    module = netlist['modules'][top]
    cells = module['cells']
    driver: dict[int, str] = {}
    for name, cell in cells.items():
        for port, bits in cell['connections'].items():
            if cell['port_directions'].get(port) == 'output':
                for bit in bits:
                    if isinstance(bit, int):
                        driver[bit] = name
    names: dict[int, str] = {}
    for net, info in module['netnames'].items():
        # Skip Yosys's generated names; they spell out the cells a net feeds.
        if info.get('hide_name') or '$' in net or '_MISTRAL_' in net:
            continue
        for bit in info['bits']:
            if isinstance(bit, int) and (bit not in names or len(net) < len(names[bit])):
                names[bit] = net
    memo: dict[int, tuple[int, int, int | None]] = {}

    def inputs(cell: dict, skip=('CLK', 'CLK1', 'CLK2')) -> list[int]:
        return [bit for port, bits in cell['connections'].items()
                if cell['port_directions'].get(port) == 'input' and port not in skip
                for bit in bits if isinstance(bit, int)]

    def depth(bit: int) -> tuple[int, int, int | None]:
        # (LUT levels, carry cells, the input bit on the deepest path)
        if bit in memo:
            return memo[bit]
        stack = [bit]
        while stack:
            current = stack[-1]
            if current in memo:
                stack.pop()
                continue
            cell = cells.get(driver.get(current, ''))
            if cell is None or cell['type'] in _SOURCES:
                memo[current] = (0, 0, None)
                stack.pop()
                continue
            if cell['type'] == 'MISTRAL_ALUT_ARITH':
                operands = [b for port in ('A', 'B', 'C', 'D0', 'D1')
                            for b in cell['connections'].get(port, []) if isinstance(b, int)]
                carry = [b for b in cell['connections'].get('CI', []) if isinstance(b, int)]
            else:
                operands, carry = inputs(cell), []
            pending = [b for b in operands + carry if b not in memo]
            if pending:
                stack.extend(pending)
                continue
            best = (0, 0, None)
            for b in operands:
                levels, carries, _ = memo[b]
                step = 0 if cell['type'] in _PASS else 1
                candidate = (levels + step, carries + (cell['type'] == 'MISTRAL_ALUT_ARITH'), b)
                best = max(best, candidate, key=lambda item: item[:2])
            for b in carry:
                levels, carries, _ = memo[b]
                best = max(best, (levels, carries + 1, b), key=lambda item: item[:2])
            memo[current] = best
            stack.pop()
        return memo[bit]

    result: dict[str, dict] = {}
    for name, cell in cells.items():
        if cell['type'] not in ('MISTRAL_FF', 'MISTRAL_M10K'):
            continue
        owner = _owner(name)
        label = _label(name, stages, labels)
        for bit in inputs(cell):
            levels, carries, _ = depth(bit)
            if label not in result or (levels, carries) > (result[label]['lut_levels'], result[label]['carry_cells']):
                result[label] = {'lut_levels': levels, 'carry_cells': carries, 'endpoint': owner, '_bit': bit}
    for record in result.values():
        path, bit = [], record.pop('_bit')
        while bit is not None:
            net = names.get(bit)
            if net and (not path or path[-1] != net):
                path.append(net)
            bit = memo.get(bit, (0, 0, None))[2]
        record['path'] = path[::-1]
    return dict(sorted(result.items()))


def expected_backend(identity: str) -> str:
    return 'cuda' if 'gpu-router=CUDA' in identity else 'hip'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--spike', choices=tuple(SPIKES), action='append')
    parser.add_argument('--seed', type=int, action='append', help='placer/router seed; repeat (default 1-4)')
    parser.add_argument('--target-mhz', type=float, default=100.0)
    parser.add_argument('--lock', type=Path, default=LOCK,
                        help=f'toolchain lock relative to sources/misteross (default: {LOCK})')
    parser.add_argument('--cache-root', type=Path, help='shared misteross-toolchains cache root')
    parser.add_argument('--gpu-device', type=int, default=0)
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'), default='HIP',
                        help="the toolchain slot's GPU router (default HIP)")
    parser.add_argument('--output', type=Path, help='diagnostic output directory')
    parser.add_argument('--version', default='diagnostic',
                        help='run label stored in every per-route JSONL row (default: diagnostic)')
    args = parser.parse_args()
    spikes = args.spike or list(SPIKES)
    seeds = args.seed or [1, 2, 3, 4]
    if any(seed < 1 or seed > 0x7fffffff for seed in seeds) or len(set(seeds)) != len(seeds):
        parser.error('--seed values must be distinct and between 1 and 2147483647')
    if not math.isfinite(args.target_mhz) or args.target_mhz <= 0:
        parser.error('--target-mhz must be finite and positive')
    if args.cache_root is not None and not args.cache_root.is_absolute():
        parser.error('--cache-root must be absolute')
    if board.gpu_run_active():
        parser.error('run outside gpu-run; each route takes its own lease')

    try:
        if args.gpu_router == 'CUDA':
            tools = board._authenticate_tools(
                ROOT, lock_path=ROOT / args.lock, cache_root=args.cache_root, gpu_router='CUDA',
                hip_architectures='unused',
                expected_configuration={'nextpnr': board.cuda_toolchain_configuration()})
        else:
            tools = board._authenticate_tools(ROOT, lock_path=ROOT / args.lock, cache_root=args.cache_root)
        yosys, nextpnr = tools['yosys'], tools['nextpnr-mistral']
        backend = expected_backend(nextpnr.identity)
        selected = {path.as_posix(): (ROOT / path).read_bytes()
                    for spike in spikes for path in sources(spike)}
        selected[(SPIKE / 'spike.qsf').as_posix()] = (ROOT / SPIKE / 'spike.qsf').read_bytes()
        selected['scripts/benchmark_fes_i386.py'] = Path(__file__).read_bytes()
        identity = {'device': DEVICE, 'target_mhz': args.target_mhz, 'router': 'gpu', 'gpu_backend': backend,
                    'synthesis': SYNTHESIS, 'spike_synthesis': SPIKE_SYNTHESIS, 'lock': args.lock.as_posix(), 'lock_sha256': digest(ROOT / args.lock),
                    'sources': {path: hashlib.sha256(data).hexdigest() for path, data in sorted(selected.items())},
                    'tools': {name: tool.identity for name, tool in sorted(tools.items())}}
        fingerprint = hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()
        output = (args.output or ROOT / 'build/benchmark/fes-i386' / fingerprint[:12]).resolve()
        output.mkdir(parents=True, exist_ok=True)
        snapshot = output / 'sources'
        for path, data in selected.items():
            destination = snapshot / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
        summary = {'format': 1, 'classification': 'i386 timing spike: contained routed diagnostic',
                   'hardware_acceptance': False, 'rbf_generated': False, 'source_fingerprint': fingerprint,
                   'created_utc': datetime.now(timezone.utc).isoformat(), **identity, 'spikes': {}}
        summary_path = output / 'summary.json'
        runs_path = output / 'runs.jsonl'
        write_json(summary_path, summary)

        # CUDA numbers devices by PCI bus, as gpu-run's leases do.
        extra_env = {'NEXTPNR_REQUIRE_GPU': '1'}
        if args.gpu_router == 'CUDA':
            extra_env['CUDA_DEVICE_ORDER'] = 'PCI_BUS_ID'
        route_env = dict(os.environ, **extra_env)
        for spike in spikes:
            top, _ = SPIKES[spike]
            work = output / spike
            work.mkdir(exist_ok=True)
            files = ' '.join(str(snapshot / path) for path in sources(spike))
            synthesis = SPIKE_SYNTHESIS.get(spike, SYNTHESIS)
            program = f'read_verilog -sv {files}; hierarchy -top {top}; {synthesis} -top {top}; stat; write_json synth.json'
            (work / 'synth.ys').write_text(program + '\n')
            started = time.monotonic()
            with (work / 'yosys.log').open('w') as log:
                result = board._run_command([str(yosys.path), '-s', 'synth.ys'], cwd=work,
                                            stdout=log, stderr=log)
            if result.returncode:
                raise RuntimeError(f'synthesis failed; see {work / "yosys.log"}')
            netlist = json.loads((work / 'synth.json').read_text())
            cells = dict(sorted(Counter(cell['type'] for cell in netlist['modules'][top]['cells'].values()).items()))
            labels = register_labels(netlist, top, STAGES[spike])
            record = {'top': top, 'synthesis': synthesis,
                      'synth_seconds': round(time.monotonic() - started, 1), 'cells': cells,
                      'synth_json_sha256': digest(work / 'synth.json'),
                      'cells_by_stage': cells_by_stage(netlist, top, labels),
                      'logic_depth': logic_depth(netlist, top, STAGES[spike], labels), 'runs': []}
            print(f'{spike} logic depth: ' + ', '.join(f'{label} {depth["lut_levels"]} LUT'
                                                      + (f' + {depth["carry_cells"]} carry' if depth['carry_cells'] else '')
                                                      for label, depth in record['logic_depth'].items()), flush=True)
            summary['spikes'][spike] = record
            write_json(summary_path, summary)

            for seed in seeds:
                destination = work / f'seed-{seed}'
                destination.mkdir(exist_ok=True)
                command = [str(nextpnr.path), '--json', str(work / 'synth.json'), '--device', DEVICE,
                           '--top', top, '--qsf', str(snapshot / SPIKE / 'spike.qsf'),
                           '--freq', f'{args.target_mhz:g}', '--seed', str(seed),
                           '--router', 'gpu', '--gpu-device', str(args.gpu_device), '--timing-allow-fail',
                           '--write', str(destination / 'routed.json'), '--report', str(destination / 'timing.json'),
                           '--detailed-timing-report']
                started = time.monotonic()
                with (destination / 'route.log').open('w') as log:
                    result = board._run_command(command, cwd=work, env=route_env, stdout=log, stderr=log,
                                                gpu_label=f'i386-spike-{spike}-seed{seed}')
                elapsed = round(time.monotonic() - started, 1)
                log_text = (destination / 'route.log').read_text(errors='replace')
                run = {'spike': spike, 'seed': seed, 'argv': command, 'exit_code': result.returncode,
                       'elapsed_seconds': elapsed, 'env': extra_env,
                       **board.gpu_route_evidence(log_text, args.gpu_device)}
                if result.returncode == 0:
                    run['gpu_backend'] = board._require_gpu_backend(log_text, backend)
                    run.update(timing_summary(destination / 'timing.json', STAGES[spike], args.target_mhz, labels))
                    run['target_met'] = run['fmax_mhz'] >= args.target_mhz
                    stages = ', '.join(f'{label} {figure["fmax_mhz"]:.1f}' for label, figure in run['stages'].items())
                    print(f'{spike} seed {seed}: {run["fmax_mhz"]:.2f} MHz; '
                          f'target {args.target_mhz:g} MHz {"met" if run["target_met"] else "missed"} ({stages})',
                          flush=True)
                record['runs'].append(run)
                with runs_path.open('a') as stream:
                    stream.write(json.dumps(route_record(run, identity, fingerprint, record, args.version),
                                            sort_keys=True) + '\n')
                write_json(summary_path, summary)
                if result.returncode:
                    raise RuntimeError(f'routing failed; see {destination / "route.log"}')
            fmax = [run['fmax_mhz'] for run in record['runs']]
            record.update({'worst_fmax_mhz': min(fmax), 'best_fmax_mhz': max(fmax),
                           'all_targets_met': all(run['target_met'] for run in record['runs'])})
            write_json(summary_path, summary)
        print('Diagnostic evidence: ' + str(summary_path), flush=True)
        return 0
    except (OSError, ValueError, RuntimeError, board.BuildError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
