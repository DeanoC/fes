#!/usr/bin/env python3
"""Export verified appliance releases and reproducible stable bootstrap images."""
import argparse
from dataclasses import asdict, dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import unicodedata

try:
    from .media_inputs import digest
except ImportError:
    from media_inputs import digest

FORMAT = 1
BOARD = 'de10-nano'
BOOT_ABI = 'fes-bootstrap-v1'
MAX_IMAGE_SIZE = (4 << 30) - 1
MAX_MANIFEST_SIZE = 8192
MANIFEST_FIELDS = {'format','board','boot_abi','version','kernel_sha256','image_sha256','image_size','fes_revision','fogcast_revision','runtime_revision'}
BOOTSTRAP_RECIPE_FILES = ('scripts/appliance.py','scripts/appliance_inside.py','scripts/media.py',
                          'scripts/media_container.py','scripts/media_inputs.py')


def canonical(data):
    return (json.dumps(data, sort_keys=True, separators=(',', ':'), ensure_ascii=False) + '\n').encode()


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON field: ' + key)
        result[key] = value
    return result


def regular(path):
    path = Path(path)
    if not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError('input must be a regular non-symlink file: ' + str(path))
    return path


def validate_manifest(data):
    if not isinstance(data,dict) or set(data) != MANIFEST_FIELDS:
        raise ValueError('release manifest has missing or unknown fields')
    if type(data['format']) is not int or data['format'] != FORMAT or data['board'] != BOARD or data['boot_abi'] != BOOT_ABI:
        raise ValueError('unsupported release format, board or boot ABI')
    version = data['version']
    if (not isinstance(version,str) or not version or len(version.encode()) > 128 or version.strip() != version
            or any(unicodedata.category(c) == 'Cc' for c in version)):
        raise ValueError('invalid release version')
    for name in ('kernel_sha256','image_sha256'):
        if not isinstance(data[name],str) or not re.fullmatch('[0-9a-f]{64}',data[name]):
            raise ValueError('invalid release digest')
    for name in ('fes_revision','fogcast_revision','runtime_revision'):
        if not isinstance(data[name],str) or not re.fullmatch('[0-9a-f]{40}',data[name]):
            raise ValueError('invalid release revision')
    if type(data['image_size']) is not int or not 2048 <= data['image_size'] <= MAX_IMAGE_SIZE:
        raise ValueError('invalid release image size')
    return data


def load_manifest(path):
    with regular(path).open('rb') as stream:
        raw = stream.read(MAX_MANIFEST_SIZE + 1)
    if len(raw) > MAX_MANIFEST_SIZE:
        raise ValueError('release manifest is too large')
    return validate_manifest(json.loads(raw,object_pairs_hook=unique))


def validate_static_arm(path):
    path = regular(path)
    size = path.stat().st_size
    if size < 84 or size > 24 << 20:
        raise ValueError('bootstrap ELF size is invalid')
    with path.open('rb') as stream:
        header = stream.read(52)
        if header[:7] != b'\x7fELF\x01\x01\x01':
            raise ValueError('bootstrap must be ELF32 little-endian ARM')
        kind,machine,version,entry,phoff,_,flags,ehsize,phsize,phnum,_,_,_ = struct.unpack_from('<HHIIIIIHHHHHH',header,16)
        if (kind != 2 or machine != 40 or version != 1 or flags >> 24 != 5 or ehsize != 52
                or phsize != 32 or not 1 <= phnum <= 128 or phoff < 52 or phoff + phnum*phsize > size):
            raise ValueError('bootstrap must be a static ARM EABI5 executable')
        executable_entry = False
        stream.seek(phoff)
        for _ in range(phnum):
            ptype,offset,address,_,filesz,memsz,pflags,_ = struct.unpack('<IIIIIIII',stream.read(32))
            if ptype in (2,3):
                raise ValueError('bootstrap may not require a dynamic loader')
            if offset + filesz > size or filesz > memsz:
                raise ValueError('invalid bootstrap ELF segment')
            if ptype == 1 and pflags & 1 and address <= entry < address + filesz:
                executable_entry = True
        if not executable_entry:
            raise ValueError('bootstrap ELF entry is not in an executable load segment')
    return digest(path)


def validate_ext4(path):
    path = regular(path)
    size = path.stat().st_size
    if not 2048 <= size <= MAX_IMAGE_SIZE:
        raise ValueError('ext4 image size is outside supported bounds')
    with path.open('rb') as stream:
        stream.seek(1024)
        sb = stream.read(1024)
    magic, = struct.unpack_from('<H',sb,56)
    compat,incompat,rocompat = struct.unpack_from('<III',sb,92)
    block_log, = struct.unpack_from('<I',sb,24)
    blocks, = struct.unpack_from('<I',sb,4)
    if incompat & 0x80:
        blocks += struct.unpack_from('<I',sb,336)[0] << 32
    # Known Linux 4.19 ext4 features emitted by the selected Buildroot recipe.
    # In particular exclude newer orphan_file, fast_commit and casefold formats.
    if (magic != 0xef53 or not incompat & 0x40 or compat & ~0x3c or incompat & ~0x2c2
            or rocompat & ~0x46b or block_log > 2 or not blocks or blocks*(1024 << block_log) > size):
        raise ValueError('ext4 features or geometry differ from locked-kernel compatibility')
    return {'compat':compat,'incompat':incompat,'ro_compat':rocompat,'block_size':1024 << block_log}


@dataclass(frozen=True)
class ReleaseProvenance:
    fes_revision: str
    fogcast_revision: str
    runtime_revision: str
    rootfs_sha256: str
    kernel_sha256: str
    image_receipt_sha256: str
    verification_sha256: str
    qemu_log_sha256: str

    def __post_init__(self):
        for name,value in asdict(self).items():
            width = 40 if name.endswith('_revision') else 64
            if not isinstance(value,str) or not re.fullmatch('[0-9a-f]{'+str(width)+'}',value):
                raise ValueError('invalid release provenance: ' + name)


@dataclass(frozen=True)
class ReleaseResult:
    directory: Path
    @property
    def image(self): return self.directory/'rootfs.img'
    @property
    def manifest(self): return self.directory/'release.json'
    @property
    def evidence(self): return self.directory/'evidence.json'


@dataclass(frozen=True)
class BootstrapResult:
    directory: Path
    @property
    def image(self): return self.directory/'linux.img'
    @property
    def evidence(self): return self.directory/'evidence.json'


def sync_directory(path):
    descriptor = os.open(path,os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(descriptor)
    finally: os.close(descriptor)


def seal(path):
    for child in path.iterdir():
        child.chmod(0o444)
        with child.open('rb') as stream: os.fsync(stream.fileno())
    sync_directory(path)


def publish(scratch,output):
    # Destination directories are immutable and nonempty. rename cannot replace
    # an existing release. No current pointer is changed by these operations.
    seal(scratch)
    try: scratch.rename(output)
    except OSError:
        scratch.chmod(0o700)
        raise
    output.chmod(0o555)
    sync_directory(output)
    sync_directory(output.parent)


def require_sealed_bundle(path,names):
    path=Path(path)
    info=path.lstat()
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode)!=0o555:
        raise ValueError('existing artifact directory is not sealed')
    if {child.name for child in path.iterdir()}!=set(names):
        raise ValueError('existing artifact has unexpected entries')
    for name in names:
        if stat.S_IMODE(regular(path/name).stat().st_mode)!=0o444:
            raise ValueError('existing artifact file is not sealed: '+name)


def bootstrap_recipe(root):
    return {name:digest(regular(Path(root)/name)) for name in BOOTSTRAP_RECIPE_FILES}


def bootstrap_identity(binary_sha,factory,container,assembly_revision,recipe):
    return hashlib.sha256(canonical({'binary':binary_sha,'factory':factory,'container_identity':container,
        'assembly_revision':assembly_revision,'assembly_recipe':recipe})).hexdigest()


def release_manifest(rootfs,*,version,provenance):
    return validate_manifest({'format':FORMAT,'board':BOARD,'boot_abi':BOOT_ABI,'version':version,
        'kernel_sha256':provenance.kernel_sha256,'image_sha256':provenance.rootfs_sha256,
        'image_size':regular(rootfs).stat().st_size,'fes_revision':provenance.fes_revision,
        'fogcast_revision':provenance.fogcast_revision,'runtime_revision':provenance.runtime_revision})


def export_release(output,rootfs,kernel,*,version,provenance):
    """Export caller-verified raw inputs, checking their explicit receipt hashes.

    The CLI obtains provenance from current cold-build validation. This lower
    level function does not independently establish the provenance's origin.
    """
    output = Path(output); rootfs = regular(rootfs); kernel = regular(kernel)
    if digest(rootfs) != provenance.rootfs_sha256: raise ValueError('rootfs differs from verified provenance')
    if digest(kernel) != provenance.kernel_sha256: raise ValueError('kernel differs from locked provenance')
    features = validate_ext4(rootfs)
    manifest = release_manifest(rootfs,version=version,provenance=provenance)
    evidence = {'format':1,'kind':'fes-appliance-release','provenance':asdict(provenance),
        'manifest_sha256':hashlib.sha256(canonical(manifest)).hexdigest(),'ext4_features':features,'hardware':'not-run'}
    result = ReleaseResult(output)
    if output.exists() or output.is_symlink():
        require_sealed_bundle(output,('rootfs.img','release.json','evidence.json'))
        if (regular(result.manifest).read_bytes()!=canonical(manifest) or regular(result.evidence).read_bytes()!=canonical(evidence)
                or digest(regular(result.image))!=provenance.rootfs_sha256):
            raise ValueError('immutable release destination differs')
        return result
    output.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.release-',dir=output.parent) as temporary:
        scratch = Path(temporary)/'bundle'; scratch.mkdir()
        shutil.copyfile(rootfs,scratch/'rootfs.img')
        if digest(scratch/'rootfs.img') != provenance.rootfs_sha256: raise ValueError('rootfs changed during export')
        (scratch/'release.json').write_bytes(canonical(manifest))
        (scratch/'evidence.json').write_bytes(canonical(evidence))
        publish(scratch,output)
    return result


def cached_media_container(root,runtime,lock):
    """Require the existing pinned container; this operation never builds/pulls."""
    try: from . import media_container
    except ImportError: import media_container
    hasher = hashlib.sha256()
    for name in media_container.CONTEXT_FILES:
        data=(Path(root)/'containers/boot-media'/name).read_bytes()
        hasher.update(name.encode()+b'\0'+len(data).to_bytes(8,'big')+data)
    tag=f'fes-boot-media:{hasher.hexdigest()}-{os.getuid()}-{os.getgid()}'
    inspected=subprocess.run([runtime,'image','inspect',tag],capture_output=True,text=True)
    if inspected.returncode:
        raise ValueError('pinned boot-media tool container is not cached; provision it separately')
    packages_sha=(Path(root)/'containers/boot-media/packages.sha256').read_text().strip()
    if lock.layout!='de10-nano-mister-v1' or not os.getuid() or not os.getgid():
        raise ValueError('pinned boot-media requires the selected layout and non-root caller')
    expected={'org.fes.media.base':media_container.BASE,'org.fes.media.context-sha256':hasher.hexdigest(),
        'org.fes.media.packages-sha256':packages_sha,'org.fes.media.uid':str(os.getuid()),'org.fes.media.gid':str(os.getgid())}
    try:
        record,=json.loads(inspected.stdout)
        image=record['Id'];config=record['Config']
        if (not re.fullmatch('sha256:[0-9a-f]{64}',image) or config.get('User')!='builder'
                or any(config.get('Labels',{}).get(k)!=v for k,v in expected.items())):
            raise ValueError('cached boot-media container differs from pinned identity')
    except (KeyError,TypeError,json.JSONDecodeError) as error:
        raise ValueError('invalid cached boot-media inspection') from error
    arguments=[runtime,'run','--rm','--pull=never','--network=none','--cap-drop=ALL','--security-opt=no-new-privileges','--entrypoint','python3',image]
    actual=subprocess.check_output([*arguments,'-c',media_container.PACKAGE_QUERY],text=True).strip()
    identity=subprocess.check_output([*arguments,'-c',"import os; print(f'{os.getuid()}:{os.getgid()}')"],text=True).strip()
    if actual!=packages_sha or identity!=f'{os.getuid()}:{os.getgid()}':
        raise ValueError('cached boot-media package set or user differs from pin')
    return image


def assemble_bootstrap(output,bootstrap_binary,factory_manifest,kernel,*,runner,binary_source_revision=None,assembly_revision=None):
    """Build two independent bootstrap files using a supplied pinned media Runner.

    binary_source_revision is supplied only by an integrator that built this
    binary from that clean selected revision. Otherwise evidence says unproven.
    assembly_revision similarly identifies the selected FES assembly source.
    Output and temporary paths must be inside the supplied Runner filesystem.
    """
    output=Path(output); binary_sha=validate_static_arm(bootstrap_binary)
    factory=load_manifest(factory_manifest)
    if digest(regular(kernel))!=factory['kernel_sha256']: raise ValueError('factory and bootstrap kernel differ')
    if binary_source_revision is not None and binary_source_revision!=factory['fogcast_revision']:
        raise ValueError('bootstrap source revision differs from factory FogCast revision')
    if assembly_revision is not None and not re.fullmatch('[0-9a-f]{40}',assembly_revision):
        raise ValueError('invalid bootstrap assembly revision')
    recipe=bootstrap_recipe(runner.root)
    output.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.bootstrap-',dir=output.parent) as temporary:
        scratch=Path(temporary)
        # Snapshot mutable caller paths and bind exactly what enters both passes.
        binary=scratch/'fes-boot'; shutil.copyfile(bootstrap_binary,binary)
        factory_path=scratch/'factory.json'; factory_path.write_bytes(canonical(factory))
        if digest(binary)!=binary_sha: raise ValueError('bootstrap binary changed during snapshot')
        results=[]
        for name in ('first.img','second.img'):
            command=['python3','/work/scripts/appliance_inside.py','assemble','--output',runner.path(scratch/name),
                '--binary',runner.path(binary),'--factory',runner.path(factory_path)]
            results.append(json.loads(runner.disk(command)))
        first,second=results
        if first!=second or digest(scratch/'first.img')!=first['image_sha256'] or digest(scratch/'second.img')!=first['image_sha256']:
            raise ValueError('bootstrap two-pass reproducibility failed')
        if bootstrap_recipe(runner.root)!=recipe:
            raise ValueError('bootstrap assembly recipe changed during construction')
        evidence={'format':1,'kind':'fes-stable-bootstrap','boot_abi':BOOT_ABI,'bootstrap_sha256':first['image_sha256'],
            'bootstrap_binary_sha256':binary_sha,'binary_source_revision':binary_source_revision,
            'binary_source_proven':binary_source_revision is not None,
            'assembly_revision':assembly_revision,'assembly_recipe':recipe,'assembly_source_proven':assembly_revision is not None,
            'classification':'source-bound-host-artifact' if binary_source_revision and assembly_revision else 'diagnostic-unproven-source',
            'kernel_sha256':factory['kernel_sha256'],
            'factory_image_sha256':factory['image_sha256'],'factory_manifest_sha256':digest(factory_path),
            'container_identity':runner.container,'tool_evidence':first,'two_pass_reproducibility':'pass','hardware':'not-run'}
        result=BootstrapResult(output)
        if output.exists() or output.is_symlink():
            require_sealed_bundle(output,('linux.img','evidence.json'))
            if regular(result.evidence).read_bytes()!=canonical(evidence) or digest(regular(result.image))!=first['image_sha256']:
                raise ValueError('immutable bootstrap destination differs')
            return result
        bundle=scratch/'bundle';bundle.mkdir();shutil.move(scratch/'first.img',bundle/'linux.img')
        (bundle/'evidence.json').write_bytes(canonical(evidence));publish(bundle,output)
    return result


def verified_inputs(root,profile):
    # Existing modules use script-style imports; keep this import boundary local.
    sys.path.insert(0,str(Path(__file__).resolve().parent))
    import media
    from media_inputs import MediaLock,resolve_payloads
    if media.cold_build.git(root,'status','--porcelain','--untracked-files=all','--ignore-submodules=all'):
        raise ValueError('appliance release requires a clean committed FES checkout')
    fingerprint,fogcast,_,env=media.select(root,profile)
    cold=media.cold_build.load_verified_image(root/'out'/profile,fingerprint)
    lock=MediaLock.load(root/'boot-media.lock.toml')
    cache=root/'out/work/boot-media'/('image-creator-'+lock.commit)
    if not cache.is_dir(): raise ValueError('locked boot payload cache is absent; provision it separately')
    payloads=resolve_payloads(root,lock,media.cold_build.run)
    import tomllib
    configuration=tomllib.loads((root/'profiles'/(profile+'.toml')).read_text())
    revisions=media.cold_build.validate(root,configuration)
    p=ReleaseProvenance(cold['fes_revision'],revisions['FogCast'],revisions['libmister-runtime'],
        cold['rootfs_sha256'],lock.kernel.sha256,cold['image_receipt_sha256'],cold['verification_sha256'],cold['qemu_log_sha256'])
    return root/'out'/profile/'linux.img',payloads.kernel,p,fogcast,env,lock


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    commands=parser.add_subparsers(dest='command',required=True)
    for command in ('release','bootstrap'):
        child=commands.add_parser(command);child.add_argument('--profile',default='native-integration-dev',choices=['native-integration-dev'])
        child.add_argument('--output',type=Path)
        if command=='release':child.add_argument('--version',required=True)
        else:
            child.add_argument('--release',type=Path,required=True)
    args=parser.parse_args();root=Path(__file__).resolve().parents[1]
    try:
        if args.output is not None:
            args.output=args.output.resolve()
            if args.command=='bootstrap' and not args.output.is_relative_to(root):
                raise ValueError('bootstrap output must be inside the FES checkout')
        sys.path.insert(0,str(root/'scripts'));import media
        with media.operation(root,args.profile):
            rootfs,kernel,p,fogcast,env,lock=verified_inputs(root,args.profile)
            if args.command=='release':
                manifest=release_manifest(rootfs,version=args.version,provenance=p)
                output=args.output or root/'out'/args.profile/'appliance/releases'/p.rootfs_sha256/hashlib.sha256(canonical(manifest)).hexdigest()
                result=export_release(output,rootfs,kernel,version=args.version,provenance=p)
            else:
                factory=load_manifest(args.release/'release.json')
                if factory!=release_manifest(rootfs,version=factory['version'],provenance=p) or digest(regular(args.release/'rootfs.img'))!=p.rootfs_sha256:
                    raise ValueError('release differs from current verified cold image')
                runtime=os.environ.get('CONTAINER_RUNTIME','docker')
                container=cached_media_container(root,runtime,lock)
                runner=object.__new__(media.Runner);runner.root=root;runner.runtime=runtime;runner.container=container
                scratch_root=root/'out/tmp';scratch_root.mkdir(parents=True,exist_ok=True)
                with tempfile.TemporaryDirectory(prefix='fes-boot-build-',dir=scratch_root) as temporary:
                    binary=Path(temporary)/'fes-boot'
                    factory_snapshot=Path(temporary)/'factory.json'
                    factory_snapshot.write_bytes(canonical(factory))
                    # Resolve the already-cached selected Go toolchain first;
                    # then invoke it directly with all dependency fetches off.
                    goroot=subprocess.check_output(['go','env','GOROOT'],cwd=fogcast,env=dict(env,GOPROXY='off'),text=True).strip()
                    build_env=dict(env,GOOS='linux',GOARCH='arm',GOARM='7',CGO_ENABLED='0',GOPROXY='off',GOSUMDB='off',GOTOOLCHAIN='local')
                    subprocess.run([str(Path(goroot)/'bin/go'),'build','-trimpath','-buildvcs=false','-ldflags=-s -w -buildid=','-o',str(binary),'./cmd/fes-boot'],cwd=fogcast,env=build_env,check=True)
                    assembly_revision=media.cold_build.git(root,'rev-parse','HEAD')
                    identity=bootstrap_identity(digest(binary),factory,container,assembly_revision,bootstrap_recipe(root))
                    output=args.output or root/'out'/args.profile/'appliance/bootstrap'/identity
                    result=assemble_bootstrap(output,binary,factory_snapshot,kernel,runner=runner,
                        binary_source_revision=p.fogcast_revision,assembly_revision=assembly_revision)
        print(result.directory)
        print('Host-only artifact; hardware acceptance not run.')
    except (OSError,ValueError,subprocess.CalledProcessError) as error:
        parser.exit(1,f'appliance: {error}\n')


if __name__=='__main__':main()
