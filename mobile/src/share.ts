/**
 * HushDrop Mobile Share & Deep-Link Bridge
 * Handles Android ACTION_SEND / ACTION_SEND_MULTIPLE incoming intents
 * and integrates with HushDropHostPlugin.
 */

import { App } from '@capacitor/app';
import { registerPlugin, PluginListenerHandle } from '@capacitor/core';

export interface SharedFileItem {
  name: string;
  size: number;
  mime: string;
  cachePath: string;
  text?: string;
}

export interface SharedFilesResult {
  files: SharedFileItem[];
}

export interface HushDropSharePluginInterface {
  getSharedFiles(): Promise<SharedFilesResult>;
  clearSharedFiles(): Promise<void>;
  readSharedFileBase64?(options: { path: string }): Promise<{ data: string }>;
  addListener(
    eventName: 'shareReceived',
    listenerFunc: (data: SharedFilesResult) => void
  ): Promise<PluginListenerHandle>;
}

export const HushDropHostShare = registerPlugin<HushDropSharePluginInterface>('HushDropHost');

// Memory queue to hold shared files while user is not authenticated or pairing
let pendingSharedQueue: SharedFileItem[] = [];

export function getPendingSharedQueue(): SharedFileItem[] {
  return [...pendingSharedQueue];
}

export function clearPendingSharedQueue(): void {
  pendingSharedQueue = [];
}

export function enqueuePendingShared(files: SharedFileItem[]): void {
  pendingSharedQueue.push(...files);
}

/**
 * Converts a native cache file path into a Web standard File object.
 */
export async function sharedItemToWebFile(item: SharedFileItem): Promise<File> {
  const cap = (window as any).Capacitor;
  if (cap && typeof cap.convertFileSrc === 'function' && item.cachePath) {
    try {
      const src = cap.convertFileSrc(item.cachePath);
      const res = await fetch(src);
      if (res.ok) {
        const blob = await res.blob();
        return new File([blob], item.name, { type: item.mime });
      }
    } catch {
      // Fallback below
    }
  }

  // Fallback via readSharedFileBase64 if available
  if (HushDropHostShare.readSharedFileBase64 && item.cachePath) {
    try {
      const { data } = await HushDropHostShare.readSharedFileBase64({ path: item.cachePath });
      const byteChars = atob(data);
      const byteNumbers = new Array(byteChars.length);
      for (let i = 0; i < byteChars.length; i++) {
        byteNumbers[i] = byteChars.charCodeAt(i);
      }
      const byteArray = new Uint8Array(byteNumbers);
      const blob = new Blob([byteArray], { type: item.mime });
      return new File([blob], item.name, { type: item.mime });
    } catch {
      // Fallback below
    }
  }

  if (item.text) {
    return new File([item.text], item.name, { type: item.mime || 'text/plain' });
  }

  throw new Error(`Не удалось загрузить файл из кэша: ${item.name}`);
}

/**
 * Subscribes to deep-links and incoming system share events.
 * If active session exists, files are delivered directly via onFilesReady.
 * If no session exists, files are enqueued and onWaitingForAuth is called.
 */
export function initShareBridge(options: {
  isSessionActive: () => boolean;
  onFilesReady: (files: File[]) => void;
  onWaitingForAuth?: (pendingItems: SharedFileItem[]) => void;
  onDeepLink?: (url: string) => void;
}): () => void {
  const cleanups: Array<() => void> = [];

  const processIncomingItems = async (items: SharedFileItem[]) => {
    if (!items || items.length === 0) return;

    if (!options.isSessionActive()) {
      enqueuePendingShared(items);
      if (options.onWaitingForAuth) {
        options.onWaitingForAuth(getPendingSharedQueue());
      }
      return;
    }

    const converted: File[] = [];
    for (const item of items) {
      try {
        const file = await sharedItemToWebFile(item);
        converted.push(file);
      } catch (err) {
        console.error('Ошибка преобразования расшаренного файла:', err);
      }
    }

    if (converted.length > 0) {
      options.onFilesReady(converted);
    }
  };

  // 1. Listen for App URL open events (deep links)
  App.addListener('appUrlOpen', (event) => {
    if (options.onDeepLink) {
      options.onDeepLink(event.url);
    }
  }).then((handle) => {
    cleanups.push(() => handle.remove());
  });

  // 2. Listen for runtime shareReceived events
  HushDropHostShare.addListener('shareReceived', (data) => {
    if (data?.files && data.files.length > 0) {
      processIncomingItems(data.files);
    }
  }).then((handle) => {
    cleanups.push(() => handle.remove());
  });

  // 3. Cold start check: poll getSharedFiles on boot
  HushDropHostShare.getSharedFiles()
    .then((res) => {
      if (res?.files && res.files.length > 0) {
        processIncomingItems(res.files);
      }
    })
    .catch(() => {});

  return () => {
    cleanups.forEach((c) => c());
  };
}
