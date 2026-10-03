/**
 * HushDrop Mobile Connection Store
 * Stores last known connection metadata (host and expected TLS fingerprint) via @capacitor/preferences.
 *
 * CRITICAL SECURITY PRINCIPLE:
 * Tokens, PINs, and session cryptographic keys MUST NEVER be stored here.
 * Session keys remain strictly ephemeral and volatile in RAM (frontend/src/api/client.ts).
 */

import { Preferences } from '@capacitor/preferences';

const KEY_LAST_HOST = 'hushdrop_last_host';
const KEY_LAST_FP = 'hushdrop_last_fingerprint';

export interface SavedConnection {
  host: string;
  fingerprint: string;
}

/**
 * Saves the host address and verified TLS certificate fingerprint.
 * NO TOKENS OR SECRETS ARE STORED.
 */
export async function saveLastConnection(host: string, fingerprint: string): Promise<void> {
  await Preferences.set({ key: KEY_LAST_HOST, value: host.trim() });
  if (fingerprint) {
    await Preferences.set({ key: KEY_LAST_FP, value: fingerprint.trim().toUpperCase() });
  } else {
    await Preferences.remove({ key: KEY_LAST_FP });
  }
}

/**
 * Retrieves the last known host and TLS fingerprint.
 */
export async function getLastConnection(): Promise<SavedConnection | null> {
  const hostResult = await Preferences.get({ key: KEY_LAST_HOST });
  if (!hostResult.value) {
    return null;
  }
  const fpResult = await Preferences.get({ key: KEY_LAST_FP });
  return {
    host: hostResult.value,
    fingerprint: fpResult.value || ''
  };
}

/**
 * Completely clears connection history (e.g., on "Forget Everything").
 */
export async function clearLastConnection(): Promise<void> {
  await Preferences.remove({ key: KEY_LAST_HOST });
  await Preferences.remove({ key: KEY_LAST_FP });
}
