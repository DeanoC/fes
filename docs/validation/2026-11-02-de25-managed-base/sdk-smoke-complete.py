"""Verify the real managed SDK in a builder with no original build volume."""
from pathlib import Path
import hashlib,json,socket,subprocess,sys,tempfile,time
sys.path.insert(0,"/src/image/de25-nano")
import managed_base as base
source=Path("/input"); output=Path("/export")
record=base.verify_receipt(source)
assert not Path("/build").exists(), "SDK test must not see the producer build directory"
with tempfile.TemporaryDirectory(prefix="fes-de25-sdk-isolation-") as temporary:
 work=Path(temporary);sdk=work/"sdk";sdk.mkdir();root=work/"rootfs";root.mkdir()
 base.image.run(["tar","-xzf",source/"aarch64-sdk.tar.gz","--strip-components=1","-C",sdk])
 base.image.run([sdk/"relocate-sdk.sh"])
 base.image.run(["tar","-xf",source/"rootfs.tar","-C",root,"--no-same-owner","--exclude=./dev/*","--exclude=dev/*"])
 compiler=sdk/"bin/aarch64-buildroot-linux-gnu-g++"
 sysroot=Path(subprocess.check_output([str(compiler),"-print-sysroot"],text=True).strip()).resolve()
 assert sysroot.is_relative_to(sdk.resolve()), "SDK compiler escapes relocated archive"
 for header in ("Imlib2.h","bluetooth/bluetooth.h","zlib.h"):
  assert (sysroot/"usr/include"/header).is_file(), header
 program=work/"sdk-proof.cpp"
 program.write_text("#define X_DISPLAY_MISSING 1\n#include <stddef.h>\n#include <Imlib2.h>\n#include <bluetooth/bluetooth.h>\n#include <zlib.h>\n#include <cstdio>\n#include <string>\nint main() { bdaddr_t addr{}; std::string v=zlibVersion(); if(v.empty() || bacmp(&addr,&addr) || imlib_get_cache_size()<0) return 1; std::puts(\"DE25_MANAGED_SDK_PASS\"); }\n")
 binary=work/"sdk-proof"
 base.image.run([compiler,program,"-o",binary,"-lImlib2","-lbluetooth","-lz"])
 base.image.elf_aarch64(binary.read_bytes())
 contract=base.load(Path("/src/image/scripts/kit-software.py"),"sdk_base_contract")
 closure=contract.base_contract(root,[binary],board="de25-nano")
 qemu=["qemu-aarch64-static","-L",str(root)]
 result=subprocess.check_output([*qemu,str(binary)],text=True).strip()
 assert result=="DE25_MANAGED_SDK_PASS",result
 architecture=subprocess.check_output([*qemu,str(root/"bin/busybox"),"uname","-m"],text=True).strip()
 assert architecture=="aarch64",architecture
 key=work/"host_key"
 base.image.run([*qemu,root/"usr/bin/dropbearkey","-t","ed25519","-f",key],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 server=subprocess.Popen([*qemu,str(root/"usr/sbin/dropbear"),"-F","-E","-P",str(work/"dropbear.pid"),"-p","127.0.0.1:2222","-r",str(key)],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
 banner=""
 try:
  deadline=time.monotonic()+10
  while time.monotonic()<deadline and server.poll() is None:
   try:
    with socket.create_connection(("127.0.0.1",2222),timeout=1) as connection: banner=base.image.read_ssh_banner(connection)
    break
   except OSError: time.sleep(.1)
  assert banner, "Dropbear identification unavailable"
 finally:
  server.terminate()
  try: server.communicate(timeout=5)
  except subprocess.TimeoutExpired: server.kill();server.communicate()
 evidence={"classification":"isolated SDK and QEMU userspace proof","source_revision":record["fes_revision"],"sdk_sha256":base.image.digest(source/"aarch64-sdk.tar.gz"),"rootfs_sha256":base.image.digest(source/"rootfs.ext4"),"compiler":subprocess.check_output([str(compiler),"--version"],text=True).splitlines()[0],"compiler_sysroot_inside_extracted_sdk":True,"source_sha256":base.image.digest(program),"executable_sha256":base.image.digest(binary),"executable_result":result,"qemu_architecture":architecture,"ssh_banner":banner,"ssh_authentication":"not-tested","base_contract":closure,"original_build_volume":"not-mounted","board_boot":"not-run","hardware_access":"not-performed"}
 output.mkdir(exist_ok=True)
 (output/"sdk-proof.cpp").write_bytes(program.read_bytes())
 (output/"sdk-proof").write_bytes(binary.read_bytes())
 (output/"sdk-proof").chmod(0o755)
 (output/"sdk-proof.json").write_text(json.dumps(evidence,indent=2,sort_keys=True)+"\n")
 print(json.dumps(evidence,indent=2,sort_keys=True))
