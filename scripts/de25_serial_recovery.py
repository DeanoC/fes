#!/usr/bin/env python3
"""Observe or reboot the DE25 J8G console after verifying its USB identity."""

import argparse
import json
import os
from pathlib import Path
import re
import select
import sys
import termios
import time


J8G_SERIAL = 'TRWU8J8G'
J8G_DEVICE = '/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0'
DEV_ROOT = Path('/dev')
SYS_ROOT = Path('/sys')
TTYUSB_DEVICE_RE = re.compile(r'^/dev/ttyUSB[0-9]+$')
OTHER_BOARD_SERIAL = 'TRWU8CBL'
BOOT_MARKERS = (b'Linux version ', b'Starting kernel', b'Run /sbin/init as init process')
LOGIN_MARKERS = (b'login:', b'Login:')
LINUX_TIOCSBRK = 0x5427
LINUX_TIOCCBRK = 0x5428


def classify_console(data):
    boot = any(marker in data for marker in BOOT_MARKERS)
    login = any(marker in data for marker in LOGIN_MARKERS)
    if login:
        state = 'login-prompt'
    elif boot:
        state = 'linux-boot'
    elif data:
        state = 'console-output'
    else:
        state = 'quiet'
    return {'state': state, 'linux_boot_seen': boot, 'login_prompt_seen': login,
            'bytes_read': len(data)}


def open_j8g(writable=False):
    # Resolve the by-id link and confirm its USB identity from sysfs because
    # ttyUSB numbering changes across enumeration. Never select TRWU8CBL.
    link_path = DEV_ROOT / Path(J8G_DEVICE).relative_to('/dev')
    if not link_path.exists() or not link_path.is_symlink():
        raise RuntimeError('J8G serial-by-id link is missing')

    resolved_path = link_path.resolve(strict=True)
    try:
        resolved_name = resolved_path.relative_to(DEV_ROOT.resolve(strict=True))
    except ValueError as error:
        raise RuntimeError(f'J8G serial-by-id link resolves outside {DEV_ROOT}: {resolved_path}') from error
    resolved_device = f'/dev/{resolved_name.as_posix()}'
    if len(resolved_name.parts) != 1 or not TTYUSB_DEVICE_RE.fullmatch(resolved_device):
        raise RuntimeError(f'J8G serial-by-id link must resolve to /dev/ttyUSB<number>: {resolved_device}')

    tty_name = resolved_name.name
    try:
        sysfs_root = SYS_ROOT.resolve(strict=True)
        sysfs_device = (SYS_ROOT / 'class' / 'tty' / tty_name / 'device').resolve(strict=True)
        sysfs_device.relative_to(sysfs_root / 'devices')
    except (OSError, ValueError) as error:
        raise RuntimeError(f'cannot resolve {tty_name} through sysfs: {error}') from error
    if sysfs_device.name != tty_name:
        raise RuntimeError(f'sysfs device does not identify {tty_name}: {sysfs_device}')

    interface_path = sysfs_device.parent
    usb_device_path = interface_path.parent
    try:
        usb_interface = (interface_path / 'bInterfaceNumber').read_text(encoding='ascii').strip()
        usb_serial = (usb_device_path / 'serial').read_text(encoding='ascii').strip()
        usb_vendor = (usb_device_path / 'idVendor').read_text(encoding='ascii').strip()
        usb_product_id = (usb_device_path / 'idProduct').read_text(encoding='ascii').strip()
        usb_product = (usb_device_path / 'product').read_text(encoding='ascii').strip()
    except OSError as error:
        raise RuntimeError(f'cannot read {tty_name} USB identity from sysfs: {error}') from error

    if usb_serial == OTHER_BOARD_SERIAL:
        raise RuntimeError(f'refusing USB serial {OTHER_BOARD_SERIAL}; only {J8G_SERIAL} is allowed')
    if usb_serial != J8G_SERIAL:
        raise RuntimeError(f'refusing unexpected USB serial {usb_serial!r}; expected {J8G_SERIAL}')
    if usb_interface != '02':
        raise RuntimeError(f'refusing {J8G_SERIAL} USB interface {usb_interface!r}; expected 02')
    if (usb_vendor, usb_product_id, usb_product) != ('09fb', '6026', 'DE25-Nano'):
        raise RuntimeError(
            f'refusing unexpected USB identity {usb_vendor}:{usb_product_id} {usb_product!r}')

    identity = {
        'resolved_device': resolved_device,
        'usb_interface': usb_interface,
        'usb_serial': usb_serial,
        'sysfs_device': str(sysfs_device),
    }
    flags = os.O_RDWR if writable else os.O_RDONLY
    fd = os.open(str(link_path), flags | os.O_NOCTTY | os.O_NONBLOCK)
    try:
        opened_device = os.fstat(fd).st_rdev
        verified_device = os.stat(resolved_path).st_rdev
        if opened_device != verified_device:
            raise RuntimeError(
                f'opened J8G device number {opened_device} does not match verified device {verified_device}')
        attrs = termios.tcgetattr(fd)
        attrs[0] = 0
        attrs[1] = 0
        attrs[2] = termios.CS8 | termios.CREAD | termios.CLOCAL
        attrs[3] = 0
        attrs[4] = termios.B115200
        attrs[5] = termios.B115200
        termios.tcsetattr(fd, termios.TCSANOW, attrs)
        return fd, identity
    except Exception:
        os.close(fd)
        raise


def capture(fd, seconds):
    end = time.monotonic() + seconds
    chunks = []
    while time.monotonic() < end:
        ready, _, _ = select.select([fd], [], [], min(0.5, max(0, end - time.monotonic())))
        if ready:
            try:
                chunk = os.read(fd, 4096)
            except BlockingIOError:
                continue
            if chunk:
                chunks.append(chunk)
    return b''.join(chunks)


def sysrq_reboot(fd):
    termios.tcflush(fd, termios.TCIOFLUSH)
    import fcntl
    # Python's termios module omits these Linux ioctls on some distributions.
    # The Linux UAPI values are stable; use module constants when exported.
    fcntl.ioctl(fd, getattr(termios, 'TIOCSBRK', LINUX_TIOCSBRK))
    time.sleep(0.25)
    fcntl.ioctl(fd, getattr(termios, 'TIOCCBRK', LINUX_TIOCCBRK))
    time.sleep(0.15)
    if os.write(fd, b'b') != 1:
        raise RuntimeError('serial SysRq reboot command was not fully written')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--probe', action='store_true', help='receive-only console observation')
    mode.add_argument('--reboot', action='store_true', help='send serial SysRq BREAK+b reboot')
    mode.add_argument('--receipt-from', type=Path,
                      help='write a restore receipt from a completed serial reboot observation')
    parser.add_argument('--serial', required=True, choices=(J8G_SERIAL,))
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--observe-seconds', type=float, default=8)
    parser.add_argument('--boot-seconds', type=float, default=120)
    parser.add_argument('--before-boot-id')
    parser.add_argument('--after-boot-id')
    parser.add_argument('--target-id')
    args = parser.parse_args(argv)
    if args.observe_seconds <= 0 or args.boot_seconds <= 0:
        parser.error('observation intervals must be positive')
    fd = None
    try:
        if args.receipt_from:
            observation = json.loads(args.receipt_from.read_text(encoding='utf-8'))
            after = observation.get('after') or {}
            resolved_device = observation.get('resolved_device')
            if (observation.get('serial') != J8G_SERIAL or
                    observation.get('device') != J8G_DEVICE or
                    not isinstance(resolved_device, str) or
                    not TTYUSB_DEVICE_RE.fullmatch(resolved_device) or
                    observation.get('usb_serial') != J8G_SERIAL or
                    observation.get('usb_interface') != '02' or
                    observation.get('command') != 'serial-sysrq-break-b' or
                    observation.get('input_sent') is not True or
                    after.get('linux_boot_seen') is not True or
                    not args.before_boot_id or not args.after_boot_id or
                    args.after_boot_id == args.before_boot_id or not args.target_id):
                raise RuntimeError('serial observation or network identity is incomplete')
            result = {'schema': 1, 'serial': J8G_SERIAL, 'device': J8G_DEVICE,
                      'command': 'serial-sysrq-break-b', 'linux_boot_seen': True,
                      'before_boot_id': args.before_boot_id,
                      'after_boot_id': args.after_boot_id,
                      'target_id': args.target_id,
                      'observation': str(args.receipt_from)}
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
            print(json.dumps({'serial': J8G_SERIAL, 'before_boot_id': args.before_boot_id,
                              'after_boot_id': args.after_boot_id, 'target_id': args.target_id}, sort_keys=True))
            return 0
        fd, identity = open_j8g(writable=args.reboot)
        initial = capture(fd, args.observe_seconds)
        result = {'schema': 1, 'serial': J8G_SERIAL, 'device': J8G_DEVICE,
                  **identity,
                  'input_sent': False, 'before': classify_console(initial),
                  'before_text': initial.decode('utf-8', 'replace')}
        if args.reboot:
            sysrq_reboot(fd)
            result['input_sent'] = True
            result['command'] = 'serial-sysrq-break-b'
            output = capture(fd, args.boot_seconds)
            result['after'] = classify_console(output)
            result['after_text'] = output.decode('utf-8', 'replace')
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
        print(json.dumps({key: result[key] for key in ('serial', 'resolved_device', 'input_sent', 'before') + (('after',) if args.reboot else ())}, sort_keys=True))
        return 0
    except (OSError, RuntimeError, termios.error, json.JSONDecodeError) as error:
        print(f'DE25 serial recovery: {error}', file=sys.stderr)
        return 2
    finally:
        if fd is not None:
            os.close(fd)


if __name__ == '__main__':
    raise SystemExit(main())
