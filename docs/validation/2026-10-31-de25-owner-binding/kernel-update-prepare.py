import pathlib,tarfile,json,hashlib,shutil
root=pathlib.Path('/home/deano/fes/out/dev/de25-linux-platform');out=root/'out/owner-bus-network-update-8e7479d48';bundle=out/'bundle';bundle.mkdir(exist_ok=False)
old=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-image/829cf0353/candidate');new=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-bus/8e7479d48/candidate')
assert json.loads((new/'verify.json').read_text())['hardware_boot']=='not-run'
a=json.loads((old/'owner-image.json').read_text());b=json.loads((new/'owner-image.json').read_text());assert a['files']['boot/socfpga_agilex5_de25_nano.dtb']==b['files']['boot/socfpga_agilex5_de25_nano.dtb'];assert a['kernel']['files']['kernel.config']==b['kernel']['files']['kernel.config']
shutil.copyfile(new/'boot/Image',bundle/'Image');shutil.copyfile(new/'boot/socfpga_agilex5_de25_nano.dtb',bundle/'unchanged.dtb')
changes={'etc/fes-kernel-release':('release',0o644),'etc/init.d/S15fpga':('startup',0o755),'usr/share/fes/kernel-modules.json':('manifest',0o644)}
old_hashes={};new_hashes={}
for directory,hashes in [(old,old_hashes),(new,new_hashes)]:
 with tarfile.open(directory/'rootfs.tar') as archive:
  for name,(alias,mode) in changes.items():
   data=archive.extractfile('./'+name).read();hashes[name]=hashlib.sha256(data).hexdigest()
   if directory==new:(bundle/alias).write_bytes(data)
script='''#!/bin/sh
set -eu
stage=/tmp/fes-owner-bus-8e7479d48
boot=$stage/boot
backup=$boot/fes-owner-before-bus
mounted=0
changed=0
hash_is() { test "$(sha256sum "$1" | cut -d ' ' -f 1)" = "$2"; }
cleanup() {
  trial_rc=$?
  trap - EXIT
  if test "$trial_rc" != 0 && test "$changed" = 1; then
    echo 'Restoring pre-update files after staging failure' >&2
    cp "$backup/Image" "$boot/Image"
'''
for name,(alias,mode) in changes.items():script+=f'    cp "$backup/{alias}" /{name}\n    chmod {mode:o} /{name}\n'
script+='''    sync
  fi
  if test "$mounted" = 1; then umount "$boot"; fi
  exit "$trial_rc"
}
trap cleanup EXIT
# This narrowly scoped correction requires this exact idle failed-owner boot.
test "$(id -u)" = 0
test "$(uname -m)" = aarch64
test "$(uname -r)" = 6.12.11-fes-de25-owner
test "$(cat /proc/sys/kernel/random/boot_id)" = b989f017-f9f3-45dd-811b-0482b1b65421
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
test ! -e /dev/fes-de25-scanout
! grep -q '^/dev/mmcblk0p1 ' /proc/mounts
'''
for name,(alias,mode) in changes.items():script+=f'hash_is /{name} {old_hashes[name]}\nhash_is "$stage/{alias}" {new_hashes[name]}\n'
script+=f'hash_is "$stage/Image" {b["files"]["boot/Image"]["sha256"]}\n'
script+='''mkdir "$boot"
mount -t vfat -o rw,nodev,nosuid,noexec /dev/mmcblk0p1 "$boot"
mounted=1
test ! -e "$backup"
'''
script+=f'hash_is "$boot/Image" {a["files"]["boot/Image"]["sha256"]}\nhash_is "$boot/socfpga_agilex5_de25_nano.dtb" {b["files"]["boot/socfpga_agilex5_de25_nano.dtb"]["sha256"]}\n'
script+='mkdir "$backup"\ncp "$boot/Image" "$backup/Image"\n'
script+=f'hash_is "$backup/Image" {a["files"]["boot/Image"]["sha256"]}\n'
for name,(alias,mode) in changes.items():script+=f'cp /{name} "$backup/{alias}"\nhash_is "$backup/{alias}" {old_hashes[name]}\n'
script+='''sync
cp "$stage/Image" "$boot/Image.fes-new"
'''
script+=f'hash_is "$boot/Image.fes-new" {b["files"]["boot/Image"]["sha256"]}\n'
script+='''sync
changed=1
mv -f "$boot/Image.fes-new" "$boot/Image"
'''
for name,(alias,mode) in changes.items():script+=f'cp "$stage/{alias}" /{name}.fes-new\nchmod {mode:o} /{name}.fes-new\nmv -f /{name}.fes-new /{name}\nhash_is /{name} {new_hashes[name]}\n'
script+=f'hash_is "$boot/Image" {b["files"]["boot/Image"]["sha256"]}\n'
script+='''sync
umount "$boot"
mounted=0
changed=0
echo DE25_OWNER_BUS_UPDATE_STAGED_PASS
'''
(bundle/'install.sh').write_text(script)
plan={'target_boot_id':'b989f017-f9f3-45dd-811b-0482b1b65421','new_image_receipt_sha256':hashlib.sha256((new/'owner-image.json').read_bytes()).hexdigest(),'old_image':a['files']['boot/Image'],'new_image':b['files']['boot/Image'],'old_root_hashes':old_hashes,'new_root_hashes':new_hashes,'dtb_unchanged':True,'kernel_config_unchanged':True,'rollback':'FAT fes-owner-before-bus/Image and three root metadata files; complete original system backup retained off-board','scope':'file-level maintenance update, not whole SD image attestation; no reboot in installer'}
(out/'plan.json').write_text(json.dumps(plan,indent=2)+'\n')
with tarfile.open(out/'bundle.tar','w') as tar:
 for p in sorted(bundle.iterdir()):tar.add(p,arcname=p.name)
print(json.dumps(plan,indent=2))
