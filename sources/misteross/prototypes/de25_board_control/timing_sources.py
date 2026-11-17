"""Remove the exact obsolete generated JTAG exception; add no timing cuts."""
import hashlib
from pathlib import Path

COMMAND = ('set_false_path -from [get_registers *altera_jtag_src_crosser:*|sink_data_buffer*] '
           '-to [get_registers *altera_jtag_src_crosser:*|src_data*]')
REPLACEMENT = '# Removed obsolete type-qualified JTAG exception; no replacement timing cut.'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def paths(project):
    project = Path(project)
    result = sorted(project.rglob('altera_avalon_st_jtag_interface.sdc'))
    if not result or any(p.is_symlink() or any(q.is_symlink() for q in p.parents) for p in result):
        raise ValueError('complete regular generated JTAG SDC inventory required')
    return result


def replacement(original):
    text = original.decode('utf-8')
    commands = [line.strip() for line in text.splitlines() if line.strip() and not line.lstrip().startswith('#')]
    if commands != [COMMAND] or text.count(COMMAND) != 1:
        raise ValueError('generated JTAG constraints changed; review before modifying')
    return text.replace(COMMAND, REPLACEMENT).encode()


def repair(project):
    """Deleting this exception can only preserve or strengthen timing checks.

    The reviewed routed candidate contains neither selected register set. The
    literal generated filter is obsolete; changing it to a broader wildcard
    would exempt other real paths. Preserve every other constraint and record
    the complete source delta instead.
    """
    files = []
    for path in paths(project):
        before = path.read_bytes()
        after = replacement(before)
        path.write_bytes(after)
        files.append(dict(path=str(path.relative_to(project)), before_sha256=sha(before), after_sha256=sha(after)))
    return dict(format=1, scope='remove-obsolete-jtag-exception-v1', files=files)


def verify(project, record):
    if (not isinstance(record, dict) or set(record) != {'format', 'scope', 'files'} or
            type(record['format']) is not int or record['format'] != 1 or
            record['scope'] != 'remove-obsolete-jtag-exception-v1' or not isinstance(record['files'], list)):
        raise ValueError('complete generated JTAG constraint repair receipt required')
    actual = paths(project)
    if len(actual) != len(record['files']):
        raise ValueError('generated JTAG SDC inventory changed')
    for path, row in zip(actual, record['files']):
        if not isinstance(row, dict) or set(row) != {'path', 'before_sha256', 'after_sha256'} or row['path'] != str(path.relative_to(project)):
            raise ValueError('generated JTAG SDC receipt path changed')
        after = path.read_bytes()
        if after.count(REPLACEMENT.encode()) != 1 or sha(after) != row['after_sha256']:
            raise ValueError('generated JTAG SDC repair changed')
        before = after.replace(REPLACEMENT.encode(), COMMAND.encode())
        if sha(before) != row['before_sha256'] or replacement(before) != after:
            raise ValueError('generated JTAG SDC repair changed other constraints')
    return record
