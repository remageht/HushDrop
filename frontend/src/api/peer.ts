import { apiClient } from './client';

export interface PeerInfo {
  id: string;
  name: string;
}

export interface FileTransferMeta {
  id: string;
  name: string;
  size: number;
  mime: string;
  totalChunks: number;
  chunkSize: number;
}

export type PeerConnectionState = 'disconnected' | 'connecting' | 'connected' | 'failed';

export interface PeerTransferProgress {
  fileId: string;
  fileName: string;
  fileSize: number;
  bytesTransferred: number;
  percent: number;
  speed: number;
  eta: number;
  status: 'transferring' | 'completed' | 'cancelled' | 'error';
  direction: 'send' | 'receive';
  error?: string;
}

const CHUNK_SIZE = 64 * 1024; // 64 KB optimal for SCTP DataChannel
const BUFFER_THRESHOLD = 256 * 1024; // 256 KB flow control threshold

export class PeerTransferManager {
  private ws: WebSocket | null = null;
  private pc: RTCPeerConnection | null = null;
  private dc: RTCDataChannel | null = null;
  private roomId: string = '';
  private localPeerId: string = '';
  private localName: string = '';
  private remotePeerId: string = '';
  private remoteName: string = '';
  private connectionState: PeerConnectionState = 'disconnected';

  // Event callbacks
  private onStateChangeCb?: (state: PeerConnectionState, remotePeer?: PeerInfo) => void;
  private onProgressCb?: (progress: PeerTransferProgress) => void;
  private onFileReceivedCb?: (file: Blob, meta: FileTransferMeta) => void;
  private onClipboardReceivedCb?: (text: string) => void;
  private onErrorCb?: (error: string) => void;

  // Active receive state
  private incomingMeta: FileTransferMeta | null = null;
  private incomingChunks: ArrayBuffer[] = [];
  private incomingBytesReceived: number = 0;
  private incomingStartTime: number = 0;

  constructor() {
    this.localPeerId = Math.random().toString(36).substring(2, 10);
    this.localName = navigator.userAgent.includes('Mobile') ? 'Мобильный' : 'Компьютер';
  }

  public subscribe(callbacks: {
    onStateChange?: (state: PeerConnectionState, remotePeer?: PeerInfo) => void;
    onProgress?: (progress: PeerTransferProgress) => void;
    onFileReceived?: (file: Blob, meta: FileTransferMeta) => void;
    onClipboardReceived?: (text: string) => void;
    onError?: (error: string) => void;
  }) {
    this.onStateChangeCb = callbacks.onStateChange;
    this.onProgressCb = callbacks.onProgress;
    this.onFileReceivedCb = callbacks.onFileReceived;
    this.onClipboardReceivedCb = callbacks.onClipboardReceived;
    this.onErrorCb = callbacks.onError;
  }

  public getLocalPeerId(): string {
    return this.localPeerId;
  }

  public getRemotePeer(): PeerInfo | null {
    if (!this.remotePeerId) return null;
    return { id: this.remotePeerId, name: this.remoteName || 'Пир' };
  }

  public getState(): PeerConnectionState {
    return this.connectionState;
  }

  public connect(roomId: string, name?: string) {
    this.disconnect();
    this.roomId = roomId || 'default';
    if (name) this.localName = name;

    this.setState('connecting');

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const token = apiClient.getAccessToken() || '';
    const wsUrl = `${protocol}//${host}/api/signal?room=${encodeURIComponent(
      this.roomId
    )}&peer=${encodeURIComponent(this.localPeerId)}&name=${encodeURIComponent(
      this.localName
    )}&token=${encodeURIComponent(token)}`;

    try {
      this.ws = new WebSocket(wsUrl);
    } catch (err: any) {
      this.setState('failed');
      this.onErrorCb?.(`Ошибка подключения к сигналингу: ${err?.message || err}`);
      return;
    }

    this.ws.onopen = () => {
      // WebSocket connected, waiting for room info
    };

    this.ws.onmessage = async (event) => {
      try {
        const msg = JSON.parse(event.data);
        await this.handleSignalingMessage(msg);
      } catch (err) {
        console.error('[WebRTC] Signaling message error:', err);
      }
    };

    this.ws.onerror = () => {
      this.setState('failed');
      this.onErrorCb?.('Ошибка соединения с сервером сигналинга');
    };

    this.ws.onclose = () => {
      if (this.connectionState !== 'connected') {
        this.setState('disconnected');
      }
    };
  }

  public disconnect() {
    if (this.dc) {
      try { this.dc.close(); } catch {}
      this.dc = null;
    }
    if (this.pc) {
      try { this.pc.close(); } catch {}
      this.pc = null;
    }
    if (this.ws) {
      try { this.ws.close(); } catch {}
      this.ws = null;
    }
    this.remotePeerId = '';
    this.remoteName = '';
    this.setState('disconnected');
  }

  private setState(state: PeerConnectionState) {
    this.connectionState = state;
    this.onStateChangeCb?.(
      state,
      this.remotePeerId ? { id: this.remotePeerId, name: this.remoteName } : undefined
    );
  }

  private async handleSignalingMessage(msg: any) {
    switch (msg.type) {
      case 'joined':
        // If other peers already exist in the room, take the first one and create an offer
        if (msg.peers && msg.peers.length > 0) {
          this.remotePeerId = msg.peers[0];
          await this.initiatePeerConnection(true);
        }
        break;

      case 'peer-joined':
        // A new peer joined. If we don't have a peer, save their ID
        if (!this.remotePeerId) {
          this.remotePeerId = msg.peerId;
          this.remoteName = msg.name || 'Пир';
        }
        break;

      case 'offer':
        if (msg.peerId) {
          this.remotePeerId = msg.peerId;
          this.remoteName = msg.name || 'Пир';
        }
        await this.initiatePeerConnection(false);
        if (this.pc && msg.payload) {
          await this.pc.setRemoteDescription(new RTCSessionDescription(msg.payload));
          const answer = await this.pc.createAnswer();
          await this.pc.setLocalDescription(answer);
          this.sendSignalingMessage({
            type: 'answer',
            targetId: this.remotePeerId,
            payload: answer,
          });
        }
        break;

      case 'answer':
        if (this.pc && msg.payload) {
          await this.pc.setRemoteDescription(new RTCSessionDescription(msg.payload));
        }
        break;

      case 'candidate':
        if (this.pc && msg.payload) {
          try {
            await this.pc.addIceCandidate(new RTCIceCandidate(msg.payload));
          } catch (err) {
            console.warn('[WebRTC] ICE candidate error:', err);
          }
        }
        break;

      case 'peer-left':
        if (msg.peerId === this.remotePeerId) {
          this.disconnect();
          this.onErrorCb?.('Удаленный пир отключился');
        }
        break;

      case 'error':
        this.onErrorCb?.(msg.message || 'Ошибка сигналинга');
        break;
    }
  }

  private sendSignalingMessage(msg: any) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private async initiatePeerConnection(isInitiator: boolean) {
    // LAN-only: strictly empty iceServers to prevent internet/STUN leaks
    this.pc = new RTCPeerConnection({
      iceServers: [],
    });

    this.pc.onicecandidate = (event) => {
      if (event.candidate && this.remotePeerId) {
        this.sendSignalingMessage({
          type: 'candidate',
          targetId: this.remotePeerId,
          payload: event.candidate,
        });
      }
    };

    this.pc.onconnectionstatechange = () => {
      const state = this.pc?.connectionState;
      if (state === 'connected') {
        this.setState('connected');
      } else if (state === 'failed' || state === 'closed') {
        this.setState('disconnected');
      }
    };

    if (isInitiator) {
      this.dc = this.pc.createDataChannel('hushdrop-data', {
        ordered: true,
      });
      this.setupDataChannel(this.dc);

      const offer = await this.pc.createOffer();
      await this.pc.setLocalDescription(offer);
      this.sendSignalingMessage({
        type: 'offer',
        targetId: this.remotePeerId,
        name: this.localName,
        payload: offer,
      });
    } else {
      this.pc.ondatachannel = (event) => {
        this.dc = event.channel;
        this.setupDataChannel(this.dc);
      };
    }
  }

  private setupDataChannel(dc: RTCDataChannel) {
    dc.binaryType = 'arraybuffer';
    dc.bufferedAmountLowThreshold = BUFFER_THRESHOLD / 2;

    dc.onopen = () => {
      this.setState('connected');
    };

    dc.onclose = () => {
      this.setState('disconnected');
    };

    dc.onerror = (err) => {
      console.error('[WebRTC DataChannel] Error:', err);
    };

    dc.onmessage = (event) => {
      if (typeof event.data === 'string') {
        try {
          const packet = JSON.parse(event.data);
          this.handleControlPacket(packet);
        } catch (e) {
          console.error('[WebRTC] Invalid text packet:', e);
        }
      } else if (event.data instanceof ArrayBuffer) {
        this.handleBinaryChunk(event.data);
      }
    };
  }

  private handleControlPacket(packet: any) {
    switch (packet.type) {
      case 'file-start':
        this.incomingMeta = packet.meta as FileTransferMeta;
        this.incomingChunks = [];
        this.incomingBytesReceived = 0;
        this.incomingStartTime = Date.now();
        this.onProgressCb?.({
          fileId: this.incomingMeta.id,
          fileName: this.incomingMeta.name,
          fileSize: this.incomingMeta.size,
          bytesTransferred: 0,
          percent: 0,
          speed: 0,
          eta: 0,
          status: 'transferring',
          direction: 'receive',
        });
        break;

      case 'file-end':
        if (this.incomingMeta) {
          const blob = new Blob(this.incomingChunks, { type: this.incomingMeta.mime || 'application/octet-stream' });
          const meta = this.incomingMeta;
          this.onProgressCb?.({
            fileId: meta.id,
            fileName: meta.name,
            fileSize: meta.size,
            bytesTransferred: meta.size,
            percent: 100,
            speed: 0,
            eta: 0,
            status: 'completed',
            direction: 'receive',
          });
          this.onFileReceivedCb?.(blob, meta);
          // Auto-download received blob
          this.triggerDownload(blob, meta.name);
          this.incomingMeta = null;
          this.incomingChunks = [];
        }
        break;

      case 'clipboard':
        if (typeof packet.text === 'string') {
          this.onClipboardReceivedCb?.(packet.text);
        }
        break;

      case 'cancel':
        if (this.incomingMeta) {
          this.onProgressCb?.({
            fileId: this.incomingMeta.id,
            fileName: this.incomingMeta.name,
            fileSize: this.incomingMeta.size,
            bytesTransferred: this.incomingBytesReceived,
            percent: 0,
            speed: 0,
            eta: 0,
            status: 'cancelled',
            direction: 'receive',
            error: 'Передача отменена отправителем',
          });
          this.incomingMeta = null;
          this.incomingChunks = [];
        }
        break;
    }
  }

  private handleBinaryChunk(chunk: ArrayBuffer) {
    if (!this.incomingMeta) return;

    this.incomingChunks.push(chunk);
    this.incomingBytesReceived += chunk.byteLength;

    const total = this.incomingMeta.size;
    const elapsed = (Date.now() - this.incomingStartTime) / 1000;
    const speed = elapsed > 0 ? this.incomingBytesReceived / elapsed : 0;
    const remaining = total - this.incomingBytesReceived;
    const eta = speed > 0 ? Math.round(remaining / speed) : 0;
    const percent = Math.min(100, Math.round((this.incomingBytesReceived / total) * 100));

    this.onProgressCb?.({
      fileId: this.incomingMeta.id,
      fileName: this.incomingMeta.name,
      fileSize: total,
      bytesTransferred: this.incomingBytesReceived,
      percent,
      speed,
      eta,
      status: 'transferring',
      direction: 'receive',
    });
  }

  public async sendFile(file: File, onProgress?: (p: PeerTransferProgress) => void): Promise<void> {
    if (!this.dc || this.dc.readyState !== 'open') {
      throw new Error('WebRTC DataChannel не открыт');
    }

    const fileId = Math.random().toString(36).substring(2, 10);
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
    const meta: FileTransferMeta = {
      id: fileId,
      name: file.name,
      size: file.size,
      mime: file.type || 'application/octet-stream',
      totalChunks,
      chunkSize: CHUNK_SIZE,
    };

    // 1. Send file start metadata
    this.dc.send(JSON.stringify({ type: 'file-start', meta }));

    const startTime = Date.now();
    let sentBytes = 0;

    // 2. Stream binary chunks with flow control
    for (let i = 0; i < totalChunks; i++) {
      const start = i * CHUNK_SIZE;
      const end = Math.min(start + CHUNK_SIZE, file.size);
      const slice = file.slice(start, end);
      const buffer = await slice.arrayBuffer();

      // Flow control: wait if buffer exceeds threshold
      if (this.dc.bufferedAmount > BUFFER_THRESHOLD) {
        await new Promise<void>((resolve) => {
          const handler = () => {
            this.dc?.removeEventListener('bufferedamountlow', handler);
            resolve();
          };
          this.dc?.addEventListener('bufferedamountlow', handler);
        });
      }

      this.dc.send(buffer);
      sentBytes += (end - start);

      const elapsed = (Date.now() - startTime) / 1000;
      const speed = elapsed > 0 ? sentBytes / elapsed : 0;
      const remaining = file.size - sentBytes;
      const eta = speed > 0 ? Math.round(remaining / speed) : 0;
      const percent = Math.min(100, Math.round((sentBytes / file.size) * 100));

      const prog: PeerTransferProgress = {
        fileId,
        fileName: file.name,
        fileSize: file.size,
        bytesTransferred: sentBytes,
        percent,
        speed,
        eta,
        status: 'transferring',
        direction: 'send',
      };
      this.onProgressCb?.(prog);
      onProgress?.(prog);
    }

    // 3. Send file completion
    this.dc.send(JSON.stringify({ type: 'file-end', id: fileId }));

    const finalProg: PeerTransferProgress = {
      fileId,
      fileName: file.name,
      fileSize: file.size,
      bytesTransferred: file.size,
      percent: 100,
      speed: 0,
      eta: 0,
      status: 'completed',
      direction: 'send',
    };
    this.onProgressCb?.(finalProg);
    onProgress?.(finalProg);
  }

  public sendClipboard(text: string) {
    if (!this.dc || this.dc.readyState !== 'open') {
      throw new Error('WebRTC DataChannel не открыт');
    }
    this.dc.send(JSON.stringify({ type: 'clipboard', text }));
  }

  private triggerDownload(blob: Blob, fileName: string) {
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    setTimeout(() => {
      window.URL.revokeObjectURL(url);
      a.remove();
    }, 1500);
  }
}

export const peerManager = new PeerTransferManager();
