export interface FileItem {
  id: string;
  originalName: string;
  cleanName: string;
  size: number;
  mimeType: string;
  totalChunks: number;
  uploadedChunks: number;
  sha256: string;
  isDangerous: boolean;
  isCompleted: boolean;
  createdAt: string;
}

export interface PairSession {
  accessToken: string;
  refreshToken: string;
  accessExpiresIn: number;
  refreshExpiresIn: number;
  fingerprint: string;
  serverPubKey?: string;
}

export interface PairInfoResponse {
  fingerprint: string;
  token: string;
  expiresIn: number;
}

export interface ActiveTransfer {
  id: string;
  file: File;
  name: string;
  size: number;
  isDangerous: boolean;
  totalChunks: number;
  uploadedChunks: number;
  progressPercent: number;
  speedBytesPerSec: number;
  etaSeconds: number;
  status: 'pending' | 'uploading' | 'completed' | 'error' | 'cancelled';
  errorMessage?: string;
  abortController?: AbortController;
}

export interface ClipboardItem {
  text: string;
  burnAfterRead: boolean;
  updatedAt: number;
  isEmpty?: boolean;
}

