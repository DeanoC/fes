"""Install the dedicated Nano handoff into a private pinned U-Boot export."""
import hashlib
import importlib.util
import json
import re
from pathlib import Path
import shutil

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def cold_reset_source(text):
    # Terasic d0f8813f: OS_PREPARE releases USB/NAND/DMA unconditionally.
    # SPL/BL31 own these resets in this profile; cleanup must retain them.
    if hashlib.sha256(text.encode()).hexdigest() != 'e8441d7467106dfc5e0b9c2b9373d8ecf44da279f66796776a48955a8ace155b':
        raise ValueError('unreviewed Nano reset cleanup source')
    for name, replacement in (
        ('static bool socfpga_reset_keep_enabled(void)', ''),
        ('static int socfpga_reset_remove(struct udevice *dev)', '''static int socfpga_reset_remove(struct udevice *dev)
{
\t/* Cold serial ownership requires all drained HPS masters to stay held.
\t * Neither OS cleanup nor a legacy environment variable may release them. */
\t(void)dev;
\treturn 0;
}
'''),
    ):
        text, count = re.subn(re.escape(name) + r'\n\{.*?\n\}\n', replacement, text, count=1, flags=re.S)
        if count != 1:
            raise ValueError('Nano reset cleanup context changed')
    return text


def atf_descriptor(bl31_sha256, bl31_size):
    if (not isinstance(bl31_sha256, str) or len(bl31_sha256) != 64 or
            any(c not in "0123456789abcdef" for c in bl31_sha256) or int(bl31_sha256, 16) == 0 or
            type(bl31_size) is not int or not 16 <= bl31_size <= 2 << 20):
        raise ValueError("missing exact BL31 identity for the matched Nano loader")
    return {'format': 2, 'scope': 'loaded-bl31-handoff-v2', 'target': 0x80000000,
            'bytes': bl31_size, 'sha256': bl31_sha256, 'bl32': 0, 'bl33': 0x80200000,
            'fdt': 0, 'params_base': 0xbff00000, 'params_bytes': 0x100000}


def checked_atf_descriptor(value):
    if (not isinstance(value,dict) or value!=atf_descriptor(value.get('sha256'),value.get('bytes')) or
            any(type(v) is not int for key,v in value.items() if key not in ('scope','sha256'))):
        raise ValueError('checked loaded BL31 handoff descriptor required')
    return dict(value)


def atf_source(text, *, firmware_ram=False, firmware_ocram=False):
    if type(firmware_ram) is not bool:raise ValueError('explicit firmware RAM guard selection required')
    if type(firmware_ocram) is not bool or firmware_ocram and not firmware_ram:
        raise ValueError('selected OCRAM branch requires the final SPL memory guard')
    before='typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params);'
    if text.count(before)!=1:raise ValueError('SPL ATF argument declaration changed')
    text=text.replace(before,'typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params, const void *cold, unsigned long cookie);')
    before='\tatf_entry(bl31_params, (void *)fdt_addr);'
    if text.count(before)!=1:raise ValueError('SPL ATF entry context changed')
    text=text.replace(before,'''\t{
\t\textern const void *fes_nano_spl_handoff(void);
\t\textern void fes_nano_spl_atf_trace(uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t);
\t\tconst void *cold = fes_nano_spl_handoff();
\t\tfes_nano_spl_atf_trace(bl31_entry, bl32_entry, bl33_entry, fdt_addr, (uintptr_t)bl31_params, (uintptr_t)cold);
\t\tatf_entry(bl31_params, (void *)fdt_addr, cold, FES_NANO_COLD_COOKIE);
\t}''')
    if firmware_ram:
        before='\t\tatf_entry(bl31_params, (void *)fdt_addr, cold, FES_NANO_COLD_COOKIE);'
        if text.count(before)!=1:raise ValueError('SPL final branch guard context changed')
        text=text.replace(before,'\t\textern int fes_nano_spl_firmware_ram_guard(uintptr_t, uintptr_t);\n'
            '\t\textern void __noreturn fes_nano_spl_firmware_ram_rejected(void);\n'
            '\t\tif (fes_nano_spl_firmware_ram_guard((uintptr_t)bl31_params, (uintptr_t)cold)) fes_nano_spl_firmware_ram_rejected();\n'+before)
    if firmware_ocram:
        text=text.replace('const void *cold, unsigned long cookie);', 'const void *cold, unsigned long cookie, unsigned long ram_cookie);')
        text=text.replace('atf_entry(bl31_params, (void *)fdt_addr, cold, FES_NANO_COLD_COOKIE);',
                          'atf_entry(bl31_params, (void *)fdt_addr, cold, FES_NANO_COLD_COOKIE, FES_NANO_FW_RAM_COOKIE);')
    return '#include <nano_cold.h>\n'+('#include <nano_firmware_ram.h>\n' if firmware_ocram else '')+text


def prepare(source, entry_build, bl31_sha256, bl31_size, *, firmware_ram=False):
    atf_handoff=atf_descriptor(bl31_sha256,bl31_size)
    spec=importlib.util.spec_from_file_location('fes_nano_cold_profile', HERE/'cold_profile.py')
    cold=importlib.util.module_from_spec(spec);spec.loader.exec_module(cold)
    image, receipt_path=cold.entry(entry_build)
    profile=cold.selected_profile(json.loads(receipt_path.read_text()))
    chain=cold.make(entry_build,firmware_ram=firmware_ram)
    if chain.get('firmware_ram') is not None:
        spec=importlib.util.spec_from_file_location('fes_affinity_call',HERE.parent/'firmware/nano_firmware_call.py')
        call=importlib.util.module_from_spec(spec);spec.loader.exec_module(call)
        path=source/call.FWCALL
        path.write_text(call.fwcall_source(path.read_text()))
    reset=source/'drivers/reset/reset-socfpga.c'
    reset.write_text(cold_reset_source(reset.read_text()))
    bootm = source / "arch/arm/lib/bootm.c"
    text = bootm.read_text()
    anchor = "\tdm_remove_devices_active();\n\n\tcleanup_before_linux();"
    if text.count(anchor) != 1:
        raise ValueError("unexpected pinned U-Boot final cleanup path")
    kick = "static void do_nonsec_virt_switch(void)\n{\n\tsmp_kick_all_cpus();"
    if text.count(kick) != 1:
        raise ValueError("unexpected pinned U-Boot secondary CPU kick path")
    text = text.replace(kick, "static void do_nonsec_virt_switch(void)\n{\n\t/* Dedicated Nano probe: retain verified OFF secondary CPUs. */")
    bootm.write_text(text.replace(anchor, "\tdm_remove_devices_active();\n\t{\n\t\textern void fes_nano_late_handoff(void);\n\t\tfes_nano_late_handoff();\n\t}\n\n\tcleanup_before_linux();"))
    for src, dest in (("handoff.c", "fes_nano_policy.c"), ("handoff.h", "handoff.h"), ("uboot.c", "fes_nano.c")):
        shutil.copyfile(HERE / src, source / "cmd" / dest)
    firmware=HERE.parent/'firmware'
    shutil.copyfile(firmware/'generated/fes_embedded_bridges.h',source/'cmd/fes_embedded_bridges.h')
    soc=source/'arch/arm/mach-socfpga'
    # SPL's ATF caller and record producer must use the same wire definition.
    # Install the shared headers on U-Boot's include path, rather than copying
    # a cookie literal into the separate common/spl translation unit.
    for name in ('nano_cold.h','nano_boot.h','nano_bridges.h'):
        shutil.copyfile(firmware/name,source/'include'/name)
    for name in ('nano_cold.c','nano_cold_mmio.inc'):
        shutil.copyfile(firmware/name,soc/name)
    # U-Boot's stdint.h is intentionally empty; its fixed-width types come
    # from linux/types.h. Keep the standalone/TF-A header unchanged.
    types=source/'include/nano_bridges.h';text=types.read_text()
    if text.count('#include <stdint.h>')!=1:raise ValueError('shared integer type context changed')
    types.write_text(text.replace('#include <stdint.h>','#include <linux/types.h>'))
    shutil.copyfile(firmware/'nano_firmware_ram.h',source/'include/nano_firmware_ram.h')
    shutil.copyfile(HERE/'spl_cold.c',soc/'fes_nano_spl.c')
    shutil.copyfile(HERE/'spl_console.inc',soc/'fes_nano_spl_console.inc')
    (soc/'fes_nano_cold_profile.h').write_text(cold.header(chain))
    # The first compilation can produce the FIT but cannot boot it. The build
    # then seals that exact FIT into SPL and rebuilds only SPL, avoiding a hash cycle.
    (soc/'fes_nano_fit_seal.h').write_text('#define FES_NANO_SEALED_FIT_SIZE 0UL\n#define FES_NANO_SEALED_FIT_SHA {0}\n')
    (soc/'fes_nano_atf_seal.h').write_text(
        '#define FES_NANO_ATF_SIZE '+str(bl31_size)+'UL\n#define FES_NANO_ATF_SHA {'+
        ','.join('0x'+bl31_sha256[i:i+2] for i in range(0,64,2))+'}\n')
    (soc/'fes_nano_spl.h').write_text('void fes_nano_spl_early(void);\nvoid fes_nano_spl_report(void);\nconst void *fes_nano_spl_handoff(void);\n')
    spl=soc/'spl_agilex5.c';text=spl.read_text()
    for before,after in (('\tret = spl_early_init();','\tfes_nano_spl_early();\n\tret = spl_early_init();'),
                         ('\tpreloader_console_init();','\tpreloader_console_init();\n\tfes_nano_spl_report();')):
        if text.count(before)!=1:raise ValueError('cold SPL entry context changed')
        text=text.replace(before,after)
    spl.write_text('#include "fes_nano_spl.h"\n'+text)
    mk=soc/'Makefile';text=mk.read_text()
    start=text.index('ifdef CONFIG_TARGET_SOCFPGA_AGILEX5\n')
    end=text.index('ifdef CONFIG_TARGET_SOCFPGA_AGILEX7M\n',start)
    block=text[start:end]
    block,count=re.subn(r'(?m)^obj-[^\n]*\+= (?:rsu|rsu_ll_qspi|rsu_misc|rsu_s10|smc_rsu_s10)\.o\n','',block)
    if count!=5:raise ValueError('native flash/RSU object context changed')
    mk.write_text(text[:start]+block+text[end:]+'\nifdef CONFIG_SPL_BUILD\nobj-y += fes_nano_spl.o nano_cold.o\nendif\n')
    atf=source/'common/spl/spl_atf.c'
    atf.write_text(atf_source(atf.read_text(), firmware_ram=chain.get('firmware_ram') is not None or
                              chain.get('hps_rom') is not None and chain.get('usb_shared') is True,
                              firmware_ocram=chain.get('firmware_ram') is not None))
    fat=source/'common/spl/spl_fat.c';text=fat.read_text()
    anchor='\terr = spl_register_fat_device(block_dev, partition);\n\tif (err)\n\t\tgoto end;\n'
    if text.count(anchor)!=1:raise ValueError('SPL FAT pre-parser context changed')
    text=text.replace(anchor,anchor+'\n\t{\n\t\textern int fes_nano_spl_fit_check(const char *);\n\t\terr = fes_nano_spl_fit_check(filename);\n\t\tif (err) goto end;\n\t}\n')
    body,count=re.subn(r'(static ulong spl_fit_read\([^\{]+\{).*?\n\}',
        r'\1\n\textern unsigned long fes_nano_spl_fit_read(void *, unsigned long, unsigned long);\n\t(void)load;\n\treturn fes_nano_spl_fit_read(buf, file_offset, size);\n}',text,count=1,flags=re.S)
    if count!=1:raise ValueError('SPL FAT stream context changed')
    fat.write_text(body)
    board_r=source/'common/board_r.c';text=board_r.read_text()
    before='\tinitr_mmc,'
    if text.count(before)!=1:raise ValueError('full loader MMC initialization context changed')
    text=text.replace(before,'\t/* Cold serial profile: SD was drained/reset by SPL; never probe it here. */')
    text,count=re.subn(r'static int initr_mmc\(void\)\n\{.*?\n\}\n','',text,count=1,flags=re.S)
    if count!=1:raise ValueError('full loader MMC implementation context changed')
    board_r.write_text(text)
    dts=source/'arch/arm/dts/socfpga_agilex5_de25_nano-u-boot.dtsi'
    dts.write_text(dts.read_text()+'''
/ {
    reserved-memory {
        fes_bl31: firmware@80000000 {
            reg = <0x0 0x80000000 0x0 0x00200000>;
            no-map;
        };
    };
};
&images {
    fes-entry {
        description = "FES native serial entry";
        type = "firmware";
        os = "linux";
        arch = "arm64";
        compression = "none";
        load = <0x96080000>;
        blob-ext { filename = "fes-entry.img"; };
        hash { algo = "crc32"; };
    };
};
&board_config { board-0 { loadables = "uboot", "fes-entry"; }; };
/ { chosen { u-boot,spl-boot-order = &mmc; }; };
''')
    board_header=source/'include/configs/socfpga_agilex5_de25_nano.h'
    text=board_header.read_text();anchor='#include <configs/socfpga_soc64_common.h>\n'
    if text.count(anchor)!=1:raise ValueError('native board environment context changed')
    board_header.write_text(text.replace(anchor,anchor+'\n/* Dedicated serial profile has no storage/flash boot scripts. */\n#undef CFG_EXTRA_ENV_SETTINGS\n#define CFG_EXTRA_ENV_SETTINGS ""\n'))
    makefile = source / "cmd/Makefile"
    makefile.write_text(makefile.read_text() + "\nobj-y += fes_nano.o fes_nano_policy.o\n")
    producer_sources = {str(p.relative_to(ROOT)): sha(p) for p in
                        (*cold.sources(), firmware/"generated/fes_embedded_bridges.h")}
    build_id = hashlib.sha256(json.dumps({"sources": producer_sources, "entry": sha(image), "bl31": bl31_sha256}, sort_keys=True).encode()).hexdigest()[:16]
    digest = bytes.fromhex(sha(image))
    arenas=[]
    if chain.get('hps_configuration'):
        m=chain['hps_configuration'];arenas.append((m['base'],m['size']))
    if chain.get('hps_reader_memory'):
        m=chain['hps_reader_memory'];arenas.append((m['arena_base'],m['arena_bytes']))
    (source / "cmd/fes_nano_config.h").write_text(
        '/* Generated from the exact entry image and handoff sources. */\n#define FES_NANO_BOOT_ID "' + build_id +
        '"\n#define FES_NANO_IMAGE_SIZE ' + str(image.stat().st_size) +
        "ULL\n#define FES_NANO_IMAGE_SHA256 {" + ",".join(hex(b) for b in digest) + "}\n"+
        '#define FES_NANO_OWNED_ARENA_COUNT '+str(len(arenas))+'U\n'+
        '#define FES_NANO_OWNED_ARENAS {'+(','.join('{'+hex(a)+'ULL,'+hex(n)+'ULL}' for a,n in arenas) if arenas else '{0,0}')+'}\n')
    return {"entry_image_sha256": sha(image), "entry_receipt_sha256": sha(receipt_path), "bl31_sha256": bl31_sha256,
            "profile_sha256": sha(profile), "build_id": build_id,
            "classification": "source-derived serial-only Nano handoff; physical gate not executed",
            "producer_sources": producer_sources, "cold_boot_profile":chain, "preloaded_entry": True,
            "atf_handoff":atf_handoff}


DISABLE = ("NET", "NETDEVICES", "USB", "ENV_IS_IN_FAT", "ENV_IS_IN_UBI", "ENV_IS_IN_SPI_FLASH",
           "FAT_WRITE", "MMC_WRITE", "CMD_SAVEENV", "MMC_SDHCI_SDMA", "MMC_SDHCI_ADMA",
           "SPL_MMC_SDHCI_SDMA", "SPL_MMC_SDHCI_ADMA", "MMC_SDHCI_ADMA_64BIT",
           "CMD_MMC", "CMD_FAT", "CMD_FS_GENERIC", "MTD", "MTD_RAW_NAND", "SPI", "SPI_FLASH",
           "CADENCE_QSPI", "DMA", "SPL_DMA", "SPL_OS_BOOT", "SPL_OPENSBI",
           "DISTRO_DEFAULTS", "BOOTSTD", "EFI_LOADER", "CMD_EXT2", "CMD_EXT4", "CMD_BOOTFLOW", "CMD_SYSBOOT",
           "SPL_SPI", "SPL_SPI_FLASH_SUPPORT", "SPL_SPI_LOAD", "SPL_NAND_SUPPORT", "SPL_MTD",
           "SPL_DM_SPI_FLASH", "SPL_DM_SPI", "CMD_FPGA", "CMD_UBI", "CMD_UBIFS", "MTD_UBI", "SPL_RAM_DEVICE",
           "SPL_FS_FAT_DMA_ALIGN")
ENABLE = ("NO_NET", "ENV_IS_NOWHERE", "CMD_BOOTI", "SHA256", "LMB", "CMD_BDI", "CMD_WDT")
