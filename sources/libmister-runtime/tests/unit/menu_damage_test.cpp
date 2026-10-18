// SPDX-License-Identifier: GPL-3.0-or-later
// Damaged menu presents: only changed rects cross the staging file, yet each
// DDR slot ends up identical to the full frame it shows.
#include "native/linux/menu_memory.hpp"
#include "native/generated/fes_application.hpp"
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <vector>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
namespace {
constexpr unsigned W=FesApplicationMenuWidth,H=FesApplicationMenuHeight;
class Ddr : public MenuMemoryOperations {
public:
 std::vector<std::uint32_t> data=std::vector<std::uint32_t>(2*FesApplicationMenuSlotBytes/4,0xdeadbeef);
 std::size_t stored=0;bool cancel=false;
 Error VerifyReservation(std::uint64_t,std::uint64_t) override {return {};}
 Error Open(int* fd) override {*fd=7;return {};}
 Error Map(int,std::uint64_t,std::size_t,void** out) override {*out=data.data();return {};}
 void Unmap(void*,std::size_t) override {}
 void Close(int) override {}
 void VisibilityBarrier() override {}
 void StoreAlignedWords(void* destination,const std::uint32_t* source,std::size_t words) override {
  auto* dest=static_cast<std::uint32_t*>(destination);
  const auto index=static_cast<std::size_t>(dest-data.data());
  const std::size_t slot_base=index>=FesApplicationMenuSlotBytes/4?FesApplicationMenuSlotBytes/4:0;
  const std::size_t in_slot=index-slot_base;
  // 32-byte bursts inside one row of one slot.
  assert(words>0&&words%8==0&&in_slot%8==0);
  assert(in_slot/W==(in_slot+words-1)/W&&in_slot+words<=std::size_t(W)*H);
  stored+=words;
  MenuMemoryOperations::StoreAlignedWords(destination,source,words);
 }
 bool MenuCopyCancelled() const override {return cancel;}
 const std::uint32_t* slot(int s) const {return data.data()+s*FesApplicationMenuSlotBytes/4;}
};
std::uint32_t Xrgb(const std::vector<unsigned char>& rgba,std::size_t pixel)
{return std::uint32_t(rgba[pixel*4+2])|std::uint32_t(rgba[pixel*4+1])<<8|std::uint32_t(rgba[pixel*4])<<16;}
// A sealed staging file. With rects, only those pixels are copied in; the
// rest of the file is junk the runtime must not use.
std::unique_ptr<MenuFrame> Stage(const std::vector<unsigned char>& rgba,const std::vector<MenuRect>* rects)
{
 std::unique_ptr<MenuFrame> frame;assert(MenuFrame::Create(&frame).ok());
 assert(ftruncate(frame->fd(),FesApplicationMenuFrameBytes)==0);
 auto* out=static_cast<unsigned char*>(mmap(nullptr,FesApplicationMenuFrameBytes,PROT_READ|PROT_WRITE,MAP_SHARED,frame->fd(),0));
 assert(out!=MAP_FAILED);
 if(!rects)std::memcpy(out,rgba.data(),rgba.size());
 else {
  std::memset(out,0xa5,FesApplicationMenuFrameBytes);
  for(const auto& r:*rects)for(unsigned y=r.y;y<unsigned(r.y)+r.h;++y)
   std::memcpy(out+(std::size_t(y)*W+r.x)*4,rgba.data()+(std::size_t(y)*W+r.x)*4,std::size_t(r.w)*4);
 }
 assert(munmap(out,FesApplicationMenuFrameBytes)==0);
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 return frame;
}
void Paint(std::vector<unsigned char>* rgba,MenuRect r,unsigned char seed)
{
 for(unsigned y=r.y;y<unsigned(r.y)+r.h;++y)for(unsigned x=r.x;x<unsigned(r.x)+r.w;++x){
  auto* p=rgba->data()+(std::size_t(y)*W+x)*4;p[0]=seed;p[1]=static_cast<unsigned char>(x);p[2]=static_cast<unsigned char>(y);p[3]=255;
 }
}
void AssertSlotIs(const Ddr& ddr,int slot,const std::vector<unsigned char>& rgba)
{
 const std::uint32_t* s=ddr.slot(slot);
 for(std::size_t i=0;i<std::size_t(W)*H;++i)if(s[i]!=Xrgb(rgba,i)){std::fprintf(stderr,"slot %d differs at %zu\n",slot,i);assert(false);}
}
}
int main()
{
 std::vector<unsigned char> a(FesApplicationMenuFrameBytes);
 for(std::size_t i=0;i<a.size();i+=4){a[i]=static_cast<unsigned char>(i);a[i+1]=static_cast<unsigned char>(i>>8);a[i+2]=static_cast<unsigned char>(i>>16);a[i+3]=255;}
 Ddr ddr;MenuMemory memory(ddr);
 assert(!memory.HasShadow(1));
 auto full=Stage(a,nullptr);
 assert(memory.CopyRgba(1,*full,1).ok());AssertSlotIs(ddr,1,a);
 assert(memory.HasShadow(1)&&!memory.HasShadow(2)&&memory.last_stored_pixels()==std::size_t(W)*H);

 // Frame 2 changes one odd-aligned rect; it goes to slot 0, which still
 // holds unrelated pixels, so the whole slot is brought up to date.
 std::vector<unsigned char> b=a;const MenuRect r1{13,40,101,30};Paint(&b,r1,0x31);
 std::vector<MenuRect> d1{r1};auto damaged=Stage(b,&d1);
 MenuDamage damage{1,d1};damaged->SetDamage(damage);
 ddr.stored=0;assert(memory.CopyRgba(0,*damaged,2).ok());AssertSlotIs(ddr,0,b);
 assert(memory.HasShadow(2)&&!memory.HasShadow(1));

 // Frame 3 goes back to slot 1: it needs its own change plus frame 2's.
 std::vector<unsigned char> c=b;const MenuRect r2{600,500,8,8};Paint(&c,r2,0x77);
 std::vector<MenuRect> d2{r2};auto second=Stage(c,&d2);second->SetDamage({2,d2});
 ddr.stored=0;assert(memory.CopyRgba(1,*second,3).ok());AssertSlotIs(ddr,1,c);
 const std::size_t expected=8*8+std::size_t(112)*30;  // r2, plus r1 widened to 8-pixel groups [8,120)
 assert(ddr.stored==expected&&memory.last_stored_pixels()==expected);

 // A damage base that is not the shadowed frame is refused and spends nothing.
 auto stale=Stage(c,&d2);stale->SetDamage({2,d2});
 ddr.stored=0;assert(!memory.CopyRgba(0,*stale,4).ok());assert(ddr.stored==0);
 assert(memory.HasShadow(3));

 // A cancelled copy leaves the slot unknown, so the shadow is discarded.
 ddr.cancel=true;auto after=Stage(c,&d2);after->SetDamage({3,d2});
 assert(!memory.CopyRgba(0,*after,4).ok());assert(!memory.HasShadow(3));
 ddr.cancel=false;memory.AllowCopies();assert(!memory.HasShadow(3));
 assert(memory.CopyRgba(0,*full,5).ok()&&memory.HasShadow(5));
 memory.AllowCopies();assert(!memory.HasShadow(5));
 std::printf("menu damage: stored %zu pixels for an 8x8 change, slots match full frames\n",expected);
}
