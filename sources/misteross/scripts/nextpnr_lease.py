"""Run one GPU-routed nextpnr-mistral invocation inside its gpu-run lease.

fes_build_common._run_command runs this file under gpu-run as
``python -I nextpnr_lease.py {lease|phase} <timeout|-> nextpnr-mistral ...``.
It imports only the standard library, and isolated mode keeps the source tree
off sys.path. An audited route therefore reads only this file from the source
tree, and every audited source closure covers it (fes#881).
"""
from __future__ import annotations

import os
from pathlib import Path
import select
import signal
import socket
import subprocess
import sys
import time
from typing import Sequence

GPU_LEASE_CONFIG_ENV = (
    "GPU_LEASE_DIR",
    "GPU_LEASE_IDS",
    "GPU_LEASE_VENDOR",
    "GPU_LEASE_MAX_PER_OWNER",
    "FES_GPU_LEASE_OWNER",
)
GPU_PHASE_LEASE_CONFIG_ENV = (*GPU_LEASE_CONFIG_ENV, "GPU_LEASE_MAX_JOBS_PER_OWNER")


def _exec_leased_nextpnr(command: Sequence[str], timeout: float | None = None) -> int:
    """Run nextpnr with the logical device and mask granted by gpu-run."""
    command = list(command)
    if not command or Path(command[0]).name != "nextpnr-mistral":
        print("FES GPU lease runner requires nextpnr-mistral", file=sys.stderr)
        return 2
    device = os.environ.get("GPU_RUN_DEVICE", "")
    if not device.isdigit():
        print("FES GPU lease did not provide a logical GPU_RUN_DEVICE", file=sys.stderr)
        return 2
    if not os.environ.get("FES_GPU_IDS") or os.environ.get("NEXTPNR_REQUIRE_GPU") != "1":
        print("FES GPU lease did not provide a strict leased-device environment", file=sys.stderr)
        return 2
    if not any(os.environ.get(name) for name in ("ROCR_VISIBLE_DEVICES", "CUDA_VISIBLE_DEVICES")):
        print("FES GPU lease did not provide a GPU visibility mask", file=sys.stderr)
        return 2

    # Producer recipes describe route options before a lease is acquired. Drop
    # any legacy/default index here and use the logical index supplied by the
    # one-device lease instead.
    routed = [command[0]]
    index = 1
    while index < len(command):
        argument = command[index]
        if argument == "--gpu-device":
            if index + 1 >= len(command):
                print("nextpnr --gpu-device is missing its value", file=sys.stderr)
                return 2
            index += 2
            continue
        if argument.startswith("--gpu-device="):
            index += 1
            continue
        if argument == "--gpu-devices" or argument.startswith("--gpu-devices="):
            print("one nextpnr invocation may use only its single leased GPU", file=sys.stderr)
            return 2
        routed.append(argument)
        index += 1
    routed.extend(("--gpu-device", device))

    tool_env = os.environ.copy()
    for name in GPU_LEASE_CONFIG_ENV:
        tool_env.pop(name, None)
    try:
        result = subprocess.run(routed, env=tool_env, timeout=timeout, check=False)
    except subprocess.TimeoutExpired:
        print(f"nextpnr route timed out after {timeout:g} seconds (GPU lease already granted)", file=sys.stderr)
        return 124
    except OSError as exc:
        print(f"cannot execute leased nextpnr: {exc}", file=sys.stderr)
        return 127
    return result.returncode if result.returncode >= 0 else 128 - result.returncode


class _LeaseRelay:
    """Relay GPU_LEASE_FD lines between nextpnr and gpu-run, timing the waits.

    The protocol has one request in flight at a time. Time spent waiting for
    gpu-run's reply is lease wait, which the route budget excludes.
    """

    def __init__(self, broker: socket.socket, client: socket.socket):
        self.broker = broker
        self.client = client
        self.in_flight = 0
        self.wait_started: float | None = None
        self.waited = 0.0

    def own_seconds(self, started: float) -> float:
        now = time.monotonic()
        waiting = 0.0 if self.wait_started is None else now - self.wait_started
        return now - started - self.waited - waiting

    def _settle(self) -> None:
        if self.in_flight == 0 and self.wait_started is not None:
            self.waited += time.monotonic() - self.wait_started
            self.wait_started = None

    def close(self) -> None:
        # Closing gpu-run's descriptor releases any grant the route still holds.
        for side in (self.client, self.broker):
            if side is not None:
                side.close()
        self.client = self.broker = None
        self.in_flight = 0
        self._settle()

    def pump(self, timeout: float) -> None:
        sides = [side for side in (self.client, self.broker) if side is not None]
        if not sides:
            time.sleep(timeout)
            return
        try:
            readable, _, _ = select.select(sides, [], [], timeout)
        except InterruptedError:
            return
        for side in readable:
            if side is not self.client and side is not self.broker:
                continue
            try:
                data = side.recv(4096)
            except InterruptedError:
                continue
            except OSError:
                data = b""
            if not data:
                # Either end closing ends the relay: nextpnr exited or gave up
                # its descriptor, or gpu-run is gone and nextpnr must see EOF.
                self.close()
                return
            target = self.broker if side is self.client else self.client
            try:
                target.sendall(data)
            except OSError:
                self.close()
                return
            lines = data.count(b"\n")
            if side is self.client:
                if lines and self.in_flight == 0:
                    self.wait_started = time.monotonic()
                self.in_flight += lines
            else:
                self.in_flight = max(0, self.in_flight - lines)
                self._settle()


def _exec_phase_leased_nextpnr(command: Sequence[str], timeout: float | None = None) -> int:
    """Run nextpnr under gpu-run --phase-lease, timing only its own work.

    nextpnr starts without a GPU and asks gpu-run for one on GPU_LEASE_FD for
    each GPU phase. This relays those requests and pauses the route budget
    while one waits, so a queued route passes or fails exactly as an
    unqueued one does. Without a grant there is no mask: nextpnr selects the
    granted physical GPU itself, so --gpu-device stays the physical id.
    """
    command = list(command)
    if not command or Path(command[0]).name != "nextpnr-mistral":
        print("FES GPU lease runner requires nextpnr-mistral", file=sys.stderr)
        return 2
    raw_fd = os.environ.get("GPU_LEASE_FD", "")
    if not raw_fd.isdigit() or os.environ.get("NEXTPNR_REQUIRE_GPU") != "1":
        print("FES GPU phase lease did not provide GPU_LEASE_FD and strict GPU routing", file=sys.stderr)
        return 2
    if any(argument == "--gpu-devices" or argument.startswith("--gpu-devices=")
           for argument in command[1:]):
        print("one nextpnr invocation may use only its single leased GPU", file=sys.stderr)
        return 2
    try:
        broker = socket.socket(fileno=int(raw_fd))
    except OSError as exc:
        print(f"FES GPU phase lease descriptor is unusable: {exc}", file=sys.stderr)
        return 2
    client, theirs = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    relay = _LeaseRelay(broker, client)
    tool_env = os.environ.copy()
    for name in GPU_PHASE_LEASE_CONFIG_ENV:
        tool_env.pop(name, None)
    tool_env["GPU_LEASE_FD"] = str(theirs.fileno())
    try:
        # Its own process group, so expiry stops whatever nextpnr started too:
        # with no outer deadline, audited_run no longer cancels the route group.
        process = subprocess.Popen(command, env=tool_env, pass_fds=(theirs.fileno(),),
                                   start_new_session=True)
    except OSError as exc:
        relay.close()
        print(f"cannot execute leased nextpnr: {exc}", file=sys.stderr)
        return 127
    finally:
        theirs.close()
    started = time.monotonic()
    try:
        while True:
            if timeout is not None and relay.own_seconds(started) >= timeout:
                _stop_route_group(process)
                print(f"nextpnr route timed out after {timeout:g} seconds of its own run time "
                      f"({relay.waited:.1f} s of GPU lease wait excluded)", file=sys.stderr)
                return 124
            relay.pump(0.05)
            returncode = process.poll()
            if returncode is not None:
                break
    except BaseException:
        _stop_route_group(process)
        raise
    finally:
        relay.close()
    return returncode if returncode >= 0 else 128 - returncode


def _stop_route_group(process: subprocess.Popen) -> None:
    """Kill the route's own process group, then reap nextpnr.

    Until nextpnr is reaped its pid, which is the group id, cannot be reused.
    """
    if process.returncode is None:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    process.wait()


def main(argv: Sequence[str]) -> int:
    if len(argv) < 2 or argv[0] not in ("lease", "phase"):
        print("usage: nextpnr_lease.py {lease|phase} <timeout|-> nextpnr-mistral ...", file=sys.stderr)
        return 2
    run = _exec_phase_leased_nextpnr if argv[0] == "phase" else _exec_leased_nextpnr
    return run(argv[2:], None if argv[1] == "-" else float(argv[1]))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
