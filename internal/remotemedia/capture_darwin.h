#ifndef FOGCAST_REMOTE_MEDIA_CAPTURE_DARWIN_H
#define FOGCAST_REMOTE_MEDIA_CAPTURE_DARWIN_H

#include <stddef.h>
#include <stdint.h>

typedef struct {
    uint8_t *avcc;
    size_t avcc_len;
    uint8_t *sps;
    size_t sps_len;
    uint8_t *pps;
    size_t pps_len;
    int nal_length_size;
    int keyframe;
    int width;
    int height;
    int64_t capture_mono_ns;
    int64_t encode_duration_ns;
} MRNativeSample;

typedef struct {
    int width;
    int height;
    int fps_numerator;
    int fps_denominator;
    uint64_t captured_frames;
    uint64_t dropped_frames;
    uint64_t encoded_frames;
    uint64_t encode_errors;
    int queue_depth;
    int queue_high_water;
    uint64_t packet_queue_drops;
    char *runtime_error;
} MRNativeStats;

void *mr_capture_open(const char *device_identifier, int width, int height,
                      int fps_numerator, int fps_denominator, int bitrate,
                      int gop, char **error_out);
int mr_capture_start(void *handle, char **error_out);
int mr_capture_next(void *handle, MRNativeSample *sample, int timeout_ms,
                    char **error_out);
void mr_capture_sample_free(MRNativeSample *sample);
void mr_capture_stats(void *handle, MRNativeStats *stats);
void mr_capture_close(void *handle);
char *mr_capture_list_devices(void);
void mr_capture_free_string(char *value);

#endif
