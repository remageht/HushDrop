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

export const isNativeHostSupported = (): boolean => {
  const cap = (window as any).Capacitor;
  if (!cap) return false;
  if (typeof cap.isPluginAvailable === 'function') {
    return cap.isPluginAvailable('HushDropHost');
  }
  return !!cap.Plugins?.HushDropHost;
};

export const startNativeHost = async (): Promise<HostStatus> => {
  const cap = (window as any).Capacitor;
  if (!cap?.Plugins?.HushDropHost) {
    throw new Error('Мобильный хост поддерживается только в Android-приложении HushDrop.');
  }
  return await cap.Plugins.HushDropHost.startHost();
};

export const stopNativeHost = async (): Promise<{ isRunning: boolean }> => {
  const cap = (window as any).Capacitor;
  if (!cap?.Plugins?.HushDropHost) {
    return { isRunning: false };
  }
  return await cap.Plugins.HushDropHost.stopHost();
};

export const getNativeHostStatus = async (): Promise<HostStatus> => {
  const cap = (window as any).Capacitor;
  if (!cap?.Plugins?.HushDropHost) {
    return { isRunning: false };
  }
  return await cap.Plugins.HushDropHost.getHostStatus();
};

export const getNativeNetworkInfo = async (): Promise<NetworkInfo> => {
  const cap = (window as any).Capacitor;
  if (!cap?.Plugins?.HushDropHost) {
    return { ips: [], selectedIp: '' };
  }
  return await cap.Plugins.HushDropHost.getNetworkInfo();
};
