#!/usr/bin/env python3
"""Build and inspect appliance FAT disks inside the existing pinned media tools."""
import argparse
from dataclasses import asdict, dataclass, replace
import json
import os
from pathlib import Path
import shutil
import struct
import tempfile

try:
    from . import appliance, media_inside as base
    from .media_inputs import MediaLock, digest, verify_file, _validate
except ImportError:
    import appliance
    import media_inside as base
    from media_inputs import MediaLock, digest, verify_file, _validate


LAYOUT_ID='de10-nano-appliance-1g-v1'


def appliance_layout(lock):
    """Derive only the reviewed appliance geometry from immutable boot inputs.

    The Cyclone V ROM locates SPL by MBR type A2. The locked SPL locates that
    same partition and loads U-Boot at its start + 0x200 sectors. FAT remains
    partition 1, preserving the locked kernel root and U-Boot environment.
    """
    _validate(lock)
    fat=replace(lock.partition_1,sector_count=(1<<30)//512)
    boot=replace(lock.partition_2,start_sector=fat.start_sector+fat.sector_count)
    return replace(lock,layout=LAYOUT_ID,partition_1=fat,partition_2=boot,
        total_sectors=boot.start_sector+boot.sector_count)


def geometry(layout):
    return {'sector_size':layout.sector_size,'total_sectors':layout.total_sectors,
        'partition_1':asdict(layout.partition_1),'partition_2':asdict(layout.partition_2)}


def verify_mbr(image,layout):
    with image.open('rb') as stream:
        mbr=stream.read(512)
        if mbr[:440]!=bytes(440) or mbr[444:446]!=bytes(2) or mbr[478:510]!=bytes(32):
            raise ValueError('appliance MBR reserved bytes differ')
        if struct.unpack_from('<I',mbr,440)[0]!=layout.disk_id or mbr[510:]!=b'\x55\xaa':
            raise ValueError('appliance MBR identifier or signature differs')
        for offset,part in ((446,layout.partition_1),(462,layout.partition_2)):
            expected=(128 if part.active else 0,bytes.fromhex(part.chs_start),part.type,
                bytes.fromhex(part.chs_end),part.start_sector,part.sector_count)
            if struct.unpack_from('<B3sB3sII',mbr,offset)!=expected:
                raise ValueError('appliance MBR partition differs')
        base.require_zero(stream,512,layout.partition_1.start_sector*512-512,'disk padding')
        base.require_zero(stream,layout.partition_2.start_sector*512+layout.uboot.size,
            layout.partition_2.sector_count*512-layout.uboot.size,'boot partition tail')


@dataclass(frozen=True)
class Inputs:
    factory: Path
    bootstrap: Path
    manifest: Path
    idle: Path
    kernel: Path
    uboot: Path
    agent_config: Path | None = None
    launcher_config: Path | None = None

    def boot_inputs(self):
        return base.ImageInputs(self.bootstrap,self.idle,self.kernel,self.uboot,
            agent_config=self.agent_config,launcher_config=self.launcher_config,
            launcher_config_sha256=digest(self.launcher_config) if self.launcher_config else None)


def check_inputs(inputs,lock,scratch):
    layout=appliance_layout(lock)
    manifest=appliance.load_manifest(inputs.manifest)
    verify_file(inputs.factory,manifest['image_size'],manifest['image_sha256'],'factory release')
    verify_file(inputs.kernel,lock.kernel.size,manifest['kernel_sha256'],'release kernel')
    appliance.validate_ext4(inputs.factory);appliance.validate_ext4(inputs.bootstrap)
    # The factory owns idle.rbf. The minimal bootstrap intentionally has no RBF.
    factory_inputs=base.ImageInputs(inputs.factory,inputs.idle,inputs.kernel,inputs.uboot,
        agent_config=inputs.agent_config,launcher_config=inputs.launcher_config,
        launcher_config_sha256=digest(inputs.launcher_config) if inputs.launcher_config else None)
    base.check_inputs(factory_inputs,lock,scratch)
    oldroot=base.run('debugfs','-R','stat /.fes-bootstrap',inputs.factory)
    if 'Type: directory' not in oldroot:
        raise ValueError('factory lacks the required /.fes-bootstrap mount directory')
    # Reserve the factory, Good, Previous and a staged candidate simultaneously.
    required=4*inputs.factory.stat().st_size+sum(Path(getattr(inputs,name)).stat().st_size
        for name in ('bootstrap','idle','kernel'))+(16<<20)
    if required > layout.partition_1.sector_count*layout.sector_size:
        raise ValueError('appliance payloads leave insufficient update and rollback capacity')
    if inputs.launcher_config and not inputs.agent_config:
        raise ValueError('launcher provisioning requires an agent configuration')
    return manifest


def owned_files(inputs,manifest):
    image=manifest['image_sha256']
    owned={'/menu.rbf':inputs.idle,'/linux/zImage_dtb':inputs.kernel,'/linux/linux.img':inputs.bootstrap,
        f'/fogcast/releases/images/{image}.img':inputs.factory,
        f'/fogcast/releases/manifests/{image}.json':inputs.manifest}
    if inputs.agent_config:owned['/fogcast/agent.toml']=inputs.agent_config
    if inputs.launcher_config:owned['/fogcast/launcher.json']=inputs.launcher_config
    return owned


def verify(image,inputs,lock):
    layout=appliance_layout(lock)
    disk_size=layout.total_sectors*layout.sector_size
    fat_offset=layout.partition_1.start_sector*layout.sector_size
    image=appliance.regular(image)
    if image.stat().st_size!=disk_size:raise ValueError('appliance disk size differs from reviewed layout')
    with tempfile.TemporaryDirectory(prefix='appliance-inspect-') as temporary:
        scratch=Path(temporary);manifest=check_inputs(inputs,lock,scratch)
        verify_mbr(image,layout)
        boot=scratch/'uboot';base.copy_region(image,boot,layout.partition_2.start_sector*512,lock.uboot.size)
        if digest(boot)!=lock.uboot.sha256:raise ValueError('appliance U-Boot differs from lock')
        fat=scratch/'fat.img';base.copy_region(image,fat,fat_offset,layout.partition_1.sector_count*512)
        base.run('fsck.fat','-vn',fat)
        device=f'{image}@@{fat_offset}'
        listing=base.run('mdir','-a','-b','-s','-i',device,'::/')
        paths=sorted(line.removeprefix('::').rstrip('/') for line in listing.splitlines() if line)
        owned=owned_files(inputs,manifest)
        expected=set(owned)|{'/linux','/fogcast','/fogcast/releases','/fogcast/releases/images','/fogcast/releases/manifests'}
        if set(paths)!=expected or len(paths)!=len(expected):raise ValueError('appliance FAT owned paths differ')
        for index,(name,source) in enumerate(sorted(owned.items())):
            extracted=scratch/f'payload-{index}'
            base.run('mcopy','-i',device,'::'+name,extracted)
            if extracted.stat().st_size!=source.stat().st_size or digest(extracted)!=digest(source):
                raise ValueError('appliance FAT payload differs: '+name)
        return {'image_sha256':digest(image),'image_size':disk_size,'paths':paths,
            'layout_id':layout.layout,'geometry':geometry(layout),
            'factory_sha256':manifest['image_sha256'],'bootstrap_sha256':digest(inputs.bootstrap),
            'kernel_sha256':lock.kernel.sha256,'uboot_sha256':lock.uboot.sha256,
            'agent_config_sha256':digest(inputs.agent_config) if inputs.agent_config else None,
            'launcher_config_sha256':digest(inputs.launcher_config) if inputs.launcher_config else None,
            'structural':'pass'}


def assemble(output,inputs,lock):
    layout=appliance_layout(lock)
    output=Path(output)
    if output.exists() or output.is_symlink():raise ValueError('appliance disk output already exists')
    output.parent.mkdir(parents=True,exist_ok=True)
    old_umask=os.umask(0o077)
    try:
        with tempfile.TemporaryDirectory(prefix='.appliance-disk-',dir=output.parent) as temporary:
            scratch=Path(temporary);manifest=check_inputs(inputs,lock,scratch)
            base._assemble_once(output,inputs.boot_inputs(),layout,scratch)
            output.chmod(0o600)
            device=f'{output}@@{base.PART1_OFFSET}'
            for directory in ('releases','releases/images','releases/manifests'):
                base.run('mmd','-i',device,'::/fogcast/'+directory)
            for name,source in (('factory.img',inputs.factory),('release.json',inputs.manifest)):
                destination=scratch/name;shutil.copyfile(source,destination)
                os.utime(destination,(base.SOURCE_DATE_EPOCH,base.SOURCE_DATE_EPOCH))
            image=manifest['image_sha256']
            base.run('mcopy','-m','-i',device,scratch/'factory.img',f'::/fogcast/releases/images/{image}.img')
            base.run('mcopy','-m','-i',device,scratch/'release.json',f'::/fogcast/releases/manifests/{image}.json')
            return verify(output,inputs,lock)
    except BaseException:
        output.unlink(missing_ok=True)
        raise
    finally:
        os.umask(old_umask)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('assemble','verify'))
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--inputs',type=Path,required=True)
    parser.add_argument('--lock',type=Path,default=Path('/work/boot-media.lock.toml'))
    args=parser.parse_args()
    try:
        raw=json.loads(appliance.regular(args.inputs).read_text(),object_pairs_hook=appliance.unique)
        inputs=Inputs(**{name:Path(path) if path is not None else None for name,path in raw.items()})
        lock=MediaLock.load(args.lock)
        result=assemble(args.output,inputs,lock) if args.command=='assemble' else verify(args.output,inputs,lock)
        print(json.dumps(result,sort_keys=True))
    except (OSError,ValueError,TypeError) as error:parser.exit(1,f'appliance media: {error}\n')


if __name__=='__main__':main()
