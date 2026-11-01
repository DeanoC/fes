#!/usr/bin/env python3
"""Prepare the matched cold serial SOF and an experimental FIT on a recovery card.

Regular files only. The recovery root FIT/raw prefix remain exact; the native
SPL selects and seals FES/UPLOAD.BIN. No flash, device, board or automatic update.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import struct
import subprocess

from cold_profile import EXPERIMENTAL_FILE, MAX_FIT_SIZE, entry as checked_entry, selected_profile
from upload import load_kit

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
VENDOR_SOF='e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09'
PARTITION_SECTOR,PARTITION_SECTORS=32768,262144


def sha(path):
    with path.open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()


def checked_native(uboot,entry):
    """Reuse the complete native FIT/SPL/entry/source checks for file composition."""
    fit,spl,image=uboot/'build/u-boot.itb',uboot/'build/spl/u-boot-spl-dtb.hex',entry/'entry.img'
    u=json.loads((uboot/'result.json').read_text());e=json.loads((entry/'result.json').read_text())
    checked_entry(entry)
    profile=selected_profile(e)
    native=u.get('native_handoff',{});config=(uboot/'build/.config').read_text()
    if (u.get('status')!='built' or u.get('bl31_bridge_mode')!='owned-cold-serial' or
            u.get('fit_sha256')!=sha(fit) or u.get('spl_sha256')!=sha(spl) or not u.get('fit_atf_verified') or
            not 0<fit.stat().st_size<=MAX_FIT_SIZE or not native.get('preloaded_entry') or
            native.get('sealed_fit_sha256')!=sha(fit) or native.get('sealed_fit_size')!=fit.stat().st_size or
            native.get('experimental_fit_path')!=EXPERIMENTAL_FILE or
            native.get('entry_image_sha256')!=sha(image) or native.get('entry_receipt_sha256')!=sha(entry/'result.json') or
            native.get('bl31_sha256')!=u.get('bl31_sha256') or native.get('profile_sha256')!=sha(profile) or
            u.get('fit_components',{}).get('fes-entry',{}).get('sha256')!=sha(image) or
            e.get('entry_image_sha256')!=sha(image) or e.get('target')!='de25-nano' or
            sha(uboot/'build/.config')!=u.get('config_sha256') or
            u.get('producer_sha256')!=sha(HERE.parent/'firmware/build_terasic_uboot.py')):
        raise ValueError('native FIT/SPL/entry/firmware seal differs from matched receipts')
    from prepare_uboot import DISABLE,ENABLE
    if (any('CONFIG_'+name+'=y\n' in config for name in DISABLE) or
            any('CONFIG_'+name+'=y\n' not in config for name in ENABLE) or
            'CONFIG_SPL_FS_LOAD_PAYLOAD_NAME="'+EXPERIMENTAL_FILE+'"\n' not in config or
            'CONFIG_SYS_MMCSD_FS_BOOT_PARTITION=1\n' not in config):
        raise ValueError('native SPL file, PIO or excluded-device configuration differs')
    for name,digest in native['producer_sources'].items():
        if sha(ROOT/name)!=digest:raise ValueError('native source changed: '+name)
    return fit,spl,image,u,e,profile


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('uboot-build','entry-build','recovery-kit','vendor-sof','quartus-pfg','output'):
        parser.add_argument('--'+name,required=True,type=Path)
    args=parser.parse_args();output=args.output.absolute()
    if (output.exists() or not output.is_relative_to(ROOT/'out') or '..' in output.parts or
            any(p.is_symlink() for p in (output,*output.parents)) or any(c in str(output) for c in ',@')):
        parser.error('output must be fresh and private below out/, without commas or @')
    uboot,entry,recovery=(p.resolve(strict=True) for p in (args.uboot_build,args.entry_build,args.recovery_kit))
    vendor,pfg=(p.resolve(strict=True) for p in (args.vendor_sof,args.quartus_pfg))
    base=load_kit(recovery);base_record=json.loads((recovery/'result.json').read_text())
    if sha(recovery/'network-recovery.sof')!=base_record['sof_sha256']:raise ValueError('recovery SOF differs')
    if sha(vendor)!=VENDOR_SOF:raise ValueError("recovery reference SOF differs")
    fit,spl,image,u,e,profile=checked_native(uboot,entry)
    native=u["native_handoff"]
    if native.get('cold_boot_profile',{}).get('hps_context') is not None:
        raise ValueError('source-bound HPS-first native chain requires hps_boot.py and its sealed FPGA project')
    tools={name:Path(shutil.which(name) or '').absolute() for name in ('mcopy',)}
    if any(not p.is_file() for p in tools.values()):raise ValueError('mtools required')
    inputs={str(p):sha(p) for p in (fit,spl,image,vendor,pfg,uboot/'result.json',entry/'result.json',uboot/'build/.config',
            recovery/'result.json',recovery/'nano-recovery-sd.img',recovery/'network-recovery.sof',
            recovery/'u-boot.itb',recovery/'RECOVERY.JSON',Path(__file__),HERE/'sof_hps.py',HERE/'upload.py',HERE/'upload_transport.py')}
    inputs.update({str(ROOT/name):digest for name,digest in native['producer_sources'].items()})
    inputs.update({str(p):sha(p) for p in tools.values()})
    output.mkdir(parents=True);commands=[]
    def run(label,argv):
        argv=[str(x) for x in argv];commands.append(argv)
        with (output/(label+'.log')).open('wb') as log:
            subprocess.run(argv,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=120)
    for src,name in ((fit,'u-boot.itb'),(spl,'u-boot-spl-dtb.hex'),(image,'ENTRY.IMG'),(vendor,'recovery-reference.sof'),
                     (recovery/'network-recovery.sof','network-recovery.sof')):
        shutil.copyfile(src,output/name)
    candidate=output/'native-probe.sof'
    run('pfg-combine',[pfg,'-c',vendor,candidate,'-o','hps_path='+str(spl)])
    run('pfg-info',[pfg,'-i',candidate]);info=(output/'pfg-info.log').read_text(encoding='latin-1')
    if 'Device name: A5EB013BB23BE4SCS' not in info or 'HPS/FPGA configuration order: HPS_AFTER_INIT_DONE' not in info:
        raise ValueError('unexpected Nano device or configuration order')
    from sof_hps import inspect
    embedded_spl=inspect(candidate.read_bytes(),spl.read_bytes(),vendor.read_bytes())
    manifest={'format':2,'classification':'matched cold serial candidate and recovery card; hardware unqualified',
        'board_serial':'TRWU8CBL','native_entry_sha256':sha(image),'fit_sha256':sha(fit),'spl_sha256':sha(spl),
        'bl31_sha256':u['bl31_sha256'],'sof_sha256':sha(candidate),'recovery_reference_sof_sha256':VENDOR_SOF,
        'network_recovery_sof_sha256':base_record['sof_sha256'],'base_recovery_image_sha256':base['image_sha256'],
        'recovery_fit_sha256':base['fit_sha256'],'protected_prefix_sha256':base['prefix_sha256'],
        'profile_sha256':sha(profile),'native_handoff':native,'experimental_fit_path':EXPERIMENTAL_FILE,
        'native_entry_delivery':'sealed FIT loadable at 0x96080000; no BL33 SD access',
        'fat32_start_sector':PARTITION_SECTOR,'fat32_sectors':PARTITION_SECTORS,'embedded_spl_independently_decoded':True,
        'embedded_spl_identity':embedded_spl,
        'limitations':['cold serial source-bound candidate; not signed boot or hardware acceptance',
                       'no active USB/Ethernet/FPGA provider; controlled read-only native SD remains diagnostic',
                       'no atomic FAT update or durability/rollback guarantee through power loss',
                       'only regular files prepared; no board, flash or card touched']}
    (output/'BOOTCHAIN.JSON').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    disk=output/'nano-probe-sd.img';shutil.copyfile(recovery/'nano-recovery-sd.img',disk)
    fat=str(disk)+'@@'+str(PARTITION_SECTOR*512)
    for src,target in ((fit,'::'+EXPERIMENTAL_FILE),(output/'BOOTCHAIN.JSON','::FES/BOOTCHAIN.JSON')):
        run('fat-write-'+src.name,[tools['mcopy'],'-i',fat,src,target])
        readback=output/(src.name+'.readback')
        run('fat-read-'+src.name,[tools['mcopy'],'-i',fat,target,readback])
        if sha(src)!=sha(readback):raise ValueError('native file readback differs')
    for target,expected in (('::u-boot.itb',base['fit_sha256']),('::FES/RECOVERY.JSON',hashlib.sha256(base['manifest']).hexdigest())):
        readback=output/('preserved-'+target.rsplit('/',1)[-1])
        run('preserve-'+readback.name,[tools['mcopy'],'-i',fat,target,readback])
        if sha(readback)!=expected:raise ValueError('recovery boot file changed')
    with disk.open('rb') as stream:
        if hashlib.sha256(stream.read(PARTITION_SECTOR*512)).hexdigest()!=base['prefix_sha256']:
            raise ValueError('protected recovery prefix changed')
        stream.seek(PARTITION_SECTOR*512);boot=stream.read(512)
        if (struct.unpack_from('<H',boot,11)[0]!=512 or boot[510:]!=b'\x55\xaa' or
                struct.unpack_from('<I',boot,28)[0]!=PARTITION_SECTOR or struct.unpack_from('<I',boot,32)[0]!=PARTITION_SECTORS):
            raise ValueError('FAT geometry changed')
        backup=struct.unpack_from('<H',boot,50)[0]
        if not 0<backup<struct.unpack_from('<H',boot,14)[0]:raise ValueError('invalid FAT backup')
        stream.seek((PARTITION_SECTOR+backup)*512)
        if stream.read(512)!=boot:raise ValueError('FAT backup changed')
    if any(sha(Path(name))!=digest for name,digest in inputs.items()):raise ValueError('packaging inputs changed')
    manifest.update(status='prepared',sd_image_sha256=sha(disk),sd_image_size=disk.stat().st_size,inputs=inputs,commands=commands)
    (output/'result.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    print('Prepared matched native/recovery files:',output)


if __name__=='__main__':
    try:main()
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:raise SystemExit('Nano boot-chain packaging: '+str(error))
