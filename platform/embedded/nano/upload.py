#!/usr/bin/env python3
"""Upload one experimental file through an already booted Nano recovery loader.

Default is a file-only plan. --execute claims TRWU8CBL, verifies its recovery
card, downloads to RAM, then writes one fixed boot or package diagnostic file.
No boot, flash, raw SD writes, boot FIT replacement or automatic activation.
"""
import argparse
from contextlib import contextmanager
import datetime
import fcntl
import hashlib
import ipaddress
import json
import os
import pwd
from pathlib import Path
import re
import secrets
import time
import threading

from upload_transport import Console, Transfer
from cold_profile import EXPERIMENTAL_FILE, MAX_FIT_SIZE
import recovery
from upload_memory import ScratchMemory

ROOT = Path(__file__).resolve().parents[3]
BOARD = 'TRWU8CBL'
LIMIT = MAX_FIT_SIZE
DESTINATIONS = {'boot':EXPERIMENTAL_FILE, 'package':'FES/PKGTEST.BIN',
                'metadata':'FES/PROGRAM.JSN', 'selection':'FES/CORESEL.JSN',
                'package-alt':'FES/PKGALT.BIN', 'metadata-alt':'FES/PROGALT.JSN'}
PREFIX_SIZE = 32768 * 512


def sha(data): return hashlib.sha256(data).hexdigest()

def load_kit(directory):
    if any(not 0<(directory/name).stat().st_size<=limit for name,limit in
           (('result.json',131072),('RECOVERY.JSON',16384),('u-boot.itb',8<<20))):
        raise ValueError('recovery metadata or FIT length exceeds its bound')
    record = json.loads((directory / 'result.json').read_text())
    manifest = (directory / 'RECOVERY.JSON').read_bytes()
    description = json.loads(manifest)
    disk = directory / 'nano-recovery-sd.img'
    if (record.get('status') != 'prepared' or record.get('native_handoff') is not False or
            record.get('recovery_phy') != recovery.PHY or record.get('phy_driver_linked') is not True or
            record.get('automatic_writes') is not False or record.get('spl_boot_mode') != 'fat' or
            record.get('spl_payload_path') != 'u-boot.itb' or record.get('spl_fat_partition') != 1 or
            record.get('fat32_start_sector') != 32768 or record.get('fat32_sectors') != 262144 or
            disk.stat().st_size != record.get('sd_image_size') or disk.stat().st_size != 150994944):
        raise ValueError('requires a corrected Nano recovery kit with FAT boot FIT')
    with disk.open('rb') as stream:
        if hashlib.file_digest(stream, 'sha256').hexdigest() != record['sd_image_sha256']:
            raise ValueError('recovery card image differs')
        stream.seek(0); prefix = stream.read(PREFIX_SIZE)
    fit = (directory / 'u-boot.itb').read_bytes()
    required=('format','fit_sha256','spl_sha256','bl31_sha256','sof_sha256','fat32_start_sector',
              'fat32_sectors','native_handoff','automatic_writes','spl_boot_mode','spl_payload_path','spl_fat_partition',
              'recovery_phy','phy_driver_linked')
    if (not fit or len(fit) > 8 << 20 or sha(fit) != record['fit_sha256'] or
            any(key not in description or description[key]!=record.get(key) for key in required) or
            any(description.get(k) != record.get(k) for k in description) or
            prefix[512*512:512*512+len(fit)] != fit):
        raise ValueError('recovery manifest or FIT differs')
    return {'manifest': manifest, 'fit': fit, 'prefix_sha256': sha(prefix),
            'image_sha256': record['sd_image_sha256'], 'fit_sha256': sha(fit)}


def settings(host, mac):
    ipaddress.IPv4Address(host)
    if not re.fullmatch(r'(?:[0-9a-f]{2}:){5}[0-9a-f]{2}', mac) or int(mac[:2], 16) & 1 or mac == '00:00:00:00:00:00':
        raise ValueError('a unicast board MAC is required')


def destination(purpose):
    if not isinstance(purpose,str) or purpose not in DESTINATIONS:raise ValueError('unknown recovery upload purpose')
    return DESTINATIONS[purpose]


def payload_limit(purpose):
    destination(purpose)
    return {'boot': LIMIT, 'package': LIMIT, 'metadata': 16<<10, 'selection': 8192,
            'package-alt': LIMIT, 'metadata-alt': 16<<10}[purpose]


class UploadSession:
    """One exclusive live console and volatile boot marker own their scratch loads.

    Repeated uploads retain the same object and console. Close or any I/O/proof
    failure retires it. A new session never adopts existing RAM reservations.
    """
    def __init__(self, console, kit, host, mac, serve=Transfer, owns=lambda: True):
        settings(host, mac)
        self.console, self.kit, self.host, self.mac = console, dict(kit), host, mac
        self.kit.update(fit=bytes(kit['fit']), manifest=bytes(kit['manifest']))
        self.serve, self.owns = serve, owns
        self.memory = ScratchMemory()
        self.token = secrets.token_hex(16)
        self.retired = False
        self.guard = threading.Lock()
        self.verified = {}

    def close(self):
        if not self.guard.acquire(blocking=False):
            raise ValueError('upload session is active')
        try:
            self.retired = True
        finally:
            self.guard.release()

    def upload(self, payload, receipt, *, purpose='boot'):
        if not 0 < len(payload) <= payload_limit(purpose):
            raise ValueError('payload exceeds the fixed purpose limit')
        payload = bytes(payload)
        with self._operation(receipt):
            self._upload(payload, receipt, purpose)
            self.verified[purpose] = (len(payload), sha(payload))

    def verify(self, payload, receipt, *, purpose='boot'):
        """Freshly read/hash a fixed file already verified in this live session."""
        target = destination(purpose)
        if not 0 < len(payload) <= payload_limit(purpose):
            raise ValueError('payload exceeds the fixed purpose limit')
        expected = (len(payload), sha(bytes(payload)))
        with self._operation(receipt):
            receipt.update(read_only=True, fresh_readback=False, purpose=purpose,
                           payload_bytes=expected[0], payload_sha256=expected[1], destination=target)
            if self.verified.get(purpose) != expected:
                raise ValueError('file was not verified by this live upload session')
            def command(value, timeout=15):
                return self._command(receipt, value, timeout)
            self._observe_memory(command, receipt, 'admit')
            output = command('fatload mmc 0:1 83000000 '+target, 60)
            if not re.search(r'(?m)^\s*'+str(expected[0])+r' bytes read(?:\s|$)', output):
                raise ValueError('fresh SD read length differs')
            digest = command('hash sha256 83000000 '+format(expected[0], 'x'))
            if not re.search(r'(?i)(?<![0-9a-f])'+expected[1]+r'(?![0-9a-f])', digest):
                raise ValueError('fresh SD read hash differs')
            self.memory.loaded(0x83000000, expected[0])
            self._observe_memory(command, receipt, 'complete')
            receipt.update(status='verified', fresh_readback=True, sd_readback_verified=True)

    @contextmanager
    def _operation(self, receipt):
        if receipt.get('commands') != [] or receipt.get('sd_write_attempted', False) is not False or receipt.get('status') == 'verified':
            raise ValueError('upload receipt must be fresh')
        if not self.guard.acquire(blocking=False):
            raise ValueError('upload session is active')
        try:
            if self.retired:
                raise ValueError('upload session is retired')
            receipt.update(status='running', upload_session=self.token, memory_observations=[], sd_write_attempted=False)
            yield
        except BaseException:
            self.retired = True
            receipt['status'] = 'failed'
            receipt['session_retired'] = True
            raise
        finally:
            self.guard.release()

    def _command(self, receipt, value, timeout=15):
        if not self.owns():
            raise ValueError('kit claim ownership changed')
        receipt['stage'] = value
        print('Nano upload: '+value, flush=True)
        output = self.console.command(value, timeout)
        if not self.owns():
            raise ValueError('kit claim ownership changed')
        receipt['commands'].append({'command': value, 'output': output})
        return output

    def _observe_memory(self, command, receipt, stage):
        def marker():
            out = command('echo FES_UPLOAD_SESSION=${fes_upload_session}')
            values = re.findall(r'(?m)^FES_UPLOAD_SESSION=([^\r\n]*)\r?$', out)
            if len(values) != 1:
                raise ValueError('upload session marker observation missing')
            return values[0]
        first = self.memory.baseline is None
        if marker() != ('' if first else self.token):
            raise ValueError('upload session boot marker differs')
        observed = self.memory.observe(command('bdinfo'))
        if first:
            command('setenv fes_upload_session '+self.token)
            if marker() != self.token:
                raise ValueError('upload session boot marker not retained')
        receipt['memory_observations'].append(dict(stage=stage, map=observed))
        receipt['scratch_owned'] = [list(row) for row in self.memory.owned]

    def _upload(self, payload, receipt, purpose):
        kit, host, mac = self.kit, self.host, self.mac
        serve = self.serve
        target=destination(purpose)
        def command(value, timeout=15):
            return self._command(receipt, value, timeout)
        def digest(address, size, expected):
            output = command(f'hash sha256 {address:x} {size:x}')
            if not re.search(r'(?i)(?<![0-9a-f])' + expected + r'(?![0-9a-f])', output):
                raise ValueError('readback hash differs')
        def byte_count(output, size, action):
            return re.search(r'(?m)^\s*'+str(size)+r' bytes '+action+r'(?:\s|$)', output)
        def prefix():
            if '32768 blocks read: OK' not in command('mmc read 83000000 0 8000', 45):
                raise ValueError('protected prefix read failed')
            digest(0x83000000, PREFIX_SIZE, kit['prefix_sha256'])
        def boot_file():
            if not byte_count(command('fatload mmc 0:1 83000000 u-boot.itb', 30),len(kit['fit']),'read'):
                raise ValueError('boot FIT length differs')
            digest(0x83000000, len(kit['fit']), kit['fit_sha256'])
            self.memory.loaded(0x83000000, len(kit['fit']))
        def recovery_manifest(address):
            if not byte_count(command(f'fatload mmc 0:1 {address:x} FES/RECOVERY.JSON'),len(kit['manifest']),'read'):
                raise ValueError('wrong recovery card manifest')
            digest(address,len(kit['manifest']),sha(kit['manifest']))
            self.memory.loaded(address, len(kit['manifest']))
        recovery.check_version(command('version'))
        self._observe_memory(command, receipt, 'admit')
        command('mmc dev 0')
        info = command('mmcinfo')
        if 'SD version' not in info or '3.7 GiB' not in info: raise ValueError('wrong spare card capacity/type')
        if not re.search(r'(?m)^\s*1\s+32768\s+262144\s', command('mmc part')):
            raise ValueError('wrong spare partition layout')
        prefix(); boot_file()
        recovery_manifest(0x82000000)
        receipt['card_verified'] = True
        for value in ('setenv autoload no', 'setenv netretry no', 'setenv bootpretryperiod 10000',
                      'setenv tftptimeout 1000', 'setenv tftptimeoutcountmax 3'):
            command(value)
        def current_mac():
            match = re.search(r'(?m)^FES_UPLOAD_ETHADDR=([^\r\n]*)\r?$',
                              command('echo FES_UPLOAD_ETHADDR=${ethaddr}'))
            if not match: raise ValueError('recovery MAC observation missing')
            return match[1].strip().lower()
        observed = current_mac()
        if not observed:
            command('setenv ethaddr ' + mac)
            observed = current_mac()
        if observed != mac.lower(): raise ValueError('recovery MAC differs; no overwrite permitted')
        receipt['recovery_mac'] = observed
        # The loader probes Ethernet lazily. Probe before sampling its MDIO bus;
        # a positive link observation must precede DHCP, not a fixed startup delay.
        if 'ethernet@10810000' not in command('net list'):
            raise ValueError('expected recovery Ethernet device absent')
        # phy_connect resets the PHY. It must finish before the readiness
        # samples, and DHCP must reuse that same prepared object.
        prepared=command(recovery.PHY['prepare_command'],5)
        expected='FES_RECOVERY_PHY PREPARED address=1 id=00221622 configured=1 reset=0'
        if prepared.splitlines().count(expected)!=1 or any(
                line.startswith('FES_RECOVERY_PHY ') and line!=expected for line in prepared.splitlines()):
            raise ValueError('recovery PHY preparation observation differs')
        receipt['phy_prepared_before_link']=True
        def phy_read(register):
            samples = re.findall(r'(?m)^([0-9a-fA-F]{4})\r?$', command(f'mii read {recovery.PHY["address"]:x} {register:x}',5))
            if len(samples)!=1 or int(samples[0],16)==0xffff:raise ValueError('unknown recovery PHY register')
            return int(samples[0],16)
        phy_id=(phy_read(2)<<16)|phy_read(3)
        if phy_id!=recovery.PHY['id']:raise ValueError('recovery PHY identity differs')
        receipt['phy_id']=phy_id
        deadline = time.monotonic() + 20
        ready = 0
        while time.monotonic() < deadline:
            status = phy_read(1)
            receipt.setdefault('phy_samples', []).append(status)
            # Pinned linux/mii.h: BMSR_LSTATUS=4, BMSR_ANEGCOMPLETE=32.
            ready = ready + 1 if status & 0x24 == 0x24 else 0
            if ready == 2: break
            time.sleep(.25)
        else: raise TimeoutError('recovery PHY link/negotiation deadline')
        receipt['phy_link_verified'] = True
        match = re.search(r'DHCP client bound to address (\d+\.\d+\.\d+\.\d+)', command('dhcp', 25))
        if not match: raise ValueError('no observed DHCP client')
        client = str(ipaddress.IPv4Address(match[1]))
        if client == host or ipaddress.ip_address(client).is_multicast or client == '0.0.0.0':
            raise ValueError('invalid client address')
        receipt['client_ip'] = client
        command('setenv serverip ' + host)
        filename = 'U' + secrets.token_hex(3).upper() + '.BIN'
        server = serve(host, client, filename, payload)
        try:
            server.start()
            if not re.search(r'(?m)^Bytes transferred = '+str(len(payload))+r'\s',command('tftpboot 82000000 '+filename,90)):
                raise ValueError('incomplete download')
            server.thread.join(timeout=3)
            if not server.complete: raise ValueError('host did not observe complete TFTP acknowledgement')
            digest(0x82000000, len(payload), sha(payload))
            self.memory.loaded(0x82000000, len(payload))
        finally: server.close()
        receipt['download_verified'] = True
        self._observe_memory(command, receipt, 'before-sd-write')
        receipt['sd_write_attempted'] = True
        if not byte_count(command(f'fatwrite mmc 0:1 82000000 {target} {len(payload):x}',60),len(payload),'written'):
            raise ValueError('incomplete SD write; experimental file may be partial')
        command(f'mw.b 82800000 a5 {len(payload):x}')
        if not byte_count(command('fatload mmc 0:1 82800000 '+target,60),len(payload),'read'):
            raise ValueError('incomplete SD readback')
        digest(0x82800000, len(payload), sha(payload))
        self.memory.loaded(0x82800000, len(payload))
        if f'{len(payload)} byte(s) were the same' not in command(f'cmp.b 82000000 82800000 {len(payload):x}', 30):
            raise ValueError('SD byte comparison failed')
        receipt['sd_readback_verified'] = True
        prefix(); boot_file(); recovery_manifest(0x83000000)
        self._observe_memory(command, receipt, 'complete')
        receipt['boot_files_unchanged'] = True
        receipt.update(status='verified', stage='complete', destination=target, purpose=purpose, recovery_manifest_unchanged=True)


class Claim:
    def __init__(self, path):
        self.path, self.token = path, secrets.token_hex(8)
        self.claimed = False
    def append(self, text, start=False):
        with open(str(self.path)+'.lock', 'a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            current = self.path.read_text()
            if start and current.rfind('## Active claim') > current.rfind('## Current status — released'):
                raise ValueError('TRWU8CBL is already claimed')
            if not start and self.token not in current[current.rfind('## Active claim'):]:
                raise ValueError('upload claim ownership changed')
            with self.path.open('a') as out: out.write(text)
    def start(self,purpose='boot'):
        target=destination(purpose)
        self.append('\n## Active claim '+datetime.datetime.now(datetime.timezone.utc).isoformat()+
                    '\n- Owner: Nano uploader '+self.token+'; TRWU8CBL only.\n'
                    '- Scope: existing recovery prompt, bounded Ethernet transfer and '+target+' write/readback; no boot/flash/raw SD changes.\n', True)
        self.claimed = True
    def owns(self):
        with open(str(self.path)+'.lock','a') as lock:
            fcntl.flock(lock,fcntl.LOCK_SH)
            current=self.path.read_text()
            return (self.claimed and current.rfind('## Active claim')>current.rfind('## Current status — released') and
                    self.token in current[current.rfind('## Active claim'):])
    def finish(self, parked, status):
        if not self.claimed: return
        title = '## Current status — released' if parked else '- Claim held; recovery needed'
        self.append('\n'+title+' '+datetime.datetime.now(datetime.timezone.utc).isoformat()+
                    '\n- Nano uploader '+self.token+': '+status+'; own UART/TFTP closed. '+
                    ('Existing recovery prompt verified; no boot/image change.\n' if parked else 'Prompt/state uncertain; inspect before releasing.\n'))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--payload',required=True,type=Path)
    parser.add_argument('--recovery-kit',required=True,type=Path)
    parser.add_argument('--host-ip',required=True)
    parser.add_argument('--mac',default='02:fe:25:08:cb:01')
    parser.add_argument('--purpose',choices=tuple(DESTINATIONS),default='boot')
    parser.add_argument('--output',required=True,type=Path)
    parser.add_argument('--kit-status',type=Path)
    parser.add_argument('--execute',action='store_true')
    args=parser.parse_args()
    output=args.output.absolute()
    if (output.exists() or '..' in output.parts or not output.is_relative_to(ROOT/'out') or
            any(p.is_symlink() for p in (output,*output.parents))):
        parser.error('output must be a fresh private directory under this worktree out/')
    home=Path(pwd.getpwuid(int(os.environ.get('SUDO_UID',os.getuid()))).pw_dir)
    authoritative=home/'kit-status/de25-TRWU8CBL-STATUS.md'
    status_file=args.kit_status or authoritative
    if args.execute and (status_file.resolve()!=authoritative.resolve() or not status_file.is_file()):
        parser.error('--execute requires the authoritative de25-TRWU8CBL-STATUS.md in the operator kit-status directory')
    limit=payload_limit(args.purpose)
    with args.payload.open('rb') as src: payload=src.read(limit+1)
    if not 0<len(payload)<=limit: parser.error('payload exceeds the fixed purpose limit')
    kit=load_kit(args.recovery_kit.resolve(strict=True))
    settings(args.host_ip,args.mac.lower())
    output.mkdir(parents=True)
    receipt={'format':1,'status':'planned','board_serial':BOARD,'payload_bytes':len(payload),'payload_sha256':sha(payload),
             'recovery_image_sha256':kit['image_sha256'],'destination':destination(args.purpose),'purpose':args.purpose,'activation':False,
             'commands':[],'sd_write_attempted':False,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    claim=None;console=None
    try:
        if args.execute:
            claim=Claim(status_file);claim.start(args.purpose)
            with (output/'console.log').open('wb') as log:
                try:
                    console=Console('/dev/serial/by-id/usb-TERASIC_DE25-Nano_'+BOARD+'-if02-port0',log)
                    session=UploadSession(console,kit,args.host_ip,args.mac.lower(),owns=claim.owns)
                    try:session.upload(payload,receipt,purpose=args.purpose)
                    finally:session.close()
                finally:
                    if console is not None:
                        if not console.parked and claim.owns():
                            try:console.park()
                            except Exception:pass
                        receipt['prompt_parked']=console.parked
                        console.close()
        print('Nano upload: '+receipt['status'],flush=True)
    except BaseException as error:
        receipt.update(status='failed',error=str(error))
        raise
    finally:
        receipt['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        (output/'result.json').write_text(json.dumps(receipt,indent=2,sort_keys=True)+'\n')
        if claim is not None:claim.finish(receipt.get('prompt_parked',False),receipt['status'])


if __name__=='__main__':
    try:main()
    except (OSError,ValueError,TimeoutError) as error:raise SystemExit('Nano upload: '+str(error))
