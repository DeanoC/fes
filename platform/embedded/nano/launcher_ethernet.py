"""Compile-time requirements for the matched PIO launcher MAC client."""
import hashlib
import json
import re
import shlex

ADAPTER = 'fes-de25-mac0-sid1-paired-menu-v1'
BASE = 0x95c00000
CPU_MAP_BYTES = 2 << 20
DMA_BYTES = 5 * 4096


def contract():
    return dict(adapter=ADAPTER, mac_base=0x10810000, mac_version=0x7631,
                phy_address=1, phy_id=0x00221622, csr_clock_hz=100000000,
                rx_buffer_shift=1, stream_id=1, dma_start=BASE, dma_bytes=DMA_BYTES,
                dma_pages=['RW', 'R', 'R', 'W', 'W'], cpu_map_bytes=CPU_MAP_BYTES,
                station='sha256-fes-de25-target-v1', addressing='dhcp4',
                service='fes-embedded-mac', initial_state='contained', network_scope='published-menu-only')


def board_network():
    # arm64map maps a whole EL1 block. The MAC client allocates just five pages.
    return dict(mac_index=0, phy_address=1, phy_id=0x00221622, csr_clock_hz=100000000,
                rx_buffer_shift=1, dma_start=BASE, dma_size=CPU_MAP_BYTES,
                address='', gateway='', mac_address='')


def selection(args, profile_bytes, pio_profile):
    if not getattr(args, 'paired_ethernet', False):
        if getattr(args, 'c64_network_scope', False):
            raise ValueError('--c64-network-scope requires --paired-ethernet')
        return None
    if (profile_bytes != pio_profile or args.program != 'fpgaload' or
            not all(getattr(args, k, False) for k in
                    ('selected_programs', 'tenfoot_serial', 'c64_disk_swap', 'shared_core_shell', 'network_dhcp')) or
            any(getattr(args, k, False) for k in
                ('sd_adma2', 'tenfoot_uart_only', 'network_allow_100m', 'network_recover_link_flap'))):
        raise ValueError('--paired-ethernet requires the matched DHCP, gigabit, C64 picker and PIO launcher')
    return contract()


def c64_marker(identity):
    keys = {'package_id', 'build_id', 'core_size', 'core_sha256', 'core_sha384', 'chunk_sha384', 'handoff_sha256'}
    if not isinstance(identity, dict) or set(identity) != keys:
        raise ValueError('incomplete C64 scope identity')
    size = identity['core_size']

    def digest(value, width):
        return isinstance(value, str) and re.fullmatch('[0-9a-f]{%d}' % width, value) and value != '0' * width

    chunk_bytes = 0xfffc0
    if (type(size) is not int or not 0 < size <= (8 << 20) - 4096 or size % 4 or
            not isinstance(identity['chunk_sha384'], list) or
            len(identity['chunk_sha384']) != (size + chunk_bytes - 1) // chunk_bytes or
            any(not digest(value, 96) for value in identity['chunk_sha384']) or
            any(not digest(identity[name], width) for name, width in (
                ('package_id', 64), ('build_id', 32), ('core_sha256', 64),
                ('core_sha384', 96), ('handoff_sha256', 64)))):
        raise ValueError('invalid C64 scope fit identity')
    canonical = (json.dumps(identity, sort_keys=True, separators=(',', ':')) + '\n').encode()
    return 'fes-c64-gp-v1/' + identity['package_id'] + '/' + hashlib.sha256(canonical).hexdigest()



def checked(receipt, elf, *, entry=False):
    selected = receipt.get('launcher_ethernet')
    linked = ADAPTER.encode() in elf
    markers = re.findall(rb'fes-c64-gp-v1/[0-9a-f]{64}/[0-9a-f]{64}', elf)
    if selected is None and not linked and not markers:
        return None
    # JSON comparison also keeps boolean values distinct from integers.
    expected = contract()
    if isinstance(selected, dict) and 'c64_scope' in selected:
        record = selected['c64_scope']
        if set(markers) != {c64_marker(record).encode()}:
            raise ValueError('compiled C64 live scope differs from the native ELF')
        expected['c64_scope'] = record
        expected['network_scope'] = 'published-menu-and-matched-c64'
    elif markers:
        raise ValueError('C64 scope present only in native ELF')
    if not linked or json.dumps(selected, sort_keys=True) != json.dumps(expected, sort_keys=True):
        raise ValueError('paired Ethernet adapter contract differs from the native ELF')
    if (receipt.get('native_program' if entry else 'program') != 'fpgaload' or
            receipt.get('sd_adma2', False) is not False or
            any(receipt.get(k) is not True for k in ('shared_core_shell', 'selected_programs', 'tenfoot_serial'))):
        raise ValueError('paired Ethernet requires the retained PIO launcher')
    if entry:
        if [BASE, BASE + CPU_MAP_BYTES] not in receipt.get('staging_exclusions', []):
            raise ValueError('paired Ethernet CPU block is missing from entry staging exclusions')
    else:
        command = receipt['command']
        flags = shlex.split(command[command.index('-ldflags') + 1])
        if (receipt.get('network_dhcp') is not True or receipt.get('c64_disk_swap') is not True or
                not any(flags[i:i+2] == ['-X', 'main.expectedNetworkDHCP=1'] for i in range(len(flags)))):
            raise ValueError('paired Ethernet requires the linked DHCP and C64 picker selections')
    return selected
