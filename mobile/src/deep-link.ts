/**
 * HushDrop Deep Link Parser & Validator
 * Validates hushdrop://pair and https:// LAN connection URLs.
 */

import { isPrivateIPv4, buildServerUrl, buildCertUrl } from './lan';

export interface PairDeepLink {
  host: string;
  port: number;
  token: string;
  fingerprint: string;
  serverUrl: string;
  certUrl: string;
}

// Regex matching 16 to 64 hex characters (one-time pairing tokens)
const TOKEN_REGEX = /^[0-9a-fA-F]{16,64}$/;

// Regex matching 32-byte SHA-256 fingerprint (e.g. AA:BB:CC:...:ZZ)
const FINGERPRINT_REGEX = /^([0-9a-fA-F]{2}:){31}[0-9a-fA-F]{2}$/;

/**
 * Parses and strictly validates a pairing URL or deep link.
 *
 * Supported formats:
 * - `hushdrop://pair?host=192.168.1.100&port=8443&token=...&fp=...`
 * - `https://192.168.1.100:8443/?token=...&fp=...`
 * - `https://192.168.1.100:8443/?token=...` (without fp query)
 */
export function parsePairDeepLink(rawUrl: string): PairDeepLink {
  if (!rawUrl || typeof rawUrl !== 'string') {
    throw new Error('Empty or invalid deep link URL.');
  }

  const trimmed = rawUrl.trim();
  let url: URL;
  try {
    // If scheme is custom hushdrop://, convert to http for URL parser compatibility
    if (trimmed.startsWith('hushdrop://')) {
      const rest = trimmed.slice('hushdrop://'.length);
      url = new URL(`http://dummy/${rest}`);
    } else {
      url = new URL(trimmed);
    }
  } catch {
    throw new Error('Malformed URL structure.');
  }

  let host = '';
  let port = 8443;
  let token = '';
  let fingerprint = '';

  if (trimmed.startsWith('hushdrop://')) {
    // hushdrop://pair?host=192.168.1.5&port=8443&token=...&fp=...
    host = url.searchParams.get('host') || '';
    const portParam = url.searchParams.get('port');
    if (portParam) {
      const parsedPort = Number.parseInt(portParam, 10);
      if (parsedPort > 0 && parsedPort <= 65535) {
        port = parsedPort;
      }
    }
    token = url.searchParams.get('token') || '';
    fingerprint = url.searchParams.get('fp') || '';
  } else if (url.protocol === 'https:' || url.protocol === 'http:') {
    host = url.hostname;
    if (url.port) {
      const parsedPort = Number.parseInt(url.port, 10);
      if (parsedPort > 0 && parsedPort <= 65535) {
        port = parsedPort;
      }
    }
    token = url.searchParams.get('token') || '';
    fingerprint = url.searchParams.get('fp') || '';
  } else {
    throw new Error(`Unsupported protocol '${url.protocol}'. Expected 'hushdrop://' or 'https://'.`);
  }

  // 1. Host Validation (Must be private LAN RFC 1918)
  if (!host || !isPrivateIPv4(host)) {
    throw new Error(`Invalid host '${host}'. Connection is restricted to private LAN (RFC 1918).`);
  }

  // 2. Token Validation (16 to 64 hex characters)
  if (!token || !TOKEN_REGEX.test(token)) {
    throw new Error('Invalid token. Must be 16-64 hex characters.');
  }

  // 3. Fingerprint Validation (Optional in URL, but if present must be exact 32-byte SHA-256)
  if (fingerprint) {
    // Normalize colons and casing
    const formattedFp = fingerprint.trim().toUpperCase();
    if (!FINGERPRINT_REGEX.test(formattedFp)) {
      throw new Error('Invalid TLS fingerprint format. Expected 32 colon-separated hex bytes.');
    }
    fingerprint = formattedFp;
  }

  return {
    host,
    port,
    token,
    fingerprint,
    serverUrl: buildServerUrl(host, port),
    certUrl: buildCertUrl(host, 8080)
  };
}
