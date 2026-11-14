"""Preserve qualified reset ownership through the captured PD reset bridges."""
import hashlib
from pathlib import Path
import re
import xml.etree.ElementTree as ET

import rom_transport

REQUEST_PARAMETERS = dict(NUM_RESET_INPUTS='1', OUTPUT_RESET_SYNC_EDGES='"deassert"',
    SYNC_DEPTH='2', RESET_REQUEST_PRESENT='1', RESET_REQ_WAIT_TIME='1',
    MIN_RST_ASSERTION_TIME='3', RESET_REQ_EARLY_DSRT_TIME='1', ADAPT_RESET_REQUEST='0',
    **{'USE_RESET_REQUEST_IN'+str(i):'0' for i in range(16)})
SOURCES = ('fes_de25_reset_bridge.sv','fes_de25_reset_bridge_hw.tcl')
PERIPHERAL = '''# Preserve early asynchronous containment and qualified memory release.
remove_interface reset
remove_instance periph_rst_in
add_instance periph_rst_in fes_de25_reset_bridge 1.0
add_connection periph_clk.out_clk periph_rst_in.clk
foreach endpoint {sysid led_pio dipsw_pio button_pio pb_cpu_0 fes_mailbox fes_ddr_probe fes_computer fes_embedded fes_rom} {
    add_connection periph_rst_in.out_reset $endpoint.reset
}
add_interface reset reset sink
set_interface_property reset EXPORT_OF periph_rst_in.in_reset
'''
TOP = '''# The top reset already releases on this exact 100 MHz clock.
load_component rst_in
if {[string compare [get_component_parameter_value SYNCHRONOUS_EDGES] none] != 0} {error "captured top reset bridge changed"}
set_component_parameter_value SYNCHRONOUS_EDGES deassert
save_component
add_connection clk_100.out_clk rst_in.clk
'''


def enabled(inputs):
    return rom_transport.checked(inputs) is not None


def qualified_top(project):
    source=(Path(project)/'golden_top.v').read_text()
    _,ports=instance(source,'fes_de25_top_reset','fes_top_reset_inst')
    if ports != dict(clk='system_clk_100_internal',ninit_done='ninit_done',pll_locked='pll_locked',reset_n='top_reset_n'):
        raise ValueError('reset bridges require the reviewed qualified 100 MHz top reset')
    _,soc=instance(source,'qsys_top','soc_inst')
    if soc.get('clk_100_clk')!='system_clk_100_internal' or soc.get('reset_reset_n')!='top_reset_n':
        raise ValueError('qualified reset and generated system clock do not match')


def install(project,inputs,script,sources):
    if not enabled(inputs):return script
    qualified_top(project)
    for name in SOURCES:
        (Path(project)/'fes_mailbox'/name).write_bytes(sources['sources/misteross/prototypes/de25_board_control/'+name])
    anchors=[('save_system $peripheral_system\n',PERIPHERAL),
             ('save_system "'+str(Path(project)/'qsys_top.qsys')+'"\n',TOP)]
    for anchor,block in anchors:
        if script.count(anchor)!=1 or block in script:
            raise ValueError('qualified reset integration anchor changed')
        script=script.replace(anchor,block+anchor)
    return script


def verify(project,inputs):
    script=(Path(project)/'integrate-mailbox.tcl').read_text()
    if not enabled(inputs):
        if PERIPHERAL in script or TOP in script:raise ValueError('unselected qualified reset integration')
        return None
    qualified_top(project)
    if script.count(PERIPHERAL)!=1 or script.count(TOP)!=1:
        raise ValueError('qualified reset integration changed')
    return True


def instance(text,module,name):
    """Read the simple named parameter/port connections emitted by PD.

    Reject duplicate names, nested expressions or a changed shape rather than
    treating unparsed generated HDL as evidence of the reviewed topology.
    """
    text=re.sub(r'//[^\n]*','',text)
    matches=list(re.finditer(r'\b'+re.escape(module)+r'\s*(?:#\s*\((.*?)\)\s*)?'+re.escape(name)+r'\s*\((.*?)\)\s*;',text,re.S))
    if len(matches)!=1:raise ValueError('generated reset instance changed: '+name)
    def fields(body):
        body=body or '';pattern=r'\.(\w+)\s*\(([^()]*)\)'
        pairs=re.findall(pattern,body);values={k:re.sub(r'\s+','',v) for k,v in pairs}
        if len(values)!=len(pairs) or re.sub(pattern,'',body).strip(' ,\t\r\n'):
            raise ValueError('generated reset instance arguments changed: '+name)
        return values
    return fields(matches[0][1]),fields(matches[0][2])


def verify_generated(project,inputs):
    if not enabled(inputs):return None
    project=Path(project)
    top=(project/'qsys_top/synth/qsys_top.v').read_text()
    periph=(project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v').read_text()
    parameters,request=instance(top,'altera_reset_controller','rst_controller')
    expected=dict(clk='clk_100_out_clk_clk',reset_in0='~rst_in_out_reset_reset',
        reset_out='rst_controller_reset_out_reset',reset_req='rst_controller_reset_out_reset_req',
        **{'reset_in'+str(i):"1'b0" for i in range(1,16)},
        **{'reset_req_in'+str(i):"1'b0" for i in range(16)})
    if parameters!=REQUEST_PARAMETERS or request!=expected:
        raise ValueError('memory reset-request sequencing changed')
    _,ocm=instance(top,'ocm','ocm')
    _,child=instance(top,'peripheral_subsys','subsys_periph')
    if (ocm.get('reset')!='rst_controller_reset_out_reset' or ocm.get('reset_req')!='rst_controller_reset_out_reset_req'
        or ocm.get('clk')!='clk_100_out_clk_clk' or child.get('reset_reset_n')!='~rst_controller_reset_out_reset'
        or child.get('clk_clk')!='clk_100_out_clk_clk' or child.get('reset_reset_req')!='rst_controller_reset_out_reset_req'):
        raise ValueError('memory and peripheral qualified reset owners differ')
    params,ports=instance(top,'rst_in','rst_in')
    if params or ports!=dict(clk='clk_100_out_clk_clk',in_reset_n='reset_reset_n',out_reset_n='rst_in_out_reset_reset'):
        raise ValueError('generated top reset bridge clock or passthrough changed')
    params,ports=instance(periph,'peripheral_subsys_periph_rst_in','periph_rst_in')
    if params or ports!=dict(clk='periph_clk_out_clk_clk',in_reset_n='reset_reset_n',in_reset_req='reset_reset_req',out_reset_n='periph_rst_in_out_reset_reset'):
        raise ValueError('early reset request or memory reset left the device owner')
    for name in ('fes_computer','fes_embedded','fes_rom','fes_mailbox','fes_ddr_probe'):
        _,ports=instance(periph,'peripheral_subsys_'+name,name)
        if ports.get('reset_n')!='periph_rst_in_out_reset_reset' or ports.get('clk')!='periph_clk_out_clk_clk':
            raise ValueError('peripheral endpoint left the shared reset: '+name)
    for name,clock,reset in [('button_pio','clk','reset_n'),('dipsw_pio','clk','reset_n'),
            ('led_pio','clk','reset_n'),('sysid','clock','reset_n'),('pb_cpu_0','clk','reset')]:
        _,ports=instance(periph,name,name)
        if (ports.get(clock)!='periph_clk_out_clk_clk' or
                ports.get(reset)!=('~' if reset=='reset' else '')+'periph_rst_in_out_reset_reset'):
            raise ValueError('peripheral bus/PIO reset owner changed: '+name)
    # The remaining peripheral controller serves synchronous interconnect reset.
    # It must not create another asynchronous device reset-release chain.
    p,_=instance(periph,'altera_reset_controller','rst_controller')
    if (p.get('OUTPUT_RESET_SYNC_EDGES')!='"both"' or p.get('RESET_REQUEST_PRESENT')!='0'
        or len(re.findall(r'\baltera_reset_controller\s*#',periph))!=1):
        raise ValueError('independent peripheral asynchronous reset chain returned')
    paths=('ip/qsys_top/rst_in.ip',)
    for path in paths:
        tree=ET.parse(project/path)
        props={n.get('parameterId'):n.findtext('{*}value') for n in tree.findall('.//{*}altera_module_parameters/{*}parameters/{*}parameter')}
        if any(props.get(k)!=v for k,v in dict(ACTIVE_LOW_RESET='1',SYNCHRONOUS_EDGES='deassert',USE_RESET_REQUEST='0',SYNC_RESET='0').items()):
            raise ValueError('qualified reset bridge parameters changed')
    material=project/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_periph_rst_in'
    source=material/'fes_de25_reset_bridge_10/synth/fes_de25_reset_bridge.sv'
    if source.read_bytes()!=(project/'fes_mailbox/fes_de25_reset_bridge.sv').read_bytes():
        raise ValueError('generated device reset bridge differs from installed source')
    wrapper=material/'synth/peripheral_subsys_periph_rst_in.v'
    params,ports=instance(wrapper.read_text(),'fes_de25_reset_bridge','periph_rst_in')
    if params or ports!=dict(clk='clk',in_reset_n='in_reset_n',in_reset_req='in_reset_req',out_reset_n='out_reset_n'):
        raise ValueError('generated device reset bridge overrides or connections changed')
    return dict(scope='qualified-system-reset-bridges-v1',system_hz=100000000,
        peripheral_owner='memory reset combined with its early asynchronous request',memory_request_parameters=parameters,
        peripheral_async_release_chains=0,hardware_qualification=False,
        source_sha256={p:hashlib.sha256((project/p).read_bytes()).hexdigest() for p in
            (*paths,str(source.relative_to(project)),str(wrapper.relative_to(project)),
             'qsys_top/synth/qsys_top.v','peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v')})
