/// <reference types="vite/client" />

import 'svelte/elements';

declare module 'svelte/elements' {
  interface HTMLTextareaAttributes {
    autocorrect?: 'on' | 'off';
  }
}

declare global {

  interface NotificationAction {
    action: string;
    title: string;
    icon?: string;
  }

  interface NotificationOptions {
    actions?: NotificationAction[];
    renotify?: boolean;
  }

  interface Navigator {
    clearAppBadge?: () => Promise<void>;
    setAppBadge?: (contents?: number) => Promise<void>;
    standalone?: boolean;
  }
  interface BarcodeDetectorOptions {
    formats?: string[];
  }

  interface DetectedBarcode {
    rawValue: string;
    format: string;
  }

  interface BarcodeDetector {
    detect(source: CanvasImageSource): Promise<DetectedBarcode[]>;
  }

  interface BarcodeDetectorConstructor {
    new (options?: BarcodeDetectorOptions): BarcodeDetector;
    getSupportedFormats(): Promise<string[]>;
  }

  var BarcodeDetector: BarcodeDetectorConstructor | undefined;
}

export {};
