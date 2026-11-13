"""One source-bound cold/serial chain identity shared by SPL and BL31."""
import hashlib
import importlib.util
import json
from pathlib import Path

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
FW=HERE.parent/'firmware'
_spec=importlib.util.spec_from_file_location('fes_cold_hps_context',HERE/'hps_context.py')
hps_context=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(hps_context)
_payload_spec=importlib.util.spec_from_file_location('fes_cold_entry_payload',HERE.parent/'entry/payload.py')
entry_payload=importlib.util.module_from_spec(_payload_spec);_payload_spec.loader.exec_module(entry_payload)
_ram_spec=importlib.util.spec_from_file_location('fes_cold_firmware_ram',FW/'nano_firmware_ram.py')
firmware_memory=importlib.util.module_from_spec(_ram_spec);_ram_spec.loader.exec_module(firmware_memory)
_cap_spec=importlib.util.spec_from_file_location('fes_cold_capability',FW/'nano_capability_profile.py')
capability=importlib.util.module_from_spec(_cap_spec);_cap_spec.loader.exec_module(capability)
_probe_spec=importlib.util.spec_from_file_location('fes_selected_sdm_probe',FW/'nano_sdm_probe_profile.py')
sdm_probe=importlib.util.module_from_spec(_probe_spec);_probe_spec.loader.exec_module(sdm_probe)
_fault_spec=importlib.util.spec_from_file_location('fes_selected_sdm_fault64',FW/'nano_sdm_fault_profile.py')
sdm_fault=importlib.util.module_from_spec(_fault_spec);_fault_spec.loader.exec_module(sdm_fault)
_input_spec=importlib.util.spec_from_file_location('fes_selected_sdm_input',FW/'nano_sdm_input_profile.py')
sdm_input=importlib.util.module_from_spec(_input_spec);_input_spec.loader.exec_module(sdm_input)
EXPERIMENTAL_FILE='FES/UPLOAD.BIN'
_fit_spec=importlib.util.spec_from_file_location('fes_cold_fit_layout',HERE/'fit_layout.py')
fit_layout=importlib.util.module_from_spec(_fit_spec);_fit_spec.loader.exec_module(fit_layout)
MAX_FIT_SIZE=fit_layout.FIT_MAX_BYTES


def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def selected_profile(receipt):
    for name in ('probe-profile.json','sd-profile.json','smmu-profile.json'):
        profile=HERE/name
        if receipt.get('firmware_profile_sha256')==sha(profile):return profile
    raise ValueError('entry image has no source-derived Nano diagnostic profile')


def check_sources(snapshot):
    if not isinstance(snapshot,dict) or not snapshot:
        raise ValueError('missing native selected-source proof')
    for name,expected in snapshot.items():
        path=ROOT/name
        if (Path(name).is_absolute() or '..' in Path(name).parts or
                not path.resolve().is_relative_to(ROOT) or sha(path)!=expected):
            raise ValueError('native selected source changed: '+name)


def entry(entry_build):
    receipt_path=entry_build/'result.json';receipt=json.loads(receipt_path.read_text())
    image=entry_build/'entry.img';profile=selected_profile(receipt)
    if profile.name=='smmu-profile.json' and receipt.get('native_program')!='probe':
        raise ValueError('SMMU observation requires the source-bound serial probe')
    if (receipt.get('status')!='built' or receipt.get('target')!='de25-nano' or
            receipt.get('entry_image_address')!=0x96080000 or receipt.get('ram_start')!=0x84000000 or
            receipt.get('ram_end')!=0x94000000 or receipt.get('uart_base')!=0x10c02100 or
            receipt.get('entry_image_sha256')!=sha(image) or receipt.get('entry_image_size')!=image.stat().st_size or
            not 0<image.stat().st_size<=32<<20 or receipt.get('firmware_profile_sha256')!=sha(profile)):
        raise ValueError('entry image is not bound to the source-derived cold Nano profile')
    usb=receipt.get('usb_control',False)
    hps_context.usb_selection.mode(receipt,program_key='native_program')
    if (usb and receipt.get('usb_input') not in ('boot-keyboard','gamepad') or
            not usb and receipt.get('usb_input') is not None):
        raise ValueError('USB boot input differs from native selection')
    entry_payload.checked(receipt,image.read_bytes(),(entry_build/'payload.elf').read_bytes())
    check_sources(receipt.get('native_build_sources'))
    for name,expected in receipt['sources'].items():
        if sha(ROOT/name)!=expected:raise ValueError('entry producer source changed: '+name)
    context=hps_context.checked(receipt,program_key='native_program')
    hps_context.checked_hps_io(receipt,program_key='native_program')
    hps_context.checked_control(receipt,program_key='native_program')
    configuration=hps_context.checked_configuration(receipt,program_key='native_program')
    reader_memory=hps_context.checked_reader_memory(receipt,program_key='native_program')
    hps_context.checked_window_read(receipt,program_key='native_program')
    hps_context.checked_menu_read(receipt,program_key='native_program')
    window=reader_memory is not None and reader_memory.get('scope')=='sealed-cold-window-reader-memory-v1'
    if type(receipt.get('window_memory',False)) is not bool or receipt.get('window_memory',False)!=window:
        raise ValueError('cold window memory requires the explicitly selected native entry')
    cpu_map=hps_context.checked_cpu_mapping(receipt,program_key='native_program')
    hps_context.checked_ddr_read(receipt,program_key='native_program')
    hps_context.checked_computer(receipt,program_key='native_program')
    hps_context.checked_rom(receipt,program_key='native_program')
    hps_context.checked_program_platform(receipt,program_key='native_program')
    selected_tape=hps_context.initial_tape.checked(receipt,program_key='native_program')
    if selected_tape and any(name not in (entry_build/'payload.elf').read_bytes() for name in
            (b'initialTapeCPU.LoadInitialTape',b'ConfigurationArena).PublishInitialTape')):
        raise ValueError('initial tape platform is absent from matched native entry')
    if context is not None and profile.name!=('sd-profile.json' if receipt.get('native_program')=='fpgaload' else 'probe-profile.json'):
        raise ValueError('HPS context diagnostic requires its source-derived serial/SD profile')
    if configuration is not None and [configuration['base'],configuration['base']+configuration['size']] not in receipt.get('staging_exclusions',[]):
        raise ValueError('cold configuration arena is not excluded from entry staging')
    if reader_memory is not None and [cpu_map['reader_base'],cpu_map['reader_base']+cpu_map['reader_bytes']] not in receipt.get('staging_exclusions',[]):
        raise ValueError('cold reader arena is not excluded from entry staging')
    return image,receipt_path


def sources():
    return [HERE.parent/'experiment.py',HERE/'fit_layout.py',HERE.parent/'entry/payload.py',ROOT/'scripts/de25_boot_contract.py']+[HERE/name for name in ('startup.py','cold_profile.py','native_admission.py','fault_status.py','mmu_status.py','mmu_logging.py','mmu_mailbox_view.py','runtime_startup.py','loader_progress.py','call_trace.py','hps_context.py','hps_io.py','usb_selection.py','initial_tape.py','spl_cold.c','spl_console.inc','prepare_uboot.py','uboot.c','handoff.c','handoff.h','probe-profile.json','sd-profile.json','smmu-profile.json')]+[
        FW/name for name in ('nano_cold.h','nano_cpu_mode.h','nano_mmu.h','nano_mmu.c','nano_mmu_platform.h','nano_mmu_platform.c','nano_mmu_memory.h','nano_mmu_memory.c','nano_mmu_transition.S','nano_mmu_transition.h','nano_mmu_logging.inc','nano_firmware_mmu.py','nano_firmware_ram.h','nano_firmware_ram.py','nano_firmware_runtime.py','nano_firmware_call.py','nano_firmware_percpu.py','nano_firmware_runtime.inc','nano_firmware_entry.inc','nano_firmware_fault.inc','nano_firmware_decode.inc','nano_firmware_cache.inc','nano_firmware_exit_cache.inc','nano_cold.c','nano_cold_mmio.inc','nano_cold_platform.c','nano_boot.h',
                            'nano_sd.h','nano_sd.c','nano_sd_platform.h','nano_sd_platform.c','generated/fes_embedded_sd.h',
                            'nano_mac.c','nano_mac.h','generated/fes_embedded_mac.h',
                            'nano_usb_service.c','nano_usb_service.h','nano_usb_platform.h','generated/fes_embedded_usb.h',
                            'nano_usb_platform.c','nano_usb_board.c','nano_usb_board.h','nano_usb_boot.inc',
                            'nano_usb_composition.c','nano_usb_composition.h',
                            'nano_program.c','nano_program.h',
                            'nano_rom.c','nano_rom.h','nano_rom_platform.inc','generated/fes_embedded_rom.h',
                            'nano_tape.c','nano_tape.h','nano_tape_service.c','nano_tape_service.h','nano_tape_platform.inc',
                            'generated/fes_embedded_tape.h','generated/fes_computer.h','generated/fes_de25_computer.h',
                            'nano_boot.c','nano_bridges.h','nano_bridges.c','nano_bridge_platform.inc','nano_bridge_platform.h',
                            'nano_owned_sip.inc','nano_computer_control.inc','nano_window_control.inc','nano_program_control.inc','prepare_nano_owned.py','prepare_tfa.py','nano_digest.py',
                            'nano_mailbox.py','nano_start_trace.py','nano_secure_mmio.py','nano_secure_mmio.h','nano_mailbox_observe.inc','nano_mailbox_observe.h','nano_mailbox_status.inc',
                            'nano_bridge_sip.c','nano_bridge_sip.h','generated/fes_embedded_bridges.h',
                            'nano_identity.c','nano_identity.h','nano_configuration.c','nano_configuration.h','nano_smmu.c','nano_smmu.h',
                            'nano_fabric_stop.c','nano_fabric_stop.h','nano_probe_stop.c','nano_probe_stop.h','generated/fes_de25_ddr_probe.h','generated/fes_embedded_control.h',
                            'nano_ddr_firewall.c','nano_ddr_firewall.h','nano_ddr_read.c','nano_ddr_read.h','nano_hps_io.c','nano_hps_io.h',
                            'generated/fes_embedded_control.hpp','nano_bridge_diag.h','nano_bridge_diag.inc',
                            'build_terasic.py','build_terasic_uboot.py','verify.py')] + capability.inputs() + sdm_probe.inputs() + sdm_fault.inputs() + sdm_input.inputs()


def make(entry_build, *, firmware_ram=False, firmware_mmu=False, capability_observe=False,sdm_arm_probe=False,sdm_fault64=False,sdm_input_transfer=False):
    if type(sdm_input_transfer) is not bool:raise ValueError('explicit boolean SDM input selection required')
    if sdm_input_transfer and (sdm_fault64 or sdm_arm_probe):raise ValueError('SDM diagnostic modes are mutually exclusive')
    if type(sdm_fault64) is not bool:raise ValueError('explicit boolean SDM fault64 selection required')
    if sdm_arm_probe and sdm_fault64:raise ValueError('arm-only and fault64 modes are mutually exclusive')
    if type(sdm_arm_probe) is not bool:raise ValueError('explicit boolean SDM arm probe selection required')
    if type(capability_observe) is not bool:raise ValueError('explicit boolean capability selection required')
    if type(firmware_ram) is not bool:raise ValueError('explicit firmware RAM selection required')
    if type(firmware_mmu) is not bool or firmware_mmu and not firmware_ram:
        raise ValueError('explicit firmware MMU selection requires the OCRAM profile')
    mmu_args={'firmware_mmu':True} if firmware_mmu else {}
    if capability_observe:mmu_args['capability_observe']=True
    if sdm_arm_probe:mmu_args['sdm_arm_probe']=True
    if sdm_fault64:mmu_args['sdm_fault64']=True
    if sdm_input_transfer:mmu_args['sdm_input_transfer']=True
    record=json.loads((entry_build/'result.json').read_text())
    if record.get('hps_project') is None:
        return _make(entry_build,firmware_ram=firmware_ram,**mmu_args)
    # Only immutable derivation results are reused. Each call brackets all
    # original validators with a complete current file/tree snapshot, and the
    # compiler's independent pre/post calls each get a new empty scope.
    spec=importlib.util.spec_from_file_location('fes_cold_source_snapshot',HERE/'native_admission.py')
    admission=importlib.util.module_from_spec(spec);spec.loader.exec_module(admission)
    entry_build=admission._plain(entry_build,directory=True)
    if not entry_build.is_relative_to(ROOT/'out'):
        raise ValueError('private cold entry required')
    receipt_path=entry_build/'result.json';record=admission._json(receipt_path)
    projects=[record['hps_project']]
    if record.get('hps_menu_project') is not None:
        projects.append(record['hps_menu_project'])
    files=[]
    for key in ('native_build_sources','sources'):
        mapping=record.get(key)
        if not isinstance(mapping,dict) or not mapping:
            raise ValueError('complete cold source mapping required')
        files.extend(admission._relative(ROOT,name) for name in mapping)
    def snapshot():
        if admission._json(receipt_path)!=record:
            raise ValueError('cold entry receipt changed during derivation')
        value=admission.source_snapshot(ROOT,projects,files,
            extra_trees=(entry_build,ROOT/'platform/embedded/tamago'))
        if admission._json(receipt_path)!=record:
            raise ValueError('cold entry receipt changed during derivation')
        return value
    with hps_context.worker_scope(projects,snapshot):
        return _make(entry_build,firmware_ram=firmware_ram,**mmu_args)


def _make(entry_build, *, firmware_ram=False, firmware_mmu=False, capability_observe=False,sdm_arm_probe=False,sdm_fault64=False,sdm_input_transfer=False):
    if type(sdm_input_transfer) is not bool:raise ValueError('explicit boolean SDM input selection required')
    if sdm_input_transfer and (sdm_fault64 or sdm_arm_probe):raise ValueError('SDM diagnostic modes are mutually exclusive')
    if type(sdm_fault64) is not bool:raise ValueError('explicit boolean SDM fault64 selection required')
    if sdm_arm_probe and sdm_fault64:raise ValueError('arm-only and fault64 modes are mutually exclusive')
    if type(sdm_arm_probe) is not bool:raise ValueError('explicit boolean SDM arm probe selection required')
    if type(capability_observe) is not bool:raise ValueError('explicit boolean capability selection required')
    if type(firmware_ram) is not bool:raise ValueError('explicit firmware RAM selection required')
    image,receipt=entry(entry_build)
    build=json.loads(receipt.read_text());profile=selected_profile(build)
    context=hps_context.checked(build,program_key='native_program')
    io_map=hps_context.checked_hps_io(build,program_key='native_program')
    control=hps_context.checked_control(build,program_key='native_program')
    configuration=hps_context.checked_configuration(build,program_key='native_program')
    reader_memory=hps_context.checked_reader_memory(build,program_key='native_program')
    cpu_map=hps_context.checked_cpu_mapping(build,program_key='native_program')
    ddr_read=hps_context.checked_ddr_read(build,program_key='native_program')
    window_read=hps_context.checked_window_read(build,program_key='native_program')
    menu_read=hps_context.checked_menu_read(build,program_key='native_program')
    computer=hps_context.checked_computer(build,program_key='native_program')
    rom=hps_context.checked_rom(build,program_key='native_program')
    companion=hps_context.checked_menu_program(build,program_key='native_program')
    program_platform=hps_context.checked_program_platform(build,program_key='native_program')
    selected=sources()+ (hps_context.inputs() if context is not None else [])
    inputs={str(p.relative_to(ROOT)):sha(p) for p in selected}
    identity={'format':1,'profile':'cold-smmu-preloaded' if profile.name=='smmu-profile.json' else 'cold-sd-pio-preloaded' if profile.name=='sd-profile.json' else 'cold-serial-preloaded',
              'sd_pio':profile.name=='sd-profile.json','usb_pio':build.get('usb_control',False),'firmware_profile_sha256':sha(profile),
              'usb_input':build.get('usb_input'),'usb_shared':build.get('usb_shared',False),
              'configuration_retained':build.get('configuration_retained',False),
              'usb_probe':hps_context.usb_selection.contained_probe(build,program_key='native_program'),
              'smmu_observe':profile.name=='smmu-profile.json',
              'secure_capture_trace':True,
              'spl_atf_trace':True,
              'native_sources_sha256':hashlib.sha256(json.dumps(build['native_build_sources'],sort_keys=True).encode()).hexdigest(),
              'boot_fit':fit_layout.for_entry(build),'experimental_fit_path':EXPERIMENTAL_FILE,'entry_image_sha256':sha(image),
              'entry_image_size':image.stat().st_size,'producer_sources':inputs,
              'uboot_pin':'d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413',
              'tfa_pin':'580dcb5c4e10aa7daff2bf69d493c076a6d3e665',
              'reference_sof_sha256':context['raw_sof_sha256'] if context else 'e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09',
              'hps_context':context,'hps_context_sha256':hps_context.digest(context) if context else None,
              'hps_io':io_map,'hps_io_sha256':hps_context.digest(io_map) if io_map else None,
              'cpu_mapping':cpu_map,'cpu_mapping_sha256':hps_context.digest(cpu_map) if cpu_map else None,
              'hps_control':control,'hps_control_sha256':hps_context.digest(control) if control else None,
              'hps_configuration':configuration,'hps_configuration_sha256':hps_context.digest(configuration) if configuration else None,
              'hps_reader_memory':reader_memory,'hps_reader_memory_sha256':hps_context.digest(reader_memory) if reader_memory else None,
              'hps_ddr_read':ddr_read,'hps_ddr_read_sha256':hps_context.digest(ddr_read) if ddr_read else None,
              'hps_computer':computer,'hps_computer_sha256':hps_context.digest(computer) if computer else None,
              'hps_rom':rom,'hps_rom_sha256':hps_context.digest(rom) if rom else None,
              'window_memory':build.get('window_memory',False),
              'menu_present':build.get('menu_present',False),'hps_menu_read':menu_read,
              'hps_menu_read_sha256':hps_context.digest(menu_read) if menu_read else None,
              'window_present':build.get('window_present',False),
              'window_storage_return':build.get('window_storage_return',False),
              'window_reload':build.get('window_reload',False),
              'window_service':build.get('window_service',False),
              'tenfoot_serial':build.get('tenfoot_serial',False),
              'initial_tape':hps_context.initial_tape.checked(build,program_key='native_program'),
              'selected_programs':build.get('selected_programs',False),
              'hps_program_platform':program_platform,'hps_program_platform_sha256':hps_context.digest(program_platform) if program_platform else None,
              'hps_menu_program':companion,'hps_menu_program_sha256':hps_context.digest(companion) if companion else None,
              'window_read':window_read is not None,'hps_window_read':window_read,
              'hps_window_read_sha256':hps_context.digest(window_read) if window_read else None,
              'computer_reload':build.get('computer_reload',False),
              'computer_storage_return':build.get('computer_storage_return',False)}
    identity['firmware_ram']=firmware_memory.selected(identity,firmware_ram)
    if identity['initial_tape'] and not firmware_ram:
        raise ValueError('initial tape requires the matched firmware OCRAM exclusion')
    import nano_firmware_mmu as mmu
    if firmware_mmu:identity['firmware_mmu']=mmu.selected(identity,firmware_mmu)
    observed=capability.selected(identity,capability_observe)
    if observed is not None:identity['capability_observation']=observed
    probe=sdm_probe.selected(identity,sdm_arm_probe)
    if probe is not None:identity['sdm_arm_probe']=probe
    fault=sdm_fault.selected(identity,sdm_fault64)
    if fault is not None:identity['sdm_fault64']=fault
    if sdm_input_transfer:
        identity['sdm_reader_route']=sdm_input.reader.derive(build.get('hps_project'),build.get('hps_menu_project'),identity)
    transfer=sdm_input.selected(identity,sdm_input_transfer)
    if transfer is not None:identity['sdm_input_transfer']=transfer
    identity['chain_sha256']=hashlib.sha256(json.dumps(identity,sort_keys=True).encode()).hexdigest()
    identity['owner_seed']=int(identity['chain_sha256'][:16],16)|1
    identity['entry_receipt_sha256']=sha(receipt)
    return identity


def initial_tape_header(profile):
    tape=hps_context.initial_tape.checked(dict(profile,program='fpgaload',usb_control=profile.get('usb_pio',False)))
    if tape and profile.get('firmware_ram')!=firmware_memory.CONTRACT:
        raise ValueError('initial tape requires the complete matched firmware OCRAM contract')
    return '#define FES_NANO_INITIAL_TAPE '+str(int(tape))+'\n'


def header(profile):
    import nano_firmware_mmu as mmu
    mmu.checked(profile)
    ram=firmware_memory.checked(profile)
    capability_contract=capability.checked(profile)
    menu_present=hps_context.menu_selection(dict(profile,program='fpgaload',usb_control=profile.get('usb_pio',False)))
    companion=hps_context.compiled_menu_program(dict(profile,program='fpgaload'))
    def array(value):return '{'+','.join('0x'+value[i:i+2] for i in range(0,len(value),2))+'}'
    smmu=profile.get('smmu_observe',False)
    if type(smmu) is not bool or smmu and (profile['sd_pio'] or
            any(profile.get(key) is not None for key in ('hps_context','hps_io','hps_control','hps_configuration','hps_reader_memory','hps_ddr_read','hps_window_read','hps_computer'))):
        raise ValueError('boot SMMU observation requires its separate serial-only profile')
    usb=profile.get('usb_pio',False)
    usb_probe=profile.get('usb_probe',False)
    if type(usb_probe) is not bool or usb_probe and not hps_context.usb_selection.contained_probe(
            dict(profile,program='usbcheck',usb_control=usb)):
        raise ValueError('USB probe requires its explicit contained boot selection')
    if (usb and profile.get('usb_input') not in ('boot-keyboard','gamepad') or
            not usb and profile.get('usb_input') is not None):
        raise ValueError('USB boot input must be explicitly selected')
    shared=profile.get('usb_shared',False)
    if type(shared) is not bool:raise ValueError('shared USB boot selection must be boolean')
    if shared:
        hps_context.usb_selection.mode(dict(profile,program='fpgaload',usb_control=usb))
        if not profile['sd_pio'] or any(profile.get(key) is None for key in
                ('hps_io','hps_control','hps_configuration','hps_reader_memory','hps_computer')):
            raise ValueError('shared USB requires sealed pins, SD and complete selected computer resources')
    if type(usb) is not bool or usb and (profile.get('hps_io') is None or smmu or not shared and (profile['sd_pio'] or
            any(profile.get(key) is not None for key in ('hps_control','hps_configuration','hps_reader_memory','hps_ddr_read','hps_window_read','hps_computer')))):
        raise ValueError('USB PIO requires its sealed pins and separate contained cold profile')
    control=profile.get('hps_control');extra='#define FES_NANO_CONTROL_PROBE '+str(int(control is not None))+'\n'
    extra+='#define FES_NANO_FIRMWARE_OCRAM '+str(int(ram is not None))+'\n'
    extra+='#define FES_NANO_CONTROLLED_USB '+str(int(usb))+'\n'
    extra+='#define FES_NANO_SHARED_USB '+str(int(shared))+'\n'
    io_map=profile.get('hps_io')
    extra+='#define FES_NANO_HPS_IO_OBSERVE '+str(int(io_map is not None))+'\n'
    if io_map is not None:
        if (profile.get('hps_context') is None or not shared and (profile['sd_pio'] or
                any(profile.get(key) is not None for key in ('hps_control','hps_configuration','hps_reader_memory','hps_ddr_read','hps_window_read','hps_computer'))) or
                profile.get('hps_io_sha256')!=hps_context.digest(io_map)):
            raise ValueError('HPS I/O observation requires its separate sealed serial-only context')
        hps_context.validate_hps_io(io_map,profile['hps_context'])
        extra+='#define FES_NANO_HPS_IO_SHA '+array(profile['hps_io_sha256'])+'\n'
        extra+='#define FES_NANO_HPS_IO_REGISTERS {'+','.join(
            '{'+hex(row['offset'])+'U,'+hex(row['value'])+'U,0xffffffffU}' for row in io_map['registers'])+'}\n'
    elif profile.get('hps_io_sha256') is not None:
        raise ValueError('HPS I/O digest has no complete observation map')
    if control is not None:
        context=profile['hps_context'];build=context['build_id']
        extra+=('#define FES_NANO_CONTROL_BASE '+hex(control['cpu_base'])+'UL\n'
                '#define FES_NANO_CONTROL_ABI '+hex(context['control_abi'])+'U\n'
                '#define FES_NANO_CONTROL_BUILD {'+','.join('0x'+build[i:i+8]+'U' for i in (24,16,8,0))+'}\n'
                '#define FES_NANO_CONTROL_MAP_SHA '+array(profile['hps_control_sha256'])+'\n')
    configuration=profile.get('hps_configuration')
    cpu_map=profile.get('cpu_mapping')
    if configuration is not None:
        expected=hps_context.cpu_mapping(control,configuration,profile.get('hps_reader_memory'),menu_program=companion)
        if cpu_map!=expected or hps_context.encode(cpu_map)!=hps_context.encode(expected) or profile.get('cpu_mapping_sha256')!=hps_context.digest(expected):
            raise ValueError('cold configuration requires its complete native CPU mapping contract')
    elif cpu_map is not None or profile.get('cpu_mapping_sha256') is not None:
        raise ValueError('native CPU mapping contract requires the cold configuration profile')
    if configuration is not None and (control is None or not profile['sd_pio']):
        raise ValueError('cold configuration requires the source-bound control and contained SD profile')
    extra+='#define FES_NANO_COLD_CONFIG '+str(int(configuration is not None))+'\n'
    if configuration is not None:
        extra+=('#define FES_NANO_CONFIG_BASE '+hex(configuration['base'])+'ULL\n'
                '#define FES_NANO_CONFIG_ARENA_SIZE '+str(configuration['size'])+'U\n'
                '#define FES_NANO_CONFIG_BYTES '+str(configuration['core_size'])+'U\n'
                '#define FES_NANO_CONFIG_CHUNK '+str(configuration['chunk_bytes'])+'U\n'
                '#define FES_NANO_CONFIG_SHA384 '+array(configuration['core_sha384'])+'\n'
                '#define FES_NANO_CONFIG_CHUNK_DIGESTS {'+','.join(array(value) for value in configuration['chunk_sha384'])+'}\n')
    reader_memory=profile.get('hps_reader_memory')
    menu=reader_memory is not None and reader_memory.get('scope')=='sealed-cold-menu-reader-memory-v1'
    if menu!=menu_present:
        raise ValueError('cold menu memory requires its complete selected presentation profile')
    if reader_memory is not None:
        window=reader_memory.get('scope')=='sealed-cold-window-reader-memory-v1'
        if (configuration is None or control is None or not profile['sd_pio'] or
                profile['hps_context']['programming_profile'] not in ((hps_context.MENU_PROFILE,) if menu else ('de25-embedded-hps-first-window-diagnostic-v1',) if window else
                    ('de25-embedded-hps-first-ddr-diagnostic-v1',*hps_context.ROM_PROFILES)) or
                type(profile.get('window_memory',False)) is not bool or profile.get('window_memory',False)!=window or
                profile.get('hps_reader_memory_sha256')!=hps_context.digest(reader_memory)):
            raise ValueError('cold reader memory requires its source-bound DDR configuration profile')
        hps_context.validate_reader_memory(profile['hps_context'],reader_memory)
        stop=control.get('fabric_stop')
        if stop!={'scope':'sealed-menu-master-stop-v1' if menu else 'sealed-window-master-stop-v1' if window else 'sealed-ddr-master-stop-v1','probe_cpu_base':0x20010100,
                  'probe_span':128,'maintenance':'held-reset-from-configuration'}:
            raise ValueError('cold DDR reader requires the sealed complete master stop map')
        extra+=('#define FES_NANO_READER_CPU_BASE '+hex(reader_memory['cpu_base'])+'ULL\n'
                '#define FES_NANO_READER_BUS_BASE '+hex(reader_memory['bus_base'])+'ULL\n'
                '#define FES_NANO_READER_BYTES '+str(reader_memory['bytes'])+'U\n'
                '#define FES_NANO_READER_ARENA_BYTES '+str(cpu_map['reader_bytes'])+'U\n'
                '#define FES_NANO_READER_MAP_SHA '+array(profile['hps_reader_memory_sha256'])+'\n'
                '#define FES_NANO_PROBE_CPU_BASE '+hex(stop['probe_cpu_base'])+'UL\n'
                '#define FES_NANO_PROBE_SPAN '+str(stop['probe_span'])+'U\n')
        if window or menu:
            if profile.get('hps_ddr_read') is not None or profile.get('hps_computer') is not None:
                raise ValueError('window memory cannot inherit a fixed-burst or computer command recipe')
            extra+=('#define FES_NANO_WINDOW_MEMORY 1\n'
                    '#define FES_NANO_WINDOW_FRAME_BYTES '+str(reader_memory['frame_bytes'])+'U\n'
                    '#define FES_NANO_WINDOW_ROW_BYTES '+str(reader_memory['row_bytes'])+'U\n'
                    '#define FES_NANO_WINDOW_STRIDE '+str(reader_memory['stride'])+'U\n'
                    '#define FES_NANO_WINDOW_ROWS '+str(reader_memory['rows'])+'U\n')
            if menu:
                extra+=('#define FES_NANO_MENU_MEMORY 1\n'
                        '#define FES_NANO_MENU_SLOTS '+str(reader_memory['slots'])+'U\n'
                        '#define FES_NANO_MENU_SLOT_BYTES '+str(reader_memory['slot_bytes'])+'U\n')
        else:extra+='#define FES_NANO_READER_PATTERN_SHA '+array(reader_memory['pattern_sha256'])+'\n'
    elif profile.get('window_memory',False) is not False:
        raise ValueError('window memory requires its complete compiled reservation')
    ddr_read=profile.get('hps_ddr_read')
    if ddr_read is not None:
        if (reader_memory is None or profile.get('hps_ddr_read_sha256')!=hps_context.digest(ddr_read) or
                set(ddr_read)!={'format','scope','package_id','build_id','reader_memory_sha256','control_map_sha256',
                    'read_bytes','open_mask','close_mask','adapter_source_sha256','adapter_generated_sha256','arcache','arprot','aruser'} or
                ddr_read['format']!=1 or ddr_read['scope']!='sealed-cold-ddr-read-v1' or
                ddr_read['package_id']!=profile['hps_context']['package_id'] or
                ddr_read['build_id']!=profile['hps_context']['build_id'] or
                ddr_read['reader_memory_sha256']!=profile['hps_reader_memory_sha256'] or
                ddr_read['control_map_sha256']!=profile['hps_control_sha256'] or
                any(type(ddr_read[k]) is not int or ddr_read[k]!=v for k,v in
                    {'format':1,'read_bytes':32,'open_mask':8,'close_mask':10,'arcache':2,'arprot':3,'aruser':224}.items()) or
                any(not isinstance(ddr_read[k],str) or len(ddr_read[k])!=64 or
                    any(c not in '0123456789abcdef' for c in ddr_read[k]) or int(ddr_read[k],16)==0
                    for k in ('adapter_source_sha256','adapter_generated_sha256'))):
            raise ValueError('active cold DDR read requires its complete sealed adapter contract')
        extra+='#define FES_NANO_DDR_READ_ATTRS_VERIFIED 1\n'
        extra+='#define FES_NANO_DDR_READ_MAP_SHA '+array(profile['hps_ddr_read_sha256'])+'\n'
    extra+='#define FES_NANO_DDR_READ_DIAGNOSTIC '+str(int(ddr_read is not None))+'\n'
    window_read=profile.get('hps_window_read')
    if (type(profile.get('window_read',False)) is not bool or
            profile.get('window_read',False)!=(window_read is not None)):
        raise ValueError('window read requires its explicit native selection')
    if window_read is not None:
        if (profile.get('window_memory') is not True or reader_memory is None or
                profile.get('hps_window_read_sha256')!=hps_context.digest(window_read) or ddr_read is not None or
                profile.get('hps_computer') is not None):
            raise ValueError('window access requires its complete separate cold profile')
        hps_context.validate_window_read(profile['hps_context'],reader_memory,profile['hps_control_sha256'],window_read)
        extra+='#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1\n'
        extra+='#define FES_NANO_WINDOW_READ_MAP_SHA '+array(profile['hps_window_read_sha256'])+'\n'
    menu_read=profile.get('hps_menu_read')
    if menu_read is not None:
        if not menu or profile.get('hps_menu_read_sha256')!=hps_context.digest(menu_read):
            raise ValueError('cold menu read requires its complete selected contract')
        hps_context.validate_menu_read(profile['hps_context'],reader_memory,profile['hps_control_sha256'],menu_read)
        extra+='#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1\n'
        extra+='#define FES_NANO_WINDOW_READ_MAP_SHA '+array(profile['hps_menu_read_sha256'])+'\n'
    elif menu or profile.get('hps_menu_read_sha256') is not None:
        raise ValueError('cold menu access requires its complete compiled adapter')
    if companion is not None:
        m=companion['memory']
        extra+='#define FES_NANO_PROGRAM_ROLES 1\n#define FES_NANO_WINDOW_MEMORY 1\n#define FES_NANO_MENU_MEMORY 1\n'
        for field,name in (('slots','MENU_SLOTS'),('slot_bytes','MENU_SLOT_BYTES'),('frame_bytes','WINDOW_FRAME_BYTES'),
                           ('row_bytes','WINDOW_ROW_BYTES'),('stride','WINDOW_STRIDE'),('rows','WINDOW_ROWS')):
            extra+='#define FES_NANO_'+name+' '+str(m[field])+'U\n'
        extra+='#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1\n'
        extra+='#define FES_NANO_WINDOW_READ_MAP_SHA '+array(hps_context.digest(companion['access']))+'\n'
    extra+='#define FES_NANO_WINDOW_READ_DIAGNOSTIC '+str(int(window_read is not None or menu or companion is not None))+'\n'
    present=profile.get('window_present',False)
    if type(present) is not bool or present and window_read is None:
        raise ValueError('window presentation requires its complete selected read path')
    extra+='#define FES_NANO_WINDOW_PRESENT_DIAGNOSTIC '+str(int(present or companion is not None))+'\n'
    window_storage_return=profile.get('window_storage_return',False)
    if type(window_storage_return) is not bool or window_storage_return and (not present or profile.get('sd_pio') is not True):
        raise ValueError('window storage return requires presentation and controlled SD')
    extra+='#define FES_NANO_WINDOW_STORAGE_RETURN '+str(int(window_storage_return or companion is not None))+'\n'
    window_reload=profile.get('window_reload',False)
    if type(window_reload) is not bool or window_reload and not window_storage_return:
        raise ValueError('window reload requires the selected window SD-return path')
    window_service=profile.get('window_service',False)
    if type(window_service) is not bool or window_service and (not window_storage_return or window_reload):
        raise ValueError('window service requires selected window SD return and excludes bounded reload')
    tenfoot_serial=profile.get('tenfoot_serial',False)
    if type(tenfoot_serial) is not bool or tenfoot_serial and (profile.get('selected_programs') is not True or
            profile.get('computer_reload') is not True or profile.get('computer_storage_return') is not True or
            profile.get('hps_computer') is None or profile.get('window_memory',False) is not False):
        raise ValueError('serial Tenfoot requires the selected computer/SD-return path')
    computer=profile.get('hps_computer')
    if computer is not None:
        if (not isinstance(computer,dict) or configuration is None or control is None or ddr_read is not None or not profile['sd_pio'] or
                profile.get('hps_computer_sha256')!=hps_context.digest(computer) or
                set(computer)!={'format','scope','package_id','build_id','control_map_sha256','handoff_sha256',
                    'cpu_base','span','abi_minor','capabilities','hps_master','endpoint_sha256','wrapper_sha256'} or
                computer['scope']!='sealed-cold-computer-control-v1' or computer['hps_master'] is not False or
                computer['package_id']!=profile['hps_context']['package_id'] or computer['build_id']!=profile['hps_context']['build_id'] or
                computer['control_map_sha256']!=profile['hps_control_sha256'] or computer['handoff_sha256']!=control['handoff_sha256'] or
                any(type(computer[k]) is not int or computer[k]!=v for k,v in
                    {'format':1,'cpu_base':0x20010300,'span':32,'abi_minor':0,'capabilities':0x803}.items()) or
                any(not isinstance(computer[k],str) or len(computer[k])!=64 or
                    any(c not in '0123456789abcdef' for c in computer[k]) or int(computer[k],16)==0
                    for k in ('endpoint_sha256','wrapper_sha256'))):
            raise ValueError('cold computer control requires its separate complete sealed internal-RAM contract')
        extra+='#define FES_NANO_COMPUTER_MAP_SHA '+array(profile['hps_computer_sha256'])+'\n'
    extra+='#define FES_NANO_COLD_COMPUTER '+str(int(computer is not None))+'\n'
    rom=profile.get('hps_rom')
    rom_profile=hps_context.uses_held_rom(profile.get('hps_context') or {})
    if rom is not None:
        if (not rom_profile or computer is None or reader_memory is None or
                profile.get('hps_rom_sha256')!=hps_context.digest(rom)):
            raise ValueError('cold ROM requires its complete selected computer, memory and resource contract')
        hps_context.validate_rom(profile['hps_context'],control,computer,rom)
        extra+=('#define FES_NANO_ROM_CPU_BASE '+hex(rom['cpu_base'])+'UL\n'
                '#define FES_NANO_ROM_BYTES '+str(rom['bytes'])+'U\n'
                '#define FES_NANO_ROM_MAP_SHA '+array(profile['hps_rom_sha256'])+'\n')
    elif rom_profile and not usb_probe or profile.get('hps_rom_sha256') is not None:
        raise ValueError('cold ROM producer requires its complete ROM resource contract')
    extra+='#define FES_NANO_COLD_ROM '+str(int(rom is not None))+'\n'
    reload=profile.get('computer_reload',False)
    if type(reload) is not bool or reload and computer is None:
        raise ValueError('computer reload requires its selected sealed computer contract')
    extra+='#define FES_NANO_COMPUTER_RELOAD '+str(int(reload))+'\n'
    storage_return=profile.get('computer_storage_return',False)
    if type(storage_return) is not bool or storage_return and (not reload or not profile['sd_pio']):
        raise ValueError('computer storage return requires its selected two-load owned SD profile')
    extra+='#define FES_NANO_COMPUTER_STORAGE_RETURN '+str(int(storage_return))+'\n'
    extra+=initial_tape_header(profile)
    extra+=sdm_probe.header(profile)
    extra+=sdm_fault.header(profile)
    extra+=sdm_input.header(profile)
    extra+=fit_layout.header(profile.get('boot_fit'))
    return ('/* Generated, same source-bound diagnostic chain in SPL and BL31. */\n'
            +extra+
            ('#define FES_NANO_FIRMWARE_MMU 1\n' if profile.get('firmware_mmu') is not None else '')+
            '#define FES_NANO_CAPABILITY_DIAGNOSTIC '+str(int(capability_contract is not None))+'\n'+
            '#define FES_NANO_SMMU_BOOT_OBSERVE '+str(int(smmu))+'\n'
            '#define FES_NANO_CONTROLLED_SD '+str(int(profile['sd_pio']))+'\n'
            '#define FES_NANO_COLD_CHAIN '+array(profile['chain_sha256'])+'\n'
            '#define FES_NANO_COLD_OWNER_SEED '+hex(profile['owner_seed'])+'ULL\n'
            '#define FES_NANO_HPS_CONTEXT_SHA '+(array(profile['hps_context_sha256']) if profile.get('hps_context_sha256') else '{0}')+'\n'
            '#define FES_NANO_FIT_PATH "'+EXPERIMENTAL_FILE+'"\n'
            '#define FES_NANO_COLD_ENTRY_SHA '+array(profile['entry_image_sha256'])+'\n'
            '#define FES_NANO_COLD_ENTRY_SIZE '+str(profile['entry_image_size'])+'UL\n')
