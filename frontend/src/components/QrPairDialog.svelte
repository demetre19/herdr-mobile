<script lang="ts">
  import AppDialog from '$components/ui/AppDialog.svelte';
  import Button from '$components/ui/Button.svelte';
  import { relayStore } from '$lib/store';

  let { open = $bindable(false) }: { open?: boolean } = $props();

  let video = $state<HTMLVideoElement>();
  let stream: MediaStream | null = null;
  let scanTimer: ReturnType<typeof setInterval> | null = null;
  let detector: BarcodeDetector | null = null;
  let cameraError = $state('');
  let pasteValue = $state('');
  let pasteError = $state('');
  let scanning = $state(false);

  const scannerSupported = $derived(
    typeof BarcodeDetector === 'function'
      && Boolean(navigator.mediaDevices?.getUserMedia),
  );

  function importLink(raw: string): boolean {
    const text = raw.trim();
    if (!text) return false;
    let url: URL;
    try {
      url = new URL(text);
    } catch {
      return false;
    }
    return relayStore.importSetupLink({
      hash: url.hash,
      protocol: url.protocol,
      host: url.host,
      pathname: url.pathname,
      search: url.search,
    });
  }

  async function scanFrame() {
    if (!detector || !video || video.readyState < 2 || !video.videoWidth) return;
    try {
      const codes = await detector.detect(video);
      const hit = codes.find((code) => importLink(code.rawValue));
      if (hit) {
        open = false;
      }
    } catch {
      // A failed frame is normal while the camera warms up; keep scanning.
    }
  }

  async function startCamera() {
    cameraError = '';
    if (!scannerSupported) return;
    try {
      detector = new BarcodeDetector!({ formats: ['qr_code'] });
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'environment' },
        audio: false,
      });
      if (video) {
        video.srcObject = stream;
        await video.play().catch(() => {});
      }
      scanning = true;
      scanTimer = setInterval(scanFrame, 300);
    } catch (err) {
      cameraError = err instanceof DOMException && err.name === 'NotAllowedError'
        ? 'Camera access was denied. Allow camera permission for this app, or paste the link below.'
        : 'The camera could not be started. Paste the link below instead.';
    }
  }

  function stopCamera() {
    scanning = false;
    if (scanTimer) {
      clearInterval(scanTimer);
      scanTimer = null;
    }
    if (stream) {
      for (const track of stream.getTracks()) track.stop();
      stream = null;
    }
    if (video) video.srcObject = null;
  }

  function submitPaste(event: SubmitEvent) {
    event.preventDefault();
    pasteError = '';
    if (importLink(pasteValue)) {
      pasteValue = '';
      open = false;
      return;
    }
    pasteError = 'That is not a Herdr setup link. Copy the whole link the computer printed.';
  }

  $effect(() => {
    if (open) {
      startCamera();
    } else {
      stopCamera();
      pasteError = '';
    }
  });
</script>

<AppDialog
  id="qr-pair-dialog"
  bind:open
  title="Pair with QR Code"
  description="Point the camera at the QR code the computer shows (herdr-pair), or paste the setup link."
>
  <div class="qr-pair">
    {#if scannerSupported}
      <div class="qr-viewport" class:qr-live={scanning}>
        <video bind:this={video} playsinline muted autoplay></video>
        {#if !scanning && !cameraError}
          <p class="hint qr-overlay">Starting the camera…</p>
        {/if}
      </div>
      {#if cameraError}<p class="warning" role="status">{cameraError}</p>{/if}
    {:else}
      <p class="hint">This browser cannot scan QR codes. Paste the setup link below instead.</p>
    {/if}

    <form class="form-stack" onsubmit={submitPaste}>
      <label for="qr-paste">Setup link</label>
      <input
        id="qr-paste"
        bind:value={pasteValue}
        type="url"
        inputmode="url"
        placeholder="https://herdr-app.seotimemachines.com/#label=…&setup=…"
      />
      {#if pasteError}<p class="warning" role="alert">{pasteError}</p>{/if}
      <div class="form-actions">
        <Button type="submit">Pair from Link</Button>
        <Button variant="ghost" onclick={() => { open = false; }}>Close</Button>
      </div>
    </form>
  </div>
</AppDialog>

<style>
  .qr-pair {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .qr-viewport {
    position: relative;
    width: 100%;
    aspect-ratio: 4 / 3;
    overflow: hidden;
    border-radius: 0.5rem;
    background: #000;
  }
  .qr-viewport video {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .qr-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 0;
  }
</style>
