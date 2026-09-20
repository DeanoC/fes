#import <AVFoundation/AVFoundation.h>
#import <AudioToolbox/AudioToolbox.h>
#import <CommonCrypto/CommonDigest.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>

#include <dispatch/dispatch.h>
#include <errno.h>
#include <mach/mach_time.h>
#include <math.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#import "audio_darwin.h"

typedef struct MRNativeAudio MRNativeAudio;

int mr_audio_close(void *handle, char **error_out);

@interface MRAudioCaptureDelegate : NSObject <AVCaptureAudioDataOutputSampleBufferDelegate>
- (instancetype)initWithAudio:(MRNativeAudio *)audio;
@property(nonatomic, assign) MRNativeAudio *audio;
@end

@interface MRAudioStreamDelegate : NSObject <SCStreamOutput>
- (instancetype)initWithAudio:(MRNativeAudio *)audio;
@property(nonatomic, assign) MRNativeAudio *audio;
@end

struct MRNativeAudio {
    pthread_mutex_t mutex;
    pthread_cond_t condition;
    int kind;
    int sample_rate;
    int channels;
    int frame_samples;
    int started;
    int closing;
    int closed;
    uint8_t *accum;
    size_t accum_len;
    int accum_frames;
    int64_t accum_capture_mono_ns;
    uint8_t *ready;
    size_t ready_len;
    int ready_frames;
    int64_t ready_capture_mono_ns;
    uint64_t callbacks;
    uint64_t callbacks_inflight;
    uint64_t non_zero_samples;
    uint64_t dropped_frames;
    int queue_depth;
    int queue_high_water;
    char *runtime_error;
    mach_timebase_info_data_t timebase;
    CFTypeRef capture_session;
    CFTypeRef capture_input;
    CFTypeRef capture_output;
    CFTypeRef capture_delegate;
    CFTypeRef stream;
    CFTypeRef stream_delegate;
    void *callback_queue;
    void *control_queue;
};

static const void *mr_audio_queue_key = &mr_audio_queue_key;

static void mr_audio_error(char **out, NSString *message) {
    if (out != NULL) *out = strdup(message.UTF8String ?: "native audio operation failed");
}

static NSString *mr_hex(uint32_t value) {
    return [NSString stringWithFormat:@"%08x", value];
}

static void mr_append_u32(NSMutableData *data, uint32_t value) {
    uint8_t bytes[4] = {
        (uint8_t)((value >> 24) & 0xff), (uint8_t)((value >> 16) & 0xff),
        (uint8_t)((value >> 8) & 0xff), (uint8_t)(value & 0xff),
    };
    [data appendBytes:bytes length:sizeof(bytes)];
}

static void mr_append_string(NSMutableData *data, NSString *value) {
    NSData *encoded = [value dataUsingEncoding:NSUTF8StringEncoding] ?: [NSData data];
    mr_append_u32(data, (uint32_t)encoded.length);
    [data appendData:encoded];
}

static NSString *mr_display_digest(NSString *uuid, NSString *vendor, NSString *model, NSString *serial) {
    NSMutableData *data = [NSMutableData data];
    const char domain[] = "fogcast-display-v1\0";
    [data appendBytes:domain length:sizeof(domain) - 1];
    mr_append_string(data, uuid);
    mr_append_string(data, vendor);
    mr_append_string(data, model);
    mr_append_string(data, serial);
    unsigned char digest[CC_SHA256_DIGEST_LENGTH];
    CC_SHA256(data.bytes, (CC_LONG)data.length, digest);
    NSMutableString *hex = [NSMutableString stringWithCapacity:CC_SHA256_DIGEST_LENGTH * 2];
    for (NSUInteger index = 0; index < CC_SHA256_DIGEST_LENGTH; index++) {
        [hex appendFormat:@"%02x", digest[index]];
    }
    return [NSString stringWithFormat:@"sha256:%@", hex];
}

static int64_t mr_monotonic_ns(MRNativeAudio *audio) {
    uint64_t ticks = mach_absolute_time();
    return (int64_t)((ticks * audio->timebase.numer) / audio->timebase.denom);
}

static void mr_set_error_locked(MRNativeAudio *audio, NSString *message) {
    free(audio->runtime_error);
    audio->runtime_error = strdup(message.UTF8String ?: "native audio callback failed");
}

static int16_t mr_float_to_pcm16(float value) {
    if (isnan(value)) return 0;
    if (value >= 1.0f) return INT16_MAX;
    if (value <= -1.0f) return INT16_MIN;
    long rounded = lroundf(value * (float)INT16_MAX);
    if (rounded > INT16_MAX) rounded = INT16_MAX;
    if (rounded < INT16_MIN) rounded = INT16_MIN;
    return (int16_t)rounded;
}

static uint64_t mr_count_nonzero(const uint8_t *bytes, size_t length) {
    uint64_t count = 0;
    for (size_t index = 0; index + 1 < length; index += 2) {
        if (bytes[index] != 0 || bytes[index + 1] != 0) count++;
    }
    return count;
}

static BOOL mr_on_audio_queue(MRNativeAudio *audio) {
    return dispatch_get_specific(mr_audio_queue_key) == audio;
}

static void mr_publish_accumulator_locked(MRNativeAudio *audio) {
    size_t frame_bytes = (size_t)audio->channels * 2;
    size_t capacity = (size_t)audio->frame_samples * frame_bytes;
    if (audio->accum_frames != audio->frame_samples || audio->accum_len != capacity) return;
    if (audio->ready != NULL) {
        // Match the platform-neutral one-slot queue: keep the newest complete
        // frame and count the replaced older frame as a drop.
        audio->dropped_frames += (uint64_t)audio->ready_frames;
        free(audio->ready);
        audio->ready = NULL;
        audio->ready_len = 0;
        audio->ready_frames = 0;
        audio->ready_capture_mono_ns = 0;
    }
    audio->ready = audio->accum;
    audio->ready_len = audio->accum_len;
    audio->ready_frames = audio->accum_frames;
    audio->ready_capture_mono_ns = audio->accum_capture_mono_ns;
    audio->queue_depth = 1;
    if (audio->queue_high_water < 1) audio->queue_high_water = 1;
    audio->non_zero_samples += mr_count_nonzero(audio->ready, audio->ready_len);
    audio->accum = calloc(1, capacity);
    if (audio->accum == NULL) {
        // The published frame remains valid; the next callback will report a
        // bounded allocation failure rather than growing an unbounded queue.
        mr_set_error_locked(audio, @"native audio callback buffer allocation failed");
    }
    audio->accum_len = 0;
    audio->accum_frames = 0;
    audio->accum_capture_mono_ns = 0;
    pthread_cond_signal(&audio->condition);
}

static BOOL mr_append_float_frame_locked(MRNativeAudio *audio, int64_t capture_ns,
                                         float value, NSString **error_message) {
    if (audio->accum == NULL) {
        if (error_message != NULL) *error_message = @"native audio callback buffer is unavailable";
        return NO;
    }
    if (audio->accum_frames == 0) audio->accum_capture_mono_ns = capture_ns;
    size_t offset = audio->accum_len;
    int16_t pcm = mr_float_to_pcm16(value);
    audio->accum[offset] = (uint8_t)(pcm & 0xff);
    audio->accum[offset + 1] = (uint8_t)(((uint16_t)pcm >> 8) & 0xff);
    audio->accum_len += 2;
    if (audio->accum_len % ((size_t)audio->channels * 2) == 0) audio->accum_frames++;
    if (audio->accum_frames == audio->frame_samples) mr_publish_accumulator_locked(audio);
    return YES;
}

static BOOL mr_append_sample_buffer_locked(MRNativeAudio *audio, CMSampleBufferRef sample_buffer,
                                           NSString **error_message) {
    CMFormatDescriptionRef format_description = CMSampleBufferGetFormatDescription(sample_buffer);
    const AudioStreamBasicDescription *description = format_description == NULL ? NULL : CMAudioFormatDescriptionGetStreamBasicDescription(format_description);
    if (description == NULL || description->mFormatID != kAudioFormatLinearPCM ||
        !(description->mFormatFlags & kAudioFormatFlagIsFloat) || description->mBitsPerChannel != 32 ||
        (int)description->mChannelsPerFrame != audio->channels ||
        (int)llround(description->mSampleRate) != audio->sample_rate) {
        if (error_message != NULL) *error_message = @"native audio callback format is not 48 kHz float PCM with the configured channel count";
        return NO;
    }
    CMItemCount frames = CMSampleBufferGetNumSamples(sample_buffer);
    if (frames <= 0) return YES;

    size_t list_size = 0;
    OSStatus status = CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(
        sample_buffer, &list_size, NULL, 0, kCFAllocatorDefault, kCFAllocatorDefault,
        kCMSampleBufferFlag_AudioBufferList_Assure16ByteAlignment, NULL);
    if (status != noErr || list_size < sizeof(AudioBufferList)) {
        if (error_message != NULL) *error_message = @"native audio callback buffer list is unavailable";
        return NO;
    }
    AudioBufferList *list = calloc(1, list_size);
    if (list == NULL) {
        if (error_message != NULL) *error_message = @"native audio callback buffer list allocation failed";
        return NO;
    }
    CMBlockBufferRef retained_block = NULL;
    status = CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(
        sample_buffer, &list_size, list, list_size, kCFAllocatorDefault, kCFAllocatorDefault,
        kCMSampleBufferFlag_AudioBufferList_Assure16ByteAlignment, &retained_block);
    if (status != noErr || list->mNumberBuffers == 0) {
        if (retained_block != NULL) CFRelease(retained_block);
        free(list);
        if (error_message != NULL) *error_message = @"native audio callback buffer list read failed";
        return NO;
    }
    BOOL planar = (description->mFormatFlags & kAudioFormatFlagIsNonInterleaved) != 0;
    size_t required_interleaved = (size_t)frames * (size_t)audio->channels * sizeof(float);
    BOOL valid_layout = planar ? (list->mNumberBuffers == (UInt32)audio->channels) : (list->mNumberBuffers == 1);
    if (valid_layout) {
        if (planar) {
            for (int channel = 0; channel < audio->channels; channel++) {
                if (list->mBuffers[channel].mData == NULL || list->mBuffers[channel].mDataByteSize < (UInt32)((size_t)frames * sizeof(float))) valid_layout = NO;
            }
        } else if (list->mBuffers[0].mData == NULL || list->mBuffers[0].mDataByteSize < required_interleaved) {
            valid_layout = NO;
        }
    }
    if (!valid_layout) {
        if (retained_block != NULL) CFRelease(retained_block);
        free(list);
        if (error_message != NULL) *error_message = @"native audio callback PCM layout is invalid";
        return NO;
    }
    int64_t capture_ns = mr_monotonic_ns(audio);
    for (CMItemCount frame = 0; frame < frames; frame++) {
        for (int channel = 0; channel < audio->channels; channel++) {
            float value;
            if (planar) {
                value = ((const float *)list->mBuffers[channel].mData)[frame];
            } else {
                value = ((const float *)list->mBuffers[0].mData)[frame * audio->channels + channel];
            }
            if (!mr_append_float_frame_locked(audio, capture_ns, value, error_message)) {
                if (retained_block != NULL) CFRelease(retained_block);
                free(list);
                return NO;
            }
        }
    }
    if (retained_block != NULL) CFRelease(retained_block);
    free(list);
    return YES;
}

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
static NSArray<AVCaptureDevice *> *mr_audio_devices(void) {
    return [AVCaptureDevice devicesWithMediaType:AVMediaTypeAudio];
}
#pragma clang diagnostic pop

static void mr_audio_callback_sample(MRNativeAudio *audio, CMSampleBufferRef sample_buffer) {
    pthread_mutex_lock(&audio->mutex);
    audio->callbacks++;
    if (audio->closing || audio->closed) {
        audio->dropped_frames += (uint64_t)CMSampleBufferGetNumSamples(sample_buffer);
        pthread_mutex_unlock(&audio->mutex);
        return;
    }
    audio->callbacks_inflight++;
    NSString *error_message = nil;
    if (!mr_append_sample_buffer_locked(audio, sample_buffer, &error_message)) {
        audio->dropped_frames += (uint64_t)CMSampleBufferGetNumSamples(sample_buffer);
        mr_set_error_locked(audio, error_message ?: @"native audio callback failed");
        pthread_cond_signal(&audio->condition);
    }
    audio->callbacks_inflight--;
    pthread_cond_broadcast(&audio->condition);
    pthread_mutex_unlock(&audio->mutex);
}

@implementation MRAudioCaptureDelegate
- (instancetype)initWithAudio:(MRNativeAudio *)audio {
    self = [super init];
    if (self != nil) self.audio = audio;
    return self;
}
- (void)captureOutput:(AVCaptureOutput *)output didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer fromConnection:(AVCaptureConnection *)connection {
    (void)output; (void)connection;
    mr_audio_callback_sample(self.audio, sampleBuffer);
}
@end

@implementation MRAudioStreamDelegate
- (instancetype)initWithAudio:(MRNativeAudio *)audio {
    self = [super init];
    if (self != nil) self.audio = audio;
    return self;
}
- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer ofType:(SCStreamOutputType)type API_AVAILABLE(macos(13.0)) {
    (void)stream;
    if (type == SCStreamOutputTypeAudio) mr_audio_callback_sample(self.audio, sampleBuffer);
}
@end

static NSString *mr_display_uuid(CGDirectDisplayID display_id) {
    CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(display_id);
    if (uuid == NULL) return @"";
    NSString *value = [[(__bridge_transfer NSUUID *)uuid UUIDString] lowercaseString];
    return value ?: @"";
}

static NSString *mr_display_serial(CGDirectDisplayID display_id) {
    uint32_t value = CGDisplaySerialNumber(display_id);
    if (value == 0 || value == UINT32_MAX) return @"";
    return mr_hex(value);
}

static BOOL mr_display_matches(SCDisplay *display, NSString *wanted_uuid, NSString *wanted_vendor,
                               NSString *wanted_model, NSString *wanted_serial) API_AVAILABLE(macos(12.3));
static BOOL mr_display_matches(SCDisplay *display, NSString *wanted_uuid, NSString *wanted_vendor,
                               NSString *wanted_model, NSString *wanted_serial) {
    CGDirectDisplayID display_id = (CGDirectDisplayID)display.displayID;
    NSString *actual_uuid = mr_display_uuid(display_id);
    NSString *actual_vendor = mr_hex(CGDisplayVendorNumber(display_id));
    NSString *actual_model = mr_hex(CGDisplayModelNumber(display_id));
    NSString *actual_serial = mr_display_serial(display_id);
    if (wanted_uuid.length > 0) {
        return [actual_uuid isEqualToString:wanted_uuid] &&
               (wanted_vendor.length == 0 || [actual_vendor isEqualToString:wanted_vendor]) &&
               (wanted_model.length == 0 || [actual_model isEqualToString:wanted_model]) &&
               (wanted_serial.length == 0 || [actual_serial isEqualToString:wanted_serial]);
    }
    return wanted_vendor.length > 0 && wanted_model.length > 0 && wanted_serial.length > 0 &&
           [actual_vendor isEqualToString:wanted_vendor] && [actual_model isEqualToString:wanted_model] &&
           [actual_serial isEqualToString:wanted_serial];
}

static BOOL mr_display_digest_matches(SCDisplay *display, NSString *wanted_digest) API_AVAILABLE(macos(12.3));
static BOOL mr_display_digest_matches(SCDisplay *display, NSString *wanted_digest) {
    if (wanted_digest.length == 0) return NO;
    CGDirectDisplayID display_id = (CGDirectDisplayID)display.displayID;
    NSString *actual = mr_display_digest(mr_display_uuid(display_id), mr_hex(CGDisplayVendorNumber(display_id)),
                                         mr_hex(CGDisplayModelNumber(display_id)), mr_display_serial(display_id));
    return [actual isEqualToString:wanted_digest];
}

static SCDisplay *mr_select_display(const MRNativeAudioConfig *config, char **error_out) API_AVAILABLE(macos(13.0));
static SCDisplay *mr_select_display(const MRNativeAudioConfig *config, char **error_out) {
    if (@available(macOS 13.0, *)) {
        // Continue below on supported systems.
    } else {
        mr_audio_error(error_out, @"host_output is unavailable on macOS versions earlier than 13");
        return nil;
    }
    NSString *wanted_uuid = [[NSString stringWithUTF8String:config->display_hardware_uuid ?: ""] lowercaseString];
    NSString *wanted_vendor = [[NSString stringWithUTF8String:config->display_vendor ?: ""] lowercaseString];
    NSString *wanted_model = [[NSString stringWithUTF8String:config->display_model ?: ""] lowercaseString];
    NSString *wanted_serial = [[NSString stringWithUTF8String:config->display_serial ?: ""] lowercaseString];
    NSString *wanted_digest = [NSString stringWithUTF8String:config->display_digest ?: ""] ?: @"";
    if (wanted_uuid.length == 0 && wanted_serial.length == 0) {
        mr_audio_error(error_out, @"host_output display selection unavailable: persistent display identity is required");
        return nil;
    }
    __block SCDisplay *match = nil;
    __block NSUInteger identity_matches = 0;
    __block NSUInteger digest_matches = 0;
    __block NSError *shareable_error = nil;
    dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
    [SCShareableContent getShareableContentExcludingDesktopWindows:YES onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content, NSError *error) {
        shareable_error = error;
        if (error == nil) {
            for (SCDisplay *display in content.displays) {
                if (mr_display_matches(display, wanted_uuid, wanted_vendor, wanted_model, wanted_serial)) {
                    identity_matches++;
                    if (mr_display_digest_matches(display, wanted_digest)) {
                        digest_matches++;
                        match = display;
                    }
                }
            }
        }
        dispatch_semaphore_signal(semaphore);
    }];
    if (dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC)) != 0) {
        mr_audio_error(error_out, @"host_output display enumeration timed out");
        return nil;
    }
    if (shareable_error != nil) {
        mr_audio_error(error_out, [NSString stringWithFormat:@"host_output display enumeration failed: %@", shareable_error.localizedDescription ?: @"unknown error"]);
        return nil;
    }
    if (identity_matches > 0 && digest_matches == 0) {
        mr_audio_error(error_out, @"configured host_output display digest does not match the effective display identity");
        return nil;
    }
    if (digest_matches == 0) {
        mr_audio_error(error_out, @"configured host_output display is unavailable or identity changed");
        return nil;
    }
    if (digest_matches != 1 || match == nil) {
        mr_audio_error(error_out, @"configured host_output display identity is ambiguous");
        return nil;
    }
    return match;
}

static BOOL mr_run_uac_operation(MRNativeAudio *audio, BOOL start, int timeout_ms) {
    AVCaptureSession *session = audio->capture_session == NULL ? nil : (__bridge AVCaptureSession *)audio->capture_session;
    dispatch_queue_t queue = audio->control_queue == NULL ? nil : (__bridge dispatch_queue_t)audio->control_queue;
    if (session == nil || queue == nil) return NO;
    dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
    dispatch_async(queue, ^{
        if (start) {
            [session startRunning];
        } else if (session.isRunning) {
            [session stopRunning];
        }
        dispatch_semaphore_signal(semaphore);
    });
    return dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeout_ms * NSEC_PER_MSEC)) == 0;
}

static BOOL mr_open_uac(MRNativeAudio *audio, const MRNativeAudioConfig *config, char **error_out) {
    NSString *wanted_uid = [NSString stringWithUTF8String:config->endpoint_uid ?: ""];
    NSString *wanted_name = [NSString stringWithUTF8String:config->endpoint_name ?: ""];
    AVCaptureDevice *device = nil;
    for (AVCaptureDevice *candidate in mr_audio_devices()) {
        if ([candidate.uniqueID isEqualToString:wanted_uid] && [candidate.localizedName isEqualToString:wanted_name]) {
            device = candidate;
            break;
        }
    }
    if (device == nil) {
        mr_audio_error(error_out, @"configured ShadowCast audio endpoint is unavailable or identity changed");
        return NO;
    }
    NSError *error = nil;
    AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
    if (input == nil) {
        mr_audio_error(error_out, [NSString stringWithFormat:@"open configured ShadowCast audio endpoint failed: %@", error.localizedDescription ?: @"unknown error"]);
        return NO;
    }
    AVCaptureSession *session = [[AVCaptureSession alloc] init];
    if (![session canAddInput:input]) {
        mr_audio_error(error_out, @"open configured ShadowCast audio endpoint failed: audio input cannot be added");
        return NO;
    }
    AVCaptureAudioDataOutput *output = [[AVCaptureAudioDataOutput alloc] init];
    if (![session canAddOutput:output]) {
        mr_audio_error(error_out, @"open configured ShadowCast audio endpoint failed: audio output cannot be added");
        return NO;
    }
    NSDictionary *settings = @{
        AVFormatIDKey: @(kAudioFormatLinearPCM),
        AVSampleRateKey: @(config->sample_rate),
        AVNumberOfChannelsKey: @(config->channels),
        AVLinearPCMBitDepthKey: @32,
        AVLinearPCMIsFloatKey: @YES,
        AVLinearPCMIsBigEndianKey: @NO,
        AVLinearPCMIsNonInterleaved: @NO,
    };
    output.audioSettings = settings;
    AVCaptureDeviceFormat *format = device.activeFormat;
    (void)format;
    [session addInput:input];
    [session addOutput:output];
    dispatch_queue_t queue = dispatch_queue_create("com.fogcast.audio.shadowcast", DISPATCH_QUEUE_SERIAL);
    dispatch_queue_t control_queue = dispatch_queue_create("com.fogcast.audio.shadowcast.control", DISPATCH_QUEUE_SERIAL);
    dispatch_queue_set_specific(queue, mr_audio_queue_key, audio, NULL);
    MRAudioCaptureDelegate *delegate = [[MRAudioCaptureDelegate alloc] initWithAudio:audio];
    [output setSampleBufferDelegate:delegate queue:queue];
    audio->capture_session = CFBridgingRetain(session);
    audio->capture_input = CFBridgingRetain(input);
    audio->capture_output = CFBridgingRetain(output);
    audio->capture_delegate = CFBridgingRetain(delegate);
    audio->callback_queue = (__bridge_retained void *)queue;
    audio->control_queue = (__bridge_retained void *)control_queue;
    return YES;
}

static BOOL mr_open_host_output(MRNativeAudio *audio, const MRNativeAudioConfig *config, char **error_out) API_AVAILABLE(macos(13.0));
static BOOL mr_open_host_output(MRNativeAudio *audio, const MRNativeAudioConfig *config, char **error_out) {
    if (@available(macOS 13.0, *)) {
        // Continue below on supported systems.
    } else {
        mr_audio_error(error_out, @"host_output is unavailable on macOS versions earlier than 13");
        return NO;
    }
    SCDisplay *display = mr_select_display(config, error_out);
    if (display == nil) return NO;
    SCStreamConfiguration *configuration = [[SCStreamConfiguration alloc] init];
    configuration.capturesAudio = YES;
    configuration.sampleRate = config->sample_rate;
    configuration.channelCount = config->channels;
    configuration.width = 2;
    configuration.height = 2;
    configuration.minimumFrameInterval = CMTimeMake(1, 60);
    SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingApplications:@[] exceptingWindows:@[]];
    SCStream *stream = [[SCStream alloc] initWithFilter:filter configuration:configuration delegate:nil];
    dispatch_queue_t queue = dispatch_queue_create("com.fogcast.audio.host-output", DISPATCH_QUEUE_SERIAL);
    dispatch_queue_set_specific(queue, mr_audio_queue_key, audio, NULL);
    MRAudioStreamDelegate *delegate = [[MRAudioStreamDelegate alloc] initWithAudio:audio];
    NSError *error = nil;
    if (![stream addStreamOutput:delegate type:SCStreamOutputTypeAudio sampleHandlerQueue:queue error:&error]) {
        mr_audio_error(error_out, [NSString stringWithFormat:@"open host_output audio stream failed: %@", error.localizedDescription ?: @"unknown error"]);
        return NO;
    }
    audio->stream = CFBridgingRetain(stream);
    audio->stream_delegate = CFBridgingRetain(delegate);
    audio->callback_queue = (__bridge_retained void *)queue;
    return YES;
}

static BOOL mr_stop_capture(MRNativeAudio *audio) {
    AVCaptureSession *session = audio->capture_session == NULL ? nil : (__bridge AVCaptureSession *)audio->capture_session;
    AVCaptureAudioDataOutput *output = audio->capture_output == NULL ? nil : (__bridge AVCaptureAudioDataOutput *)audio->capture_output;
    if (output != nil) [output setSampleBufferDelegate:nil queue:nil];
    BOOL stopped = YES;
    if (session != nil && audio->control_queue != NULL) stopped = mr_run_uac_operation(audio, NO, 2000);
    if (audio->stream != NULL) {
        if (@available(macOS 13.0, *)) {
            SCStream *stream = (__bridge SCStream *)audio->stream;
            dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
            __block NSError *stop_error = nil;
            [stream stopCaptureWithCompletionHandler:^(NSError *error) {
                stop_error = error;
                dispatch_semaphore_signal(semaphore);
            }];
            if (dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC)) != 0 || stop_error != nil) stopped = NO;
        }
    }
    if (!stopped) {
        pthread_mutex_lock(&audio->mutex);
        mr_set_error_locked(audio, @"native audio capture stop did not complete before the cleanup deadline");
        pthread_mutex_unlock(&audio->mutex);
        return NO;
    }
    dispatch_queue_t queue = audio->callback_queue == NULL ? nil : (__bridge dispatch_queue_t)audio->callback_queue;
    if (queue != nil && !mr_on_audio_queue(audio)) dispatch_sync(queue, ^{});
    pthread_mutex_lock(&audio->mutex);
    BOOL callbacks_quiesced = audio->callbacks_inflight == 0;
    pthread_mutex_unlock(&audio->mutex);
    if (!callbacks_quiesced) {
        pthread_mutex_lock(&audio->mutex);
        mr_set_error_locked(audio, @"native audio callbacks did not quiesce before cleanup");
        pthread_mutex_unlock(&audio->mutex);
        return NO;
    }
    if (audio->callback_queue != NULL) { (void)CFBridgingRelease(audio->callback_queue); audio->callback_queue = NULL; }
    if (audio->capture_delegate != NULL) { (void)CFBridgingRelease(audio->capture_delegate); audio->capture_delegate = NULL; }
    if (audio->stream_delegate != NULL) { (void)CFBridgingRelease(audio->stream_delegate); audio->stream_delegate = NULL; }
    if (audio->capture_output != NULL) { (void)CFBridgingRelease(audio->capture_output); audio->capture_output = NULL; }
    if (audio->capture_input != NULL) { (void)CFBridgingRelease(audio->capture_input); audio->capture_input = NULL; }
    if (audio->capture_session != NULL) { (void)CFBridgingRelease(audio->capture_session); audio->capture_session = NULL; }
    if (audio->stream != NULL) { (void)CFBridgingRelease(audio->stream); audio->stream = NULL; }
    if (audio->control_queue != NULL) { (void)CFBridgingRelease(audio->control_queue); audio->control_queue = NULL; }
    return YES;
}

static BOOL mr_abort_start(MRNativeAudio *audio) {
    pthread_mutex_lock(&audio->mutex);
    audio->closing = 1;
    audio->started = 0;
    pthread_cond_broadcast(&audio->condition);
    pthread_mutex_unlock(&audio->mutex);
    return mr_stop_capture(audio);
}

static void mr_destroy_audio(MRNativeAudio *audio) {
    if (audio == NULL) return;
    pthread_mutex_lock(&audio->mutex);
    audio->closed = 1;
    free(audio->accum); audio->accum = NULL;
    free(audio->ready); audio->ready = NULL;
    free(audio->runtime_error); audio->runtime_error = NULL;
    pthread_mutex_unlock(&audio->mutex);
    pthread_cond_destroy(&audio->condition);
    pthread_mutex_destroy(&audio->mutex);
    free(audio);
}

int mr_audio_microphone_authorization_status(void) {
    return (int)[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
}

int mr_audio_request_microphone_authorization(char **error_out) {
    __block BOOL granted = NO;
    dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
    [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL access_granted) {
        granted = access_granted;
        dispatch_semaphore_signal(semaphore);
    }];
    if (dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 30 * NSEC_PER_SEC)) != 0) {
        mr_audio_error(error_out, @"the Microphone authorization prompt timed out; open System Settings > Privacy & Security > Microphone for FogCast Host Capture");
        return 0;
    }
    return granted ? mr_audio_microphone_authorization_status() : AVAuthorizationStatusDenied;
}

int mr_audio_screen_authorization_status(void) {
    // CoreGraphics exposes a preflight boolean rather than AVFoundation's
    // four-state TCC enum. A false preflight is treated as not-determined so
    // the signed helper can make one request; a failed request is returned as
    // an actionable denied status by mr_audio_request_screen_authorization.
    if (@available(macOS 10.15, *)) return CGPreflightScreenCaptureAccess() ? 3 : 0;
    return 2;
}

int mr_audio_host_output_available(void) {
    if (@available(macOS 13.0, *)) return 1;
    return 0;
}

int mr_audio_request_screen_authorization(char **error_out) {
    if (@available(macOS 10.15, *)) {
        // Continue below on supported systems.
    } else {
        mr_audio_error(error_out, @"Screen Recording is unavailable on this macOS version");
        return 2;
    }
    if (CGRequestScreenCaptureAccess()) return 3;
    mr_audio_error(error_out, @"Screen Recording access was not granted; open System Settings > Privacy & Security > Screen Recording for FogCast Host Capture");
    return 2;
}

int mr_audio_validate_uac(const char *uid, const char *name, char **error_out) {
    NSString *wanted_uid = [NSString stringWithUTF8String:uid ?: ""];
    NSString *wanted_name = [NSString stringWithUTF8String:name ?: ""];
    if (wanted_uid.length == 0 || wanted_name.length == 0) {
        mr_audio_error(error_out, @"ShadowCast audio endpoint identity is required");
        return -1;
    }
    for (AVCaptureDevice *device in mr_audio_devices()) {
        if ([device.uniqueID isEqualToString:wanted_uid] && [device.localizedName isEqualToString:wanted_name]) return 0;
    }
    mr_audio_error(error_out, @"configured ShadowCast audio endpoint is unavailable or identity changed");
    return -1;
}

int mr_audio_validate_display(const char *hardware_uuid, const char *vendor, const char *model,
                              const char *serial, char **error_out) {
    NSString *uuid = [NSString stringWithUTF8String:hardware_uuid ?: ""] ?: @"";
    NSString *vendor_string = [NSString stringWithUTF8String:vendor ?: ""] ?: @"";
    NSString *model_string = [NSString stringWithUTF8String:model ?: ""] ?: @"";
    NSString *serial_string = [NSString stringWithUTF8String:serial ?: ""] ?: @"";
    NSString *digest = mr_display_digest(uuid, vendor_string, model_string, serial_string);
    MRNativeAudioConfig config = { .kind = MR_AUDIO_KIND_HOST_OUTPUT, .display_hardware_uuid = hardware_uuid,
                                   .display_vendor = vendor, .display_model = model, .display_serial = serial,
                                   .display_digest = digest.UTF8String };
    if (@available(macOS 13.0, *)) {
        SCDisplay *display = mr_select_display(&config, error_out);
        return display == nil ? -1 : 0;
    }
    mr_audio_error(error_out, @"host_output is unavailable on macOS versions earlier than 13");
    return -1;
}

void *mr_audio_open(const MRNativeAudioConfig *config, char **error_out) {
    if (config == NULL || (config->kind != MR_AUDIO_KIND_SHADOWCAST_UAC && config->kind != MR_AUDIO_KIND_HOST_OUTPUT) ||
        config->sample_rate != 48000 || (config->channels != 1 && config->channels != 2) || config->frame_samples != 240) {
        mr_audio_error(error_out, @"native audio configuration is not the supported 48 kHz 240-frame PCM contract");
        return NULL;
    }
    MRNativeAudio *audio = calloc(1, sizeof(*audio));
    if (audio == NULL) { mr_audio_error(error_out, @"native audio source allocation failed"); return NULL; }
    pthread_mutex_init(&audio->mutex, NULL);
    pthread_cond_init(&audio->condition, NULL);
    mach_timebase_info(&audio->timebase);
    audio->kind = config->kind;
    audio->sample_rate = config->sample_rate;
    audio->channels = config->channels;
    audio->frame_samples = config->frame_samples;
    audio->accum = calloc(1, (size_t)audio->channels * 2 * audio->frame_samples);
    if (audio->accum == NULL) { mr_audio_error(error_out, @"native audio source buffer allocation failed"); mr_destroy_audio(audio); return NULL; }
    BOOL opened = NO;
    if (config->kind == MR_AUDIO_KIND_SHADOWCAST_UAC) {
        opened = mr_open_uac(audio, config, error_out);
    } else if (@available(macOS 13.0, *)) {
        opened = mr_open_host_output(audio, config, error_out);
    } else {
        mr_audio_error(error_out, @"host_output is unavailable on macOS versions earlier than 13");
    }
    if (!opened) { (void)mr_stop_capture(audio); mr_destroy_audio(audio); return NULL; }
    return audio;
}

int mr_audio_start(void *handle, char **error_out) {
    MRNativeAudio *audio = (MRNativeAudio *)handle;
    if (audio == NULL) { mr_audio_error(error_out, @"native audio source is unavailable"); return -1; }
    pthread_mutex_lock(&audio->mutex);
    if (audio->closed || audio->closing) { pthread_mutex_unlock(&audio->mutex); mr_audio_error(error_out, @"native audio source is closed"); return -1; }
    if (audio->started) { pthread_mutex_unlock(&audio->mutex); return 0; }
    audio->started = 1;
    pthread_mutex_unlock(&audio->mutex);

    if (audio->kind == MR_AUDIO_KIND_SHADOWCAST_UAC) {
        if (!mr_run_uac_operation(audio, YES, 3000)) {
            mr_audio_error(error_out, @"start ShadowCast audio capture did not complete before the startup deadline");
            (void)mr_abort_start(audio);
            return -1;
        }
    } else if (@available(macOS 13.0, *)) {
        SCStream *stream = (__bridge SCStream *)audio->stream;
        dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
        __block NSError *start_error = nil;
        [stream startCaptureWithCompletionHandler:^(NSError *error) { start_error = error; dispatch_semaphore_signal(semaphore); }];
        if (dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC)) != 0 || start_error != nil) {
            mr_audio_error(error_out, [NSString stringWithFormat:@"start host_output audio stream failed: %@", start_error.localizedDescription ?: @"timed out"]);
            mr_abort_start(audio);
            return -1;
        }
    }
    pthread_mutex_lock(&audio->mutex);
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += 3;
    while (audio->ready == NULL && audio->runtime_error == NULL && !audio->closing) {
        if (pthread_cond_timedwait(&audio->condition, &audio->mutex, &deadline) == ETIMEDOUT) break;
    }
    BOOL ready = audio->ready != NULL;
    NSString *runtime_error = audio->runtime_error == NULL ? nil : [NSString stringWithUTF8String:audio->runtime_error];
    pthread_mutex_unlock(&audio->mutex);
    if (!ready) {
        mr_audio_error(error_out, runtime_error ?: @"native audio source produced no complete 240-frame buffer before the startup deadline");
        mr_abort_start(audio);
        return -1;
    }
    return 0;
}

int mr_audio_next(void *handle, MRNativeAudioSample *sample, int timeout_ms, char **error_out) {
    MRNativeAudio *audio = (MRNativeAudio *)handle;
    if (audio == NULL || sample == NULL) { mr_audio_error(error_out, @"native audio sample request is invalid"); return -1; }
    memset(sample, 0, sizeof(*sample));
    pthread_mutex_lock(&audio->mutex);
    if (!audio->started || audio->closed) { pthread_mutex_unlock(&audio->mutex); mr_audio_error(error_out, @"native audio source is not running"); return -1; }
    if (timeout_ms < 1) timeout_ms = 1;
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += timeout_ms / 1000;
    deadline.tv_nsec += (long)(timeout_ms % 1000) * 1000000L;
    if (deadline.tv_nsec >= 1000000000L) { deadline.tv_sec++; deadline.tv_nsec -= 1000000000L; }
    while (audio->ready == NULL && !audio->closing && audio->runtime_error == NULL) {
        if (pthread_cond_timedwait(&audio->condition, &audio->mutex, &deadline) == ETIMEDOUT) { pthread_mutex_unlock(&audio->mutex); return 1; }
    }
    if (audio->ready == NULL) {
        NSString *error = audio->runtime_error == NULL ? nil : [NSString stringWithUTF8String:audio->runtime_error];
        pthread_mutex_unlock(&audio->mutex);
        mr_audio_error(error_out, error ?: @"native audio source is closed");
        return -1;
    }
    sample->pcm16 = audio->ready;
    sample->pcm16_len = audio->ready_len;
    sample->frames = audio->ready_frames;
    sample->channels = audio->channels;
    sample->capture_mono_ns = audio->ready_capture_mono_ns;
    audio->ready = NULL;
    audio->ready_len = 0;
    audio->ready_frames = 0;
    audio->ready_capture_mono_ns = 0;
    audio->queue_depth = 0;
    pthread_mutex_unlock(&audio->mutex);
    return 0;
}

void mr_audio_sample_free(MRNativeAudioSample *sample) {
    if (sample == NULL) return;
    free(sample->pcm16);
    memset(sample, 0, sizeof(*sample));
}

void mr_audio_stats(void *handle, MRNativeAudioStats *stats) {
    MRNativeAudio *audio = (MRNativeAudio *)handle;
    if (stats == NULL) return;
    memset(stats, 0, sizeof(*stats));
    if (audio == NULL) return;
    pthread_mutex_lock(&audio->mutex);
    stats->callbacks = audio->callbacks;
    stats->non_zero_samples = audio->non_zero_samples;
    stats->dropped_frames = audio->dropped_frames;
    stats->queue_depth = audio->queue_depth;
    stats->queue_high_water = audio->queue_high_water;
    stats->runtime_error = audio->runtime_error == NULL ? NULL : strdup(audio->runtime_error);
    pthread_mutex_unlock(&audio->mutex);
}

int mr_audio_close(void *handle, char **error_out) {
    MRNativeAudio *audio = (MRNativeAudio *)handle;
    if (audio == NULL) return 0;
    pthread_mutex_lock(&audio->mutex);
    if (audio->closed) { pthread_mutex_unlock(&audio->mutex); return 0; }
    audio->closing = 1;
    pthread_cond_broadcast(&audio->condition);
    pthread_mutex_unlock(&audio->mutex);
    if (!mr_stop_capture(audio)) {
        if (error_out != NULL && *error_out == NULL) {
            pthread_mutex_lock(&audio->mutex);
            *error_out = strdup(audio->runtime_error == NULL ? "native audio cleanup is incomplete; retry close" : audio->runtime_error);
            pthread_mutex_unlock(&audio->mutex);
        }
        return -1;
    }
    pthread_mutex_lock(&audio->mutex);
    audio->closed = 1;
    audio->started = 0;
    pthread_cond_broadcast(&audio->condition);
    pthread_mutex_unlock(&audio->mutex);
    mr_destroy_audio(audio);
    return 0;
}
