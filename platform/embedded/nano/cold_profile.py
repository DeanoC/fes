"""One source-bound cold/serial chain identity shared by SPL and BL31."""
import hashlib
import json
from pathlib import Path

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
FW=HERE.parent/'firmware'
EXPERIMENTAL_FILE='FES/UPLOAD.BIN'
MAX_FIT_SIZE=8<<20


def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def entry(entry_build):
    receipt_path=entry_build/'result.json';receipt=json.loads(receipt_path.read_text())
    image=entry_build/'entry.img';profile=HERE/'probe-profile.json'
    if (receipt.get('status')!='built' or receipt.get('target')!='de25-nano' or
            receipt.get('entry_image_address')!=0x96080000 or receipt.get('ram_start')!=0x84000000 or
            receipt.get('ram_end')!=0x94000000 or receipt.get('uart_base')!=0x10c02100 or
            receipt.get('entry_image_sha256')!=sha(image) or receipt.get('entry_image_size')!=image.stat().st_size or
            not 0<image.stat().st_size<=32<<20 or receipt.get('firmware_profile_sha256')!=sha(profile)):
        raise ValueError('entry image is not bound to the source-derived serial-only Nano profile')
    for name,expected in receipt['sources'].items():
        if sha(ROOT/name)!=expected:raise ValueError('entry producer source changed: '+name)
    return image,receipt_path


def sources():
    return [HERE/name for name in ('cold_profile.py','spl_cold.c','prepare_uboot.py','uboot.c','handoff.c','handoff.h','probe-profile.json')]+[
        FW/name for name in ('nano_cold.h','nano_cold.c','nano_cold_mmio.inc','nano_cold_platform.c','nano_boot.h',
                            'nano_sd.h','nano_sd.c','generated/fes_embedded_sd.h',
                            'nano_boot.c','nano_bridges.h','nano_bridges.c','nano_bridge_platform.inc','nano_bridge_platform.h',
                            'nano_owned_sip.inc','prepare_nano_owned.py','prepare_tfa.py',
                            'nano_bridge_sip.c','nano_bridge_sip.h','generated/fes_embedded_bridges.h',
                            'generated/fes_embedded_control.hpp','nano_bridge_diag.h','nano_bridge_diag.inc',
                            'build_terasic.py','build_terasic_uboot.py','verify.py')]


def make(entry_build):
    image,receipt=entry(entry_build)
    inputs={str(p.relative_to(ROOT)):sha(p) for p in sources()}
    identity={'format':1,'profile':'cold-serial-preloaded','experimental_fit_path':EXPERIMENTAL_FILE,'entry_image_sha256':sha(image),
              'entry_image_size':image.stat().st_size,'producer_sources':inputs,
              'uboot_pin':'d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413',
              'tfa_pin':'580dcb5c4e10aa7daff2bf69d493c076a6d3e665',
              'reference_sof_sha256':'e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09'}
    identity['chain_sha256']=hashlib.sha256(json.dumps(identity,sort_keys=True).encode()).hexdigest()
    identity['owner_seed']=int(identity['chain_sha256'][:16],16)|1
    identity['entry_receipt_sha256']=sha(receipt)
    return identity


def header(profile):
    def array(value):return '{'+','.join('0x'+value[i:i+2] for i in range(0,len(value),2))+'}'
    return ('/* Generated, same source-bound diagnostic chain in SPL and BL31. */\n'
            '#define FES_NANO_COLD_CHAIN '+array(profile['chain_sha256'])+'\n'
            '#define FES_NANO_COLD_OWNER_SEED '+hex(profile['owner_seed'])+'ULL\n'
            '#define FES_NANO_FIT_PATH "'+EXPERIMENTAL_FILE+'"\n'
            '#define FES_NANO_COLD_ENTRY_SHA '+array(profile['entry_image_sha256'])+'\n'
            '#define FES_NANO_COLD_ENTRY_SIZE '+str(profile['entry_image_size'])+'UL\n')
