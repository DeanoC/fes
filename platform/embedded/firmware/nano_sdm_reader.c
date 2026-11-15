/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_sdm_reader.h"
int nano_sdm_reader_observed(const struct fes_nano_smmu_observation *o)
{
 if(!o || o->identified!=1 || o->controls_settled!=1 || o->secure_implemented!=1 || o->bypass_controls_clear)return 0;
 const struct fes_nano_smmu_snapshot *s=&o->registers;
 return s->idr0==0x080f7e3f && s->idr1==0x0e739d18 && s->aidr==1 && s->s_idr1==0x80000018 &&
  !s->cr0 && !s->cr0ack && s->gbpa==FES_NANO_SMMU_ABORT && !s->agbpa &&
  !s->s_cr0 && !s->s_cr0ack && !s->s_init && !s->s_gbpa && !s->s_agbpa;
}
