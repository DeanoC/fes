#ifndef FOGCAST_ATTRACT_VIDEO_DARWIN_H
#define FOGCAST_ATTRACT_VIDEO_DARWIN_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void *fogcast_attract_video_open(const char *path, char **err);
int fogcast_attract_video_dimensions(void *video, int *w, int *h);
/* Returns 1 if buf was filled with a new RGBA frame, 0 if the previous frame
   should be kept, -1 on natural end, -2 on error. */
int fogcast_attract_video_copy_rgba(void *video, uint8_t *buf, int stride, int height, int *ended, char **err);
void fogcast_attract_video_close(void *video);

#ifdef __cplusplus
}
#endif

#endif
