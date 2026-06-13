import FingerprintJS from '@fingerprintjs/fingerprintjs';

const DEVICE_ID_KEY = 'seymours-gallery-device-id';

let cachedDeviceId: string | null = null;
let fpPromise: Promise<string> | null = null;

/**
 * Returns an anonymous device identifier using FingerprintJS (browser fingerprinting).
 * The ID is cached in memory and localStorage for performance.
 *
 * FingerprintJS generates a hash based on browser characteristics
 * (fonts, screen, canvas, plugins, etc.) — no cookies, no PII.
 * Falls back to a random UUID stored in localStorage if fingerprinting fails.
 */
export async function getDeviceId(): Promise<string> {
  if (cachedDeviceId) return cachedDeviceId;

  // Check localStorage first for instant availability
  const stored = localStorage.getItem(DEVICE_ID_KEY);
  if (stored) {
    cachedDeviceId = stored;
    return stored;
  }

  // Kick off fingerprinting if not already in flight
  if (!fpPromise) {
    fpPromise = computeFingerprint();
  }

  const deviceId = await fpPromise;
  cachedDeviceId = deviceId;
  localStorage.setItem(DEVICE_ID_KEY, deviceId);
  return deviceId;
}

/**
 * Synchronous getter when the device ID is already available.
 * Returns null if not yet resolved. Useful for rendering.
 */
export function getDeviceIdSync(): string | null {
  if (cachedDeviceId) return cachedDeviceId;
  const stored = localStorage.getItem(DEVICE_ID_KEY);
  if (stored) cachedDeviceId = stored;
  return cachedDeviceId;
}

async function computeFingerprint(): Promise<string> {
  try {
    const fp = await FingerprintJS.load();
    const result = await fp.get();
    // visitorId is a stable hash, e.g. "a1b2c3d4e5f6..."
    return `fp_${result.visitorId}`;
  } catch {
    // Fallback: random UUID stored in localStorage
    return fallbackUUID();
  }
}

function fallbackUUID(): string {
  // crypto.randomUUID is available in all modern browsers
  const id = crypto.randomUUID();
  localStorage.setItem(DEVICE_ID_KEY, id);
  return id;
}
