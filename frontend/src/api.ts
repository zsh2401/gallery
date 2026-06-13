import type { ApiEnvelope } from './types';
import { getAlbumToken } from './password';

export const API_BASE =
  import.meta.env.VITE_API_BASE?.replace(/\/$/, '') ?? 'http://127.0.0.1:8080';

let deviceId: string | null = null;

export function setApiDeviceId(id: string): void {
  deviceId = id;
}

export function mediaURL(path: string | undefined): string {
  if (!path) return '';
  if (path.startsWith('http://') || path.startsWith('https://')) return path;
  return `${API_BASE}${path}`;
}

export async function postAPI<T>(
  path: string,
  payload: unknown = {},
  albumId?: string
): Promise<T> {
  const body: Record<string, unknown> =
    payload && typeof payload === 'object' ? { ...(payload as Record<string, unknown>) } : {};
  if (deviceId) {
    body.deviceId = deviceId;
  }
  if (albumId) {
    const token = getAlbumToken(albumId);
    if (token) {
      body.token = token;
    }
  }
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(body)
  });
  const apiBody = (await res.json()) as ApiEnvelope<T>;
  if (!apiBody.ok) {
    throw new Error(apiBody.error || `Request failed: ${res.status}`);
  }
  return apiBody.data;
}
