#include <unistd.h>
#include <cstdio>
int main(int argc,char** argv) { if(argc!=2)return 64; alarm(30); execl(argv[1],argv[1],static_cast<char*>(nullptr)); perror("exec"); return 127; }
