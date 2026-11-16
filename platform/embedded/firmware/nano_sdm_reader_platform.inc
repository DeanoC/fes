/* SPDX-License-Identifier: BSD-3-Clause */
/* The reviewed generated MPFE drives Secure stream1 independently of its
 * Non-secure AXI transaction attribute. This passive handoff preserves the
 * retired NS source abort and immutable storage; it changes no SMMU control. */
_Static_assert(FES_NANO_RETAINED_READER_ROUTE==1,"sealed secure-SID1 reader proof required");
static int retained_reader_scope(void *context)
{
 return context==&sdm_arm_probe.input && fes_nano_sdm_transfer_reader_ready()==1 &&
  retained_source_owned(&sdm_arm_probe.input,1)==1;
}
static int retained_reader_read(void *context,uint32_t offset,uint32_t *value)
{
 if(!value || !fes_nano_smmu_offset(offset) || retained_reader_scope(context)!=1)return -1;
 FES_PROBE_BARRIER();*value=FES_PROBE_READ32(UINT64_C(0x16000000)+offset);FES_PROBE_BARRIER();
 return retained_reader_scope(context)==1 ? 0:-1;
}
int fes_nano_sdm_source_reader_ready(void)
{
 const struct nano_sdm_input_binding *b=&sdm_arm_probe.input;
 struct fes_nano_smmu_observation observed={0};
 struct fes_nano_smmu_probe probe={.io={&sdm_arm_probe.input,retained_reader_scope,retained_reader_read}};
 int ready=1;
 for(unsigned pass=0;pass<2 && ready;pass++){
  probe=(struct fes_nano_smmu_probe){.io={&sdm_arm_probe.input,retained_reader_scope,retained_reader_read}};
  ready=fes_nano_sdm_transfer_reader_ready()==1 && retained_source_ready(b,1,1)==1 &&
   !fes_nano_smmu_observe(&probe,&observed) && nano_sdm_reader_observed(&observed)==1 &&
   retained_reader_scope(&sdm_arm_probe.input)==1;
 }
 NOTICE("FES_NANO_READER_ROUTE status=%d owner=%llx stream=1 secure=1 aruser=3e0 arprot=3 bank=secure ns_abort=1 source_retained=1 source_generation=%llu generation=%llu binding=%s\n",
  ready?0:-1,(unsigned long long)b->owner,(unsigned long long)b->generation,
  (unsigned long long)fes_nano_owned_bridges.state.generation,FES_NANO_READER_ROUTE_SHA);
 return ready;
}
