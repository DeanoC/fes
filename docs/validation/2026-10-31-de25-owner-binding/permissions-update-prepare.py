import pathlib,tarfile,hashlib,json,shutil
out=pathlib.Path('out/owner-mdev-network-update-8281d03a1');bundle=out/'bundle';bundle.mkdir()
old=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-bus/8e7479d48/candidate');new=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-bus/mdev-final/candidate');assert json.loads((new/'verify.json').read_text())['hardware_boot']=='not-run'
files={'etc/mdev.conf':('mdev',0o644),'etc/init.d/S15fpga':('startup',0o755),'usr/share/fes/kernel-modules.json':('manifest',0o644)};old_hash={};new_hash={}
for path,hashes in [(old,old_hash),(new,new_hash)]:
 with tarfile.open(path/'rootfs.tar') as t:
  for name,(alias,mode) in files.items():
   data=t.extractfile('./'+name).read();hashes[name]=hashlib.sha256(data).hexdigest()
   if path==new:(bundle/alias).write_bytes(data)
binary=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-userspace/d64ced1eabf3e514626cd8563e6cb68dba68e3d0/owner_unconfigured.aarch64');assert hashlib.sha256(binary.read_bytes()).hexdigest()=='b5db0fc187290073086714b94ccbdd6729011cd20c781e4e7730028f931d0032';shutil.copyfile(binary,bundle/'owner_unconfigured');(bundle/'owner_unconfigured').chmod(0o700)
script='''#!/bin/sh
set -eu
stage=/tmp/fes-owner-mdev-8281d03a1
backup=/root/fes-owner-mdev-backup-8281d03a1
changed=0
hash_is() { test "$(sha256sum "$1" | cut -d ' ' -f 1)" = "$2"; }
cleanup() {
 trial_rc=$?; trap - EXIT
 if test "$trial_rc" != 0 && test "$changed" = 1; then
'''
for name,(alias,mode) in files.items():script+=f'  cp "$backup/{alias}" /{name}; chmod {mode:o} /{name}\n'
script+='''  mdev -s
  sync
 fi
 exit "$trial_rc"
}
trap cleanup EXIT
test "$(id -u)" = 0
test "$(uname -r)" = 6.12.11-fes-de25-owner
test "$(cat /proc/sys/kernel/random/boot_id)" = aef6b113-efe8-4d0c-b164-efbf302bfd70
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
test -c /dev/fes-de25-scanout
test ! -e /sys/bus/platform/drivers/simple-pm-bus/unbind
'''
for name,(alias,mode) in files.items():script+=f'hash_is /{name} {old_hash[name]}\nhash_is "$stage/{alias}" {new_hash[name]}\n'
script+='umask 077\nmkdir "$backup"\n'
for name,(alias,mode) in files.items():script+=f'cp /{name} "$backup/{alias}"\nhash_is "$backup/{alias}" {old_hash[name]}\n'
script+='sync\nchanged=1\n'
for name,(alias,mode) in files.items():script+=f'cp "$stage/{alias}" /{name}.fes-new\nchmod {mode:o} /{name}.fes-new\nmv -f /{name}.fes-new /{name}\nhash_is /{name} {new_hash[name]}\n'
script+='''mdev -s
"$stage/owner_unconfigured" --boot-id aef6b113-efe8-4d0c-b164-efbf302bfd70
/etc/init.d/S15fpga start
sync
changed=0
echo DE25_OWNER_MDEV_PERSISTENT_RULE_PASS
'''
(bundle/'install.sh').write_text(script)
(out/'plan.json').write_text(json.dumps({'boot_id':'aef6b113-efe8-4d0c-b164-efbf302bfd70','old_hashes':old_hash,'new_hashes':new_hash,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'image_receipt_sha256':hashlib.sha256((new/'owner-image.json').read_bytes()).hexdigest()},indent=2)+'\n')
with tarfile.open(out/'bundle.tar','w') as t:
 for p in bundle.iterdir():t.add(p,arcname=p.name)
