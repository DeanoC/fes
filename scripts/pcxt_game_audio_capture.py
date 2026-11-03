"""Supervise one owned HDMI audio capture; no device opens until start()."""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import threading
import time
import wave

class AudioError(RuntimeError): pass
MAX_PROGRESS_STALL_NS = 8_000_000_000

def capture_identity(kit, *, sysroot=Path('/sys'), procroot=Path('/proc'), devroot=Path('/dev'), runner=subprocess.run):
    """Bind ALSA and fanout video to one expected physical USB capture card."""
    if kit != 'A': raise AudioError('only Kit A capture mapping is supported')
    raw = devroot/'v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0'
    loop = devroot/'v4l/by-id/fes-shadowcast-hil'
    def usb(node):
        node=node.resolve(strict=True)
        for parent in (node,*node.parents):
            if (parent/'idVendor').is_file() and (parent/'idProduct').is_file() and (parent/'serial').is_file():
                return {'path':str(parent),'vendor':(parent/'idVendor').read_text().strip().lower(),
                        'product':(parent/'idProduct').read_text().strip().lower(),'serial':(parent/'serial').read_text().strip()}
        raise AudioError('USB capture identity unavailable')
    try:
        card=(procroot/'asound/S3').resolve(strict=True).name
        if not card.startswith('card') or not card[4:].isdigit(): raise AudioError('ALSA card identity unavailable')
        video=raw.resolve(strict=True).name
        loopname=loop.resolve(strict=True).name
        if not video.startswith('video') or not loopname.startswith('video'):
            raise AudioError('capture paths do not resolve to video devices')
        audio_usb=usb(sysroot/'class/sound'/card/'device')
        video_usb=usb(sysroot/'class/video4linux'/video/'device')
        if audio_usb != video_usb or (audio_usb['vendor'],audio_usb['product'],audio_usb['serial']) != ('32ed','3701','KT044001'):
            raise AudioError('ALSA and video physical capture identities differ')
        if (sysroot/'class/video4linux'/loopname/'name').read_text().strip() != 'fes-shadowcast-hil':
            raise AudioError('HIL video loopback identity differs')
        result=runner(['systemctl','show','fes-shadowcast-fanout.service','--property=MainPID','--value'],
                      check=True,capture_output=True,text=True,timeout=5)
        pid=int(result.stdout.strip())
        if pid <= 0: raise AudioError('capture fanout is not running')
        command=(procroot/str(pid)/'cmdline').read_bytes().split(b'\0')
        command=[x.decode() for x in command if x]
        if (not command or Path(command[0]).name != 'ffmpeg' or '-i' not in command or
            command[command.index('-i')+1] != str(raw) or str(loop) not in command):
            raise AudioError('capture fanout does not connect the expected video endpoints')
        return {'kit':kit,'alsa_card':card,'physical_video':str(raw),'hil_video':str(loop),
                'usb':audio_usb,'fanout_pid':pid}
    except (OSError,ValueError,IndexError,UnicodeError,subprocess.SubprocessError) as exc:
        raise AudioError('capture mapping unavailable or invalid') from exc


class AudioRecorder:
    def __init__(self, evidence, kit):
        if kit != 'A':
            raise AudioError('only Kit A audio capture is supported')
        self.evidence = Path(evidence)
        self.device = 'hw:CARD=S3,DEV=0'
        self.kit = kit
        self.mapping_path = self.evidence/'audio-device.json'
        self.path = self.evidence/'hdmi-audio.wav'
        self.progress_path = self.evidence/'audio-progress.jsonl'
        self.stderr_path = self.evidence/'audio.stderr'
        if any(p.exists() for p in (self.path,self.progress_path,self.stderr_path,self.mapping_path)):
            raise AudioError('audio evidence already exists')
        self.proc = None; self.ready = threading.Event(); self.first_progress_ns = None
        self.reader_ended = threading.Event(); self.reader_error = None; self.final_progress = False
        self.last_audio_us = 0; self.last_advance_ns = None
        self.reader = None; self.stop_request_ns = None
        self.stderr = self.progress = None
    def start(self):
        try:
            self.mapping=capture_identity(self.kit)
            self.mapping_path.write_text(json.dumps(self.mapping,indent=2)+'\n')
            self.stderr=self.stderr_path.open('x')
            self.progress=self.progress_path.open('x')
            self.start_min_ns=time.monotonic_ns()
            self.proc=subprocess.Popen(['ffmpeg','-nostdin','-hide_banner','-loglevel','warning',
                '-f','alsa','-ac','2','-ar','48000','-i',self.device,'-t','600',
                '-c:a','pcm_s16le','-progress','pipe:1','-stats_period','0.5',str(self.path)],
                stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=self.stderr,text=True,bufsize=1)
            self.start_max_ns=time.monotonic_ns()
            self.reader=threading.Thread(target=self._read_progress,daemon=True); self.reader.start()
            deadline=time.monotonic()+30
            while not self.ready.wait(0.1):
                if self.proc.poll() is not None: raise AudioError('audio capture exited before samples arrived')
                if self.reader_ended.is_set(): raise AudioError('audio progress reader stopped before samples arrived')
                if time.monotonic()>deadline: raise AudioError('audio capture did not start within 30 seconds')
            self.check()
        except BaseException as original:
            try:self._abort_start()
            except BaseException as cleanup:
                error = AudioError('audio capture startup cleanup could not be verified')
                error.audio_cleanup_incomplete = True
                raise error from cleanup
            raise

    def _read_progress(self):
        try:
            for line in self.proc.stdout:
                now=time.monotonic_ns(); self.progress.write(json.dumps({'monotonic_ns':now,'line':line.rstrip()})+'\n'); self.progress.flush()
                if line.rstrip() == 'progress=end': self.final_progress=True
                if line.startswith('out_time_us='):
                    try: position=int(line.split('=',1)[1])
                    except ValueError: position=0
                    positive=position>0
                    if position>self.last_audio_us:
                        self.last_audio_us=position; self.last_advance_ns=now
                    if positive and self.first_progress_ns is None:
                        self.first_progress_ns=now; self.ready.set()
        except BaseException as exc:
            self.reader_error=repr(exc)
        finally:
            self.reader_ended.set()
    def _abort_start(self):
        """Close every opened fd and reap only our partially started process."""
        failure=None
        try:
            if self.proc is not None and self.proc.poll() is None:
                try:self.proc.terminate()
                except ProcessLookupError:pass
                try:self.proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    try:self.proc.kill()
                    except ProcessLookupError:pass
                    self.proc.wait(timeout=5)
            if self.reader is not None and self.reader.ident is not None:
                self.reader.join(timeout=5)
                if self.reader.is_alive():raise AudioError('audio startup reader did not stop')
        except BaseException as exc:failure=exc
        finally:
            streams=(self.proc.stdout if self.proc is not None else None,self.progress,self.stderr)
            if self.reader is None or not self.reader.is_alive():
                for stream in streams:
                    if stream is not None:
                        try:stream.close()
                        except BaseException as exc:
                            if failure is None:failure=exc
        if failure is not None:raise AudioError('audio startup resources did not stop cleanly') from failure
    def check(self):
        if self.proc is None or self.proc.poll() is not None: raise AudioError('audio recorder stopped before diagnostic completed')
        if self.reader_error is not None or self.reader_ended.is_set():
            raise AudioError('audio progress reader failed or stopped early')
        if self.last_advance_ns is None or time.monotonic_ns()-self.last_advance_ns > MAX_PROGRESS_STALL_NS:
            raise AudioError('audio sample progress stalled')
        if capture_identity(self.kit) != self.mapping: raise AudioError('audio/video capture mapping changed')

    def wait_advanced_after(self, timestamp_ns, timeout=10):
        if type(timestamp_ns) is not int or timestamp_ns <= 0:
            raise AudioError('invalid audio coverage timestamp')
        deadline=time.monotonic()+timeout
        while True:
            self.check()
            if self.last_advance_ns is not None and self.last_advance_ns >= timestamp_ns:
                return
            if time.monotonic() >= deadline:
                raise AudioError('audio did not advance after the final observation window')
            time.sleep(.1)

    def stop(self, phases=None):
        if self.proc is None: raise AudioError('audio recorder was not started')
        self.stop_request_ns=time.monotonic_ns()
        running = self.proc.poll() is None
        error = None
        rc = None
        try:
            if running: self.proc.send_signal(signal.SIGINT)
            try:
                rc=self.proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                try:self.proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    error=AudioError('audio recorder cleanup unverified')
                    error.audio_cleanup_incomplete=True
                else:
                    error=AudioError('audio recorder did not stop in time')
        except BaseException as exc:
            error=exc
            error.audio_cleanup_incomplete=True
        if self.reader:
            self.reader.join(timeout=5)
            if self.reader.is_alive():
                error=error or AudioError('audio recorder reader did not stop')
                error.audio_cleanup_incomplete=True
        if not getattr(error, 'audio_cleanup_incomplete', False):
            if self.proc.stdout: self.proc.stdout.close()
            self.progress.close(); self.stderr.close()
        if error is not None: raise error
        if not running: raise AudioError('audio recorder stopped before diagnostic completed')
        if self.reader_error is not None or not self.reader_ended.is_set() or not self.final_progress:
            raise AudioError('audio progress reader did not complete cleanly')
        try:
            progress_rows=[json.loads(line) for line in self.progress_path.read_text().splitlines()]
            if (not progress_rows or any(not isinstance(row,dict) for row in progress_rows)
                    or progress_rows[-1].get('line') != 'progress=end'
                    or any(type(row.get('monotonic_ns')) is not int for row in progress_rows)
                    or any(a['monotonic_ns'] > b['monotonic_ns'] for a,b in zip(progress_rows,progress_rows[1:]))):
                raise ValueError('invalid progress trace')
        except (OSError, ValueError, TypeError, json.JSONDecodeError) as exc:
            raise AudioError('audio progress trace is incomplete') from exc
        advances=[]; previous_us=0
        for row in progress_rows:
            line=row.get('line','')
            if not isinstance(line,str) or not line.startswith('out_time_us='): continue
            try: value=int(line.split('=',1)[1])
            except ValueError: continue
            if previous_us and value<previous_us:
                raise AudioError('audio sample progress moved backwards')
            if value>previous_us:
                advances.append((row['monotonic_ns'],value)); previous_us=value
        if not advances or any(b[0]-a[0]>MAX_PROGRESS_STALL_NS for a,b in zip(advances,advances[1:])):
            raise AudioError('audio progress coverage is missing or stalled')
        if phases is not None:
            if not isinstance(phases,list) or len(phases)!=3:
                raise AudioError('three audio observation windows are required')
            previous_end=0
            for phase in phases:
                if not isinstance(phase,dict): raise AudioError('invalid audio observation window')
                begin,end=phase.get('observation_started_monotonic_ns'),phase.get('observation_finished_monotonic_ns')
                if (type(begin) is not int or type(end) is not int or begin<previous_end or begin>end
                        or not any(ns<=begin for ns,_ in advances) or not any(ns>=end for ns,_ in advances)):
                    raise AudioError('audio progress does not cover an observation window')
                previous_end=end
        if rc not in (0,255): raise AudioError('audio recorder failed, rc='+str(rc))
        try:
            with wave.open(str(self.path),'rb') as f:
                if (f.getnchannels(),f.getsampwidth(),f.getframerate(),f.getcomptype()) != (2,2,48000,'NONE') or f.getnframes() < 48000:
                    raise AudioError('audio capture format or duration invalid')
                frames=f.getnframes()
                remaining=frames
                while remaining:
                    amount=min(remaining, 48000)
                    data=f.readframes(amount)
                    if len(data) != amount*4:
                        raise AudioError('audio capture PCM data is truncated')
                    remaining-=amount
                if phases is not None and frames*1_000_000_000 < (previous_end-phases[0]['observation_started_monotonic_ns'])*48000:
                    raise AudioError('audio samples are shorter than the host observation window span')
        except (OSError,wave.Error) as exc: raise AudioError('audio capture not a complete WAV') from exc
        with self.path.open('rb') as stream: digest=hashlib.file_digest(stream,'sha256').hexdigest()
        with self.progress_path.open('rb') as stream: progress_sha=hashlib.file_digest(stream,'sha256').hexdigest()
        with self.mapping_path.open('rb') as stream: mapping_sha=hashlib.file_digest(stream,'sha256').hexdigest()
        result={'file':self.path.name,'sha256':digest,'frames':frames,'sample_rate':48000,'device':self.device,
                'spawn_before_monotonic_ns':self.start_min_ns,'spawn_after_monotonic_ns':self.start_max_ns,
                'first_progress_monotonic_ns':self.first_progress_ns,'stop_requested_monotonic_ns':self.stop_request_ns,
                'progress_file':self.progress_path.name,'progress_sha256':progress_sha,
                'mapping_file':self.mapping_path.name,'mapping_sha256':mapping_sha,
                'progress_coverage':{'advance_count':len(advances),'first_advance_ns':advances[0][0],
                                     'last_advance_ns':advances[-1][0], 'windows_checked':len(phases) if phases is not None else 0,
                                     'window_span_ns':(previous_end-phases[0]['observation_started_monotonic_ns']) if phases is not None else None,
                                     'max_host_progress_gap_ns':MAX_PROGRESS_STALL_NS},
                'clock_note':'spawn and first progress bracket host observation only; buffering and device clock remain uncalibrated'}
        (self.evidence/'audio-capture.json').write_text(json.dumps(result,indent=2)+'\n')
        return result
