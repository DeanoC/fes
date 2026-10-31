"""Explicit network/SD-writing U-Boot recovery profile; never native handoff."""

# Keep recovery independent of native device ownership. It uses vendor Ethernet
# support and the strict firmware, stops at a manual prompt and writes nothing
# automatically. A stable board MAC must be supplied before network use.
ENABLE = ('NET', 'NETDEVICES', 'CMD_NET', 'CMD_TFTPBOOT', 'CMD_DHCP',
          'CMD_MMC', 'MMC_WRITE', 'CMD_FAT', 'FAT_WRITE', 'CMD_HASH', 'SHA256',
          'ENV_IS_NOWHERE', 'DWC_ETH_XGMAC', 'DWC_ETH_XGMAC_SOCFPGA')
DISABLE = ('NO_NET', 'NET_RANDOM_ETHADDR', 'USB', 'CMD_SAVEENV',
           'ENV_IS_IN_FAT', 'ENV_IS_IN_MMC', 'ENV_IS_IN_SPI_FLASH', 'ENV_IS_IN_UBI',
           'MMC_SDHCI_SDMA', 'MMC_SDHCI_ADMA', 'MMC_SDHCI_ADMA_64BIT',
           'SPL_MMC_SDHCI_SDMA', 'SPL_MMC_SDHCI_ADMA')


def check(config):
    if any('CONFIG_' + name + '=y\n' not in config for name in ENABLE):
        raise ValueError('network recovery lacks a required Ethernet, checksum or SD-writing option')
    if any('CONFIG_' + name + '=y\n' in config for name in DISABLE):
        raise ValueError('network recovery enabled excluded DMA, random MAC, USB or persistent environment')
    if 'CONFIG_BOOTDELAY=-1\n' not in config or 'CONFIG_BOOTCOMMAND=""\n' not in config:
        raise ValueError('network recovery must stop without automatic boot or writes')
