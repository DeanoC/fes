#!/usr/bin/env python3
"""Observe or reboot only DE25 J8G through its serial-by-id console."""

import argparse
import json
import os
from pathlib import Path
import select
import sys
import termios
import time


J8G_SERIAL = 'TRWU8J8G'
J8G_DEVICE = '/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0'
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
    # Use the serial identity path and refuse any resolved port except the
    # observed J8G ttyUSB2. In particular, never touch TRWU8CBL ttyUSB0/1.
    if not os.path.islink(J8G_DEVICE):
        raise RuntimeError('J8G serial-by-id link is missing')
    resolved = os.path.realpath(J8G_DEVICE)
    if resolved != '/dev/ttyUSB2':
        raise RuntimeError(f'J8G serial-by-id link resolves unexpectedly: {resolved}')
    flags = os.O_RDWR if writable else os.O_RDONLY
    fd = os.open(J8G_DEVICE, flags | os.O_NOCTTY | os.O_NONBLOCK)
    attrs = termios.tcgetattr(fd)
    attrs[0] = 0
    attrs[1] = 0
    attrs[2] = termios.CS8 | termios.CREAD | termios.CLOCAL
    attrs[3] = 0
    attrs[4] = termios.B115200
    attrs[5] = termios.B115200
    termios.tcsetattr(fd, termios.TCSANOW, attrs)
    return fd


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
    parser.add_argument('--menu-confirmed', action='store_true',
                        help='confirm the claimed kit has returned to MENU after Stop')
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
    if args.reboot and not args.menu_confirmed:
        parser.error('serial reboot requires verified MENU (--menu-confirmed); use --probe if state is unknown')
    if args.menu_confirmed and not args.reboot:
        parser.error('--menu-confirmed requires --reboot')
    fd = None
    try:
        if args.receipt_from:
            observation = json.loads(args.receipt_from.read_text(encoding='utf-8'))
            after = observation.get('after') or {}
            if (observation.get('serial') != J8G_SERIAL or
                    observation.get('device') != J8G_DEVICE or
                    observation.get('resolved_device') != '/dev/ttyUSB2' or
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
        fd = open_j8g(writable=args.reboot)
        initial = capture(fd, args.observe_seconds)
        result = {'schema': 1, 'serial': J8G_SERIAL, 'device': J8G_DEVICE,
                  'resolved_device': os.path.realpath(J8G_DEVICE),
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
