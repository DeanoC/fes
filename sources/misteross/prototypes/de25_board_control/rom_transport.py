"""Explicit CPU-fed ROM variant of the captured HPS-first computer producer."""
from __future__ import annotations

import hashlib
from pathlib import Path
import re
import xml.etree.ElementTree as ET

import computer_model
import reader_memory

BOARD_REVISION = '79d616e85e19d65f0d645b67a85ffe7e142dcdfc'

CORE_INPUTS = (
    'sources/misteross/cores/fes-zx81/rtl/zx81_machine.sv',
    'sources/misteross/cores/fes-zx81/rtl/zx81_machine_clock.v',
    'sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v',
)
FILES = ('fes_de25_rom.sv', 'fes_de25_rom_hw.tcl', 'fes_embedded_rom.sv',
         'fes_embedded_rom_csr.sv', 'generated/fes_embedded_rom.vh')
COMPONENT = 'sources/misteross/boards/de25nano/zx81-hps/fes_de25_computer_hw.tcl'
VIDEO_SOURCE = 'sources/misteross/prototypes/de25_board_control/video.v'
MACRO = 'set_global_assignment -name VERILOG_MACRO FES_DE25_EXTERNAL_ROM=1\n'
INTERFACE = '''
# The ROM and machine share this exact clock/reset domain.
add_interface rom conduit end
set_interface_property rom associatedClock clk
set_interface_property rom associatedReset reset
foreach {port role direction width} {
    rom_address address Output 13 rom_data data Input 8
    rom_cpu_held held Output 1 rom_loaded loaded Input 1 rom_failed failed Input 1
} { add_interface_port rom $port $role $direction $width }
'''


def selection(enabled, model):
    if type(enabled) is not bool:
        raise ValueError('ROM transport selection must be boolean')
    computer_model.checked(model)
    return {'kind': 'cpu-csr-v1', 'bytes': 4096 if model == 'zx80' else 8192,
            'peripheral_offset': 0x10500, 'span': 128} if enabled else None


def checked(inputs):
    value = inputs.get('rom_transport')
    if value is None:
        return None
    expected = selection(True, inputs.get('computer_model', 'zx81'))
    if (not isinstance(value, dict) or value != expected or
            any(type(value[k]) is not type(v) for k, v in expected.items()) or
            inputs.get('board_fes_revision') != BOARD_REVISION or
            reader_memory.mode(inputs['reader_memory']) != 1 or
            inputs['computer'].get('hps_initialization') != 'HPS FIRST'):
        raise ValueError('ROM transport requires the exact HPS-first DDR computer variant')
    return value


def parameters(inputs):
    value = checked(inputs)
    return {'rom_transport': value['kind'], 'rom_bytes': value['bytes'],
            'rom_peripheral_offset': value['peripheral_offset'], 'rom_span': value['span']} if value else {}


def integration(value):
    return (f'add_instance fes_rom fes_de25_rom 1.0\n'
            f'set_instance_parameter_value fes_rom ROM_BYTES {value["bytes"]}\n'
            'add_connection periph_clk.out_clk fes_rom.clk\n'
            'add_connection periph_rst_in.out_reset fes_rom.reset\n'
            'add_connection pb_cpu_0.m0 fes_rom.avs\n'
            f'set_connection_parameter_value pb_cpu_0.m0/fes_rom.avs baseAddress {value["peripheral_offset"]:#x}\n'
            'add_connection fes_computer.rom fes_rom.rom\n')


def install(project: Path, inputs, sources, script):
    value = checked(inputs)
    if value is None:
        return script
    local = project/'fes_mailbox'
    # The ROM variant owns this reviewed capture implementation instead of
    # silently retaining the historical board video's inferred reset control.
    (local/'video.v').write_bytes(sources[VIDEO_SOURCE])
    inputs['computer']['source_sha256']['video.v'] = hashlib.sha256(sources[VIDEO_SOURCE]).hexdigest()
    for name in FILES:
        (local/name).write_bytes(sources['sources/misteross/prototypes/de25_board_control/'+name])
    component = local/'fes_de25_computer_hw.tcl'
    expected = sources[COMPONENT].decode()
    if component.read_text() != expected or 'FES_DE25_EXTERNAL_ROM' in (project/'golden_top.qsf').read_text():
        raise ValueError('captured computer component or ROM selection differs')
    component.write_text(expected+INTERFACE)
    inputs['computer']['source_sha256']['fes_de25_computer_hw.tcl'] = hashlib.sha256(component.read_bytes()).hexdigest()
    qsf = project/'golden_top.qsf'
    qsf.write_text(qsf.read_text()+MACRO)
    anchor = 'save_system $peripheral_system\n'
    if script.count(anchor) != 1 or 'add_instance fes_rom ' in script:
        raise ValueError('ROM integration anchor differs')
    return script.replace(anchor,integration(value)+'\n'+anchor)


def verify(project, inputs, sources):
    value = checked(inputs)
    qsf = (project/'golden_top.qsf').read_text()
    if value is None:
        if 'FES_DE25_EXTERNAL_ROM' in qsf:
            raise ValueError('unselected ROM transport macro')
        return None
    if qsf.count(MACRO) != 1:
        raise ValueError('ROM macro differs from the selected variant')
    local = project/'fes_mailbox'
    if (VIDEO_SOURCE not in sources or (local/'video.v').read_bytes() != sources[VIDEO_SOURCE] or
            inputs['computer']['source_sha256'].get('video.v') != hashlib.sha256(sources[VIDEO_SOURCE]).hexdigest()):
        raise ValueError('ROM computer video differs from the selected source')
    for name in FILES:
        if (local/name).read_bytes() != sources['sources/misteross/prototypes/de25_board_control/'+name]:
            raise ValueError('ROM source differs: '+name)
    component = local/'fes_de25_computer_hw.tcl'
    if (component.read_bytes() != sources[COMPONENT]+INTERFACE.encode() or
            inputs['computer']['source_sha256'].get('fes_de25_computer_hw.tcl') != hashlib.sha256(component.read_bytes()).hexdigest()):
        raise ValueError('ROM computer conduit differs')
    if (project/'integrate-mailbox.tcl').read_text().count(integration(value)) != 1:
        raise ValueError('ROM route, clock, reset or size differs')
    return value


def verify_generated(project, inputs):
    value = checked(inputs)
    tree = ET.parse(project/'qsys_top/qsys_top.sopcinfo')
    modules = {m.get('path'): m for m in tree.findall('module')}
    rom = modules.get('subsys_periph.fes_rom')
    if value is None:
        if rom is not None:
            raise ValueError('unselected generated ROM peripheral')
        return None
    if rom is None or rom.get('kind') != 'fes_de25_rom' or rom.get('version') != '1.0':
        raise ValueError('generated ROM peripheral differs')

    def interfaces(module):
        raw = module.findtext("parameter[@name='componentDefinition']/value", '')
        points = ET.fromstring(raw).findall('boundary/interfaces/interface')
        result = {p.findtext('name'): p for p in points}
        if len(result) != len(points) or None in result:
            raise ValueError('generated ROM interface inventory is ambiguous')
        return result

    def ports(point):
        entries = point.findall('ports/port')
        result = {p.findtext('role'): (p.findtext('direction'), int(p.findtext('width'))) for p in entries}
        if len(entries) != len(result):
            raise ValueError('generated ROM port roles are ambiguous')
        return result

    points = interfaces(rom)
    expected_avs = {'address': ('Input',7), 'read': ('Input',1), 'write': ('Input',1),
        'byteenable': ('Input',4), 'writedata': ('Input',32), 'readdata': ('Output',32),
        'readdatavalid': ('Output',1), 'writeresponsevalid': ('Output',1),
        'response': ('Output',2), 'waitrequest': ('Output',1)}
    expected_rom = {'address': ('Input',13), 'data': ('Output',8), 'held': ('Input',1),
        'loaded': ('Output',1), 'failed': ('Output',1)}
    if (set(points) != {'clk','reset','avs','rom'} or ports(points['avs']) != expected_avs or
            ports(points['rom']) != expected_rom):
        raise ValueError('generated ROM interface differs or adds a master')
    entries=points['avs'].findall('parameters/parameterValueMap/entry')
    properties={e.findtext('key'):e.findtext('value') for e in entries}
    required_properties={'associatedClock':'clk','associatedReset':'reset','addressUnits':'SYMBOLS',
        'bitsPerSymbol':'8','explicitAddressSpan':'128','maximumPendingReadTransactions':'1',
        'maximumPendingWriteTransactions':'1','minimumResponseLatency':'1','waitrequestAllowance':'0'}
    if len(properties)!=len(entries) or any(properties.get(k)!=v for k,v in required_properties.items()):
        raise ValueError('generated ROM Avalon addressing or response contract differs')
    computer = modules.get('subsys_periph.fes_computer')
    if computer is None:
        raise ValueError('generated ROM computer is absent')
    expected_computer = {k: ('Output' if direction=='Input' else 'Input', width)
                          for k,(direction,width) in expected_rom.items()}
    computer_points = interfaces(computer)
    if 'rom' not in computer_points or ports(computer_points['rom']) != expected_computer:
        raise ValueError('generated computer ROM conduit differs')
    connections = tree.findall('connection')

    def connection(kind,start,end):
        found = [c for c in connections if (c.get('kind'),c.get('start'),c.get('end'))==(kind,start,end)]
        if len(found)!=1:
            raise ValueError('generated ROM route differs: '+kind)
        return found[0]

    prefix = 'subsys_periph_'
    avs = connection('avalon',prefix+'pb_cpu_0.m0',prefix+'fes_rom.avs')
    if int(avs.findtext("parameter[@name='baseAddress']/value",'-1'),0) != value['peripheral_offset']:
        raise ValueError('generated ROM byte address differs')
    for endpoint in ('fes_rom','fes_computer'):
        clk = connection('clock',prefix+'periph_clk.out_clk',prefix+endpoint+'.clk')
        if clk.findtext("parameter[@name='clockRateSysInfo']/value")!='100000000':
            raise ValueError('generated ROM/computer clock differs')
        connection('reset',prefix+'periph_rst_in.out_reset',prefix+endpoint+'.reset')
    conduit = [c for c in connections if c.get('kind')=='conduit' and
        {c.get('start'),c.get('end')}=={prefix+'fes_computer.rom',prefix+'fes_rom.rom'}]
    if len(conduit)!=1:
        raise ValueError('generated ROM is not connected to this computer')
    material = project/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_rom'
    for name in FILES:
        if name.endswith('_hw.tcl'):
            continue
        if (material/'fes_de25_rom_10/synth'/name).read_bytes() != (project/'fes_mailbox'/name).read_bytes():
            raise ValueError('generated ROM source differs: '+name)
    wrapper = (material/'synth/peripheral_subsys_fes_rom.v').read_text()
    defaults = re.findall(r'\bparameter\s*(?:integer\s+)?(?:\[[^\]]+\]\s*)?(\w+)\s*=\s*([^,\r\n)]+)',wrapper)
    match = re.findall(r'\bfes_de25_rom\s*#\((.*?)\)\s+fes_rom\s*\(',wrapper,re.S)
    def integer(text):
        text=text.strip()
        literal=re.fullmatch(r"(?:32)?'([bdh])([0-9a-fA-F]+)",text)
        return int(literal[2],{'b':2,'d':10,'h':16}[literal[1]]) if literal else int(text,0)
    parent=(project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v').read_text()
    if (len(defaults)!=1 or defaults[0][0]!='ROM_BYTES' or integer(defaults[0][1])!=value['bytes'] or len(match)!=1 or
            re.sub(r'\s+','',match[0])!='.ROM_BYTES(ROM_BYTES)'):
        raise ValueError('generated ROM size or build override differs')
    if len(re.findall(r'\bperipheral_subsys_fes_rom\s+fes_rom\s*\(',parent))!=1:
        raise ValueError('generated ROM parent instance overrides its parameters')
    return {**value,'clock_hz':100000000,'new_dma_master':False,
            'held_conduit':'fes_computer.rom/fes_rom.rom'}
