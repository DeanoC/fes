"""Bind the opt-in menu to the reviewed board's pixel clock/reset and HDMI."""
import hashlib
import importlib.util
from pathlib import Path
import xml.etree.ElementTree as ET
import reset_domains

BOARD_REVISION='79d616e85e19d65f0d645b67a85ffe7e142dcdfc'
PREFIX='sources/misteross/prototypes/de25_board_control/'
PIPELINE='sources/misteross/prototypes/embedded_control/'
VIDEO='sources/misteross/cores/fes-menu/rtl/'
FILES=('fes_de25_menu.sv','fes_de25_menu_cdc.sv','fes_de25_menu_hw.tcl','menu_clocks.sdc','menu_transport.py')
INPUTS=tuple(PREFIX+n for n in FILES)+tuple(PIPELINE+n for n in
    ('embedded_menu_top.sv','fes_embedded_menu_control.sv','fes_embedded_menu_scanout.sv'))+tuple(VIDEO+n for n in ('fes_menu_video.v','fes_menu_reader.v'))
PERIPHERAL='''# Menu: CPU control stays at 100 MHz; only scanout/AXI use the pixel clock.
add_connection fes_computer_pixel_clk.out_clk fes_embedded.pixel_clk
add_interface fes_menu_pixel_reset reset sink
set_interface_property fes_menu_pixel_reset EXPORT_OF fes_embedded.pixel_reset
remove_interface fes_computer_video
add_interface fes_computer_video conduit end
set_interface_property fes_computer_video EXPORT_OF fes_embedded.video
'''
PARENT='''# Explicit parent clock/reset bridges let generated interconnect use the pixel owner.
remove_interface fes_computer_pixel_clk
add_instance fes_menu_pixel_clk altera_clock_bridge 19.2.0
set_instance_parameter_value fes_menu_pixel_clk EXPLICIT_CLOCK_RATE 74250000
add_connection fes_menu_pixel_clk.out_clk subsys_periph.fes_computer_pixel_clk
add_interface fes_computer_pixel_clk clock sink
set_interface_property fes_computer_pixel_clk EXPORT_OF fes_menu_pixel_clk.in_clk
add_instance fes_menu_reset altera_reset_bridge 19.2.0
set_instance_parameter_value fes_menu_reset ACTIVE_LOW_RESET 1
set_instance_parameter_value fes_menu_reset SYNCHRONOUS_EDGES deassert
set_instance_parameter_value fes_menu_reset USE_RESET_REQUEST 0
set_instance_parameter_value fes_menu_reset SYNC_RESET 0
add_connection fes_menu_pixel_clk.out_clk fes_menu_reset.clk
add_connection fes_menu_reset.out_reset subsys_periph.fes_menu_pixel_reset
add_interface fes_menu_pixel_reset reset sink
set_interface_property fes_menu_pixel_reset EXPORT_OF fes_menu_reset.in_reset
'''

def enabled(inputs):return inputs.get('reader_memory',{}).get('backend')=='hps-ddr-menu'

def parameters(inputs):
    if not enabled(inputs):return {}
    if (inputs.get('board_fes_revision')!=BOARD_REVISION or inputs.get('computer',{}).get('hps_initialization')!='HPS FIRST' or
        inputs.get('reader_memory')!={'backend':'hps-ddr-menu','bus_base':0x95000000,'bytes':8<<20} or inputs.get('rom_transport')):
        raise ValueError('menu requires its reviewed HPS-first board and complete two-slot layout')
    return dict(menu_transport='bounded-cpu-pixel-csr-v1',menu_system_hz=100000000,menu_pixel_hz=74250000,
        menu_csr_timeout_cycles=1048576,menu_slots=2,menu_slot_bytes=4<<20)

def _replace(text,old,new):
    if text.count(old)!=1:raise ValueError('menu integration anchor changed')
    return text.replace(old,new)

def reader(original):
    before=b'(* ramstyle = "M10K" *)'
    if original.count(before)!=1:raise ValueError('menu FIFO derivation changed')
    return original.replace(before,b'(* ramstyle = "M20K" *)')

def install(project,inputs,sources,script):
    if not enabled(inputs):return script
    parameters(inputs);project=Path(project);local=project/'fes_mailbox'
    if sources[PREFIX+'fes_de25_menu.sv'].count(b'parameter integer CSR_TIMEOUT_CYCLES = 1048576')!=1:
        raise ValueError('menu CPU timeout differs from compiled contract')
    for name in FILES:
        if name.endswith(('.sv','.tcl')):(local/name).write_bytes(sources[PREFIX+name])
    for name in ('embedded_menu_top.sv','fes_embedded_menu_control.sv','fes_embedded_menu_scanout.sv'):
        (local/name).write_bytes(sources[PIPELINE+name])
    (local/'fes_menu_video.v').write_bytes(sources[VIDEO+'fes_menu_video.v'])
    (local/'fes_menu_reader.v').write_bytes(reader(sources[VIDEO+'fes_menu_reader.v']))
    (local/'clocks.sdc').write_bytes(sources[PREFIX+'menu_clocks.sdc'])
    inputs['computer']['source_sha256']['clocks.sdc']=hashlib.sha256(sources[PREFIX+'menu_clocks.sdc']).hexdigest()
    script=_replace(script,'add_instance fes_embedded fes_de25_embedded 1.0\n','add_instance fes_embedded fes_de25_menu 1.0\n')
    script=_replace(script,'save_system $peripheral_system\n',PERIPHERAL+'save_system $peripheral_system\n')
    ends=[line for line in script.splitlines(keepends=True) if line.startswith('save_system ') and str(project/'qsys_top.qsys') in line]
    if len(ends)!=1:raise ValueError('menu top save anchor changed')
    script=_replace(script,ends[0],PARENT+ends[0])
    path=project/'golden_top.v';top=path.read_text()
    top=_replace(top,'qsys_top soc_inst (','wire menu_pixel_reset_n;\nqsys_top soc_inst (\n .fes_menu_pixel_reset_reset_n(menu_pixel_reset_n),')
    top=_replace(top,'.pixel_clk(computer_pixel_clk),','.pixel_clk(computer_pixel_clk), .pixel_reset_n(menu_pixel_reset_n),')
    path.write_text(top)
    return script

def verify(project,inputs,sources):
    project=Path(project);script=(project/'integrate-mailbox.tcl').read_text()
    if not enabled(inputs):
        if PERIPHERAL in script or PARENT in script:raise ValueError('unselected menu transport')
        return None
    expected=parameters(inputs);local=project/'fes_mailbox'
    for name in FILES:
        if name.endswith(('.sv','.tcl')) and (local/name).read_bytes()!=sources[PREFIX+name]:raise ValueError('menu installed source differs: '+name)
    for name in ('embedded_menu_top.sv','fes_embedded_menu_control.sv','fes_embedded_menu_scanout.sv'):
        if (local/name).read_bytes()!=sources[PIPELINE+name]:raise ValueError('menu pipeline source differs')
    for name,raw in [('fes_menu_video.v',sources[VIDEO+'fes_menu_video.v']),('fes_menu_reader.v',reader(sources[VIDEO+'fes_menu_reader.v'])),('clocks.sdc',sources[PREFIX+'menu_clocks.sdc'])]:
        if (local/name).read_bytes()!=raw:raise ValueError('menu video/constraints source differs')
    if script.count(PERIPHERAL)!=1 or script.count(PARENT)!=1 or script.count('add_instance fes_embedded fes_de25_menu 1.0\n')!=1:
        raise ValueError('menu clock/reset/video routing changed')
    top=(project/'golden_top.v').read_text()
    if (top.count('.fes_menu_pixel_reset_reset_n(menu_pixel_reset_n)')!=1 or
        top.count('.pixel_reset_n(menu_pixel_reset_n)')!=1 or
        top.count('.fes_computer_pixel_clk_clk(computer_pixel_clk)')!=1):raise ValueError('menu clock/reset top ports changed')
    if inputs['computer']['source_sha256'].get('clocks.sdc')!=hashlib.sha256(sources[PREFIX+'menu_clocks.sdc']).hexdigest():
        raise ValueError('menu constraints differ from source identity')
    return expected

def verify_generated(project,inputs,hps=None):
    if not enabled(inputs):return None
    if hps is None:
        spec=importlib.util.spec_from_file_location('menu_hps_handoff',Path(__file__).resolve().parents[2]/'boards/de25nano/hps/build.py')
        hps=importlib.util.module_from_spec(spec);spec.loader.exec_module(hps)
    project=Path(project);parameters(inputs)
    tree=ET.parse(project/'qsys_top/qsys_top.sopcinfo')
    modules={m.get('path'):m for m in tree.findall('module')}
    menu=modules['subsys_periph.fes_embedded']
    if menu.get('kind')!='fes_de25_menu':raise ValueError('generated menu component changed')
    routes=[c for c in tree.findall('connection') if (c.get('kind'),c.get('start'),c.get('end'))==
        ('clock','subsys_periph_fes_computer_pixel_clk.out_clk','subsys_periph_fes_embedded.pixel_clk')]
    if len(routes)!=1 or routes[0].findtext("parameter[@name='clockRateSysInfo']/value")!='74250000':
        raise ValueError('generated menu pixel clock changed')
    if hps.component_ports(menu,'video')!={'rgb':('Output',24),'de':('Output',1),'hs':('Output',1),'vs':('Output',1)}:
        raise ValueError('generated menu video interface changed')
    top=(project/'qsys_top/synth/qsys_top.v').read_text()
    periph=(project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v').read_text()
    _,child=reset_domains.instance(top,'peripheral_subsys','subsys_periph')
    _,endpoint=reset_domains.instance(periph,'peripheral_subsys_fes_embedded','fes_embedded')
    for signal in ('rgb','de','hs','vs'):
        name='fes_computer_video_'+signal
        if child.get(name)!=name or endpoint.get('video_'+signal)!=name:
            raise ValueError('generated menu HDMI source is not its pixel output')
    reset='fes_menu_pixel_reset_reset_n'
    if child.get(reset)!='fes_menu_reset_out_reset_reset' or endpoint.get('pixel_reset_n')!=reset or endpoint.get('pixel_clk')!='fes_computer_pixel_clk_out_clk_clk':
        raise ValueError('generated menu pixel reset/clock export changed')
    _,clock=reset_domains.instance(top,'qsys_top_fes_menu_pixel_clk','fes_menu_pixel_clk')
    _,reset_owner=reset_domains.instance(top,'qsys_top_fes_menu_reset','fes_menu_reset')
    if (clock!=dict(in_clk='fes_computer_pixel_clk_clk',out_clk='fes_menu_pixel_clk_out_clk_clk') or
        reset_owner!=dict(clk='fes_menu_pixel_clk_out_clk_clk',in_reset_n=reset,out_reset_n='fes_menu_reset_out_reset_reset') or
        child.get('fes_computer_pixel_clk_clk')!='fes_menu_pixel_clk_out_clk_clk'):
        raise ValueError('generated menu parent clock/reset bridge changed')
    bridge_parameters={
        'fes_menu_reset':dict(ACTIVE_LOW_RESET='1',SYNCHRONOUS_EDGES='deassert',NUM_RESET_OUTPUTS='1',
            USE_RESET_REQUEST='0',SYNC_RESET='0',AUTO_CLK_CLOCK_RATE='74250000'),
        'fes_menu_pixel_clk':dict(DERIVED_CLOCK_RATE='0',EXPLICIT_CLOCK_RATE='74250000',NUM_CLOCK_OUTPUTS='1')}
    bridge_files=[]
    for name,expected in bridge_parameters.items():
        path=project/'ip/qsys_top'/('qsys_top_'+name+'.ip');bridge_files.append(path)
        observed={n.get('parameterId'):n.findtext('{*}value') for n in
            ET.parse(path).findall('.//{*}altera_module_parameters/{*}parameters/{*}parameter')}
        if observed!=expected:raise ValueError('generated menu parent bridge parameters changed: '+name)
    return dict(scope='bounded-cpu-pixel-menu-v1',system_hz=100000000,pixel_hz=74250000,
        slots=2,slot_bytes=4<<20,csr_timeout_cycles=1048576,physical_qualification=False,
        generated_sha256={str(p.relative_to(project)):hashlib.sha256(p.read_bytes()).hexdigest() for p in
            (project/'qsys_top/qsys_top.sopcinfo',project/'qsys_top/synth/qsys_top.v',project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v',*bridge_files)})
