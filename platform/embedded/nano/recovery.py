"""Explicit network/SD-writing U-Boot recovery profile; never native handoff."""

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
LOCALVERSION = '-fes-nano-recovery-ksz9031'
# Exact top-kit readback, not the synthetic PHY used by native host tests.
PHY = {'address':1, 'id':0x00221622, 'driver':'Micrel ksz9031', 'aneg_timeout_ms':4000}


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
    for name,kind,binding in (('ksz9031_config','FUNC','LOCAL'),('_u_boot_list_2_phy_driver_2_ksz9031','OBJECT','GLOBAL')):
        rows=[line.split() for line in symbols.splitlines() if line.split() and line.split()[-1]==name]
        if (len(rows)!=1 or len(rows[0])!=8 or rows[0][3]!=kind or rows[0][4]!=binding or
                rows[0][6]=='UND' or not rows[0][2].isdigit() or int(rows[0][2])==0):
            raise ValueError('recovery does not link its KSZ9031 driver: '+name)
