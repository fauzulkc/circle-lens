/**
 * Settings both tools share, and the one place that cleans them. Anything read back from storage or a
 * settings file is untrusted: `cleanSettings` keeps only known keys with values in range.
 */

export const ZOOM_MIN = 1.2;
export const ZOOM_MAX = 4;
export const RADIUS_MIN = 100;
export const RADIUS_MAX = 300;

export const LENS_DEFAULTS = Object.freeze({
  /** Watch for the circle gesture at all. */
  enabled: true,
  /** Circles to draw (1-3, in halves). */
  loops: 2,
  /** The zoom the lens opens at. */
  zoom: 1.8,
  /** The glass radius in CSS pixels. */
  radius: 150,
  /** Seconds after the lens opens or closes before a circle counts again. */
  gap: 1.5,
});

/** Rounds to a tenth and keeps the zoom between ZOOM_MIN and ZOOM_MAX. */
export const clampZoom = (zoom) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(zoom * 10) / 10));

const numberIn = (value, min, max) => (typeof value === 'number' && Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : undefined);

/** Known keys only, each in range; anything missing or invalid falls back to the default. */
export function cleanSettings(raw) {
  const input = raw && typeof raw === 'object' ? raw : {};
  const loops = numberIn(input.loops, 1, 3);
  const zoom = numberIn(input.zoom, ZOOM_MIN, ZOOM_MAX);
  const radius = numberIn(input.radius, RADIUS_MIN, RADIUS_MAX);
  const gap = numberIn(input.gap, 0.6, 5);
  return {
    enabled: typeof input.enabled === 'boolean' ? input.enabled : LENS_DEFAULTS.enabled,
    loops: loops === undefined ? LENS_DEFAULTS.loops : Math.round(loops * 2) / 2,
    zoom: zoom === undefined ? LENS_DEFAULTS.zoom : clampZoom(zoom),
    radius: radius === undefined ? LENS_DEFAULTS.radius : Math.round(radius),
    gap: gap === undefined ? LENS_DEFAULTS.gap : Math.round(gap * 10) / 10,
  };
}
