"""Explicit, source-pinned recovery return observations; no device operations.

This layer applies after the existing PHY preparation. Removing its insertions
must reproduce those exact inputs, including every original stop and deadline.
Markers mean a call boundary returned, never that DMA or a PHY is drained.
"""
import hashlib
from pathlib import Path

UART_DATA = 0x10c02100
UART_STATUS = UART_DATA + 0x14
PINNED = {
    'net/net.c': 'f8056812c01bb023db647ce64abbd26777bac7764d4549c86d0f0de0e24b1c32',
    'cmd/net.c': '692965784e07e1e6bb778a69bdbeba7501a4f6a5c0a2e223bb3af680ba14d8ea',
    'drivers/net/dwc_eth_xgmac.c': 'b218457648a285165c88873eba16cc770ba1f16055b9aed1355224c7a1ecbe47',
    'net/Makefile': '194e96537ba13c38c4485c640202ddd3ca50c4ecf2e663107120c75441e935bc',
}
HEADER_PATH = 'include/fes_recovery_return_trace.h'
HELPER_PATH = 'net/fes_recovery_return_trace.c'
HEADER = '''/* SPDX-License-Identifier: GPL-2.0+ */
void fes_recovery_return_begin(void);
void fes_recovery_return_mark(const char *phase);
void fes_recovery_return_end(void);
'''
HELPER = '''// SPDX-License-Identifier: GPL-2.0+
/* Recovery-only bounded UART observations. No timer, heap, network or SD call. */
#include <linux/types.h>
#include <asm/io.h>
#include <fes_recovery_return_trace.h>

#ifndef FES_RETURN_PARK
#define FES_RETURN_PARK() do { for (;;) __asm__ __volatile__("wfe"); } while (0)
#endif
static unsigned int fes_return_active;

static void fes_return_wait(unsigned int mask)
{
	unsigned int poll;
	for (poll = 0; poll < 100000; ++poll)
		if (readl((void *)@UART_STATUS@UL) & mask)
			return;
	FES_RETURN_PARK();
}

static void fes_return_text(const char *text)
{
	unsigned int i;
	for (i = 0; i < 96; ++i) {
		if (!text[i])
			return;
		fes_return_wait(1U << 5);
		writel((unsigned char)text[i], (void *)@UART_DATA@UL);
	}
	FES_RETURN_PARK();
}

void fes_recovery_return_mark(const char *phase)
{
	if (!fes_return_active)
		return;
	fes_return_text("FES_RECOVERY_RETURN phase=");
	fes_return_text(phase);
	fes_return_text("\\r\\n");
	fes_return_wait(1U << 6);
}

void fes_recovery_return_begin(void)
{
	if (fes_return_active)
		FES_RETURN_PARK();
	fes_return_active = 1;
	fes_recovery_return_mark("begin");
}

void fes_recovery_return_end(void)
{
	fes_recovery_return_mark("command-return");
	fes_return_active = 0;
}
'''.replace('@UART_STATUS@', hex(UART_STATUS)).replace('@UART_DATA@', hex(UART_DATA))
PHASES = (
    'begin', 'count-before', 'count-after', 'filesize-before', 'filesize-after',
    'fileaddr-before', 'fileaddr-after', 'eth-halt-before', 'stop-enter',
    'tx-disable-before', 'tx-disable-after', 'tx-drain-before', 'tx-drain-after',
    'mac-disable-before', 'mac-disable-after', 'rx-drain-before', 'rx-drain-after',
    'rx-disable-before', 'rx-disable-after', 'phy-stop-before', 'phy-stop-after',
    'resets-stop-before', 'resets-stop-after', 'stop-return', 'eth-halt-after',
    'protocol-before', 'protocol-after', 'net-loop-return', 'net-loop-after',
    'env-update-before', 'env-update-after', 'autostart-before', 'autostart-after',
    'command-return',
)


def _around(text, indent, phase):
    return (indent + 'fes_recovery_return_mark("' + phase + '-before");\n' +
            text + '\n' + indent + 'fes_recovery_return_mark("' + phase + '-after");')


INCLUDE = ('#include <net.h>\n', '#include <net.h>\n#include <fes_recovery_return_trace.h>\n')
NET_PATCHES = [
    INCLUDE,
    ('case NETLOOP_SUCCESS:\n\t\t\tnet_cleanup_loop();',
     'case NETLOOP_SUCCESS:\n\t\t\tnet_cleanup_loop();\n'
     '\t\t\tif (protocol == TFTPGET && net_boot_file_size > 0)\n'
     '\t\t\t\tfes_recovery_return_begin();'),
    ('\t\t\t\tprintf("Bytes transferred = %u (%x hex)\\n",\n'
     '\t\t\t\t       net_boot_file_size, net_boot_file_size);',
     _around('\t\t\t\tprintf("Bytes transferred = %u (%x hex)\\n",\n'
             '\t\t\t\t       net_boot_file_size, net_boot_file_size);', '\t\t\t\t', 'count')),
    ('\t\t\t\tenv_set_hex("filesize", net_boot_file_size);',
     _around('\t\t\t\tenv_set_hex("filesize", net_boot_file_size);', '\t\t\t\t', 'filesize')),
    ('\t\t\t\tenv_set_hex("fileaddr", image_load_addr);',
     _around('\t\t\t\tenv_set_hex("fileaddr", image_load_addr);', '\t\t\t\t', 'fileaddr')),
    ('\t\t\tif (protocol != NETCONS && protocol != NCSI)\n\t\t\t\teth_halt();\n\t\t\telse',
     '\t\t\tif (protocol != NETCONS && protocol != NCSI) {\n' +
     _around('\t\t\t\teth_halt();', '\t\t\t\t', 'eth-halt') + '\n\t\t\t} else'),
    ('\t\t\teth_set_last_protocol(protocol);',
     _around('\t\t\teth_set_last_protocol(protocol);', '\t\t\t', 'protocol')),
    ('\t\tpcap_print_status();\n#endif\n\treturn ret;',
     '\t\tpcap_print_status();\n#endif\n\tfes_recovery_return_mark("net-loop-return");\n\treturn ret;'),
]
CMD_PATCHES = [
    INCLUDE,
    ('\tsize = net_loop(proto);', '\tsize = net_loop(proto);\n\tfes_recovery_return_mark("net-loop-after");'),
    ('\tnetboot_update_env();', _around('\tnetboot_update_env();', '\t', 'env-update')),
    ('\trcode = bootm_maybe_autostart(cmdtp, argv[0]);',
     _around('\trcode = bootm_maybe_autostart(cmdtp, argv[0]);', '\t', 'autostart')),
    ('\treturn rcode;', '\tfes_recovery_return_end();\n\treturn rcode;'),
]
STOP_PATCHES = [
    ('\tif (!xgmac->started)\n\t\treturn;', '\tfes_recovery_return_mark("stop-enter");\n\tif (!xgmac->started)\n\t\treturn;'),
    ('\tclrbits_le32(&xgmac->dma_regs->ch0_tx_control,\n\t\t     XGMAC_DMA_CH0_TX_CONTROL_ST);',
     _around('\tclrbits_le32(&xgmac->dma_regs->ch0_tx_control,\n\t\t     XGMAC_DMA_CH0_TX_CONTROL_ST);', '\t', 'tx-disable')),
    ('\t/* Wait for TX all packets to drain out of MTL */', '\tfes_recovery_return_mark("tx-drain-before");\n\t/* Wait for TX all packets to drain out of MTL */'),
    ('\t/* Turn off MAC TX and RX */', '\tfes_recovery_return_mark("tx-drain-after");\n\tfes_recovery_return_mark("mac-disable-before");\n\t/* Turn off MAC TX and RX */'),
    ('\t/* Wait for all RX packets to drain out of MTL */', '\tfes_recovery_return_mark("mac-disable-after");\n\tfes_recovery_return_mark("rx-drain-before");\n\t/* Wait for all RX packets to drain out of MTL */'),
    ('\t/* Turn off RX DMA */', '\tfes_recovery_return_mark("rx-drain-after");\n\tfes_recovery_return_mark("rx-disable-before");\n\t/* Turn off RX DMA */'),
    ('\tif (xgmac->phy)\n\t\tphy_shutdown(xgmac->phy);',
     '\tfes_recovery_return_mark("rx-disable-after");\n\tif (xgmac->phy) {\n' +
     _around('\t\tphy_shutdown(xgmac->phy);', '\t\t', 'phy-stop') + '\n\t}'),
    ('\txgmac->config->ops->xgmac_stop_resets(dev);',
     _around('\txgmac->config->ops->xgmac_stop_resets(dev);', '\t', 'resets-stop') + '\n\tfes_recovery_return_mark("stop-return");'),
]
SUFFIX = '\nobj-y += fes_recovery_return_trace.o\n'


def _apply(text, patches, inverse=False):
    for before, after in reversed(patches) if inverse else patches:
        old, new = (after, before) if inverse else (before, after)
        if text.count(old) != 1:
            raise ValueError('recovery return anchor differs: ' + old[:70])
        text = text.replace(old, new, 1)
    return text


def _driver(text, inverse=False):
    text = _apply(text, [INCLUDE], inverse)
    start = text.index('static void xgmac_stop(struct udevice *dev)')
    end = text.index('\nstatic int xgmac_send(', start)
    return text[:start] + _apply(text[start:end], STOP_PATCHES, inverse) + text[end:]


def _read(source, names):
    values = {}
    for name in names:
        p = Path(source) / name
        if p.is_symlink() or not p.is_file() or not 0 < p.stat().st_size <= 1 << 20:
            raise ValueError('recovery return input is not a bounded regular file: ' + name)
        values[name] = p.read_text()
    return values


def prepare(source):
    source = Path(source)
    original = _read(source, PINNED)
    if any(hashlib.sha256(text.encode()).hexdigest() != PINNED[name] for name, text in original.items()):
        raise ValueError('recovery return source differs from its pinned input')
    if any((source / name).exists() or (source / name).is_symlink() for name in (HEADER_PATH, HELPER_PATH)):
        raise ValueError('recovery return helper already exists')
    changed = {'net/net.c': _apply(original['net/net.c'], NET_PATCHES),
               'cmd/net.c': _apply(original['cmd/net.c'], CMD_PATCHES),
               'drivers/net/dwc_eth_xgmac.c': _driver(original['drivers/net/dwc_eth_xgmac.c']),
               'net/Makefile': original['net/Makefile'] + SUFFIX,
               HEADER_PATH: HEADER, HELPER_PATH: HELPER}
    for name, text in changed.items():
        (source / name).parent.mkdir(parents=True, exist_ok=True)
        (source / name).write_text(text)
    return dict(scope='recovery-return-observations-v1', originals=dict(PINNED),
                prepared={n: hashlib.sha256(t.encode()).hexdigest() for n, t in changed.items()})


def reconstruct(source, record):
    names = set(PINNED) | {HEADER_PATH, HELPER_PATH}
    if (not isinstance(record, dict) or set(record) != {'scope', 'originals', 'prepared'} or
            record['scope'] != 'recovery-return-observations-v1' or record['originals'] != PINNED or
            not isinstance(record['prepared'], dict) or set(record['prepared']) != names):
        raise ValueError('recovery return receipt differs')
    actual = _read(source, names)
    if any(hashlib.sha256(t.encode()).hexdigest() != record['prepared'][n] for n, t in actual.items()):
        raise ValueError('recovery return prepared source changed')
    if actual[HEADER_PATH] != HEADER or actual[HELPER_PATH] != HELPER or not actual['net/Makefile'].endswith(SUFFIX):
        raise ValueError('recovery return helper or build entry differs')
    original = {'net/net.c': _apply(actual['net/net.c'], NET_PATCHES, True),
                'cmd/net.c': _apply(actual['cmd/net.c'], CMD_PATCHES, True),
                'drivers/net/dwc_eth_xgmac.c': _driver(actual['drivers/net/dwc_eth_xgmac.c'], True),
                'net/Makefile': actual['net/Makefile'][:-len(SUFFIX)]}
    if any(hashlib.sha256(t.encode()).hexdigest() != PINNED[n] for n, t in original.items()):
        raise ValueError('recovery return source does not reconstruct pinned input')
    return original


def check_phy_sources(source, record, phy_record, recovery):
    """Run the unchanged PHY validator on reconstructed private source bytes."""
    import tempfile
    original = reconstruct(source, record)
    names = set(recovery.PINNED_PHY_SOURCES) | {'cmd/fes_nano_phy.c'}
    actual = _read(source, names)
    actual['drivers/net/dwc_eth_xgmac.c'] = original['drivers/net/dwc_eth_xgmac.c']
    with tempfile.TemporaryDirectory(prefix='fes-recovery-return-inverse-') as directory:
        for name, text in actual.items():
            p = Path(directory) / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(text)
        recovery.check_phy_sources(directory, phy_record)


def check_linked(symbols):
    for name in ('fes_recovery_return_begin', 'fes_recovery_return_mark', 'fes_recovery_return_end'):
        rows = [line.split() for line in symbols.splitlines() if line.split() and line.split()[-1] == name]
        if (len(rows) != 1 or len(rows[0]) != 8 or rows[0][3:5] != ['FUNC', 'GLOBAL'] or
                rows[0][6] == 'UND' or not rows[0][2].isdigit() or int(rows[0][2]) == 0):
            raise ValueError('recovery return helper not linked: ' + name)


def check_console(fdtget, dtb, *, env=None):
    """Resolve the compiled stdout alias; serial0 is UART1 on this board."""
    import re
    import subprocess
    def prop(node, name, kind='s'):
        return subprocess.check_output([str(fdtget), '-t', kind, str(dtb), node, name],
                                       env=env, text=True, timeout=30).strip()
    stdout = prop('/chosen', 'stdout-path')
    match = re.fullmatch(r'([A-Za-z0-9_/@,.-]+):115200n8', stdout)
    if match is None or '..' in match[1]:
        raise ValueError('trace console selection or baud differs')
    selected = match[1]
    node = selected if selected.startswith('/') else prop('/aliases', selected)
    if not re.fullmatch(r'/[A-Za-z0-9_/@,.-]+', node) or '..' in node:
        raise ValueError('trace console alias is not a device path')
    reg = prop(node, 'reg', 'x').split()
    shift, width = prop(node, 'reg-shift', 'x'), prop(node, 'reg-io-width', 'x')
    if (len(reg) != 2 or [int(v, 16) for v in reg] != [UART_DATA, 0x100] or
            shift != '2' or width != '4' or prop(node, 'status') != 'okay' or
            prop(node, 'compatible') != 'snps,dw-apb-uart'):
        raise ValueError('trace helper does not match the enabled board console')
    return dict(stdout_path=stdout, resolved_node=node, data_address=UART_DATA,
                status_address=UART_STATUS, bytes=0x100, reg_shift=2, reg_io_width=4)


def decode(text):
    """Retain a bounded partial trace; never turn it into command/SD permission."""
    import re
    if len(text.encode()) > 1 << 20:
        raise ValueError('recovery return console exceeds byte bound')
    records = []
    valid = True
    lines = text.splitlines(keepends=True)
    for line in lines:
        if not line.startswith('FES_RECOVERY_RETURN'):
            continue
        if not line.endswith('\n'):
            return dict(valid=valid, complete=False, phases=records, partial=True)
        match = re.fullmatch(r'FES_RECOVERY_RETURN phase=([a-z-]+)\r?\n', line)
        if len(records) >= len(PHASES) or not match or match[1] != PHASES[len(records)]:
            valid = False
            break
        records.append(match[1])
    return dict(valid=valid, complete=valid and tuple(records) == PHASES, phases=records, partial=False)
