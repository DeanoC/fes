// SPDX-License-Identifier: GPL-2.0-only
#include <assert.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <stdint.h>
#include <pthread.h>
#include <stdatomic.h>
#ifndef CONFIG_FES_DE25_DMA_PLATFORM
#define CONFIG_FES_DE25_DMA_PLATFORM 1
#endif
#define IS_ENABLED(x) (x)
#ifndef CONFIG_FES_DE25_SDM_CONTAINMENT
#define CONFIG_FES_DE25_SDM_CONTAINMENT 1
#endif
#define EXPORT_SYMBOL_GPL(x)
#define SVC_CLIENT_FPGA "fpga"
#define SVC_NUM_CHANNEL 4
#define EPROBE_DEFER 517
#define ERR_PTR(x) ((void*)(intptr_t)(x))
#define PTR_ERR(x) ((intptr_t)(x))
#define IS_ERR(x) ((uintptr_t)(x)>=(uintptr_t)-4095)
#define dev_err(d,...) ((void)(d))
#define dev_dbg(d,...) ((void)(d))
typedef pthread_mutex_t spinlock_t;
#define spin_lock_irqsave(l,f) do {(f)=0;assert(!pthread_mutex_lock(l));}while(0)
#define spin_unlock_irqrestore(l,f) do {(void)(f);assert(!pthread_mutex_unlock(l));}while(0)
struct driver {void *owner;};
struct device {struct driver *driver;};
struct stratix10_svc_client {struct device *dev;};
struct stratix10_svc_controller;
struct stratix10_svc_chan {const char *name;struct stratix10_svc_client *scl;struct stratix10_svc_controller *ctrl;spinlock_t lock;};
struct stratix10_svc_controller {struct device *dev;struct stratix10_svc_chan *chans;int num_active_client;bool is_smmu_enabled,use_dma_mem;spinlock_t de25_admission_lock;bool de25_block_started;int de25_block_status;};
static struct stratix10_svc_controller ctrl;
static struct stratix10_svc_chan channels[4];
static struct driver driver;
static struct device device={&driver};
static struct stratix10_svc_client clients[4];
static struct {bool empty;} svc_ctrl;
#define list_empty(p) ((p)->empty)
#define list_first_entry(p,type,member) (&ctrl)
static atomic_int module_refs,backend_calls;
static bool module_allowed=true,initialized;
static bool try_module_get(void *owner){(void)owner;if(!module_allowed)return false;atomic_fetch_add(&module_refs,1);return true;}
static void module_put(void *owner){(void)owner;assert(atomic_fetch_sub(&module_refs,1)>0);}
static pthread_mutex_t backend_lock=PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t backend_cond=PTHREAD_COND_INITIALIZER;
static bool backend_entered,backend_release=true;
static int backend_result;
static int fes_de25_sdm_block_dma(struct device *dev){
 assert(dev==&device);
 // The production backend can sleep. Admission lock must already be released.
 assert(!pthread_mutex_trylock(&ctrl.de25_admission_lock));
 assert(ctrl.de25_block_started);assert(!pthread_mutex_unlock(&ctrl.de25_admission_lock));
 atomic_fetch_add(&backend_calls,1);
 assert(!pthread_mutex_lock(&backend_lock));backend_entered=true;assert(!pthread_cond_broadcast(&backend_cond));
 while(!backend_release)assert(!pthread_cond_wait(&backend_cond,&backend_lock));
 assert(!pthread_mutex_unlock(&backend_lock));return backend_result;
}
