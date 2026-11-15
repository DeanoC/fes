#!/usr/bin/env python3
"""Create a network recovery SOF and regular-file test-card image, off-board."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import struct
import subprocess

import recovery

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
VENDOR_SOF = 'e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09'
FIT_SECTOR, EXPERIMENT_SECTOR, FAT_SECTOR, FAT_SECTORS = 512, 16384, 32768, 262144


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def checked_build(directory):
    receipt = json.loads((directory / 'result.json').read_text())
    fit, spl, config = (directory / name for name in
                       ('build/u-boot.itb', 'build/spl/u-boot-spl-dtb.hex', 'build/.config'))
    mode = receipt.get('network_recovery', {})
    phy_sources=mode.get('phy_sources')
    recovery.check_phy_sources(directory/'source',phy_sources)
    if (receipt.get('status') != 'built' or receipt.get('native_handoff') is not None or
            mode != {'profile_sha256': sha(HERE / 'recovery.py'), 'manual_prompt': True,
                     'automatic_writes': False, 'random_mac': False, 'sd_dma': False, 'native_handoff': False,
                     'phy':recovery.PHY, 'phy_driver_linked':True, 'phy_sources':phy_sources} or
            receipt.get('producer_sha256') != sha(HERE.parent / 'firmware/build_terasic_uboot.py') or
            receipt.get('uboot_elf_sha256') != sha(directory/'build/u-boot') or
            receipt.get('fit_sha256') != sha(fit) or receipt.get('spl_sha256') != sha(spl) or
            receipt.get('config_sha256') != sha(config) or receipt.get('fit_atf_verified') is not True or
            not fit.stat().st_size or fit.stat().st_size > (EXPERIMENT_SECTOR - FIT_SECTOR) * 512):
        raise ValueError('not a current separate Nano recovery build')
    text = config.read_text()
    recovery.check(text)
    if ('U-Boot 2025.01'+recovery.LOCALVERSION+' (').encode() not in (directory/'build/u-boot').read_bytes():
        raise ValueError('actual recovery ELF does not contain its selected version banner')
    recovery.check_linked(subprocess.check_output(['readelf','-Ws',directory/'build/u-boot'],text=True,timeout=30))
    if 'CONFIG_SYS_MMCSD_RAW_MODE_U_BOOT_SECTOR=0x200\n' not in text:
        raise ValueError('recovery SPL raw FIT offset differs')
    # spl_soc64.c selects filesystem mode whenever SPL_FS_FAT is enabled.
    # The raw-sector setting alone does not describe the effective boot path.
    for setting in ('CONFIG_SPL_FS_FAT=y', 'CONFIG_SYS_MMCSD_FS_BOOT=y',
                    'CONFIG_SYS_MMCSD_FS_BOOT_PARTITION=1',
                    'CONFIG_SPL_FS_LOAD_PAYLOAD_NAME="u-boot.itb"'):
        if setting + '\n' not in text:
            raise ValueError('recovery SPL FAT boot path differs: ' + setting)
    return receipt, fit, spl, config


def make_card(output, fit, tools, run):
    disk = output/'nano-recovery-sd.img'
    with disk.open('xb') as stream:stream.truncate((FAT_SECTOR+FAT_SECTORS)*512)
    mbr=bytearray(512);mbr[446:462]=struct.pack('<B3sB3sII',0,b'\xfe\xff\xff',0x0c,b'\xfe\xff\xff',FAT_SECTOR,FAT_SECTORS);mbr[510:]=b'\x55\xaa'
    with disk.open('r+b') as stream:
        stream.write(mbr);stream.seek(FIT_SECTOR*512);stream.write(fit.read_bytes())
    fat=str(disk)+'@@'+str(FAT_SECTOR*512)
    run('fat-format',[tools['mformat'],'-i',fat,'-F','-H',FAT_SECTOR,'-v','FESRECOVERY','::'])
    run('fat-dir',[tools['mmd'],'-i',fat,'::FES'])
    run('fat-boot',[tools['mcopy'],'-i',fat,fit,'::u-boot.itb'])
    run('fat-boot-readback',[tools['mcopy'],'-i',fat,'::u-boot.itb',output/'u-boot.itb.readback'])
    if sha(fit) != sha(output/'u-boot.itb.readback'):
        raise ValueError('FAT boot FIT readback differs')
    run('fat-manifest',[tools['mcopy'],'-i',fat,output/'RECOVERY.JSON','::FES/RECOVERY.JSON'])
    run('fat-readback',[tools['mcopy'],'-i',fat,'::FES/RECOVERY.JSON',output/'RECOVERY.readback'])
    if sha(output/'RECOVERY.JSON') != sha(output/'RECOVERY.readback'):
        raise ValueError('recovery manifest readback differs')
    with disk.open('rb') as stream:
        if stream.read(512) != mbr:raise ValueError('recovery MBR changed')
        stream.seek(FIT_SECTOR*512)
        if stream.read(fit.stat().st_size) != fit.read_bytes():raise ValueError('raw recovery FIT differs')
        stream.seek(FAT_SECTOR*512);boot=stream.read(512)
        if (struct.unpack_from('<H',boot,11)[0] != 512 or boot[510:] != b'\x55\xaa' or
                struct.unpack_from('<I',boot,28)[0] != FAT_SECTOR or struct.unpack_from('<I',boot,32)[0] != FAT_SECTORS):
            raise ValueError('recovery FAT geometry differs')
        backup=struct.unpack_from('<H',boot,50)[0]
        if not 0 < backup < struct.unpack_from('<H',boot,14)[0]:raise ValueError('invalid FAT backup')
        stream.seek((FAT_SECTOR+backup)*512)
        if stream.read(512) != boot:raise ValueError('FAT backup differs')
    return disk


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--uboot-build', required=True, type=Path)
    parser.add_argument('--vendor-sof', required=True, type=Path)
    parser.add_argument('--quartus-pfg', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    output = args.output.absolute()
    if (output.exists() or '..' in output.parts or not output.is_relative_to(ROOT / 'out') or
            any(p.is_symlink() for p in (output, *output.parents)) or any(c in str(output) for c in ',@')):
        parser.error('output must be a fresh private directory below this worktree out/, without commas or @')
    directory, vendor, pfg = (p.resolve(strict=True) for p in (args.uboot_build, args.vendor_sof, args.quartus_pfg))
    receipt, fit, spl, config = checked_build(directory)
    if sha(vendor) != VENDOR_SOF:
        raise ValueError('recovery SOF is not the retained Nano reference')
    # mtools dispatches on argv[0]; keep the mformat/mmd/mcopy symlink names.
    tools = {name: Path(shutil.which(name) or '').absolute() for name in ('mformat', 'mmd', 'mcopy')}
    if any(not p.is_file() for p in tools.values()):
        raise ValueError('mtools is required')
    inputs = {str(p): sha(p) for p in (fit, spl, config, directory/'build/u-boot', directory/'result.json', vendor, pfg,
                                       Path(__file__), HERE/'recovery.py', HERE.parent/'firmware/build_terasic_uboot.py')}
    inputs.update({str(p): sha(p) for p in tools.values()})
    inputs.update({str(directory/'source'/name):expected for name,expected in receipt['network_recovery']['phy_sources']['prepared'].items()})
    output.mkdir(parents=True)
    commands = []
    def run(label, argv):
        argv = [str(x) for x in argv]; commands.append(argv)
        with (output/(label+'.log')).open('wb') as log:
            subprocess.run(argv, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
    shutil.copy2(fit, output/'u-boot.itb')
    shutil.copy2(spl, output/'u-boot-spl-dtb.hex')
    shutil.copy2(vendor, output/'recovery-reference.sof')
    sof = output/'network-recovery.sof'
    run('pfg-combine', [pfg, '-c', vendor, sof, '-o', 'hps_path='+str(spl)])
    run('pfg-info', [pfg, '-i', sof])
    info = (output/'pfg-info.log').read_text(encoding='latin-1')
    if 'Device name: A5EB013BB23BE4SCS' not in info or 'HPS/FPGA configuration order: HPS_AFTER_INIT_DONE' not in info:
        raise ValueError('unexpected recovery device or boot order')
    manifest = {'format':1, 'classification':'matched network recovery files; no board boot or SD write acceptance',
                'fit_sha256':sha(fit), 'spl_sha256':sha(spl), 'bl31_sha256':receipt['bl31_sha256'],
                'sof_sha256':sha(sof), 'fit_raw_sector':FIT_SECTOR, 'fat32_start_sector':FAT_SECTOR,
                'fat32_sectors':FAT_SECTORS, 'native_handoff':False, 'automatic_writes':False,
                'spl_boot_mode':'fat', 'spl_payload_path':'u-boot.itb', 'spl_fat_partition':1,
                'recovery_phy':recovery.PHY, 'phy_driver_linked':True,
                'recovery_fit_reserved_end_sector':EXPERIMENT_SECTOR,
                'future_experiment_fit_reserved_sectors':[EXPERIMENT_SECTOR,FAT_SECTOR],
                'embedded_spl_independently_decoded':False,
                'limitations':['Ethernet/PHY, stable MAC and SD writes need a bounded board test',
                               'manual U-Boot update commands; no atomic updater or protected dual FIT slots',
                               'regular-file image only; factory card, physical media and board untouched']}
    (output/'RECOVERY.JSON').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    disk = make_card(output, fit, tools, run)
    if any(sha(Path(p)) != h for p,h in inputs.items()):raise ValueError('recovery inputs changed')
    manifest.update(status='prepared',inputs=inputs,commands=commands,sd_image_sha256=sha(disk),sd_image_size=disk.stat().st_size)
    (output/'result.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    print('Prepared recovery files:',output)


if __name__ == '__main__':
    try:main()
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        raise SystemExit('Nano recovery packaging: '+str(error))
