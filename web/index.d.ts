export interface CircleLensOptions {
  /** Watch for the circle gesture. `open()` and `toggle()` work either way. Default true. */
  enabled?: boolean;
  /** Circles to draw to open or close it, 1 to 3 in halves. Default 2. */
  loops?: number;
  /** The zoom it opens at, 1.2 to 4. Default 1.8. */
  zoom?: number;
  /** The glass radius in CSS pixels, 100 to 300. Default 150. */
  radius?: number;
  /** Seconds after it opens or closes before a circle counts again, 0.6 to 5. Default 1.5. */
  gap?: number;
  /** Ease the zoom out while the pointer moves or the page scrolls fast. Default true. */
  zoomOutWhenFast?: boolean;
  /** Esc closes it and +/− change the zoom while it is open. Default true. */
  keys?: boolean;
  /** Called when the lens opens. */
  onOpen?: () => void;
  /** Called once the lens has closed. */
  onClose?: () => void;
}

export interface CircleLens {
  /** Open the lens at (x, y) in CSS pixels from the viewport's top-left, or at the pointer. */
  open(x?: number, y?: number): void;
  /** Close the lens; resolves once it has gone. */
  close(): Promise<void>;
  /** Open or close it. */
  toggle(x?: number, y?: number): Promise<void> | void;
  /** Change options; anything left out keeps its value. A new radius applies the next time it opens. */
  set(options: CircleLensOptions): void;
  /** Close it, stop watching for the gesture and remove everything it added to the page. */
  destroy(): void;
  readonly isOpen: boolean;
  readonly destroyed: boolean;
  readonly options: Required<Omit<CircleLensOptions, 'onOpen' | 'onClose'>>;
}

/**
 * Start Circle Lens on this page. There is one lens per page: calling it again updates the running lens
 * and returns it. Without a `window` (server-side rendering) it returns a lens that does nothing.
 */
export function circleLens(options?: CircleLensOptions): CircleLens;

/** Options with anything unknown or out of range replaced by the default. */
export function cleanOptions(options?: CircleLensOptions): Required<Omit<CircleLensOptions, 'onOpen' | 'onClose'>>;

/** Options from a script tag's `dataset` (`data-zoom="2"`, `data-zoom-out-when-fast="false"`, …). */
export function optionsFromAttributes(dataset: Record<string, string | undefined>): CircleLensOptions;
