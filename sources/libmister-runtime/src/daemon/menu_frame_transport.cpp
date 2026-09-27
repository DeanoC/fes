// SPDX-License-Identifier: GPL-3.0-or-later
#include "daemon/menu_frame_transport.hpp"
#include <algorithm>
#include <cerrno>
#include <climits>
#include <cstring>
#include <poll.h>
#include <sys/socket.h>
#include <unistd.h>
namespace mister { namespace daemon {
namespace {
Error Invalid(const char* text){return {ErrorCode::invalid_request,text,"request"};}
Error Io(const char* text){return {ErrorCode::io_failed,std::string(text)+": "+std::strerror(errno),"request"};}
Error Ready(int socket,short events,FrameDeadline deadline){
 for(;;){
  const auto remaining=std::chrono::duration_cast<std::chrono::milliseconds>(deadline-std::chrono::steady_clock::now()).count();
  if(remaining<=0)return Invalid("request deadline exceeded");
  pollfd descriptor{socket,events,0};
  const auto result=poll(&descriptor,1,static_cast<int>(std::min<long long>(remaining,INT_MAX)));
  if(result<0&&errno==EINTR)continue;
  if(result<0)return Io("poll request");
  if(result==0)continue;
  if(descriptor.revents&(events|POLLHUP))return {};
  return Invalid("request socket failed");
 }
}
void Close(std::vector<int>& fds){for(int fd:fds)(void)close(fd);fds.clear();}
}
ReceivedFrame::~ReceivedFrame(){Close(fds);}
ReceivedFrame::ReceivedFrame(ReceivedFrame&& other) noexcept {line.swap(other.line);fds.swap(other.fds);}
ReceivedFrame& ReceivedFrame::operator=(ReceivedFrame&& other) noexcept {
 if(this!=&other){Close(fds);line=std::move(other.line);fds.swap(other.fds);}return *this;
}
Error ReceiveFrame(int socket,FrameDeadline deadline,ReceivedFrame* output){
 if(!output)return Invalid("missing received frame output");
 *output=ReceivedFrame{};ReceivedFrame frame;
 for(;;){
  Error error=Ready(socket,POLLIN,deadline);if(!error.ok())return error;
  char bytes[4096];
  const auto peeked=recv(socket,bytes,sizeof(bytes),MSG_PEEK|MSG_DONTWAIT);
  if(peeked<0&&(errno==EINTR||errno==EAGAIN||errno==EWOULDBLOCK))continue;
  if(peeked<0)return Io("peek request");
  if(peeked==0)return Invalid("request ended before newline");
  const auto newline=static_cast<const char*>(memchr(bytes,'\n',static_cast<std::size_t>(peeked)));
  const auto wanted=newline?static_cast<std::size_t>(newline-bytes+1):static_cast<std::size_t>(peeked);
  iovec data{bytes,wanted};alignas(cmsghdr) char control[CMSG_SPACE(8*sizeof(int))]{};
  msghdr message{};message.msg_iov=&data;message.msg_iovlen=1;
  message.msg_control=control;message.msg_controllen=sizeof(control);
  const auto count=recvmsg(socket,&message,MSG_CMSG_CLOEXEC|MSG_DONTWAIT);
  if(count<0&&(errno==EINTR||errno==EAGAIN||errno==EWOULDBLOCK))continue;
  if(count<0)return Io("read request");
  if(count==0)return Invalid("request ended before newline");
  bool ancillary_invalid=false;
  for(auto header=CMSG_FIRSTHDR(&message);header;header=CMSG_NXTHDR(&message,header)){
   if(header->cmsg_level!=SOL_SOCKET||header->cmsg_type!=SCM_RIGHTS||header->cmsg_len<CMSG_LEN(0)) {ancillary_invalid=true;continue;}
   const auto size=header->cmsg_len-CMSG_LEN(0);
   if(size%sizeof(int))ancillary_invalid=true;
   for(std::size_t offset=0;offset+sizeof(int)<=size;offset+=sizeof(int)){
    int fd=-1;memcpy(&fd,CMSG_DATA(header)+offset,sizeof(fd));frame.fds.push_back(fd);
   }
  }
  if((message.msg_flags&(MSG_CTRUNC|MSG_TRUNC))||ancillary_invalid||frame.fds.size()>8)
   return Invalid("invalid or truncated request descriptors");
  for(ssize_t i=0;i<count;++i){
   if(bytes[i]=='\n'){*output=std::move(frame);return {};}
   if(frame.line.size()==65535)return Invalid("frame_too_large");
   frame.line.push_back(bytes[i]);
  }
 }
}
Error SendFrame(int socket,const std::string& line,int fd,FrameDeadline deadline){
 if(line.find('\n')!=std::string::npos)return Invalid("frame contains a newline");
 const auto bytes=line+"\n";std::size_t offset=0;bool descriptor_sent=false;
 while(offset<bytes.size()){
  Error error=Ready(socket,POLLOUT,deadline);if(!error.ok())return error;
  iovec data{const_cast<char*>(bytes.data()+offset),bytes.size()-offset};
  alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))]{};msghdr message{};
  message.msg_iov=&data;message.msg_iovlen=1;
  if(fd>=0&&!descriptor_sent){
   message.msg_control=control;message.msg_controllen=sizeof(control);auto header=CMSG_FIRSTHDR(&message);
   header->cmsg_level=SOL_SOCKET;header->cmsg_type=SCM_RIGHTS;header->cmsg_len=CMSG_LEN(sizeof(fd));memcpy(CMSG_DATA(header),&fd,sizeof(fd));
  }
  const auto count=sendmsg(socket,&message,MSG_NOSIGNAL|MSG_DONTWAIT);
  if(count<0&&(errno==EINTR||errno==EAGAIN||errno==EWOULDBLOCK))continue;
  if(count<=0)return Io("send response");
  descriptor_sent=true;offset+=static_cast<std::size_t>(count);
 }
 return {};
}
} }
