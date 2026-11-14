#!/usr/bin/env python3
"""Reject altered copies of an actual generated DE25 handoff."""
import argparse
import importlib.util
import json
from pathlib import Path
import shutil
import sys
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
SPEC = importlib.util.spec_from_file_location("de25_control_builder", HERE / "build.py")
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)

FILES = ("qsys_top/qsys_top.sopcinfo", "fes_mailbox/fes_de25_embedded.sv",
         "fes_mailbox/fes_de25_read_width.sv",
         "peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v",
         "fes_mailbox/fes_embedded_control.sv", "fes_mailbox/generated/fes_embedded_control.vh",
         "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/synth/peripheral_subsys_fes_embedded.v",
         "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_de25_embedded.sv",
         "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_de25_read_width.sv",
         "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_embedded_control.sv",
         "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_embedded_control.vh")
WINDOW_FILES = tuple(prefix + name for prefix in (
    "fes_mailbox/",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/",
) for name in ("fes_embedded_window_reader.sv", "fes_embedded_window_control.sv"))
COMPUTER_FILES = tuple(prefix + name for prefix in (
    "fes_mailbox/",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_computer/fes_de25_computer_10/synth/",
) for name in ("fes_de25_computer.v", "generated/fes_de25_computer.vh"))


def copy_project(source, target):
    for name in FILES + WINDOW_FILES + COMPUTER_FILES:
        dst = target / name
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source / name, dst)


def mutate(project, kind):
    handoff = project / "qsys_top/qsys_top.sopcinfo"
    tree = ET.parse(handoff)
    root = tree.getroot()
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in root.findall("connection")}
    endpoint = ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_embedded.avs")
    jtag = ("avalon", "subsys_debug_fpga_m.master", "subsys_periph_pb_cpu_0.s0")
    lw = ("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0")
    if kind == "wrong-base":
        connections[endpoint].find("parameter[@name='baseAddress']/value").text = "0x10440"
    elif kind == "missing-jtag":
        root.remove(connections[jtag])
    elif kind == "wrong-span":
        value = connections[lw].find("parameter[@name='addressMapSysInfo']/value")
        mapping = ET.fromstring(value.text)
        slave = next(x for x in mapping.findall("slave") if x.get("name") == "subsys_periph.fes_embedded.avs")
        slave.set("end", hex(int(slave.get("start"), 0) + 128))
        value.text = ET.tostring(mapping, encoding="unicode")
    elif kind in ('missing-ddr','duplicate-ddr','wrong-ddr-target'):
        connection=next(c for c in root.findall('connection') if c.get('start')=='subsys_periph_fes_embedded.mem')
        if kind=='missing-ddr':root.remove(connection)
        elif kind=='duplicate-ddr':root.append(ET.fromstring(ET.tostring(connection)))
        else:connection.set('end','unqualified.memory')
    elif kind in ('ddr-port-width','ddr-narrow-read'):
        component=next(m for m in root.findall('module') if m.get('path')=='subsys_periph.fes_embedded')
        value=component.find("parameter[@name='componentDefinition']/value")
        definition=ET.fromstring(value.text)
        interface=next(i for i in definition.findall('boundary/interfaces/interface') if i.findtext('name')=='mem')
        role='rdata' if kind=='ddr-narrow-read' else 'wdata'
        port=next(p for p in interface.findall('ports/port') if p.findtext('role')==role)
        port.find('width').text='128' if kind=='ddr-narrow-read' else '129'
        value.text=ET.tostring(definition,encoding='unicode')
    elif kind in ('ddr-read-issuing','ddr-combined-issuing'):
        component=next(m for m in root.findall('module') if m.get('path')=='subsys_periph.fes_embedded')
        value=component.find("parameter[@name='componentDefinition']/value")
        definition=ET.fromstring(value.text)
        interface=next(i for i in definition.findall('boundary/interfaces/interface') if i.findtext('name')=='mem')
        key='readIssuingCapability' if kind=='ddr-read-issuing' else 'combinedIssuingCapability'
        entry=next(e for e in interface.findall('parameters/parameterValueMap/entry') if e.findtext('key')==key)
        entry.find('value').text='99'
        value.text=ET.tostring(definition,encoding='unicode')
    elif kind in ('missing-maintenance-hold','live-maintenance-reset','duplicate-maintenance-reset'):
        target='subsys_debug_jtag_rst_in.in_reset'
        connection=next(c for c in root.findall('connection') if c.get('kind')=='reset' and c.get('end')==target)
        if kind=='missing-maintenance-hold':root.remove(connection)
        elif kind=='duplicate-maintenance-reset':root.append(ET.fromstring(ET.tostring(connection)))
        else:connection.set('start','rst_in.out_reset')
    elif kind=='extra-ddr-master':
        c=ET.SubElement(root,'connection',kind='avalon',start='unqualified.master',end='subsys_hps_f2sdram_adapter.axi4_sub')
        c.set('name','unqualified.master/subsys_hps_f2sdram_adapter.axi4_sub')
    elif kind=='coherent-master':
        ET.SubElement(root,'module',path='subsys_debug.hps_m',kind='altera_jtag_avalon_master')
    elif kind=='coherent-route':
        ET.SubElement(root,'connection',kind='avalon',start='unqualified.master',end='subsys_hps_agilex_hps.fpga2hps')
    elif kind in ('coherent-port','coherent-clock','coherent-reset','missing-hps-interfaces','duplicate-hps-interface'):
        module=next(m for m in root.findall('module') if m.get('path')=='subsys_hps.agilex_hps')
        value=module.find("parameter[@name='componentDefinition']/value")
        definition=ET.fromstring(value.text)
        interfaces=definition.find('boundary/interfaces')
        if kind=='missing-hps-interfaces':
            interfaces.clear()
        elif kind=='duplicate-hps-interface':
            interfaces.append(ET.fromstring(ET.tostring(interfaces[0])))
        else:
            point=ET.SubElement(interfaces,'interface')
            ET.SubElement(point,'name').text={'coherent-port':'fpga2hps','coherent-clock':'fpga2hps_clock','coherent-reset':'fpga2hps_reset'}[kind]
        value.text=ET.tostring(definition,encoding='unicode')
    else:
        raise ValueError("unknown handoff mutation")
    tree.write(handoff, encoding="utf-8", xml_declaration=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        project, out = args.project.absolute(), args.output.absolute()
        for path in (project, out):
            if ".." in path.parts or any(x.is_symlink() for x in (path, *path.parents)) or not path.is_relative_to(ROOT / "out"):
                raise ValueError("paths must be private locations beneath this worktree's out/")
        if not project.is_dir() or out.exists():
            raise ValueError("project must exist and output must be fresh")
        out.mkdir(parents=True)
        board_dir = project / "board"
        inputs = json.loads((board_dir / "hps/inputs.json").read_text())
        board = BUILD.load_builder(out, inputs["embedded_control"]["board_fes_revision"])
        hps = board.load(board_dir / "functional-source/sources/misteross/boards/de25nano/hps/build.py", "de25_handoff_frozen_hps")
        source = board_dir / "hps/project"
        positive = out / "positive"
        copy_project(source, positive)
        memory=inputs['reader_memory']
        observed = BUILD.verify_control(positive, hps, board, memory)
        results = []
        cases = (("wrong-base", "peripheral base differs"), ("missing-jtag", "subsys_debug_fpga_m.master"),
                 ("wrong-span", "embedded lw map differs"), ("changed-generated-source", "generated embedded source differs"),
                 ("changed-window-source", "generated embedded source differs"))
        cases += (("changed-width-source","generated embedded source differs"),
                  ("changed-computer-source", "generated computer source differs"))
        if BUILD.reader_memory.mode(memory):
            cases += tuple((name,'embedded DDR route differs') for name in ('missing-ddr','duplicate-ddr','wrong-ddr-target'))
            cases += (('ddr-port-width','DDR AXI interface differs'),
                      ('ddr-narrow-read','DDR AXI interface differs'),
                      ('ddr-read-issuing','DDR issuing contract differs'),
                      ('ddr-combined-issuing','DDR issuing contract differs'),
                      ('wrong-forwarded-window','memory parameters differ'),
                      ('parent-override','memory parameters differ'),
                      ('untied-write','generated embedded source differs'),
                      ('untied-maintenance','generated embedded source differs'))
            cases += tuple((name,'maintenance master reset hold route differs') for name in
                           ('missing-maintenance-hold','live-maintenance-reset','duplicate-maintenance-reset'))
            cases += tuple((name,'unused coherent F2H maintenance path remains enabled') for name in
                           ('coherent-master','coherent-route','coherent-port','coherent-clock','coherent-reset'))
            cases += tuple((name,'generated HPS interface inventory differs') for name in ('missing-hps-interfaces','duplicate-hps-interface'))
            cases += (('extra-ddr-master','DDR master inventory differs'),)
        for name, expected in cases:
            trial = out / name
            copy_project(source, trial)
            if name == "changed-generated-source":
                target = trial / 'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_embedded_control.sv'
                target.write_bytes(target.read_bytes() + b"\n// altered for negative test\n")
            elif name == "changed-window-source":
                target = trial / WINDOW_FILES[-1]
                target.write_bytes(target.read_bytes() + b"\n// altered for negative test\n")
            elif name == "changed-width-source":
                target=trial/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_de25_read_width.sv'
                target.write_bytes(target.read_bytes()+b'\n// altered for negative test\n')
            elif name == "changed-computer-source":
                target = trial / COMPUTER_FILES[-2]
                target.write_bytes(target.read_bytes() + b"\n// altered for negative test\n")
            elif name=='wrong-forwarded-window':
                target=trial/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/synth/peripheral_subsys_fes_embedded.v'
                text=target.read_text()
                # The wrapper forwards this name, rather than a literal.
                import re
                text,count=re.subn(r'(\.DDR_BYTES\s*\()DDR_BYTES(\))',r'\g<1>0\2',text)
                if count!=1:raise ValueError('generated window forwarding anchor differs')
                target.write_text(text)
            elif name=='parent-override':
                target=trial/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v'
                target.write_text(target.read_text().replace('peripheral_subsys_fes_embedded fes_embedded (',
                    'peripheral_subsys_fes_embedded #(.MEMORY_BACKEND(0)) fes_embedded ('))
            elif name=='untied-write':
                target=trial/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_de25_embedded.sv'
                text=target.read_text();anchor="assign ddr_awvalid = 1'b0;"
                if text.count(anchor)!=1:raise ValueError('write tie anchor differs')
                target.write_text(text.replace(anchor,"assign ddr_awvalid = 1'b1;"))
            elif name=='untied-maintenance':
                target=trial/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_embedded/fes_de25_embedded_10/synth/fes_de25_embedded.sv'
                text=target.read_text();anchor='assign maintenance_reset_n = !DDR_MODE;'
                if text.count(anchor)!=1:raise ValueError('maintenance hold anchor differs')
                target.write_text(text.replace(anchor,"assign maintenance_reset_n = 1'b1;"))
            else:
                mutate(trial, name)
            try:
                BUILD.verify_control(trial, hps, board, memory)
            except (ValueError, KeyError) as error:
                if expected not in str(error):
                    raise ValueError("altered handoff failed for the wrong reason: " + name) from error
                results.append({"case": name, "rejected": True, "reason": str(error)})
            else:
                raise ValueError("altered handoff was accepted: " + name)
        (out / "result.json").write_text(json.dumps({"status": "passed", "observed": observed, "negative_cases": results}, indent=2) + "\n")
        print("PASS:", out)
        return 0
    except (OSError, ValueError, KeyError, ET.ParseError) as error:
        print("de25 handoff test:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
