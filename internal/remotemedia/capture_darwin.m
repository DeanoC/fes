#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <VideoToolbox/VideoToolbox.h>
#include <mach/mach_time.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

#include "capture_darwin.h"

typedef struct MRFrameTiming {
    int64_t capture_mono_ns;
    int64_t submitted_mono_ns;
    CVPixelBufferRef pixel_buffer;
} MRFrameTiming;

typedef struct {
    pthread_mutex_t mutex;
    pthread_cond_t condition;
    CFTypeRef session;
    CFTypeRef output;
    CFTypeRef delegate;
    void *callback_queue;
    VTCompressionSessionRef encoder;
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
    int encode_in_flight;
    int started;
    int has_sample;
    int closing;
    uint64_t captured_frames;
    uint64_t dropped_frames;
    uint64_t encoded_frames;
    uint64_t encode_errors;
    int queue_depth;
    int queue_high_water;
    uint64_t packet_queue_drops;
    int fps_numerator;
    int fps_denominator;
    char *runtime_error;
    mach_timebase_info_data_t timebase;
    MRFrameTiming frame_timing;
    MRFrameTiming *pending_timing;
    CMTime last_pts;
    int has_last_pts;
} MRNativeCapture;

static void mr_release_encoder(MRNativeCapture *capture) {
    if (capture->encoder == NULL) return;
    VTCompressionSessionCompleteFrames(capture->encoder, kCMTimeInvalid);
    pthread_mutex_lock(&capture->mutex);
    while (capture->encode_in_flight) {
        pthread_cond_wait(&capture->condition, &capture->mutex);
    }
    pthread_mutex_unlock(&capture->mutex);
    VTCompressionSessionInvalidate(capture->encoder);
    CFRelease(capture->encoder);
    capture->encoder = NULL;
}

static void mr_release_queue(MRNativeCapture *capture) {
    if (capture->callback_queue == NULL) return;
    dispatch_queue_t queue = (__bridge_transfer dispatch_queue_t)capture->callback_queue;
    dispatch_sync(queue, ^{});
    capture->callback_queue = NULL;
}

static void mr_stop_capture(MRNativeCapture *capture) {
    AVCaptureVideoDataOutput *output = capture->output == NULL ? nil : (__bridge AVCaptureVideoDataOutput *)capture->output;
    if (output != nil) [output setSampleBufferDelegate:nil queue:NULL];
    mr_release_queue(capture);
    mr_release_encoder(capture);
}

static void mr_stop_session(MRNativeCapture *capture) {
    AVCaptureSession *session = capture->session == NULL ? nil : (__bridge AVCaptureSession *)capture->session;
    if (session != nil && session.isRunning) [session stopRunning];
}

static void mr_release_object_fields(MRNativeCapture *capture) {
    if (capture->output != NULL) { CFRelease(capture->output); capture->output = NULL; }
    if (capture->session != NULL) { CFRelease(capture->session); capture->session = NULL; }
    if (capture->delegate != NULL) { CFRelease(capture->delegate); capture->delegate = NULL; }
}

static NSArray<AVCaptureDevice *> *mr_video_devices(void) {
    AVCaptureDeviceDiscoverySession *discovery =
        [AVCaptureDeviceDiscoverySession discoverySessionWithDeviceTypes:@[AVCaptureDeviceTypeExternal]
                                                                  mediaType:AVMediaTypeVideo
                                                                   position:AVCaptureDevicePositionUnspecified];
    return discovery.devices;
}

static char *mr_error(NSString *message) {
    const char *utf8 = message.UTF8String;
    if (utf8 == NULL) return NULL;
    return strdup(utf8);
}

static BOOL mr_set_encoder_property(VTCompressionSessionRef encoder, CFStringRef key,
                                    CFTypeRef value, NSString *name, char **error_out) {
    OSStatus status = VTSessionSetProperty(encoder, key, value);
    if (status == noErr) return YES;
    if (error_out != NULL) {
        *error_out = mr_error([NSString stringWithFormat:@"VideoToolbox %@ failed: %d", name, (int)status]);
    }
    return NO;
}

static void mr_try_encoder_property(VTCompressionSessionRef encoder, CFStringRef key,
                                    CFTypeRef value) {
    (void)VTSessionSetProperty(encoder, key, value);
}

static int64_t mr_monotonic_ns(MRNativeCapture *capture) {
    uint64_t ticks = mach_absolute_time();
    return (int64_t)((ticks * capture->timebase.numer) / capture->timebase.denom);
}

static void mr_set_error_locked(MRNativeCapture *capture, NSString *message) {
    free(capture->runtime_error);
    capture->runtime_error = mr_error(message);
}

static void mr_clear_sample_locked(MRNativeCapture *capture) {
    free(capture->avcc); capture->avcc = NULL; capture->avcc_len = 0;
    free(capture->sps); capture->sps = NULL; capture->sps_len = 0;
    free(capture->pps); capture->pps = NULL; capture->pps_len = 0;
    capture->has_sample = 0;
    capture->queue_depth = 0;
}

static void mr_release_timing_buffer_locked(MRFrameTiming *timing) {
    if (timing == NULL || timing->pixel_buffer == NULL) return;
    CVPixelBufferRelease(timing->pixel_buffer);
    timing->pixel_buffer = NULL;
}

static void mr_free_pending_timing_locked(MRNativeCapture *capture) {
    if (capture->pending_timing == NULL) return;
    mr_release_timing_buffer_locked(capture->pending_timing);
    capture->pending_timing = NULL;
    capture->encode_in_flight = 0;
}

static void mr_compression_output(void *refcon, void *sourceFrameRefcon,
                                  OSStatus status, VTEncodeInfoFlags infoFlags,
                                  CMSampleBufferRef sampleBuffer) {
    MRNativeCapture *capture = (MRNativeCapture *)refcon;
    (void)infoFlags;
    MRFrameTiming *timing = (MRFrameTiming *)sourceFrameRefcon;

    pthread_mutex_lock(&capture->mutex);
    if (timing == NULL || timing != &capture->frame_timing ||
        capture->pending_timing != timing || !capture->encode_in_flight) {
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    int64_t capture_mono_ns = timing->capture_mono_ns;
    int64_t encode_duration_ns = mr_monotonic_ns(capture) - timing->submitted_mono_ns;
    mr_release_timing_buffer_locked(timing);
    capture->pending_timing = NULL;
    capture->encode_in_flight = 0;
    capture->capture_mono_ns = capture_mono_ns;
    capture->encode_duration_ns = encode_duration_ns;
    if (capture->closing) {
        pthread_cond_signal(&capture->condition);
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    if (status != noErr || sampleBuffer == NULL) {
        capture->encode_errors++;
        mr_set_error_locked(capture, [NSString stringWithFormat:@"VideoToolbox encode failed: %d", (int)status]);
        pthread_cond_signal(&capture->condition);
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    CMBlockBufferRef block = CMSampleBufferGetDataBuffer(sampleBuffer);
    size_t block_length = block == NULL ? 0 : CMBlockBufferGetDataLength(block);
    if (block == NULL || block_length == 0) {
        capture->encode_errors++;
        mr_set_error_locked(capture, @"VideoToolbox returned no sample block");
        pthread_cond_signal(&capture->condition);
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    uint8_t *bytes = malloc(block_length);
    if (bytes == NULL || CMBlockBufferCopyDataBytes(block, 0, block_length, bytes) != kCMBlockBufferNoErr) {
        free(bytes);
        capture->encode_errors++;
        mr_set_error_locked(capture, @"VideoToolbox sample copy failed");
        pthread_cond_signal(&capture->condition);
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    CMFormatDescriptionRef format = CMSampleBufferGetFormatDescription(sampleBuffer);
    const uint8_t *parameter_set = NULL;
    size_t parameter_set_size = 0;
    size_t parameter_set_count = 0;
    int nal_length_size = 4;
    if (format != NULL) {
        if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 0, &parameter_set,
                                                                &parameter_set_size,
                                                                &parameter_set_count,
                                                                &nal_length_size) == noErr && parameter_set_size > 0) {
            if (capture->sps_len != parameter_set_size || memcmp(capture->sps, parameter_set, parameter_set_size) != 0) {
                uint8_t *copy = malloc(parameter_set_size);
                if (copy == NULL) {
                    free(bytes);
                    capture->encode_errors++;
                    mr_set_error_locked(capture, @"SPS allocation failed");
                    pthread_cond_signal(&capture->condition);
                    pthread_mutex_unlock(&capture->mutex);
                    return;
                }
                memcpy(copy, parameter_set, parameter_set_size);
                free(capture->sps);
                capture->sps = copy;
                capture->sps_len = parameter_set_size;
            }
        }
        if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 1, &parameter_set,
                                                                &parameter_set_size,
                                                                &parameter_set_count,
                                                                &nal_length_size) == noErr && parameter_set_size > 0) {
            if (capture->pps_len != parameter_set_size || memcmp(capture->pps, parameter_set, parameter_set_size) != 0) {
                uint8_t *copy = malloc(parameter_set_size);
                if (copy == NULL) {
                    free(bytes);
                    capture->encode_errors++;
                    mr_set_error_locked(capture, @"PPS allocation failed");
                    pthread_cond_signal(&capture->condition);
                    pthread_mutex_unlock(&capture->mutex);
                    return;
                }
                memcpy(copy, parameter_set, parameter_set_size);
                free(capture->pps);
                capture->pps = copy;
                capture->pps_len = parameter_set_size;
            }
        }
    }
    if (capture->has_sample) {
        capture->dropped_frames++;
        capture->packet_queue_drops++;
        mr_clear_sample_locked(capture);
    }
    capture->avcc = bytes;
    capture->avcc_len = block_length;
    capture->nal_length_size = nal_length_size;
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false);
    CFDictionaryRef attachment = attachments != NULL && CFArrayGetCount(attachments) > 0 ? CFArrayGetValueAtIndex(attachments, 0) : NULL;
    CFTypeRef not_sync = attachment == NULL ? NULL : CFDictionaryGetValue(attachment, kCMSampleAttachmentKey_NotSync);
    capture->keyframe = not_sync == NULL || !CFBooleanGetValue((CFBooleanRef)not_sync);
    CMVideoDimensions dimensions = format == NULL ? (CMVideoDimensions){0, 0} : CMVideoFormatDescriptionGetDimensions(format);
    capture->width = dimensions.width;
    capture->height = dimensions.height;
    capture->encoded_frames++;
    capture->has_sample = 1;
    capture->queue_depth = 1;
    capture->queue_high_water = 1;
    pthread_cond_signal(&capture->condition);
    pthread_mutex_unlock(&capture->mutex);
}

@interface MRVideoDelegate : NSObject <AVCaptureVideoDataOutputSampleBufferDelegate>
@property(nonatomic, assign) MRNativeCapture *capture;
@end

@implementation MRVideoDelegate
- (void)captureOutput:(AVCaptureOutput *)output didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer fromConnection:(AVCaptureConnection *)connection {
    (void)output; (void)connection;
    MRNativeCapture *capture = self.capture;
    pthread_mutex_lock(&capture->mutex);
    capture->captured_frames++;
    if (capture->closing) {
        capture->dropped_frames++;
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    if (capture->has_sample) {
        capture->dropped_frames++;
        capture->packet_queue_drops++;
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    if (capture->encode_in_flight) {
        capture->dropped_frames++;
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    int64_t capture_mono_ns = mr_monotonic_ns(capture);
    CVPixelBufferRef pixel_buffer = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (pixel_buffer == NULL) {
        capture->dropped_frames++;
        capture->encode_errors++;
        mr_set_error_locked(capture, @"capture sample has no pixel buffer");
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    MRFrameTiming *timing = &capture->frame_timing;
    timing->capture_mono_ns = capture_mono_ns;
    timing->submitted_mono_ns = mr_monotonic_ns(capture);
    timing->pixel_buffer = CVPixelBufferRetain(pixel_buffer);
    capture->pending_timing = timing;
    capture->encode_in_flight = 1;
    pthread_mutex_unlock(&capture->mutex);
    CMTime pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer);
    pthread_mutex_lock(&capture->mutex);
    BOOL invalid_pts = !CMTIME_IS_VALID(pts) || !CMTIME_IS_NUMERIC(pts) || pts.value < 0 || pts.timescale <= 0 || (capture->has_last_pts && CMTimeCompare(pts, capture->last_pts) <= 0);
    if (!invalid_pts) {
        capture->last_pts = pts;
        capture->has_last_pts = 1;
    }
    pthread_mutex_unlock(&capture->mutex);
    if (invalid_pts) {
        pthread_mutex_lock(&capture->mutex);
        if (capture->pending_timing == timing) capture->pending_timing = NULL;
        capture->encode_in_flight = 0;
        capture->dropped_frames++;
        capture->encode_errors++;
        mr_set_error_locked(capture, @"capture sample has an invalid presentation timestamp");
        pthread_cond_signal(&capture->condition);
        mr_release_timing_buffer_locked(timing);
        pthread_mutex_unlock(&capture->mutex);
        return;
    }
    VTEncodeInfoFlags flags = 0;
    OSStatus status = VTCompressionSessionEncodeFrame(capture->encoder, pixel_buffer, pts, kCMTimeInvalid, NULL, timing, &flags);
    if (status != noErr) {
        pthread_mutex_lock(&capture->mutex);
        if (capture->pending_timing == timing) capture->pending_timing = NULL;
        capture->encode_in_flight = 0;
        capture->dropped_frames++;
        capture->encode_errors++;
        mr_set_error_locked(capture, [NSString stringWithFormat:@"VTCompressionSessionEncodeFrame failed: %d", (int)status]);
        pthread_cond_signal(&capture->condition);
        mr_release_timing_buffer_locked(timing);
        pthread_mutex_unlock(&capture->mutex);
    }
}
- (void)captureOutput:(AVCaptureOutput *)output didDropSampleBuffer:(CMSampleBufferRef)sampleBuffer fromConnection:(AVCaptureConnection *)connection {
    (void)output; (void)sampleBuffer; (void)connection;
    MRNativeCapture *capture = self.capture;
    pthread_mutex_lock(&capture->mutex);
    capture->dropped_frames++;
    pthread_mutex_unlock(&capture->mutex);
}
@end

static void mr_release_capture(MRNativeCapture *capture) {
    if (capture == NULL) return;
    mr_stop_capture(capture);
    mr_release_object_fields(capture);
    pthread_mutex_lock(&capture->mutex);
    mr_clear_sample_locked(capture);
    mr_free_pending_timing_locked(capture);
    pthread_mutex_unlock(&capture->mutex);
    pthread_cond_destroy(&capture->condition);
    pthread_mutex_destroy(&capture->mutex);
    free(capture);
}

void *mr_capture_open(const char *device_identifier, int width, int height,
                      int fps_numerator, int fps_denominator, int bitrate,
                      int gop, char **error_out) {
    if (error_out != NULL) *error_out = NULL;
    @autoreleasepool {
        if (device_identifier == NULL || device_identifier[0] == '\0') {
            if (error_out != NULL) *error_out = mr_error(@"a physical capture-device identifier is required");
            return NULL;
        }
        MRNativeCapture *capture = calloc(1, sizeof(MRNativeCapture));
        if (capture == NULL) { if (error_out != NULL) *error_out = mr_error(@"capture allocation failed"); return NULL; }
        mach_timebase_info(&capture->timebase);
        pthread_mutex_init(&capture->mutex, NULL);
        pthread_cond_init(&capture->condition, NULL);
        NSString *device_identifier_string = [NSString stringWithUTF8String:device_identifier];
        AVCaptureDevice *device = nil;
        NSArray<AVCaptureDevice *> *devices = mr_video_devices();
        for (AVCaptureDevice *candidate in devices) {
            if ([candidate.uniqueID isEqualToString:device_identifier_string] ||
                [candidate.localizedName isEqualToString:device_identifier_string]) {
                device = candidate;
                break;
            }
        }
        if (device == nil) {
            if (error_out != NULL) *error_out = mr_error([NSString stringWithFormat:@"physical HDMI capture device %s was not found", device_identifier]);
            mr_release_capture(capture); return NULL;
        }
        NSError *input_error = nil;
        AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&input_error];
        if (input == nil) {
            if (error_out != NULL) *error_out = mr_error(input_error.localizedDescription ?: @"cannot open capture device");
            mr_release_capture(capture); return NULL;
        }
        capture->session = (__bridge_retained CFTypeRef)[[AVCaptureSession alloc] init];
        AVCaptureSession *session = (__bridge AVCaptureSession *)capture->session;
        BOOL can_add_input = [session canAddInput:input];
        capture->output = (__bridge_retained CFTypeRef)[[AVCaptureVideoDataOutput alloc] init];
        AVCaptureVideoDataOutput *output = (__bridge AVCaptureVideoDataOutput *)capture->output;
        output.alwaysDiscardsLateVideoFrames = YES;
        output.videoSettings = @{(id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA)};
        BOOL can_add_output = [session canAddOutput:output];
        if (!can_add_input || !can_add_output) {
            if (error_out != NULL) *error_out = mr_error(@"capture session cannot add device input/output");
            mr_release_capture(capture); return NULL;
        }
        BOOL requested_mode = width > 0 || height > 0 || fps_numerator > 0;
        BOOL selected_mode = !requested_mode;
        if (requested_mode) {
            for (AVCaptureDeviceFormat *format in device.formats) {
                CMVideoDimensions dimensions = CMVideoFormatDescriptionGetDimensions(format.formatDescription);
                if (width > 0 && dimensions.width != width) continue;
                if (height > 0 && dimensions.height != height) continue;
                if (fps_numerator > 0 && fps_denominator > 0) {
                    double requested_fps = (double)fps_numerator / (double)fps_denominator;
                    BOOL supported = NO;
                    for (AVFrameRateRange *range in format.videoSupportedFrameRateRanges) {
                        if (requested_fps >= range.minFrameRate && requested_fps <= range.maxFrameRate) {
                            supported = YES;
                            break;
                        }
                    }
                    if (!supported) continue;
                }
                NSError *configuration_error = nil;
                if (![device lockForConfiguration:&configuration_error]) {
                    if (error_out != NULL) *error_out = mr_error(configuration_error.localizedDescription ?: @"cannot configure capture device");
                    mr_release_capture(capture); return NULL;
                }
                device.activeFormat = format;
                if (fps_numerator > 0 && fps_denominator > 0) {
                    CMTime duration = CMTimeMake(fps_denominator, fps_numerator);
                    device.activeVideoMinFrameDuration = duration;
                    device.activeVideoMaxFrameDuration = duration;
                }
                [device unlockForConfiguration];
                selected_mode = YES;
                break;
            }
        }
        if (!selected_mode) {
            if (error_out != NULL) *error_out = mr_error(@"capture device does not support the requested resolution/frame rate");
            mr_release_capture(capture); return NULL;
        }
        CMVideoDimensions source_dimensions = CMVideoFormatDescriptionGetDimensions(device.activeFormat.formatDescription);
        if (source_dimensions.width <= 0 || source_dimensions.height <= 0) {
            if (error_out != NULL) *error_out = mr_error(@"capture device has no active video dimensions");
            mr_release_capture(capture); return NULL;
        }
        capture->width = source_dimensions.width;
        capture->height = source_dimensions.height;
        CMTime source_duration = device.activeVideoMinFrameDuration;
        if (!CMTIME_IS_VALID(source_duration) || source_duration.value <= 0 || source_duration.timescale <= 0) {
            source_duration = device.activeVideoMaxFrameDuration;
        }
        if (CMTIME_IS_VALID(source_duration) && source_duration.value > 0 && source_duration.timescale > 0) {
            capture->fps_numerator = source_duration.timescale;
            capture->fps_denominator = source_duration.value;
        } else {
            capture->fps_numerator = 0;
            capture->fps_denominator = 0;
        }
        [session addInput:input];
        [session addOutput:output];
        capture->delegate = (__bridge_retained CFTypeRef)[[MRVideoDelegate alloc] init];
        MRVideoDelegate *delegate = (__bridge MRVideoDelegate *)capture->delegate;
        delegate.capture = capture;
        dispatch_queue_t callback_queue = dispatch_queue_create("com.fogcast.remote-media.capture", DISPATCH_QUEUE_SERIAL);
    capture->callback_queue = (__bridge_retained void *)callback_queue;
    [output setSampleBufferDelegate:delegate queue:callback_queue];
        int encode_width = source_dimensions.width;
        int encode_height = source_dimensions.height;
        double encode_fps = 60.0;
        if (capture->fps_numerator > 0 && capture->fps_denominator > 0) {
            encode_fps = (double)capture->fps_numerator / (double)capture->fps_denominator;
        } else if (fps_numerator > 0 && fps_denominator > 0) {
            encode_fps = (double)fps_numerator / (double)fps_denominator;
        }
        NSDictionary *encoder_specification = @{
            (__bridge NSString *)kVTVideoEncoderSpecification_EnableHardwareAcceleratedVideoEncoder: @YES,
            (__bridge NSString *)kVTVideoEncoderSpecification_EnableLowLatencyRateControl: @YES,
        };
        OSStatus status = VTCompressionSessionCreate(NULL, encode_width, encode_height, kCMVideoCodecType_H264,
                                                      (__bridge CFDictionaryRef)encoder_specification, NULL, NULL,
                                                      mr_compression_output, capture, &capture->encoder);
        if (status != noErr) {
            if (error_out != NULL) *error_out = mr_error([NSString stringWithFormat:@"VideoToolbox H.264 session creation failed: %d", (int)status]);
            mr_release_capture(capture); return NULL;
        }
        mr_try_encoder_property(capture->encoder, kVTCompressionPropertyKey_PrioritizeEncodingSpeedOverQuality,
                                 kCFBooleanTrue);
        if (!mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_RealTime,
                                     kCFBooleanTrue, @"real-time mode", error_out) ||
            !mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_AllowFrameReordering,
                                     kCFBooleanFalse, @"frame reordering policy", error_out) ||
            !mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_MaxKeyFrameInterval,
                                     (__bridge CFTypeRef)@(gop > 0 ? gop : 30), @"keyframe interval", error_out) ||
            !mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_MaxKeyFrameIntervalDuration,
                                     (__bridge CFTypeRef)@(0.5), @"keyframe interval duration", error_out) ||
            !mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_AverageBitRate,
                                     (__bridge CFTypeRef)@(bitrate > 0 ? bitrate : 8000000), @"average bitrate", error_out) ||
            !mr_set_encoder_property(capture->encoder, kVTCompressionPropertyKey_ExpectedFrameRate,
                                     (__bridge CFTypeRef)@(encode_fps), @"expected frame rate", error_out)) {
            mr_release_capture(capture);
            return NULL;
        }
        status = VTCompressionSessionPrepareToEncodeFrames(capture->encoder);
        if (status != noErr) {
            if (error_out != NULL) *error_out = mr_error([NSString stringWithFormat:@"VideoToolbox encoder preparation failed: %d", (int)status]);
            mr_release_capture(capture);
            return NULL;
        }
        return capture;
    }
}

int mr_capture_start(void *handle, char **error_out) {
    if (error_out != NULL) *error_out = NULL;
    MRNativeCapture *capture = (MRNativeCapture *)handle;
    if (capture == NULL || capture->session == NULL) { if (error_out != NULL) *error_out = mr_error(@"capture handle is not initialized"); return -1; }
    pthread_mutex_lock(&capture->mutex);
    if (capture->started) {
        pthread_mutex_unlock(&capture->mutex);
        return 0;
    }
    pthread_mutex_unlock(&capture->mutex);
    AVCaptureSession *session = (__bridge AVCaptureSession *)capture->session;
    [session startRunning];
    pthread_mutex_lock(&capture->mutex);
    capture->started = session.isRunning ? 1 : 0;
    pthread_mutex_unlock(&capture->mutex);
    if (!session.isRunning && error_out != NULL) *error_out = mr_error(@"capture session did not start");
    return session.isRunning ? 0 : -1;
}

int mr_capture_next(void *handle, MRNativeSample *sample, int timeout_ms, char **error_out) {
    if (error_out != NULL) *error_out = NULL;
    MRNativeCapture *capture = (MRNativeCapture *)handle;
    if (capture == NULL || sample == NULL) { if (error_out != NULL) *error_out = mr_error(@"capture handle or sample is null"); return -1; }
    memset(sample, 0, sizeof(*sample));
    pthread_mutex_lock(&capture->mutex);
    if (!capture->has_sample && timeout_ms > 0) {
        struct timespec deadline;
        clock_gettime(CLOCK_REALTIME, &deadline);
        deadline.tv_sec += timeout_ms / 1000;
        deadline.tv_nsec += (timeout_ms % 1000) * 1000000;
        if (deadline.tv_nsec >= 1000000000) { deadline.tv_sec++; deadline.tv_nsec -= 1000000000; }
        pthread_cond_timedwait(&capture->condition, &capture->mutex, &deadline);
    }
    if (!capture->has_sample) {
        if (capture->runtime_error != NULL && error_out != NULL) *error_out = strdup(capture->runtime_error);
        pthread_mutex_unlock(&capture->mutex);
        return 1;
    }
    sample->avcc = capture->avcc; sample->avcc_len = capture->avcc_len; capture->avcc = NULL; capture->avcc_len = 0;
    sample->sps = capture->sps; sample->sps_len = capture->sps_len; capture->sps = NULL; capture->sps_len = 0;
    sample->pps = capture->pps; sample->pps_len = capture->pps_len; capture->pps = NULL; capture->pps_len = 0;
    sample->nal_length_size = capture->nal_length_size; sample->keyframe = capture->keyframe;
    sample->width = capture->width; sample->height = capture->height; sample->capture_mono_ns = capture->capture_mono_ns;
    sample->encode_duration_ns = capture->encode_duration_ns; capture->has_sample = 0; capture->queue_depth = 0;
    pthread_mutex_unlock(&capture->mutex);
    return 0;
}

void mr_capture_sample_free(MRNativeSample *sample) {
    if (sample == NULL) return;
    free(sample->avcc); free(sample->sps); free(sample->pps); memset(sample, 0, sizeof(*sample));
}

void mr_capture_stats(void *handle, MRNativeStats *stats) {
    if (handle == NULL || stats == NULL) return;
    MRNativeCapture *capture = (MRNativeCapture *)handle;
    pthread_mutex_lock(&capture->mutex);
    memset(stats, 0, sizeof(*stats));
    stats->width = capture->width; stats->height = capture->height; stats->fps_numerator = capture->fps_numerator; stats->fps_denominator = capture->fps_denominator; stats->captured_frames = capture->captured_frames;
    stats->dropped_frames = capture->dropped_frames; stats->encoded_frames = capture->encoded_frames;
    stats->encode_errors = capture->encode_errors; stats->packet_queue_drops = capture->packet_queue_drops; stats->queue_depth = capture->queue_depth; stats->queue_high_water = capture->queue_high_water;
    stats->runtime_error = capture->runtime_error == NULL ? NULL : strdup(capture->runtime_error);
    pthread_mutex_unlock(&capture->mutex);
}

void mr_capture_close(void *handle) {
    MRNativeCapture *capture = (MRNativeCapture *)handle;
    if (capture == NULL) return;
    pthread_mutex_lock(&capture->mutex); capture->closing = 1; pthread_mutex_unlock(&capture->mutex);
    mr_stop_session(capture);
    mr_stop_capture(capture);
    pthread_mutex_lock(&capture->mutex); mr_clear_sample_locked(capture); free(capture->runtime_error); capture->runtime_error = NULL; mr_free_pending_timing_locked(capture); pthread_mutex_unlock(&capture->mutex);
    mr_release_object_fields(capture);
    pthread_cond_destroy(&capture->condition); pthread_mutex_destroy(&capture->mutex); free(capture);
}

char *mr_capture_list_devices(void) {
    @autoreleasepool {
        NSMutableArray *devices = [NSMutableArray array];
        for (AVCaptureDevice *device in mr_video_devices()) {
            [devices addObject:@{ @"name": device.localizedName ?: @"", @"unique_id": device.uniqueID ?: @"", @"external": @YES }];
        }
        NSError *error = nil;
        NSData *data = [NSJSONSerialization dataWithJSONObject:devices options:0 error:&error];
        if (error != nil || data == nil) return NULL;
        return strndup(data.bytes, data.length);
    }
}

void mr_capture_free_string(char *value) { free(value); }
