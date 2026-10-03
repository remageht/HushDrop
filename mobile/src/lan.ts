/**
 * HushDrop LAN Validation & URL Construction
 * Enforces RFC 1918 private networking boundaries.
 */

/**
 * Checks whether an IPv4 address belongs to RFC 1918 private networks or loopback.
 * Supported ranges:
 * - 10.0.0.0/8 (10.0.0.0 - 10.255.255.255)
 * - 172.16.0.0/12 (172.16.0.0 - 172.31.255.255)
 * - 192.168.0.0/16 (192.168.0.0 - 192.168.255.255)
 * - 127.0.0.0/8 (Loopback)
 * - "localhost"
 */
export function isPrivateIPv4(ipOrHost: string): boolean {
  const trimmed = ipOrHost.trim().toLowerCase();
  if (trimmed === 'localhost') {
    return true;
  }

  // Strict IPv4 octet check
  const parts = trimmed.split('.');
  if (parts.length !== 4) {
    return false;
  }

  const octets: number[] = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) {
      return false;
    }
    const num = Number.parseInt(part, 10);
    if (num < 0 || num > 255) {
      return false;
    }
    octets.push(num);
  }

  const [o1, o2] = octets;

  // 127.0.0.0/8 (Loopback)
  if (o1 === 127) {
    return true;
  }

  // 10.0.0.0/8
  if (o1 === 10) {
    return true;
  }

  // 172.16.0.0/12 (172.16.0.0 - 172.31.255.255)
  if (o1 === 172 && o2 >= 16 && o2 <= 31) {
    return true;
  }

  // 192.168.0.0/16
  if (o1 === 192 && o2 === 168) {
    return true;
  }

  return false;
}

/**
 * Constructs a secure HTTPS URL for HushDrop server.
 * Rejects any non-LAN / non-private targets.
 */
export function buildServerUrl(host: string, port = 8443): string {
  const cleanHost = host.trim().replace(/^https?:\/\//i, '').split(':')[0];
  if (!isPrivateIPv4(cleanHost)) {
    throw new Error(`Forbidden address '${cleanHost}'. HushDrop operates strictly within private LAN (RFC 1918).`);
  }
  return `https://${cleanHost}:${port}`;
}

/**
 * Constructs a plain HTTP URL for certificate downloading.
 * Used exclusively for retrieving self-signed certificate over HTTP (:8080/cert).
 */
export function buildCertUrl(host: string, port = 8080): string {
  const cleanHost = host.trim().replace(/^https?:\/\//i, '').split(':')[0];
  if (!isPrivateIPv4(cleanHost)) {
    throw new Error(`Forbidden address '${cleanHost}'. HushDrop operates strictly within private LAN (RFC 1918).`);
  }
  return `http://${cleanHost}:${port}/cert`;
}
