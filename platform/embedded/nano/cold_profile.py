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
EXPERIMENTAL_FILE='FES/UPLOAD.BIN'
MAX_FIT_SIZE=8<<20


def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def selected_profile(receipt):
    for name in ('probe-profile.json','sd-profile.json'):
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
    if (receipt.get('status')!='built' or receipt.get('target')!='de25-nano' or
            receipt.get('entry_image_address')!=0x96080000 or receipt.get('ram_start')!=0x84000000 or
            receipt.get('ram_end')!=0x94000000 or receipt.get('uart_base')!=0x10c02100 or
            receipt.get('entry_image_sha256')!=sha(image) or receipt.get('entry_image_size')!=image.stat().st_size or
            not 0<image.stat().st_size<=32<<20 or receipt.get('firmware_profile_sha256')!=sha(profile)):
        raise ValueError('entry image is not bound to the source-derived cold Nano profile')
    check_sources(receipt.get('native_build_sources'))
    for name,expected in receipt['sources'].items():
        if sha(ROOT/name)!=expected:raise ValueError('entry producer source changed: '+name)
    context=hps_context.checked(receipt,program_key='native_program')
    hps_context.checked_control(receipt,program_key='native_program')
    configuration=hps_context.checked_configuration(receipt,program_key='native_program')
    if context is not None and profile.name!='probe-profile.json':
        raise ValueError('HPS context diagnostic requires the contained serial profile')
    if configuration is not None and [configuration['base'],configuration['base']+configuration['size']] not in receipt.get('staging_exclusions',[]):
        raise ValueError('cold configuration arena is not excluded from entry staging')
    return image,receipt_path


def sources():
    return [HERE/name for name in ('cold_profile.py','hps_context.py','spl_cold.c','prepare_uboot.py','uboot.c','handoff.c','handoff.h','probe-profile.json','sd-profile.json')]+[
        FW/name for name in ('nano_cold.h','nano_cold.c','nano_cold_mmio.inc','nano_cold_platform.c','nano_boot.h',
                            'nano_sd.h','nano_sd.c','nano_sd_platform.h','nano_sd_platform.c','generated/fes_embedded_sd.h',
                            'nano_boot.c','nano_bridges.h','nano_bridges.c','nano_bridge_platform.inc','nano_bridge_platform.h',
                            'nano_owned_sip.inc','prepare_nano_owned.py','prepare_tfa.py','nano_digest.py',
                            'nano_bridge_sip.c','nano_bridge_sip.h','generated/fes_embedded_bridges.h',
                            'nano_identity.c','nano_identity.h','nano_configuration.c','nano_configuration.h','generated/fes_embedded_control.h',
                            'generated/fes_embedded_control.hpp','nano_bridge_diag.h','nano_bridge_diag.inc',
                            'build_terasic.py','build_terasic_uboot.py','verify.py')]


def make(entry_build):
    image,receipt=entry(entry_build)
    build=json.loads(receipt.read_text());profile=selected_profile(build)
    context=hps_context.checked(build,program_key='native_program')
    control=hps_context.checked_control(build,program_key='native_program')
    configuration=hps_context.checked_configuration(build,program_key='native_program')
    selected=sources()+ (hps_context.inputs() if context is not None else [])
    inputs={str(p.relative_to(ROOT)):sha(p) for p in selected}
    identity={'format':1,'profile':'cold-sd-pio-preloaded' if profile.name=='sd-profile.json' else 'cold-serial-preloaded',
              'sd_pio':profile.name=='sd-profile.json','firmware_profile_sha256':sha(profile),
              'native_sources_sha256':hashlib.sha256(json.dumps(build['native_build_sources'],sort_keys=True).encode()).hexdigest(),
              'experimental_fit_path':EXPERIMENTAL_FILE,'entry_image_sha256':sha(image),
              'entry_image_size':image.stat().st_size,'producer_sources':inputs,
              'uboot_pin':'d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413',
              'tfa_pin':'580dcb5c4e10aa7daff2bf69d493c076a6d3e665',
              'reference_sof_sha256':context['raw_sof_sha256'] if context else 'e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09',
              'hps_context':context,'hps_context_sha256':hps_context.digest(context) if context else None,
              'hps_control':control,'hps_control_sha256':hps_context.digest(control) if control else None,
              'hps_configuration':configuration,'hps_configuration_sha256':hps_context.digest(configuration) if configuration else None}
    identity['chain_sha256']=hashlib.sha256(json.dumps(identity,sort_keys=True).encode()).hexdigest()
    identity['owner_seed']=int(identity['chain_sha256'][:16],16)|1
    identity['entry_receipt_sha256']=sha(receipt)
    return identity


def header(profile):
    def array(value):return '{'+','.join('0x'+value[i:i+2] for i in range(0,len(value),2))+'}'
    control=profile.get('hps_control');extra='#define FES_NANO_CONTROL_PROBE '+str(int(control is not None))+'\n'
    if control is not None:
        context=profile['hps_context'];build=context['build_id']
        extra+=('#define FES_NANO_CONTROL_BASE '+hex(control['cpu_base'])+'UL\n'
                '#define FES_NANO_CONTROL_ABI '+hex(context['control_abi'])+'U\n'
                '#define FES_NANO_CONTROL_BUILD {'+','.join('0x'+build[i:i+8]+'U' for i in (24,16,8,0))+'}\n'
                '#define FES_NANO_CONTROL_MAP_SHA '+array(profile['hps_control_sha256'])+'\n')
    configuration=profile.get('hps_configuration')
    if configuration is not None and (control is None or profile['sd_pio']):
        raise ValueError('cold configuration requires the contained control profile without SD')
    extra+='#define FES_NANO_COLD_CONFIG '+str(int(configuration is not None))+'\n'
    if configuration is not None:
        extra+=('#define FES_NANO_CONFIG_BASE '+hex(configuration['base'])+'ULL\n'
                '#define FES_NANO_CONFIG_ARENA_SIZE '+str(configuration['size'])+'U\n'
                '#define FES_NANO_CONFIG_BYTES '+str(configuration['core_size'])+'U\n'
                '#define FES_NANO_CONFIG_CHUNK '+str(configuration['chunk_bytes'])+'U\n'
                '#define FES_NANO_CONFIG_SHA384 '+array(configuration['core_sha384'])+'\n'
                '#define FES_NANO_CONFIG_CHUNK_DIGESTS {'+','.join(array(value) for value in configuration['chunk_sha384'])+'}\n')
    return ('/* Generated, same source-bound diagnostic chain in SPL and BL31. */\n'
            +extra+
            '#define FES_NANO_CONTROLLED_SD '+str(int(profile['sd_pio']))+'\n'
            '#define FES_NANO_COLD_CHAIN '+array(profile['chain_sha256'])+'\n'
            '#define FES_NANO_COLD_OWNER_SEED '+hex(profile['owner_seed'])+'ULL\n'
            '#define FES_NANO_HPS_CONTEXT_SHA '+(array(profile['hps_context_sha256']) if profile.get('hps_context_sha256') else '{0}')+'\n'
            '#define FES_NANO_FIT_PATH "'+EXPERIMENTAL_FILE+'"\n'
            '#define FES_NANO_COLD_ENTRY_SHA '+array(profile['entry_image_sha256'])+'\n'
            '#define FES_NANO_COLD_ENTRY_SIZE '+str(profile['entry_image_size'])+'UL\n')
