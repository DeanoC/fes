// SPDX-License-Identifier: GPL-2.0-only
// Executes the pinned manager's actual init/write/complete functions.
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include <errno.h>
#ifndef CONFIG_FES_DE25_DMA_PLATFORM
#define CONFIG_FES_DE25_DMA_PLATFORM 1
#endif
#define IS_ENABLED(x) (x)
#define NUM_SVC_BUFS 4
#define S10_RECONFIG_TIMEOUT 5UL
#define S10_BUFFER_TIMEOUT 5UL
#define msecs_to_jiffies(x) ((unsigned long)(x)/1000UL)
#define time_after_eq(a,b) ((long)((a)-(b)) >= 0)
#define min(a,b) ((a)<(b)?(a):(b))
#define BIT(x) (1UL << (x))
#define dev_err(dev,...) ((void)(dev))
#define dev_dbg(dev,...) ((void)(dev))
#define FPGA_MGR_PARTIAL_RECONFIG 1
#define FPGA_MGR_BITSTREAM_AUTHENTICATE 2
#define COMMAND_RECONFIG_FLAG_PARTIAL 0
#define COMMAND_AUTHENTICATE_BITSTREAM 1
#define INVALID_FIRMWARE_VERSION 65535
#define COMMAND_RECONFIG 1
#define COMMAND_RECONFIG_DATA_SUBMIT 2
#define COMMAND_RECONFIG_DATA_CLAIM 3
#define COMMAND_RECONFIG_STATUS 4
#define SVC_STATUS_OK 0
#define SVC_STATUS_BUFFER_DONE 1
#define SVC_STATUS_BUFFER_SUBMITTED 2
#define SVC_STATUS_COMPLETED 3
#define SVC_STATUS_ERROR 4
typedef unsigned int uint;
struct device { int unused; };
struct completion { int unused; };
struct stratix10_svc_command_config_type { unsigned long flags; };
struct s10_priv {
 struct { struct device *dev; } client;
 void *chan;
 struct completion status_return_completion;
 struct { unsigned long lock; } svc_bufs[NUM_SVC_BUFS];
 unsigned long status, write_deadline;
 unsigned int fw_version;
 bool config_uncertain, config_active;
};
struct fpga_manager { struct s10_priv *priv; };
struct fpga_image_info { unsigned long flags; };
static unsigned long jiffies;
static int mode, commands, writes, waits, joins, last_command;
static bool control_mode;
static struct s10_priv priv;
static struct fpga_manager manager={&priv};
static struct fpga_image_info info;
static void reinit_completion(struct completion *c) { (void)c; }
static bool test_and_clear_bit(int b,unsigned long *v) { bool r=*v & BIT(b); *v &= ~BIT(b); return r; }
static int s10_get_unlocked_buffer_count(struct fpga_manager *m)
{ int n=0; for(int i=0;i<4;i++) if(!m->priv->svc_bufs[i].lock)n++; return n; }
static void s10_receive_callback(void) {}
static int s10_svc_send_msg(struct s10_priv *p,int cmd,void *payload,size_t len,void (*cb)(void))
{
 (void)p;(void)payload;(void)len;(void)cb;commands++;last_command=cmd;
 if((mode==1 && cmd==COMMAND_RECONFIG)||(mode==5 && cmd==COMMAND_RECONFIG_STATUS))return -ENOMEM;
 if(mode==9 && cmd==COMMAND_RECONFIG_STATUS)jiffies+=5;
 return 0;
}
static int s10_send_buf(struct fpga_manager *m,const char *buf,size_t count)
{
 (void)buf;writes++;last_command=COMMAND_RECONFIG_DATA_SUBMIT;
 if(mode==4)return -EIO;
 m->priv->svc_bufs[0].lock=1;
 return count<4?(int)count:4;
}
static long wait_for_completion_timeout(struct completion *c,unsigned long timeout)
{
 (void)c;assert(timeout && timeout<=5);waits++;
 if((mode==2 && last_command==COMMAND_RECONFIG)||(mode==6 && last_command==COMMAND_RECONFIG_STATUS)) {jiffies+=timeout;return 0;}
 jiffies++;
 if(last_command==COMMAND_RECONFIG) {
  priv.status|=BIT(SVC_STATUS_OK);
  if(mode==3)priv.status|=BIT(SVC_STATUS_ERROR);
 } else if(last_command==COMMAND_RECONFIG_DATA_SUBMIT)priv.status|=BIT(SVC_STATUS_BUFFER_SUBMITTED);
 else if(last_command==COMMAND_RECONFIG_DATA_CLAIM) {
  priv.status|=BIT(SVC_STATUS_BUFFER_DONE);
  for(int i=0;i<4;i++)priv.svc_bufs[i].lock=0;
 } else {
  assert(last_command==COMMAND_RECONFIG_STATUS);
  if(mode!=10)priv.status|=BIT(SVC_STATUS_COMPLETED);
  if(mode==7)priv.status|=BIT(SVC_STATUS_ERROR);
 }
 return timeout;
}
static void stratix10_svc_done(void *c)
{
 (void)c;joins++;
 if(last_command==COMMAND_RECONFIG_STATUS && mode==11)priv.status|=BIT(SVC_STATUS_ERROR);
 if(last_command==COMMAND_RECONFIG_STATUS && mode==12)priv.svc_bufs[0].lock=1;
 // A host thread join is never itself permission to drop uncertainty.
 if(IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM) && !control_mode)assert(priv.config_uncertain);
}
#include "retention-functions.c"
static void reset(void)
{
 memset(&priv,0,sizeof(priv));priv.fw_version=1;info.flags=0;
 jiffies=0;commands=writes=waits=joins=last_command=mode=0;
}
static void refused_again(void)
{
 int old_commands=commands,old_waits=waits,old_joins=joins,old_writes=writes;
 unsigned long locks[4];for(int i=0;i<4;i++)locks[i]=priv.svc_bufs[i].lock;
 struct fpga_manager fresh_caller={&priv};
 assert(priv.config_uncertain && !priv.config_active);
 assert(s10_ops_write_init(&fresh_caller,&info,NULL,0)==-EIO);
 assert(s10_ops_write_complete(&fresh_caller,&info)==-EIO);
 assert(s10_ops_write(&fresh_caller,"x",1)==-EIO);
 assert(commands==old_commands && waits==old_waits && joins==old_joins && writes==old_writes);
 for(int i=0;i<4;i++)assert(locks[i]==priv.svc_bufs[i].lock);
}
static void successful_cycles(void)
{
 reset();
 for(int cycle=0;cycle<2;cycle++) {
  assert(s10_ops_write_init(&manager,&info,NULL,0)==0);
  if(IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM))assert(priv.config_uncertain && priv.config_active);
  assert(s10_ops_write(&manager,"12345678",8)==0);
  assert(s10_ops_write_complete(&manager,&info)==0);
  assert(!priv.config_uncertain && !priv.config_active);
 }
 assert(joins==2);
 puts("two successful programming cycles release only after completion and join");
}
int main(int argc, char **argv)
{
 (void)argv;
 if(argc>1) {
  control_mode=true;reset();mode=1;priv.svc_bufs[0].lock=1;
  assert(s10_ops_write_init(&manager,&info,NULL,0)==-ENOMEM);
  mode=0;struct fpga_manager fresh={&priv};
  int retry=s10_ops_write_init(&fresh,&info,NULL,0);
  assert(retry == -EIO && "failed load must refuse a new caller");
  return 0;
 }
 successful_cycles();
 if(!IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM))return 0;
 for(int test=1;test<=3;test++) {
  reset();mode=test;priv.svc_bufs[0].lock=1;
  int expected=test==1?-ENOMEM:(test==2?-ETIMEDOUT:-EFAULT);
  assert(s10_ops_write_init(&manager,&info,NULL,0)==expected);
  assert(priv.svc_bufs[0].lock==1);refused_again();
  printf("init failure %d retains locks and rejects a fresh caller\n",test);
 }
 reset();assert(s10_ops_write_init(&manager,&info,NULL,0)==0);
 mode=4;priv.svc_bufs[0].lock=1;
 assert(s10_ops_write(&manager,"x",1)==-EIO);refused_again();
 puts("data failure retains kernel state and rejects complete/retry");
 for(int test=5;test<=12;test++) {
  reset();assert(s10_ops_write_init(&manager,&info,NULL,0)==0);
  assert(s10_ops_write(&manager,"x",1)==0);
  mode=test;if(test==8)priv.svc_bufs[0].lock=1;
  int expected=test==5?-ENOMEM:((test==6||test==9||test==10)?-ETIMEDOUT:((test==7||test==11)?-EFAULT:-EIO));
  assert(s10_ops_write_complete(&manager,&info)==expected);refused_again();
  printf("completion failure %d retains uncertainty without a clear path\n",test);
 }
 reset();priv.fw_version=INVALID_FIRMWARE_VERSION;info.flags=FPGA_MGR_BITSTREAM_AUTHENTICATE;
 assert(s10_ops_write_init(&manager,&info,NULL,0)==-EINVAL);
 assert(!priv.config_uncertain && !priv.config_active && !commands && !waits && !joins);
 info.flags=0;assert(s10_ops_write_init(&manager,&info,NULL,0)==0);
 puts("validation before a request leaves an untouched manager reusable");
 return 0;
}
