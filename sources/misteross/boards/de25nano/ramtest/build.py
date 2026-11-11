#!/usr/bin/env python3
"""Build a DE25 three-memory diagnostic from frozen FES and Terasic inputs."""
from pathlib import Path
import argparse
import ast
import hashlib
import importlib.util
import json
import os
import re
import subprocess
import tempfile
import zipfile
import xml.etree.ElementTree as ET

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[4]
BOARD='sources/misteross/boards/de25nano/'
LOCAL=('build.py','scan.v','endpoint.v','sdram.v','sdram_endpoint.v',
       'fes_de25_ramtest_hw.tcl','fes_de25_sdram_ramtest_hw.tcl',
       'emif-b.tcl','integrate.tcl','clocks.sdc',
       'generated/fes_de25_ramtest.vh','generated/fes_de25_ramtest.hpp')
VENDOR_PREFIX='Demonstration/FPGA/RTL_LPDDR4_AXI4_Test/'
VENDOR_FILES=('EMIF_Qsys.qsys','golden_top.v','axil_driver_calibration.sv',
    *('ip/EMIF_Qsys/'+n+'.ip' for n in ('EMIF_Qsys_clock_bridge_0',
       'EMIF_Qsys_emif_io96b_lpddr4_0','EMIF_Qsys_emif_ddr4a_0',
       'EMIF_Qsys_iopll_0','EMIF_Qsys_reset_in')))

def sha(data):return hashlib.sha256(data).hexdigest()

def load(path,name):
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

def identity():
    revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    dirty=subprocess.check_output(['git','status','--porcelain','--untracked-files=normal'],cwd=ROOT,text=True)
    return {'base_revision' if dirty else 'fes_revision':revision,
            'source_classification':'working-copy-diagnostic' if dirty else 'committed'}

def capture(revision=None):
    def read(path):
        return (ROOT/path).read_bytes() if revision is None else subprocess.check_output(['git','show',revision+':'+path],cwd=ROOT)
    path=BOARD+'hps/build.py'; source=read(path)
    declarations=[n.value for n in ast.parse(source).body if isinstance(n,ast.Assign)
        and any(isinstance(x,ast.Name) and x.id=='SOURCES' for x in n.targets)]
    if len(declarations)!=1:raise ValueError('HPS source declaration changed')
    names=ast.literal_eval(declarations[0])
    if not isinstance(names,tuple) or any(not isinstance(n,str) or Path(n).is_absolute() or '..' in Path(n).parts for n in names):
        raise ValueError('unsafe HPS source closure')
    paths={BOARD+'hps/'+n for n in names}|{BOARD+'build.py'}|{BOARD+'ramtest/'+n for n in LOCAL}
    return {p:read(p) for p in sorted(paths)}

def replace_once(source,before,after):
    if source.count(before)!=1:raise ValueError('RAM tester patch anchor changed: '+before[:100])
    return source.replace(before,after)

def constant(header,name):
    values=re.findall(r'^`define FES_DE25_RAMTEST_'+name+r" 32'h([0-9a-f]{8})$",header,re.M)
    if len(values)!=1:raise ValueError('missing/duplicate RAM-test constant: '+name)
    return int(values[0],16)

def prepare(archive,output=None,revision=None):
    sources=capture(revision)
    selected={'fes_revision':revision,'source_classification':'committed'} if revision else identity()
    if revision is None:
        selected={'base_revision':selected.get('fes_revision',selected.get('base_revision')),
                  'source_classification':'working-copy-diagnostic'}
    if output is None:
        parent=ROOT/'out/de25-ramtest';parent.mkdir(parents=True,exist_ok=True)
        output=Path(tempfile.mkdtemp(prefix='run-',dir=parent))
    else:output=Path(output).resolve();output.mkdir(parents=True,exist_ok=False)
    snapshot=output/'functional-source'
    for name,data in sources.items():
        p=snapshot/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)
    hps=load(snapshot/BOARD/'hps/build.py','ramtest_retained_hps')
    hps.source_identity=lambda:selected.copy()
    archive=Path(archive)
    if not archive.is_file() or archive.is_symlink():raise ValueError('vendor ZIP must be a regular file')
    data=archive.read_bytes()
    if sha(data)!=hps.lock()['archive']['sha256']:raise ValueError('vendor archive differs from lock')
    retained=output/'vendor.zip';retained.write_bytes(data)
    directory=hps.prepare_project(retained,output/'hps');project=directory/'project'
    with zipfile.ZipFile(retained) as z:
        if any(z.namelist().count(VENDOR_PREFIX+n)!=1 for n in VENDOR_FILES):raise ValueError('vendor RAM reference changed')
        vendor={n:z.read(VENDOR_PREFIX+n) for n in VENDOR_FILES}
    emif=project/'ramtest_b';emif.mkdir()
    metadata={}
    for name,data in vendor.items():
        if name in ('golden_top.v','axil_driver_calibration.sv'):continue
        if name.endswith(('.ip','.qsys')):
            count=data.count(b'A5EB013BB23BE4SR1')
            if count==0:raise ValueError('vendor SCS correction anchor changed: '+name)
            data=data.replace(b'A5EB013BB23BE4SR1',b'A5EB013BB23BE4SCS')
            metadata[name]={'r1_to_scs_occurrences':count,'derived_sha256':sha(data)}
        p=emif/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)
    local=project/'fes_mailbox'
    for name in LOCAL:
        if name in ('build.py','emif-b.tcl','integrate.tcl'):continue
        p=local/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(sources[BOARD+'ramtest/'+name])
    (local/'axil_driver_calibration.sv').write_bytes(vendor['axil_driver_calibration.sv'])
    header=sources[BOARD+'ramtest/generated/fes_de25_ramtest.vh'].decode()
    fingerprint=sha(json.dumps({'source_sha256':{n:sha(d) for n,d in sources.items()},
        'archive_sha256':sha(retained.read_bytes()),'hps_fpga_hz':100000000,'sdram_hz':50000000},sort_keys=True).encode())[:32]
    compose(project,emif,local,hps,sources,vendor,header,fingerprint)
    inputs=json.loads((directory/'inputs.json').read_text());inputs.pop('base_revision',None);inputs.update(selected)
    inputs['functional_snapshot_sha256']={n:sha(d) for n,d in sources.items()}
    inputs['ramtest']={'build_id':fingerprint,'vendor_reference_sha256':{n:sha(d) for n,d in vendor.items()},
        'device_metadata_repairs':metadata,'hps_fpga_hz':100000000,'sdram_hz':50000000,
        'physical_mmio':'not-established','runtime_admission':'closed','hps_memory_requires_kernel_grant':True,
        'hps_destructive_enabled':False,
        'source_files_sha256':{n:sha((local/n).read_bytes()) for n in LOCAL if (local/n).is_file()},
        'prepared_qsf_sha256':sha((project/'golden_top.qsf').read_bytes()),
        'prepared_top_sha256':sha((project/'golden_top.v').read_bytes())}
    b_ip=emif_b_ip(emif/'EMIF_Qsys.qsys')
    inputs['ramtest']['emif_b_source']=str((emif/b_ip).relative_to(project))
    inputs['ramtest']['emif_b_physical_parameters']={n:v for n,v in hps.module_parameters(emif/b_ip).items()
        if n.startswith(('MEM_','PHY_'))}
    (directory/'inputs.json').write_text(json.dumps(inputs,indent=2,sort_keys=True)+'\n')
    return output,hps

def emif_b_ip(system):
    ns={'a':'http://www.altera.com/XMLSchema/IPXact2014/extensions',
        'i':'http://www.accellera.org/XMLSchema/IPXACT/1685-2014'}
    tree=ET.parse(system)
    selected=[n for n in tree.findall('.//a:module',ns)
        if n.findtext('a:entity_info/i:library',namespaces=ns)=='emif_lpddr4b']
    if len(selected)!=1:raise ValueError('missing/duplicate vendor FPGA DDR controller')
    values=selected[0].findall('.//i:parameter[@parameterId="logicalView"]/i:value',ns)
    if len(values)!=1:raise ValueError('FPGA DDR source identity changed')
    path=Path(values[0].text)
    if path.is_absolute() or '..' in path.parts:raise ValueError('unsafe FPGA DDR source path')
    return path

def compose(project,emif,local,hps,sources,vendor,header,fingerprint):
    emif_script=sources[BOARD+'ramtest/emif-b.tcl'].decode()
    emif_script=emif_script.replace('@EMIF_SOURCE@',hps.tcl_word(emif/'EMIF_Qsys.qsys'))
    emif_script=emif_script.replace('@EMIF_SYSTEM@',hps.tcl_word(emif/'ramtest_b.qsys'))
    script=(project/'integrate-mailbox.tcl').read_text()
    extra=sources[BOARD+'ramtest/integrate.tcl'].decode()
    for name in ('HPS','FPGA','SDRAM'):
        extra=extra.replace('@'+name+'_BASE@',hex(constant(header,name+'_OFFSET')))
    script=replace_once(script,'save_system $peripheral_system\n\n# Refresh',extra+'\nsave_system $peripheral_system\n\n# Refresh')
    exports='''
add_instance ramtest_b ramtest_b 0.0
add_instance fes_ramtest_control50 altera_clock_bridge 19.2.0
set_instance_parameter_value fes_ramtest_control50 EXPLICIT_CLOCK_RATE 50000000
add_interface fes_ramtest_control50 clock sink
set_interface_property fes_ramtest_control50 EXPORT_OF fes_ramtest_control50.in_clk
add_connection clk_100.out_clk ramtest_b.fabric100
add_connection fes_ramtest_control50.out_clk ramtest_b.control50
add_connection fes_ramtest_control50.out_clk subsys_periph.fes_ramtest_clk50
add_connection rst_in.out_reset ramtest_b.reset
add_connection rst_in.out_reset ramtest_b.emif_lpddr4b_s0_axi4lite_reset_n
add_connection subsys_periph.fes_ramtest_hps_mem subsys_hps.f2sdram_adapter_axi4_sub
set_connection_parameter_value subsys_periph.fes_ramtest_hps_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0
add_connection subsys_periph.fes_ramtest_fpga_mem ramtest_b.emif_lpddr4b_s0_axi4
set_connection_parameter_value subsys_periph.fes_ramtest_fpga_mem/ramtest_b.emif_lpddr4b_s0_axi4 baseAddress 0x0
# Existing JTAG master gains control-only access; no new raw DDR master.
add_connection subsys_debug.fpga_m_master subsys_periph.pb_cpu_0_s0
set_connection_parameter_value subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 baseAddress 0x0
'''
    for name,kind in [('mem','conduit end'),('mem_ck','conduit end'),('mem_reset_n','conduit end'),('oct','conduit end'),('ref_clk','clock sink')]:
        exports+=f'add_interface fes_ramtest_b_{name} {kind}\nset_interface_property fes_ramtest_b_{name} EXPORT_OF ramtest_b.emif_lpddr4b_{name}\n'
    for name,internal,kind in [('fes_ramtest_b_control','ramtest_b.emif_lpddr4b_s0_axi4lite','axi4lite end'),
        ('fes_ramtest_b_ready','ramtest_b.emif_lpddr4b_s0_axi4_ctrl_ready','reset source'),
        ('fes_ramtest_dram','subsys_periph.fes_ramtest_dram','conduit end'),
        ('fes_ramtest_hps_calibration','subsys_periph.fes_ramtest_hps_calibration','conduit end'),
        ('fes_ramtest_fpga_calibration','subsys_periph.fes_ramtest_fpga_calibration','conduit end')]:
        exports+=f'add_interface {name} {kind}\nset_interface_property {name} EXPORT_OF {internal}\n'
    anchor='save_system '+hps.tcl_word(project/'qsys_top.qsys')+'\n'
    script=replace_once(script,anchor,exports+anchor)
    script=replace_once(script,'subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 baseAddress 0x0',
        'subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 baseAddress '+hex(constant(header,'JTAG_BRIDGE_BASE')))
    # Catalog discovery happens at qsys-script startup. Save the new B system
    # in a separate invocation before the root design instantiates it.
    (project/'prepare-emif-b.tcl').write_text(emif_script)
    (project/'integrate-mailbox.tcl').write_text(script)
    compose_top(project,hps,vendor)
    qsf_source=(project/'golden_top.qsf').read_text()
    (project/'golden_top.qsf').write_text(replace_once(qsf_source,
        'set_global_assignment -name IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox"',
        'set_global_assignment -name IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox;ramtest_b;ramtest_b/**/*"'))
    with (project/'golden_top.qsf').open('a') as qsf:
        qsf.write('\n# Three-memory DE25 diagnostic; runtime admission remains closed.\n'
            'set_global_assignment -name SEED 1\n'
            f'set_global_assignment -name VERILOG_MACRO "FES_DE25_RAMTEST_BUILD_ID=128\'h{fingerprint}"\n'
            'set_global_assignment -name QSYS_FILE ramtest_b/ramtest_b.qsys\n'
            'set_global_assignment -name SYSTEMVERILOG_FILE fes_mailbox/axil_driver_calibration.sv\n'
            'set_global_assignment -name SEARCH_PATH fes_mailbox\n'
            'set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n')
        # Existing generic instances retain logical-view IP files. Unlike newly
        # added instances, Platform Designer does not register these in QSF.
        for ip in (emif_b_ip(emif/'EMIF_Qsys.qsys'),Path('ip/EMIF_Qsys/EMIF_Qsys_reset_in.ip')):
            if not (emif/ip).is_file():raise ValueError('missing selected vendor DDR IP: '+str(ip))
            qsf.write('set_global_assignment -name IP_FILE '+str((emif/ip).relative_to(project))+'\n')

def compose_top(project,hps,vendor):
    top=(project/'golden_top.v').read_text()
    top=replace_once(top,'//`define ENABLE_LPDDR4B','`define ENABLE_LPDDR4B')
    top=replace_once(top,'.ninit_done_ninit_done                     (ninit_done),',
        ".ninit_done_ninit_done                     (ninit_done),\n"
        ".h2f_warm_reset_handshake_reset_req (),\n.h2f_warm_reset_handshake_reset_ack (1'b1),")
    declarations='wire fes_ram_cal_done, fes_ram_b_ready, fes_ram_cal_reset_n;\nwire [1:0] fes_ram_dqm;\n'
    ports=['.fes_ramtest_control50_clk(CLOCK1_50)',
        # HPS transport exists for grant testing in RTL, but this bitstream has
        # no kernel-owned grant binding or proven release fence. Keep its
        # destructive engine disabled even if raw diagnostic CSRs are written.
        '.fes_ramtest_hps_calibration_calibrated(1\'b0)',
        '.fes_ramtest_hps_calibration_ready(1\'b0)',
        '.fes_ramtest_fpga_calibration_calibrated(fes_ram_cal_done)',
        '.fes_ramtest_fpga_calibration_ready(fes_ram_b_ready)',
        '.fes_ramtest_b_ready_reset_n(fes_ram_b_ready)']
    for role,pin in [('clk','CLK'),('cke','CKE'),('addr','ADDR'),('ba','BA'),
        ('cs_n','CS_n'),('ras_n','RAS_n'),('cas_n','CAS_n'),('we_n','WE_n'),('dq','DQ')]:
        ports.append(f'.fes_ramtest_dram_{role}(DRAM_{pin})')
    ports.append('.fes_ramtest_dram_dqm(fes_ram_dqm)')
    vendor_top=vendor['golden_top.v'].decode()
    memory_ports=re.findall(r'\.((?:emif_lpddr4b_(?:mem|oct|ref_clk))[A-Za-z0-9_]+)\s*\(\s*(LPDDR4B_\w+)\s*\)',vendor_top)
    if len(memory_ports)!=12:raise ValueError('vendor FPGA DDR pin/export map changed: '+str(len(memory_ports)))
    ports.extend('.'+name.replace('emif_lpddr4b','fes_ramtest_b',1)+'('+pin+')' for name,pin in memory_ports)
    driver=[]
    fields=[('araddr',27),('arprot',3),('arvalid',1),('arready',1),('rdata',32),('rresp',2),('rvalid',1),('rready',1),
        ('awaddr',27),('awprot',3),('awvalid',1),('awready',1),('wdata',32),('wstrb',4),('wvalid',1),('wready',1),
        ('bresp',2),('bvalid',1),('bready',1)]
    for name,width in fields:
        signal='fes_ram_cal_'+name
        declarations+=f'wire {"["+str(width-1)+":0] " if width>1 else ""}{signal};\n'
        ports.append(f'.fes_ramtest_b_control_{name}({signal})')
        driver.append(f'.axil_driver_{name}({signal})')
    top=replace_once(top,'qsys_top soc_inst (',declarations+'qsys_top soc_inst (\n'+',\n'.join(ports)+',')
    helper='''
assign {DRAM_UDQM,DRAM_LDQM}=fes_ram_dqm;
fes_de25_top_reset fes_ram_cal_reset(.clk(CLOCK1_50),.ninit_done(ninit_done),
    .pll_locked(1'b1),.reset_n(fes_ram_cal_reset_n));
axil_driver_calibration #(.AXIL_DRIVER_ADDRESS_WIDTH(27)) fes_ram_calibration (
    .axil_driver_clk(CLOCK1_50),.axil_driver_rst_n(fes_ram_cal_reset_n),
    .cal_done_rst_n(fes_ram_cal_done),
'''+',\n'.join(driver)+');\nendmodule\n'
    top=replace_once(top,'endmodule',helper)
    (project/'golden_top.v').write_text(top)

def generate(output,hps,tool,version):
    directory=output/'hps';project=directory/'project'
    (directory/'quartus-version.txt').write_text(version+'\n')
    qsys=tool.parents[2]/'qsys/bin/qsys-script'
    for script,log in [('prepare-emif-b.tcl','emif-b-integration.log'),
                       ('integrate-mailbox.tcl','integration.log')]:
        hps.run_logged([qsys,'--quartus-project=golden_top',
            '--search-path=custom_ip/**/*,fes_mailbox,ramtest_b,ramtest_b/**/*,$','--script='+script],
            project,directory/log)
        if re.search(r'\bError:',(directory/log).read_text()):
            raise ValueError('Platform Designer reported errors; retained '+str(directory/log))
    generator=tool.with_name('quartus_ipgenerate')
    hps.run_logged([generator,'--clear_ip_generation_dirs','golden_top'],project,directory/'clear-generation.log')
    hps.run_logged([generator,'--generate_project_ip_files','--synthesis=verilog','golden_top'],project,directory/'ip-generation.log')
    inputs=json.loads((directory/'inputs.json').read_text())
    changed={name for name,expected in inputs['vendor_hps_emif_parameters'].items()
             if hps.module_parameters(project/name)!=expected}
    if changed:raise ValueError('changed preserved HPS/EMIF parameters: '+str(changed))
    vendor=json.loads((directory/'vendor-inputs.json').read_text())
    for name,expected in vendor['source_sha256'].items():
        if name.startswith('software/') and hps.digest(project/name)!=expected:raise ValueError('changed vendor boot data: '+name)
    record=hps.verify_mailbox_handoff(project);record['ddr_probe']=hps.verify_probe_handoff(project)
    # Detailed memory routes and clock/geometry review are bound before compile.
    record['ramtest']=verify_generated(project,hps)
    (directory/'generation.json').write_text(json.dumps(record,indent=2,sort_keys=True)+'\n')

def verify_generated(project,hps):
    inputs=json.loads((project.parent/'inputs.json').read_text())
    expected=inputs['ramtest']['emif_b_physical_parameters']
    actual=hps.module_parameters(project/inputs['ramtest']['emif_b_source'])
    if not expected or any(actual.get(k)!=v for k,v in expected.items()):
        raise ValueError('generation changed FPGA DDR memory/PHY parameters')
    tree=ET.parse(project/'qsys_top/qsys_top.sopcinfo')
    modules={m.get('path'):m for m in tree.findall('module')}
    connections={(c.get('kind'),c.get('start'),c.get('end')):c for c in tree.findall('connection')}
    def route(kind,start,end,base=None,rate=None):
        c=connections.get((kind,start,end))
        if c is None:raise ValueError('RAM tester route missing: '+start+' -> '+end)
        if base is not None and int(c.findtext("parameter[@name='baseAddress']/value"),0)!=base:
            raise ValueError('RAM tester route base changed: '+start)
        if rate is not None and c.findtext("parameter[@name='clockRateSysInfo']/value")!=str(rate):
            raise ValueError('RAM tester route clock changed: '+start)
        return c
    definition=(project/'fes_mailbox/generated/fes_de25_ramtest.vh').read_text()
    endpoints={}
    for channel,kind,rate in [('hps','fes_de25_ramtest',100000000),
        ('fpga','fes_de25_ramtest',100000000),('sdram','fes_de25_sdram_ramtest',50000000)]:
        path='subsys_periph.fes_ramtest_'+channel
        module=modules.get(path)
        if module is None or module.get('kind')!=kind:raise ValueError('missing RAM tester: '+path)
        ports=hps.component_ports(module,'avs')
        if ports.get('address')!=('Input',8) or ports.get('writedata')!=('Input',32) or ports.get('response')!=('Output',2):
            raise ValueError('RAM tester control width/response changed: '+path)
        route('avalon','subsys_periph_pb_cpu_0.m0',path.replace('.','_')+'.avs',
              base=constant(definition,channel.upper()+'_OFFSET'))
        route('reset','subsys_periph_periph_rst_in.out_reset',path.replace('.','_')+'.reset')
        destination=path.replace('.','_')+'.clk'
        clocks=[c for c in tree.findall('connection') if c.get('kind')=='clock' and c.get('end')==destination]
        if len(clocks)!=1 or clocks[0].findtext("parameter[@name='clockRateSysInfo']/value")!=str(rate):
            raise ValueError('RAM tester clock differs: '+path)
        endpoints[channel]={'instance':path,'clock_hz':rate,'peripheral_offset':constant(definition,channel.upper()+'_OFFSET')}
        if channel!='sdram':
            expected_ports={'address':('Output',32),'burstcount':('Output',5),
                'read':('Output',1),'write':('Output',1),'writedata':('Output',128),
                'byteenable':('Output',16),'waitrequest':('Input',1),
                'readdata':('Input',128),'readdatavalid':('Input',1),
                'writeresponsevalid':('Input',1),'response':('Input',2)}
            if hps.component_ports(module,'avm')!=expected_ports:
                raise ValueError('RAM tester memory master/response differs: '+channel)
    route('avalon','subsys_periph_fes_ramtest_hps.avm','subsys_hps_f2sdram_adapter.axi4_sub',base=0)
    route('avalon','subsys_hps_f2sdram_adapter.axi4_man','subsys_hps_agilex_hps.f2sdram',base=0)
    b_route=route('avalon','subsys_periph_fes_ramtest_fpga.avm','ramtest_b_emif_lpddr4b.s0_axi4',base=0)
    for path,interface,address_bits in [('subsys_hps.f2sdram_adapter','axi4_sub',32),('ramtest_b.emif_lpddr4b','s0_axi4',30)]:
        m=modules.get(path)
        if m is None:raise ValueError('RAM tester memory controller missing: '+path)
        ports=hps.component_ports(m,interface)
        for name,direction,width in [('araddr','Input',address_bits),('awaddr','Input',address_bits),
            ('wdata','Input',256),('rdata','Output',256),('rresp','Output',2),('bresp','Output',2)]:
            if ports.get(name)!=(direction,width):raise ValueError('RAM tester controller AXI contract differs: '+path+'.'+name)
    b_map=ET.fromstring(b_route.findtext("parameter[@name='addressMapSysInfo']/value"))
    b_slaves=[n for n in b_map.findall('slave')]
    if len(b_slaves)!=1 or b_slaves[0].get('name')!='ramtest_b.emif_lpddr4b.s0_axi4' or \
        int(b_slaves[0].get('start'),0)!=0 or int(b_slaves[0].get('end'),0)!=constant(definition,'LPDDR4_BYTES'):
        raise ValueError('RAM tester FPGA DDR aperture differs')
    route('clock','ramtest_b_fabric100.out_clk','ramtest_b_emif_lpddr4b.s0_axi4_clock_in',rate=100000000)
    route('clock','ramtest_b_control50.out_clk','ramtest_b_emif_lpddr4b.s0_axi4lite_clock',rate=50000000)
    route('reset','rst_in.out_reset','ramtest_b_emif_lpddr4b.s0_axi4lite_reset_n')
    jtag=route('avalon','subsys_debug_fpga_m.master','subsys_periph_pb_cpu_0.s0',
               base=constant(definition,'JTAG_BRIDGE_BASE'))
    # The SDRAM's custom controller is an internal subordinate; its physical
    # two-chip pin interface, rather than the DE10 addon map, is the boundary.
    dram=hps.component_ports(modules['subsys_periph.fes_ramtest_sdram'],'dram')
    expected_dram={name:('Output',width) for name,width in
        [('clk',1),('cke',1),('addr',13),('ba',2),('cs_n',2),('ras_n',1),('cas_n',1),('we_n',1),('dqm',2)]}
    expected_dram['dq']=('Bidir',16)
    if dram!=expected_dram:raise ValueError('RAM tester SDRAM pin geometry differs')
    lw=[c for c in tree.findall('connection') if c.get('start')=='subsys_hps_agilex_hps.lwhps2fpga' and c.get('end')=='subsys_periph_pb_cpu_0.s0']
    if len(lw)!=1:raise ValueError('missing preserved HPS lightweight bridge')
    regions=hps.peripheral_slave_map(lw[0].findtext("parameter[@name='addressMapSysInfo']/value"),hps.probe_base())
    for channel,info in endpoints.items():
        found=[r for r in regions if r['name']==info['instance']+'.avs']
        if len(found)!=1 or found[0]['base']!=info['peripheral_offset'] or found[0]['span']!=256:
            raise ValueError('RAM tester base/span differs: '+channel)
    jtag_map=hps.peripheral_slave_map(jtag.findtext("parameter[@name='addressMapSysInfo']/value"),
        hps.probe_base()+constant(definition,'JTAG_BRIDGE_BASE'))
    for channel,info in endpoints.items():
        found=[r for r in jtag_map if r['name']==info['instance']+'.avs']
        if len(found)!=1 or found[0]['base']!=info['peripheral_offset']+constant(definition,'JTAG_BRIDGE_BASE') or found[0]['span']!=256:
            raise ValueError('RAM tester JTAG map differs: '+channel)
    for name,expected in inputs['ramtest']['source_files_sha256'].items():
        if hps.digest(project/'fes_mailbox'/name)!=expected:raise ValueError('RAM tester source changed during generation: '+name)
    if hps.digest(project/'golden_top.v')!=inputs['ramtest']['prepared_top_sha256']:
        raise ValueError('RAM tester physical top changed during generation')
    return {'endpoints':endpoints,'peripheral_slave_map':regions,
        'jtag_slave_map':jtag_map,'memory_master_data_bits':128,'controller_data_bits':256,
        'emif_b_preserved_physical_parameters':len(inputs['ramtest']['emif_b_physical_parameters']),
        'physical_acceptance':'not-run'}

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=('project','generate','build'))
    parser.add_argument('--vendor-archive',type=Path,required=True)
    parser.add_argument('--quartus-root',default=os.environ.get('QUARTUS_ROOTDIR',''))
    parser.add_argument('--output',type=Path)
    args=parser.parse_args(argv)
    try:
        selected=identity();revision=selected.get('fes_revision') if args.action!='project' else None
        if args.action!='project' and not revision:raise ValueError('generation/build requires clean committed sources')
        output,hps=prepare(args.vendor_archive,args.output,revision)
        print('RAM tester diagnostic: '+str(output),flush=True)
        if args.action!='project':
            if identity()!=selected:raise ValueError('source changed before generation')
            tool,version=hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(output,hps,tool,version)
            if args.action=='build':print('Diagnostic SOF: '+str(hps.compile_project(output/'hps',tool)),flush=True)
    except (OSError,ValueError,KeyError,zipfile.BadZipFile,subprocess.CalledProcessError) as error:
        parser.exit(1,'de25-ramtest: '+str(error)+'\n')

if __name__=='__main__':main()
