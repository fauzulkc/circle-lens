import { cleanSettings } from '../../shared/lens-settings.js';

/**
 * Options for the website lens: the settings both tools share (cleaned the same way, so anything out of
 * range falls back to the default) plus `keys`, whether Esc and +/− work while the lens is open.
 *
 * @param {Record<string, unknown>} [raw]
 */
export function cleanOptions(raw) {
  const input = raw && typeof raw === 'object' ? raw : {};
  return { ...cleanSettings(input), keys: typeof input.keys === 'boolean' ? input.keys : true };
}

const NUMBERS = { zoom: 'zoom', radius: 'radius', loops: 'loops', gap: 'gap' };
const SWITCHES = { enabled: 'enabled', keys: 'keys', zoomOutWhenFast: 'zoomOutWhenFast' };

/**
 * Options from a script tag's data attributes, e.g. `data-zoom="2" data-zoom-out-when-fast="false"`
 * (`dataset` turns those into `zoom` and `zoomOutWhenFast`). Anything unreadable is left to the default.
 *
 * @param {Record<string, string | undefined>} dataset
 */
export function optionsFromAttributes(dataset) {
  const options = {};
  for (const [attribute, key] of Object.entries(NUMBERS)) {
    const value = dataset[attribute];
    if (value !== undefined && value.trim() !== '' && Number.isFinite(Number(value))) options[key] = Number(value);
  }
  for (const [attribute, key] of Object.entries(SWITCHES)) {
    const value = dataset[attribute]?.trim().toLowerCase();
    if (value === '' || value === 'true') options[key] = true;
    else if (value === 'false') options[key] = false;
  }
  return options;
}
