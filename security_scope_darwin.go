//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Foundation -framework AppKit
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <CoreFoundation/CoreFoundation.h>
#import <dispatch/dispatch.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    char *path;
    unsigned char *bookmark;
    long bookmark_len;
    void *url;
    char *error;
    int cancelled;
} YTDirectoryAccess;

static void YTSetError(YTDirectoryAccess *result, NSError *error) {
    if (error != nil) {
        const char *message = [[error localizedDescription] UTF8String];
        result->error = strdup(message ? message : "macOS file access failed");
    }
}

static void YTCopyBookmarkAndPath(YTDirectoryAccess *result, NSURL *url) {
    NSError *error = nil;
    NSData *data = [url bookmarkDataWithOptions:NSURLBookmarkCreationWithSecurityScope
                  includingResourceValuesForKeys:nil relativeToURL:nil error:&error];
    if (data == nil) {
        YTSetError(result, error);
        return;
    }
    const char *path = [[url path] fileSystemRepresentation];
    if (result->path == NULL) {
        result->path = path ? strdup(path) : NULL;
    }
    if (result->path == NULL) {
        result->error = strdup("could not copy selected folder path");
        return;
    }
    result->bookmark_len = (long)[data length];
    result->bookmark = malloc((size_t)result->bookmark_len);
    if (result->bookmark == NULL) {
        result->bookmark_len = 0;
        result->error = strdup("could not allocate bookmark data");
        return;
    }
    memcpy(result->bookmark, [data bytes], (size_t)result->bookmark_len);
}

static YTDirectoryAccess *YTSelectDirectory(const char *title, const char *initialDirectory) {
    YTDirectoryAccess *result = calloc(1, sizeof(YTDirectoryAccess));
    if (result == NULL) return NULL;
    void (^showPanel)(void) = ^{
        @autoreleasepool {
            NSOpenPanel *panel = [NSOpenPanel openPanel];
            panel.title = [NSString stringWithUTF8String:title];
            panel.canChooseFiles = NO;
            panel.canChooseDirectories = YES;
            panel.allowsMultipleSelection = NO;
            panel.canCreateDirectories = NO;
            if (initialDirectory != NULL && initialDirectory[0] != '\0') {
                NSString *initialPath = [NSString stringWithUTF8String:initialDirectory];
                BOOL isDirectory = NO;
                if ([[NSFileManager defaultManager] fileExistsAtPath:initialPath isDirectory:&isDirectory] && isDirectory) {
                    panel.directoryURL = [NSURL fileURLWithPath:initialPath isDirectory:YES];
                }
            }
            if ([panel runModal] != NSModalResponseOK || panel.URL == nil) {
                result->cancelled = 1;
                return;
            }
            NSURL *url = panel.URL;
            if (![url startAccessingSecurityScopedResource]) {
                result->error = strdup("macOS denied access to the selected folder");
                return;
            }
            result->url = (void *)CFRetain((__bridge CFTypeRef)url);
            YTCopyBookmarkAndPath(result, url);
            if (result->error != NULL) {
                [url stopAccessingSecurityScopedResource];
                CFRelease(result->url);
                result->url = NULL;
            }
        }
    };
    if ([NSThread isMainThread]) {
        showPanel();
    } else {
        dispatch_sync(dispatch_get_main_queue(), showPanel);
    }
    return result;
}

static YTDirectoryAccess *YTResolveBookmark(const unsigned char *bytes, long length) {
    YTDirectoryAccess *result = calloc(1, sizeof(YTDirectoryAccess));
    if (result == NULL) return NULL;
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
        BOOL stale = NO;
        NSError *error = nil;
        NSURL *url = [NSURL URLByResolvingBookmarkData:data
            options:NSURLBookmarkResolutionWithSecurityScope
            relativeToURL:nil bookmarkDataIsStale:&stale error:&error];
        if (url == nil) {
            YTSetError(result, error);
            return result;
        }
        if (![url startAccessingSecurityScopedResource]) {
            result->error = strdup("macOS denied access to the bookmarked folder");
            return result;
        }
        result->url = (void *)CFRetain((__bridge CFTypeRef)url);
        result->path = strdup([[url path] fileSystemRepresentation]);
        if (result->path == NULL) {
            result->error = strdup("could not copy bookmarked folder path");
        } else if (stale) {
            YTCopyBookmarkAndPath(result, url);
        }
        if (result->error != NULL) {
            [url stopAccessingSecurityScopedResource];
            CFRelease(result->url);
            result->url = NULL;
        }
    }
    return result;
}

static void YTStopAccessing(void *handle) {
    if (handle == NULL) return;
    NSURL *url = (NSURL *)handle;
    [url stopAccessingSecurityScopedResource];
    CFRelease(handle);
}

static void YTReleaseAccessHandle(void *handle) {
    if (handle != NULL) CFRelease(handle);
}

static void YTFreeDirectoryAccess(YTDirectoryAccess *result) {
    if (result == NULL) return;
    free(result->path);
    free(result->bookmark);
    free(result->error);
    free(result);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

func pickSecurityScopedDirectory(_ context.Context, title, initialDirectory string) (string, []byte, func(), error) {
	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	cinitial := C.CString(initialDirectory)
	defer C.free(unsafe.Pointer(cinitial))
	result := C.YTSelectDirectory(ctitle, cinitial)
	return readDirectoryAccessResult(result)
}

func resolveSecurityScopedBookmark(bookmark []byte) (string, []byte, func(), error) {
	if len(bookmark) == 0 {
		return "", nil, nil, errors.New("security-scoped bookmark is empty")
	}
	result := C.YTResolveBookmark((*C.uchar)(unsafe.Pointer(&bookmark[0])), C.long(len(bookmark)))
	return readDirectoryAccessResult(result)
}

func readDirectoryAccessResult(result *C.YTDirectoryAccess) (string, []byte, func(), error) {
	if result == nil {
		return "", nil, nil, errors.New("could not allocate macOS directory access result")
	}
	defer C.YTFreeDirectoryAccess(result)
	if result.cancelled != 0 {
		return "", nil, nil, nil
	}
	if result.error != nil {
		return "", nil, nil, fmt.Errorf("macOS folder permission: %s", C.GoString(result.error))
	}
	if result.path == nil || result.url == nil {
		return "", nil, nil, errors.New("macOS did not return folder access")
	}
	path := C.GoString(result.path)
	bookmark := C.GoBytes(unsafe.Pointer(result.bookmark), C.int(result.bookmark_len))
	handle := result.url
	stop := func() { C.YTStopAccessing(handle) }
	return path, bookmark, stop, nil
}
