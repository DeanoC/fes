/* Standalone V4L2 acquisition experiment. Does not send input to a kit.
 * Each little-endian record contains magic, kernel sequence, kernel timestamp
 * in ns, dequeue monotonic ns, kernel flags, payload length, then raw YUYV.
 * A kernel timestamp does not establish the age of frames upstream of V4L2.
 */
#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <linux/videodev2.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>

#define WIDTH 1920u
#define HEIGHT 1080u
#define FRAME_SIZE (WIDTH * HEIGHT * 2u)
struct mapped { void *ptr; size_t len; };

static int xioctl(int fd, unsigned long op, void *arg) {
    int result;
    do { result = ioctl(fd, op, arg); } while (result < 0 && errno == EINTR);
    return result;
}
static int write_all(int fd, const void *data, size_t n) {
    const unsigned char *p = data;
    while (n) {
        ssize_t wrote = write(fd, p, n);
        if (wrote < 0 && errno == EINTR) continue;
        if (wrote <= 0) return -1;
        p += wrote; n -= (size_t)wrote;
    }
    return 0;
}
static void le32(unsigned char *out, uint32_t n) {
    for (int i = 0; i < 4; ++i) out[i] = (unsigned char)(n >> (8*i));
}
static void le64(unsigned char *out, uint64_t n) {
    for (int i = 0; i < 8; ++i) out[i] = (unsigned char)(n >> (8*i));
}
int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr, "usage: pcxt_v4l2_capture DEVICE\n"); return 2; }
    int fd = open(argv[1], O_RDWR | O_NONBLOCK | O_CLOEXEC);
    if (fd < 0) { perror("open"); return 1; }
    struct v4l2_capability cap = {0};
    if (xioctl(fd, VIDIOC_QUERYCAP, &cap) < 0) { perror("query capabilities"); return 1; }
    uint32_t capabilities = (cap.capabilities & V4L2_CAP_DEVICE_CAPS) ? cap.device_caps : cap.capabilities;
    if (!(capabilities & V4L2_CAP_VIDEO_CAPTURE) || !(capabilities & V4L2_CAP_STREAMING)) {
        fprintf(stderr, "device does not support single-plane streaming capture\n"); return 1;
    }
    struct v4l2_format fmt = {0}; fmt.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    fmt.fmt.pix.width = WIDTH; fmt.fmt.pix.height = HEIGHT;
    fmt.fmt.pix.pixelformat = V4L2_PIX_FMT_YUYV; fmt.fmt.pix.field = V4L2_FIELD_NONE;
    if (xioctl(fd, VIDIOC_S_FMT, &fmt) < 0) { perror("set format"); return 1; }
    if (fmt.fmt.pix.width != WIDTH || fmt.fmt.pix.height != HEIGHT ||
        fmt.fmt.pix.pixelformat != V4L2_PIX_FMT_YUYV || fmt.fmt.pix.field != V4L2_FIELD_NONE ||
        fmt.fmt.pix.bytesperline != WIDTH*2 ||
        fmt.fmt.pix.sizeimage < FRAME_SIZE) {
        fprintf(stderr, "device returned an incompatible format or row stride\n"); return 1;
    }
    struct v4l2_requestbuffers req = {0}; req.count = 8; req.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    req.memory = V4L2_MEMORY_MMAP;
    if (xioctl(fd, VIDIOC_REQBUFS, &req) < 0 || req.count < 2 || req.count > 64) {
        perror("request buffers"); return 1;
    }
    struct mapped *maps = calloc(req.count, sizeof(*maps));
    if (!maps) { perror("calloc"); return 1; }
    for (uint32_t i = 0; i < req.count; ++i) {
        struct v4l2_buffer b = {0}; b.type = req.type; b.memory = req.memory; b.index = i;
        if (xioctl(fd, VIDIOC_QUERYBUF, &b) < 0) { perror("query buffer"); return 1; }
        maps[i].len = b.length;
        maps[i].ptr = mmap(NULL, b.length, PROT_READ | PROT_WRITE, MAP_SHARED, fd, b.m.offset);
        if (maps[i].ptr == MAP_FAILED) { perror("mmap"); return 1; }
        if (xioctl(fd, VIDIOC_QBUF, &b) < 0) { perror("queue buffer"); return 1; }
    }
    enum v4l2_buf_type type = req.type;
    if (xioctl(fd, VIDIOC_STREAMON, &type) < 0) { perror("stream on"); return 1; }
    for (;;) {
        struct pollfd pfd = {.fd = fd, .events = POLLIN};
        int result;
        do { result = poll(&pfd, 1, 2000); } while (result < 0 && errno == EINTR);
        if (result <= 0 || !(pfd.revents & POLLIN) || (pfd.revents & (POLLERR | POLLHUP | POLLNVAL))) {
            fprintf(stderr, "capture timeout or device failure\n"); return 1;
        }
        struct v4l2_buffer b = {0}; b.type = req.type; b.memory = req.memory;
        if (xioctl(fd, VIDIOC_DQBUF, &b) < 0) { if (errno == EAGAIN) continue; perror("dequeue"); return 1; }
        struct timespec now;
        if (clock_gettime(CLOCK_MONOTONIC, &now) || b.index >= req.count || b.bytesused != FRAME_SIZE ||
            maps[b.index].len < FRAME_SIZE || (b.flags & V4L2_BUF_FLAG_ERROR) ||
            b.timestamp.tv_sec < 0 || b.timestamp.tv_usec < 0 || b.timestamp.tv_usec >= 1000000) {
            fprintf(stderr, "invalid captured buffer\n"); return 1;
        }
        unsigned char header[32] = {'P','X','T','1'};
        le32(header+4, b.sequence);
        le64(header+8, (uint64_t)b.timestamp.tv_sec*1000000000ull + (uint64_t)b.timestamp.tv_usec*1000ull);
        le64(header+16, (uint64_t)now.tv_sec*1000000000ull + (uint64_t)now.tv_nsec);
        le32(header+24, b.flags); le32(header+28, b.bytesused);
        if (write_all(STDOUT_FILENO, header, sizeof(header)) || write_all(STDOUT_FILENO, maps[b.index].ptr, FRAME_SIZE)) {
            perror("write capture stream"); return 1;
        }
        if (xioctl(fd, VIDIOC_QBUF, &b) < 0) { perror("requeue buffer"); return 1; }
    }
}
