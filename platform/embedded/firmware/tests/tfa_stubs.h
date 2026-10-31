// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))
#define PLATFORM_MODEL 5
#define PLAT_SOCFPGA_AGILEX5 5
#define DRAM_BASE 0x80000000U
#define DRAM_SIZE 0x80000000U
#define BL31_LIMIT 0x82000000U
#define CONFIG_TEST_FLAG(flag, name) ((flag)&(name))
#define PARTIAL_CONFIG 1
#define AUTHENTICATION 2
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define MBOX_RECONFIG 6
#define MBOX_RECONFIG_STATUS 9
#define MBOX_CONFIG_STATUS 4
#define MBOX_CFGSTAT_STATE_CONFIG 0x10000000U
#define MBOX_JOB_ID 1
#define MBOX_CMD_CANCEL 3
#define MBOX_RECONFIG_DATA 8
#define CMD_CASUAL 0
#define CMD_INDIRECT 1
#define MBOX_NO_RESPONSE -1
#define MBOX_TIMEOUT -2
#define BOOT_SCRATCH_POR_1 1
#define SDM_BE_ARADDR_REMAP 2
#define SDM_BE_AWADDR_REMAP 3
#define SOCFPGA_SYSMGR(r) (r)
typedef unsigned __int128 uint128_t;
#ifdef __cplusplus
extern "C" {
#endif
#ifdef FES_TEST_NANO_BRIDGES
void fes_tfa_owned_sip(uint32_t, uint64_t, uint64_t, uint64_t, uint64_t, uint64_t[4]);
unsigned fes_tfa_legacy_routes(void);
#endif
int socfpga_bridges_disable(uint32_t);
int socfpga_bridges_enable(uint32_t);
uint32_t mmio_read_32(uintptr_t);
void mmio_write_32(uintptr_t,uint32_t);
int mailbox_send_cmd(int,int,const uint32_t*,unsigned int,int,uint32_t*,unsigned int*);
int mailbox_send_cmd_async(uint32_t*,int,const uint32_t*,unsigned int,int);
int mailbox_read_response(uint32_t*,uint32_t*,unsigned int*);
void mailbox_clear_response(void);
#ifdef FES_TEST_TERASIC_TFA
int intel_mailbox_get_config_status(uint32_t,bool,uint32_t*);
int mailbox_send_fpga_config_comp(void);
#else
uint32_t intel_mailbox_get_config_status(int,bool);
#endif
void intel_smmu_hps_remapper_init(uint64_t*);
void fes_tfa_reset(void);
void fes_tfa_query(uint64_t result[4]);
int fes_tfa_start(unsigned int);
int fes_tfa_write(uint64_t,uint64_t);
int fes_tfa_completed(uint32_t addresses[3]);
int fes_tfa_isdone(void);
int fes_tfa_isdone_detail(uint32_t*);
uint64_t fes_tfa_translate(uint64_t);
#ifdef __cplusplus
}
#endif
