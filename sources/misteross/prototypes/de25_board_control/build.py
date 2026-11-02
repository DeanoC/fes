#!/usr/bin/env python3
"""Overlay the embedded-control diagnostic onto a frozen DE25 ZX81 producer.

This builds a private, explicitly two-revision diagnostic; it never programs a
board. An opt-in canonical package is for model use only.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import inspect
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import reader_memory
MISTEROSS = HERE.parents[1]
ROOT = MISTEROSS.parents[1]
BOARD = "sources/misteross/boards/de25nano/"
OVERLAY = "sources/misteross/prototypes/de25_board_control/"
ENDPOINT = "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv"
DEFINITION = "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh"
INPUTS = (OVERLAY + "build.py", OVERLAY + "fes_de25_embedded.sv",
          OVERLAY + "fes_de25_embedded_hw.tcl", OVERLAY + "integrate.tcl", OVERLAY + "seal.py",
          OVERLAY + "reader_memory.py", ENDPOINT, DEFINITION)
BOOTSTRAP = ("build.py", "display/build.py", "zx81/build.py", "zx81-hps/build.py")
BASE = 0x10400
SPAN = 0x100
CONTROL_ABI = 0x10203040
COMPILE_TASKS = ('dni_ipgenerate', 'dni_elaboration', 'dni_synthesis',
                 'fitter_plan', 'fitter_place', 'fitter_route', 'fitter_retime',
                 'fitter_finalize', 'sta_signoff', 'assembler')
PHASE_LIMIT_SECONDS = 120


def run_phase(command, directory, log):
    """Bound one standard Quartus task and stop only its own process group."""
    command = [str(value) for value in command]
    print('+ ' + ' '.join(command), flush=True)
    started = time.monotonic()
    with log.open('xb') as stream:
        child = subprocess.Popen(command, cwd=directory, stdout=stream,
                                 stderr=subprocess.STDOUT, start_new_session=True)
        beat = started + 12
        completed = False
        try:
            while child.poll() is None:
                now = time.monotonic()
                if now - started >= PHASE_LIMIT_SECONDS:
                    raise subprocess.TimeoutExpired(command, PHASE_LIMIT_SECONDS)
                if now >= beat:
                    print('Quartus task ' + command[-1] + ': ' + str(int(now-started)) + 's', flush=True)
                    beat = now + 12
                time.sleep(.1)
            if child.returncode:
                raise subprocess.CalledProcessError(child.returncode, command)
            completed = True
        finally:
            if not completed:
                try:
                    os.killpg(child.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    pass
                # A failed shell can exit before its compiler workers. Stop
                # that owned group even when the direct child was reaped.
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.wait(timeout=5)
    return {'argv': command, 'elapsed_seconds': time.monotonic()-started,
            'log_sha256': digest(log.read_bytes()), 'returncode': child.returncode}


def compile_project(output, tool):
    """Keep the standard flow, with an independent bound for every leaf task."""
    project = output/'project'
    commands = []
    for task in COMPILE_TASKS:
        command = [tool, '--flow', 'compile', 'golden_top', '-start', task, '-end', task]
        commands.append(run_phase(command, project, output/('compile-'+task+'.log')))
    artifact = project/'output_files/golden_top.sof'
    if not artifact.is_file() or not artifact.stat().st_size:
        raise ValueError('Quartus produced no nonempty HPS diagnostic SOF')
    files = {str(path.relative_to(project)): digest(path.read_bytes()) for path in project.rglob('*')
             if path.is_file() and (path.suffix in ('.sdc', '.sopcinfo') or 'handoff' in str(path))}
    reports = {str(path.relative_to(project)): digest(path.read_bytes()) for path in (project/'output_files').glob('*')
               if path.is_file() and path.suffix in ('.rpt', '.summary')}
    record = {'diagnostic_only': True, 'board': 'de25_nano', 'device': 'A5EB013BB23BE4SCS',
              'fes_revision': json.loads((output/'inputs.json').read_text()).get('fes_revision'),
              'parallel_processors': json.loads((output/'inputs.json').read_text())['compile_processors'],
              'artifact': str(artifact.relative_to(output)), 'sha256': digest(artifact.read_bytes()),
              'quartus_version': (output/'quartus-version.txt').read_text().strip(),
              'commands': commands, 'phase_limit_seconds': PHASE_LIMIT_SECONDS,
              'inputs_sha256': digest((output/'inputs.json').read_bytes()),
              'report_sha256': reports, 'generated_handoff_and_sdc_sha256': files,
              'timing_acceptance': 'not-qualified', 'hardware_acceptance': 'not-run',
              'boot_firmware_compatibility': 'not-qualified'}
    (output/'build.json').write_text(json.dumps(record,indent=2,sort_keys=True)+'\n')
    return artifact


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(*args, check=True):
    return subprocess.run(["git", "-C", str(ROOT), *args], stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, check=check)


def capture_overlay(allow_dirty):
    revision = git("rev-parse", "--verify", "HEAD").stdout.decode().strip()
    tracked = git("ls-files", "--error-unmatch", "--", *INPUTS, check=False).returncode == 0
    changed = git("diff", "--quiet", "HEAD", "--", *INPUTS, check=False).returncode
    if changed not in (0, 1):
        raise ValueError("cannot inspect overlay source state")
    dirty = not tracked or changed == 1
    if dirty and not allow_dirty:
        raise ValueError("overlay inputs must be committed; --allow-dirty is diagnostic only")
    sources = {}
    for name in INPUTS:
        path = ROOT / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("overlay source must be a regular file: " + name)
        sources[name] = path.read_bytes()
    return revision, "working-tree" if dirty else "committed", sources


def load_builder(output, revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("board revision must be a full lowercase Git commit")
    bootstrap = output / "bootstrap"
    for relative in BOOTSTRAP:
        target = bootstrap / BOARD / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(git("show", revision + ":" + BOARD + relative).stdout)
    path = bootstrap / BOARD / "zx81-hps/build.py"
    spec = importlib.util.spec_from_file_location("de25_frozen_zx81_builder", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise ValueError("board integration anchor changed")
    return text.replace(before, after)


def configure_processors(output, processors):
    """Override only the captured helper's two-worker compiler selection."""
    if type(processors) is not int or not 1 <= processors <= 16:
        raise ValueError('compile processor count must be between 1 and 16')
    qsf = output/'hps/project/golden_top.qsf'
    source = qsf.read_text()
    pattern = r'(?m)^set_global_assignment -name NUM_PARALLEL_PROCESSORS ([^\r\n]+)$'
    if re.findall(pattern, source) != ['2']:
        raise ValueError('captured compile processor selection differs')
    selected = re.sub(pattern, 'set_global_assignment -name NUM_PARALLEL_PROCESSORS '+str(processors), source)
    inputs_file = output/'hps/inputs.json'
    inputs = json.loads(inputs_file.read_text())
    inputs['compile_processors'] = processors
    qsf.write_text(selected)
    inputs_file.write_text(json.dumps(inputs, indent=2, sort_keys=True)+'\n')


def overlay_project(output, board, sources, identity, record=None):
    project = output / "hps/project"
    local = project / "fes_mailbox"
    wrapper = sources[OVERLAY + "fes_de25_embedded.sv"].decode()
    if (re.findall(r"parameter\s+\[127:0\]\s+BUILD_ID\s*=\s*`(\w+)", wrapper) != ["FES_DE25_COMPUTER_BUILD_ID"] or
            re.findall(r"parameter\s+\[31:0\]\s+CONTROL_ABI\s*=\s*32'h([0-9a-fA-F]{8})", wrapper) != [f"{CONTROL_ABI:08x}"]):
        raise ValueError("embedded wrapper default build ID or ABI differs")
    for source, target in ((OVERLAY + "fes_de25_embedded.sv", "fes_de25_embedded.sv"),
                           (OVERLAY + "fes_de25_embedded_hw.tcl", "fes_de25_embedded_hw.tcl"),
                           (ENDPOINT, "fes_embedded_control.sv"),
                           (DEFINITION, "generated/fes_embedded_control.vh")):
        (local / target).write_bytes(sources[source])
    integration = project / "integrate-mailbox.tcl"
    script = integration.read_text()
    anchor = "save_system $peripheral_system\n\n# Refresh"
    script = replace_once(script, anchor, sources[OVERLAY + "integrate.tcl"].decode() + "\n" + anchor)
    inputs_file = output / "hps/inputs.json"
    inputs = json.loads(inputs_file.read_text())
    memory = reader_memory.checked(inputs['reader_memory'])
    mode = int(memory['backend'] == 'hps-ddr')
    script = script.replace('@MEMORY_BACKEND@', str(mode)).replace('@DDR_BASE@', str(memory['bus_base'])).replace('@DDR_BYTES@', str(memory['bytes']))
    if mode:
        # The captured producer has already expanded @TOP_SYSTEM@. Insert
        # while the top system, not its child subsystem, is loaded.
        lines = script.splitlines(keepends=True)
        ends = [i for i,line in enumerate(lines) if line.startswith('save_system ') and '$peripheral_system' not in line]
        if len(ends) != 1:
            raise ValueError('captured top-system save anchor differs')
        lines.insert(ends[0], 'add_connection subsys_periph.fes_embedded_mem subsys_hps.f2sdram_adapter_axi4_sub\n'
                     'set_connection_parameter_value subsys_periph.fes_embedded_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n'
                     '# Hold every maintenance master, including the coherent HPS path.\n'
                     'remove_connection rst_in.out_reset/subsys_debug.reset\n'
                     'add_connection subsys_periph.fes_maintenance_reset subsys_debug.reset\n')
        script = ''.join(lines)
    integration.write_text(script)
    build_id = inputs["computer"]["build_id"]
    qsf = (project / "golden_top.qsf").read_text()
    if qsf.count(f'FES_DE25_COMPUTER_BUILD_ID=128\'h{build_id}') != 1:
        raise ValueError("board build-ID assignment differs from the retained input record")
    if not re.fullmatch(r"[0-9a-f]{32}", build_id) or build_id == "0" * 32:
        raise ValueError("board producer emitted an invalid build ID")
    fingerprint_fields = {"source_sha256": inputs['functional_snapshot_sha256'],
        "vendor_sha256": inputs['vendor_archive_sha256'], "system_hz": 100000000, "pixel_hz": 74250000,
        "reader_memory": memory}
    if 'hps_initialization' in inputs['computer']:
        fingerprint_fields['hps_initialization'] = inputs['computer']['hps_initialization']
    fingerprint = digest(json.dumps(fingerprint_fields, sort_keys=True).encode())[:32]
    qsf = replace_once(qsf, f"FES_DE25_COMPUTER_BUILD_ID=128'h{build_id}", f"FES_DE25_COMPUTER_BUILD_ID=128'h{fingerprint}")
    build_id = fingerprint
    if record is not None:
        import seal
        build_id = seal.build_identity(record)
        if build_id == "0" * 32:
            raise ValueError("canonical build ID is zero")
        qsf = replace_once(qsf, f"FES_DE25_COMPUTER_BUILD_ID=128'h{fingerprint}",
                           f"FES_DE25_COMPUTER_BUILD_ID=128'h{build_id}")
    (project / "golden_top.qsf").write_text(qsf)
    inputs["computer"]["build_id"] = build_id
    inputs["computer"]["integration_sha256"] = digest(script.encode())
    inputs["embedded_control"] = {"build_id": build_id, "control_abi": reader_memory.control_abi(memory),
        "peripheral_offset": BASE, "span": SPAN,
        "jtag_bridge_base": board.JTAG_CONTROL_BRIDGE_BASE,
        "source_sha256": {name: digest(sources[name]) for name in INPUTS},
        "physical_cpu_mmio": "not-established", "physical_ddr": "wired-unqualified" if mode else "not-connected",
        "reader_memory": memory,
        "traffic": "bounded read-only AXI DDR window" if mode else "bounded on-chip diagnostic responder", "package_admission": "not-qualified",
        "identity_class": "canonical functional build record; diagnostic only" if record else "board-producer source/vendor fingerprint; not a package seal",
        "board_fingerprint": fingerprint,
        **identity}
    if record is not None:
        inputs["embedded_control"]["build_inputs_sha256"] = digest(record)
    inputs_file.write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return build_id


def verify_control(project, hps, board, memory=None):
    memory = reader_memory.checked(memory if memory is not None else reader_memory.selection('onchip',0,0))
    mode = int(memory['backend'] == 'hps-ddr')
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    component = modules.get("subsys_periph.fes_embedded")
    if component is None or component.get("kind") != "fes_de25_embedded" or component.get("version") != "1.0":
        raise ValueError("generated embedded control endpoint is missing or differs")
    ports = hps.component_ports(component, "avs")
    expected = {"address": ("Input", 8), "read": ("Input", 1), "write": ("Input", 1),
                "byteenable": ("Input", 4), "writedata": ("Input", 32), "readdata": ("Output", 32),
                "readdatavalid": ("Output", 1), "waitrequest": ("Output", 1),
                "writeresponsevalid": ("Output", 1), "response": ("Output", 2)}
    if ports != expected:
        raise ValueError("generated embedded Avalon interface differs")
    generated = project / "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded"
    material = generated / "fes_de25_embedded_10/synth"
    local = project / "fes_mailbox"
    for generated_name, source_name in (("fes_de25_embedded.sv", "fes_de25_embedded.sv"),
                                        ("fes_embedded_control.sv", "fes_embedded_control.sv"),
                                        ("fes_embedded_control.vh", "generated/fes_embedded_control.vh")):
        if (material / generated_name).read_bytes() != (local / source_name).read_bytes():
            raise ValueError("generated embedded source differs: " + generated_name)
    wrapper = (generated / "synth/peripheral_subsys_fes_embedded.v").read_text()
    matches = re.findall(r"\bfes_de25_embedded\s*#\((.*?)\)\s+fes_embedded\s*\(", wrapper, re.S)
    if len(matches) != 1:
        raise ValueError('generated embedded parameterized instance differs')
    pairs = re.findall(r'\.(\w+)\s*\(\s*([^()]*)\s*\)', matches[0])
    remainder = re.sub(r'\.\w+\s*\(\s*[^()]*\s*\)', '', matches[0])
    def integer(value):
        value=value.strip()
        match=re.fullmatch(r"(?:32)?'([bdh])([0-9a-fA-F]+)",value)
        return int(match[2],{'b':2,'d':10,'h':16}[match[1]]) if match else int(value,0)
    expected_params={'MEMORY_BACKEND':mode,'DDR_BASE':memory['bus_base'],'DDR_BYTES':memory['bytes']}
    defaults=re.findall(r'\bparameter\s*(?:\[[^\]]+\]\s*)?(\w+)\s*=\s*([^,\r\n)]+)',wrapper)
    parent=(project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v').read_text()
    if (remainder.strip(' \r\n\t,') or len(pairs)!=3 or
            {name:value.strip() for name,value in pairs} != {name:name for name in expected_params} or
            len(defaults)!=3 or {name:integer(value) for name,value in defaults}!=expected_params or
            len(re.findall(r'\bperipheral_subsys_fes_embedded\s+fes_embedded\s*\(',parent))!=1):
        raise ValueError('generated embedded memory parameters differ or override build identity')
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    required = (("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_embedded.avs"),
                ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_embedded.clk"),
                ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_embedded.reset"))
    if any(key not in connections for key in required):
        raise ValueError("generated embedded bridge/clock/reset route differs")
    if int(connections[required[0]].findtext("parameter[@name='baseAddress']/value", "-1"), 0) != BASE:
        raise ValueError("generated embedded peripheral base differs")
    if connections[required[1]].findtext("parameter[@name='clockRateSysInfo']/value") != "100000000":
        raise ValueError("generated embedded clock differs")
    memory_routes=[c for c in tree.findall('connection') if c.get('start')=='subsys_periph_fes_embedded.mem']
    if mode:
        expected_mem={'arvalid':('Output',1),'araddr':('Output',32),'arlen':('Output',8),
          'arsize':('Output',3),'arburst':('Output',2),'arid':('Output',1),'arlock':('Output',1),
          'arcache':('Output',4),'arprot':('Output',3),'arqos':('Output',4),'arready':('Input',1),
          'rvalid':('Input',1),'rdata':('Input',128),'rlast':('Input',1),'rresp':('Input',2),
          'rid':('Input',1),'rready':('Output',1),
          'awvalid':('Output',1),'awaddr':('Output',32),'awlen':('Output',8),'awsize':('Output',3),
          'awburst':('Output',2),'awid':('Output',1),'awlock':('Output',1),'awcache':('Output',4),
          'awprot':('Output',3),'awqos':('Output',4),'awready':('Input',1),'wvalid':('Output',1),
          'wdata':('Output',128),'wstrb':('Output',16),'wlast':('Output',1),'wready':('Input',1),
          'bvalid':('Input',1),'bresp':('Input',2),'bid':('Input',1),'bready':('Output',1)}
        if hps.component_ports(component,'mem')!=expected_mem:
            raise ValueError('generated embedded DDR AXI interface differs')
        if (len(memory_routes)!=1 or memory_routes[0].get('end')!='subsys_hps_f2sdram_adapter.axi4_sub' or
            int(memory_routes[0].findtext("parameter[@name='baseAddress']/value",'-1'),0)!=0):
            raise ValueError('generated embedded DDR route differs')
        sink=hps.component_ports(modules['subsys_hps.f2sdram_adapter'],'axi4_sub')
        if sink.get('araddr')!=('Input',32) or sink.get('rdata')!=('Output',256):
            raise ValueError('generated HPS DDR adapter widths differ')
        if ('avalon','subsys_hps_f2sdram_adapter.axi4_man','subsys_hps_agilex_hps.f2sdram') not in connections:
            raise ValueError('generated HPS DDR adapter is not connected to HPS memory')
        if hps.component_ports(component,'maintenance_reset') != {'reset_n':('Output',1)}:
            raise ValueError('generated maintenance hold reset interface differs')
        maintenance = ('subsys_debug_jtag_rst_in.in_reset',
                       'subsys_debug_fpga_m.clk_reset',
                       'subsys_debug_hps_f2sdram.clk_reset',
                       'subsys_debug_hps_m.clk_reset')
        expected_debug={'subsys_debug.fpga_m','subsys_debug.hps_f2sdram','subsys_debug.hps_m'}
        actual_debug={m.get('path') for m in tree.findall('module') if m.get('kind')=='altera_jtag_avalon_master'}
        if actual_debug!=expected_debug:
            raise ValueError('generated maintenance master inventory differs')
        sources = ('subsys_periph_fes_embedded.maintenance_reset',) + ('subsys_debug_jtag_rst_in.out_reset',)*3
        for target, source in zip(maintenance,sources):
            incoming = [c for c in tree.findall('connection') if c.get('kind')=='reset' and c.get('end')==target]
            if len(incoming)!=1 or incoming[0].get('start')!=source:
                raise ValueError('generated maintenance master reset hold route differs: '+target)
        incoming = [c.get('start') for c in tree.findall('connection')
                    if c.get('end')=='subsys_hps_f2sdram_adapter.axi4_sub']
        if sorted(incoming)!=sorted(('ext_hps_f2sdram_master.expanded_master',
                                     'subsys_periph_fes_ddr_probe.avm','subsys_periph_fes_embedded.mem')):
            raise ValueError('generated DDR master inventory differs')
    elif memory_routes:
        raise ValueError('on-chip reader unexpectedly connects to DDR')
    lw = connections[("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0")]
    jtag = connections[("avalon", "subsys_debug_fpga_m.master", "subsys_periph_pb_cpu_0.s0")]
    if int(jtag.findtext("parameter[@name='baseAddress']/value", "-1"), 0) != board.JTAG_CONTROL_BRIDGE_BASE:
        raise ValueError("generated JTAG bridge base differs")
    maps = {}
    for name, connection, base in (("lw", lw, 0), ("jtag", jtag, board.JTAG_CONTROL_BRIDGE_BASE)):
        regions = hps.peripheral_slave_map(connection.findtext("parameter[@name='addressMapSysInfo']/value"),
                                           base + hps.probe_base())
        matches = [r for r in regions if r["name"] == "subsys_periph.fes_embedded.avs"]
        if len(matches) != 1 or matches[0]["base"] != base + BASE or matches[0]["span"] != SPAN:
            raise ValueError("generated embedded " + name + " map differs")
        maps[name] = matches[0]
    return {"instance": component.get("path"), "clock_hz": 100000000, "maps": maps,
            "cpu_physical_address": "not-established", "reader_memory":memory,
            "physical_ddr": "wired-unqualified" if mode else "not-connected",
            "maintenance_masters": "held-reset-from-configuration" if mode else "unchanged",
            "other_ddr_master": "fes_ddr_probe; independent cancel/drain required" if mode else "not-covered"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--board-revision", required=True)
    parser.add_argument("--vendor-archive", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    parser.add_argument("--allow-dirty", action="store_true")
    parser.add_argument("--sealed-diagnostic", action="store_true", help="build a committed diagnostic-only format-2 package")
    parser.add_argument("--hps-first", action="store_true", help="select the captured board producer's HPS-first profile")
    parser.add_argument('--reader-memory', choices=('onchip', 'hps-ddr'), default='onchip')
    parser.add_argument('--ddr-bus-base', type=lambda x:int(x,0), default=0)
    parser.add_argument('--ddr-bytes', type=lambda x:int(x,0), default=0)
    parser.add_argument("--jobs", type=int, choices=range(1, 17), default=2,
                        help="explicit compiler worker limit, recorded in the functional identity (default: 2)")
    args = parser.parse_args()
    try:
        memory = reader_memory.selection(args.reader_memory, args.ddr_bus_base, args.ddr_bytes)
        output = args.output.absolute()
        if ".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or not output.is_relative_to(ROOT / "out") or output.exists():
            raise ValueError("output must be a fresh private directory under this worktree's out/")
        if args.sealed_diagnostic and (args.action != "build" or args.allow_dirty):
            raise ValueError("sealed diagnostic requires build and committed source")
        revision, state, overlay = capture_overlay(args.allow_dirty)
        if args.sealed_diagnostic and git("status", "--porcelain", "--untracked-files=all").stdout:
            raise ValueError("sealed diagnostic requires a clean checkout")
        output.mkdir(parents=True)
        board = load_builder(output, args.board_revision)
        sources = board.capture(args.board_revision)
        if set(sources) & set(overlay):
            raise ValueError("overlay unexpectedly replaces a captured board source")
        sources.update(overlay)
        selected = {"source_classification": "two-revision-diagnostic",
                    "board_fes_revision": args.board_revision, "overlay_fes_revision": revision,
                    "overlay_source_state": state}
        if args.hps_first:
            if "hps_first" not in inspect.signature(board.prepare_captured).parameters:
                raise ValueError("selected board revision does not support HPS-first preparation")
            result, hps, _ = board.prepare_captured(args.vendor_archive, output / "board", sources, selected, hps_first=True)
        else:
            result, hps, _ = board.prepare_captured(args.vendor_archive, output / "board", sources, selected)
        configure_processors(result, args.jobs)
        inputs_file = result / 'hps/inputs.json'
        inputs = json.loads(inputs_file.read_text())
        inputs['reader_memory'] = memory
        inputs_file.write_text(json.dumps(inputs, indent=2, sort_keys=True)+'\n')
        record = None
        if args.sealed_diagnostic:
            import seal
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            tools = seal.tool_snapshot(tool, version)
            inputs = json.loads((result / "hps/inputs.json").read_text())
            record = seal.make_record(inputs, revision, git("remote", "get-url", "origin").stdout.decode().strip(), tools)
        build_id = overlay_project(result, board, sources, selected, record)
        print("DE25 embedded diagnostic:", result, "build ID:", build_id, flush=True)
        if args.action != "project":
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            board.generate(result, hps, tool, version)
            generation = result / "hps/generation.json"
            evidence = json.loads(generation.read_text())
            evidence["embedded_control"] = verify_control(result / "hps/project", hps, board, memory)
            generation.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n")
            if args.action == "build":
                artifact = compile_project(result / "hps", tool)
                checks = {"system": board.pll_review(result / "hps/project/ip/pll", 100000000),
                          "pixel": board.pll_review(result / "hps/project/sys_pll", 74250000)}
                if checks != evidence["computer"]["pll_reviews"]:
                    raise ValueError("compile changed reviewed PLL identity")
                print("Diagnostic SOF:", artifact, flush=True)
                if record is not None:
                    if (revision, state, overlay) != capture_overlay(False):
                        raise ValueError("overlay source changed during build")
                    sealed = seal.publish(result / "hps", artifact, args.vendor_archive, tool, record, tools, version)
                    print("Diagnostic package:", sealed["package_id"], flush=True)
        if (revision, state, overlay) != capture_overlay(args.allow_dirty):
            raise ValueError("overlay source changed during the build")
        return 0
    except (OSError, ValueError, KeyError, subprocess.SubprocessError, ET.ParseError) as error:
        print("de25 embedded diagnostic:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
