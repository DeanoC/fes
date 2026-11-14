"""Exact first DATA edge, after existing verification and before mailbox post."""
import hashlib,json,difflib
from pathlib import Path
INPUT_SHA256='2ecf7481fe799c84731e2a2837226bc855f807223cd497040e5aeb651de51bb9'
SCOPE='nano-first-SDM-DATA-64-and-bounded-events-v2'
def _base_changed(data):
 if hashlib.sha256(data).hexdigest()!=INPUT_SHA256:raise ValueError('exact reviewed remap-observer service required')
 text=data.decode();edits=[]
 pairs=[('#include <assert.h>','#include <assert.h>\n#include "fes_nano_cold_profile.h"\n#include "fault64_service.h"'),
 ('\t\tunsigned observed = fes_nano_sdm_note(args[1], args[2]);', '''        struct nano_fault64_vendor edge={
            .address=args[1],.offered=args[2],.buffer_address=buffer->addr,
            .buffer_size=buffer->size,.written=buffer->size_written,
            .requested=buffer->write_requested,.subblocks=buffer->subblocks_sent,
            .block_number=buffer->block_number,.current_buffer=current_buffer,
            .current_block=current_block,.read_block=read_block,
            .blocks_submitted=blocks_submitted,.send_id=send_id,
            .block_bytes=bytes_per_block,.credits=max_blocks,
            .reconfiguration=request_type==RECONFIGURATION,
            .full_configuration=bridge_disable==true,
            .first_buffer=buffer==&fpga_config_buffers[0]};
        for(unsigned slot=0;slot<FPGA_CONFIG_BUFFER_SIZE;slot++)
            edge.active_buffers+=fpga_config_buffers[slot].write_requested!=0;
        args[2]=fes_nano_sdm_fault64_before(&edge);
\t\tunsigned observed = fes_nano_sdm_note(args[1], args[2]);'''),
 ('\t\tif (observed) NOTICE("FES_NANO_SDM_RESULT status=%d\\n", status);',
  '\t\tif (observed) NOTICE("FES_NANO_SDM_RESULT status=%d\\n", status);\n        fes_nano_sdm_fault64_after(status);')]
 pairs.append(('        status = intel_fpga_config_start(0);', '        fes_nano_sdm_fault64_arm(g_remapper_bypass);\n        status = intel_fpga_config_start(0);\n        fes_nano_sdm_fault64_start_result(status);'))
 for before,after in pairs:
  if text.count(before)!=1:raise ValueError('first DATA source context changed')
  text=text.replace(before,after);edits.append((before,after))
 restored=text
 for before,after in reversed(edits):restored=restored.replace(after,before)
 if restored.encode()!=data:raise ValueError('nonreversible diagnostic transformation')
 return text.encode()

# The reviewed first-DATA transformation remains exact and reversible. This
# second phase moves only the clock hook and the blocking result output.
def changed(data):
 text=_base_changed(data).decode();original=text;edits=[]
 pairs=[('\t\tunsigned observed = fes_nano_sdm_note(args[1], args[2]);',
         '\t\tunsigned observed = fes_nano_sdm_note(args[1], args[2]);\n        fes_nano_sdm_fault64_submit(observed);'),
        ('\t\tif (observed) NOTICE("FES_NANO_SDM_RESULT status=%d\\n", status);','')]
 for before,after in pairs:
  if text.count(before)!=1:raise ValueError('exact submit/result clock edge changed')
  text=text.replace(before,after);edits.append((before,after))
 restored=text
 for before,after in reversed(edits):
  if after:restored=restored.replace(after,before)
  else:
   anchor='\n        fes_nano_sdm_fault64_after(status);'
   if restored.count(anchor)!=1:raise ValueError('terminal result context changed')
   restored=restored.replace(anchor,before+anchor)
 if restored!=original:raise ValueError('clock-edge transformation not reversible')
 return text.encode()
