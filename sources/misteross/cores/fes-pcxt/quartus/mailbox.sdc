# SPDX-License-Identifier: GPL-2.0-or-later
# Optional diagnostic contract. The ordinary clock constraints stay intact.
# The RTL's ownership mailbox is proved under arbitrary clock interleavings:
# published_bank is held through acknowledgement and is stable for two pixel
# samples before adoption. A one-pixel-period max delay retains another full
# pixel cycle of protocol headroom. This does not establish analogue MTBF.
# The proof assumes coordinated reset, as the video RTL requires. The builder
# verifies both two-stage release chains share the same PLL-lock assertion;
# reset pulse width, recovery/removal sources and MTBF still need qualification.
proc mailbox_regs {pattern expected} {
 set originals [get_registers -no_duplicates $pattern]
 set names {}
 foreach_in_collection r $originals {
  set n [get_node_info -name $r]
  if {![regexp {~DUPLICATE[0-9]*$} $n]} {lappend names $n}
 }
 if {[lsort $names] ne [lsort $expected]} {error "Mailbox endpoint binding failed: $pattern: $names"}
 set all [get_registers $pattern]
 foreach_in_collection r $all {
  set n [get_node_info -name $r]
  regsub {~DUPLICATE[0-9]*$} $n {} base
  if {[lsearch -exact $expected $base] < 0} {error "Unexpected mailbox duplicate $n"}
 }
 return $all
}
set received [mailbox_regs {system_video_adapter_received} {system_video_adapter_received}]
set sent [mailbox_regs {system_video_adapter_sent} {system_video_adapter_sent}]
set hold_source [mailbox_regs {system_machine_reset} {system_machine_reset}]
foreach role {ack sent hold} {
 set head($role) [mailbox_regs system_video_adapter_${role}_meta system_video_adapter_${role}_meta]
 set tail($role) [mailbox_regs system_video_adapter_${role}_sync system_video_adapter_${role}_sync]
 if {[get_collection_size $head($role)] != 1 || [get_collection_size $tail($role)] != 1} {
  error "Duplicated mailbox synchronizer: $role"
 }
}
# Only the foreign-domain launch to each first data stage is asynchronous.
# The stage-to-stage paths and every synchronous reset path remain timed.
set_false_path -from $received -to $head(ack)
set_false_path -from $sent -to $head(sent)
set_false_path -from $hold_source -to $head(hold)
# The payload is held, not synchronized. Keep it timed to the pixel capture
# registers and bound its physical delay to one pixel period.
set published [mailbox_regs {system_video_adapter_published_bank[*]} {system_video_adapter_published_bank[0] system_video_adapter_published_bank[1]}]
set front [mailbox_regs {system_video_adapter_front[*]} {system_video_adapter_front[0] system_video_adapter_front[1]}]
set pixel [get_clocks {video_clock|general[0].gpll~PLL_OUTPUT_COUNTER|divclk}]
if {[get_collection_size $pixel] != 1} {error "Missing pixel capture clock"}
set period [get_clock_info -period $pixel]
set_max_delay $period -from $published -to $front
puts "MAILBOX_CANDIDATE: three first-stage source pairs; two held payload bits; max delay $period ns"
