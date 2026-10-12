// The macOS backend's Objective-C side. Everything that touches AppKit runs on the main thread, which is
// also the Go app's UI thread; clGrab runs on the capture thread and only uses ScreenCaptureKit.

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>
#import <QuartzCore/QuartzCore.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreText/CoreText.h>
#include "desktop_darwin.h"
#include "_cgo_export.h"

static NSPanel *lensPanel;
static NSStatusItem *statusItem;
static id pointerMonitor;
static CFMachPortRef keyTap;
static CFRunLoopSourceRef keyTapSource;
static NSMutableArray *hotKeys; // EventHotKeyRef values, boxed
static EventHandlerRef hotKeyHandler;

/* ---------- the app ---------- */

@interface CLMenuTarget : NSObject
- (void)clicked:(NSMenuItem *)item;
@end

@implementation CLMenuTarget
- (void)clicked:(NSMenuItem *)item {
	goMenuClicked((int)item.tag);
}
@end

static CLMenuTarget *menuTarget;

void clStart(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; // no Dock icon
	lensPanel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 10, 10)
	                                       styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
	                                         backing:NSBackingStoreBuffered
	                                           defer:NO];
	lensPanel.opaque = NO;
	lensPanel.backgroundColor = NSColor.clearColor;
	lensPanel.hasShadow = NO;
	lensPanel.ignoresMouseEvents = YES;
	lensPanel.level = NSScreenSaverWindowLevel;
	lensPanel.hidesOnDeactivate = NO;
	lensPanel.releasedWhenClosed = NO;
	lensPanel.sharingType = NSWindowSharingNone;
	lensPanel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary |
	                               NSWindowCollectionBehaviorFullScreenAuxiliary | NSWindowCollectionBehaviorIgnoresCycle;
	NSView *view = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 10, 10)];
	view.wantsLayer = YES;
	view.layer.contentsGravity = kCAGravityResize;
	lensPanel.contentView = view;
	menuTarget = [CLMenuTarget new];
	statusItem = [NSStatusBar.systemStatusBar statusItemWithLength:NSSquareStatusItemLength];
	hotKeys = [NSMutableArray new];
}

void clRun(void) { [NSApp run]; }

void clStop(void) {
	[NSApp stop:nil];
	// stop takes effect after the next event, so send one.
	NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0
	                                  timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
	[NSApp postEvent:wake atStart:YES];
}

void clPost(uintptr_t id) {
	dispatch_async(dispatch_get_main_queue(), ^{
		goRunPosted(id);
	});
}

/* ---------- the screen ---------- */

void clPointer(double *x, double *y) {
	CGEventRef event = CGEventCreate(NULL);
	CGPoint at = CGEventGetLocation(event);
	CFRelease(event);
	*x = at.x;
	*y = at.y;
}

// displayAt is the display under a point (top-left global points), or the main one.
static CGDirectDisplayID displayAt(double x, double y) {
	CGDirectDisplayID display = CGMainDisplayID();
	uint32_t count = 0;
	CGGetDisplaysWithPoint(CGPointMake(x, y), 1, &display, &count);
	return count > 0 ? display : CGMainDisplayID();
}

double clScaleAt(double x, double y) {
	CGDirectDisplayID display = displayAt(x, y);
	CGDisplayModeRef mode = CGDisplayCopyDisplayMode(display);
	if (mode == NULL) return 1;
	double scale = (double)CGDisplayModeGetPixelWidth(mode) / (double)CGDisplayModeGetWidth(mode);
	CGDisplayModeRelease(mode);
	return scale > 0 ? scale : 1;
}

void clScreenAt(double x, double y, double *left, double *top, double *width, double *height) {
	CGRect bounds = CGDisplayBounds(displayAt(x, y));
	*left = bounds.origin.x;
	*top = bounds.origin.y;
	*width = bounds.size.width;
	*height = bounds.size.height;
}

double clRefreshRate(void) {
	CGDisplayModeRef mode = CGDisplayCopyDisplayMode(CGMainDisplayID());
	double rate = mode ? CGDisplayModeGetRefreshRate(mode) : 0;
	if (mode) CGDisplayModeRelease(mode);
	if (rate < 20 || rate > 500) rate = 60; // built-in panels report 0; ProMotion is adaptive
	return rate;
}

// clScreenAccess says whether the app may record the screen; with ask, macOS asks (once).
int clScreenAccess(int ask) {
	if (CGPreflightScreenCaptureAccess()) return 1;
	if (ask) CGRequestScreenCaptureAccess();
	return 0;
}

static SCShareableContent *shareable;
static CFAbsoluteTime shareableAt;
static NSLock *shareableLock;

// currentContent is the list of displays and apps, refreshed every few seconds at most.
static SCShareableContent *currentContent(void) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{ shareableLock = [NSLock new]; });
	[shareableLock lock];
	SCShareableContent *content = shareable;
	BOOL stale = content == nil || CFAbsoluteTimeGetCurrent() - shareableAt > 5;
	[shareableLock unlock];
	if (!stale) return content;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	__block SCShareableContent *fresh = nil;
	[SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES
	                                             completionHandler:^(SCShareableContent *found, NSError *error) {
		fresh = found;
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC));
	if (fresh == nil) return content;
	[shareableLock lock];
	shareable = fresh;
	shareableAt = CFAbsoluteTimeGetCurrent();
	[shareableLock unlock];
	return fresh;
}

// clGrab copies part of the screen (global points) at pixelWidth × pixelHeight into opaque BGRA,
// leaving this app's windows (the lens) out.
int clGrab(double left, double top, double width, double height, int pixelWidth, int pixelHeight, uint8_t *into) {
	@autoreleasepool {
		SCShareableContent *content = currentContent();
		if (content == nil) return 0;
		CGDirectDisplayID displayID = displayAt(left + width / 2, top + height / 2);
		SCDisplay *display = nil;
		for (SCDisplay *candidate in content.displays) {
			if (candidate.displayID == displayID) display = candidate;
		}
		if (display == nil) return 0;
		NSMutableArray *ours = [NSMutableArray new];
		for (SCRunningApplication *running in content.applications) {
			if (running.processID == getpid()) [ours addObject:running];
		}
		SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingApplications:ours exceptingWindows:@[]];
		CGRect bounds = CGDisplayBounds(displayID);
		SCStreamConfiguration *configuration = [SCStreamConfiguration new];
		configuration.sourceRect = CGRectMake(left - bounds.origin.x, top - bounds.origin.y, width, height);
		configuration.width = pixelWidth;
		configuration.height = pixelHeight;
		configuration.showsCursor = NO;
		configuration.pixelFormat = kCVPixelFormatType_32BGRA;
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		__block CGImageRef image = NULL;
		[SCScreenshotManager captureImageWithFilter:filter configuration:configuration
		                          completionHandler:^(CGImageRef captured, NSError *error) {
			if (captured) image = CGImageRetain(captured);
			dispatch_semaphore_signal(done);
		}];
		dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC));
		if (image == NULL) return 0;
		CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
		CGContextRef context = CGBitmapContextCreate(into, pixelWidth, pixelHeight, 8, pixelWidth * 4, space,
		                                             kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
		CGColorSpaceRelease(space);
		if (context == NULL) {
			CGImageRelease(image);
			return 0;
		}
		CGContextSetRGBFillColor(context, 0, 0, 0, 1);
		CGContextFillRect(context, CGRectMake(0, 0, pixelWidth, pixelHeight));
		CGContextDrawImage(context, CGRectMake(0, 0, pixelWidth, pixelHeight), image);
		CGContextRelease(context);
		CGImageRelease(image);
		return 1;
	}
}

/* ---------- the lens window ---------- */

void clShowLens(const uint8_t *pixels, int width, int height, double left, double top, double scale) {
	@autoreleasepool {
		CFDataRef data = CFDataCreate(NULL, pixels, (CFIndex)width * height * 4);
		CGDataProviderRef provider = CGDataProviderCreateWithCFData(data);
		CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
		CGImageRef image = CGImageCreate(width, height, 8, 32, width * 4, space,
		                                 kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little, provider, NULL, false,
		                                 kCGRenderingIntentDefault);
		CGColorSpaceRelease(space);
		CGDataProviderRelease(provider);
		CFRelease(data);
		double primaryHeight = NSScreen.screens.firstObject.frame.size.height;
		NSRect frame = NSMakeRect(left, primaryHeight - top - height / scale, width / scale, height / scale);
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		if (!NSEqualRects(lensPanel.frame, frame)) [lensPanel setFrame:frame display:NO];
		lensPanel.contentView.layer.contentsScale = scale;
		lensPanel.contentView.layer.contents = (__bridge id)image;
		[CATransaction commit];
		CGImageRelease(image);
		if (!lensPanel.visible) [lensPanel orderFrontRegardless];
	}
}

void clHideLens(void) {
	[lensPanel orderOut:nil];
	lensPanel.contentView.layer.contents = nil;
}

int clText(const char *utf8, double size, uint8_t **out, int *width, int *height) {
	@autoreleasepool {
		NSFont *font = [NSFont systemFontOfSize:size weight:NSFontWeightMedium];
		NSAttributedString *text = [[NSAttributedString alloc] initWithString:[NSString stringWithUTF8String:utf8]
		                                                           attributes:@{NSFontAttributeName : font}];
		CTLineRef line = CTLineCreateWithAttributedString((__bridge CFAttributedStringRef)text);
		CGFloat ascent = 0, descent = 0, leading = 0;
		double lineWidth = CTLineGetTypographicBounds(line, &ascent, &descent, &leading);
		int w = (int)ceil(lineWidth), h = (int)ceil(ascent + descent);
		if (w <= 0 || h <= 0) {
			CFRelease(line);
			return 0;
		}
		uint8_t *coverage = calloc((size_t)w * h, 1);
		CGContextRef context = CGBitmapContextCreate(coverage, w, h, 8, w, NULL, (CGBitmapInfo)kCGImageAlphaOnly);
		CGContextSetTextPosition(context, 0, ceil(descent));
		CTLineDraw(line, context);
		CGContextRelease(context);
		CFRelease(line);
		*out = coverage;
		*width = w;
		*height = h;
		return 1;
	}
}

void clFree(void *pointer) { free(pointer); }

int clDark(void) {
	NSAppearanceName name = [NSApp.effectiveAppearance bestMatchFromAppearancesWithNames:@[ NSAppearanceNameAqua, NSAppearanceNameDarkAqua ]];
	return [name isEqualToString:NSAppearanceNameDarkAqua];
}

int clReducedMotion(void) { return NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion; }

/* ---------- input ---------- */

void clWatchPointer(int on) {
	if (on && pointerMonitor == nil) {
		NSEventMask mask = NSEventMaskMouseMoved | NSEventMaskLeftMouseDragged | NSEventMaskRightMouseDragged |
		                   NSEventMaskOtherMouseDragged | NSEventMaskLeftMouseDown | NSEventMaskLeftMouseUp |
		                   NSEventMaskRightMouseDown | NSEventMaskRightMouseUp | NSEventMaskOtherMouseDown |
		                   NSEventMaskOtherMouseUp | NSEventMaskScrollWheel;
		pointerMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:mask handler:^(NSEvent *event) {
			switch (event.type) {
			case NSEventTypeLeftMouseDown:
			case NSEventTypeRightMouseDown:
			case NSEventTypeOtherMouseDown:
				goButton(1);
				break;
			case NSEventTypeLeftMouseUp:
			case NSEventTypeRightMouseUp:
			case NSEventTypeOtherMouseUp:
				goButton(0);
				break;
			case NSEventTypeScrollWheel: {
				double delta = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY / 40.0 : event.scrollingDeltaY;
				goScrolled(delta, (event.modifierFlags & NSEventModifierFlagControl) != 0);
				break;
			}
			default: {
				CGPoint at = CGEventGetLocation(event.CGEvent);
				goPointerMoved(at.x, at.y);
			}
			}
		}];
	} else if (!on && pointerMonitor != nil) {
		[NSEvent removeMonitor:pointerMonitor];
		pointerMonitor = nil;
	}
}

// keyTapped reports modifier keys going down and up, and that some other key went down or up (never
// which one). The tap only listens: it can't change or stop a key.
static CGEventRef keyTapped(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *context) {
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (keyTap) CGEventTapEnable(keyTap, true);
		return event;
	}
	if (type == kCGEventKeyDown || type == kCGEventKeyUp) {
		goModifier(0, type == kCGEventKeyDown);
		return event;
	}
	if (type == kCGEventFlagsChanged) {
		int64_t code = CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
		CGEventFlags flags = CGEventGetFlags(event);
		switch (code) {
		case kVK_Shift:
		case kVK_RightShift:
			goModifier(1, (flags & kCGEventFlagMaskShift) != 0);
			break;
		case kVK_Control:
		case kVK_RightControl:
			goModifier(2, (flags & kCGEventFlagMaskControl) != 0);
			break;
		case kVK_Option:
		case kVK_RightOption:
			goModifier(3, (flags & kCGEventFlagMaskAlternate) != 0);
			break;
		case kVK_Command:
		case kVK_RightCommand:
			goModifier(0, (flags & kCGEventFlagMaskCommand) != 0);
			break;
		}
	}
	return event;
}

// clWatchModifiers starts or stops the key tap. It needs Input Monitoring; without it, macOS is asked
// (once) and this returns 0.
int clWatchModifiers(int on) {
	if (!on) {
		if (keyTap) {
			CGEventTapEnable(keyTap, false);
			CFRunLoopRemoveSource(CFRunLoopGetMain(), keyTapSource, kCFRunLoopCommonModes);
			CFRelease(keyTapSource);
			CFRelease(keyTap);
			keyTap = NULL;
			keyTapSource = NULL;
		}
		return 1;
	}
	if (keyTap) return 1;
	if (!CGPreflightListenEventAccess()) {
		CGRequestListenEventAccess();
		return 0;
	}
	CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged) | CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventKeyUp);
	keyTap = CGEventTapCreate(kCGSessionEventTap, kCGTailAppendEventTap, kCGEventTapOptionListenOnly, mask, keyTapped, NULL);
	if (keyTap == NULL) return 0;
	keyTapSource = CFMachPortCreateRunLoopSource(NULL, keyTap, 0);
	CFRunLoopAddSource(CFRunLoopGetMain(), keyTapSource, kCFRunLoopCommonModes);
	CGEventTapEnable(keyTap, true);
	return 1;
}

static OSStatus hotKeyPressed(EventHandlerCallRef next, EventRef event, void *context) {
	EventHotKeyID id;
	if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(id), NULL, &id) == noErr) {
		goHotKey((int)id.id);
	}
	return noErr;
}

// clGrabKeys registers key combinations (Carbon hot keys, which need no permission), replacing the
// previous set. Each reports its index.
void clGrabKeys(int count, const uint32_t *keycodes, const uint32_t *modifiers) {
	if (hotKeyHandler == NULL) {
		EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
		InstallApplicationEventHandler(&hotKeyPressed, 1, &spec, NULL, &hotKeyHandler);
	}
	for (NSValue *boxed in hotKeys) {
		UnregisterEventHotKey((EventHotKeyRef)boxed.pointerValue);
	}
	[hotKeys removeAllObjects];
	for (int i = 0; i < count; i++) {
		EventHotKeyRef ref = NULL;
		EventHotKeyID id = {'CLns', (UInt32)i};
		if (RegisterEventHotKey(keycodes[i], modifiers[i], id, GetApplicationEventTarget(), 0, &ref) == noErr) {
			[hotKeys addObject:[NSValue valueWithPointer:ref]];
		}
	}
}

/* ---------- the menu bar item ---------- */

static NSMenu *menuFrom(NSArray *items) {
	NSMenu *menu = [NSMenu new];
	menu.autoenablesItems = NO;
	for (NSDictionary *entry in items) {
		if ([entry[@"separator"] boolValue]) {
			[menu addItem:NSMenuItem.separatorItem];
			continue;
		}
		NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:entry[@"label"] action:@selector(clicked:) keyEquivalent:@""];
		item.target = menuTarget;
		item.tag = [entry[@"id"] integerValue];
		item.enabled = [entry[@"enabled"] boolValue];
		item.state = [entry[@"checked"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
		NSArray *children = entry[@"children"];
		if (children.count > 0) item.submenu = menuFrom(children);
		[menu addItem:item];
	}
	return menu;
}

void clSetMenu(const char *json, const uint8_t *iconRGBA, int iconSide) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:json length:strlen(json)];
		NSArray *items = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
		if (items) statusItem.menu = menuFrom(items);
		NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:iconSide pixelsHigh:iconSide
		                                                              bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO
		                                                             colorSpaceName:NSDeviceRGBColorSpace bitmapFormat:NSBitmapFormatAlphaNonpremultiplied
		                                                                bytesPerRow:iconSide * 4 bitsPerPixel:32];
		memcpy(bitmap.bitmapData, iconRGBA, (size_t)iconSide * iconSide * 4);
		NSImage *image = [[NSImage alloc] initWithSize:NSMakeSize(18, 18)];
		[image addRepresentation:bitmap];
		image.template = YES; // drawn in the menu bar's own colour, light or dark
		statusItem.button.image = image;
		statusItem.button.toolTip = @"Circle Lens";
	}
}

void clOpenAddress(const char *address) {
	NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
	if (url) [NSWorkspace.sharedWorkspace openURL:url];
}
