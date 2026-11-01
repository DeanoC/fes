/* SPDX-License-Identifier: BSD-3-Clause */
/* Synthetic SDM at the hardware register boundary; driver and engine above are real. */
static uint32_t registers[0x500/4];
static unsigned mode,doorbells,sent,acknowledged,bytes,command_wraps,response_wraps;
static unsigned mmio_writes,mmio_reads,observations;
static bool observing;
static unsigned queue[64],qread,qwrite,delays;
static uint32_t *reg(unsigned off) { assert(off<sizeof(registers) && !(off&3));return &registers[off/4]; }
static void reply_word(uint32_t word) {
 uint32_t *rin=reg(MBOX_RIN),rout=*reg(MBOX_ROUT),next=(*rin+1)%MBOX_RESP_BUFFER_SIZE;
 assert(next!=rout);*reg(MBOX_RESP_BUFFER+4*(*rin))=word;
 if(!next)response_wraps++;
 *rin=next;*reg(MBOX_DOORBELL_FROM_SDM)=1;
}
static void reply(unsigned job,unsigned error,unsigned n,const uint32_t *args) {
 reply_word(MBOX_CLIENT_ID_CMD(1)|MBOX_JOB_ID_CMD(job)|MBOX_CMD_LEN_CMD(n)|error);
 for(unsigned i=0;i<n;i++)reply_word(args[i]);
}
static void advance(void) {
 if(qread==qwrite)return;
 if(mode==1 && ++delays%3)return;
 if(mode==2 && acknowledged==12)return; /* peer stops replying, not a parser defect */
 if(mode==7 && acknowledged>=12)return;
 if(mode>=5 && acknowledged>=12 && qwrite-qread<2 && bytes<2027520)return;
 unsigned job=queue[qread++%64];
 unsigned error=acknowledged==12 ? (mode==3 ? 17 : (mode==4 || mode==6) ? 2 : 0) : 0;
 if(mode==8 && acknowledged==13)error=2;
 reply(job,error,0,NULL);acknowledged++;
}
static void consume(void) {
 uint32_t *cout=reg(MBOX_COUT),cin=*reg(MBOX_CIN);
 while(*cout!=cin) {
  uint32_t h=*reg(MBOX_CMD_BUFFER+4*(*cout));unsigned job=MBOX_RESP_JOB_ID(h),n=MBOX_RESP_LEN(h);
  *cout=(*cout+1)%MBOX_CMD_BUFFER_SIZE;if(!*cout)command_wraps++;
  uint32_t args[8];assert(n<=8);
  for(unsigned i=0;i<n;i++) { assert(*cout!=cin);args[i]=*reg(MBOX_CMD_BUFFER+4*(*cout));*cout=(*cout+1)%MBOX_CMD_BUFFER_SIZE;if(!*cout)command_wraps++; }
  switch(MBOX_CMD_MASK(h)) {
  case MBOX_CMD_FPGA_CONFIG_COMP:
  case MBOX_CMD_CANCEL:assert(!n);reply(job,0,0,NULL);break;
  case MBOX_RECONFIG: { uint32_t limits[2]={4,1048560};assert(!n);reply(job,0,2,limits);break; }
  case MBOX_RECONFIG_DATA:
   assert(n==3 && (h&MBOX_INDIRECT(1)) && args[0]==256);
   assert(args[1]==0x14800000+bytes && args[2] && args[2]<=65536);
   assert(job==sent%14);bytes+=args[2];sent++;queue[qwrite++%64]=job;break;
  default:assert(0);
  }
 }
}
uint32_t mmio_read_32(uintptr_t addr) {
 if(addr==BOOT_SCRATCH_POR_1)return 0;
 assert(addr>=MBOX_OFFSET);mmio_reads++;
 if(observing)assert(addr==MBOX_OFFSET+MBOX_CIN || addr==MBOX_OFFSET+MBOX_COUT || addr==MBOX_OFFSET+MBOX_RIN || addr==MBOX_OFFSET+MBOX_ROUT || addr==MBOX_OFFSET+MBOX_DOORBELL_FROM_SDM || addr==MBOX_OFFSET+MBOX_STATUS);
 return *reg(addr-MBOX_OFFSET);
}
void mmio_write_32(uintptr_t addr,uint32_t value) {
 assert(!observing);mmio_writes++;
 if(addr==SDM_BE_ARADDR_REMAP || addr==SDM_BE_AWADDR_REMAP) { assert(value==DRAM_BASE);return; }
 assert(addr>=MBOX_OFFSET);unsigned off=addr-MBOX_OFFSET;
 if(off==MBOX_DOORBELL_TO_SDM) { assert(value==1);doorbells++;consume();return; }
 if(off==MBOX_CIN)assert(value<MBOX_CMD_BUFFER_SIZE);
 if(off==MBOX_ROUT)assert(value<MBOX_RESP_BUFFER_SIZE);
 *reg(off)=value;
}
void udelay(unsigned n) { (void)n;advance(); }
void mdelay(unsigned n) { (void)n;advance(); }
int mailbox_init(void) { assert(!"unexpected driver reset");return MBOX_TIMEOUT; }
int socfpga_bridges_disable(uint32_t mask) { assert(mask==~0U);return 0; }
static void test_notice(const char *format,...) {
 char line[512];va_list args;va_start(args,format);int n=vsnprintf(line,sizeof(line),format,args);va_end(args);
 assert(n>0 && (size_t)n<sizeof(line));
 assert(strstr(line,"FES_NANO_CORE_MAILBOX phase=3 owner=42 generation=1 ")==line);
 observations++;
}
static void observe(unsigned completed,unsigned pending) {
 unsigned writes=mmio_writes,reads=mmio_reads;uint32_t before[sizeof(registers)/4];
 memcpy(before,registers,sizeof(before));mailbox_service_t service=mbox_svc;
 observing=true;fes_nano_mailbox_observe(3,completed,pending,send_id,0x42,1);observing=false;
 assert(mmio_writes==writes && mmio_reads==reads+6 && !mbox_db_lock);
 assert(!memcmp(before,registers,sizeof(before)) && !memcmp(&service,&mbox_svc,sizeof(service)));
}
static int windowed(void) {
 unsigned accepted=0,done=0,pending=0,polls=0,total=2027520;
 while(done<total) {
  while(accepted<total && pending<4) {
   unsigned amount=total-accepted<65536 ? total-accepted:65536;
   int status=intel_fpga_config_write(0x94800000+accepted,amount);
   assert(status==(pending==3 ? 1:0));accepted+=amount;pending++;
  }
  unsigned tries=0;
  for(;;) {
   for(unsigned i=0;i<3;i++)advance();
   uint32_t addresses[3]={0},count=0,job=0;
   int status=intel_fpga_config_completed_write(addresses,&count,&job);polls++;
   observe(done,accepted-done);
   if(status==4) {
    assert((mode==6 || mode==8) && done==786432 && !count && pending==4 && fes_contained_transport_status==-2);
    assert(request_type==NO_REQUEST);goto stopped_window;
   }
   if(!count) {
    assert(status==1 && pending);
    if(++tries==10) {assert(mode==7 && done==786432 && pending==4 && !fes_contained_transport_status);goto stopped_window;}
    continue;
   }
   assert(status==0 && count<=3 && count<=pending);
   for(unsigned i=0;i<count;i++) {
    assert(addresses[i]==0x14800000+done);
    unsigned amount=total-done<65536 ? total-done:65536;done+=amount;pending--;
   }
   break;
  }
 }
 assert(mode==5 && done==total && accepted==total && !pending && sent==31 && acknowledged==31 && max_blocks==4);
stopped_window:
 assert(observations==polls);
 printf("window mode=%u completed=%u accepted=%u pending=%u submitted=%u ack=%u polls=%u status=%d\n",mode,done,accepted,pending,sent,acknowledged,polls,fes_contained_transport_status);
 return 0;
}
int main(int argc,char **argv) {
 assert(argc==3);mode=(unsigned)strtoul(argv[1],NULL,10);unsigned initial=(unsigned)strtoul(argv[2],NULL,10);
 assert(initial<16 && mode<9);*reg(MBOX_RIN)=*reg(MBOX_ROUT)=initial;*reg(MBOX_CIN)=*reg(MBOX_COUT)=30;
 mailbox_init_v3();assert(intel_fpga_config_start(0)==0);assert(max_blocks==4 && bytes_per_block==1048560);
 if(mode>=5)return windowed();
 unsigned done=0,polls=0,total=2027520;
 while(done<total) {
  unsigned chunk=total-done<65536 ? total-done:65536;
  assert(intel_fpga_config_write(0x94800000+done,chunk)==0);
  uint32_t addresses[3]={0},count=0,job=0;int status;
  for(unsigned tries=0;;tries++) {
   assert(tries<=10);advance();count=0;
   status=intel_fpga_config_completed_write(addresses,&count,&job);polls++;observe(done,count?0:chunk);
   if(status==4) { assert((mode==3 || mode==4) && done==786432 && !count && fes_contained_transport_status==(mode==3 ? -17:-2));goto stopped; }
   if(count) { assert(status==0 && count==1 && addresses[0]==0x14800000+done);break; }
   assert(status==1);
   if(mode==2 && tries==10) { assert(done==786432 && !fes_contained_transport_status);goto stopped; }
  }
  done+=chunk;
 }
 assert(mode<2 && bytes==total && sent==31 && acknowledged==31 && max_blocks==4);
 assert(command_wraps>=3 && response_wraps>=2);
stopped:
 assert(observations==polls);
 printf("mode=%u initial=%u completed=%u submitted=%u ack=%u polls=%u cmd_wraps=%u resp_wraps=%u status=%d\n",mode,initial,done,sent,acknowledged,polls,command_wraps,response_wraps,fes_contained_transport_status);
 return 0;
}
