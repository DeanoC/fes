"""Explicit network/SD-writing U-Boot recovery profile; never native handoff."""
import hashlib
from pathlib import Path

# Keep recovery independent of native device ownership. It uses vendor Ethernet
# support and the strict firmware, stops at a manual prompt and writes nothing
# automatically. A stable board MAC must be supplied before network use.
ENABLE = ('NET', 'NETDEVICES', 'CMD_NET', 'CMD_TFTPBOOT', 'CMD_DHCP',
          'CMD_MMC', 'MMC_WRITE', 'CMD_FAT', 'FAT_WRITE', 'CMD_HASH', 'SHA256', 'CMD_MII',
          'ENV_IS_NOWHERE', 'DWC_ETH_XGMAC', 'DWC_ETH_XGMAC_SOCFPGA',
          'PHY_MICREL', 'PHY_MICREL_KSZ90X1', 'PHY_GIGE')
DISABLE = ('NO_NET', 'NET_RANDOM_ETHADDR', 'USB', 'CMD_SAVEENV',
           'ENV_IS_IN_FAT', 'ENV_IS_IN_MMC', 'ENV_IS_IN_SPI_FLASH', 'ENV_IS_IN_UBI',
           'MMC_SDHCI_SDMA', 'MMC_SDHCI_ADMA', 'MMC_SDHCI_ADMA_64BIT',
           'SPL_MMC_SDHCI_SDMA', 'SPL_MMC_SDHCI_ADMA')
LOCALVERSION = '-fes-nano-recovery-phy-prepare-gd0f8813fd6bf'
# Exact top-kit readback, not the synthetic PHY used by native host tests.
PHY = {'address':1, 'id':0x00221622, 'driver':'Micrel ksz9031', 'aneg_timeout_ms':4000,
       'prepare_command':'fesphy', 'prepare_before_link':True}

PINNED_PHY_SOURCES={
    'drivers/net/dwc_eth_xgmac.c':'0e22fd3acb1cfd3806f9fbc0abc2cb0f527c395cdc394a747a5a6e56f1d6aed7',
    'drivers/net/dwc_eth_xgmac.h':'fa38d452898cefc79c35ee209e2213da672566f640ddd3d4fd5d5bf2da2c1ca8',
    'cmd/Makefile':'7352588e2d55075e6a3c749256e2a8112f246c860349c5e7696d1132a9dae2a3'}

# The vendor's first xgmac_start calls phy_connect, which resets the PHY. Move
# that operation to an explicit command, before the uploader samples link.
# DHCP must consume prepared state without another reset/configuration.
PHY_PREPARE=r'''
int fes_nano_recovery_phy_prepare(struct udevice *dev)
{
	struct xgmac_priv *xgmac = dev_get_priv(dev);
	struct phy_device *phy;
	int addr, high, low, control, ret;

	if (dev_read_addr(dev) != 0x10810000 || !xgmac->mii)
		return -ENODEV;
	if (xgmac->fes_phy_attempted) {
		if (!xgmac->fes_phy_prepared || !xgmac->phy)
			return -EIO;
		phy = xgmac->phy;
	} else {
		/* Consume before the first reset; uncertainty is never replayed. */
		xgmac->fes_phy_attempted = true;
		addr = xgmac_get_phy_addr(xgmac, dev);
		if (addr != 1)
			return -ENODEV;
		high = xgmac->mii->read(xgmac->mii, addr, MDIO_DEVAD_NONE, MII_PHYSID1);
		low = xgmac->mii->read(xgmac->mii, addr, MDIO_DEVAD_NONE, MII_PHYSID2);
		if (high != 0x0022 || low != 0x1622)
			return -ENODEV;
		phy = phy_connect(xgmac->mii, addr, dev, xgmac->config->interface(dev));
		if (!phy)
			return -ENODEV;
		if (phy->phy_id != 0x00221622) {
			ret = -ENODEV;
			goto fail;
		}
		control = phy_read(phy, MDIO_DEVAD_NONE, MII_BMCR);
		if (control < 0 || (control & BMCR_RESET)) {
			ret = -EIO;
			goto fail;
		}
		if (xgmac->max_speed) {
			ret = phy_set_supported(phy, xgmac->max_speed);
			if (ret)
				goto fail;
		}
		phy->node = xgmac->phy_of_node;
		ret = phy_config(phy);
		if (ret)
			goto fail;
		xgmac->phy = phy;
		xgmac->fes_phy_prepared = true;
	}
	/* A later command is a fresh read-only check of that same preparation. */
	high = phy_read(phy, MDIO_DEVAD_NONE, MII_PHYSID1);
	low = phy_read(phy, MDIO_DEVAD_NONE, MII_PHYSID2);
	control = phy_read(phy, MDIO_DEVAD_NONE, MII_BMCR);
	if (high == 0x0022 && low == 0x1622 && control >= 0 && !(control & BMCR_RESET))
		return 0;
	xgmac->fes_phy_prepared = false;
	return -EIO;
fail:
	phy_shutdown(phy);
	return ret;
}

'''

PHY_COMMAND=r'''// SPDX-License-Identifier: GPL-2.0+
/* Recovery-only explicit PHY preparation; no packet, SD or boot operation. */
#ifndef CONFIG_SPL_BUILD
#include <command.h>
#include <dm.h>
#include <dm/uclass.h>
#include <dm/uclass-id.h>

extern int fes_nano_recovery_phy_prepare(struct udevice *dev);

static int do_fesphy(struct cmd_tbl *cmdtp, int flag, int argc, char *const argv[])
{
	struct udevice *dev;
	int ret = uclass_get_device_by_name(UCLASS_ETH, "ethernet@10810000", &dev);

	if (!ret)
		ret = fes_nano_recovery_phy_prepare(dev);
	if (ret) {
		printf("FES_RECOVERY_PHY REJECT status=%d\n", ret);
		return CMD_RET_FAILURE;
	}
	puts("FES_RECOVERY_PHY PREPARED address=1 id=00221622 configured=1 reset=0\n");
	return CMD_RET_SUCCESS;
}

U_BOOT_CMD(fesphy, 1, 0, do_fesphy, "prepare the matched recovery PHY before link polling", "");
#endif
'''

PHY_OLD_START='\t/*\n\t * if PHY was already connected and configured,\n\t * don\'t need to reconnect/reconfigure again\n\t */\n\tif (!xgmac->phy) {\n\t\taddr = xgmac_get_phy_addr(xgmac, dev);\n\t\txgmac->phy = phy_connect(xgmac->mii, addr, dev,\n\t\t\t\t\t xgmac->config->interface(dev));\n\t\tif (!xgmac->phy) {\n\t\t\tpr_err("%s phy_connect() failed\\n", dev->name);\n\t\t\tgoto err_stop_resets;\n\t\t}\n\n\t\tif (xgmac->max_speed) {\n\t\t\tret = phy_set_supported(xgmac->phy, xgmac->max_speed);\n\t\t\tif (ret) {\n\t\t\t\tpr_err("%s phy_set_supported() failed: %d\\n",\n\t\t\t\t       dev->name, ret);\n\n\t\t\t\tgoto err_shutdown_phy;\n\t\t\t}\n\t\t}\n\n\t\txgmac->phy->node = xgmac->phy_of_node;\n\t\tret = phy_config(xgmac->phy);\n\t\tif (ret < 0) {\n\t\t\tpr_err("%s phy_config() failed: %d\\n", dev->name, ret);\n\t\t\tgoto err_shutdown_phy;\n\t\t}\n\t}\n'
PHY_READY_GATE='\t/* fesphy prepared this exact object before link readiness. */\n\tif (!xgmac->fes_phy_attempted || !xgmac->fes_phy_prepared || !xgmac->phy) {\n\t\tret = -EIO;\n\t\tgoto err_stop_resets;\n\t}\n'


def prepare_phy_sources(source):
    """Transform a fresh private copy of the pinned Terasic driver only."""
    source=Path(source)
    originals={}
    for name,expected in PINNED_PHY_SOURCES.items():
        path=source/name
        if path.is_symlink() or not path.is_file() or not 0<path.stat().st_size<=1<<20:
            raise ValueError('recovery PHY source is not a bounded regular file: '+name)
        originals[name]=path.read_bytes()
        if hashlib.sha256(originals[name]).hexdigest()!=expected:
            raise ValueError('recovery PHY source differs from the pinned Terasic driver: '+name)
    command=source/'cmd/fes_nano_phy.c'
    if command.exists() or command.is_symlink():raise ValueError('recovery PHY command already exists')
    driver=originals['drivers/net/dwc_eth_xgmac.c'].decode()
    start=driver.index('\t/*\n\t * if PHY was already connected and configured,')
    end=driver.index('\n\tret = phy_startup(xgmac->phy);',start)
    gate=PHY_READY_GATE
    driver=driver[:start]+gate+driver[end:]
    anchor='static int xgmac_start(struct udevice *dev)'
    if driver.count(anchor)!=1 or driver.count('\tint addr = -1;\n')!=1:
        raise ValueError('recovery PHY start anchors differ')
    driver=driver.replace('\tint addr = -1;\n','').replace(anchor,PHY_PREPARE+anchor)
    header=originals['drivers/net/dwc_eth_xgmac.h'].decode()
    field='\tstruct phy_device *phy;\n'
    if header.count(field)!=1:raise ValueError('recovery PHY state anchor differs')
    header=header.replace(field,field+'\tbool fes_phy_attempted;\n\tbool fes_phy_prepared;\n')
    header+='\nint fes_nano_recovery_phy_prepare(struct udevice *dev);\n'
    changed={'drivers/net/dwc_eth_xgmac.c':driver,'drivers/net/dwc_eth_xgmac.h':header,
             'cmd/Makefile':originals['cmd/Makefile'].decode()+'\nobj-y += fes_nano_phy.o\n',
             'cmd/fes_nano_phy.c':PHY_COMMAND}
    for name,text in changed.items():(source/name).write_text(text)
    return {'scope':'recovery-explicit-phy-before-link-v1','originals':dict(PINNED_PHY_SOURCES),
            'prepared':{name:hashlib.sha256(text.encode()).hexdigest() for name,text in changed.items()}}


def check_phy_sources(source,record):
    """Reconstruct all pinned inputs; a self-consistent edited receipt is insufficient."""
    names=set(PINNED_PHY_SOURCES)|{'cmd/fes_nano_phy.c'}
    if (not isinstance(record,dict) or set(record)!={'scope','originals','prepared'} or
            record['scope']!='recovery-explicit-phy-before-link-v1' or
            record['originals']!=PINNED_PHY_SOURCES or not isinstance(record['prepared'],dict) or
            set(record['prepared'])!=names):
        raise ValueError('recovery PHY transformation receipt differs')
    actual={}
    for name in names:
        path=Path(source)/name
        if path.is_symlink() or not path.is_file() or not 0<path.stat().st_size<=1<<20:
            raise ValueError('recovery PHY prepared source is not a bounded regular file: '+name)
        raw=path.read_bytes()
        actual[name]=raw.decode()
        if hashlib.sha256(raw).hexdigest()!=record['prepared'][name]:
            raise ValueError('recovery PHY prepared source changed: '+name)
    driver=actual['drivers/net/dwc_eth_xgmac.c']
    address='\tstruct xgmac_desc *rx_desc = NULL;\n'
    if driver.count(PHY_PREPARE)!=1 or driver.count(PHY_READY_GATE)!=1 or driver.count(address)!=1:
        raise ValueError('recovery PHY driver transformation differs')
    driver=driver.replace(PHY_PREPARE,'').replace(PHY_READY_GATE,PHY_OLD_START)
    driver=driver.replace(address,address+'\tint addr = -1;\n')
    header=actual['drivers/net/dwc_eth_xgmac.h']
    fields='\tbool fes_phy_attempted;\n\tbool fes_phy_prepared;\n'
    declaration='\nint fes_nano_recovery_phy_prepare(struct udevice *dev);\n'
    suffix='\nobj-y += fes_nano_phy.o\n'
    if (header.count(fields)!=1 or not header.endswith(declaration) or not actual['cmd/Makefile'].endswith(suffix) or
            actual['cmd/fes_nano_phy.c']!=PHY_COMMAND):
        raise ValueError('recovery PHY state or command transformation differs')
    reconstructed={'drivers/net/dwc_eth_xgmac.c':driver,'drivers/net/dwc_eth_xgmac.h':header[:-len(declaration)].replace(fields,''),
                   'cmd/Makefile':actual['cmd/Makefile'][:-len(suffix)]}
    if any(hashlib.sha256(text.encode()).hexdigest()!=PINNED_PHY_SOURCES[name] for name,text in reconstructed.items()):
        raise ValueError('recovery PHY prepared source does not reconstruct pinned input')
    return record


def check(config):
    if any('CONFIG_' + name + '=y\n' not in config for name in ENABLE):
        raise ValueError('network recovery lacks a required Ethernet, checksum or SD-writing option')
    if any('CONFIG_' + name + '=y\n' in config for name in DISABLE):
        raise ValueError('network recovery enabled excluded DMA, random MAC, USB or persistent environment')
    if 'CONFIG_BOOTDELAY=-1\n' not in config or 'CONFIG_BOOTCOMMAND=""\n' not in config:
        raise ValueError('network recovery must stop without automatic boot or writes')
    if ('CONFIG_PHY_ANEG_TIMEOUT=4000\n' not in config or
            'CONFIG_LOCALVERSION="'+LOCALVERSION+'"\n' not in config):
        raise ValueError('network recovery PHY deadline or identified vendor profile differs')


def check_linked(symbols):
    for name,kind,binding in (('ksz9031_config','FUNC','LOCAL'),('_u_boot_list_2_phy_driver_2_ksz9031','OBJECT','GLOBAL'),
                              ('fes_nano_recovery_phy_prepare','FUNC','GLOBAL'),
                              ('_u_boot_list_2_cmd_2_fesphy','OBJECT','GLOBAL')):
        rows=[line.split() for line in symbols.splitlines() if line.split() and line.split()[-1]==name]
        if (len(rows)!=1 or len(rows[0])!=8 or rows[0][3]!=kind or rows[0][4]!=binding or
                rows[0][6]=='UND' or not rows[0][2].isdigit() or int(rows[0][2])==0):
            raise ValueError('recovery does not link its KSZ9031 driver: '+name)
