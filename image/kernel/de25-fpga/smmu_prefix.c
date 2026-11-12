// SPDX-License-Identifier: GPL-2.0-only

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>
void *memcpy(void *dst,const void *src,size_t n){unsigned char *d=dst;const unsigned char *s=src;for(size_t i=0;i<n;i++)d[i]=s[i];return dst;}
void *memset(void *dst,int v,size_t n){unsigned char *d=dst;for(size_t i=0;i<n;i++)d[i]=(unsigned char)v;return dst;}
static void emit(const char *s) {
 size_t n=0;while(s[n])n++;
#if defined(__aarch64__)
 register long x0 asm("x0")=1;register const char *x1 asm("x1")=s;register size_t x2 asm("x2")=n;register long x8 asm("x8")=64;
 asm volatile("svc 0" : "+r"(x0) : "r"(x1),"r"(x2),"r"(x8) : "memory");
#elif defined(__x86_64__)
 long ret;asm volatile("syscall" : "=a"(ret) : "a"(1L),"D"(1L),"S"(s),"d"(n) : "rcx","r11","memory");
#else
#error Unsupported fixture CPU
#endif
}
static __attribute__((noreturn)) void finish(int code) {
#if defined(__aarch64__)
 register long x0 asm("x0")=code;register long x8 asm("x8")=93;
 asm volatile("svc 0" :: "r"(x0),"r"(x8) : "memory");
#else
 asm volatile("syscall" :: "a"(60L),"D"((long)code) : "rcx","r11","memory");
#endif
 __builtin_unreachable();
}
#define CHECK(x) do {if(!(x)){emit("FAIL " #x "\n");finish(90);}}while(0)

typedef uint32_t u32;
typedef uint64_t __le64;
#define IS_ENABLED(x) (x)
#ifndef CONFIG_FES_DE25_DMA_PLATFORM
#define CONFIG_FES_DE25_DMA_PLATFORM 1
#endif
#define __maybe_unused __attribute__((unused))
#define ENODEV 19
#define EINVAL 22
#define EOPNOTSUPP 95
#define EXDEV 18
#define ETIMEDOUT 110
#define EIO 5
#define STRTAB_STE_0_V 1UL
#define STRTAB_STE_0_CFG 14ULL
#define STRTAB_STE_0_CFG_ABORT 0
#define FIELD_PREP(mask,v) (((uint64_t)(v)<<__builtin_ctzll(mask))&(mask))
#define cpu_to_le64(v) (v)
#define CMDQ_OP_CFGI_STE 3
#define container_of(p,type,member) ((type*)((char*)(p)-offsetof(type,member)))
struct arm_smmu_ste{__le64 data[8];};
struct device{void *of_node;bool pci;struct dev_iommu *iommu;struct iommu_group *group;struct iommu_domain *domain;};
struct iommu_domain{int type;};
struct arm_smmu_stream{u32 id;};
struct arm_smmu_cdtable{bool in_use;};
struct arm_smmu_device{struct arm_smmu_ste *entry;unsigned options;};
struct arm_smmu_master{struct arm_smmu_device *smmu;struct device *dev;struct arm_smmu_stream *streams;unsigned num_streams;bool ats_enabled,ste_ats_enabled,stall_enabled,sva_enabled,iopf_enabled,de25_block_attempted;int de25_block_status;struct arm_smmu_cdtable cd_table;};
struct arm_smmu_entry_writer;
struct arm_smmu_entry_writer_ops{void (*get_used)(const __le64*,__le64*);void (*sync)(struct arm_smmu_entry_writer*);};
struct arm_smmu_entry_writer{const struct arm_smmu_entry_writer_ops *ops;struct arm_smmu_master *master;};
struct arm_smmu_cmdq_ent{int opcode;struct{u32 sid;bool leaf;}cfgi;struct{u32 sid;}prefetch;};
static int arm_smmu_asid_lock,compatible_node,bad_node,lock_held;
static int responses[4],calls,entry_writes,update_steps;
static struct arm_smmu_ste entry,other_entry;
static struct arm_smmu_device smmu;
static struct device dev;
static struct arm_smmu_stream stream;
static struct arm_smmu_master master;
static bool of_device_is_compatible(void *node,const char *text){(void)text;return node==&compatible_node;}
static bool arm_smmu_ssids_in_use(struct arm_smmu_cdtable *t){return t->in_use;}
static struct arm_smmu_ste *arm_smmu_get_step_for_sid(struct arm_smmu_device *s,u32 sid){CHECK(s==&smmu && sid==10);return s->entry;}
#define lockdep_assert_held(p) CHECK((p)==&arm_smmu_asid_lock && lock_held)
static void arm_smmu_get_ste_used(const __le64 *e,__le64 *u){(void)e;(void)u;}
static int arm_smmu_cmdq_issue_cmd_with_sync(struct arm_smmu_device *s,struct arm_smmu_cmdq_ent *c){
 CHECK(s==&smmu && c->opcode==3 && c->cfgi.sid==10 && c->cfgi.leaf && calls<4);
 return responses[calls++];
}
static void arm_smmu_write_entry(struct arm_smmu_entry_writer *w,__le64 *e,const __le64 *target){
 CHECK(w->master==&master && e==entry.data && target[0]==1);for(int i=1;i<8;i++)CHECK(target[i]==0);
 entry_writes++;
 for(int i=0;i<update_steps;i++)w->ops->sync(w);
 for(int i=0;i<8;i++)e[i]=target[i];
}

#define EINPROGRESS 115
#define EBUSY 16
#define IOMMU_DOMAIN_DMA 4
#define EXPORT_SYMBOL_GPL(x)
#define READ_ONCE(x) (x)
#define le64_to_cpu(x) (x)
#define NUM_ENTRY_QWORDS 8
#define ARM_SMMU_OPT_SKIP_PREFETCH 1
#define CMDQ_OP_PREFETCH_CFG 1
static int arm_smmu_ops,other_ops;
struct iommu_fwspec{unsigned num_ids;u32 ids[1];};
struct iommu_device{const void *ops;};
struct dev_iommu{struct iommu_device *iommu_dev;struct iommu_fwspec *spec;struct arm_smmu_master *priv;};
struct iommu_group{struct device *members[2];unsigned count;int refs;};
static struct iommu_device idev;
static struct dev_iommu dinfo;
static struct iommu_fwspec spec;
static struct iommu_group group;
static struct iommu_domain domain;
static struct device other_dev;
static int group_locked,force_asid_busy,group_gets,group_puts;
static bool dev_is_pci(struct device *d){return d->pci;}
static struct iommu_group *iommu_group_get(struct device *d){group_gets++;if(d->group)d->group->refs++;return d->group;}
static void iommu_group_put(struct iommu_group *g){CHECK(g->refs==1);g->refs--;group_puts++;}
static int iommu_group_for_each_dev(struct iommu_group *g,void *data,int (*cb)(struct device*,void*)){
 CHECK(g==&group && g->refs==1 && !group_locked);group_locked=1;int ret=0;
 for(unsigned i=0;i<g->count;i++){ret=cb(g->members[i],data);if(ret)break;}
 group_locked=0;return ret;
}
static struct iommu_fwspec *dev_iommu_fwspec_get(struct device *d){return d->iommu?d->iommu->spec:0;}
static void *dev_iommu_priv_get(struct device *d){return d->iommu?d->iommu->priv:0;}
static struct iommu_domain *iommu_get_domain_for_dev(struct device *d){CHECK(group_locked);return d->domain;}
static bool mutex_trylock(int *p){CHECK(group_locked && p==&arm_smmu_asid_lock);if(force_asid_busy)return false;CHECK(!lock_held);lock_held=1;return true;}
static void mutex_unlock(int *p){CHECK(p==&arm_smmu_asid_lock && group_locked && lock_held);lock_held=0;}
static int arm_smmu_cmdq_issue_cmd(struct arm_smmu_device *s,struct arm_smmu_cmdq_ent *c){(void)s;(void)c;CHECK(0);return -5;}
int strcmp(const char *a,const char *b){while(*a && *a==*b){a++;b++;}return (unsigned char)*a-(unsigned char)*b;}
#define SVC_CLIENT_FPGA "fpga"
struct stratix10_svc_controller{struct device *dev;bool is_smmu_enabled,use_dma_mem;int num_active_client;};
struct stratix10_svc_chan{struct stratix10_svc_controller *ctrl;const char *name;};
static struct stratix10_svc_controller ctrl;
static struct stratix10_svc_chan chan;
