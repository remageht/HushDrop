/**
 * HushDrop Mobile Share & Deep-Link Bridge
 * Handles Android ACTION_SEND / ACTION_SEND_MULTIPLE incoming intents
 * and integrates with HushDropHostPlugin.
 */

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

export interface PluginListenerHandle {
  remove: () => Promise<void> | void;
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

const getCapacitor = () => (typeof window !== 'undefined' ? (window as any).Capacitor : null);

export const getHushDropHostPlugin = (): HushDropSharePluginInterface | null => {
  const cap = getCapacitor();
  return cap?.Plugins?.HushDropHost || null;
};

// Memory queue to hold shared files while user is not authenticated or pairing
let pendingSharedQueue: SharedFileItem[] = [];
const pendingListeners = new Set<(queue: SharedFileItem[]) => void>();

export function getPendingSharedQueue(): SharedFileItem[] {
  return [...pendingSharedQueue];
}

export function subscribePendingShared(listener: (queue: SharedFileItem[]) => void): () => void {
  pendingListeners.add(listener);
  listener([...pendingSharedQueue]);
  return () => {
    pendingListeners.delete(listener);
  };
}

function notifyPendingChanged(): void {
  const current = [...pendingSharedQueue];
  pendingListeners.forEach((l) => {
    try { l(current); } catch {}
  });
}

export function clearPendingSharedQueue(): void {
  pendingSharedQueue = [];
  notifyPendingChanged();
  const plugin = getHushDropHostPlugin();
  try {
    plugin?.clearSharedFiles?.();
  } catch {}
}

export function enqueuePendingShared(files: SharedFileItem[]): void {
  pendingSharedQueue.push(...files);
  notifyPendingChanged();
}

/**
 * Converts and removes all pending shared items from the queue.
 */
export async function drainPendingSharedFiles(
  fetchBlob?: (url: string) => Promise<Blob>
): Promise<File[]> {
  const items = [...pendingSharedQueue];
  clearPendingSharedQueue();
  const converted: File[] = [];
  for (const item of items) {
    try {
      const file = await sharedItemToWebFile(item, fetchBlob);
      converted.push(file);
    } catch (err) {
      console.error('Ошибка преобразования расшаренного файла:', err);
    }
  }
  return converted;
}

// Ready file listeners (for delivering files to active SendPage upload queue)
type FilesReadyListener = (files: File[]) => void;
const readyListeners = new Set<FilesReadyListener>();
let queuedReadyFiles: File[] = [];

export function subscribeFilesReady(listener: FilesReadyListener): () => void {
  readyListeners.add(listener);
  if (queuedReadyFiles.length > 0) {
    const files = [...queuedReadyFiles];
    queuedReadyFiles = [];
    listener(files);
  }
  return () => {
    readyListeners.delete(listener);
  };
}

export function emitFilesReady(files: File[]): void {
  if (files.length === 0) return;
  if (readyListeners.size === 0) {
    queuedReadyFiles.push(...files);
  } else {
    readyListeners.forEach((l) => {
      try { l(files); } catch {}
    });
  }
}

/**
 * Converts a native cache file path into a Web standard File object.
 * Uses client.ts blob fetcher when provided, avoiding raw fetch in components.
 */
export async function sharedItemToWebFile(
  item: SharedFileItem,
  fetchBlob?: (url: string) => Promise<Blob>
): Promise<File> {
  const cap = getCapacitor();
  const plugin = getHushDropHostPlugin();

  if (cap && typeof cap.convertFileSrc === 'function' && item.cachePath) {
    try {
      const src = cap.convertFileSrc(item.cachePath);
      const blob = fetchBlob ? await fetchBlob(src) : await (await fetch(src)).blob();
      return new File([blob], item.name, { type: item.mime });
    } catch {
      // Fallback below
    }
  }

  // Fallback via readSharedFileBase64 if available
  if (plugin && typeof plugin.readSharedFileBase64 === 'function' && item.cachePath) {
    try {
      const { data } = await plugin.readSharedFileBase64({ path: item.cachePath });
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
  fetchBlob?: (url: string) => Promise<Blob>;
}): () => void {
  const cap = getCapacitor();
  const plugin = getHushDropHostPlugin();
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
        const file = await sharedItemToWebFile(item, options.fetchBlob);
        converted.push(file);
      } catch (err) {
        console.error('Ошибка преобразования расшаренного файла:', err);
      }
    }

    if (converted.length > 0) {
      options.onFilesReady(converted);
      emitFilesReady(converted);
    }
  };

  // 1. Listen for App URL open events (deep links)
  const appPlugin = cap?.Plugins?.App;
  if (appPlugin && typeof appPlugin.addListener === 'function') {
    try {
      const handleOrPromise = appPlugin.addListener('appUrlOpen', (event: any) => {
        if (options.onDeepLink && event?.url) {
          options.onDeepLink(event.url);
        }
      });
      if (handleOrPromise) {
        if (typeof (handleOrPromise as any).then === 'function') {
          (handleOrPromise as Promise<any>)
            .then((h) => {
              if (h?.remove) cleanups.push(() => h.remove());
            })
            .catch(() => {});
        } else if (typeof (handleOrPromise as any).remove === 'function') {
          cleanups.push(() => (handleOrPromise as any).remove());
        }
      }
    } catch (err) {
      console.warn('Failed to attach appUrlOpen listener:', err);
    }
  }

  // 2. Listen for runtime shareReceived events
  if (plugin && typeof plugin.addListener === 'function') {
    try {
      const handleOrPromise = plugin.addListener('shareReceived', (data: any) => {
        if (data?.files && data.files.length > 0) {
          processIncomingItems(data.files);
        }
      });
      if (handleOrPromise) {
        if (typeof (handleOrPromise as any).then === 'function') {
          (handleOrPromise as Promise<any>)
            .then((h) => {
              if (h?.remove) cleanups.push(() => h.remove());
            })
            .catch(() => {});
        } else if (typeof (handleOrPromise as any).remove === 'function') {
          cleanups.push(() => (handleOrPromise as any).remove());
        }
      }
    } catch (err) {
      console.warn('Failed to attach shareReceived listener:', err);
    }
  }

  // 3. Cold start check: query getSharedFiles on boot
  if (plugin && typeof plugin.getSharedFiles === 'function') {
    plugin.getSharedFiles()
      .then((res: any) => {
        if (res?.files && res.files.length > 0) {
          processIncomingItems(res.files);
        }
      })
      .catch(() => {});
  }

  return () => {
    cleanups.forEach((c) => {
      try { c(); } catch {}
    });
  };
}
