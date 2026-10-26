// Lightweight zero-dependency ZIP archive creator (PKZIP 2.0 / STORE method)
// Generates standard .zip files with folder hierarchy for instant streaming without CPU overhead.

// Precomputed CRC32 table
const CRC32_TABLE = new Uint32Array(256);
for (let i = 0; i < 256; i++) {
  let c = i;
  for (let k = 0; k < 8; k++) {
    c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  }
  CRC32_TABLE[i] = c >>> 0;
}

export function computeCRC32(bytes: Uint8Array): number {
  let crc = 0xffffffff;
  for (let i = 0; i < bytes.length; i++) {
    crc = (crc >>> 8) ^ CRC32_TABLE[(crc ^ bytes[i]) & 0xff];
  }
  return (crc ^ 0xffffffff) >>> 0;
}

interface ZipEntry {
  path: string;
  bytes: Uint8Array;
  crc: number;
  offset: number;
}

export async function createZipFromFiles(
  files: { file: File; relativePath?: string }[],
  archiveName: string
): Promise<File> {
  const entries: ZipEntry[] = [];
  const parts: BlobPart[] = [];
  let currentOffset = 0;

  const encoder = new TextEncoder();

  // 1. Write Local File Headers + File Data
  for (const item of files) {
    const rawPath = item.relativePath || item.file.webkitRelativePath || item.file.name;
    // Normalize path separators to forward slash
    const normalizedPath = rawPath.replace(/\\/g, '/').replace(/^\/+/, '');
    const pathBytes = encoder.encode(normalizedPath);

    const fileBuffer = await item.file.arrayBuffer();
    const fileBytes = new Uint8Array(fileBuffer);
    const crc = computeCRC32(fileBytes);
    const size = fileBytes.length;

    // Local file header (30 bytes + path length)
    const header = new Uint8Array(30 + pathBytes.length);
    const view = new DataView(header.buffer);

    // Signature: 0x04034b50 ("PK\x03\x04")
    view.setUint32(0, 0x04034b50, true);
    // Version needed: 2.0 (20)
    view.setUint16(4, 20, true);
    // General purpose bit flag: 0x0800 (UTF-8 filename)
    view.setUint16(6, 0x0800, true);
    // Compression method: 0 (STORE / uncompressed)
    view.setUint16(8, 0, true);
    // File modification time & date (Dos time: 0)
    view.setUint16(10, 0x546b, true); // ~10:35:22
    view.setUint16(12, 0x5d59, true); // ~2026-10-25
    // CRC-32
    view.setUint32(14, crc, true);
    // Compressed size
    view.setUint32(18, size, true);
    // Uncompressed size
    view.setUint32(22, size, true);
    // Filename length
    view.setUint16(26, pathBytes.length, true);
    // Extra field length
    view.setUint16(28, 0, true);

    header.set(pathBytes, 30);

    entries.push({
      path: normalizedPath,
      bytes: fileBytes,
      crc,
      offset: currentOffset,
    });

    parts.push(header);
    parts.push(fileBytes);
    currentOffset += header.length + fileBytes.length;
  }

  // 2. Write Central Directory Headers
  const centralDirStart = currentOffset;
  let centralDirSize = 0;

  for (const entry of entries) {
    const pathBytes = encoder.encode(entry.path);
    const size = entry.bytes.length;

    // Central directory header (46 bytes + path length)
    const cdHeader = new Uint8Array(46 + pathBytes.length);
    const view = new DataView(cdHeader.buffer);

    // Signature: 0x02014b50 ("PK\x01\x02")
    view.setUint32(0, 0x02014b50, true);
    // Version made by: 20 (UNIX / DOS)
    view.setUint16(4, 20, true);
    // Version needed: 20
    view.setUint16(6, 20, true);
    // General purpose bit flag (UTF-8)
    view.setUint16(8, 0x0800, true);
    // Compression method: 0
    view.setUint16(10, 0, true);
    // Dos time & date
    view.setUint16(12, 0x546b, true);
    view.setUint16(14, 0x5d59, true);
    // CRC-32
    view.setUint32(16, entry.crc, true);
    // Compressed size
    view.setUint32(20, size, true);
    // Uncompressed size
    view.setUint32(24, size, true);
    // Filename length
    view.setUint16(28, pathBytes.length, true);
    // Extra field length
    view.setUint16(30, 0, true);
    // File comment length
    view.setUint16(32, 0, true);
    // Disk number start
    view.setUint16(34, 0, true);
    // Internal file attributes
    view.setUint16(36, 0, true);
    // External file attributes
    view.setUint32(38, 0, true);
    // Relative offset of local header
    view.setUint32(42, entry.offset, true);

    cdHeader.set(pathBytes, 46);

    parts.push(cdHeader);
    centralDirSize += cdHeader.length;
  }

  // 3. Write End of Central Directory Record (22 bytes)
  const eocd = new Uint8Array(22);
  const eocdView = new DataView(eocd.buffer);

  // Signature: 0x06054b50 ("PK\x05\x06")
  eocdView.setUint32(0, 0x06054b50, true);
  // Number of this disk
  eocdView.setUint16(4, 0, true);
  // Disk with start of central directory
  eocdView.setUint16(6, 0, true);
  // Total entries on this disk
  eocdView.setUint16(8, entries.length, true);
  // Total entries in central directory
  eocdView.setUint16(10, entries.length, true);
  // Size of central directory
  eocdView.setUint32(12, centralDirSize, true);
  // Offset of start of central directory
  eocdView.setUint32(16, centralDirStart, true);
  // Comment length: 0
  eocdView.setUint16(20, 0, true);

  parts.push(eocd);

  const finalBlob = new Blob(parts, { type: 'application/zip' });
  const finalFilename = archiveName.endsWith('.zip') ? archiveName : `${archiveName}.zip`;
  return new File([finalBlob], finalFilename, { type: 'application/zip' });
}
