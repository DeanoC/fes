"""Bound pc386-owned subprocess trees and report their progress."""
import os
import shutil
import signal
import subprocess
import sys
import threading
import time


def run(command, *, cwd, env=None, timeout, label, heartbeat=30.0, grace=5.0,
        stdout=None, status_stream=None):
    status_stream = sys.stdout if status_stream is None else status_stream
    if timeout <= 0 or heartbeat <= 0 or grace <= 0:
        raise ValueError('positive job cap, heartbeat and cleanup grace are required')
    if threading.current_thread() is not threading.main_thread():
        raise RuntimeError('pc386 jobs require main-thread signal ownership')
    disk_floor = 30 * 1024**3
    if shutil.disk_usage(cwd).free < disk_floor:
        print(f'{label}: failed: disk below 30 GiB before launch', file=status_stream, flush=True)
        raise OSError('disk below 30 GiB before launch')
    child = None
    reaped = False
    pending_signal = None
    previous = {}
    started = time.monotonic()

    def leader_exited():
        # WNOWAIT deliberately leaves the leader unreaped. Its PID remains
        # reserved until the whole owned process group has been killed, so the
        # group ID cannot be recycled into an unrelated job during cleanup.
        return os.waitid(os.P_PID, child.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is not None

    def signal_group(signum):
        try:
            os.killpg(child.pid, signum)
        except ProcessLookupError:
            pass

    def stop_child(*, graceful):
        nonlocal reaped
        if child is None or reaped:
            return
        if graceful and not leader_exited():
            signal_group(signal.SIGTERM)
            # A leader may exit before a TERM-ignoring descendant. Keep its
            # PID reserved through the whole grace, then kill the exact group.
            deadline = time.monotonic() + grace
            while time.monotonic() < deadline:
                time.sleep(min(.1, max(0, deadline - time.monotonic())))
        signal_group(signal.SIGKILL)
        child.wait()
        reaped = True

    def interrupted(signum, _frame):
        nonlocal pending_signal
        # A signal can arrive inside Popen before its handle is assigned.
        if child is None:
            pending_signal = signum
            return
        stop_child(graceful=True)
        raise SystemExit(128 + signum)

    try:
        # An inherited ignored SIGCHLD can cause the kernel to reap a child
        # immediately, invalidating the reserved-PID cleanup guarantee.
        previous[signal.SIGCHLD] = signal.getsignal(signal.SIGCHLD)
        signal.signal(signal.SIGCHLD, signal.SIG_DFL)
        for signum in (signal.SIGTERM, signal.SIGINT):
            previous[signum] = signal.getsignal(signum)
            signal.signal(signum, interrupted)
        child = subprocess.Popen(command, cwd=cwd, env=env, stdout=stdout, start_new_session=True)
        if pending_signal is not None:
            interrupted(pending_signal, None)
        print(f'{label}: started pid={child.pid}, cap={timeout:g}s', file=status_stream, flush=True)
        next_heartbeat = time.monotonic() + heartbeat
        while True:
            if leader_exited():
                # Even on success an orphaned descendant cannot outlive the job.
                signal_group(signal.SIGKILL)
                status = child.wait()
                reaped = True
                print(f'{label}: {"finished" if status == 0 else "failed"} exit={status}', file=status_stream, flush=True)
                return status
            now = time.monotonic()
            remaining = timeout - (now - started)
            if shutil.disk_usage(cwd).free < disk_floor:
                stop_child(graceful=True)
                print(f'{label}: failed: disk below 30 GiB', file=status_stream, flush=True)
                raise OSError('disk below 30 GiB during job')
            if remaining <= 0:
                stop_child(graceful=True)
                print(f'{label}: failed: time cap {timeout:g}s exceeded', file=status_stream, flush=True)
                raise TimeoutError(f'{label} exceeded {timeout:g}s')
            if now >= next_heartbeat:
                print(f'{label}: heartbeat elapsed={now - started:.0f}s', file=status_stream, flush=True)
                next_heartbeat = now + heartbeat
            time.sleep(min(.2, remaining, max(.001, next_heartbeat - now)))
    except TimeoutError:
        raise  # The timeout was already reported above.
    except BaseException as exc:
        print(f'{label}: failed: {type(exc).__name__}', file=status_stream, flush=True)
        raise
    finally:
        stop_child(graceful=True)
        for signum, handler in previous.items():
            signal.signal(signum, handler)
