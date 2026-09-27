// SPDX-License-Identifier: GPL-3.0-or-later
#include "daemon/menu_frame_transport.hpp"
#include <cassert>
#include <chrono>
#include <dirent.h>
#include <fcntl.h>
#include <sys/socket.h>
#include <unistd.h>
#include <thread>
#include <cstdio>
using namespace mister::daemon;
auto Deadline(){return std::chrono::steady_clock::now()+std::chrono::seconds(2);}
unsigned Fds(){auto directory=opendir("/proc/self/fd");assert(directory);unsigned count=0;while(readdir(directory))++count;closedir(directory);return count;}
struct Pair {int fd[2];Pair(){assert(socketpair(AF_UNIX,SOCK_STREAM|SOCK_CLOEXEC,0,fd)==0);}~Pair(){close(fd[0]);close(fd[1]);}};
int main(){
 const int original=open("/dev/null",O_RDONLY|O_CLOEXEC);assert(original>=0);
 {Pair pair;
  assert(SendFrame(pair.fd[0],"begin",-1,Deadline()).ok());
  assert(SendFrame(pair.fd[0],"commit",original,Deadline()).ok());
  ReceivedFrame first,second;assert(ReceiveFrame(pair.fd[1],Deadline(),&first).ok());
  assert(first.line=="begin"&&first.fds.empty());
  assert(ReceiveFrame(pair.fd[1],Deadline(),&second).ok());assert(second.line=="commit"&&second.fds.size()==1);
  const int received=second.fds[0];assert(fcntl(received,F_GETFD)&FD_CLOEXEC);
  second=ReceivedFrame{};assert(fcntl(received,F_GETFD)<0);
 }
 {Pair pair;assert(send(pair.fd[0],"split",5,0)==5);assert(SendFrame(pair.fd[0]," frame",original,Deadline()).ok());
  ReceivedFrame frame;assert(ReceiveFrame(pair.fd[1],Deadline(),&frame).ok());assert(frame.line=="split frame"&&frame.fds.size()==1);
 }
 {Pair pair;const unsigned before=Fds();
  int descriptors[32];for(auto& descriptor:descriptors)descriptor=original;
  char byte='\n';iovec data{&byte,1};char control[CMSG_SPACE(sizeof(descriptors))]{};
  msghdr message{};message.msg_iov=&data;message.msg_iovlen=1;message.msg_control=control;message.msg_controllen=sizeof(control);
  auto header=CMSG_FIRSTHDR(&message);header->cmsg_level=SOL_SOCKET;header->cmsg_type=SCM_RIGHTS;header->cmsg_len=CMSG_LEN(sizeof(descriptors));
  __builtin_memcpy(CMSG_DATA(header),descriptors,sizeof(descriptors));assert(sendmsg(pair.fd[0],&message,0)==1);
  ReceivedFrame frame;assert(!ReceiveFrame(pair.fd[1],Deadline(),&frame).ok());assert(frame.fds.empty());assert(Fds()==before);
 }
 {Pair pair;ReceivedFrame frame;assert(!ReceiveFrame(pair.fd[1],std::chrono::steady_clock::now()+std::chrono::milliseconds(20),&frame).ok());}
 {Pair pair;assert(send(pair.fd[0],"partial",7,0)==7);shutdown(pair.fd[0],SHUT_WR);ReceivedFrame frame;assert(!ReceiveFrame(pair.fd[1],Deadline(),&frame).ok());}
 for(unsigned size:{65535u,65536u}) {
  Pair pair;ReceivedFrame frame;const std::string line(size,'x');
  std::thread sender([&]{assert(SendFrame(pair.fd[0],line,-1,Deadline()).ok());});
  const auto error=ReceiveFrame(pair.fd[1],Deadline(),&frame);sender.join();assert(error.ok()==(size==65535));
 }
 close(original);puts("menu transport phase/FD ownership/truncation/bounds/deadline tests passed");
}
