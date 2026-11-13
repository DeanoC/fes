// SPDX-License-Identifier: GPL-2.0-only
static void reset(void){
 if(initialized){assert(!pthread_mutex_destroy(&ctrl.de25_admission_lock));for(int i=0;i<4;i++)assert(!pthread_mutex_destroy(&channels[i].lock));}
 memset(&ctrl,0,sizeof(ctrl));memset(channels,0,sizeof(channels));
 ctrl.dev=&device;ctrl.chans=channels;ctrl.is_smmu_enabled=true;ctrl.use_dma_mem=true;assert(!pthread_mutex_init(&ctrl.de25_admission_lock,0));
 const char *names[4]={"fpga","rsu","fcs","hwm"};
 for(int i=0;i<4;i++){channels[i].name=names[i];channels[i].ctrl=&ctrl;clients[i].dev=&device;assert(!pthread_mutex_init(&channels[i].lock,0));}
 atomic_store(&module_refs,0);atomic_store(&backend_calls,0);module_allowed=true;svc_ctrl.empty=false;
 backend_entered=false;backend_release=true;backend_result=0;initialized=true;
}
static struct stratix10_svc_chan *own(void){struct stratix10_svc_chan *p=stratix10_svc_request_channel_byname(&clients[0],"fpga");assert(p==&channels[0]);return p;}
static void *block_thread(void *arg){return (void*)(intptr_t)stratix10_svc_block_dma(arg);}
static pthread_barrier_t request_barrier;
static void *request_thread(void *arg){int b=pthread_barrier_wait(&request_barrier);assert(b==0 || b==PTHREAD_BARRIER_SERIAL_THREAD);return stratix10_svc_request_channel_byname(arg,"fpga");}
int main(void){
 if(!IS_ENABLED(CONFIG_FES_DE25_SDM_CONTAINMENT)){
  reset();for(int i=0;i<4;i++)assert(stratix10_svc_request_channel_byname(&clients[i],channels[i].name)==&channels[i]);
  assert(ctrl.num_active_client==4 && atomic_load(&module_refs)==4);assert(stratix10_svc_block_dma(&channels[0])==-EOPNOTSUPP);
  for(int i=0;i<4;i++)stratix10_svc_free_channel(&channels[i]);assert(ctrl.num_active_client==0 && atomic_load(&module_refs)==0 && !atomic_load(&backend_calls));
  puts("PASS disabled original admission/free behavior; containment unsupported");return 0;
 }
 for(int failure=0;failure<2;failure++){
  reset();struct stratix10_svc_chan *p=own();backend_result=failure?-ETIMEDOUT:0;backend_release=false;pthread_t t;assert(!pthread_create(&t,0,block_thread,p));
  assert(!pthread_mutex_lock(&backend_lock));while(!backend_entered)assert(!pthread_cond_wait(&backend_cond,&backend_lock));assert(!pthread_mutex_unlock(&backend_lock));
  assert(stratix10_svc_block_dma(p)==-EINPROGRESS);
  assert(PTR_ERR(stratix10_svc_request_channel_byname(&clients[1],"rsu"))==-EIO);
  assert(PTR_ERR(stratix10_svc_request_channel_byname(&clients[0],"fpga"))==-EIO);
  stratix10_svc_free_channel(p);assert(p->scl==&clients[0] && ctrl.num_active_client==1 && atomic_load(&module_refs)==1);
  assert(!pthread_mutex_lock(&backend_lock));backend_release=true;assert(!pthread_cond_broadcast(&backend_cond));assert(!pthread_mutex_unlock(&backend_lock));void *result;assert(!pthread_join(t,&result));assert((intptr_t)result==backend_result);
  for(int i=0;i<3;i++)assert(stratix10_svc_block_dma(p)==backend_result);
  assert(atomic_load(&backend_calls)==1 && ctrl.de25_block_status==backend_result);
  assert(PTR_ERR(stratix10_svc_request_channel_byname(&clients[1],"rsu"))==-EIO);
  stratix10_svc_free_channel(p);assert(ctrl.num_active_client==1 && atomic_load(&module_refs)==1 && p->scl);
  puts(failure?"PASS failed block remains closed and cached":"PASS successful block remains closed and cached");
 }
 reset();struct stratix10_svc_chan *p=own();struct stratix10_svc_chan *foreign=stratix10_svc_request_channel_byname(&clients[1],"rsu");assert(foreign==&channels[1]);
 assert(stratix10_svc_block_dma(p)==-EBUSY && !ctrl.de25_block_started && atomic_load(&backend_calls)==0);stratix10_svc_free_channel(foreign);stratix10_svc_free_channel(p);assert(!ctrl.num_active_client && !atomic_load(&module_refs));puts("PASS prior foreign admission refuses containment without mutation");
 reset();module_allowed=false;assert(PTR_ERR(stratix10_svc_request_channel_byname(&clients[0],"fpga"))==-EBUSY);assert(!ctrl.num_active_client && !atomic_load(&module_refs));module_allowed=true;p=own();stratix10_svc_free_channel(p);assert(!ctrl.num_active_client);puts("PASS module refusal releases admission lock");
 for(int round=0;round<25;round++){
  reset();assert(!pthread_barrier_init(&request_barrier,0,3));pthread_t a,b;assert(!pthread_create(&a,0,request_thread,&clients[0]));assert(!pthread_create(&b,0,request_thread,&clients[1]));int br=pthread_barrier_wait(&request_barrier);assert(br==0 || br==PTHREAD_BARRIER_SERIAL_THREAD);
  void *ra,*rb;assert(!pthread_join(a,&ra));assert(!pthread_join(b,&rb));assert(!pthread_barrier_destroy(&request_barrier));assert((ra==&channels[0]) != (rb==&channels[0]));assert(PTR_ERR(ra)==-EBUSY || PTR_ERR(rb)==-EBUSY);assert(ctrl.num_active_client==1 && atomic_load(&module_refs)==1);stratix10_svc_free_channel(&channels[0]);assert(!ctrl.num_active_client && !atomic_load(&module_refs));
 }
 puts("PASS25 concurrent same-channel admissions elect one owner");return 0;
}
