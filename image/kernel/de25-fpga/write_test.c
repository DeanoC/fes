// SPDX-License-Identifier: GPL-2.0
// The actual patched write function is included below; only its environment is scripted.
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <limits.h>
#ifndef CONFIG_FES_DE25_DMA_PLATFORM
#define CONFIG_FES_DE25_DMA_PLATFORM 1
#endif
#define IS_ENABLED(x) (x)
#define NUM_SVC_BUFS 4
#define S10_BUFFER_TIMEOUT 5UL
#define msecs_to_jiffies(x) ((unsigned long)(x) / 1000UL)
#define time_after_eq(a,b) ((long)((a) - (b)) >= 0)
#define min(a,b) ((a) < (b) ? (a) : (b))
#define SVC_STATUS_BUFFER_DONE 0
#define SVC_STATUS_BUFFER_SUBMITTED 1
#define SVC_STATUS_ERROR 2
#define COMMAND_RECONFIG_DATA_CLAIM 3
#define dev_err(dev, ...) ((void)(dev))
struct device { int unused; };
struct completion { int unused; };
struct s10_priv { struct { struct device *dev; } client; struct completion status_return_completion; unsigned long status; void *chan; bool config_active; bool config_uncertain; unsigned long write_deadline; };
struct fpga_manager { struct s10_priv *priv; };
static unsigned long fake_jiffies;
static int clock_reads;
static int sends, waits, claims, dones, scenario;
static bool free_buffers;
static struct s10_priv priv;
static struct fpga_manager manager = { &priv };
static char data[8];
static inline unsigned long clock_now(void)
{
 if (scenario == 12) {
  clock_reads++;
  if (clock_reads == 1) return 0;
  if (clock_reads <= 3) return 29;
  return 31;
 }
 return fake_jiffies;
}
#define jiffies clock_now()
static void reinit_completion(struct completion *c) { (void)c; }
static bool test_and_clear_bit(int n, unsigned long *bits) { bool v = *bits & (1UL << n); *bits &= ~(1UL << n); return v; }
static void s10_receive_callback(void) {}
static int s10_send_buf(struct fpga_manager *m, const char *buf, size_t count)
{
 (void)m; assert(count <= sizeof(data)); assert(buf == data + sizeof(data) - count);
 assert(count > 0); if (sends < 1000000) sends++;
 if (scenario == 1) return -ENOMEM;
 if (scenario == 2 || scenario == 3) return -ENOBUFS;
 if (scenario == 7 && sends == 2) return -EIO;
 if (scenario == 10) fake_jiffies += 30;
 return count < 4 ? (int)count : 4;
}
static int s10_get_unlocked_buffer_count(struct fpga_manager *m) { (void)m; return free_buffers ? 4 : 0; }
static int s10_svc_send_msg(struct s10_priv *p, int cmd, void *payload, int size, void (*cb)(void))
{ (void)p; (void)payload; (void)size; (void)cb; assert(cmd == COMMAND_RECONFIG_DATA_CLAIM); claims++; return scenario == 8 ? -EIO : 0; }
static long wait_for_completion_timeout(struct completion *c, unsigned long timeout)
{
 (void)c; assert(timeout > 0 && timeout <= 5); if (scenario == 12) assert(timeout == 1); waits++;
 if (scenario == 2 || scenario == 6) { fake_jiffies += timeout; return 0; }
 fake_jiffies++;
 if (scenario == 9) return timeout; // completion with no recognized status
 if (scenario == 4 || scenario == 5) priv.status |= 1UL << SVC_STATUS_ERROR;
 if (claims || scenario == 5) { priv.status |= 1UL << SVC_STATUS_BUFFER_DONE; free_buffers = true; }
 else priv.status |= 1UL << SVC_STATUS_BUFFER_SUBMITTED;
 return timeout;
}
static void stratix10_svc_done(void *c) { (void)c; dones++; }
#include "write-function.c"
static void run(int which, int expected, int max_sends)
{
 scenario=which; clock_reads=0; sends=waits=claims=dones=0; free_buffers=false; priv.status=0; fake_jiffies=which==11 ? ULONG_MAX-2 : 0;
 priv.config_active = true; priv.config_uncertain = true; priv.write_deadline = clock_now() + 30;
 assert(s10_ops_write(&manager, data, sizeof(data)) == expected);
 if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM) && expected) assert(!priv.config_active && priv.config_uncertain);
 assert(sends <= max_sends); assert(dones == (expected != 0));
 if (which==1) assert(sends==1 && waits==0);
 if (which==2) assert(sends==1 && waits==1);
 if (which==3) assert(waits==sends && waits==30);
 if (which==4 || which==5) assert(sends==1 && waits==1);
 if (which==7) assert(sends==2 && waits==1);
 if (which==10) assert(sends==1 && waits==0);
 printf("case %d PASS\n",which);
}
static void fragmented_write(void)
{
 scenario=0; clock_reads=0; sends=waits=claims=dones=0; free_buffers=false;
 fake_jiffies=0; priv.status=0; priv.config_active=true; priv.config_uncertain=true;
 priv.write_deadline=30;
 assert(s10_ops_write(&manager, data, sizeof(data))==0);
 assert(priv.config_active && priv.config_uncertain);
 fake_jiffies=29; sends=waits=claims=0; free_buffers=false; priv.status=0;
 assert(s10_ops_write(&manager, data, sizeof(data))==-ETIMEDOUT);
 assert(!priv.config_active && priv.config_uncertain && priv.write_deadline==30);
 int old_sends=sends, old_dones=dones;
 assert(s10_ops_write(&manager, data, sizeof(data))==-EIO);
 assert(sends==old_sends && dones==old_dones);
 puts("case 13 PASS: fragments share one deadline; failure cannot resume");
}
int main(int argc, char **argv)
{
 if(argc>1) { if (!strcmp(argv[1], "positive")) run(0,0,2); else run(1,-ENOMEM,1); return 0; }
 run(0,0,2); run(1,-ENOMEM,1); run(2,-ETIMEDOUT,1);
 run(3,-ETIMEDOUT,30); run(4,-EFAULT,1); run(5,-EFAULT,1);
 run(6,-ETIMEDOUT,1); run(7,-EIO,2); run(8,-EIO,2);
 run(12,-ETIMEDOUT,1); run(9,-ETIMEDOUT,2); run(10,-ETIMEDOUT,1); run(11,0,2); fragmented_write();
 return 0;
}
