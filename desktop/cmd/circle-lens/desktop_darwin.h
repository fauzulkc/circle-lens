// The Objective-C side of the macOS backend (desktop_darwin.m), called from desktop_darwin.go.
// Coordinates are global points with the origin at the top left of the main display, as Core Graphics
// counts them; pixels are backing pixels.

#include <stdint.h>

void clStart(void);
void clRun(void);
void clStop(void);
void clPost(uintptr_t id);

void clPointer(double *x, double *y);
double clScaleAt(double x, double y);
void clScreenAt(double x, double y, double *left, double *top, double *width, double *height);
double clRefreshRate(void);
int clGrab(double left, double top, double width, double height, int pixelWidth, int pixelHeight, uint8_t *into);
int clScreenAccess(int ask);

void clShowLens(const uint8_t *pixels, int width, int height, double left, double top, double scale);
void clHideLens(void);
int clText(const char *utf8, double size, uint8_t **out, int *width, int *height);
void clFree(void *pointer);
int clDark(void);
int clReducedMotion(void);

void clWatchPointer(int on);
int clWatchModifiers(int on);
void clGrabKeys(int count, const uint32_t *keycodes, const uint32_t *modifiers);

void clSetMenu(const char *json, const uint8_t *iconRGBA, int iconSide);
void clOpenAddress(const char *address);
