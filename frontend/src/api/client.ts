import { FileItem, PairInfoResponse, PairSession, ClipboardItem } from '../types';

const CHUNK_SIZE = 4 * 1024 * 1024; // 4MB

export type AuthListener = (isAuthenticated: boolean) => void;

class ApiClient {
  private accessToken: string | null = null;
  private refreshToken: string | null = null;
  private fingerprint: string | null = null;
  private serverBaseUrl: string = '';
  private authListeners: Set<AuthListener> = new Set();

  constructor() {
    this.accessToken = sessionStorage.getItem('hushdrop_access_token');
    this.refreshToken = sessionStorage.getItem('hushdrop_refresh_token');
    this.fingerprint = sessionStorage.getItem('hushdrop_fingerprint');
    this.serverBaseUrl = sessionStorage.getItem('hushdrop_server_url') || localStorage.getItem('hushdrop_server_url') || '';
  }

  public setServerBaseUrl(url: string) {
    this.serverBaseUrl = url.trim().replace(/\/+$/, '');
    sessionStorage.setItem('hushdrop_server_url', this.serverBaseUrl);
    localStorage.setItem('hushdrop_server_url', this.serverBaseUrl);
  }

  public getServerBaseUrl(): string {
    return this.serverBaseUrl;
  }

  public resolveUrl(endpoint: string): string {
    if (this.serverBaseUrl && endpoint.startsWith('/')) {
      return `${this.serverBaseUrl}${endpoint}`;
    }
    return endpoint;
  }

  public subscribeAuth(listener: AuthListener): () => void {
    this.authListeners.add(listener);
    listener(this.isAuthenticated());
    return () => this.authListeners.delete(listener);
  }

  private notifyAuthChange() {
    const authed = this.isAuthenticated();
    this.authListeners.forEach((fn) => fn(authed));
  }

  public isAuthenticated(): boolean {
    return !!this.accessToken;
  }

  public getFingerprint(): string | null {
    return this.fingerprint;
  }

  private saveSession(session: PairSession) {
    this.accessToken = session.accessToken;
    this.refreshToken = session.refreshToken;
    this.fingerprint = session.fingerprint;
    sessionStorage.setItem('hushdrop_access_token', session.accessToken);
    sessionStorage.setItem('hushdrop_refresh_token', session.refreshToken);
    if (session.fingerprint) {
      sessionStorage.setItem('hushdrop_fingerprint', session.fingerprint);
    }
    this.notifyAuthChange();
  }

  public clearSession() {
    this.accessToken = null;
    this.refreshToken = null;
    sessionStorage.removeItem('hushdrop_access_token');
    sessionStorage.removeItem('hushdrop_refresh_token');
    this.notifyAuthChange();
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {},
    isRetry = false
  ): Promise<T> {
    const headers = new Headers(options.headers || {});

    if (this.accessToken && !headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${this.accessToken}`);
    }

    let response: Response;
    try {
      response = await fetch(this.resolveUrl(endpoint), {
        ...options,
        headers,
      });
    } catch (networkError) {
      throw new Error('Сетевая ошибка: проверьте подключение к той же сети Wi-Fi.');
    }

    if (response.status === 401 && !isRetry && this.refreshToken) {
      const refreshed = await this.tryRefresh();
      if (refreshed) {
        return this.request<T>(endpoint, options, true);
      } else {
        this.clearSession();
        throw new Error('Сессия истекла. Пожалуйста, подтвердите PIN повторно.');
      }
    }

    if (response.status === 429) {
      const retryAfter = response.headers.get('Retry-After') || '60';
      throw new Error(`Превышен лимит запросов (20/мин). Подождите ${retryAfter} сек.`);
    }

    if (!response.ok) {
      let errMsg = `Ошибка сервера (${response.status})`;
      try {
        const body = await response.json();
        if (body && body.error) {
          errMsg = body.error;
        }
      } catch {
        // Fallback to generic message
      }
      throw new Error(errMsg);
    }

    return response.json();
  }

  private async tryRefresh(): Promise<boolean> {
    if (!this.refreshToken) return false;
    try {
      const resp = await fetch(this.resolveUrl('/api/pair/refresh'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: this.refreshToken }),
      });
      if (resp.ok) {
        const data = await resp.json();
        this.saveSession({
          accessToken: data.accessToken,
          refreshToken: data.refreshToken,
          accessExpiresIn: data.accessExpiresIn,
          refreshExpiresIn: data.refreshExpiresIn,
          fingerprint: this.fingerprint || '',
        });
        return true;
      }
    } catch {
      // refresh failure
    }
    return false;
  }

  // API Methods
  public async getHealth(): Promise<{ status: string; version: string }> {
    const resp = await fetch(this.resolveUrl('/health'));
    return resp.json();
  }

  public async getPairInfo(): Promise<PairInfoResponse> {
    const resp = await fetch(this.resolveUrl('/api/pair/info'));
    if (!resp.ok) {
      throw new Error('Не удалось получить информацию о сервере');
    }
    return resp.json();
  }

  public async pair(pin: string, token: string): Promise<PairSession> {
    const resp = await fetch(this.resolveUrl('/api/pair'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin, token }),
    });

    if (!resp.ok) {
      let msg = 'Неверный PIN или токен';
      try {
        const data = await resp.json();
        if (data.error) msg = data.error;
      } catch {
        // generic
      }
      throw new Error(msg);
    }

    const session: PairSession = await resp.json();
    this.saveSession(session);
    return session;
  }

  public async revokeAll(): Promise<void> {
    try {
      await this.request('/api/revoke', { method: 'POST' });
    } finally {
      this.clearSession();
    }
  }

  public async listFiles(): Promise<FileItem[]> {
    const res = await this.request<{ files: FileItem[] }>('/api/files');
    return res.files || [];
  }

  public async fetchLocalBlob(url: string): Promise<Blob> {
    const res = await fetch(url);
    if (!res.ok) {
      throw new Error(`Не удалось загрузить локальные данные: ${res.statusText}`);
    }
    return await res.blob();
  }

  public async uploadChunk(
    fileId: string,
    fileName: string,
    fileSize: number,
    chunkIndex: number,
    totalChunks: number,
    chunkBlob: Blob,
    signal?: AbortSignal
  ): Promise<{ isCompleted: boolean; file?: FileItem }> {
    const headers = new Headers({
      'Content-Type': 'application/octet-stream',
      'X-File-Id': fileId,
      'X-Chunk-Index': String(chunkIndex),
      'X-Total-Chunks': String(totalChunks),
      'X-File-Name': encodeURIComponent(fileName),
      'X-File-Size': String(fileSize),
    });

    if (this.accessToken) {
      headers.set('Authorization', `Bearer ${this.accessToken}`);
    }

    const response = await fetch(this.resolveUrl('/api/upload'), {
      method: 'POST',
      headers,
      body: chunkBlob,
      signal,
    });

    if (response.status === 413) {
      throw new Error('Размер чанка превышает 4МБ или файл более 5ГБ.');
    }

    if (!response.ok) {
      let msg = `Ошибка передачи чанка #${chunkIndex}`;
      try {
        const body = await response.json();
        if (body.error) msg = body.error;
      } catch {}
      throw new Error(msg);
    }

    return response.json();
  }

  public async uploadFile(
    file: File,
    onProgress: (percent: number, speedBytesPerSec: number, etaSeconds: number) => void,
    signal?: AbortSignal
  ): Promise<FileItem | undefined> {
    const fileId = `${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE) || 1;

    let uploadedBytes = 0;
    const startTime = Date.now();
    let completedItem: FileItem | undefined;

    for (let i = 0; i < totalChunks; i++) {
      if (signal?.aborted) {
        throw new Error('Передача отменена');
      }

      const start = i * CHUNK_SIZE;
      const end = Math.min(start + CHUNK_SIZE, file.size);
      const chunkBlob = file.slice(start, end);

      const result = await this.uploadChunk(
        fileId,
        file.name,
        file.size,
        i,
        totalChunks,
        chunkBlob,
        signal
      );

      uploadedBytes += (end - start);
      const elapsedSec = (Date.now() - startTime) / 1000;
      const speed = elapsedSec > 0 ? uploadedBytes / elapsedSec : 0;
      const remainingBytes = file.size - uploadedBytes;
      const eta = speed > 0 ? Math.round(remainingBytes / speed) : 0;
      const percent = Math.min(100, Math.round((uploadedBytes / file.size) * 100));

      onProgress(percent, speed, eta);

      if (result.isCompleted && result.file) {
        completedItem = result.file;
      }
    }

    return completedItem;
  }

  public async downloadFile(fileId: string, fileName: string): Promise<void> {
    const headers = new Headers();
    if (this.accessToken) {
      headers.set('Authorization', `Bearer ${this.accessToken}`);
    }

    const response = await fetch(this.resolveUrl(`/api/download?id=${encodeURIComponent(fileId)}`), {
      method: 'GET',
      headers,
    });

    if (!response.ok) {
      throw new Error('Не удалось загрузить файл с сервера');
    }

    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
  }

  public async getClipboard(): Promise<ClipboardItem> {
    return this.request<ClipboardItem>('/api/clipboard', {
      method: 'GET',
    });
  }

  public async setClipboard(
    text: string,
    burnAfterRead: boolean = false
  ): Promise<{ success: boolean; updatedAt: number }> {
    return this.request<{ success: boolean; updatedAt: number }>('/api/clipboard', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ text, burnAfterRead }),
    });
  }

  public async clearClipboard(): Promise<void> {
    await this.request('/api/clipboard', {
      method: 'DELETE',
    });
  }
}

export const apiClient = new ApiClient();
