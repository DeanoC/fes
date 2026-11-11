"""Extract the locked vendor FPGA modules from regular BSP image files only."""
import json
import re
from pathlib import Path, PurePosixPath
import struct
import subprocess
import tempfile
import zipfile

import boot

LANE = Path(__file__).resolve().parent


def load_lock():
    record = json.loads((LANE / 'kernel-modules.lock.json').read_text())
    vendor = boot.load_lock()
    kernel = next(row for row in vendor['boot_files'] if row['name'] == 'Image')
    if (record['format'] != 1 or record['kernel_image_sha256'] != kernel['sha256']
            or not re.fullmatch(r'[0-9A-Za-z][0-9A-Za-z.+_-]{0,127}', record['kernel'])):
        raise ValueError('module lock differs from selected vendor kernel')
    names = set()
    for row in record['modules']:
        path = PurePosixPath(row['path'])
        if (path.parts[:3] != ('kernel', 'drivers', 'fpga') or len(path.parts) != 4
                or path.suffix != '.ko' or row['path'] in names):
            raise ValueError('invalid or duplicate FPGA module path')
        names.add(row['path'])
    if len(names) != 4:
        raise ValueError('expected the four selected vendor FPGA modules')
    return record


def module_info(path):
    """Read ELF64 AArch64 .modinfo without executing the module."""
    return module_info_bytes(path.read_bytes())


def module_info_bytes(data):
    if (len(data) < 64 or data[:6] != b'\x7fELF\x02\x01'
            or struct.unpack_from('<HH', data, 16) != (1, 183)):
        raise ValueError('expected AArch64 relocatable ELF module')
    offset = struct.unpack_from('<Q', data, 40)[0]
    size, count, strings_index = struct.unpack_from('<HHH', data, 58)
    if size != 64 or not count or strings_index >= count or offset + size * count > len(data):
        raise ValueError('invalid module section table')
    sections = [struct.unpack_from('<IIQQQQIIQQ', data, offset + n * size) for n in range(count)]
    def section(row):
        start, length = row[4:6]
        if start + length > len(data):
            raise ValueError('module section outside file')
        return data[start:start + length]
    strings = section(sections[strings_index])
    matches = []
    for row in sections:
        name = row[0]
        end = strings.find(b'\0', name)
        if name >= len(strings) or end < 0:
            raise ValueError('invalid module section name')
        if strings[name:end] == b'.modinfo':
            matches.append(section(row))
    if len(matches) != 1:
        raise ValueError('expected one module info section')
    info = {}
    for item in matches[0].split(b'\0'):
        if not item:
            continue
        key, separator, value = item.partition(b'=')
        if not separator:
            raise ValueError('invalid module info entry')
        key, value = key.decode('ascii'), value.decode('ascii')
        if key in ('name', 'depends', 'vermagic'):
            if key in info:
                raise ValueError('duplicate module identity field')
            info[key] = value
    if set(info) != {'name', 'depends', 'vermagic'}:
        raise ValueError('missing module identity fields')
    return info


def verify_modules(root, record):
    selected = {}
    for row in record['modules']:
        path = root / row['path']
        boot.check_payload(path, row)
        info = module_info(path)
        if info['vermagic'].split(' ', 1)[0] != record['kernel']:
            raise ValueError('module vermagic differs from selected kernel')
        name = info['name'].replace('-', '_')
        if name in selected or name != path.stem.replace('-', '_'):
            raise ValueError('module name differs or is duplicated')
        selected[name] = (row['path'], info)
    dependencies = []
    for path, info in selected.values():
        names = [n.replace('-', '_') for n in info['depends'].split(',') if n]
        if any(n not in selected for n in names):
            raise ValueError('module has dependency outside selected closure')
        dependencies.append(path + ':' + ''.join(' ' + selected[n][0] for n in names))
    return '\n'.join(dependencies) + '\n'


def verify_rootfs(image, archive, members, read_image):
    record = load_lock()
    prefix = 'lib/modules/' + record['kernel'] + '/'
    if {name for name in members if name.startswith('lib/modules/') and name.endswith('.ko')} != {
            prefix + row['path'] for row in record['modules']}:
        raise ValueError('rootfs kernel-module file set differs')
    with tempfile.TemporaryDirectory(prefix='de25-module-check-') as temporary:
        root = Path(temporary)
        for row in record['modules']:
            name = prefix + row['path']
            item = members.get(name)
            if item is None or not item.isfile():
                raise ValueError('missing regular rootfs module: ' + name)
            data = archive.extractfile(item).read()
            if read_image(image, 'cat /' + name) != data:
                raise ValueError('rootfs tar and ext4 disagree on ' + name)
            path = root / row['path']
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        dependencies = verify_modules(root, record).encode()
    expected = {prefix + 'modules.dep': dependencies,
                'etc/fes-kernel-release': (record['kernel'] + '\n').encode(),
                'usr/share/fes/kernel-modules.json': (json.dumps(record, indent=2) + '\n').encode(),
                'etc/init.d/S15fpga': (LANE / 'rootfs/overlay/etc/init.d/S15fpga').read_bytes()}
    for name, data in expected.items():
        item = members.get(name)
        if item is None or not item.isfile() or archive.extractfile(item).read() != data:
            raise ValueError('rootfs kernel-module configuration differs: ' + name)
        if read_image(image, 'cat /' + name) != data:
            raise ValueError('rootfs tar and ext4 disagree on ' + name)
    if members['etc/init.d/S15fpga'].mode & 0o111 != 0o111:
        raise ValueError('FPGA boot initialization is not executable')
    return {'release': record['kernel'], 'modules': len(record['modules']), 'autoload': 'S15fpga'}


def prepare(archive, output, record=None):
    record = load_lock() if record is None else record
    vendor = boot.load_lock()
    boot.check_payload(archive, vendor['bsp'])
    if output.exists() or output.is_symlink():
        raise ValueError('module overlay output already exists')
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.de25-modules-', dir=output.parent) as temporary:
        temporary = Path(temporary)
        partition = temporary / 'vendor-root.ext3'
        part = vendor['partitions'][1]
        offset, length = part['start_sector'] * 512, part['sectors'] * 512
        # Authenticate the entire decompressed image while retaining only p2.
        import hashlib
        digest = hashlib.sha256()
        with zipfile.ZipFile(archive) as zipped:
            entry = zipped.getinfo(vendor['image_member'])
            if entry.file_size != vendor['image_size']:
                raise ValueError('vendor module source image size differs')
            with zipped.open(entry) as source, partition.open('xb') as target:
                position = 0
                while data := source.read(boot.CHUNK):
                    digest.update(data)
                    begin, end = max(position, offset), min(position + len(data), offset + length)
                    if end > begin:
                        block = data[begin-position:end-position]
                        if block == bytes(len(block)):
                            target.seek(len(block), 1)
                        else:
                            target.write(block)
                    position += len(data)
                target.truncate(length)
        if position != vendor['image_size'] or digest.hexdigest() != vendor['image_sha256']:
            raise ValueError('vendor module source image hash differs')
        overlay = temporary / 'overlay'
        root = overlay / 'lib/modules' / record['kernel']
        for row in record['modules']:
            dest = root / row['path']
            dest.parent.mkdir(parents=True, exist_ok=True)
            # debugfs reads a private regular file, never a device or mounted card.
            command = 'dump /lib/modules/' + record['kernel'] + '/' + row['path'] + ' ' + json.dumps(str(dest))
            subprocess.run(['debugfs', '-R', command, str(partition)], check=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        (root / 'modules.dep').write_text(verify_modules(root, record))
        (root / 'modules.alias').write_text('')
        (overlay / 'etc').mkdir()
        (overlay / 'etc/fes-kernel-release').write_text(record['kernel'] + '\n')
        metadata = overlay / 'usr/share/fes'
        metadata.mkdir(parents=True)
        (metadata / 'kernel-modules.json').write_text(json.dumps(record, indent=2) + '\n')
        overlay.rename(output)
    return record
