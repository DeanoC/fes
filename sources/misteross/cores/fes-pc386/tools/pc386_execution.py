"""Capture the pc386 Quartus and Yosys execution environment."""
import hashlib
import os
import stat
import platform
from pathlib import Path
import re
import shutil
import subprocess
import sys
import sysconfig
from scripts import fes_build_common as board


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def tree_digest(root):
    digest, count = hashlib.sha256(), 0
    for path in sorted(Path(root).rglob('*')):
        if path.is_file():
            digest.update(str(path.relative_to(root)).encode() + b'\0')
            digest.update(bytes.fromhex(sha(path)))
            count += 1
    if not count:
        raise board.BuildError('empty tool installation')
    return {'sha256': digest.hexdigest(), 'files': count}


def runtime_dependencies(executables, env, quartus_native):
    files = {}
    for binary in sorted(set(executables)):
        with binary.open('rb') as stream:
            if stream.read(4) != b'\x7fELF':
                continue
        # qenv.sh prepends this directory for the native Quartus processes.
        native_env = dict(env, LD_LIBRARY_PATH=str(quartus_native)) if binary.parent == quartus_native else env
        result = subprocess.run(['/usr/bin/ldd', str(binary)], env=native_env,
                                capture_output=True, text=True, check=False)
        output = result.stdout + result.stderr
        if 'not found' in output or (result.returncode and
                'not a dynamic executable' not in output and 'statically linked' not in output):
            raise board.BuildError('cannot identify runtime libraries for ' + str(binary))
        for name in re.findall(r'(?:=>\s+|^\s*)(/[^\s]+)', result.stdout, re.MULTILINE):
            files[name] = sha(Path(name))
    return dict(sorted(files.items()))


def path_inventory(value):
    """Bind every executable selectable through the preserved search path."""
    result = {}
    for entry in value.split(os.pathsep):
        if not entry or not Path(entry).is_absolute():
            raise board.BuildError('pc386 execution PATH entries must be absolute and nonempty')
        if entry in result:
            continue
        directory = Path(entry)
        if not directory.exists():
            result[entry] = None
            continue
        if not directory.is_dir():
            raise board.BuildError('pc386 execution PATH entry is not a directory: ' + entry)
        resolved = directory.resolve(strict=True)
        commands = {}
        for path in sorted(directory.iterdir()):
            try:
                if not path.is_file() or not os.access(path, os.X_OK):
                    continue
                target = path.resolve(strict=True)
                before = target.stat()
                if not stat.S_ISREG(before.st_mode):
                    raise board.BuildError('PATH executable is not regular: ' + str(path))
                digest = sha(target)
                after = target.stat()
                if ((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns, before.st_mode)
                        != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns, after.st_mode)
                        or path.resolve(strict=True) != target):
                    raise board.BuildError('PATH executable changed while recording: ' + str(path))
                commands[path.name] = {'target': str(target), 'sha256': digest, 'mode': stat.S_IMODE(after.st_mode)}
            except FileNotFoundError as exc:
                raise board.BuildError('PATH changed while recording: ' + str(path)) from exc
        if directory.resolve(strict=True) != resolved:
            raise board.BuildError('PATH directory changed while recording: ' + entry)
        result[entry] = {'directory': str(resolved), 'commands': commands}
    return result


def execution_inputs(paths, env, _unused_gpu_device=0):
    # Quartus uses device databases, Tcl and native DLLs outside its wrapper.
    # Bind the complete install trees; no GPU or driver enters this lane.
    installations = {str(paths['quartus'].parent.parent): tree_digest(paths['quartus'].parent.parent),
                     str(paths['yosys'].parent.parent): tree_digest(paths['yosys'].parent.parent)}
    helpers = {'python': Path(sys.executable), 'bash': Path('/bin/bash'), 'sh': Path('/bin/sh')}
    for name in ('as', 'ld', 'git', 'uname', 'dirname', 'basename', 'which',
                 'tail', 'grep', 'sed', 'uniq', 'env', 'locale', 'awk'):
        path = shutil.which(name, path=env['PATH'])
        if path is None:
            raise board.BuildError('missing build helper: ' + name)
        helpers[name] = Path(path)
    # Bind the interpreter actually used by build()/the proof subprocesses,
    # including its standard-library sources and native extension modules.
    # Bytecode caches are derived from these sources and change during imports.
    stdlib = Path(sysconfig.get_path('stdlib'))
    support = {str(p): sha(p) for p in sorted(stdlib.rglob('*')) if p.is_file()
               and not {'__pycache__', 'site-packages', 'dist-packages'} & set(p.relative_to(stdlib).parts)
               and p.suffix not in ('.pyc', '.pyo')}
    if not support:
        raise board.BuildError('missing Python standard-library support')
    native = paths['quartus'].parent.parent / 'linux64'
    executables = [*helpers.values(), paths['yosys'], paths['yosys'].with_name('yosys-abc'),
                   *(native / ('quartus_' + name) for name in ('sh', 'map', 'fit', 'asm', 'sta')),
                   *(Path(p) for p in support if p.endswith('.so'))]
    libraries = runtime_dependencies(executables, env, native)
    for name in ('/etc/os-release', '/etc/ld.so.cache'):
        if Path(name).is_file():
            libraries[name] = sha(name)
    return {'version': 2, 'toolchain': 'quartus', 'installations': installations,
            'path_commands': path_inventory(env['PATH']),
            'helpers': {name: {'path': str(p), 'sha256': sha(p)} for name, p in helpers.items()},
            'python': {'executable': sys.executable, 'version': sys.version,
                       'prefix': sys.prefix, 'base_prefix': sys.base_prefix, 'support': support},
            'runtime_libraries': dict(sorted(libraries.items())),
            'system': platform.system(), 'machine': platform.machine(), 'kernel': platform.release(),
            'environment': dict(env, HOME='<private-build-home>')}
