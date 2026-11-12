import hashlib,json,pathlib,subprocess,tarfile
root=pathlib.Path('/src');sdk=pathlib.Path('/tmp/sdk');sdk.mkdir();archive=pathlib.Path('/image/aarch64-sdk.tar.gz')
h=hashlib.sha256(archive.read_bytes()).hexdigest();assert h=='7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7'
subprocess.run(['tar','-xzf',str(archive),'--strip-components=1','-C',str(sdk)],check=True)
subprocess.run([str(sdk/'relocate-sdk.sh')],check=True)
cc=sdk/'bin/aarch64-buildroot-linux-gnu-gcc';binary=pathlib.Path('/export/owner_unconfigured.aarch64')
subprocess.run([str(cc),'-std=c11','-O2','-Wall','-Wextra','-Werror','-static','-o',str(binary),str(root/'image/kernel/de25-platform/owner_unconfigured.c')],check=True)
header=subprocess.check_output([str(sdk/'bin/aarch64-buildroot-linux-gnu-readelf'),'-h','-l','-d',str(binary)],text=True);assert 'AArch64' in header and 'INTERP' not in header and '(NEEDED)' not in header
pathlib.Path('/export/readelf.txt').write_text(header)
cases=[]
for args,needle in [([],b'usage:'),(['--boot-id','bad'],b'invalid expected boot UUID'),(['--boot-id','00000000-0000-4000-8000-000000000001'],b'requires the AArch64 owner-candidate board kernel')]:
 r=subprocess.run(['qemu-aarch64',str(binary),*args],capture_output=True,timeout=10);assert r.returncode==1 and needle in r.stderr and b'USERSPACE_PASS' not in r.stdout
 cases.append({'args':args,'returncode':r.returncode,'stderr':r.stderr.decode()})
record={'classification':'off-board AArch64 build and host-refusal checks','sdk_sha256':h,'compiler':subprocess.check_output([str(cc),'--version'],text=True).splitlines()[0],'files':{str(p.relative_to(root)) : hashlib.sha256(p.read_bytes()).hexdigest() for p in [root/'image/kernel/de25-platform/owner_unconfigured.c',root/'image/kernel/de25-dma/de25_dma.h']},'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'binary_size':binary.stat().st_size,'cases':cases,'board_syscalls':'not-run'}
pathlib.Path('/export/result.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record),flush=True)
