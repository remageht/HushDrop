import { registerPlugin } from '@capacitor/core';

export interface HostStatus {
  isRunning: boolean;
  ip?: string;
  port?: number;
  httpPort?: number;
  pin?: string;
  token?: string;
  fingerprint?: string;
  expiresIn?: number;
  activeSessions?: number;
  filesCount?: number;
  qrUrl?: string;
}

export interface NetworkInfo {
  ips: string[];
  selectedIp: string;
}

export interface HostedFileResult {
  success: boolean;
  file?: {
    id: string;
    originalName: string;
    cleanName: string;
    size: number;
    mimeType: string;
    sha256: string;
    isDangerous: boolean;
    isCompleted: boolean;
  };
}

export interface HushDropHostPluginInterface {
  startHost(): Promise<HostStatus>;
  stopHost(): Promise<{ isRunning: boolean }>;
  getHostStatus(): Promise<HostStatus>;
  getNetworkInfo(): Promise<NetworkInfo>;
  addHostedFile(options: { path: string; name?: string; mimeType?: string }): Promise<HostedFileResult>;
}

export const HushDropHost = registerPlugin<HushDropHostPluginInterface>('HushDropHost');
