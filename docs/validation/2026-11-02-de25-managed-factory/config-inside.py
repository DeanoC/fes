import hashlib,json,os,shutil,subprocess,sys
from pathlib import Path
sys.path.insert(0,"/src/image/de25-nano")
import managed_base as base
image=base.image
key="factory-"+"3"*64
base.prepare_sdk(key)
_,work,command,env=image.prepare_buildroot(managed=True,factory_key=key)
config=(work/".config").read_text().splitlines()
required=("BR2_aarch64=y","BR2_cortex_a53=y","BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y","BR2_ROOTFS_MERGED_USR=y","# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set","BR2_PACKAGE_BLUEZ5_UTILS=y","BR2_PACKAGE_DBUS=y")
for option in required:assert option in config,option
prefix=base.toolchain_path(key)/"bin/aarch64-buildroot-linux-gnu-gcc"
assert subprocess.check_output([prefix,"-dumpmachine"],text=True).strip()=="aarch64-buildroot-linux-gnu"
fogcast=Path("/build/fogcast");(fogcast/"bin").mkdir(parents=True,exist_ok=True)
binary=fogcast/"bin/mister-runtime-linux-aarch64";shutil.copyfile("/runtime-input/mister-runtime",binary)
expected="05c25c74d9c53a073625d56c36e04ef63a407c71c823a25e4e50448c6325dffd"
assert image.digest(binary)==expected
makefile=Path("/build/package.mk");makefile.write_text("include /src/image/buildroot/package/mister-runtime/mister-runtime.mk\nall: /build/fogcast/bin/.installed\n/build/fogcast/bin/.installed:\n\t$(MISTER_RUNTIME_INSTALL_TARGET_CMDS)\n.PHONY: all /build/fogcast/bin/.installed\n")
subprocess.run(["make","--no-print-directory","-s","-f",str(makefile),"BR2_aarch64=y","FOGCAST_DIR="+str(fogcast),"TARGET_DIR=/build/runtime-installed","INSTALL=/usr/bin/install","FES_PREBUILT_RUNTIME_SHA256="+expected],check=True)
installed=Path("/build/runtime-installed/usr/sbin/mister-runtime");assert image.digest(installed)==expected
header=subprocess.check_output(["readelf","-h",installed],text=True);assert "AArch64" in header and "ELF64" in header
shutil.copyfile(work/".config","/export/buildroot.config")
record={"classification":"actual pinned Buildroot defconfig and real AArch64 prebuilt runtime installation only","required_config":required,"compiler_machine":"aarch64-buildroot-linux-gnu","compiler_sha256":image.digest(prefix.resolve()),"runtime_sha256":expected,"configuration_sha256":image.digest(work/".config"),"factory_rootfs":"not-built","MENU_seal":"not-tested; no package supplied to this configuration proof","hardware_access":"not-performed"}
Path("/export/receipt.json").write_text(json.dumps(record,indent=2)+"\n");print(json.dumps(record,indent=2))
