#ifndef MR_AUDIO_DARWIN_H
#define MR_AUDIO_DARWIN_H

#include <stddef.h>
#include <stdint.h>

enum {
    MR_AUDIO_KIND_SHADOWCAST_UAC = 1,
    MR_AUDIO_KIND_HOST_OUTPUT = 2,
};

typedef struct {
    int kind;
    int sample_rate;
    int channels;
    int frame_samples;
    const char *endpoint_uid;
    const char *endpoint_name;
    const char *display_hardware_uuid;
    const char *display_vendor;
    const char *display_model;
    const char *display_serial;
    const char *display_digest;
} MRNativeAudioConfig;

typedef struct {
    uint8_t *pcm16;
    size_t pcm16_len;
    int frames;
    int channels;
    int64_t capture_mono_ns;
} MRNativeAudioSample;

typedef struct {
    uint64_t callbacks;
    uint64_t non_zero_samples;
    uint64_t dropped_frames;
    int queue_depth;
    int queue_high_water;
    char *runtime_error;
} MRNativeAudioStats;

void *mr_audio_open(const MRNativeAudioConfig *config, char **error_out);
int mr_audio_start(void *handle, char **error_out);
int mr_audio_next(void *handle, MRNativeAudioSample *sample, int timeout_ms,
                  char **error_out);
void mr_audio_sample_free(MRNativeAudioSample *sample);
void mr_audio_stats(void *handle, MRNativeAudioStats *stats);
int mr_audio_close(void *handle, char **error_out);

int mr_audio_microphone_authorization_status(void);
int mr_audio_request_microphone_authorization(char **error_out);
int mr_audio_screen_authorization_status(void);
int mr_audio_request_screen_authorization(char **error_out);
int mr_audio_host_output_available(void);

// These helpers retain the explicit identity checks used by tests and by the
// native open path. They never select a default device or display.
int mr_audio_validate_uac(const char *uid, const char *name, char **error_out);
int mr_audio_validate_display(const char *hardware_uuid, const char *vendor,
                              const char *model, const char *serial,
                              char **error_out);

#endif
