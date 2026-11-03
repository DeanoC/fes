"""Derive immutable HPS-first source context without a native SPL hash cycle.

The descriptor binds the sealed FPGA inputs before the native image is built.
SPL and BL31 bind its digest through the existing cold chain. This is a local
source-bound diagnostic assertion, not measured/signed boot or admission.
"""
import hashlib
import importlib.util
import json
import re
from pathlib import Path
import subprocess
import struct
import sys
from types import SimpleNamespace
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
PROTOTYPE = ROOT/'sources/misteross/prototypes/de25_board_control'
SCOPE = 'source-bound-hps-first-context-v1'


def encode(context):
    return (json.dumps(context, sort_keys=True, separators=(',', ':'))+'\n').encode()


def digest(context):
    return hashlib.sha256(encode(context)).hexdigest()


def inputs():
    return [Path(__file__), HERE/'hps_io.py', PROTOTYPE/'verify.py', PROTOTYPE/'seal.py', PROTOTYPE/'reader_memory.py', PROTOTYPE/'computer_model.py', PROTOTYPE/'build.py',
            ROOT/'sources/misteross/boards/de25nano/hps/build.py', ROOT/'sources/misteross/boards/de25nano/build.py',
            PROTOTYPE.parent/'embedded_control/producer.py',
            ROOT/'sources/misteross/scripts/core_package.py',
            ROOT/'sources/misteross/scripts/export_core_package.py',
            ROOT/'sources/misteross/scripts/compiler_read_audit.py',
            ROOT/'sources/misteross/scripts/rfc3986_validator.py',
            ROOT/'sources/misteross/scripts/source_repository.py']


def _worker(project, mode=None):
    # The FPGA producer imports private modules named seal/scripts/verify and
    # adjusts sys.path. Isolate those imports from the firmware builders, whose
    # own verify module must remain authoritative after this call returns.
    result = subprocess.run([sys.executable, str(Path(__file__).resolve()), str(project), *([mode] if mode else [])],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
    if result.returncode:
        raise ValueError('sealed HPS context verification failed: '+result.stderr.decode(errors='replace')[-4096:])
    return json.loads(result.stdout)


def derive(project):
    return _worker(project)


def control(project):
    return _worker(project, 'control')


def hps_io(project):
    return _worker(project, 'hps-io')


def _pinmux_module():
    spec=importlib.util.spec_from_file_location('fes_hps_io_map',HERE/'hps_io.py')
    pinmux=importlib.util.module_from_spec(spec);spec.loader.exec_module(pinmux)
    return pinmux


def validate_hps_io(mapping, context):
    return _pinmux_module().validate(mapping,context)


def _hps_io(project):
    return _pinmux_module().derive(project, _derive(project))


def computer(project):
    return _worker(project, 'computer')


def configuration(project):
    return _worker(project, 'configuration')


def program_platform(project):
    return _worker(project, 'program-platform')


def selected_program(project):
    return _worker(project, 'selected-program')


def reader_memory(project):
    return _worker(project, 'reader-memory')


def ddr_read(project):
    return _worker(project, 'ddr-read')


def _ddr_adapter(project, vendor_sha):
    """Match the complete generated adapter to its sealed vendor template.

    Attribute constants alone are insufficient: an edited generated module
    could contain the right strings and route different values to the HPS.
    Never execute the vendor Tcl to reconstruct this small literal template.
    """
    project=Path(project);hps=project/'board/hps';source=hps/'project'
    archive=project/'board/vendor.zip'
    tcl=source/'custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl'
    generated=source/'hps_subsys/ip/hps_subsys/f2sdram_adapter/f2sdram_adapter_10/synth'
    modules=list(generated.glob('*.v'))
    if len(modules)!=1:raise ValueError('ambiguous generated DDR adapter')
    paths=(archive,tcl,modules[0])
    if any(any(p.is_symlink() for p in (path,*path.parents)) or path.stat().st_size>256<<20 for path in paths):
        raise ValueError('private bounded DDR adapter inputs required')
    if hashlib.sha256(archive.read_bytes()).hexdigest()!=vendor_sha:
        raise ValueError('DDR adapter vendor archive differs from the sealed source')
    with zipfile.ZipFile(archive) as z:
        name='Demonstration/SoC_FPGA/GHRD/custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl'
        if z.namelist().count(name)!=1 or z.getinfo(name).file_size>1<<20:
            raise ValueError('missing or ambiguous vendor DDR adapter template')
        raw=z.read(name)
    if tcl.read_bytes()!=raw:raise ValueError('DDR adapter source differs from the sealed vendor')
    text=raw.decode();begin='set verilog_code {';end='\n}\n\nadd_fileset_file '
    if text.count(begin)!=1 or text.count(end)!=1:
        raise ValueError('unsupported vendor DDR adapter template')
    template=text.split(begin,1)[1].split(end,1)[0]
    name=modules[0].stem
    if not re.fullmatch(r'f2sdram_adapter_f2sdram_adapter_10_[a-z0-9]+',name):
        raise ValueError('unexpected generated DDR adapter module')
    expected=template.replace('${output_name}',name).replace(r'\[','[').replace(r'\]',']')+'\n'
    if modules[0].read_text()!=expected:
        raise ValueError('generated DDR adapter differs from the complete vendor template')
    for name,value in (('man_arcache',"4'h2"),('man_arprot',"3'b011"),('man_aruser',"8'hE0")):
        if re.findall(r'(?m)^assign\s+'+name+r'\s*=\s*([^;]+);',expected)!=[value]:
            raise ValueError('DDR adapter read attributes differ: '+name)
    return {'adapter_source_sha256':hashlib.sha256(raw).hexdigest(),
            'adapter_generated_sha256':hashlib.sha256(expected.encode()).hexdigest(),
            'arcache':2,'arprot':3,'aruser':224}


def _ddr_read(project):
    context=_derive(project);memory=_reader_memory(project);control_map=_control(project)
    if memory is None:raise ValueError('active DDR read requires the sealed DDR diagnostic')
    inputs=json.loads((Path(project)/'board/hps/inputs.json').read_text())
    attributes=_ddr_adapter(project,inputs['vendor_archive_sha256'])
    return dict(format=1,scope='sealed-cold-ddr-read-v1',package_id=context['package_id'],
                build_id=context['build_id'],reader_memory_sha256=digest(memory),
                control_map_sha256=digest(control_map),read_bytes=32,open_mask=8,close_mask=10,**attributes)


def reader_pattern():
    return b''.join(struct.pack('<I', (0x4e414e4f ^ (i * 0x9e3779b9)) & 0xffffffff)
                    for i in range(8192 // 4))


def _reader_memory(project):
    context = _derive(project)
    if context['programming_profile'] == 'de25-embedded-hps-first-diagnostic-v1':
        return None
    parameters = json.loads((Path(project)/'board/hps/build-inputs.json').read_text())['parameters']
    return reader_memory_contract(context, parameters)


def reader_memory_contract(context, parameters):
    """Exact compiled memory layout. This descriptor grants no FPGA access."""
    if context['programming_profile']=='de25-embedded-hps-first-window-diagnostic-v1':
        if (context['control_abi']!=0x10203042 or parameters.get('reader_memory_backend')!='hps-ddr-window' or
                type(parameters.get('reader_memory_base')) is not int or parameters['reader_memory_base']!=0x95000000 or
                type(parameters.get('reader_memory_bytes')) is not int or parameters['reader_memory_bytes']!=4<<20):
            raise ValueError('native window reader requires its exact sealed 4 MiB window')
        return {'format':1,'scope':'sealed-cold-window-reader-memory-v1',
                'package_id':context['package_id'],'build_id':context['build_id'],
                'cpu_base':0x95000000,'bus_base':0x95000000,'bytes':4<<20,
                'arena_base':0x95000000,'arena_bytes':4<<20,
                'cpu_attributes':'normal-uncached','lifetime':'until-reboot',
                'fpga_addressing':'physical-identity-unqualified',
                'row_bytes':5120,'stride':5120,'rows':720,'frame_bytes':3686400,'pixel_format':'xrgb8888'}
    if (context['programming_profile'] != 'de25-embedded-hps-first-ddr-diagnostic-v1' or
            context['control_abi'] != 0x10203041 or
            parameters.get('reader_memory_backend') != 'hps-ddr' or
            type(parameters.get('reader_memory_base')) is not int or parameters['reader_memory_base'] != 0x95000000 or
            type(parameters.get('reader_memory_bytes')) is not int or parameters['reader_memory_bytes'] != 8192):
        raise ValueError('native DDR reader requires the fixed sealed 8192-byte window')
    # This is a requested physical identity path, not evidence that the MPFE TBU
    # is bypassed or configured. CPU mapping and reservation grant no FPGA access.
    return {'format': 1, 'scope': 'sealed-cold-ddr-reader-memory-v1',
            'package_id': context['package_id'], 'build_id': context['build_id'],
            'cpu_base': 0x95000000, 'bus_base': parameters['reader_memory_base'], 'bytes': 8192,
            'arena_base': 0x95000000, 'arena_bytes': 2 << 20,
            'cpu_attributes': 'normal-uncached', 'lifetime': 'until-reboot',
            'fpga_addressing': 'physical-identity-unqualified',
            'pattern': 'le32-xor-nano-golden-v1', 'pattern_sha256': hashlib.sha256(reader_pattern()).hexdigest()}


def validate_reader_memory(context, mapping):
    if not isinstance(mapping,dict):
        raise ValueError('compiled reader memory descriptor required')
    window=context['programming_profile']=='de25-embedded-hps-first-window-diagnostic-v1'
    expected=reader_memory_contract(context,{'reader_memory_backend':'hps-ddr-window' if window else 'hps-ddr',
        'reader_memory_base':mapping.get('bus_base'),'reader_memory_bytes':mapping.get('bytes')})
    if encode(mapping)!=encode(expected):
        raise ValueError('reader memory differs from its compiled geometry and lifetime')
    return expected


def package_archive(project, mapping):
    """Copy only the canonical archive of this exact sealed source."""
    if mapping != configuration(project):
        raise ValueError('configuration package map differs from the sealed source')
    record=_worker(project,'package')
    raw=Path(record['path']).read_bytes()
    if len(raw)!=record['size'] or hashlib.sha256(raw).hexdigest()!=record['sha256']:
        raise ValueError('sealed package archive changed after verification')
    return raw


def _package(project):
    mapping=_configuration(project)
    path=Path(project)/'board/hps/packages'/(mapping['package_id']+'.fcore')
    if any(p.is_symlink() for p in (path,*path.parents)):
        raise ValueError('configuration package has a symlink component')
    import seal
    from scripts import export_core_package
    package=seal.read_package(path)
    raw=path.read_bytes()
    canonical=export_core_package._archive_bytes(package.manifest_bytes,package.payload_bytes,package.rom_map_bytes)
    if (not 0<len(raw)<=8<<20 or raw!=canonical or package.package_id!=mapping['package_id'] or
            len(package.payload_bytes)!=mapping['core_size'] or
            hashlib.sha256(package.payload_bytes).hexdigest()!=mapping['core_sha256']):
        raise ValueError('configuration archive differs from the canonical sealed package')
    return {'path':str(path),'sha256':hashlib.sha256(raw).hexdigest(),'size':len(raw)}


def _configuration(project):
    context=_derive(project)
    path=Path(project)/'board/hps/packages'/context['package_id']/'core.rbf'
    if any(p.is_symlink() for p in (path,*path.parents)):
        raise ValueError('configuration payload has a symlink component')
    # The pinned SDM client advertises 0xffff0 bytes per descriptor. Round
    # down to our coherent arena's 64-byte alignment, keeping every DMA start
    # aligned without changing the source, arena or completion deadline.
    raw=path.read_bytes();chunk=0xfffc0
    if (not raw or len(raw)>(8<<20)-4096 or len(raw)&3 or len(raw)!=context['core_size'] or
            hashlib.sha256(raw).hexdigest()!=context['core_sha256']):
        raise ValueError('bounded word-aligned sealed core required for configuration')
    # Outside Go RAM 84000000..94000000, sealed FIT 94000000..94800000,
    # entry staging at 96080000 and the conservative high-DDR U-Boot envelope.
    return {'format':1,'scope':'sealed-cold-configuration-v1',
            'package_id':context['package_id'],'build_id':context['build_id'],
            'core_sha256':context['core_sha256'],'core_sha384':hashlib.sha384(raw).hexdigest(),
            'core_size':len(raw),'base':0x94800000,'size':8<<20,'dram_base':0x80000000,
            'chunk_bytes':chunk,'chunk_sha384':[hashlib.sha384(raw[i:i+chunk]).hexdigest() for i in range(0,len(raw),chunk)]}


def _derive(project):
    project = Path(project).absolute()
    if (not project.is_relative_to(ROOT/'out') or '..' in project.parts or
            any(p.is_symlink() for p in (project, *project.parents)) or not project.is_dir()):
        raise ValueError('HPS-first project must be an existing private directory below out/')
    sys.path.insert(0, str(PROTOTYPE))
    spec = importlib.util.spec_from_file_location('nano_hps_context_verifier', PROTOTYPE/'verify.py')
    verifier = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(verifier)
    source, _, _ = verifier.inputs_for(project)
    import seal
    if not source['embedded_control'].get('build_inputs_sha256') or seal.boot_order(source) != 'HPS FIRST':
        raise ValueError('HPS context requires a sealed HPS-first project')
    hps = project/'board/hps'
    pair = json.loads((hps/'hps-first-pair.json').read_text())
    record = json.loads((hps/'build-inputs.json').read_text())
    package = seal.read_package(hps/'packages'/pair['package_id'])
    abi = package.fields['abi']
    profile = seal.profile_for(source)
    if package.fields['target']['programming_profile'] != profile:
        raise ValueError('package profile differs from the sealed memory backend')
    return {
        'format': 1, 'scope': SCOPE, 'platform': seal.PLATFORM, 'device': seal.DEVICE,
        'programming_profile': profile, 'core_id': package.fields['core']['id'],
        'abi': abi['id'], 'abi_major': abi['major'], 'abi_minor': abi['minor'],
        'package_id': pair['package_id'], 'build_id': pair['build_id'],
        'build_inputs_sha256': pair['build_inputs_sha256'], 'core_sha256': pair['core_sha256'],
        'core_size': pair['core_size'], 'hps_io_hash': pair['hps_io_hash'],
        'raw_sof_sha256': verifier.sha(hps/'project/output_files/golden_top.sof'),
        'original_pair_sha256': verifier.sha(hps/'hps-first-pair.json'),
        'pfg_identity': pair['pfg_identity'], 'hps_initialization': 'HPS FIRST',
        'overlay_revision': record['revision'], 'board_revision': source['board_fes_revision'],
        'control_abi': source['embedded_control']['control_abi'],
        'peripheral_offset': source['embedded_control']['peripheral_offset'],
        'peripheral_span': source['embedded_control']['span'],
    }


def _control(project):
    context=_derive(project)
    hps=Path(project).absolute()/'board/hps'
    generation=json.loads((hps/'generation.json').read_text())
    build=json.loads((hps/'build.json').read_text())
    name=generation['top_system_handoff']
    if name!='qsys_top/qsys_top.sopcinfo':
        raise ValueError('unexpected top-level FPGA address handoff')
    path=hps/'project'/name
    if any(p.is_symlink() for p in (path,*path.parents)) or path.stat().st_size>64<<20:
        raise ValueError('private bounded FPGA address handoff required')
    raw=path.read_bytes();sha=hashlib.sha256(raw).hexdigest()
    if sha!=generation['sha256'] or sha!=build['generated_handoff_and_sdc_sha256'][name]:
        raise ValueError('FPGA address handoff differs from compiler/generator receipts')
    connections=list(ET.fromstring(raw).iter('connection'))
    def parameters(start,end):
        found=[c for c in connections if c.get('kind')=='avalon' and c.get('start')==start and c.get('end')==end]
        if len(found)!=1:raise ValueError('ambiguous/missing FPGA control route')
        values={}
        for p in found[0].findall('parameter'):
            key=p.get('name')
            if key in values:raise ValueError('duplicate FPGA connection parameter')
            values[key]=(p.findtext('value') or '').strip()
        return values
    lw=parameters('subsys_hps_agilex_hps.lwhps2fpga','subsys_periph_pb_cpu_0.s0')
    endpoint=parameters('subsys_periph_pb_cpu_0.m0','subsys_periph_fes_embedded.avs')
    bridge=int(lw['baseAddress'],0);offset=int(endpoint['baseAddress'],0)
    slaves=list(ET.fromstring(lw['addressMapSysInfo']).iter('slave'))
    matches=[s for s in slaves if s.get('name')=='subsys_periph.fes_embedded.avs']
    if len(matches)!=1:raise ValueError('ambiguous/missing FPGA endpoint address')
    slave=matches[0];start=int(slave.get('start'),0);end=int(slave.get('end'),0)
    if (bridge!=0 or offset!=context['peripheral_offset'] or start!=offset or
            end-start!=context['peripheral_span'] or slave.get('datawidth')!='32'):
        raise ValueError('FPGA control route differs from the sealed endpoint')
    for other in slaves:
        if other is not slave and int(other.get('start'),0)<end and start<int(other.get('end'),0):
            raise ValueError('overlapping FPGA control route')
    # Altera Agilex 5 GSRD, MPU lightweight HPS-to-FPGA address map:
    # https://altera-fpga.github.io/rel-25.1/embedded-designs/agilex-5/e-series/premium/gsrd/ug-gsrd-agx5e-premium/#lightweight-hps-to-fpga-address-map
    base=0x20000000
    result={'format':1,'scope':'sealed-lw-identity-probe-v1','architectural_lw_base':base,
            'bridge_base':bridge,'peripheral_offset':offset,'span':end-start,'cpu_base':base+bridge+offset,
            'handoff_sha256':sha,'package_id':context['package_id'],'build_id':context['build_id']}
    if context['programming_profile'] in ('de25-embedded-hps-first-ddr-diagnostic-v1','de25-embedded-hps-first-window-diagnostic-v1'):
        _fabric_stop_inventory(Path(project))
        route=parameters('subsys_periph_pb_cpu_0.m0','subsys_periph_fes_ddr_probe.avs')
        probes=[s for s in slaves if s.get('name')=='subsys_periph.fes_ddr_probe.avs']
        if (len(probes)!=1 or int(route['baseAddress'],0)!=0x10100 or
                int(probes[0].get('start'),0)!=0x10100 or int(probes[0].get('end'),0)!=0x10180 or
                probes[0].get('datawidth')!='32'):
            raise ValueError('inherited DDR probe control route differs')
        stop_scope='sealed-window-master-stop-v1' if context['control_abi']==0x10203042 else 'sealed-ddr-master-stop-v1'
        result['fabric_stop']={'scope':stop_scope,'probe_cpu_base':base+0x10100,
                               'probe_span':128,'maintenance':'held-reset-from-configuration'}
    return result


def _computer(project):
    """Bind the real computer route to the existing sealed control hierarchy.

    This fixed internal-RAM computer has no HPS master. It permits a separate
    control-only diagnostic, never data release or ordinary package admission.
    """
    project=Path(project).absolute();control_map=_control(project);context=_derive(project)
    hps=project/'board/hps';local=hps/'project/fes_mailbox'
    source=json.loads((hps/'inputs.json').read_text())['computer']
    names=('fes_de25_computer.v','fes_de25_computer_hw.tcl',
           'fes_computer_mailbox.v','generated/fes_de25_computer.vh','generated/fes_computer.vh')
    for name in names:
        path=local/name
        if (any(p.is_symlink() for p in (path,*path.parents)) or not path.is_file() or
                hashlib.sha256(path.read_bytes()).hexdigest()!=source['source_sha256'].get(name)):
            raise ValueError('computer endpoint differs from sealed source: '+name)
    text=(local/'fes_de25_computer.v').read_text()
    params=re.findall(r'fes_computer_mailbox\s*#\((.*?)\)\s*gp_mailbox\s*\(',text,re.S)
    expected='.ENABLE_KEYBOARD(1),.ENABLE_ZX81_TAPE(1),.MEDIA_AW(14),.UNIT0_MIN(`FES_COMPUTER_ZX81_TAPE_MIN_BYTES),.UNIT0_MAX(`FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES)'
    if source.get('new_dma_master') is not False or len(params)!=1 or re.sub(r'\s+','',params[0])!=expected:
        raise ValueError('unsupported computer capability or HPS-master contract')
    def constant(file,name):
        values=re.findall(r'(?m)^`define '+re.escape(name)+r" 32'h([0-9a-fA-F]{8})\s*$",(local/file).read_text())
        if len(values)!=1:raise ValueError('missing computer constant: '+name)
        return int(values[0],16)
    offset=constant('generated/fes_de25_computer.vh','FES_DE25_COMPUTER_PERIPHERAL_OFFSET')
    span=constant('generated/fes_de25_computer.vh','FES_DE25_COMPUTER_SPAN')
    if (offset,span)!=(0x10300,32) or constant('generated/fes_computer.vh','FES_COMPUTER_ABI_MINOR')!=0:
        raise ValueError('unsupported computer CSR/ABI version')
    if any(constant('generated/fes_de25_computer.vh',key)!=value for key,value in
            {'FES_DE25_COMPUTER_VERSION':0x10001,'FES_DE25_COMPUTER_FETCH_COUNT_OFFSET':24,
             'FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET':28}.items()):
        raise ValueError('computer transport lacks the compiled fetch observation contract')
    material=hps/'project/peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_computer/fes_de25_computer_10/synth'
    for name in ('fes_de25_computer.v','generated/fes_de25_computer.vh'):
        path=material/name
        if any(p.is_symlink() for p in (path,*path.parents)) or path.read_bytes()!=(local/name).read_bytes():
            raise ValueError('generated computer activity source differs from the selected endpoint')
    path=hps/'project/qsys_top/qsys_top.sopcinfo';raw=path.read_bytes()
    if hashlib.sha256(raw).hexdigest()!=control_map['handoff_sha256']:
        raise ValueError('computer and control use different generated handoffs')
    tree=ET.fromstring(raw)
    modules=[m for m in tree.iter('module') if m.get('path')=='subsys_periph.fes_computer']
    if len(modules)!=1 or modules[0].get('kind')!='fes_de25_computer' or modules[0].get('version')!='1.0':
        raise ValueError('missing computer endpoint/version')
    def connection(kind,start,end):
        found=[c for c in tree.iter('connection') if (c.get('kind'),c.get('start'),c.get('end'))==(kind,start,end)]
        if len(found)!=1:raise ValueError('missing/ambiguous computer route')
        return found[0]
    lw=connection('avalon','subsys_hps_agilex_hps.lwhps2fpga','subsys_periph_pb_cpu_0.s0')
    route=connection('avalon','subsys_periph_pb_cpu_0.m0','subsys_periph_fes_computer.avs')
    if int(route.findtext("parameter[@name='baseAddress']/value"),0)!=offset:
        raise ValueError('computer route base differs')
    regions=list(ET.fromstring(lw.findtext("parameter[@name='addressMapSysInfo']/value")).iter('slave'))
    found=[s for s in regions if s.get('name')=='subsys_periph.fes_computer.avs']
    if len(found)!=1 or (int(found[0].get('start'),0),int(found[0].get('end'),0),found[0].get('datawidth'))!=(offset,offset+span,'32'):
        raise ValueError('computer region differs')
    if any(s is not found[0] and int(s.get('start'),0)<offset+span and offset<int(s.get('end'),0) for s in regions):
        raise ValueError('overlapping computer region')
    connection('reset','subsys_periph_periph_rst_in.out_reset','subsys_periph_fes_computer.reset')
    for start,end,hz in (('subsys_periph_periph_clk.out_clk','subsys_periph_fes_computer.clk',100000000),
                         ('subsys_periph_fes_computer_pixel_clk.out_clk','subsys_periph_fes_computer.pixel_clk',74250000)):
        c=connection('clock',start,end)
        if c.findtext("parameter[@name='clockRateSysInfo']/value")!=str(hz):
            raise ValueError('computer clock route differs')
    wrapper=hps/'project/peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_computer/synth/peripheral_subsys_fes_computer.v'
    if any(p.is_symlink() for p in (wrapper,*wrapper.parents)) or wrapper.stat().st_size>65536:
        raise ValueError('bounded regular computer wrapper required')
    inst=re.findall(r'fes_de25_computer\s*#\((.*?)\)\s*fes_computer\s*\(',wrapper.read_text(),re.S)
    if len(inst)!=1 or re.sub(r'\s+','',inst[0])!='.EXTERNAL_RAM(0)':
        raise ValueError('computer wrapper is not internal RAM')
    return {'format':1,'scope':'sealed-cold-computer-control-v1','package_id':context['package_id'],
            'build_id':context['build_id'],'control_map_sha256':digest(control_map),
            'handoff_sha256':control_map['handoff_sha256'],
            'cpu_base':control_map['architectural_lw_base']+control_map['bridge_base']+offset,'span':span,
            'abi_minor':0,'capabilities':0x803,'hps_master':False,
            'endpoint_sha256':hashlib.sha256((local/'fes_de25_computer.v').read_bytes()).hexdigest(),
            'wrapper_sha256':hashlib.sha256(wrapper.read_bytes()).hexdigest()}


def _fabric_stop_inventory(project):
    """Recheck the generated complete inventory, not a claimed receipt flag."""
    def load(name,path):
        spec=importlib.util.spec_from_file_location(name,path)
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        return module
    builder=load('native_fabric_stop_builder',PROTOTYPE/'build.py')
    hps_tools=load('native_fabric_stop_hps',ROOT/'sources/misteross/boards/de25nano/hps/build.py')
    hps=project/'board/hps';record=json.loads((hps/'inputs.json').read_text())
    source=record['functional_snapshot_sha256'];memory=record['reader_memory']
    if memory not in ({'backend':'hps-ddr','bus_base':0x95000000,'bytes':8192},
                      {'backend':'hps-ddr-window','bus_base':0x95000000,'bytes':4<<20}):
        raise ValueError('master stop requires an exact compiled DDR reader window')
    board=SimpleNamespace(JTAG_CONTROL_BRIDGE_BASE=record['embedded_control']['jtag_bridge_base'])
    proof=builder.verify_control(hps/'project',hps_tools,board,memory)
    if (proof['maintenance_masters']!='held-reset-from-configuration' or
            proof['other_ddr_master']!='fes_ddr_probe; independent cancel/drain required'):
        raise ValueError('complete DDR master stop inventory missing')
    # Verify the tied reset expression and the generated inherited probe against
    # the very same source snapshot accepted by _derive. No arbitrary generated
    # source may supply the IDLE/maintenance observations used by the callback.
    local=hps/'project/fes_mailbox'
    endpoint=(local/'fes_de25_embedded.sv').read_text()
    if endpoint.count('assign maintenance_reset_n = !DDR_MODE;')!=1:
        raise ValueError('maintenance reset constant source differs')
    generated=hps/'project/peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ddr_probe/fes_de25_ddr_probe_10/synth'
    for name in ('fes_de25_ddr_probe.v','generated/fes_de25_ddr_probe.vh'):
        expected=source['sources/misteross/boards/de25nano/hps/'+name]
        for path in (local/name,generated/name):
            if hashlib.sha256(path.read_bytes()).hexdigest()!=expected:
                raise ValueError('inherited DDR probe source differs from sealed inventory')
    return proof


def _program_parameters(project):
    """Read the complete fixed HPS/EMIF recipe without rewriting its review.

    These are source compatibility assertions, not live memory qualification.
    New personas retain the board's existing HPS subsystem and DDR settings.
    """
    hps = Path(project)/'board/hps'
    record = json.loads((hps/'inputs.json').read_text())
    expected = json.loads(json.dumps(record['vendor_hps_emif_parameters']))
    hps_ip = 'hps_subsys/ip/hps_subsys/agilex_hps.ip'
    names = {hps_ip, 'hps_subsys/ip/hps_subsys/f2sdram_adapter.ip',
             'hps_subsys/ip/qsys_top/emif_io96b_hps.ip'}
    if set(expected) != names or expected[hps_ip].get('f2s_data_width') != '256':
        raise ValueError('complete pinned HPS/EMIF parameter inventory required')
    expected[hps_ip]['f2s_data_width'] = '0'
    defaults = ('ED_en_sim_gen', 'FPGA_EMAC0_tx_clk_en', 'FPGA_EMAC1_tx_clk_en',
                'FPGA_EMAC2_tx_clk_en', 'FPGA_EMAC_PTP_clk_en')
    if any(key in expected[hps_ip] for key in defaults):
        raise ValueError('pinned HPS parameter defaults changed')
    expected[hps_ip].update({key: 'true' for key in defaults})
    ns = {'ip': 'http://www.accellera.org/XMLSchema/IPXACT/1685-2014',
          'altera': 'http://www.altera.com/XMLSchema/IPXact2014/extensions'}
    observed = {}
    for name in sorted(names):
        path = hps/'project'/name
        if any(p.is_symlink() for p in (path, *path.parents)) or not 0 < path.stat().st_size <= 2<<20:
            raise ValueError('private bounded HPS/EMIF parameter source required')
        tree = ET.fromstring(path.read_bytes())
        rows = [('', item) for item in tree.findall('.//altera:altera_module_parameters/ip:parameters/ip:parameter', ns)]
        for module in tree.findall('.//altera:sub_module', ns):
            prefix = module.get('{'+ns['altera']+'}name')
            if not prefix:
                raise ValueError('missing HPS/EMIF submodule name')
            rows.extend(('submodule/'+prefix+'/', item) for item in module.findall('ip:parameters/ip:parameter', ns))
        values = {}
        for prefix, item in rows:
            key = item.get('parameterId')
            if not key or prefix+key in values:
                raise ValueError('duplicate or missing HPS/EMIF parameter')
            values[prefix+key] = item.findtext('ip:value', namespaces=ns) or ''
        if values != expected[name]:
            raise ValueError('HPS/EMIF parameters differ from the fixed compiled board recipe: '+name)
        observed[name] = values
    return observed


def _program_platform(project, context=None):
    context = _derive(project) if context is None else context
    if context['programming_profile'] != 'de25-embedded-hps-first-ddr-diagnostic-v1':
        raise ValueError('selected programs require the complete contained DDR-shell inventory')
    parameters = _program_parameters(project)
    pinmux = _hps_io(project)
    inventory = _fabric_stop_inventory(Path(project))
    platform = {key: context[key] for key in ('platform', 'device', 'programming_profile',
                'abi', 'abi_major', 'abi_minor', 'control_abi', 'hps_io_hash', 'pfg_identity')}
    platform.update(format=1, scope='nano-contained-program-platform-v1',
                    hps_parameters_sha256=digest(parameters), hps_handoff_sha256=pinmux['handoff_sha256'],
                    master_inventory_sha256=digest(inventory))
    return {'format': 1, 'scope': 'nano-program-platform-source-v1',
            'boot_context_sha256': digest(context), 'platform': platform}


def _selected_program(project):
    context = _derive(project)
    return {'format': 1, 'scope': 'nano-selected-program-v1',
            'package_id': context['package_id'], 'build_id': context['build_id'],
            'platform': _program_platform(project, context)['platform'],
            'control': _control(project), 'computer': _computer(project)}


def checked(record, program_key='program'):
    keys = ('hps_context', 'hps_context_sha256', 'hps_project')
    if all(record.get(key) is None for key in keys):
        if record.get(program_key) in ('bootcontext','hpsio','fpgacontext','fpgastatus','fpgaload'):
            raise ValueError('native context image is missing its sealed HPS context')
        return None
    if any(record.get(key) is None for key in keys) or record.get(program_key) not in ('bootcontext','hpsio','fpgacontext','fpgastatus','fpgaload'):
        raise ValueError('HPS source context requires a dedicated native context image')
    expected = derive(record['hps_project'])
    if record['hps_context'] != expected or record['hps_context_sha256'] != digest(expected):
        raise ValueError('native HPS context differs from the sealed FPGA source')
    return expected


def checked_hps_io(record, program_key='program'):
    keys=('hps_io','hps_io_sha256')
    if record.get(program_key)!='hpsio':
        if any(record.get(key) is not None for key in keys):
            raise ValueError('live HPS I/O observation requires its dedicated native image')
        return None
    checked(record,program_key)
    expected=hps_io(record['hps_project'])
    if record.get(keys[0])!=expected or record.get(keys[1])!=digest(expected):
        raise ValueError('HPS I/O observation map differs from the sealed compiled handoff')
    return expected


def checked_control(record, program_key='program'):
    keys=('hps_control','hps_control_sha256')
    if all(record.get(key) is None for key in keys) and record.get(program_key) not in ('fpgacontext','fpgastatus','fpgaload'):return None
    if record.get(program_key) not in ('fpgacontext','fpgastatus','fpgaload') or any(record.get(key) is None for key in keys):
        raise ValueError('dedicated native FPGA diagnostic and sealed control map required')
    checked(record,program_key)
    expected=control(record['hps_project'])
    if record['hps_control']!=expected or record['hps_control_sha256']!=digest(expected):
        raise ValueError('native control map differs from the compiled FPGA hierarchy')
    return expected


def checked_configuration(record, program_key='program'):
    keys=('hps_configuration','hps_configuration_sha256')
    if all(record.get(key) is None for key in keys) and record.get(program_key)!='fpgaload':return None
    if record.get(program_key)!='fpgaload' or any(record.get(key) is None for key in keys):
        raise ValueError('cold configuration requires a dedicated native image and sealed map')
    checked_control(record,program_key)
    expected=configuration(record['hps_project'])
    if record['hps_configuration']!=expected or record['hps_configuration_sha256']!=digest(expected):
        raise ValueError('cold configuration map differs from the sealed core')
    return expected


def cpu_mapping(control_map, configuration_map, reader_map):
    """Expected EL1 resource layout, never a CPU or physical-memory grant."""
    if (not isinstance(control_map,dict) or not {'cpu_base','span'}<=set(control_map) or
            not isinstance(configuration_map,dict) or not {'base','size'}<=set(configuration_map) or
            reader_map is not None and (not isinstance(reader_map,dict) or not {'arena_base','arena_bytes'}<=set(reader_map))):
        raise ValueError('complete source-derived CPU resource inputs required')
    fields={'ram_base':0x84000000,'ram_bytes':256<<20,
            'control_base':control_map['cpu_base'],'control_bytes':control_map['span'],
            'dma_base':configuration_map['base'],'dma_bytes':configuration_map['size'],
            'reader_base':reader_map['arena_base'] if reader_map else 0,
            'reader_bytes':reader_map['arena_bytes'] if reader_map else 0}
    if (any(type(v) is not int for v in fields.values()) or
            (fields['control_base'],fields['control_bytes'],fields['dma_base'],fields['dma_bytes'])!=(0x20010400,256,0x94800000,8<<20) or
            (fields['reader_base'],fields['reader_bytes']) not in ((0,0),(0x95000000,2<<20),(0x95000000,4<<20))):
        raise ValueError('unsupported source-derived native CPU resource layout')
    return dict(format=1,scope='pinned-tamago-el1-resources-v1',**fields)


def checked_cpu_mapping(record, program_key='program'):
    keys=('cpu_mapping','cpu_mapping_sha256')
    if record.get(program_key)!='fpgaload':
        if any(record.get(key) is not None for key in keys):
            raise ValueError('native CPU mapping contract requires the dedicated FPGA load image')
        return None
    configuration_map=checked_configuration(record,program_key)
    reader_map=checked_reader_memory(record,program_key)
    expected=cpu_mapping(record['hps_control'],configuration_map,reader_map)
    if record.get(keys[0])!=expected or encode(record[keys[0]])!=encode(expected) or record.get(keys[1])!=digest(expected):
        raise ValueError('native CPU mapping contract differs from the selected resources')
    return expected


def checked_reader_memory(record, program_key='program'):
    keys = ('hps_reader_memory', 'hps_reader_memory_sha256')
    if record.get(program_key) != 'fpgaload':
        if any(record.get(key) is not None for key in keys):
            raise ValueError('reader memory requires the dedicated cold configuration image')
        return None
    checked_configuration(record, program_key)
    expected = reader_memory(record['hps_project'])
    if (record.get(keys[0]) != expected or
            record.get(keys[1]) != (digest(expected) if expected is not None else None)):
        raise ValueError('native reader memory differs from the sealed source')
    return expected


def checked_ddr_read(record, program_key='program'):
    keys=('hps_ddr_read','hps_ddr_read_sha256')
    if all(record.get(key) is None for key in keys):return None
    if record.get(program_key)!='fpgaload' or any(record.get(key) is None for key in keys):
        raise ValueError('active DDR read requires its dedicated cold configuration selection')
    if checked_reader_memory(record,program_key) is None:
        raise ValueError('active DDR read requires pinned sealed DDR memory')
    expected=ddr_read(record['hps_project'])
    if record[keys[0]]!=expected or record[keys[1]]!=digest(expected):
        raise ValueError('active DDR read differs from the sealed generated adapter')
    return expected


def checked_computer(record, program_key='program'):
    keys=('hps_computer','hps_computer_sha256')
    reload=record.get('computer_reload',False)
    if type(reload) is not bool or reload and record.get('hps_computer') is None:
        raise ValueError('computer reload requires an explicit boolean and sealed computer contract')
    storage_return=record.get('computer_storage_return',False)
    if type(storage_return) is not bool or storage_return and not reload:
        raise ValueError('computer storage return requires the explicit two-load computer selection')
    if all(record.get(key) is None for key in keys):return None
    if record.get(program_key)!='fpgaload' or any(record.get(key) is None for key in keys) or record.get('hps_ddr_read') is not None:
        raise ValueError('computer control requires its dedicated control-only cold configuration')
    checked_configuration(record,program_key)
    expected=computer(record['hps_project'])
    if record[keys[0]]!=expected or record[keys[1]]!=digest(expected):
        raise ValueError('computer control differs from sealed hardware source')
    return expected


def checked_program_platform(record, program_key='program'):
    selected = record.get('selected_programs', False)
    keys = ('hps_program_platform', 'hps_program_platform_sha256')
    if type(selected) is not bool:
        raise ValueError('selected program mode requires an explicit boolean')
    if not selected:
        if any(record.get(key) is not None for key in keys):
            raise ValueError('program platform requires selected program mode')
        return None
    if (record.get(program_key) != 'fpgaload' or checked_computer(record, program_key) is None or
            record.get('computer_reload') is not True or record.get('computer_storage_return') is not True):
        raise ValueError('selected programs require the contained computer and fresh SD returns')
    expected = program_platform(record['hps_project'])
    if record.get(keys[0]) != expected or record.get(keys[1]) != digest(expected):
        raise ValueError('program platform differs from the compiled boot source')
    return expected


if __name__ == '__main__':
    actions = {'control': _control, 'configuration': _configuration, 'package': _package,
               'reader-memory': _reader_memory, 'ddr-read': _ddr_read, 'computer': _computer,
               'hps-io': _hps_io, 'program-platform': _program_platform, 'selected-program': _selected_program}
    if len(sys.argv) not in (2,3) or len(sys.argv)==3 and sys.argv[2] not in actions:
        raise SystemExit('usage: hps_context.py SEALED_PROJECT ['+'|'.join(actions)+']')
    action = _derive if len(sys.argv)==2 else actions[sys.argv[2]]
    sys.stdout.buffer.write(encode(action(sys.argv[1])))
