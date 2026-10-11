import { circleLens, cleanOptions, optionsFromAttributes } from './index.js';

/**
 * The script-tag build (dist/circle-lens.min.js). It starts the lens with options from its own data
 * attributes and puts the API on `window.CircleLens`:
 *
 *   <script src="https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js" data-zoom="2" defer></script>
 *
 * Add `data-manual` to load it without starting, then call `CircleLens.circleLens({ … })` yourself.
 */

const script = document.currentScript;
const dataset = script?.dataset ?? {};
const api = { circleLens, cleanOptions, version: __VERSION__, lens: null };
window.CircleLens = api;
if (!('manual' in dataset)) api.lens = circleLens(optionsFromAttributes(dataset));
