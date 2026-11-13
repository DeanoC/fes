/*
 * Copyright (c) 2019-2023, ARM Limited and Contributors. All rights reserved.
 * Copyright (c) 2019-2023, Intel Corporation. All rights reserved.
 * Copyright (c) 2024, Altera Corporation. All rights reserved.
 *
 * SPDX-License-Identifier: BSD-3-Clause
 */
int mailbox_hps_stage_notify(unsigned int);
/* Actual BSD vendor functions; only MMIO, locks and delay/log primitives injected. */
#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#define SIP_SVC_V3 1
#define BIT(n) (1U<<(n))
#define U(n) n##U
#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))
#define MBOX_OFFSET 0x10000000U
#define INFO(...) ((void)0)
#define VERBOSE(...) ((void)0)
#define ERROR(...) ((void)0)
#define EXTRACTED_FUNCTION_COUNT 19U
#define MBOX_ATF_CLIENT_ID				0x1U
#define MBOX_MAX_JOB_ID					0xFU
#define MBOX_MAX_IND_JOB_ID				(MBOX_MAX_JOB_ID - 1U)
#define MBOX_JOB_ID					MBOX_MAX_JOB_ID
#define MBOX_TEST_BIT					BIT(31)
#define MBOX_CIN					0x00 /* Command valid offset, to SDM */
#define MBOX_ROUT					0x04 /* Response output offset, to SDM */
#define MBOX_URG					0x08 /* Urgent command, to SDM */
#define MBOX_INT					0x0C /* Interrupt enables, to SDM */
#define MBOX_COUT					0x20 /* Command free offset, from SDM */
#define MBOX_RIN					0x24 /* Response valid offset, from SDM */
#define MBOX_STATUS					0x2C /* Mailbox status from SDM to client */
#define MBOX_CMD_BUFFER					0x40 /* Circular buffer, cmds to SDM */
#define MBOX_RESP_BUFFER				0xC0 /* Circular buffer, resp from SDM */
#define MBOX_DOORBELL_TO_SDM				0x400 /* Doorbell from HPS to SDM */
#define MBOX_DOORBELL_FROM_SDM				0x480 /* Doorbell from SDM to HPS */
#define MBOX_CMD_NOOP					0x00
#define MBOX_CMD_SYNC					0x01
#define MBOX_CMD_RESTART				0x02
#define MBOX_CMD_CANCEL					0x03
#define MBOX_CMD_VAB_SRC_CERT				0x0B
#define MBOX_CMD_GET_IDCODE				0x10
#define MBOX_CMD_GET_USERCODE				0x13
#define MBOX_CMD_GET_CHIPID				0x12
#define MBOX_CMD_FPGA_CONFIG_COMP			0x45
#define MBOX_CMD_REBOOT_HPS				0x47
#define MBOX_CONFIG_STATUS				0x04
#define MBOX_RECONFIG					0x06
#define MBOX_RECONFIG_DATA				0x08
#define MBOX_RECONFIG_STATUS				0x09
#define MBOX_HWMON_READVOLT				0x18
#define MBOX_HWMON_READTEMP				0x19
#define MBOX_CMD_QSPI_OPEN				0x32
#define MBOX_CMD_QSPI_CLOSE				0x33
#define MBOX_CMD_QSPI_SET_CS				0x34
#define MBOX_CMD_QSPI_ERASE				0x38
#define MBOX_CMD_QSPI_WRITE				0x39
#define MBOX_CMD_QSPI_READ				0x3A
#define MBOX_CMD_QSPI_DIRECT				0x3B
#define MBOX_CMD_QSPI_GET_DEV_INFO			0x74
#define MBOX_CMD_SEU_ERR_READ				0x3C
#define MBOX_CMD_SAFE_INJECT_SEU_ERR			0x41
#define MBOX_GET_SUBPARTITION_TABLE			0x5A
#define MBOX_RSU_STATUS					0x5B
#define MBOX_RSU_UPDATE					0x5C
#define MBOX_HPS_STAGE_NOTIFY				0x5D
#define MBOX_RSU_GET_DEVICE_INFO			0x74
#define MBOX_FCS_GET_PROVISION				0x7B
#define MBOX_FCS_CNTR_SET_PREAUTH			0x7C
#define MBOX_FCS_ENCRYPT_REQ				0x7E
#define MBOX_FCS_DECRYPT_REQ				0x7F
#define MBOX_FCS_RANDOM_GEN				0x80
#define MBOX_FCS_AES_CRYPT_REQ				0x81
#define MBOX_FCS_GET_DIGEST_REQ				0x82
#define MBOX_FCS_MAC_VERIFY_REQ				0x83
#define MBOX_FCS_ECDSA_HASH_SIGN_REQ			0x84
#define MBOX_FCS_ECDSA_SHA2_DATA_SIGN_REQ		0x85
#define MBOX_FCS_ECDSA_HASH_SIG_VERIFY			0x86
#define MBOX_FCS_ECDSA_SHA2_DATA_SIGN_VERIFY		0x87
#define MBOX_FCS_ECDSA_GET_PUBKEY			0x88
#define MBOX_FCS_ECDH_REQUEST				0x89
#define MBOX_FCS_HKDF_REQUEST				0x8B
#define MBOX_FCS_OPEN_CS_SESSION			0xA0
#define MBOX_FCS_CLOSE_CS_SESSION			0xA1
#define MBOX_FCS_IMPORT_CS_KEY				0xA5
#define MBOX_FCS_EXPORT_CS_KEY				0xA6
#define MBOX_FCS_REMOVE_CS_KEY				0xA7
#define MBOX_FCS_GET_CS_KEY_INFO			0xA8
#define MBOX_FCS_CREATE_CS_KEY				0xA9
#define MBOX_PSG_SIGMA_TEARDOWN				0xD5
#define MBOX_CREATE_CERT_ON_RELOAD			0x180
#define MBOX_GET_ATTESTATION_CERT			0x181
#define MBOX_ATTESTATION_SUBKEY				0x182
#define MBOX_GET_MEASUREMENT				0x183
#define MBOX_CMD_MCTP_MSG				0x194
#define MBOX_GET_ROM_PATCH_SHA384			0x1B0
#define MBOX_CMD_GET_DEVICEID				0x500
#define CMD_DIRECT					0
#define CMD_INDIRECT					1
#define CMD_CASUAL					0
#define CMD_URGENT					1
#define MBOX_CMD_FLAG_DIRECT				BIT(0)
#define MBOX_CMD_FLAG_INDIRECT				BIT(1)
#define MBOX_CMD_FLAG_CASUAL				BIT(2)
#define MBOX_CMD_FLAG_URGENT				BIT(3)
#define MBOX_CMD_FLAG_CASUAL_INDIRECT			(MBOX_CMD_FLAG_CASUAL | \
							 MBOX_CMD_FLAG_INDIRECT)
#define IS_CMD_SET(cmd, _type)				((((cmd) & MBOX_CMD_FLAG_##_type) != 0) ? \
								1 : 0)
#define MBOX_WORD_BYTE					4U
#define MBOX_RESP_BUFFER_SIZE				16
#define MBOX_CMD_BUFFER_SIZE				32
#define MBOX_INC_HEADER_MAX_WORD_SIZE			1024U
#define HPS_EXECUTION_STATE_FSBL			0
#define HPS_EXECUTION_STATE_SSBL			1
#define HPS_EXECUTION_STATE_OS				2
#define MBOX_RET_OK					0
#define MBOX_RET_ERROR					-1
#define MBOX_NO_RESPONSE				-2
#define MBOX_WRONG_ID					-3
#define MBOX_BUFFER_FULL				-4
#define MBOX_BUSY					-5
#define MBOX_TIMEOUT					-2047
#define MBOX_RET_SDOS_DECRYPTION_ERROR_102		-258
#define MBOX_RET_SDOS_DECRYPTION_ERROR_103		-259
#define RECONFIG_STATUS_STATE				0
#define RECONFIG_STATUS_PIN_STATUS			2
#define RECONFIG_STATUS_SOFTFUNC_STATUS			3
#define PIN_STATUS_NSTATUS				(U(1) << 31)
#define SOFTFUNC_STATUS_SEU_ERROR			(1 << 3)
#define SOFTFUNC_STATUS_INIT_DONE			(1 << 1)
#define SOFTFUNC_STATUS_CONF_DONE			(1 << 0)
#define MBOX_CFGSTAT_STATE_IDLE				0x00000000
#define MBOX_CFGSTAT_STATE_CONFIG			0x10000000
#define MBOX_CFGSTAT_VAB_BS_PREAUTH			0x20000000
#define MBOX_CFGSTAT_STATE_FAILACK			0x08000000
#define MBOX_CFGSTAT_STATE_ERROR_INVALID		0xf0000001
#define MBOX_CFGSTAT_STATE_ERROR_CORRUPT		0xf0000002
#define MBOX_CFGSTAT_STATE_ERROR_AUTH			0xf0000003
#define MBOX_CFGSTAT_STATE_ERROR_CORE_IO		0xf0000004
#define MBOX_CFGSTAT_STATE_ERROR_HARDWARE		0xf0000005
#define MBOX_CFGSTAT_STATE_ERROR_FAKE			0xf0000006
#define MBOX_CFGSTAT_STATE_ERROR_BOOT_INFO		0xf0000007
#define MBOX_CFGSTAT_STATE_ERROR_QSPI_ERROR		0xf0000008
#define MBOX_ENTRY_TO_ADDR(_buf, ptr)			(MBOX_OFFSET + (MBOX_##_buf##_BUFFER) \
								+ MBOX_WORD_BYTE * (ptr))
#define MBOX_INT_FLAG_COE				BIT(0) /* COUT update interrupt enable */
#define MBOX_INT_FLAG_RIE				BIT(1) /* RIN update interrupt enable */
#define MBOX_INT_FLAG_UAE				BIT(8) /* Urgent ACK interrupt enable */
#define MBOX_COE_BIT(INTERRUPT)				((INTERRUPT) & MBOX_INT_FLAG_COE)
#define MBOX_RIE_BIT(INTERRUPT)				((INTERRUPT) & MBOX_INT_FLAG_RIE)
#define MBOX_UAE_BIT(INTERRUPT)				((INTERRUPT) & MBOX_INT_FLAG_UAE)
#define MBOX_RESP_ERR(BUFFER)				((BUFFER) & 0x000007ff)
#define MBOX_RESP_LEN(BUFFER)				(((BUFFER) & 0x007ff000) >> 12)
#define MBOX_RESP_CLIENT_ID(BUFFER)			(((BUFFER) & 0xf0000000) >> 28)
#define MBOX_RESP_JOB_ID(BUFFER)			(((BUFFER) & 0x0f000000) >> 24)
#define MBOX_RESP_TRANSACTION_ID(BUFFER)		(((BUFFER) & 0xff000000) >> 24)
#define MBOX_STATUS_UA_MASK				(1<<8)
#define MBOX_CLIENT_ID_CMD(CLIENT_ID)			((CLIENT_ID) << 28)
#define MBOX_JOB_ID_CMD(JOB_ID)				(JOB_ID << 24)
#define MBOX_CMD_LEN_CMD(CMD_LEN)			((CMD_LEN) << 12)
#define MBOX_INDIRECT(val)				((val) << 11)
#define MBOX_CMD_MASK(header)				((header) & 0x7ff)
#define MBOX_DATA_MAX_LEN				0x3ff
#define MBOX_PAYLOAD_FLAG_BUSY				BIT(0)
#define RSU_VERSION_ACMF				BIT(8)
#define RSU_VERSION_ACMF_MASK				0xff00
#define CONFIG_STATUS_WORD_SIZE				16U
#define CONFIG_STATUS_FW_VER_OFFSET			1
#define CONFIG_STATUS_FW_VER_MASK			0x00FFFFFF
#define MBOX_QSPI_SET_CS_OFFSET				(28)
#define MBOX_QSPI_SET_CS_MODE_OFFSET			(27)
#define MBOX_QSPI_SET_CS_CA_OFFSET			(26)
#define MBOX_QSPI_ERASE_SIZE_GRAN			(0x400)
#define MBOX_4K_ALIGNED_MASK				(0xFFF)
#define MBOX_IS_4K_ALIGNED(x)				((x) & MBOX_4K_ALIGNED_MASK)
#define MBOX_IS_WORD_ALIGNED(x)				(!((x) & 0x3))
#define MBOX_QSPI_RW_MAX_WORDS				(0x1000)
#define MBOX_CLIENT_ID_SHIFT				(28)
#define MBOX_JOB_ID_SHIFT				(24)
#define MBOX_CMD_LEN_SHIFT				(12)
#define MBOX_INDIRECT_SHIFT				(11)
#define MBOX_FRAME_CMD_HEADER(client_id, job_id, args_len, indirect, cmd)\
				((client_id << MBOX_CLIENT_ID_SHIFT) |	 \
				(job_id << MBOX_JOB_ID_SHIFT) |		 \
				(args_len << MBOX_CMD_LEN_SHIFT) |	 \
				(indirect << MBOX_INDIRECT_SHIFT) |	 \
				cmd)
#define FLAG_SDM_RESPONSE_IS_VALID			BIT(0)
#define FLAG_SDM_RESPONSE_IS_USED			BIT(1)
#define FLAG_SDM_RESPONSE_IS_IN_PROGRESS		BIT(2)
#define FLAG_SDM_RESPONSE_IS_POLL_ON_INTR		BIT(3)
#define MBOX_SVC_CMD_QUEUE_SIZE				(32)
#define MBOX_SVC_RESP_QUEUE_SIZE			(32)
#define MBOX_SVC_MAX_JOB_ID				(16)
#define MBOX_SVC_CMD_ARG_SIZE				(2)
#define MBOX_SVC_CMD_IS_USED				BIT(0)
#define MBOX_SVC_CMD_CB_ARGS_SIZE			(4)
#define MBOX_SVC_MAX_CLIENTS				(16)
#define MBOX_SVC_MAX_RESP_DATA_SIZE			(32)
#define MBOX_SVC_SMC_RET_MAX_SIZE			(8)
#define MBOX_MAX_TIDS					(256)
#define MBOX_TID_BITMAP_SIZE				(sizeof(uint64_t) * 8)
#define MBOX_MAX_TIDS_BITMAP				(MBOX_MAX_TIDS / MBOX_TID_BITMAP_SIZE)
#define MBOX_GET_TRANS_ID(cid, jib)			(((cid) << 4) | (jib))
typedef enum sdm_resp_state {
	SRS_WAIT_FOR_RESP = 0x00U,
	SRS_WAIT_FOR_HEADER,
	SRS_WAIT_FOR_ARGUMENTS,
	SRS_SYNC_ERROR
} sdm_resp_state_t;

/* SDM response data structure */
typedef struct sdm_response {
	bool is_poll_intr;
	uint8_t client_id;
	uint8_t job_id;
	uint16_t resp_len;
	uint16_t err_code;
	uint32_t flags;
	uint32_t header;
	uint16_t rcvd_resp_len;
	uint32_t resp_data[MBOX_SVC_MAX_RESP_DATA_SIZE];
} sdm_response_t;

/* SDM client callback template */
typedef uint8_t (*sdm_command_callback)(void *resp, void *cmd,
					uint32_t *ret_args);

/* SDM command data structure */
typedef struct sdm_command {
	uint8_t client_id;
	uint8_t job_id;
	uint32_t flags;
	sdm_command_callback cb;
	uint32_t *cb_args;
	uint8_t cb_args_len;
	/* Capacity in uint32_t words, independent of callback metadata width. */
	uint32_t response_capacity;
} sdm_command_t;

/* Get the transcation ID from client ID and job ID. */
#define MBOX_GET_TRANS_ID(cid, jib)			(((cid) << 4) | (jib))

/* Mailbox service data structure */
typedef struct mailbox_service {
	sdm_resp_state_t resp_state;
	sdm_resp_state_t next_resp_state;
	uint32_t flags;
	int curr_di;
	uint64_t received_bitmap[MBOX_MAX_TIDS_BITMAP];
	uint64_t interrupt_bitmap[MBOX_MAX_TIDS_BITMAP];
	sdm_command_t cmd_queue[MBOX_SVC_CMD_QUEUE_SIZE];
	sdm_response_t resp_queue[MBOX_SVC_RESP_QUEUE_SIZE];
} mailbox_service_t;


/* SPDX-License-Identifier: BSD-3-Clause */
#define main old_shared_boot_main
#include "nano_usb_shared_boot_test.c"
#undef main
#include <setjmp.h>
#include "nano_configuration.h"
#include "nano_program.h"
#include "nano_fabric_stop.h"
#include "reader_profile_fixture.h"
struct fes_nano_mac fes_nano_owned_mac;
static struct fes_nano_configuration configuration;
static struct fes_nano_program configuration_program;
static struct fes_nano_fabric_stop fabric_stop;
static uint64_t fabric_configuration_generation,fabric_generation;
static unsigned mode,smmu_reads,smmu_writes,slots,parks;
static uint64_t ticks,arena[8192];
static uint32_t registers[64],route_remap;
static unsigned retire_fault,retire_armed;
static unsigned source_hash_checks,hash_failure,source_hash_delay,live_mutation,mutation_at,live_owner_checks;
static uint32_t live_bad_register=UINT32_MAX;
static void mutate_live(void);
static unsigned mutated,cb_stores,cb_loads;
static void mutate_on_memory(void);
static uint64_t counter(void);
static jmp_buf terminal;
#define FES_NANO_SDM_SOURCE_LEASE 1
#define FES_NANO_PROBE_HEAP_FIRST 0x84000000ULL
#define FES_NANO_PROBE_HEAP_LAST 0x94000000ULL
#define FES_NANO_CONFIG_BASE 0x94800000ULL
#define FES_NANO_CONFIG_ARENA_SIZE (8U<<20)
int fes_nano_mmu_platform_scope(void){return 1;}
static uint64_t address_of(const char *name){
 if(!strcmp(name,"__FES_SMMU_PROBE_START__") || !strcmp(name,"__FES_SMMU_STREAMS__"))return 0x94000000;
 if(!strcmp(name,"__FES_SMMU_PROBE_END__"))return 0x94010000;
 if(!strcmp(name,"__FES_SMMU_SDM_ROOT__"))return 0x94004000;
 if(!strcmp(name,"__FES_SMMU_CMDQ__"))return 0x94008000;
 if(!strcmp(name,"__FES_SMMU_EVTQ__"))return 0x94009000;
 assert(!strcmp(name,"fes_nano_mmu_ddr"));return 0x80050000;
}
static uint64_t table_word(const char *name,unsigned index){
 if(!strcmp(name,"fes_nano_mmu_root")){assert(index==2);return 0x80050003;}
 assert(!strcmp(name,"fes_nano_mmu_ddr") && (index==160 || (index>=164 && index<=167)));
 return (UINT64_C(0x0040000080000669)+((uint64_t)index<<21)) ^ ((mode==28 || (mode==34 && mutated)) && index==164 ? 4U:0U);
}
static unsigned smmu_index(uint64_t address){
 assert(address>=0x16000000 && address<=0x160100ac);unsigned offset=(unsigned)(address-0x16000000);
 if(offset==0x8004)return 48;
 const unsigned secure[]={0x8020,0x8024,0x803c,0x8044,0x8048};
 for(unsigned n=0;n<5;n++)if(offset==secure[n])return 49+n;
 if(offset==0x100a8)return 44;if(offset==0x100ac)return 45;assert(offset<=0xb4 && !(offset&3));return offset/4;
}
static uint32_t probe_read(uint64_t address){
 if(address==0x10d12184)return 3;if(address==0x10d121b0)return 0xa000a;
 if(address==0x10d1225c)return 0;
 if(address==0x10d12280 || address==0x10d12284)return route_remap;
 unsigned index=smmu_index(address);smmu_reads++;
 if(retire_armed && address==0x16000044){
  if(retire_fault==2)return 0;
  if(retire_fault!=1)registers[index]&=~0x80000000U;
 }
 if(retire_armed && address==0x16000024){
  if(retire_fault==3)return 13;
  if(retire_fault==4)return 1;
 }
 if(retire_armed && address==0x1600009c && registers[0x98/4]==12 && slots<12){
  if(retire_fault!=7)registers[index]=++slots;
  if(retire_fault==8 && slots==9)registers[index]=7;
 }
 if(mode==13 && smmu_reads==1)model.base.h.usb[8/4]|=1U<<5;
 if(address==0x1600009c && registers[0x98/4] && slots<6)registers[index]=++slots;
 return registers[index];
}
static void probe_write(uint64_t address,uint32_t value){
 unsigned index=smmu_index(address);smmu_writes++;registers[index]=value;
 if(address==0x16000020)registers[0x24/4]=value;
 if(address==0x16000044){
  assert(value==0x80100000);retire_armed=1;
  if(retire_fault==5)registers[44]=1;
  if(retire_fault==6)registers[0x60/4]=1;
  if(retire_fault==13)arena[0]^=1;
  if(retire_fault==14)ticks+=20000000;
 }
 if(retire_armed && address==0x16000098 && retire_fault==9)configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;
 if(mode==14 && smmu_writes==1)model.base.h.usb[8/4]|=1U<<5;
}
static uint64_t load_word(uint64_t address){cb_loads++;assert(address>=0x94000000 && address<0x94010000 && !(address&7));return arena[(address-0x94000000)/8];}
static void store_word(uint64_t address,uint64_t value){if(retire_armed && retire_fault==12 && !mutated){mutated=1;configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;}cb_stores++;assert(address>=0x94000000 && address<0x94010000 && !(address&7));arena[(address-0x94000000)/8]=value;mutate_on_memory();}
static void park(void){parks++;longjmp(terminal,1);}
#define FES_PROBE_CPU() 1
#define FES_PROBE_FREQUENCY() 400000000U
#define FES_PROBE_COUNTER() counter()
#define FES_PROBE_SYMBOL(name) address_of(#name)
#define FES_PROBE_TABLE(name,index) table_word(#name,index)
#ifndef FES_TEST_RETIRE_METER
#define FES_TEST_RETIRE_METER(kind) ((void)0)
#endif
#ifndef FES_TEST_RETIRE_COST_MODE
#define FES_TEST_RETIRE_COST_MODE 0
#endif
#define FES_PROBE_READ32(address) (FES_TEST_RETIRE_METER(0),probe_read(address))
#define FES_PROBE_WRITE32(address,value) (FES_TEST_RETIRE_METER(1),probe_write(address,value))
#define FES_PROBE_LOAD64(address) (FES_TEST_RETIRE_METER(2),load_word(address))
#define FES_PROBE_STORE64(address,value) (FES_TEST_RETIRE_METER(3),store_word(address,value))
#define FES_PROBE_BARRIER() FES_TEST_RETIRE_METER(4)
#define FES_PROBE_WAIT() park()
#undef NOTICE
#define NOTICE(...) ((void)0)
static int configuration_check(void *c,uint64_t address,uint32_t bytes,unsigned whole){
 assert(c==&provider && whole<=1 && address>=FES_NANO_CONFIG_BASE && bytes && address-FES_NANO_CONFIG_BASE+bytes<=configuration.bytes);
 if(whole)assert(address==FES_NANO_CONFIG_BASE && bytes==configuration.bytes);
 source_hash_checks++;ticks+=source_hash_delay;return hash_failure ? -1:0;
}
#define FES_NANO_INPUT_BUILD "0000000000000000"
#include <stdarg.h>
static char terminal_log[4096];static unsigned terminal_used;
static void terminal_notice(const char *format,...){
 va_list args;va_start(args,format);int n=vsnprintf(terminal_log+terminal_used,sizeof(terminal_log)-terminal_used,format,args);va_end(args);
 assert(n>=0 && (unsigned)n<sizeof(terminal_log)-terminal_used);terminal_used+=(unsigned)n;
}
#undef NOTICE
#define NOTICE(...) terminal_notice(__VA_ARGS__)
#include "nano_sdm_source_platform.inc"
static uint64_t counter(void){
 FES_TEST_RETIRE_METER(5);
 if(retire_armed && source_retirement.phase==RET_COMPLETE){
  if(retire_fault==10)registers[44]=1;
  if(retire_fault==11)registers[0x20/4]=13;
 }
 mutate_live();if(mode==33 && sdm_arm_probe.command.submitted && !sdm_arm_probe.command.timed)return 0;return ++ticks*400;}
static int foreign_source(void *c,const struct nano_sdm_input_binding *b){(void)c;(void)b;assert(0);return 1;}
static int foreign_load(void *c,uint32_t o,uint64_t *v,unsigned n){(void)c;(void)o;(void)v;(void)n;assert(0);return 0;}
static int foreign_store(void *c,uint32_t o,const uint64_t *v,unsigned n){(void)c;(void)o;(void)v;(void)n;assert(0);return 0;}
static void mutate_on_memory(void){
 if(mutated || mode<15 || mode==28 || mode==29 || mode==30 || mode==31 || mode==32 || mode==33)return;
 mutated=1;
 switch(mode){case 15:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
 case 16:configuration_program.bytes+=4;break;case 17:configuration_program.chunk+=64;break;
 case 18:configuration_program.owner++;break;case 19:configuration_program.generation++;break;
 case 20:configuration_program.bound=0;break;case 21:configuration_program.role=2;break;
 case 22:sdm_arm_probe.input.digest384[0]^=1;break;case 23:sdm_arm_probe.input.bytes+=4;break;
 case 24:sdm_arm_probe.input_scope=foreign_source;break;case 25:sdm_arm_probe.io.load_block=foreign_load;break;
 case 26:sdm_arm_probe.io.store_block=foreign_store;break;case 27:sdm_arm_probe.input_selected=0;break;
 case 34:break;case 35:sdm_arm_probe.io.context=&model;break;default:assert(0);}
}
static int forged_contained(void *p,struct fes_nano_cold *c,uint64_t owner,uint64_t epoch)
{(void)p;(void)c;(void)owner;(void)epoch;assert(0);return 1;}

static void source_setup(unsigned selected){mode=selected;
 setup(&model);model.base.h.usb_owner=&usb_platform;memset(&model.base.bridges,0,sizeof(model.base.bridges));
 model.base.h.producer_bits=FES_NANO_PRODUCERS_HPS;
 struct fes_nano_boot_policy policy={.qualifications=FES_NANO_BOOT_QUALIFIED,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
 memcpy(policy.chain,provider.record.boot.chain,32);
 struct fes_nano_master_ops masters_ops={&provider,cold_master_owns,cold_master_quiesce,cold_master_quiescent,master_per0,master_assert,barrier,barrier};
 struct fes_nano_bridge_ops raw={.context=&model.base,.read_reset=bridge_read,.enable=no_bridge,.disable=no_bridge};
 assert(!fes_nano_boot_accept(&model.base.master_boot,&model.base.bridges,&provider.record.boot,&policy,masters_ops,raw));
 assert(!initialize(&model.base));assert(!fes_nano_sd_init(&model.base.service,&model.base.bridges,fes_nano_sd_platform_ops(&sd_platform),FES_NANO_SD_QUALIFIED));
 assert(fes_nano_platform_usb_setup()==1);uint64_t r[4];
 fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_ACQUIRE,17,0,0,r);assert(!r[0]);host_started(&model);
 fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_CONFIRM,17,1,0,r);assert(!r[0]);
 assert(usb_platform.active==1 && usb_platform.confirmed==1 && usb_platform.generation==2);
 assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
 assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;assert(!call(&model.base,FES_EMBEDDED_SD_STOP));
 assert(!fes_nano_cold_owns(&provider,18,provider.record.boot.epoch));assert(fes_nano_cold_owns(&provider,17,provider.record.boot.epoch)==1);
 if(mode==1){fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_STOP,17,2,0,r);assert(!r[0]);}
 assert(fes_nano_owned_bridges.state.never_enabled && fes_nano_owned_bridges.state.reset==15);
 uint64_t generation=fes_nano_owned_bridges.state.generation;
 uint32_t bytes=mode==31 ? NANO_SDM_INPUT_MAX:mode==32 ? 1708036U:1708032U;
 configuration=(struct fes_nano_configuration){.bridges=&fes_nano_owned_bridges,.owner=17,.generation=generation,.bytes=bytes,
 .maximum=8388608,.chunk=1048512,.base=0x94800000,.initialized=1,.prepared=1,.started=1,.issued=FES_NANO_CONFIG_START,.stage=FES_CONFIG_START};
 configuration_program=(struct fes_nano_program){.owner=17,.generation=generation,.bytes=bytes,.chunk=1048512,.initialized=1,.bound=1,
 .mixed_roles=1,.role=FES_NANO_PROGRAM_ROLE_MENU,.control_abi=FES_NANO_PROGRAM_MENU_ABI};
 for(unsigned n=0;n<48;n++)configuration_program.header[FES_NANO_PROGRAM_SHA384+n]=(uint8_t)(n+1);
 if(mode==29)configuration.bytes=configuration_program.bytes=NANO_SDM_INPUT_MAX+4;
 if(mode==30)memset(configuration_program.header+FES_NANO_PROGRAM_SHA384,0,48);
 registers[0]=0x080f7e3f;registers[1]=0x0e739d18;registers[5]=0x400075;registers[0x2c/4]=4;registers[7]=1;registers[48]=0x80000018;
 unsigned usb_writes=model.base.h.usb_writes,physical_accesses=model.accesses,phy_requests=model.requests;
 if(mode==2)model.base.h.usb[8/4]|=1U<<5;
 if(mode==3)model.base.h.usb[24/4]=1;
 if(mode==4)model.base.h.usb[16/4]=0;
 if(mode==5)usb_platform.generation++;
 if(mode==6)provider.record.boot.owner++;
 if(mode==7)sd_platform.active=1;
 if(mode==8)model.pins[0]^=1;
 if(mode==9)usb_platform.io.contained_scope=forged_contained;
 if(mode==10)usb_platform.confirmed=0;
 if(mode==11)fes_nano_owned_bridges.device_lease=1;
 if(mode==12)model.clocks[0]=1;

}
/* SPDX-License-Identifier: BSD-3-Clause */

static void mutate_live(void){
 if(!live_mutation || ++live_owner_checks!=mutation_at)return;
 if(live_mutation==1)configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;
 if(live_mutation==2)usb_platform.generation++;
 if(live_mutation==3)registers[0x100a8==0 ? 0:44]=1;
}
/* SPDX-License-Identifier: BSD-3-Clause */
#include <limits.h>
#include <unistd.h>
#include <sys/wait.h>

#define PLATFORM_MODEL 5
#define PLAT_SOCFPGA_AGILEX5 5
#define FPGA_CONFIG_BUFFER_SIZE 4
#define FLAG_PARTIAL_CONFIG BIT(0)
#define FLAG_AUTHENTICATION BIT(1)
#define CONFIG_TEST_FLAG(_flag,_type) (((_flag)&FLAG_##_type)==FLAG_##_type)
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define DRAM_BASE UINT64_C(0x80000000)
#define DRAM_SIZE UINT64_C(0x80000000)
#define BL31_LIMIT UINT64_C(0x94000000)
typedef __uint128_t uint128_t;
typedef enum {NO_REQUEST,RECONFIGURATION,BITSTREAM_AUTH} config_type;
struct fpga_config_info {uint32_t addr;int size,size_written;uint32_t write_requested;int subblocks_sent,block_number;};
static config_type request_type=NO_REQUEST;
static int current_block,current_buffer,read_block,max_blocks;
static uint32_t send_id,bytes_per_block,blocks_submitted;
static bool bridge_disable;
static struct fpga_config_info fpga_config_buffers[4];
static int fes_contained_transport_status;
static unsigned fes_contained_transport_phase;
typedef unsigned spinlock_t;
static spinlock_t mbox_db_lock,mbox_read_lock,mbox_write_lock;
static void spin_lock(spinlock_t *p){assert(!*p);*p=1;}
static void spin_unlock(spinlock_t *p){assert(*p);*p=0;}
static mailbox_service_t mbox_svc;
static uint8_t async_v1_job_id;
static int mailbox_resp_ctr;
struct fes_nano_isolation_observation {unsigned active,polling,bell,polls,branch,cin,cout;int fill;uint32_t header;};
static struct fes_nano_isolation_observation fes_nano_isolation_io;
static uint32_t stream[4096],is_header[4096],stream_words,consumed,rout,reads,payload_reads,writes,delays;
static uint32_t command[4],command_words,cin,word_calls,fail_word;
static uint32_t pending[14],pending_count,data_posts,control_posts,sdm_bytes;
static uint32_t fixture_credits=4,fixture_block=0xffff0,fixture_state,fixture_soft,fixture_status_words=6;
static uint32_t fixture_pin,fixture_ack_xor,fixture_ack_words;
static unsigned duplicate_acks,drop_acks,prepare_fails,bridge_fails,bridges_calls,remap_calls,prepared_calls;
static unsigned owners,times,fail_owner,fail_time,back_time,jump_time;
static uint64_t fixture_now=100,fixture_step=1;
static struct nano_sdm_input_binding expected;
static void push(uint32_t word,int header){assert(stream_words<4096);stream[stream_words]=word;is_header[stream_words++]=header;}
static void packet(uint32_t job,uint32_t words){push((1U<<28)|(job<<24)|(words<<12),1);}
static void commit_command(void){
 assert(command_words);uint32_t cmd=MBOX_CMD_MASK(command[0]),job=(command[0]>>24)&15U;
 if(cmd==MBOX_RECONFIG_DATA){
  assert(command_words==4 && command[1]==256 && job<14);
  assert(command[2]==NANO_SDM_TRANSFER_WIRE+sdm_bytes && command[3] && !(command[3]&3));
  assert(command[3]<=fixture_block && sdm_bytes+command[3]<=expected.bytes);
  sdm_bytes+=command[3];data_posts++;assert(pending_count<14);pending[pending_count++]=job;
 }else if(cmd==MBOX_RECONFIG){
  assert(job==15 && command_words==1);control_posts++;packet(job,2);push(fixture_credits,0);push(fixture_block,0);
 }else if(cmd==MBOX_RECONFIG_STATUS || cmd==MBOX_CONFIG_STATUS){
  assert(job==15 && command_words==1);control_posts++;packet(job,fixture_status_words);
  for(unsigned n=0;n<fixture_status_words;n++)push(n==RECONFIG_STATUS_STATE ? fixture_state:n==RECONFIG_STATUS_PIN_STATUS ? fixture_pin:n==RECONFIG_STATUS_SOFTFUNC_STATUS ? fixture_soft:0,0);
 }else if(cmd==MBOX_HPS_STAGE_NOTIFY){
  assert(job==15 && command_words==2 && command[1]==1);control_posts++;packet(job,0);
 }else{
  assert(cmd==MBOX_CMD_FPGA_CONFIG_COMP || cmd==MBOX_CMD_CANCEL);assert(job==15 && command_words==1);
  control_posts++;packet(job,0);
 }
 command_words=0;
}
static uint32_t mmio_read_32(uint64_t address){
 reads++;uint64_t off=address-MBOX_OFFSET;
 if(off==MBOX_CIN || off==MBOX_COUT)return cin;
 if(off==MBOX_RIN){unsigned remaining=stream_words-consumed;return (rout+(remaining>15 ? 15:remaining))%16;}
 if(off==MBOX_ROUT)return rout;
 if(off==MBOX_DOORBELL_FROM_SDM)return consumed<stream_words;
 assert(off>=MBOX_RESP_BUFFER && off<MBOX_RESP_BUFFER+64 && !((off-MBOX_RESP_BUFFER)&3));
 assert((off-MBOX_RESP_BUFFER)/4==rout && consumed<stream_words);
 payload_reads+=!is_header[consumed];return stream[consumed];
}
static void mmio_write_32(uint64_t address,uint32_t value){
 writes++;uint64_t off=address-MBOX_OFFSET;
 if(off==MBOX_ROUT){
  /* The vendor clear operation is allowed only with no outstanding words. */
  if(value==rout){assert(consumed==stream_words);return;}
  assert(value==(rout+1)%16 && consumed<stream_words);rout=value;consumed++;return;
 }
 if(off==MBOX_DOORBELL_TO_SDM){assert(value==1);commit_command();return;}
 assert(off==MBOX_DOORBELL_FROM_SDM && value==0);
}
static int write_mailbox_cmd_buffer(uint32_t *out,uint32_t in,uint32_t data,bool *bell){
 (void)in;(void)bell;word_calls++;if(word_calls==fail_word)return -110;
 assert(command_words<4);command[command_words++]=data;cin=(cin+1)%MBOX_CMD_BUFFER_SIZE;*out=cin;return 0;
}
static void udelay(uint32_t n){assert(n==100);delays++;fixture_now+=n;}
static void mdelay(uint32_t n){assert(n==5 || n==10);delays++;fixture_now+=n*1000U;}
static int socfpga_bridges_disable(uint32_t mask){assert(mask==UINT32_MAX);bridges_calls++;return bridge_fails ? -5:0;}
static void intel_smmu_hps_remapper_init(uint64_t *mem){remap_calls++;route_remap=0x80000000;if(mem){assert(*mem>=NANO_SDM_INPUT_BASE);*mem-=UINT64_C(0x80000000);}}
static unsigned fes_nano_sdm_note(uint32_t address,uint32_t bytes){(void)address;(void)bytes;return 0;}
static void fes_nano_start_note(unsigned phase,int status){(void)phase;(void)status;}

static sdm_command_t *mailbox_get_free_cmd_desc(void);
static int32_t mailbox_get_free_resp_desc(void);
static int mailbox_fill_cmd_desc(uint8_t client_id, uint8_t job_id,
				 uint32_t *resp_buff, uint32_t response_capacity);
static sdm_command_t *mailbox_get_cmd_desc(uint8_t client_id, uint8_t job_id);
static sdm_response_t *mailbox_get_resp_desc(uint8_t client_id, uint8_t job_id, uint8_t *index);
static sdm_response_t *mailbox_get_resp_desc_cid(uint8_t client_id, uint8_t *index);
static inline void mailbox_free_cmd_desc(sdm_command_t *cmd_desc);
static inline void mailbox_free_resp_desc(uint8_t index);
static void mailbox_response_parser(void);
static int mailbox_response_handler_fsm(void);
static int mailbox_read_response_v3_selected(uint8_t client_id, uint8_t *job_id,
				    uint32_t *header, uint32_t *resp,
				    uint32_t *resp_len,
				    uint8_t ignore_client_id, int expected_job);
static int mailbox_read_response_v3(uint8_t client_id, uint8_t *job_id,
    uint32_t *header, uint32_t *resp, uint32_t *resp_len, uint8_t ignore_client_id);
int mailbox_read_response_async_expected(unsigned int expected_job,
    unsigned int *job_id,uint32_t *header,uint32_t *response,unsigned int *resp_len);
int mailbox_read_response_async(unsigned int *job_id, uint32_t *header,
				uint32_t *response, unsigned int *resp_len,
				uint8_t ignore_client_id);
int mailbox_send_cmd_async_v3(uint8_t client_id, uint8_t job_id, uint32_t cmd,
			      uint32_t *args, uint32_t args_len, uint8_t cmd_flag,
			      sdm_command_callback cb, uint32_t *cb_args,
			      uint32_t cb_args_len);
static int mailbox_poll_response_v3(uint8_t client_id, uint8_t job_id,
				    uint32_t *resp, unsigned int *resp_len,
				    uint32_t urgent);
int mailbox_poll_response(uint32_t job_id, uint32_t urgent, uint32_t *response,
			  unsigned int *resp_len);
int mailbox_send_cmd(uint32_t job_id, uint32_t cmd, uint32_t *args,
			unsigned int len, uint32_t urgent, uint32_t *response,
			unsigned int *resp_len);
int intel_mailbox_get_config_status(uint32_t cmd, bool init_done,
				    uint32_t *err_states);
int mailbox_send_cmd_async(uint32_t *job_id, uint32_t cmd, uint32_t *args,
			  unsigned int len, unsigned int indirect);
static int fill_mailbox_circular_buffer(uint32_t header_cmd, uint32_t *args,
					unsigned int len);
void mailbox_clear_response(void);
int mailbox_send_fpga_config_comp(void);
static int fes_contained_transport_failure(unsigned phase, int status);
static uint32_t fes_contained_isdone_detail(uint32_t status, uint32_t detail);
static int intel_fpga_sdm_write_buffer(struct fpga_config_info *buffer);
static int intel_fpga_sdm_write_all(void);
static uint32_t intel_mailbox_fpga_config_isdone_unchecked(uint32_t *err_states);
static uint32_t intel_mailbox_fpga_config_isdone(uint32_t *err_states);
static int mark_last_buffer_xfer_completed(uint32_t *buffer_addr_completed);
static int intel_fpga_config_completed_write(uint32_t *completed_addr,
					uint32_t *count, uint32_t *job_id);
static int intel_fpga_config_start(uint32_t flag);
static bool is_fpga_config_buffer_full(void);
bool is_address_in_ddr_range(uint64_t addr, uint64_t size);
static uint32_t intel_fpga_config_write(uint64_t mem, uint64_t size);
static sdm_command_t *mailbox_get_free_cmd_desc(void)
{
	spin_lock(&mbox_db_lock);
	static uint32_t free_index;

	/* Rollover the command queue free index */
	if (free_index == (MBOX_SVC_CMD_QUEUE_SIZE - 1))
		free_index = 0U;

	for (; free_index < MBOX_SVC_CMD_QUEUE_SIZE; free_index++) {
		if (!(mbox_svc.cmd_queue[free_index].flags & MBOX_SVC_CMD_IS_USED)) {
			mbox_svc.cmd_queue[free_index].flags |= MBOX_SVC_CMD_IS_USED;
			spin_unlock(&mbox_db_lock);
			return &(mbox_svc.cmd_queue[free_index]);
		}
	}

	/* No free command descriptors are available */
	spin_unlock(&mbox_db_lock);
	VERBOSE("MBOX: No free command descriptors available\n");

	return NULL;
}
static int32_t mailbox_get_free_resp_desc(void)
{
	spin_lock(&mbox_db_lock);
	static uint32_t free_index = MBOX_SVC_RESP_QUEUE_SIZE - 1;
	uint32_t count = 0, try = 0;

	for (try = 0; try < MBOX_SVC_RESP_QUEUE_SIZE; try++) {
		free_index = (free_index + 1) % MBOX_SVC_RESP_QUEUE_SIZE;
		if (!(mbox_svc.resp_queue[free_index].flags &
			FLAG_SDM_RESPONSE_IS_USED)) {
			count = free_index;
			spin_unlock(&mbox_db_lock);
			return count;
		}
	}

	/* No free descriptors are available */
	spin_unlock(&mbox_db_lock);
	VERBOSE("MBOX: No free response descriptors are available\n");

	return MBOX_BUFFER_FULL;
}
static int mailbox_fill_cmd_desc(uint8_t client_id, uint8_t job_id,
				 uint32_t *resp_buff, uint32_t response_capacity)
{
	sdm_command_t *cmd_desc = NULL;

	/* Get a free command descriptor */
	cmd_desc = mailbox_get_free_cmd_desc();
	if (cmd_desc == NULL)
		return MBOX_BUFFER_FULL;

	/* Record all the given values for the command. */
	cmd_desc->client_id = client_id;
	cmd_desc->job_id = job_id;
	cmd_desc->cb = NULL;
	cmd_desc->cb_args = resp_buff;
	cmd_desc->cb_args_len = 0U;
	cmd_desc->response_capacity = response_capacity;

	return MBOX_RET_OK;
}
static sdm_command_t *mailbox_get_cmd_desc(uint8_t client_id, uint8_t job_id)
{
	spin_lock(&mbox_db_lock);
	for (uint32_t count = 0; count < MBOX_SVC_CMD_QUEUE_SIZE; count++) {
		if ((mbox_svc.cmd_queue[count].client_id == client_id) &&
		    (mbox_svc.cmd_queue[count].job_id == job_id) &&
		    (mbox_svc.cmd_queue[count].flags & MBOX_SVC_CMD_IS_USED)) {
			spin_unlock(&mbox_db_lock);
			return &(mbox_svc.cmd_queue[count]);
		}
	}

	spin_unlock(&mbox_db_lock);
	VERBOSE("MBOX: Command descriptor not found for cid %d, jid %d\n",
		client_id, job_id);

	return NULL;
}
static sdm_response_t *mailbox_get_resp_desc(uint8_t client_id, uint8_t job_id, uint8_t *index)
{
	spin_lock(&mbox_db_lock);
	/*
	 * Let's first check whether we have a response bitmap set for the given
	 * client ID and job ID.
	 */
	uint8_t transaction_id = MBOX_GET_TRANS_ID(client_id, job_id);

	if ((mbox_svc.received_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] &
		(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE))) == 0) {
		spin_unlock(&mbox_db_lock);
		VERBOSE("MBOX: Response bitmap not set for cid %d, jid %d, bitmap 0x%16lx\n",
			client_id, job_id, mbox_svc.received_bitmap[transaction_id / 64]);
		return NULL;
	}

	for (uint32_t count = 0; count < MBOX_SVC_RESP_QUEUE_SIZE; count++) {
		if (mbox_svc.resp_queue[count].flags & FLAG_SDM_RESPONSE_IS_VALID) {
			if ((mbox_svc.resp_queue[count].client_id == client_id) &&
			    (mbox_svc.resp_queue[count].job_id == job_id)) {
				*index = count;
				mbox_svc.received_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] &=
					~(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE));
				mbox_svc.interrupt_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] &=
					~(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE));
				spin_unlock(&mbox_db_lock);
				return &(mbox_svc.resp_queue[count]);
			}
		}
	}

	spin_unlock(&mbox_db_lock);
	VERBOSE("MBOX: Response descriptor not found for cid %d, jid %d\n",
		client_id, job_id);

	return NULL;
}
static sdm_response_t *mailbox_get_resp_desc_cid(uint8_t client_id, uint8_t *index)
{
	spin_lock(&mbox_db_lock);

	for (uint32_t count = 0; count < MBOX_SVC_RESP_QUEUE_SIZE; count++) {
		if ((mbox_svc.resp_queue[count].client_id == client_id) &&
		    (mbox_svc.resp_queue[count].flags & FLAG_SDM_RESPONSE_IS_VALID)) {
			*index = count;
			/*
			 * Once we get the valid response descriptor, get the
			 * job ID and mark up the bitmaps.
			 */
			uint8_t job_id = mbox_svc.resp_queue[count].job_id;
			uint8_t transaction_id = MBOX_GET_TRANS_ID(client_id, job_id);

			mbox_svc.received_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] &=
				~(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE));
			mbox_svc.interrupt_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] &=
				~(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE));
			spin_unlock(&mbox_db_lock);
			return &(mbox_svc.resp_queue[count]);
		}
	}

	spin_unlock(&mbox_db_lock);
	VERBOSE("MBOX: Response descriptor not found for cid %d\n", client_id);

	return NULL;
}
static inline void mailbox_free_cmd_desc(sdm_command_t *cmd_desc)
{
	if (cmd_desc == NULL)
		return;

	spin_lock(&mbox_db_lock);
	memset((void *)cmd_desc, 0, sizeof(sdm_command_t));
	spin_unlock(&mbox_db_lock);
}
static inline void mailbox_free_resp_desc(uint8_t index)
{
	if (index >= MBOX_SVC_RESP_QUEUE_SIZE)
		return;

	spin_lock(&mbox_db_lock);
	memset((void *)&mbox_svc.resp_queue[index], 0, sizeof(sdm_response_t));
	spin_unlock(&mbox_db_lock);
}
static void mailbox_response_parser(void)
{
	int di = -1;		/* Descriptor index */
	uint32_t rin;
	uint32_t rout;

	switch (mbox_svc.next_resp_state) {
	case SRS_WAIT_FOR_RESP:
	{
		mbox_svc.resp_state = SRS_WAIT_FOR_RESP;

		rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
		rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
		if (rin != rout)
			mbox_svc.next_resp_state = SRS_WAIT_FOR_HEADER;

		break;
	}

	case SRS_WAIT_FOR_HEADER:
	{
		mbox_svc.resp_state = SRS_WAIT_FOR_HEADER;
		uint32_t resp_hdr;
		uint8_t trans_id = 0U;

		rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
		rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
		if (rin != rout) {
			/* Read the header and dequeue from the queue. */
			resp_hdr = mmio_read_32(MBOX_ENTRY_TO_ADDR(RESP, (rout)++));
			/* Strong owned transfer gate: peek before consuming the header or
			 * reading a payload. Failure retains the FIFO and all buffers. */
			if (fes_nano_sdm_transfer_peek_header(resp_hdr)) {
				mbox_svc.next_resp_state=SRS_SYNC_ERROR;
				break;
			}
			rout %= MBOX_RESP_BUFFER_SIZE;
			mmio_write_32(MBOX_OFFSET + MBOX_ROUT, rout);

			/* Allocate a new response descriptor */
			di = mailbox_get_free_resp_desc();
			if (di != -1) {
				mbox_svc.curr_di = di;
				mbox_svc.resp_queue[di].header = resp_hdr;
				mbox_svc.resp_queue[di].client_id = MBOX_RESP_CLIENT_ID(resp_hdr);
				mbox_svc.resp_queue[di].job_id = MBOX_RESP_JOB_ID(resp_hdr);
				mbox_svc.resp_queue[di].resp_len = MBOX_RESP_LEN(resp_hdr);
				mbox_svc.resp_queue[di].flags |= (FLAG_SDM_RESPONSE_IS_USED |
								  FLAG_SDM_RESPONSE_IS_IN_PROGRESS);
				mbox_svc.resp_queue[di].err_code = MBOX_RESP_ERR(resp_hdr);
				trans_id = MBOX_RESP_TRANSACTION_ID(resp_hdr);

				VERBOSE("MBOX: Resp Hdr: cid %d, jid %d, len %d, err_code 0x%x\n",
					mbox_svc.resp_queue[di].client_id,
					mbox_svc.resp_queue[di].job_id,
					mbox_svc.resp_queue[di].resp_len,
					mbox_svc.resp_queue[di].err_code);

				/* Check if the response is an argument response */
				if (mbox_svc.resp_queue[di].resp_len > 0) {
					mbox_svc.next_resp_state = SRS_WAIT_FOR_ARGUMENTS;
				} else {
					VERBOSE("MBOX: Received complete response with no args\n");
					/* Non argument response, done */
					mbox_svc.resp_queue[mbox_svc.curr_di].flags |=
									FLAG_SDM_RESPONSE_IS_VALID;

					/* Go back to waiting for new response */
					mbox_svc.next_resp_state = SRS_WAIT_FOR_RESP;
					mbox_svc.curr_di = -1;

					/* Mark the transaction ID as received */
					spin_lock(&mbox_db_lock);
					mbox_svc.received_bitmap[trans_id / MBOX_TID_BITMAP_SIZE] |=
							(1ULL << (trans_id % MBOX_TID_BITMAP_SIZE));
					spin_unlock(&mbox_db_lock);
				}
			} else {
				mbox_svc.next_resp_state = SRS_SYNC_ERROR;
			}
		}
		break;
	}

	case SRS_WAIT_FOR_ARGUMENTS:
	{
		mbox_svc.resp_state = SRS_WAIT_FOR_ARGUMENTS;
		uint16_t mbox_resp_len = mbox_svc.resp_queue[mbox_svc.curr_di].resp_len;
		uint32_t *read_buff = NULL;
		uint16_t read_len = 0U;
		uint32_t read_max_len = 0U;
		uint32_t timeout = 0U;

		/* Determine where to copy the buffer. */
		sdm_command_t *cmd_desc = mailbox_get_cmd_desc(
						mbox_svc.resp_queue[mbox_svc.curr_di].client_id,
						mbox_svc.resp_queue[mbox_svc.curr_di].job_id);
		if (cmd_desc != NULL && cmd_desc->cb_args != NULL) {
			read_buff = cmd_desc->cb_args;
			read_max_len = cmd_desc->response_capacity;
		} else {
			read_buff = (uint32_t *)mbox_svc.resp_queue[mbox_svc.curr_di].resp_data;
			read_max_len = MBOX_SVC_MAX_RESP_DATA_SIZE;
		}

		/* Reject oversized responses before reading/copying any payload.
		 * Leave parser/resources retained for the caller's terminal failure. */
		if (mbox_resp_len > read_max_len) {
			mbox_svc.next_resp_state = SRS_SYNC_ERROR;
			break;
		}

		rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
		rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);

		while ((read_len < mbox_resp_len) && (rin != rout) && (read_len < read_max_len)) {
			timeout = 10000U;

			/* Copy the response data to the buffer */
			read_buff[read_len++] = mmio_read_32(MBOX_ENTRY_TO_ADDR(RESP, (rout)++));

			VERBOSE("MBOX: 0x%x\n", read_buff[read_len - 1]);

			/* Update the read out buffer index */
			rout %= MBOX_RESP_BUFFER_SIZE;
			mmio_write_32(MBOX_OFFSET + MBOX_ROUT, rout);

			/*
			 * The response buffer is of 16 words size, this loop checks
			 * if the response buffer is empty and if empty trigger an
			 * interrupt to SDM and wait for the response buffer to fill
			 */
			do {
				if (read_len == mbox_resp_len)
					break;

				rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
				if (rout == rin) {
					mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_TO_SDM, 1U);
					udelay(100);
				} else {
					break;
				}
				timeout--;
			} while ((read_len < mbox_resp_len) && (timeout != 0U));

			if (timeout == 0U) {
				INFO("MBOX: Timeout waiting for response data\n");
				mbox_svc.next_resp_state = SRS_SYNC_ERROR;
				break;
			}
		}

		/* Check if we have received all the arguments */
		mbox_svc.resp_queue[mbox_svc.curr_di].rcvd_resp_len = read_len;
		if (mbox_resp_len == read_len) {
			uint8_t transaction_id =
					((mbox_svc.resp_queue[mbox_svc.curr_di].client_id << 4) |
					 (mbox_svc.resp_queue[mbox_svc.curr_di].job_id));
			VERBOSE("MBOX: Received all the response data len %d, transaction_id %d\n",
				read_len, transaction_id);

			spin_lock(&mbox_db_lock);
			mbox_svc.received_bitmap[transaction_id / MBOX_TID_BITMAP_SIZE] |=
						(1ULL << (transaction_id % MBOX_TID_BITMAP_SIZE));
			spin_unlock(&mbox_db_lock);

			mbox_svc.resp_queue[mbox_svc.curr_di].flags |= FLAG_SDM_RESPONSE_IS_VALID;
			mbox_svc.next_resp_state = SRS_WAIT_FOR_RESP;
			mbox_svc.curr_di = -1;
		} else {
			mbox_svc.next_resp_state = SRS_SYNC_ERROR;
			VERBOSE("MBOX: Received partial response data len %d, max len %d\n",
				read_len, read_max_len);
		}
		break;
	}

	case SRS_SYNC_ERROR:
	{
		mbox_svc.resp_state = SRS_SYNC_ERROR;
		INFO("MBOX: Error in response handling\n");
		break;
	}

	default:
		break;
	} /* switch */
}
static int mailbox_response_handler_fsm(void)
{
	int status = MBOX_RET_OK;

	spin_lock(&mbox_read_lock);
	/* Mailbox peripheral response parser */
	do {
		/* Iterate till the state machine transition ends */
		mailbox_response_parser();

		/* Note down if there is any error in the response parsing */
		if (mbox_svc.next_resp_state == SRS_SYNC_ERROR)
			status = MBOX_RET_ERROR;

	} while (mbox_svc.resp_state != mbox_svc.next_resp_state);
	spin_unlock(&mbox_read_lock);

	return status;
}
static int mailbox_read_response_v3_selected(uint8_t client_id, uint8_t *job_id,
				    uint32_t *header, uint32_t *resp,
				    uint32_t *resp_len,
				    uint8_t ignore_client_id, int expected_job)
{
	uint8_t di = 0U;
	int status = MBOX_RET_OK;
	sdm_response_t *resp_desc = NULL;
	sdm_command_t *cmd_desc = NULL;

	/*
	 * In the V1, the client ID is always MBOX_ATF_CLIENT_ID and in this
	 * routine we will collect the response which only belongs to this
	 * client ID. So safe to ignore this input.
	 */
	(void)ignore_client_id;

	/* Clear the SDM doorbell interrupt */
	if (mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM) == 1U)
		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM, 0U);

	/* Fill the command descriptor index and get the same */
	status = mailbox_fill_cmd_desc(client_id, async_v1_job_id, resp,
			(resp != NULL && resp_len != NULL) ? *resp_len : 0U);
	if (status != MBOX_RET_OK)
		return status;

	cmd_desc = mailbox_get_cmd_desc(client_id, async_v1_job_id);

	/* Get the response from SDM, just go through one cycle */
	status = mailbox_response_handler_fsm();
	if (status != MBOX_RET_OK) {
		mailbox_free_cmd_desc(cmd_desc);
		*resp_len = 0U;
		return status;
	}

	/* Check the local response queue with the given client ID */
	resp_desc = expected_job < 0 ? mailbox_get_resp_desc_cid(client_id, &di) :
		mailbox_get_resp_desc(client_id, (uint8_t)expected_job, &di);
	if (resp_desc == NULL) {
		mailbox_free_cmd_desc(cmd_desc);
		*resp_len = 0U;
		return MBOX_NO_RESPONSE;
	}

	/* Update the received mailbox response length, job ID and header */
	*job_id = resp_desc->job_id;
	*resp_len = resp_desc->rcvd_resp_len;
	if (header != NULL)
		*header = resp_desc->header;

	/* Check the mailbox response error code */
	if (MBOX_RESP_ERR(resp_desc->header) > 0U) {
		INFO("MBOX: Error in async response: %x\n", resp_desc->header);
		status = -MBOX_RESP_ERR(resp_desc->header);
	}

	/* Free up the response and command descriptors */
	mailbox_free_resp_desc(di);
	mailbox_free_cmd_desc(cmd_desc);

	return status;
}
static int mailbox_read_response_v3(uint8_t client_id, uint8_t *job_id,
    uint32_t *header, uint32_t *resp, uint32_t *resp_len, uint8_t ignore_client_id)
{
    return mailbox_read_response_v3_selected(client_id,job_id,header,resp,resp_len,ignore_client_id,-1);
}
int mailbox_read_response_async_expected(unsigned int expected_job,
    unsigned int *job_id,uint32_t *header,uint32_t *response,unsigned int *resp_len)
{
    if(expected_job>=MBOX_MAX_IND_JOB_ID || job_id==NULL || header==NULL || resp_len==NULL)
        return MBOX_WRONG_ID;
    uint8_t observed_job=0;
    int status=mailbox_read_response_v3_selected(MBOX_ATF_CLIENT_ID,&observed_job,header,response,resp_len,0,(int)expected_job);
    if(status==MBOX_RET_OK)*job_id=observed_job;
    return status;
}
int mailbox_read_response_async(unsigned int *job_id, uint32_t *header,
				uint32_t *response, unsigned int *resp_len,
				uint8_t ignore_client_id)
{
#if SIP_SVC_V3
	/* Just to avoid the build warning */
	(void)mailbox_resp_ctr;
	return mailbox_read_response_v3(MBOX_ATF_CLIENT_ID,
					(uint8_t *)job_id, header,
					response, resp_len,
					ignore_client_id);
#else
	uint32_t rin;
	uint32_t rout;
	uint32_t resp_data;
	uint32_t ret_resp_len = 0;
	uint8_t is_done = 0;
	uint32_t resp_len_check = 0;

	if ((mailbox_resp_ctr.flag & MBOX_PAYLOAD_FLAG_BUSY) != 0) {
		ret_resp_len = MBOX_RESP_LEN(
				mailbox_resp_ctr.payload->header) -
				mailbox_resp_ctr.index;
	}

	if (mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM) == 1U) {
		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM, 0U);
	}

	rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
	rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);

	while (rout != rin && !is_done) {

		resp_data = mmio_read_32(MBOX_ENTRY_TO_ADDR(RESP, (rout)++));

		rout %= MBOX_RESP_BUFFER_SIZE;
		mmio_write_32(MBOX_OFFSET + MBOX_ROUT, rout);
		rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);

		if ((mailbox_resp_ctr.flag & MBOX_PAYLOAD_FLAG_BUSY) != 0) {
			mailbox_resp_ctr.payload->data[mailbox_resp_ctr.index] = resp_data;
			mailbox_resp_ctr.index++;
			ret_resp_len--;
		} else {
			if (!ignore_client_id) {
				if (MBOX_RESP_CLIENT_ID(resp_data) != MBOX_ATF_CLIENT_ID) {
					*resp_len = 0;
					return MBOX_WRONG_ID;
				}
			}
			*job_id = MBOX_RESP_JOB_ID(resp_data);
			ret_resp_len = MBOX_RESP_LEN(resp_data);
			mailbox_resp_ctr.payload->header = resp_data;
			mailbox_resp_ctr.flag |= MBOX_PAYLOAD_FLAG_BUSY;
		}

		if (ret_resp_len == 0) {
			is_done = 1;
		}
	}

	if (is_done != 0) {

		/* copy header data to input address if applicable */
		if (header != 0) {
			*header = mailbox_resp_ctr.payload->header;
		}

		/* copy response data to input buffer if applicable */
		ret_resp_len = MBOX_RESP_LEN(mailbox_resp_ctr.payload->header);
		if ((ret_resp_len > 0) && (response != NULL) && (resp_len != NULL)) {
			if (*resp_len > ret_resp_len) {
				*resp_len = ret_resp_len;
			}

			resp_len_check = (uint32_t) *resp_len;

			if (resp_len_check > MBOX_DATA_MAX_LEN) {
				return MBOX_RET_ERROR;
			}

			memcpy_s((uint8_t *) response, *resp_len * MBOX_WORD_BYTE,
				(uint8_t *) mailbox_resp_ctr.payload->data,
				*resp_len * MBOX_WORD_BYTE);
		}

		/* reset async response param */
		mailbox_resp_ctr.index = 0;
		mailbox_resp_ctr.flag = 0;

		if (MBOX_RESP_ERR(mailbox_resp_ctr.payload->header) > 0U) {
			INFO("Error in async response: %x\n",
				mailbox_resp_ctr.payload->header);
			return -MBOX_RESP_ERR(mailbox_resp_ctr.payload->header);
		}

		return MBOX_RET_OK;
	}

	*resp_len = 0;
	return (mailbox_resp_ctr.flag & MBOX_PAYLOAD_FLAG_BUSY) ? MBOX_BUSY : MBOX_NO_RESPONSE;
#endif
}
int mailbox_send_cmd_async_v3(uint8_t client_id, uint8_t job_id, uint32_t cmd,
			      uint32_t *args, uint32_t args_len, uint8_t cmd_flag,
			      sdm_command_callback cb, uint32_t *cb_args,
			      uint32_t cb_args_len)
{
	int status = 0;
	sdm_command_t *cmd_desc = NULL;

	VERBOSE("MBOX: cid: %d, jid: %d, cmd: %d, cmd_flag: %d\n",
		client_id, job_id, cmd, cmd_flag);

	if (IS_CMD_SET(cmd_flag, URGENT)) {
		mmio_write_32(MBOX_OFFSET + MBOX_URG, cmd);
		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_TO_SDM, 1U);
	} else {
		/* Get a free command descriptor */
		cmd_desc = mailbox_get_free_cmd_desc();
		if (cmd_desc == NULL)
			return MBOX_BUFFER_FULL;

		/* Record all the given values for the command. */
		cmd_desc->client_id = client_id;
		cmd_desc->job_id = job_id;
		cmd_desc->cb = cb;
		cmd_desc->cb_args = cb_args;
		cmd_desc->cb_args_len = cb_args_len;
		cmd_desc->response_capacity = cb_args_len;

		/* Push the command to mailbox FIFO */
		status = fill_mailbox_circular_buffer(
					MBOX_FRAME_CMD_HEADER(client_id, job_id,
					args_len, IS_CMD_SET(cmd_flag, INDIRECT), cmd),
					args,
					args_len);

		if (status != MBOX_RET_OK) {
			INFO("MBOX: Failed to push the command to mailbox FIFO\n");
			/* Free the command descriptor. */
			mailbox_free_cmd_desc(cmd_desc);
		}
	}

	INFO("MBOX: %s: status: %d\n", __func__, status);

	return status;
}
static int mailbox_poll_response_v3(uint8_t client_id, uint8_t job_id,
				    uint32_t *resp, unsigned int *resp_len,
				    uint32_t urgent)
{
	unsigned int timeout = 40U;
	unsigned int sdm_loop = 255U;
	bool is_cmd_desc_fill = false;
	uint8_t di = 0U;
	sdm_response_t *resp_desc = NULL;
	sdm_command_t *cmd_desc = NULL;

	while (sdm_loop != 0U) {
		do {
			uint32_t bell = mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM);
			if (fes_nano_isolation_io.polling) {
				fes_nano_isolation_io.bell = bell;
				fes_nano_isolation_io.polls++;
			}
			if (bell == 1U) {
				break;
			}
			mdelay(10U);
		} while (--timeout != 0U);

		if (timeout == 0U) {
			if (fes_nano_isolation_io.polling) fes_nano_isolation_io.branch = 1U;
			INFO("%s: Timed out waiting for SDM intr\n", __func__);
			break;
		}

		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM, 0U);

		if ((urgent & 1U) != 0U) {
			mdelay(5U);
			if ((mmio_read_32(MBOX_OFFSET + MBOX_STATUS) &
				MBOX_STATUS_UA_MASK) ^
				(urgent & MBOX_STATUS_UA_MASK)) {
				mmio_write_32(MBOX_OFFSET + MBOX_URG, 0U);
				return MBOX_RET_OK;
			}

			mmio_write_32(MBOX_OFFSET + MBOX_URG, 0U);
			ERROR("MBOX: Mailbox did not get UA");
			return MBOX_RET_ERROR;
		}

		/* Fill the command descriptor index and get the same. */
		if (!is_cmd_desc_fill) {
			if (mailbox_fill_cmd_desc(client_id, job_id, resp,
				(resp != NULL && resp_len != NULL) ? *resp_len : 0U) != MBOX_RET_OK) {
				if (fes_nano_isolation_io.polling) fes_nano_isolation_io.branch = 2U;
				return MBOX_BUFFER_FULL;
			}

			cmd_desc = mailbox_get_cmd_desc(client_id, job_id);
			is_cmd_desc_fill = true;
		}

		/* Since it is sync call, will try to read till it time out */
		int parser_status = mailbox_response_handler_fsm();
		if (parser_status != MBOX_RET_OK) {
			mailbox_free_cmd_desc(cmd_desc);
			if (resp_len != NULL) *resp_len = 0U;
			return parser_status;
		}

		/* Check the response queue with the given client ID and job ID */
		resp_desc = mailbox_get_resp_desc(client_id, job_id, &di);
		if (resp_desc) {
			VERBOSE("%s: Resp received for cid %d, jid %d\n",
				__func__, resp_desc->client_id, resp_desc->job_id);

			uint16_t header = resp_desc->header;
			if (fes_nano_isolation_io.polling) {
				fes_nano_isolation_io.branch = 3U;
				fes_nano_isolation_io.header = header;
			}

			/* Update the return response length */
			if (resp_len)
				*resp_len = resp_desc->rcvd_resp_len;

			/* Free the response and command descriptor */
			mailbox_free_resp_desc(di);
			mailbox_free_cmd_desc(cmd_desc);

			if (MBOX_RESP_ERR(header) > 0U) {
				INFO("%s: SDM err code: 0x%x\n", __func__,
					MBOX_RESP_ERR(header));
				return -MBOX_RESP_ERR(header);
			}

			VERBOSE("%s: MBOX_RET_OK\n", __func__);
			return MBOX_RET_OK;
		}
		sdm_loop--;
	}

	if (fes_nano_isolation_io.polling && !fes_nano_isolation_io.branch)
		fes_nano_isolation_io.branch = 4U;
	INFO("%s: Timed out waiting for SDM\n", __func__);
	return MBOX_TIMEOUT;
}
int mailbox_poll_response(uint32_t job_id, uint32_t urgent, uint32_t *response,
			  unsigned int *resp_len)
{
#if SIP_SVC_V3
	return mailbox_poll_response_v3(MBOX_ATF_CLIENT_ID, (uint8_t)job_id,
					response, resp_len, urgent);
#else
	unsigned int timeout = 40U;
	unsigned int sdm_loop = 255U;
	unsigned int ret_resp_len;
	uint32_t rin;
	uint32_t rout;
	uint32_t resp_data;

	while (sdm_loop != 0U) {
		do {
			if (mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM)
				== 1U) {
				break;
			}
			mdelay(10U);
		} while (--timeout != 0U);

		if (timeout == 0U) {
			break;
		}

		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM, 0U);

		if ((urgent & 1U) != 0U) {
			mdelay(5U);
			if ((mmio_read_32(MBOX_OFFSET + MBOX_STATUS) &
				MBOX_STATUS_UA_MASK) ^
				(urgent & MBOX_STATUS_UA_MASK)) {
				mmio_write_32(MBOX_OFFSET + MBOX_URG, 0U);
				return MBOX_RET_OK;
			}

			mmio_write_32(MBOX_OFFSET + MBOX_URG, 0U);
			ERROR("MBOX: Mailbox did not get UA");
			return MBOX_RET_ERROR;
		}

		rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
		rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);

		while (rout != rin) {
			resp_data = mmio_read_32(MBOX_ENTRY_TO_ADDR(RESP,
								(rout)++));

			rout %= MBOX_RESP_BUFFER_SIZE;
			mmio_write_32(MBOX_OFFSET + MBOX_ROUT, rout);

			if ((MBOX_RESP_CLIENT_ID(resp_data) != MBOX_ATF_CLIENT_ID) ||
			    (MBOX_RESP_JOB_ID(resp_data) != job_id)) {
				continue;
			}

			/* Get the return response len from the response header. */
			ret_resp_len = MBOX_RESP_LEN(resp_data);

			/* Print the response header. */
			VERBOSE("MBOX: RespHdr: cid %d, jid %d, len %d, err_code 0x%x\n",
				MBOX_RESP_CLIENT_ID(resp_data),
				MBOX_RESP_JOB_ID(resp_data),
				MBOX_RESP_LEN(resp_data),
				MBOX_RESP_ERR(resp_data));

			if (iterate_resp(ret_resp_len, response, resp_len)
				!= MBOX_RET_OK) {
				return MBOX_TIMEOUT;
			}

			if (MBOX_RESP_ERR(resp_data) > 0U) {
				INFO("SDM response: Return Code: 0x%x\n", MBOX_RESP_ERR(resp_data));
				return -MBOX_RESP_ERR(resp_data);
			}

			return MBOX_RET_OK;
		}

	sdm_loop--;
	}

	INFO("Timed out waiting for SDM\n");
	return MBOX_TIMEOUT;
#endif
}
int mailbox_send_cmd(uint32_t job_id, uint32_t cmd, uint32_t *args,
			unsigned int len, uint32_t urgent, uint32_t *response,
			unsigned int *resp_len)
{
	int status = 0;

	if (urgent != 0U) {
		urgent |= mmio_read_32(MBOX_OFFSET + MBOX_STATUS) &
					MBOX_STATUS_UA_MASK;
		mmio_write_32(MBOX_OFFSET + MBOX_URG, cmd);
		mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_TO_SDM, 1U);
	} else {
		status = fill_mailbox_circular_buffer(
			MBOX_CLIENT_ID_CMD(MBOX_ATF_CLIENT_ID) |
			MBOX_JOB_ID_CMD(job_id) |
			MBOX_CMD_LEN_CMD(len) |
			cmd, args, len);
	}

	if (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)
		fes_nano_isolation_io.fill = status;
	if (status != 0) {
		return status;
	}

	if (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)
		fes_nano_isolation_io.polling = 1U;
	status = mailbox_poll_response(job_id, urgent, response, resp_len);
	if (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)
		fes_nano_isolation_io.polling = 0U;

	return status;
}
int intel_mailbox_get_config_status(uint32_t cmd, bool init_done,
				    uint32_t *err_states)
{
	int status;
	uint32_t res, response[6];
	unsigned int resp_len = ARRAY_SIZE(response);

	status = mailbox_send_cmd(MBOX_JOB_ID, cmd, NULL, 0U, CMD_CASUAL,
				response, &resp_len);

	if (status < 0) {
		return status;
	}
	if (resp_len < 4U || resp_len > ARRAY_SIZE(response))
		return MBOX_RET_ERROR;

	res = response[RECONFIG_STATUS_STATE];

	if (err_states != NULL)
		*err_states = res;

	if (res == MBOX_CFGSTAT_VAB_BS_PREAUTH) {
		return MBOX_CFGSTAT_STATE_CONFIG;
	}

	if (res != MBOX_CFGSTAT_STATE_IDLE) {
		return res;
	}

	res = response[RECONFIG_STATUS_PIN_STATUS];
	if ((res & PIN_STATUS_NSTATUS) == 0U) {
		return MBOX_CFGSTAT_STATE_ERROR_HARDWARE;
	}

	res = response[RECONFIG_STATUS_SOFTFUNC_STATUS];
	if ((res & SOFTFUNC_STATUS_SEU_ERROR) != 0U) {
		return MBOX_CFGSTAT_STATE_ERROR_HARDWARE;
	}

	if ((res & SOFTFUNC_STATUS_CONF_DONE) == 0U) {
		return MBOX_CFGSTAT_STATE_CONFIG;
	}

	if (init_done && (res & SOFTFUNC_STATUS_INIT_DONE) == 0U) {
		return MBOX_CFGSTAT_STATE_CONFIG;
	}

	return MBOX_RET_OK;
}
int mailbox_send_cmd_async(uint32_t *job_id, uint32_t cmd, uint32_t *args,
			  unsigned int len, unsigned int indirect)
{
	int status;

	status = fill_mailbox_circular_buffer(
				MBOX_CLIENT_ID_CMD(MBOX_ATF_CLIENT_ID) |
				MBOX_JOB_ID_CMD(*job_id) |
				MBOX_CMD_LEN_CMD(len) |
				MBOX_INDIRECT(indirect) |
				cmd, args, len);
	if (status < 0) {
		return status;
	}

#if SIP_SVC_V3
	async_v1_job_id = (uint8_t)*job_id;
#endif
	*job_id = (*job_id + 1U) % MBOX_MAX_IND_JOB_ID;

	return MBOX_RET_OK;
}
static int fill_mailbox_circular_buffer(uint32_t header_cmd, uint32_t *args,
					unsigned int len)
{
	uint32_t sdm_read_offset, cmd_free_offset;
	unsigned int i;
	int ret;
	bool is_doorbell_triggered = false;

#if SIP_SVC_V3
	spin_lock(&mbox_write_lock);
#endif

	cmd_free_offset = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
	sdm_read_offset = mmio_read_32(MBOX_OFFSET + MBOX_COUT);
	if (fes_nano_isolation_io.active && MBOX_CMD_MASK(header_cmd) == MBOX_CMD_FPGA_CONFIG_COMP) {
		fes_nano_isolation_io.cin = cmd_free_offset;
		fes_nano_isolation_io.cout = sdm_read_offset;
	}

	/* Write the command header here */
	ret = write_mailbox_cmd_buffer(&cmd_free_offset, sdm_read_offset,
				       header_cmd, &is_doorbell_triggered);
	if (ret != 0) {
		goto restart_mailbox;
	}

	/* Write the payload here w.r.to args and its len - one by one. */
	for (i = 0U; i < len; i++) {
		is_doorbell_triggered = false;
		ret = write_mailbox_cmd_buffer(&cmd_free_offset,
					       sdm_read_offset, args[i],
					       &is_doorbell_triggered);
		if (ret != 0) {
			goto restart_mailbox;
		}
	}

	mmio_write_32(MBOX_OFFSET + MBOX_DOORBELL_TO_SDM, 1U);

#if SIP_SVC_V3
	spin_unlock(&mbox_write_lock);
#endif
	return MBOX_RET_OK;

restart_mailbox:
	/* Terminal partial post: retain source/FIFO/queues. No reset/replay. */

#if SIP_SVC_V3
	spin_unlock(&mbox_write_lock);
#endif
	return MBOX_TIMEOUT;
}
void mailbox_clear_response(void)
{
	mmio_write_32(MBOX_OFFSET + MBOX_ROUT,
		mmio_read_32(MBOX_OFFSET + MBOX_RIN));
}
int mailbox_send_fpga_config_comp(void)
{
	int ret;

	fes_nano_isolation_io = (struct fes_nano_isolation_observation){.active = 1U};
	ret = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_FPGA_CONFIG_COMP, NULL, 0U,
				 CMD_CASUAL, NULL, NULL);
	fes_nano_isolation_io.active = 0U;
	NOTICE("FES_NANO_ISOLATION_IO fill=%d branch=%u cin=%x cout=%x bell=%x polls=%u header=%x\n",
	       fes_nano_isolation_io.fill, fes_nano_isolation_io.branch,
	       fes_nano_isolation_io.cin, fes_nano_isolation_io.cout,
	       fes_nano_isolation_io.bell, fes_nano_isolation_io.polls,
	       fes_nano_isolation_io.header);
	NOTICE("FES_NANO_ISOLATION status=%d\n", ret);

	if (ret != 0) {
		INFO("SOCFPGA: FPGA configuration complete response, Return Code: 0x%x\n",
			MBOX_RESP_ERR(-ret));
		return MBOX_RET_ERROR;
	}

	return MBOX_RET_OK;
}
static int fes_contained_transport_failure(unsigned phase, int status)
{
	if (fes_contained_transport_status == 0) {
		fes_contained_transport_status = status;
		fes_contained_transport_phase = phase;
	}
	request_type = NO_REQUEST;
	return INTEL_SIP_SMC_STATUS_ERROR;
}
static uint32_t fes_contained_isdone_detail(uint32_t status, uint32_t detail)
{
	return status == INTEL_SIP_SMC_STATUS_BUSY ? 0U : detail;
}
static int intel_fpga_sdm_write_buffer(struct fpga_config_info *buffer)
{
	uint32_t args[3];

	while (max_blocks > 0 && buffer->size > buffer->size_written) {
		args[0] = (1<<8);
		args[1] = buffer->addr + buffer->size_written;
		if (buffer->size - buffer->size_written <= bytes_per_block) {
			args[2] = buffer->size - buffer->size_written;
			current_buffer++;
			current_buffer %= FPGA_CONFIG_BUFFER_SIZE;
		} else {
			args[2] = bytes_per_block;
		}

		int gate = fes_nano_sdm_transfer_before(send_id,args[1],args[2],(uint32_t)max_blocks);
		if (gate) return fes_contained_transport_failure(20U,gate);
		unsigned observed = fes_nano_sdm_note(args[1], args[2]);
		int status = mailbox_send_cmd_async(&send_id, MBOX_RECONFIG_DATA, args,
					3U, CMD_INDIRECT);
		gate = fes_nano_sdm_transfer_posted(status,send_id);
		if (gate) return fes_contained_transport_failure(21U,gate);
		if (observed) NOTICE("FES_NANO_SDM_RESULT status=%d\n", status);
		if (status != 0) return fes_contained_transport_failure(1U, status);
		buffer->size_written += args[2];

		buffer->subblocks_sent++;
		max_blocks--;
	}

	return !max_blocks;
}
static int intel_fpga_sdm_write_all(void)
{
	if (fes_contained_transport_status != 0) return INTEL_SIP_SMC_STATUS_ERROR;
	for (int i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		int status = intel_fpga_sdm_write_buffer(&fpga_config_buffers[current_buffer]);
		if (status == INTEL_SIP_SMC_STATUS_ERROR) return status;
		if (status != 0) {
			break;
		}
	}
	return 0;
}
static uint32_t intel_mailbox_fpga_config_isdone_unchecked(uint32_t *err_states)
{
	uint32_t ret;

	if (err_states == NULL)
		return INTEL_SIP_SMC_STATUS_REJECTED;

	switch (request_type) {
	case RECONFIGURATION:
		ret = intel_mailbox_get_config_status(MBOX_RECONFIG_STATUS,
							true, err_states);
		break;
	case BITSTREAM_AUTH:
		ret = intel_mailbox_get_config_status(MBOX_RECONFIG_STATUS,
							false, err_states);
		break;
	default:
		ret = intel_mailbox_get_config_status(MBOX_CONFIG_STATUS,
							false, err_states);
		break;
	}

	if (ret != 0U) {
		if (ret == MBOX_CFGSTAT_STATE_CONFIG) {
			return INTEL_SIP_SMC_STATUS_BUSY;
		} else {
			request_type = NO_REQUEST;
			return INTEL_SIP_SMC_STATUS_ERROR;
		}
	}

	/* FES contained profile: ISDONE never opens a bridge. The runtime
	 * separately opens only the control bridge, verifies live identity,
	 * then explicitly releases data bridges. */
	request_type = NO_REQUEST;

	return INTEL_SIP_SMC_STATUS_OK;
}
static uint32_t intel_mailbox_fpga_config_isdone(uint32_t *err_states)
{
	if (!err_states) return INTEL_SIP_SMC_STATUS_REJECTED;
	int gate=fes_nano_sdm_transfer_status_begin();
	if (gate) return fes_contained_transport_failure(24U,gate);
	*err_states=0;
	uint32_t status=intel_mailbox_fpga_config_isdone_unchecked(err_states);
	gate=fes_nano_sdm_transfer_status_result(status,fes_contained_isdone_detail(status,*err_states));
	if (gate) return fes_contained_transport_failure(25U,gate);
	return status;
}
static int mark_last_buffer_xfer_completed(uint32_t *buffer_addr_completed)
{
	int i;

	for (i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		if (fpga_config_buffers[i].block_number == current_block) {
			fpga_config_buffers[i].subblocks_sent--;
			if (fpga_config_buffers[i].subblocks_sent == 0
			&& fpga_config_buffers[i].size <=
			fpga_config_buffers[i].size_written) {
				int gate = fes_nano_sdm_transfer_buffer(fpga_config_buffers[i].addr,
				    (uint32_t)fpga_config_buffers[i].size);
				if (gate) { fes_contained_transport_failure(22U,gate); return -1; }
				fpga_config_buffers[i].write_requested = 0;
				current_block++;
				*buffer_addr_completed =
					fpga_config_buffers[i].addr;
				return 0;
			}
		}
	}

	return -1;
}
static int intel_fpga_config_completed_write(uint32_t *completed_addr,
					uint32_t *count, uint32_t *job_id)
{
	/* DATA ACKs carry no payload. A non-null zero-capacity guard also
	 * makes an unexpected argument response fail before any copy. */
	uint32_t resp_guard = 0;
	unsigned int resp_len = 0;
	int status = INTEL_SIP_SMC_STATUS_OK;
	int all_completed = 1;
	*count = 0;
	if (fes_contained_transport_status != 0) return INTEL_SIP_SMC_STATUS_ERROR;

	while (*count < 3) {

		uint32_t resp_header = 0;
		status = fes_nano_sdm_transfer_read(job_id,&resp_header,&resp_guard,&resp_len);
		int gate = fes_nano_sdm_transfer_response(status,resp_header,*job_id,resp_len);
		if (gate) { *count=0; return fes_contained_transport_failure(23U,gate); }

		if (status == MBOX_NO_RESPONSE && resp_header == 0) break;
		if (status != 0) {
			*count = 0;
			return fes_contained_transport_failure(2U, status);
		}

		max_blocks++;

		if (mark_last_buffer_xfer_completed(
			&completed_addr[*count]) == 0) {
			*count = *count + 1;
		} else {
			break;
		}
	}

	if (intel_fpga_sdm_write_all() != 0) {
		*count = 0;
		return INTEL_SIP_SMC_STATUS_ERROR;
	}

	if (*count > 0) {
		status = INTEL_SIP_SMC_STATUS_OK;
	} else if (*count == 0) {
		status = INTEL_SIP_SMC_STATUS_BUSY;
	}

	for (int i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		if (fpga_config_buffers[i].write_requested != 0) {
			all_completed = 0;
			break;
		}
	}

	if (all_completed == 1) {
		return INTEL_SIP_SMC_STATUS_OK;
	}

	return status;
}
static int intel_fpga_config_start(uint32_t flag)
{
	uint32_t argument = 0x1;
	uint32_t response[3];
	int status = 0;
	unsigned int size = 0;
	unsigned int resp_len = ARRAY_SIZE(response);

	fes_nano_start_note(2U, fes_contained_transport_status);
	/* FES profile admits full configuration only. */
	if (flag != 0U) {
		return INTEL_SIP_SMC_STATUS_REJECTED;
	}
	if (fes_contained_transport_status != 0)
		return INTEL_SIP_SMC_STATUS_ERROR;

	/* Containment precedes every SDM configuration command. */
	status = socfpga_bridges_disable(~0U);
	fes_nano_start_note(3U, status);
	if (status != 0) {
		return fes_contained_transport_failure(29U,status ? status:-5);
	}
	status=fes_nano_sdm_transfer_prepare();
	if (status) return fes_contained_transport_failure(26U,status);
#if PLATFORM_MODEL == PLAT_SOCFPGA_AGILEX5
	status = mailbox_send_fpga_config_comp();
	fes_nano_start_note(4U, status);
	if (status < 0) {
		return fes_contained_transport_failure(29U,status ? status:-5);
	}
#endif

	request_type = RECONFIGURATION;

	if (!CONFIG_TEST_FLAG(flag, PARTIAL_CONFIG)) {
		bridge_disable = true;
	}

	if (CONFIG_TEST_FLAG(flag, AUTHENTICATION)) {
		size = 1;
		bridge_disable = false;
		request_type = BITSTREAM_AUTH;
	}

#if PLATFORM_MODEL == PLAT_SOCFPGA_AGILEX5
	fes_nano_start_note(12U, 0);
	intel_smmu_hps_remapper_init(0U);
	fes_nano_start_note(13U, 0);
#endif

	mailbox_clear_response();
	fes_nano_start_note(14U, 0);

	status = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_CANCEL, NULL, 0U,
			CMD_CASUAL, NULL, NULL);
	fes_nano_start_note(5U, status);
	if (status < 0) {
		return fes_contained_transport_failure(29U,status ? status:-5);
	}

	status = mailbox_send_cmd(MBOX_JOB_ID, MBOX_RECONFIG, &argument, size,
			CMD_CASUAL, response, &resp_len);
	fes_nano_start_note(6U, status);
	fes_nano_start_note(7U, (int)resp_len);

	if (status < 0 || resp_len < 2U || resp_len > ARRAY_SIZE(response) ||
	    response[0] == 0U || response[0] > INT32_MAX || response[1] == 0U) {
		return fes_contained_transport_failure(29U,status ? status:-5);
	}

	max_blocks = response[0];
	bytes_per_block = response[1];
	status=fes_nano_sdm_transfer_begin((uint32_t)max_blocks,bytes_per_block,send_id);
	if (status) return fes_contained_transport_failure(27U,status);
	fes_nano_start_note(8U, 0);

	for (int i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		fpga_config_buffers[i].size = 0;
		fpga_config_buffers[i].size_written = 0;
		fpga_config_buffers[i].addr = 0;
		fpga_config_buffers[i].write_requested = 0;
		fpga_config_buffers[i].block_number = 0;
		fpga_config_buffers[i].subblocks_sent = 0;
	}

	blocks_submitted = 0;
	current_block = 0;
	read_block = 0;
	current_buffer = 0;


	return INTEL_SIP_SMC_STATUS_OK;
}
static bool is_fpga_config_buffer_full(void)
{
	for (int i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		if (!fpga_config_buffers[i].write_requested) {
			return false;
		}
	}
	return true;
}
bool is_address_in_ddr_range(uint64_t addr, uint64_t size)
{
	uint128_t dram_max_sz = (uint128_t)DRAM_BASE + (uint128_t)DRAM_SIZE;
	uint128_t dram_region_end = (uint128_t)addr + (uint128_t)size;

	if (!addr && !size) {
		return true;
	}
	if (size > (UINT64_MAX - addr)) {
		return false;
	}
	if (addr < BL31_LIMIT) {
		return false;
	}
	if (dram_region_end > dram_max_sz) {
		return false;
	}

	return true;
}
static uint32_t intel_fpga_config_write(uint64_t mem, uint64_t size)
{
	int i;

	if (intel_fpga_sdm_write_all() != 0) return INTEL_SIP_SMC_STATUS_ERROR;

	if (!is_address_in_ddr_range(mem, size) ||
		is_fpga_config_buffer_full()) {
		return INTEL_SIP_SMC_STATUS_REJECTED;
	}

	int gate=fes_nano_sdm_transfer_accept(mem,size);
	if (gate) return fes_contained_transport_failure(28U,gate);
#if PLATFORM_MODEL == PLAT_SOCFPGA_AGILEX5
	intel_smmu_hps_remapper_init(&mem);
#endif

	for (i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
		int j = (i + current_buffer) % FPGA_CONFIG_BUFFER_SIZE;

		if (!fpga_config_buffers[j].write_requested) {
			fpga_config_buffers[j].addr = mem;
			fpga_config_buffers[j].size = size;
			fpga_config_buffers[j].size_written = 0;
			fpga_config_buffers[j].write_requested = 1;
			fpga_config_buffers[j].block_number =
				blocks_submitted++;
			fpga_config_buffers[j].subblocks_sent = 0;
			break;
		}
	}

	if (is_fpga_config_buffer_full()) {
		return INTEL_SIP_SMC_STATUS_BUSY;
	}

	return INTEL_SIP_SMC_STATUS_OK;
}
#include "nano_sdm_control_mailbox.inc"
#include "input_transfer_service.c"
/* SPDX-License-Identifier: BSD-3-Clause */
#include <unistd.h>
#include <sys/wait.h>
static unsigned joined_cases,complete_calls,reverse_order,trial_case;
static int complete(void *c,uint64_t owner,uint64_t generation,unsigned rearmed){
 assert(c==&provider && owner==configuration.owner && generation==configuration.generation && !rearmed);
 assert(transfer.configured && !configuration.pending && configuration.completed==configuration.bytes);
 complete_calls++;return 0;
}
static void flush_pending(void){
 if(drop_acks){pending_count=0;return;}
 for(unsigned n=0;n<pending_count;n++){
  unsigned i=reverse_order ? pending_count-1-n:n;
  push(((1U<<28)|(pending[i]<<24)|(fixture_ack_words<<12))^fixture_ack_xor,1);
  for(unsigned w=0;w<fixture_ack_words;w++)push(0xdeadbeef,0);
  if(duplicate_acks)push((1U<<28)|(pending[i]<<24),1);
 }
 pending_count=0;
}
static void mutate_result(void){
 switch(trial_case-100){
 case 0:transfer.binding.chunk=0;break;case 1:transfer.binding.bytes=0;break;
 case 2:transfer.configured=0;break;case 3:transfer.failed=1;break;
 case 4:transfer.count=1;break;case 5:transfer.pending=1;break;
 case 6:accepted_bytes-=4;break;case 7:transfer.posted_bytes-=4;break;
 case 8:transfer.acknowledged_bytes-=4;break;case 9:transfer.completed_buffer_bytes-=4;break;
 case 10:transfer.posts++;break;case 11:transfer.acks++;break;case 12:transfer.buffer_reports++;break;
 case 13:transfer.headers++;break;case 14:transfer.start++;break;case 15:transfer.last=0;break;
 case 16:configuration.issued=FES_NANO_CONFIG_WRITE;break;case 17:configuration.pending=4;break;
 case 18:configuration.completed-=4;break;case 19:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
 case 20:registers[44]=1;break;case 21:ticks+=20000000;break;case 22:ticks=0;break;
 case 23:live_mutation=3;mutation_at=live_owner_checks+1;break;
 default:assert(0);
 }
}
static int dispatch(uint32_t fid,uint64_t address,uint32_t bytes,uint64_t result[4]){
 memset(result,0,4*sizeof(*result));
 int guard=fes_nano_configuration_before(&configuration,fid,address,bytes,request_type!=NO_REQUEST);
 if(guard){result[0]=(uint32_t)guard;fes_nano_sdm_source_finish((uint32_t)result[0],0);}
 uint32_t count=0,job=0,done[3]={0},detail=0;
 if(fid==FES_NANO_CONFIG_START)result[0]=intel_fpga_config_start(0);
 else if(fid==FES_NANO_CONFIG_WRITE)result[0]=intel_fpga_config_write(address,bytes);
 else if(fid==FES_NANO_CONFIG_COMPLETED){
  result[0]=intel_fpga_config_completed_write(done,&count,&job);assert(count<=3);
  if(count){assert(!result[0]);for(unsigned n=0;n<count;n++)result[n+1]=done[n];}
 }else if(fid==FES_NANO_CONFIG_ISDONE){
  result[0]=intel_mailbox_fpga_config_isdone(&detail);result[1]=fes_contained_isdone_detail((uint32_t)result[0],detail);
  if(result[0]==0){
   if(trial_case>=100)mutate_result();
   fes_nano_sdm_source_finish((uint32_t)result[0],(uint32_t)result[1]);
  }
 }else assert(0);
 if(result[0]!=0 && result[0]!=1)fes_nano_sdm_source_finish((uint32_t)result[0],(uint32_t)result[1]);
 fes_nano_configuration_after(&configuration,fid,result);
 return (int)result[0];
}
static int transfer_cycle(void){
 uint64_t r[4];int status=dispatch(FES_NANO_CONFIG_START,0,0,r);if(status)return status;
 uint32_t offset=0;unsigned polls=0;
 while(configuration.completed<configuration.bytes){
  if(offset<configuration.bytes && configuration.pending<configuration.chunk*4){
   uint32_t bytes=configuration.bytes-offset;if(bytes>configuration.chunk)bytes=configuration.chunk;
   status=dispatch(FES_NANO_CONFIG_WRITE,configuration.base+offset,bytes,r);
   if(status!=0 && status!=1)return status;offset+=bytes;
  }
  flush_pending();status=dispatch(FES_NANO_CONFIG_COMPLETED,0,0,r);
  if(status!=0 && status!=1)return status;
  assert(++polls<21000);
 }
 return dispatch(FES_NANO_CONFIG_ISDONE,0,0,r);
}
static void joined_setup(void){
 source_setup(0);mode=0;
 mbox_svc.curr_di=-1;mbox_svc.resp_state=mbox_svc.next_resp_state=SRS_WAIT_FOR_RESP;
 configuration.issued=0;configuration.started=0;configuration.stage=FES_CONFIG_ARM;
 configuration.io=(struct fes_nano_configuration_ops){.context=&provider,.check=configuration_check,.complete=complete};
 expected=(struct nano_sdm_input_binding){.owner=configuration.owner,.epoch=provider.record.boot.epoch,.generation=configuration.generation,.bytes=configuration.bytes,.chunk=configuration.chunk,.idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};
 memcpy(expected.digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48);
 fixture_pin=PIN_STATUS_NSTATUS;fixture_soft=SOFTFUNC_STATUS_CONF_DONE|SOFTFUNC_STATUS_INIT_DONE;
}
static void joined_case(void){
 joined_setup();unsigned usb_writes=model.base.h.usb_writes,physical_requests=model.requests;
 assert(mailbox_hps_stage_notify(1)==0);
 assert(!service_error && !prepare_attempted && !sdm_arm_entered && !smmu_writes && !data_posts);
 assert(consumed==1 && stream_words==1);
 if(trial_case==1)fixture_ack_words=1;
 if(trial_case==2)fixture_ack_xor=1;
 if(trial_case==3)duplicate_acks=1;
 if(trial_case==4)hash_failure=1;
 if(trial_case==5)fail_word=5;
 if(trial_case==6)fixture_status_words=3;
 if(trial_case==7)fixture_state=7;
 if(trial_case==8)fixture_soft|=SOFTFUNC_STATUS_SEU_ERROR;
 if(trial_case==9)fixture_pin=0;
 volatile unsigned returned=0;
 if(!setjmp(terminal)){assert(!transfer_cycle());returned=1;}
 if(returned){
  assert(!parks && complete_calls==1 && configuration.finished && source_retirement.continued);
  assert(strstr(terminal_log,"FES_NANO_INPUT_CONTINUE build="));
  assert(!strstr(terminal_log,"FES_NANO_INPUT_PARK"));
 }else{
  assert(parks==1 && !complete_calls && !configuration.finished && !source_retirement.continued);
  assert(strstr(terminal_log,"FES_NANO_INPUT_PARK retained=1 resume=0 activated=0"));
 }
 char *line=strstr(terminal_log,"FES_NANO_INPUT_RESULT build=0000000000000000 ");assert(line);
 struct nano_sdm_transfer_result observed;fes_nano_sdm_transfer_result(&observed);
 if(!trial_case){
  assert(strstr(line," status=0 detail=0 proof=1 "));
  assert(transfer.configured && !configuration.failed && (returned ? (configuration.finished && !configuration.issued) : (!configuration.finished && configuration.issued==FES_NANO_CONFIG_ISDONE)));
  assert(observed.completed==configuration.bytes && observed.acknowledged==configuration.bytes && !observed.outstanding && !observed.pending);
 }else{
  if(trial_case==123)fprintf(stderr,"FINAL_SCOPE_COUNTEREXAMPLE event_producer=%u %s",registers[44],line);
  assert(strstr(line," proof=0 ") && !strstr(line," status=0 "));
 }
 if(!trial_case){
  if(!retire_fault && !(FES_TEST_RETIRE_COST_MODE && source_retirement.failed)){assert(source_retirement.retired && source_retirement.command_receipt.commands_completed && source_retirement.command_receipt.producer==12);assert(smmu_writes==17 && registers[0x20/4]==0 && registers[0x44/4]==0x100000);}
  else {if(!source_retirement.failed || source_retirement.retired)fprintf(stderr,"RETIRE_CASE %u attempted=%u failed=%u retired=%u scope=%u source_failed=%u\n%s",retire_fault,source_retirement.attempted,source_retirement.failed,source_retirement.retired,sdm_probe_scope_reason,sdm_source_failed,terminal_log);assert(source_retirement.failed && !source_retirement.continued && (!source_retirement.retired || FES_TEST_RETIRE_COST_MODE));}
  unsigned before=smmu_writes;struct nano_retire_receipt again={0};assert(fes_nano_sdm_retire_source(&again));assert(smmu_writes==before && !again.translation_retired);
 }else assert(smmu_writes==12 || trial_case==4);
 assert(usb_writes==model.base.h.usb_writes && physical_requests==model.requests);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
}
static void run_joined(void){
 pid_t p=fork();assert(p>=0);if(!p){joined_case();_exit(0);}int status;assert(waitpid(p,&status,0)==p);
 if(!WIFEXITED(status) || WEXITSTATUS(status)){
  fprintf(stderr,"joined=%u trial=%u reverse=%u credits=%u block=%u\n",joined_cases,trial_case,reverse_order,fixture_credits,fixture_block);exit(1);
 }joined_cases++;
}

int mailbox_hps_stage_notify(uint32_t execution_stage)
{
	return mailbox_send_cmd(MBOX_JOB_ID, MBOX_HPS_STAGE_NOTIFY,
				&execution_stage, 1U, CMD_CASUAL,
				NULL, NULL);
}

static void lifecycle_case(unsigned kind){
 joined_setup();
 if(kind==1)prepare_attempted=1;
 if(kind==2)service_error=-13;
 if(kind==3){prepare_attempted=1;service_error=-5;}
 int status=mailbox_hps_stage_notify(1);
 if(!kind){assert(!status && !service_error && consumed==1);}
 else{assert(status && service_error && consumed==0 && stream_words==1);}
 assert(!sdm_arm_entered && !smmu_writes && !data_posts);
}
#ifndef FES_TEST_JOINED_MAIN
#define FES_TEST_JOINED_MAIN main
#endif
int FES_TEST_JOINED_MAIN(void){
 for(retire_fault=1;retire_fault<=14;retire_fault++){trial_case=0;fixture_credits=4;run_joined();}
 retire_fault=0;
 for(unsigned kind=0;kind<4;kind++){
  pid_t child=fork();assert(child>=0);if(!child){lifecycle_case(kind);_exit(0);}int status;assert(waitpid(child,&status,0)==child && WIFEXITED(status) && !WEXITSTATUS(status));
 }
 for(reverse_order=0;reverse_order<2;reverse_order++){
  for(fixture_credits=1;fixture_credits<=14;fixture_credits++){trial_case=0;run_joined();}
  fixture_credits=4;for(trial_case=1;trial_case<=9;trial_case++)run_joined();
  for(trial_case=100;trial_case<=123;trial_case++)run_joined();
 }
 printf("{\"status\":\"passed-startup-then-owned-full-transfer\",\"joined_cases\":%u,\"lifecycle_cases\":4,\"startup_notification_precedes_every_transfer\":true,\"actual_vendor_functions\":36,\"hardware_actions\":false}\n",joined_cases);
 return 0;
}
