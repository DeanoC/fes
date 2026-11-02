"""Derive immutable HPS-first source context without a native SPL hash cycle.

The descriptor binds the sealed FPGA inputs before the native image is built.
SPL and BL31 bind its digest through the existing cold chain. This is a local
source-bound diagnostic assertion, not measured/signed boot or admission.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import struct
import sys
from types import SimpleNamespace
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
PROTOTYPE = ROOT/'sources/misteross/prototypes/de25_board_control'
SCOPE = 'source-bound-hps-first-context-v1'


def encode(context):
    return (json.dumps(context, sort_keys=True, separators=(',', ':'))+'\n').encode()


def digest(context):
    return hashlib.sha256(encode(context)).hexdigest()


def inputs():
    return [Path(__file__), PROTOTYPE/'verify.py', PROTOTYPE/'seal.py', PROTOTYPE/'reader_memory.py', PROTOTYPE/'build.py',
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


def configuration(project):
    return _worker(project, 'configuration')


def reader_memory(project):
    return _worker(project, 'reader-memory')


def reader_pattern():
    return b''.join(struct.pack('<I', (0x4e414e4f ^ (i * 0x9e3779b9)) & 0xffffffff)
                    for i in range(8192 // 4))


def _reader_memory(project):
    context = _derive(project)
    if context['programming_profile'] == 'de25-embedded-hps-first-diagnostic-v1':
        return None
    parameters = json.loads((Path(project)/'board/hps/build-inputs.json').read_text())['parameters']
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
    if (not raw or len(raw)>8<<20 or len(raw)&3 or len(raw)!=context['core_size'] or
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
    if context['programming_profile']=='de25-embedded-hps-first-ddr-diagnostic-v1':
        _fabric_stop_inventory(Path(project))
        route=parameters('subsys_periph_pb_cpu_0.m0','subsys_periph_fes_ddr_probe.avs')
        probes=[s for s in slaves if s.get('name')=='subsys_periph.fes_ddr_probe.avs']
        if (len(probes)!=1 or int(route['baseAddress'],0)!=0x10100 or
                int(probes[0].get('start'),0)!=0x10100 or int(probes[0].get('end'),0)!=0x10180 or
                probes[0].get('datawidth')!='32'):
            raise ValueError('inherited DDR probe control route differs')
        result['fabric_stop']={'scope':'sealed-ddr-master-stop-v1','probe_cpu_base':base+0x10100,
                               'probe_span':128,'maintenance':'held-reset-from-configuration'}
    return result


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
    if memory!={'backend':'hps-ddr','bus_base':0x95000000,'bytes':8192}:
        raise ValueError('master stop requires the fixed DDR diagnostic')
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


def checked(record, program_key='program'):
    keys = ('hps_context', 'hps_context_sha256', 'hps_project')
    if all(record.get(key) is None for key in keys):
        if record.get(program_key) in ('bootcontext','fpgacontext','fpgastatus','fpgaload'):
            raise ValueError('native context image is missing its sealed HPS context')
        return None
    if any(record.get(key) is None for key in keys) or record.get(program_key) not in ('bootcontext','fpgacontext','fpgastatus','fpgaload'):
        raise ValueError('HPS source context requires a dedicated native context image')
    expected = derive(record['hps_project'])
    if record['hps_context'] != expected or record['hps_context_sha256'] != digest(expected):
        raise ValueError('native HPS context differs from the sealed FPGA source')
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


if __name__ == '__main__':
    if len(sys.argv) not in (2,3) or len(sys.argv)==3 and sys.argv[2] not in ('control','configuration','package','reader-memory'):
        raise SystemExit('usage: hps_context.py SEALED_PROJECT [control|configuration|package|reader-memory]')
    action=_derive if len(sys.argv)==2 else {'control':_control,'configuration':_configuration,'package':_package,'reader-memory':_reader_memory}[sys.argv[2]]
    sys.stdout.buffer.write(encode(action(sys.argv[1])))
