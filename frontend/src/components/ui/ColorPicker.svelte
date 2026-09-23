<script lang="ts">
  // Themed color picker: the native <input type="color"> popup ignores the app
  // palette, so this renders the same saturation/value field + hue strip every
  // platform ships, but drawn with our surface/border tokens.
  interface Props {
    value?: string;
    disabled?: boolean;
    ariaLabel?: string;
    onchange?: (hex: string) => void;
  }

  let { value = $bindable('#e07820'), disabled = false, ariaLabel = 'Choose accent color', onchange }: Props = $props();

  interface Hsv { h: number; s: number; v: number }

  function clamp01(n: number): number { return Math.min(1, Math.max(0, n)); }

  function hexToHsv(hex: string): Hsv | null {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return null;
    const n = parseInt(m[1], 16);
    const r = ((n >> 16) & 255) / 255, g = ((n >> 8) & 255) / 255, b = (n & 255) / 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    let h = 0;
    if (d) {
      if (max === r) h = 60 * (((g - b) / d) % 6);
      else if (max === g) h = 60 * ((b - r) / d + 2);
      else h = 60 * ((r - g) / d + 4);
    }
    if (h < 0) h += 360;
    return { h, s: max ? d / max : 0, v: max };
  }

  function hsvToHex({ h, s, v }: Hsv): string {
    const c = v * s, x = c * (1 - Math.abs(((h / 60) % 2) - 1)), m = v - c;
    const [r, g, b] = h < 60 ? [c, x, 0] : h < 120 ? [x, c, 0] : h < 180 ? [0, c, x]
      : h < 240 ? [0, x, c] : h < 300 ? [x, 0, c] : [c, 0, x];
    const to = (n: number) => Math.round((n + m) * 255).toString(16).padStart(2, '0');
    return `#${to(r)}${to(g)}${to(b)}`;
  }

  let open = $state(false);
  let hsv = $state<Hsv>(hexToHsv(value) ?? { h: 24, s: 0.85, v: 0.88 });
  let hexDraft = $state(value);
  let root = $state<HTMLDivElement>(null!);
  let svEl = $state<HTMLDivElement>(null!);
  let hueEl = $state<HTMLDivElement>(null!);

  // External value changes (reset, another picker) re-seed the field; edits we
  // emitted ourselves compare equal and skip.
  $effect(() => {
    const parsed = hexToHsv(value);
    if (parsed && hsvToHex(hsv).toLowerCase() !== value.toLowerCase()) hsv = parsed;
    if (hexDraft.toLowerCase() !== value.toLowerCase()) hexDraft = value;
  });

  function commit() {
    const hex = hsvToHex(hsv);
    value = hex;
    hexDraft = hex;
    onchange?.(hex);
  }

  function svFromEvent(event: PointerEvent) {
    const rect = svEl.getBoundingClientRect();
    hsv = { ...hsv, s: clamp01((event.clientX - rect.left) / rect.width), v: 1 - clamp01((event.clientY - rect.top) / rect.height) };
    commit();
  }

  function hueFromEvent(event: PointerEvent) {
    const rect = hueEl.getBoundingClientRect();
    hsv = { ...hsv, h: clamp01((event.clientX - rect.left) / rect.width) * 360 };
    commit();
  }

  function drag(event: PointerEvent, apply: (e: PointerEvent) => void) {
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    apply(event);
  }

  function hexInput(event: Event) {
    const raw = (event.currentTarget as HTMLInputElement).value;
    hexDraft = raw;
    const parsed = hexToHsv(raw);
    if (parsed) { hsv = parsed; commit(); }
  }

  function svKey(event: KeyboardEvent) {
    const step = event.shiftKey ? 0.1 : 0.02;
    if (event.key === 'ArrowLeft') hsv = { ...hsv, s: clamp01(hsv.s - step) };
    else if (event.key === 'ArrowRight') hsv = { ...hsv, s: clamp01(hsv.s + step) };
    else if (event.key === 'ArrowUp') hsv = { ...hsv, v: clamp01(hsv.v + step) };
    else if (event.key === 'ArrowDown') hsv = { ...hsv, v: clamp01(hsv.v - step) };
    else return;
    event.preventDefault();
    commit();
  }

  function hueKey(event: KeyboardEvent) {
    const step = event.shiftKey ? 30 : 5;
    if (event.key === 'ArrowLeft') hsv = { ...hsv, h: (hsv.h - step + 360) % 360 };
    else if (event.key === 'ArrowRight') hsv = { ...hsv, h: (hsv.h + step) % 360 };
    else return;
    event.preventDefault();
    commit();
  }

  function windowPointer(event: PointerEvent) {
    if (open && root && !root.contains(event.target as Node)) open = false;
  }

  function windowKey(event: KeyboardEvent) {
    if (open && event.key === 'Escape') { open = false; event.stopPropagation(); }
  }
</script>

<svelte:window onpointerdown={windowPointer} onkeydown={windowKey} />

<div class="color-picker" bind:this={root}>
  <button
    type="button"
    class="color-swatch-trigger"
    {disabled}
    aria-label={ariaLabel}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={() => (open = !open)}
  >
    <span class="color-swatch-chip" style:background={value}></span>
    <span class="color-swatch-hex">{value}</span>
  </button>
  {#if open}
    <div class="color-popover" role="dialog" aria-label="Accent color picker">
      <div
        bind:this={svEl}
        class="cp-sv"
        role="slider"
        tabindex="0"
        aria-label="Saturation and brightness"
        aria-valuetext={`saturation ${Math.round(hsv.s * 100)}%, brightness ${Math.round(hsv.v * 100)}%`}
        style:background={`linear-gradient(to top, #000, transparent), linear-gradient(to right, #fff, hsl(${hsv.h} 100% 50%))`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(hsv.v * 100)}
        onpointerdown={(e) => drag(e, svFromEvent)}
        onpointermove={(e) => { if (e.buttons) svFromEvent(e); }}
        onkeydown={svKey}
      >
        <span class="cp-thumb" style:left={`${hsv.s * 100}%`} style:top={`${(1 - hsv.v) * 100}%`}></span>
      </div>
      <div
        bind:this={hueEl}
        class="cp-hue"
        role="slider"
        tabindex="0"
        aria-label="Hue"
        aria-valuemin={0}
        aria-valuemax={360}
        aria-valuenow={Math.round(hsv.h)}
        onpointerdown={(e) => drag(e, hueFromEvent)}
        onpointermove={(e) => { if (e.buttons) hueFromEvent(e); }}
        onkeydown={hueKey}
      >
        <span class="cp-thumb" style:left={`${(hsv.h / 360) * 100}%`} style:top="50%"></span>
      </div>
      <label class="cp-hex-row">
        <span>Hex</span>
        <input class="cp-hex" value={hexDraft} oninput={hexInput} spellcheck="false" autocomplete="off" maxlength="7" />
      </label>
    </div>
  {/if}
</div>
