#!/usr/bin/env python3
"""Minimal deterministic bootstrap ext4 construction in pinned boot-media tools."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

try:
    from .appliance import canonical,digest,load_manifest,validate_ext4,validate_static_arm
except ImportError:
    from appliance import canonical,digest,load_manifest,validate_ext4,validate_static_arm

EPOCH=1751459412
UUID='46455331-626f-4f74-8000-000000000001'
SIZE=32 << 20
DIRECTORIES=('.fes-bootstrap','dev','etc','etc/fes','media','media/fat','proc','run','sbin','sys','tmp')
ENV={**os.environ,'TZ':'UTC','LC_ALL':'C','SOURCE_DATE_EPOCH':str(EPOCH),'E2FSPROGS_FAKE_TIME':str(EPOCH)}


def run(command):
    completed=subprocess.run(list(map(str,command)),env=ENV,text=True,capture_output=True,check=True)
    return completed.stdout+completed.stderr


def debugfs(image,command,*,write=False):
    return run(['debugfs',*(['-w'] if write else []),'-R',command,image])


def assemble(output,binary,factory):
    output=Path(output)
    if output.exists() or output.is_symlink():raise ValueError('bootstrap output already exists')
    binary_sha=validate_static_arm(binary);manifest=load_manifest(factory)
    output.parent.mkdir(parents=True,exist_ok=True)
    try:
        with tempfile.TemporaryDirectory(prefix='.bootstrap-tree-',dir=output.parent) as temporary:
            tree=Path(temporary)
            for name in DIRECTORIES:(tree/name).mkdir(mode=0o755,parents=True,exist_ok=True)
            shutil.copyfile(binary,tree/'sbin/init');(tree/'sbin/init').chmod(0o755)
            (tree/'etc/fes/factory.json').write_bytes(canonical(manifest));(tree/'etc/fes/factory.json').chmod(0o444)
            (tree/'tmp').chmod(0o1777)
            for path in [*tree.rglob('*'),tree]:os.utime(path,(EPOCH,EPOCH),follow_symlinks=False)
            with output.open('xb') as stream:stream.truncate(SIZE)
            run(['mke2fs','-q','-F','-t','ext4','-b','1024','-I','256','-N','128','-m','0','-L','FES_BOOTSTRAP','-U',UUID,
                '-O','none,extent,filetype,sparse_super,large_file,dir_index',
                '-E',f'lazy_itable_init=0,lazy_journal_init=0,root_owner=0:0,hash_seed={UUID}',
                '-d',tree,output])
            # debugfs creates device inodes without host mknod privileges.
            commands=Path(temporary)/'devices.commands'
            commands.write_text('cd /dev\nmknod console c 5 1\nmknod null c 1 3\nset_inode_field console mode 020600\nset_inode_field null mode 020666\n')
            run(['debugfs','-w','-f',commands,output])
            inodes=set()
            for directory in ('/','/lost+found',*('/'+name for name in DIRECTORIES)):
                text=debugfs(output,'ls -p '+directory)
                for line in text.splitlines():
                    match=re.match(r'^/(\d+)/',line)
                    if match and int(match[1]):inodes.add(int(match[1]))
            if not inodes:raise ValueError('cannot enumerate bootstrap inodes')
            # Debian mkfs -d copies host ctime. Normalize every allocated inode
            # after population; the selected Buildroot's patched mke2fs is not
            # needed and its compiler/output volume remains untouched.
            lines=[]
            for inode in sorted(inodes):
                for field in ('atime','mtime','ctime','crtime'):
                    lines.append(f'set_inode_field <{inode}> {field} @{EPOCH}')
                    lines.append(f'set_inode_field <{inode}> {field}_extra 0')
                for field in ('uid','gid','generation','dtime'):lines.append(f'set_inode_field <{inode}> {field} 0')
            for field in ('mtime','wtime','lastcheck','mkfs_time'):lines.append(f'set_super_value {field} @{EPOCH}')
            commands.write_text('\n'.join(lines)+'\n');run(['debugfs','-w','-f',commands,output])
            run(['e2fsck','-f','-n',output])
            features=validate_ext4(output)
            for name in ('/sbin/init','/etc/fes/factory.json','/dev/console','/dev/null',*('/'+n for n in DIRECTORIES)):
                record=debugfs(output,'stat '+name)
                if 'Inode:' not in record or 'User:     0' not in record:raise ValueError('bootstrap inode missing or incorrectly owned: '+name)
            if 'character special' not in debugfs(output,'stat /dev/console'):raise ValueError('bootstrap console is not a device')
            return {'image_sha256':digest(output),'image_size':SIZE,'binary_sha256':binary_sha,
                'factory_manifest_sha256':hashlib.sha256(canonical(manifest)).hexdigest(),'ext4_features':features,
                'source_date_epoch':EPOCH,'filesystem_uuid':UUID,'mke2fs_version':run(['mke2fs','-V']).strip(),
                'debugfs_version':run(['debugfs','-V']).strip()}
    except BaseException:
        output.unlink(missing_ok=True)
        raise


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('command',choices=['assemble'])
    parser.add_argument('--output',type=Path,required=True);parser.add_argument('--binary',type=Path,required=True);parser.add_argument('--factory',type=Path,required=True)
    args=parser.parse_args()
    try:print(json.dumps(assemble(args.output,args.binary,args.factory),sort_keys=True))
    except (OSError,ValueError,subprocess.CalledProcessError) as error:parser.exit(1,f'appliance bootstrap: {error}\n')


if __name__=='__main__':main()
