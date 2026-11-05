#!/usr/bin/env python3
"""Execute the ROM RAM/stream RTL; no FPGA build, device or admission grant."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time
import uuid
import zlib

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT/'out'/('rom-stream-'+uuid.uuid4().hex))
    args = parser.parse_args()
    output = args.output.absolute()
    if output.exists() or not output.is_relative_to(ROOT/'out') or '..' in output.parts or any(p.is_symlink() for p in (output, *output.parents)):
        parser.error('a fresh private output beneath out/ is required')
    if shutil.disk_usage(ROOT).free < 30*(1 << 30):
        parser.error('30 GiB free space is required')
    output.mkdir(parents=True)
    fpga = ROOT/'sources/misteross'
    machine_sources = [fpga/p for p in ('cores/fes-zx81/rtl/zx81_machine.sv', 'cores/fes-zx81/rtl/zx81_machine_clock.v',
                       'cores/fes-zx81/rtl/zx81_dpram.v', 'cores/fes-common/rtl/z80/fes_z80_alu.sv',
                       'cores/fes-common/rtl/z80/fes_z80_bus.sv', 'cores/fes-common/rtl/z80/fes_z80_engine.sv',
                       'cores/fes-common/rtl/z80/fes_z80_nmos.sv')]
    firmware = ROOT/'platform/embedded/firmware'
    inputs = [HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv', HERE/'rom_probe.cpp', HERE/'rom_machine.sv', HERE/'rom_machine_probe.cpp', HERE/'rom_csr_probe.cpp',
              firmware/'nano_rom.c', firmware/'nano_rom.h', firmware/'generated/fes_embedded_rom.h', HERE/'generated/fes_embedded_rom.vh',
              ROOT/'sources/mister-packages/packages/fabric/fes_fabric_embedded_rom.yaml',
              Path(__file__).resolve(), *machine_sources, fpga/'cores/fes-zx81/rtl/zx81_model.vh']
    inputs += list((fpga/'cores/fes-common/rtl/z80').glob('*.svh'))
    secure_sources = [firmware/name for name in ('nano_program.c', 'nano_bridges.c', 'nano_bridge_sip.c', 'tests/nano_rom_platform_test.c')]
    inputs += secure_sources + [firmware/name for name in ('nano_program.h', 'nano_bridges.h', 'nano_bridge_sip.h',
        'nano_bridge_platform.h', 'nano_rom_platform.inc', 'nano_computer_control.inc', 'nano_configuration.h',
        'nano_identity.h', 'nano_cold_platform.c', 'tests/nano_rom_platform_test.h',
        'generated/fes_embedded_bridges.h')]
    # The secure fixture includes the real platform service header closure.
    inputs += sorted(firmware.glob('*.h')) + sorted((firmware/'generated').glob('*.h'))
    board_sources = [HERE/name for name in ('fes_de25_rom.sv', 'fes_de25_computer.v', 'rom_board_machine.sv',
        'rom_board_probe.cpp', 'fes_de25_rom_hw.tcl', 'rom_transport.py', 'generated/fes_de25_computer.vh')]
    board_sources += [fpga/name for name in ('cores/fes-common/rtl/fes_computer_mailbox.v',
        'cores/fes-common/generated/fes_computer.vh', 'cores/fes-zx81/rtl/zx81_keyboard.v',
        'cores/fes-zx81/rtl/zx81_tape_store.v', 'cores/fes-zx81/rtl/zx81_bus_pack.vh',
        'cores/fes-zx81/rtl/zx8x.hex')]
    inputs += board_sources
    video_path = 'sources/misteross/boards/de25nano/zx81-hps/video.v'
    video_revision = '79d616e85e19d65f0d645b67a85ffe7e142dcdfc'
    video = subprocess.check_output(['git','-C',str(ROOT),'show',video_revision+':'+video_path])
    (output/'video.v').write_bytes(video)
    (output/'zx8x.hex').write_bytes((fpga/'cores/fes-zx81/rtl/zx8x.hex').read_bytes())
    sources = {p: hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs}
    commands = []

    def run(name, argv, cwd=ROOT):
        started = time.monotonic()
        with (output/(name+'.log')).open('xb') as log:
            child = subprocess.Popen(list(map(str, argv)), cwd=cwd, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            beat = started+10
            try:
                while child.poll() is None:
                    now = time.monotonic()
                    if now >= started+90:
                        raise TimeoutError(name+' exceeded 90s')
                    if now >= beat:
                        print(name+': running', flush=True)
                        beat = now+10
                    time.sleep(.1)
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
            commands.append(dict(name=name, argv=list(map(str, argv)), returncode=child.returncode,
                                 elapsed_seconds=time.monotonic()-started,
                                 log_sha256=hashlib.sha256((output/(name+'.log')).read_bytes()).hexdigest()))
            if child.returncode:
                raise RuntimeError(name+' failed; see '+str(output/(name+'.log')))

    run('firmware-build', ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-ffreestanding', '-fno-builtin',
                          '-c', firmware/'nano_rom.c', '-o', output/'nano_rom.o'])
    objects = [output/'nano_rom.o']
    for source in secure_sources:
        obj = output/(source.stem+'.o')
        run(source.stem+'-build', ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
            '-I'+str(firmware), '-I'+str(firmware/'tests'), '-c', source, '-o', obj])
        objects.append(obj)
    run('csr-lint', ['verilator', '--lint-only', '-Wall', '--top-module', 'fes_embedded_rom_csr',
        '-I'+str(HERE/'generated'), HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv'])
    run('avalon-lint', ['verilator', '--lint-only', '-Wall', '--top-module', 'fes_de25_rom',
        '-I'+str(HERE/'generated'), HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv', HERE/'fes_de25_rom.sv'])
    for size in (4096, 8192):
        target = output/str(size)
        run('build-'+str(size), ['verilator', '-Wall', '--cc', '--exe', '--build', '-j', '2', '--Mdir', target,
                                '--top-module', 'fes_embedded_rom', '-GROM_BYTES='+str(size),
                                HERE/'fes_embedded_rom.sv', HERE/'rom_probe.cpp'])
        data = bytes((i*37+(i >> 3)+0x5a) & 255 for i in range(size))
        run('execute-'+str(size), [target/'Vfes_embedded_rom', str(size), format(zlib.crc32(data), 'x')])
        # Match the existing real-machine simulator's source waivers. The new
        # ROM module above is compiled separately with all warnings fatal.
        waivers = ['UNUSEDSIGNAL', 'UNOPTFLAT', 'CASEINCOMPLETE', 'WIDTHTRUNC', 'WIDTHEXPAND', 'SYNCASYNCNET',
                   'PINCONNECTEMPTY', 'DECLFILENAME', 'IMPLICITSTATIC', 'VARHIDDEN', 'UNUSEDPARAM', 'CASEX', 'BLKSEQ']
        machine = output/('machine-'+str(size))
        run('machine-build-'+str(size), ['verilator', '-Wall', *('-Wno-'+v for v in waivers),
            '--cc', '--exe', '--build', '-j', '2', '--Mdir', machine, '--top-module', 'rom_machine',
            '-GROM_BYTES='+str(size), *(['-DZX81_MODEL_ZX80'] if size == 4096 else []),
            '-I'+str(fpga/'cores/fes-zx81/rtl'), '-I'+str(fpga/'cores/fes-common/rtl/z80'),
            '-I'+str(HERE/'generated'), HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv', HERE/'rom_machine.sv', *machine_sources, HERE/'rom_machine_probe.cpp'])
        program = bytes.fromhex('3e5a32004076') + bytes(size-6)
        run('machine-execute-'+str(size), [machine/'Vrom_machine', str(size), format(zlib.crc32(program), 'x')], cwd=fpga)
        csr = output/('csr-'+str(size))
        run('csr-build-'+str(size), ['verilator', '-Wall', *('-Wno-'+v for v in waivers),
            '--cc', '--exe', '--build', '-j', '2', '--Mdir', csr, '--top-module', 'rom_machine',
            '-GROM_BYTES='+str(size), '-GCSR_MODE=1', "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
            *(['-DZX81_MODEL_ZX80'] if size == 4096 else []),
            '-I'+str(fpga/'cores/fes-zx81/rtl'), '-I'+str(fpga/'cores/fes-common/rtl/z80'), '-I'+str(HERE/'generated'),
            '-CFLAGS', '-std=c++17 -I'+str(firmware), '-LDFLAGS', ' '.join(map(str,objects)),
            HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv', HERE/'rom_machine.sv', *machine_sources, HERE/'rom_csr_probe.cpp'])
        run('csr-execute-'+str(size), [csr/'Vrom_machine', str(size), format(zlib.crc32(program), 'x')], cwd=fpga)
        board = output/('board-'+str(size))
        run('board-build-'+str(size), ['verilator', '-Wall', *('-Wno-'+v for v in waivers),
            '--cc', '--exe', '--build', '-j', '2', '--Mdir', board, '--top-module', 'rom_board_machine',
            '-GROM_BYTES='+str(size), '-DFES_DE25_EXTERNAL_ROM=1',
            *(['-DZX81_MODEL_ZX80'] if size == 4096 else []),
            '-I'+str(HERE), '-I'+str(HERE/'generated'), '-I'+str(fpga/'cores/fes-common/generated'),
            '-I'+str(fpga/'cores/fes-zx81/rtl'), '-I'+str(fpga/'cores/fes-common/rtl/z80'),
            '-CFLAGS', '-std=c++17 -I'+str(firmware), '-LDFLAGS', ' '.join(map(str,objects)),
            HERE/'fes_embedded_rom.sv', HERE/'fes_embedded_rom_csr.sv', HERE/'fes_de25_rom.sv',
            HERE/'fes_de25_computer.v', HERE/'rom_board_machine.sv', *machine_sources,
            fpga/'cores/fes-common/rtl/fes_computer_mailbox.v', fpga/'cores/fes-zx81/rtl/zx81_keyboard.v',
            fpga/'cores/fes-zx81/rtl/zx81_tape_store.v', output/'video.v', HERE/'rom_board_probe.cpp'])
        run('board-execute-'+str(size), [board/'Vrom_board_machine', str(size), format(zlib.crc32(program), 'x')], cwd=output)
    assert all(hashlib.sha256(p.read_bytes()).hexdigest() == digest for p, digest in sources.items())
    result = dict(status='passed', classification='actual C ROM transport, opt-in secure-provider/control gate and real ZX80/ZX81 RTL; injected platform and exact-image digest oracle, no hardware or native admission proof',
                  source_sha256={str(p.relative_to(ROOT)): digest for p, digest in sources.items()}, commands=commands,
                  independent_crc_oracle='Python zlib.crc32', hardware_actions=False, ordinary_admission=False)
    result['board_rom_wiring_model'] = True
    result['captured_video'] = dict(revision=video_revision,path=video_path,sha256=hashlib.sha256(video).hexdigest())
    (output/'result.json').write_text(json.dumps(result, indent=2)+'\n')
    print('PASS ROM stream RTL:', output, flush=True)


if __name__ == '__main__':
    main()
