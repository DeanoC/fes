// SPDX-License-Identifier: GPL-2.0-only
#if defined(__x86_64__)
asm(".global _start\n_start:\nandq $-16,%rsp\ncall run_cases\nud2\n");
#else
asm(".global _start\n_start:\nbl run_cases\nb .\n");
#endif
static void reset(void){
 (void)&bad_node;(void)&other_entry;(void)&other_ops;(void)&other_dev;
 entry=(struct arm_smmu_ste){0};smmu=(struct arm_smmu_device){.entry=&entry,.options=1};
 domain.type=IOMMU_DOMAIN_DMA;spec=(struct iommu_fwspec){.num_ids=1,.ids={10}};idev.ops=&arm_smmu_ops;
 dinfo=(struct dev_iommu){.iommu_dev=&idev,.spec=&spec,.priv=&master};
 dev=(struct device){.of_node=&compatible_node,.iommu=&dinfo,.domain=&domain,.group=&group};stream.id=10;
 master=(struct arm_smmu_master){.smmu=&smmu,.dev=&dev,.streams=&stream,.num_streams=1};
 group=(struct iommu_group){.members={&dev},.count=1};
 ctrl=(struct stratix10_svc_controller){.dev=&dev,.is_smmu_enabled=true,.use_dma_mem=true,.num_active_client=1};chan=(struct stratix10_svc_chan){.ctrl=&ctrl,.name="fpga"};
 for(int i=0;i<4;i++)responses[i]=0;
 calls=entry_writes=group_gets=group_puts=group_locked=force_asid_busy=lock_held=0;update_steps=2;
}
void run_cases(void){
#if !CONFIG_FES_DE25_DMA_PLATFORM
 reset();CHECK(stratix10_svc_block_dma(0)==-95 && stratix10_svc_block_dma(&chan)==-95 && group_gets==0 && calls==0);emit("PASS profile disabled refuses without access\n");finish(0);
#else
 for(int i=0;i<23;i++){
  reset();int wanted=0;emit("CASE SMMU_boundary\n");
  switch(i){
   case 0:break;
   case 1:responses[0]=-110;wanted=-110;break;
   case 2:responses[2]=-5;wanted=-5;break;
   case 3:force_asid_busy=1;wanted=-16;break;
   case 4:idev.ops=&other_ops;wanted=-19;break;
   case 5:domain.type=9;wanted=-22;break;
   case 6:dev.group=0;wanted=-19;break;
   case 7:spec.ids[0]=11;wanted=-22;break;
   case 8:group.members[0]=&other_dev;wanted=-19;break;
   case 9:dev.pci=true;wanted=-19;break;
   case 10:dev.of_node=&bad_node;wanted=-19;break;
   case 11:stream.id=11;wanted=-22;break;
   case 12:master.sva_enabled=true;wanted=-95;break;
   case 13:spec.num_ids=2;wanted=-22;break;
   case 14:dev.iommu=0;wanted=-19;break;
   case 15:dinfo.iommu_dev=0;wanted=-19;break;
   case 16:dinfo.spec=0;wanted=-22;break;
   case 17:dinfo.priv=0;wanted=-22;break;
   case 18:master.stall_enabled=true;wanted=-95;break;
   case 19:master.ats_enabled=true;wanted=-95;break;
   case 20:case 21:case 22:responses[0]=-110;wanted=-110;break;
  }
  int ret=fes_de25_sdm_block_dma(&dev);CHECK(ret==wanted && group.refs==0 && !group_locked && !lock_held);
  if(i==0 || i==1 || i==2 || i>=20){
   CHECK(calls==3 && entry_writes==1 && master.de25_block_attempted && master.de25_block_status==wanted);
   for(int j=0;j<4;j++)responses[j]=0;
   ret=fes_de25_sdm_block_dma(&dev);CHECK(ret==wanted && calls==3 && entry_writes==1);
   if(i>=20){
    struct arm_smmu_ste target={0};
    if(i==21)target.data[0]=9;
    if(i==22){target.data[0]=1;target.data[4]=7;}
    arm_smmu_write_ste(&master,10,&entry,&target);CHECK(calls==3 && entry_writes==1 && master.de25_block_status==wanted);
   }
  } else CHECK(calls==0 && entry_writes==0 && !master.de25_block_attempted);
 }
 emit("PASS23 SMMU boundary cases; injected kernel locks, group iterator, entry writer and CMD_SYNC; no hardware fence\n");finish(0);
#endif
}
