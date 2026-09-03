//go:build darwin && cgo

#import "player_darwin.h"

#import <AVFoundation/AVFoundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>

#include <stdlib.h>
#include <string.h>

static const int kFogcastAttractMaxW = 1280;
static const int kFogcastAttractMaxH = 720;
static const int64_t kFogcastAttractLoadTimeoutNs = 5LL * 1000000000LL;

static char *fogcast_dup_msg(NSString *msg, const char *fallback) {
	const char *s = msg.UTF8String;
	if (s == NULL || s[0] == '\0') {
		s = fallback;
	}
	return strdup(s);
}

static int64_t fogcast_monotonic_ns(void) {
	return (int64_t)([[NSProcessInfo processInfo] systemUptime] * 1000000000.0);
}

static CGAffineTransform fogcast_track_render_transform(AVAssetTrack *track, int dstW, int dstH) {
	CGSize natural = track.naturalSize;
	CGAffineTransform t = track.preferredTransform;
	CGRect bounding = CGRectApplyAffineTransform(CGRectMake(0, 0, natural.width, natural.height), t);
	t.tx -= bounding.origin.x;
	t.ty -= bounding.origin.y;
	CGFloat bw = (CGFloat)fabs(bounding.size.width);
	CGFloat bh = (CGFloat)fabs(bounding.size.height);
	if (bw < 1) {
		bw = 1;
	}
	if (bh < 1) {
		bh = 1;
	}
	return CGAffineTransformConcat(t, CGAffineTransformMakeScale((CGFloat)dstW / bw, (CGFloat)dstH / bh));
}

static void fogcast_fit_size(int srcW, int srcH, int *dstW, int *dstH) {
	if (srcW < 1) {
		srcW = 1;
	}
	if (srcH < 1) {
		srcH = 1;
	}
	if (srcW <= kFogcastAttractMaxW && srcH <= kFogcastAttractMaxH) {
		*dstW = srcW;
		*dstH = srcH;
		return;
	}
	double sx = (double)kFogcastAttractMaxW / (double)srcW;
	double sy = (double)kFogcastAttractMaxH / (double)srcH;
	double scale = sx < sy ? sx : sy;
	int w = (int)(srcW * scale + 0.5);
	int h = (int)(srcH * scale + 0.5);
	if (w < 1) {
		w = 1;
	}
	if (h < 1) {
		h = 1;
	}
	*dstW = w;
	*dstH = h;
}

@interface FogcastAttractDecoder : NSObject
@property (nonatomic, strong) AVAssetReader *reader;
@property (nonatomic, strong) AVAssetReaderOutput *output;
@property (nonatomic, assign) int width;
@property (nonatomic, assign) int height;
@property (nonatomic, assign) BOOL clockSet;
@property (nonatomic, assign) int64_t startNs;
@property (nonatomic, assign) int64_t pendingPtsNs;
@property (nonatomic, assign) CVPixelBufferRef pending;
@property (nonatomic, assign) int ended;
@end

@implementation FogcastAttractDecoder

- (void)dealloc {
	if (_pending != NULL) {
		CVPixelBufferRelease(_pending);
		_pending = NULL;
	}
	if (_reader != nil && _reader.status == AVAssetReaderStatusReading) {
		[_reader cancelReading];
	}
}

- (BOOL)pullPending:(char **)err {
	if (_ended) {
		return NO;
	}
	CMSampleBufferRef sample = [_output copyNextSampleBuffer];
	if (sample == NULL) {
		AVAssetReaderStatus status = _reader.status;
		if (status == AVAssetReaderStatusCompleted || status == AVAssetReaderStatusCancelled) {
			_ended = 1;
			return NO;
		}
		if (status == AVAssetReaderStatusFailed) {
			if (err != NULL) {
				*err = fogcast_dup_msg(_reader.error.localizedDescription, "video decode failed");
			}
			return NO;
		}
		return NO;
	}
	CMTime pts = CMSampleBufferGetPresentationTimeStamp(sample);
	if (CMTIME_IS_NUMERIC(pts)) {
		double seconds = CMTimeGetSeconds(pts);
		if (seconds < 0) {
			seconds = 0;
		}
		_pendingPtsNs = (int64_t)(seconds * 1000000000.0);
	} else {
		_pendingPtsNs = 0;
	}
	CVImageBufferRef image = CMSampleBufferGetImageBuffer(sample);
	if (image != NULL) {
		_pending = (CVPixelBufferRef)image;
		CVPixelBufferRetain(_pending);
	}
	CFRelease(sample);
	if (_pending == NULL) {
		return [self pullPending:err];
	}
	return YES;
}

- (void)blit:(CVPixelBufferRef)src to:(uint8_t *)dst stride:(int)stride height:(int)height {
	if (src == NULL || dst == NULL || stride < 4 || height < 1) {
		return;
	}
	if (CVPixelBufferLockBaseAddress(src, kCVPixelBufferLock_ReadOnly) != kCVReturnSuccess) {
		return;
	}
	uint8_t *base = (uint8_t *)CVPixelBufferGetBaseAddress(src);
	size_t srcStride = CVPixelBufferGetBytesPerRow(src);
	int w = (int)CVPixelBufferGetWidth(src);
	int h = (int)CVPixelBufferGetHeight(src);
	if (w > _width) {
		w = _width;
	}
	if (h > height) {
		h = height;
	}
	if (w < 1 || h < 1 || base == NULL) {
		CVPixelBufferUnlockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
		return;
	}
	for (int y = 0; y < h; y++) {
		uint8_t *srow = base + (size_t)y * srcStride;
		uint8_t *drow = dst + y * stride;
		for (int x = 0; x < w; x++) {
			drow[0] = srow[2];
			drow[1] = srow[1];
			drow[2] = srow[0];
			drow[3] = srow[3];
			srow += 4;
			drow += 4;
		}
	}
	CVPixelBufferUnlockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
}

@end

void *fogcast_attract_video_open(const char *path, char **err) {
	@autoreleasepool {
		if (path == NULL || path[0] == '\0') {
			if (err != NULL) {
				*err = strdup("video path is empty");
			}
			return NULL;
		}
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		AVURLAsset *asset = [AVURLAsset URLAssetWithURL:url options:@{
			AVURLAssetPreferPreciseDurationAndTimingKey : @YES
		}];
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[asset loadValuesAsynchronouslyForKeys:@[ @"tracks" ] completionHandler:^{
			dispatch_semaphore_signal(sem);
		}];
		if (dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, kFogcastAttractLoadTimeoutNs)) != 0) {
			if (err != NULL) {
				*err = strdup("video load timed out");
			}
			return NULL;
		}
		NSError *trackErr = nil;
		if ([asset statusOfValueForKey:@"tracks" error:&trackErr] != AVKeyValueStatusLoaded) {
			if (err != NULL) {
				*err = fogcast_dup_msg(trackErr.localizedDescription, "video tracks failed to load");
			}
			return NULL;
		}
		__block NSArray<AVAssetTrack *> *tracks = nil;
		__block NSError *loadTracksErr = nil;
		if (@available(macOS 12, *)) {
			dispatch_semaphore_t trackSem = dispatch_semaphore_create(0);
			[asset loadTracksWithMediaType:AVMediaTypeVideo completionHandler:^(NSArray<AVAssetTrack *> *loaded, NSError *e) {
				tracks = loaded;
				loadTracksErr = e;
				dispatch_semaphore_signal(trackSem);
			}];
			if (dispatch_semaphore_wait(trackSem, dispatch_time(DISPATCH_TIME_NOW, kFogcastAttractLoadTimeoutNs)) != 0) {
				if (err != NULL) {
					*err = strdup("video tracks timed out");
				}
				return NULL;
			}
		} else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			tracks = [asset tracksWithMediaType:AVMediaTypeVideo];
#pragma clang diagnostic pop
		}
		if (loadTracksErr != nil) {
			if (err != NULL) {
				*err = fogcast_dup_msg(loadTracksErr.localizedDescription, "video tracks failed to load");
			}
			return NULL;
		}
		if (tracks.count < 1) {
			if (err != NULL) {
				*err = strdup("video has no video track");
			}
			return NULL;
		}
		AVAssetTrack *track = tracks.firstObject;
		CGSize natural = track.naturalSize;
		CGSize transformed = CGSizeApplyAffineTransform(natural, track.preferredTransform);
		int srcW = (int)fabs(transformed.width);
		int srcH = (int)fabs(transformed.height);
		if (srcW < 1 || srcH < 1) {
			srcW = (int)natural.width;
			srcH = (int)natural.height;
		}
		if (srcW < 1 || srcH < 1) {
			if (err != NULL) {
				*err = strdup("video dimensions are invalid");
			}
			return NULL;
		}
		int dstW = 0, dstH = 0;
		fogcast_fit_size(srcW, srcH, &dstW, &dstH);

		NSError *readerErr = nil;
		AVAssetReader *reader = [AVAssetReader assetReaderWithAsset:asset error:&readerErr];
		if (reader == nil) {
			if (err != NULL) {
				*err = fogcast_dup_msg(readerErr.localizedDescription, "asset reader failed");
			}
			return NULL;
		}
		NSDictionary *settings = @{
			(id)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_32BGRA)
		};
		AVMutableVideoComposition *composition = [AVMutableVideoComposition videoComposition];
		CMTime frameDuration = track.minFrameDuration;
		if (!CMTIME_IS_NUMERIC(frameDuration) || CMTIME_COMPARE_INLINE(frameDuration, <=, kCMTimeZero)) {
			float fps = track.nominalFrameRate;
			if (fps < 1.f) {
				fps = 30.f;
			}
			frameDuration = CMTimeMake(1, (int32_t)(fps + 0.5f));
		}
		composition.frameDuration = frameDuration;
		composition.renderSize = CGSizeMake(dstW, dstH);
		CMTime duration = asset.duration;
		if (!CMTIME_IS_NUMERIC(duration) || CMTIME_COMPARE_INLINE(duration, <=, kCMTimeZero)) {
			duration = kCMTimePositiveInfinity;
		}
		AVMutableVideoCompositionInstruction *instruction = [AVMutableVideoCompositionInstruction videoCompositionInstruction];
		instruction.timeRange = CMTimeRangeMake(kCMTimeZero, duration);
		AVMutableVideoCompositionLayerInstruction *layer =
			[AVMutableVideoCompositionLayerInstruction videoCompositionLayerInstructionWithAssetTrack:track];
		[layer setTransform:fogcast_track_render_transform(track, dstW, dstH) atTime:kCMTimeZero];
		instruction.layerInstructions = @[ layer ];
		composition.instructions = @[ instruction ];
		AVAssetReaderVideoCompositionOutput *output =
			[AVAssetReaderVideoCompositionOutput assetReaderVideoCompositionOutputWithVideoTracks:@[ track ]
										      videoSettings:settings];
		output.videoComposition = composition;
		output.alwaysCopiesSampleData = NO;
		if (![reader canAddOutput:output]) {
			if (err != NULL) {
				*err = strdup("cannot add video output");
			}
			return NULL;
		}
		[reader addOutput:output];
		if (![reader startReading]) {
			if (err != NULL) {
				*err = fogcast_dup_msg(reader.error.localizedDescription, "start reading failed");
			}
			return NULL;
		}
		FogcastAttractDecoder *dec = [FogcastAttractDecoder new];
		dec.reader = reader;
		dec.output = output;
		dec.width = dstW;
		dec.height = dstH;
		return (__bridge_retained void *)dec;
	}
}

int fogcast_attract_video_dimensions(void *video, int *w, int *h) {
	if (video == NULL) {
		return 0;
	}
	FogcastAttractDecoder *dec = (__bridge FogcastAttractDecoder *)video;
	if (w != NULL) {
		*w = dec.width;
	}
	if (h != NULL) {
		*h = dec.height;
	}
	return 1;
}

int fogcast_attract_video_copy_rgba(void *video, uint8_t *buf, int stride, int height, int *ended, char **err) {
	if (video == NULL) {
		if (err != NULL) {
			*err = strdup("video player is nil");
		}
		return -2;
	}
	@autoreleasepool {
		FogcastAttractDecoder *dec = (__bridge FogcastAttractDecoder *)video;
		if (dec.ended && dec.pending == NULL) {
			if (ended != NULL) {
				*ended = 1;
			}
			return -1;
		}
		int64_t now = fogcast_monotonic_ns();
		BOOL first = !dec.clockSet;
		CVPixelBufferRef due = NULL;
		while (1) {
			if (dec.pending == NULL) {
				char *pullErr = NULL;
				if (![dec pullPending:&pullErr]) {
					if (pullErr != NULL) {
						if (due != NULL) {
							CVPixelBufferRelease(due);
						}
						if (err != NULL) {
							*err = pullErr;
						}
						return -2;
					}
					break;
				}
			}
			if (!dec.clockSet) {
				dec.startNs = now - dec.pendingPtsNs;
				dec.clockSet = YES;
			} else if (dec.pendingPtsNs > (now - dec.startNs)) {
				break;
			}
			if (due != NULL) {
				CVPixelBufferRelease(due);
			}
			due = dec.pending;
			dec.pending = NULL;
			if (first) {
				break;
			}
		}
		if (due != NULL) {
			[dec blit:due to:buf stride:stride height:height];
			CVPixelBufferRelease(due);
			if (dec.ended && dec.pending == NULL && ended != NULL) {
				*ended = 1;
			}
			return 1;
		}
		if (dec.ended) {
			if (ended != NULL) {
				*ended = 1;
			}
			return -1;
		}
		return 0;
	}
}

void fogcast_attract_video_close(void *video) {
	if (video == NULL) {
		return;
	}
	@autoreleasepool {
		FogcastAttractDecoder *dec = (__bridge_transfer FogcastAttractDecoder *)video;
		if (dec.reader != nil && dec.reader.status == AVAssetReaderStatusReading) {
			[dec.reader cancelReading];
		}
		dec = nil;
	}
}
