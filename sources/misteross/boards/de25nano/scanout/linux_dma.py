"""Prepare the explicit, unadmitted Linux SMMU profile in a private project."""
import hashlib
from pathlib import Path
import re

HPS_IP = 'hps_subsys/ip/hps_subsys/agilex_hps.ip'
ADAPTER = 'custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl'


def constant(definition, name):
    values = re.findall(r"^`define FES_DE25_SCANOUT_" + name + r" 32'h([0-9a-f]{8})$", definition, re.M)
    if len(values) != 1:
        raise ValueError('missing generated DMA constant: ' + name)
    return int(values[0], 16)


def retarget_adapter(data, definition):
    """Keep vendor AXI behavior; extend only fixed address USER fields."""
    sid = constant(definition, 'SMMU_STREAM_ID')
    shift = constant(definition, 'SMMU_SID_SHIFT')
    secure_bit = constant(definition, 'SMMU_SECURE_BIT')
    width = constant(definition, 'SMMU_USER_BITS')
    if sid <= 11 or sid > 65535 or (shift, secure_bit, width) != (8, 24, 25):
        raise ValueError('invalid selected SMMU stream/layout')
    text = data.decode()
    for channel in ('ar', 'aw'):
        before = f'add_interface_port axi4_man man_{channel}user {channel}user Output 8\n'
        if text.count(before) != 1:
            raise ValueError('vendor address USER declaration changed')
        text = text.replace(before, before.replace('Output 8', 'Output 25'))
        pattern = r'(output wire\s+)\\\[7:0\\\](\s+man_' + channel + r'user\s*,)'
        text, count = re.subn(pattern, lambda m: m[1] + r'\[24:0\]' + m[2], text)
        if count != 1:
            raise ValueError('vendor address USER HDL port changed')
        pattern = r'(assign man_' + channel + r"user\s*=\s*)8'hE0(\s*;)"
        text, count = re.subn(pattern, lambda m: m[1] + f"25'h{((sid << shift) | 0xe0):07x}" + m[2], text)
        if count != 1:
            raise ValueError('vendor address USER fixed attributes changed')
    return text.encode(), {'stream_id': sid, 'user_bits': width, 'sid_shift': shift,
        'secure_bit': secure_bit, 'secure': False, 'attributes': 0xe0,
        'scope': 'scanout-only generated F2SDRAM path', 'master_ownership': 'scanout-only',
        'original_sha256': hashlib.sha256(data).hexdigest(), 'derived_sha256': hashlib.sha256(text.encode()).hexdigest(),
        'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}


def configure(project, definition, hps):
    path = project / ADAPTER
    data, profile = retarget_adapter(path.read_bytes(), definition)
    path.write_bytes(data)
    child = hps.tcl_word(project / 'hps_subsys/hps_subsys.qsys')
    script = f'''# Explicit Linux SMMU profile. This changes port exposure, not boot data.
load_system {child}
load_component agilex_hps
set_component_parameter_value f2sdram_SMMU true
if {{[get_component_parameter_value f2sdram_SMMU] != 1}} {{error "SMMU port exposure was not enabled"}}
save_component
load_component f2sdram_adapter
if {{[get_component_parameter_value DATA_WIDTH] != 256}} {{error "F2SDRAM adapter data width changed"}}
foreach port {{man_aruser man_awuser}} {{
    if {{[get_component_interface_port_property axi4_man $port WIDTH] != 25}} {{error "SMMU USER width changed"}}
}}
save_component
save_system {child}
'''
    jtag = hps.tcl_word(project / 'jtag_subsys/jtag_subsys.qsys')
    script += f'''# Remove the JTAG master for the Linux-owned fabric memory path.
load_system {jtag}
if {{[get_interface_property hps_f2sdram_master EXPORT_OF] ne "hps_f2sdram.master"}} {{error "JTAG memory export changed"}}
remove_interface hps_f2sdram_master
remove_instance hps_f2sdram
save_system {jtag}
'''
    return script, profile, {HPS_IP: {'f2sdram_SMMU': {'before': 'false', 'after': 'true'}}}



def isolate_script(script, top_save):
    probe = ("add_instance fes_ddr_probe fes_de25_ddr_probe 1.0\n"
        "add_connection periph_clk.out_clk fes_ddr_probe.clk\n"
        "add_connection periph_rst_in.out_reset fes_ddr_probe.reset\n"
        "add_connection pb_cpu_0.m0 fes_ddr_probe.avs\n"
        r"set_connection_parameter_value pb_cpu_0.m0/fes_ddr_probe.avs baseAddress 0x[0-9a-f]+" + "\n"
        "add_interface fes_probe_mem avalon start\n"
        "set_interface_property fes_probe_mem EXPORT_OF fes_ddr_probe.avm\n")
    pattern = re.escape(probe).replace(re.escape(r"0x[0-9a-f]+"), r"0x[0-9a-f]+")
    script, count = re.subn(pattern, "", script)
    if count != 1:
        raise ValueError("inherited probe integration changed")
    route = ("add_connection subsys_periph.fes_probe_mem subsys_hps.f2sdram_adapter_axi4_sub\n"
        "set_connection_parameter_value subsys_periph.fes_probe_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n")
    if script.count(route) != 1 or script.count(top_save) != 1:
        raise ValueError("inherited probe/top integration changed")
    script = script.replace(route, "")
    removal = ("# The Linux stream must have no diagnostic master.\n"
        "remove_instance ext_hps_f2sdram_master\n")
    return script.replace(top_save, removal + top_save)


def verify(project, hps, profile):
    definition = (project / 'fes_mailbox/generated/fes_de25_scanout.vh').read_text()
    sid = constant(definition, 'SMMU_STREAM_ID')
    if profile.get('master_ownership') != 'scanout-only':
        raise ValueError('Linux DMA requires isolated scanout master ownership')
    ownership = hps.verify_scanout_memory_handoff(project)
    if profile.get('stream_id') != sid or profile.get('secure') is not False:
        raise ValueError('captured Linux DMA stream differs')
    if hps.digest(project / ADAPTER) != profile['derived_sha256']:
        raise ValueError('derived SMMU adapter changed')
    if hps.module_parameters(project / HPS_IP).get('f2sdram_SMMU') != 'true':
        raise ValueError('generated HPS SMMU ports not enabled')
    import xml.etree.ElementTree as ET
    tree = ET.parse(project / 'qsys_top/qsys_top.sopcinfo')
    modules = {m.get('path'): m for m in tree.findall('module')}
    for name, interface, direction in (('subsys_hps.agilex_hps', 'f2sdram', 'Input'),
                                       ('subsys_hps.f2sdram_adapter', 'axi4_man', 'Output')):
        ports = hps.component_ports(modules[name], interface)
        if any(ports.get(channel + 'user') != (direction, 25) for channel in ('ar', 'aw')):
            raise ValueError('generated SMMU USER path width differs')
    word = (sid << 8) | 0xe0
    sources = []
    directory = project / 'hps_subsys/ip/hps_subsys/f2sdram_adapter'
    qip = directory / 'f2sdram_adapter.qip'
    references = re.findall(r'-name (?:VERILOG_FILE|SYSTEMVERILOG_FILE) \[file join \$::quartus\(qip_path\) "([^"\n]+)"\]', qip.read_text())
    if not references:
        raise ValueError('generated adapter QIP has no selected HDL')
    for name in references:
        relative = Path(name)
        if relative.is_absolute() or '..' in relative.parts:
            raise ValueError('unsafe generated adapter HDL reference')
        path = directory / relative
        if path.is_symlink() or not path.is_file():
            raise ValueError('generated adapter HDL is not a regular file')
        text = path.read_text()
        if all(re.search(r'assign man_' + channel + r"user\s*=\s*25'h" + f'{word:07x}' + r'\s*;', text)
               for channel in ('ar', 'aw')):
            sources.append(path)
    if len(sources) != 1:
        raise ValueError('generated adapter does not drive the selected nonsecure stream')
    return {**profile, 'generated_adapter': str(sources[0].relative_to(project)),
            'generated_adapter_sha256': hps.digest(sources[0]), 'generated_qip_sha256': hps.digest(qip),
            'address_user_word': word, 'master_handoff': ownership,
            'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}
