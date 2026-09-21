#!/usr/bin/env python3
"""Publish/reverify private appliance card files without writing a physical card."""
import argparse
from dataclasses import asdict, dataclass
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import tomllib

try:
    from . import appliance
    from . import platform as fes_platform
    from .appliance_media_inside import Inputs
    from .media_inputs import digest, verify_file
except ImportError:
    import appliance
    import platform as fes_platform
    from appliance_media_inside import Inputs
    from media_inputs import digest, verify_file

PROFILE='native-integration-dev'
RECIPE_FILES=('scripts/appliance.py','scripts/appliance_inside.py','scripts/appliance_media.py','scripts/appliance_media_inside.py',
    'scripts/media.py','scripts/media_inputs.py','scripts/media_inside.py','scripts/media_container.py','scripts/platform.py','scripts/prepare_launcher.py',
    'boot-media.lock.toml','containers/boot-media/Dockerfile','containers/boot-media/create-builder-user.sh','containers/boot-media/packages.sha256')


@dataclass(frozen=True)
class Result:
    directory: Path
    @property
    def image(self):return self.directory/'card.img'
    @property
    def evidence(self):return self.directory/'evidence.json'


def compare_card(actual,expected):
    actual=appliance.regular(actual);expected=appliance.regular(expected)
    if actual.stat().st_size!=expected.stat().st_size or digest(actual)!=digest(expected):
        raise ValueError('appliance disk differs from independent current-recipe reconstruction')


def require_current_recipe(root,bindings):
    """Close the evidence snapshot around execution of live assembly scripts."""
    try:
        current={name:digest(appliance.regular(Path(root)/name)) for name in RECIPE_FILES}
    except (OSError,ValueError) as error:
        raise ValueError('appliance media recipe changed during construction or verification') from error
    if current!=bindings['recipe']:
        raise ValueError('appliance media recipe changed during construction or verification')


def compare_bundle(actual,expected,names):
    actual=Path(actual)
    if actual.is_symlink() or not actual.is_dir() or set(p.name for p in actual.iterdir())!=set(names):
        raise ValueError('appliance bundle files differ from expected closed set')
    if actual.stat().st_mode & 0o222:raise ValueError('release/bootstrap bundle must be sealed')
    for name in names:
        a=appliance.regular(actual/name);b=appliance.regular(expected/name)
        if a.stat().st_mode & 0o222:raise ValueError('release/bootstrap file must be sealed')
        if a.stat().st_size!=b.stat().st_size or digest(a)!=digest(b):
            raise ValueError('appliance bundle differs from independently verified inputs: '+name)


def build_selected_binary(root,fogcast,output,env):
    return fes_platform.build_static_arm(root,fogcast,output,env)


def retained_assembly_revision(root,bootstrap_directory,fogcast):
    evidence_path=appliance.regular(Path(bootstrap_directory)/'evidence.json')
    with evidence_path.open('rb') as stream:raw=stream.read(65537)
    if len(raw)>65536:raise ValueError('bootstrap evidence is too large')
    evidence=json.loads(raw,object_pairs_hook=appliance.unique)
    revision=evidence.get('assembly_revision')
    import re
    if not isinstance(revision,str) or not re.fullmatch('[0-9a-f]{40}',revision):
        raise ValueError('bootstrap lacks source-proven assembly revision')
    record=evidence.get('platform_build')
    if not isinstance(record,dict):
        raise ValueError('bootstrap lacks platform build provenance')
    # Bind the retained provenance selector to real repository source bytes.
    # Exact reconstructed evidence below checks the remaining fields and schema.
    for name,expected in appliance.bootstrap_recipe(root).items():
        source=subprocess.check_output(['git','-C',str(root),'show',f'{revision}:{name}'],stderr=subprocess.DEVNULL)
        if hashlib.sha256(source).hexdigest()!=expected:
            raise ValueError('bootstrap assembly revision differs from current recipe')
    fes_platform.verify_retained(root,fogcast,record,assembly_revision=revision)
    return revision


def private_card(directory):
    directory=Path(directory)
    if directory.is_symlink() or not directory.is_dir() or set(p.name for p in directory.iterdir())!={'card.img','evidence.json'}:
        raise ValueError('appliance card output is not an immutable artifact directory')
    if directory.stat().st_mode & 0o777 != 0o700:
        raise ValueError('appliance card directory must be private (0700)')
    for name in ('card.img','evidence.json'):
        if appliance.regular(directory/name).stat().st_mode & 0o777 != 0o600:
            raise ValueError('appliance card and evidence must be private (0600)')


def prepare(root,profile,release_directory,bootstrap_directory,scratch,*,agent_config=None,unprovisioned=False):
    """Establish trust from cold receipts, selected source builds and locked tools.

    Supplied evidence is compared to freshly reconstructed evidence; none of its
    claimed digests, source revisions or successful checks are trusted as input.
    """
    rootfs,kernel,provenance,fogcast,env,lock=appliance.verified_inputs(root,profile)
    manifest=appliance.load_manifest(Path(release_directory)/'release.json')
    expected_release=appliance.export_release(scratch/'expected-release',rootfs,kernel,version=manifest['version'],provenance=provenance)
    compare_bundle(release_directory,expected_release.directory,('rootfs.img','release.json','evidence.json'))
    sys.path.insert(0,str(Path(__file__).resolve().parent));import media
    runtime=os.environ.get('CONTAINER_RUNTIME','docker')
    runner=object.__new__(media.Runner);runner.root=root;runner.runtime=runtime
    runner.container=appliance.cached_media_container(root,runtime,lock)
    assembly_revision=retained_assembly_revision(root,bootstrap_directory,fogcast)
    binary=scratch/'fes-boot'
    binary_sha,platform_build=build_selected_binary(root,fogcast,binary,env)
    expected_bootstrap=appliance.assemble_bootstrap(scratch/'expected-bootstrap',binary,expected_release.manifest,kernel,
        runner=runner,binary_source_revision=provenance.fogcast_revision,assembly_revision=assembly_revision,platform_build=platform_build)
    compare_bundle(bootstrap_directory,expected_bootstrap.directory,('linux.img','evidence.json'))
    # Extract and compare the factory's idle bytes to the selected native lock.
    idle=scratch/'idle.rbf'
    runner.disk(['debugfs','-R',f'dump /usr/share/mister-runtime/idle.rbf "{runner.path(idle)}"',runner.path(expected_release.image)])
    policy=tomllib.loads((root/'image/build/native-inputs.toml').read_text())
    idle_lock=policy['idle_rbf']
    splash_lock=policy['splash_rbf']
    if idle_lock['install_path']!='/usr/share/mister-runtime/idle.rbf':raise ValueError('selected idle install path differs')
    if splash_lock['fat_destination']!='/menu.rbf':raise ValueError('selected splash FAT destination differs')
    verify_file(idle,idle_lock['size'],idle_lock['sha256'],'selected factory idle')
    splash=scratch/'splash.rbf'
    native_splash=root/'image/build/cache/target-image/native/splash.rbf'
    if native_splash.is_file() and not native_splash.is_symlink():
        shutil.copyfile(native_splash,splash)
        verify_file(splash,splash_lock['size'],splash_lock['sha256'],'selected splash')
    else:
        shutil.copyfile(idle,splash)
        verify_file(splash,splash_lock['size'],splash_lock['sha256'],'selected splash')
    kernel_copy=scratch/'kernel';shutil.copyfile(kernel,kernel_copy)
    verify_file(kernel_copy,lock.kernel.size,lock.kernel.sha256,'kernel snapshot')
    uboot_source=root/'out/work/boot-media'/('image-creator-'+lock.commit)/lock.uboot.path
    uboot=scratch/'uboot';shutil.copyfile(appliance.regular(uboot_source),uboot)
    verify_file(uboot,lock.uboot.size,lock.uboot.sha256,'U-Boot snapshot')
    config,config_sha=media.resolve_agent_config(agent_config,scratch,auto=not unprovisioned)
    launcher,launcher_sha=media.resolve_launcher_config(config,scratch,auto=not unprovisioned and agent_config is None)
    inputs=Inputs(expected_release.image,expected_bootstrap.image,expected_release.manifest,idle,kernel_copy,uboot,config,launcher,splash)
    bindings={'format':1,'kind':'fes-appliance-card','profile':profile,'release_manifest_sha256':digest(expected_release.manifest),
        'release_evidence_sha256':digest(expected_release.evidence),'bootstrap_evidence_sha256':digest(expected_bootstrap.evidence),
        'factory_image_sha256':provenance.rootfs_sha256,'bootstrap_sha256':digest(expected_bootstrap.image),
        'bootstrap_binary_sha256':digest(binary),'fogcast_revision':provenance.fogcast_revision,'fes_revision':provenance.fes_revision,
        'runtime_revision':provenance.runtime_revision,'kernel_sha256':lock.kernel.sha256,'uboot_sha256':lock.uboot.sha256,
        'container_identity':runner.container,'recipe':{name:digest(root/name) for name in RECIPE_FILES},
        'agent_config_sha256':config_sha,'launcher_config_sha256':launcher_sha,'hardware':'not-run'}
    return inputs,bindings,runner,lock


def reconstruct(scratch,inputs,bindings,runner):
    arguments={name:runner.path(path) if path is not None else None for name,path in asdict(inputs).items()}
    specification=scratch/'inputs.json';specification.write_bytes(appliance.canonical(arguments));specification.chmod(0o600)
    results=[]
    for name in ('first.img','second.img'):
        results.append(json.loads(runner.disk(['python3','/work/scripts/appliance_media_inside.py','assemble',
            '--output',runner.path(scratch/name),'--inputs',runner.path(specification)])))
    if results[0]!=results[1]:raise ValueError('independent appliance card assembly results differ')
    compare_card(scratch/'first.img',scratch/'second.img')
    if digest(scratch/'first.img')!=results[0]['image_sha256']:raise ValueError('card assembly evidence differs from bytes')
    evidence={**bindings,'output':{'path':'card.img','size':(scratch/'first.img').stat().st_size,'sha256':results[0]['image_sha256']},
        'layout_id':results[0]['layout_id'],'geometry':results[0]['geometry'],
        'assembly_sha256':[results[0]['image_sha256'],results[1]['image_sha256']],
        'checks':{'structural':'pass','two_pass_reproducibility':'pass'},'owned_paths':results[0]['paths']}
    evidence_path=scratch/'evidence.json';evidence_path.write_bytes(appliance.canonical(evidence));evidence_path.chmod(0o600)
    return scratch/'first.img',evidence_path


def publish(output,image,evidence,*,check_recipe=None):
    output=Path(output);result=Result(output)
    if output.exists() or output.is_symlink():
        private_card(output)
        compare_card(result.image,image)
        if appliance.regular(result.evidence).read_bytes()!=evidence.read_bytes():raise ValueError('existing appliance card evidence differs')
        if check_recipe:check_recipe()
        return result
    output.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.appliance-publish-',dir=output.parent) as temporary:
        bundle=Path(temporary)/'bundle';bundle.mkdir(mode=0o700)
        for name,source in (('card.img',image),('evidence.json',evidence)):
            target=bundle/name;shutil.copyfile(source,target);target.chmod(0o600)
            with target.open('rb') as stream:os.fsync(stream.fileno())
        compare_card(bundle/'card.img',image)
        appliance.sync_directory(bundle)
        if check_recipe:check_recipe()
        bundle.rename(output);appliance.sync_directory(output.parent)
    return result


def execute(root,command,release,bootstrap,output,*,profile=PROFILE,agent_config=None,unprovisioned=False):
    root=Path(root).resolve();output=Path(output).absolute()
    if agent_config and unprovisioned:raise ValueError('agent configuration and unprovisioned mode are mutually exclusive')
    sys.path.insert(0,str(Path(__file__).resolve().parent));import media
    with media.operation(root,profile):
        scratch_root=root/'out/tmp';scratch_root.mkdir(parents=True,exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='appliance-card-',dir=scratch_root) as temporary:
            scratch=Path(temporary)
            inputs,bindings,runner,_=prepare(root,profile,release,bootstrap,scratch,agent_config=agent_config,unprovisioned=unprovisioned)
            image,evidence=reconstruct(scratch,inputs,bindings,runner)
            require_current_recipe(root,bindings)
            if command=='build':return publish(output,image,evidence,check_recipe=lambda:require_current_recipe(root,bindings))
            if command!='verify':raise ValueError('unknown appliance card operation')
            result=Result(output)
            private_card(output)
            compare_card(result.image,image)
            if set(p.name for p in output.iterdir())!={'card.img','evidence.json'} or appliance.regular(result.evidence).read_bytes()!=evidence.read_bytes():
                raise ValueError('appliance card evidence differs from current verified inputs')
            require_current_recipe(root,bindings)
            return result


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    commands=parser.add_subparsers(dest='command',required=True)
    for name in ('build','verify'):
        child=commands.add_parser(name);child.add_argument('--release',type=Path,required=True)
        child.add_argument('--bootstrap',type=Path,required=True);child.add_argument('--output',type=Path,required=True)
        child.add_argument('--profile',choices=[PROFILE],default=PROFILE)
        provisioning=child.add_mutually_exclusive_group();provisioning.add_argument('--agent-config',type=Path)
        provisioning.add_argument('--unprovisioned',action='store_true')
    args=parser.parse_args()
    try:
        sys.path.insert(0,str(Path(__file__).resolve().parent));import media
        with media.termination_handling():
            result=execute(Path(__file__).resolve().parents[1],args.command,args.release,args.bootstrap,args.output,
                profile=args.profile,agent_config=args.agent_config,unprovisioned=args.unprovisioned)
        print(result.image);print('Appliance disk verified by exact reconstruction; hardware not run. No block device written.')
    except media.TerminationRequested as error:parser.exit(128+error.signum,f'appliance media: {error}\n')
    except (OSError,ValueError,subprocess.CalledProcessError) as error:parser.exit(1,f'appliance media: {error}\n')


if __name__=='__main__':main()
