import React, { useState, useEffect, useRef } from 'react';
import {
  peerManager,
  PeerConnectionState,
  PeerInfo,
  PeerTransferProgress,
} from '../api/peer';
import {
  Zap,
  CheckCircle2,
  AlertCircle,
  Upload,
  ArrowRight,
  Clipboard,
  Check,
  Share2,
  HardDrive,
  Copy,
} from 'lucide-react';

import { createZipFromFiles } from '../utils/zip';
import {
  FolderUp,
  FileUp,
} from 'lucide-react';

export const PeerPage: React.FC = () => {
  const [roomId, setRoomId] = useState<string>('lan-direct');
  const [connectionState, setConnectionState] = useState<PeerConnectionState>(peerManager.getState());
  const [remotePeer, setRemotePeer] = useState<PeerInfo | null>(peerManager.getRemotePeer());
  const [progresses, setProgresses] = useState<Record<string, PeerTransferProgress>>({});
  const [clipboardText, setClipboardText] = useState<string>('');
  const [receivedClipboard, setReceivedClipboard] = useState<string>('');
  const [copied, setCopied] = useState<boolean>(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [isZipping, setIsZipping] = useState<boolean>(false);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    // Check if ?room= query param exists
    const params = new URLSearchParams(window.location.search);
    const roomParam = params.get('room');
    if (roomParam) {
      setRoomId(roomParam);
      peerManager.connect(roomParam);
    }

    peerManager.subscribe({
      onStateChange: (state, peer) => {
        setConnectionState(state);
        setRemotePeer(peer || null);
        if (state === 'connected') {
          setStatusMessage(`Подключено напрямую к: ${peer?.name || 'Пир'}`);
        } else if (state === 'disconnected') {
          setStatusMessage('Отключено');
        }
      },
      onProgress: (prog) => {
        setProgresses((prev) => ({
          ...prev,
          [prog.fileId]: prog,
        }));
      },
      onClipboardReceived: (text) => {
        setReceivedClipboard(text);
        setStatusMessage('Получен текст в P2P буфер');
      },
      onError: (err) => {
        setStatusMessage(`Ошибка: ${err}`);
      },
    });

    return () => {
      // Don't auto-disconnect so user can switch tabs while transfer is running
    };
  }, []);

  const handleConnect = () => {
    if (!roomId.trim()) return;
    peerManager.connect(roomId.trim());
  };

  const handleDisconnect = () => {
    peerManager.disconnect();
  };

  const handleFiles = async (fileList: FileList | File[]) => {
    const files = Array.from(fileList);
    if (files.length === 0) return;

    // Check if files are from a folder
    const hasRelative = files.some((f) => !!f.webkitRelativePath && f.webkitRelativePath.includes('/'));
    if (hasRelative) {
      const rootFolder = files[0].webkitRelativePath.split('/')[0] || 'folder';
      setIsZipping(true);
      try {
        const zipFile = await createZipFromFiles(
          files.map((f) => ({ file: f, relativePath: f.webkitRelativePath })),
          `${rootFolder}.zip`
        );
        setIsZipping(false);
        await peerManager.sendFile(zipFile);
      } catch (err: any) {
        setIsZipping(false);
        setStatusMessage(`Ошибка упаковки папки: ${err?.message || err}`);
      }
      return;
    }

    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      try {
        await peerManager.sendFile(file);
      } catch (err: any) {
        setStatusMessage(`Ошибка отправки ${file.name}: ${err?.message || err}`);
      }
    }
  };

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      await handleFiles(e.target.files);
    }
    if (fileInputRef.current) fileInputRef.current.value = '';
    if (folderInputRef.current) folderInputRef.current.value = '';
  };

  const handleSendClipboard = () => {
    if (!clipboardText.trim()) return;
    try {
      peerManager.sendClipboard(clipboardText);
      setClipboardText('');
      setStatusMessage('Текст отправлен пиру');
    } catch (err: any) {
      setStatusMessage(`Не удалось отправить текст: ${err?.message || err}`);
    }
  };

  const handleCopyReceived = async () => {
    if (!receivedClipboard) return;
    try {
      await navigator.clipboard.writeText(receivedClipboard);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {}
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  return (
    <div className="space-y-6 animate-in fade-in duration-200">
      {/* Top Banner / Status Card */}
      <div className="p-5 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="flex items-center space-x-3.5">
            <div
              className={`w-12 h-12 rounded-2xl flex items-center justify-center shrink-0 ${
                connectionState === 'connected'
                  ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                  : connectionState === 'connecting'
                  ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20 animate-pulse'
                  : 'bg-slate-800/80 text-slate-400 border border-slate-700/60'
              }`}
            >
              <Zap className="w-6 h-6" />
            </div>
            <div>
              <div className="flex items-center space-x-2">
                <p className="text-sm font-bold text-white">Прямой P2P (WebRTC DataChannel)</p>
                <span
                  className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${
                    connectionState === 'connected'
                      ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/30'
                      : connectionState === 'connecting'
                      ? 'bg-amber-500/20 text-amber-300 border border-amber-500/30'
                      : 'bg-slate-800 text-slate-400 border border-slate-700'
                  }`}
                >
                  {connectionState === 'connected'
                    ? 'Подключено'
                    : connectionState === 'connecting'
                    ? 'Согласование...'
                    : 'Офлайн'}
                </span>
              </div>
              <p className="text-xs text-slate-400 mt-0.5">
                {connectionState === 'connected'
                  ? `Прямой канал с ${remotePeer?.name || 'пиром'}. Данные идут без сервера.`
                  : 'Введите имя комнаты на двух устройствах для создания прямого канала.'}
              </p>
            </div>
          </div>

          {/* Room Controls */}
          <div className="flex items-center space-x-2">
            {connectionState === 'connected' ? (
              <button
                onClick={handleDisconnect}
                className="px-4 py-2 rounded-xl text-xs font-semibold bg-rose-600/20 hover:bg-rose-600/30 text-rose-300 border border-rose-500/30 transition"
              >
                Отключиться
              </button>
            ) : (
              <div className="flex items-center space-x-2 w-full sm:w-auto">
                <input
                  type="text"
                  value={roomId}
                  onChange={(e) => setRoomId(e.target.value)}
                  placeholder="Имя комнаты"
                  className="px-3 py-2 rounded-xl text-xs bg-slate-950 border border-slate-700 text-white focus:outline-none focus:border-emerald-500 w-32 sm:w-36 font-mono"
                />
                <button
                  onClick={handleConnect}
                  disabled={connectionState === 'connecting'}
                  className="px-4 py-2 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition disabled:opacity-50 shrink-0 shadow-lg shadow-emerald-600/20"
                >
                  {connectionState === 'connecting' ? 'Вход...' : 'Войти'}
                </button>
              </div>
            )}
          </div>
        </div>

        {statusMessage && (
          <div className="text-[11px] text-slate-400 bg-slate-950/40 px-3 py-1.5 rounded-lg border border-slate-800 flex items-center justify-between">
            <span>{statusMessage}</span>
            <span className="font-mono text-slate-500">ID: {peerManager.getLocalPeerId()}</span>
          </div>
        )}
      </div>

      {/* File Transfer Section */}
      <div className="p-6 rounded-2xl bg-slate-900/40 border border-slate-800 space-y-4">
        <h3 className="text-xs font-bold uppercase tracking-wider text-slate-400 flex items-center space-x-2">
          <Upload className="w-4 h-4 text-emerald-400" />
          <span>Прямая передача файлов</span>
        </h3>

        <input
          ref={fileInputRef}
          type="file"
          multiple
          onChange={handleFileChange}
          className="hidden"
          id="p2p-file-input"
          disabled={connectionState !== 'connected'}
        />
        <input
          ref={folderInputRef}
          type="file"
          // @ts-expect-error webkitdirectory is standard in browser engines
          webkitdirectory=""
          multiple
          onChange={handleFileChange}
          className="hidden"
          id="p2p-folder-input"
          disabled={connectionState !== 'connected'}
        />

        <div
          className={`border-2 border-dashed rounded-2xl p-8 flex flex-col items-center justify-center text-center transition ${
            connectionState === 'connected'
              ? 'border-slate-700 bg-slate-950/30'
              : 'border-slate-800 bg-slate-950/10 opacity-50'
          }`}
        >
          <div className="w-12 h-12 rounded-2xl bg-slate-800 flex items-center justify-center text-slate-300 mb-3 shadow-inner">
            <HardDrive className="w-6 h-6 text-emerald-400" />
          </div>
          <p className="text-sm font-semibold text-white mb-1">
            {connectionState === 'connected'
              ? 'Прямая передача файлов и каталогов'
              : 'Подключитесь к комнате P2P для отправки'}
          </p>
          <p className="text-xs text-slate-500 mb-4">
            Папки автоматически упаковываются в архив на лету с сохранением структуры
          </p>

          <div className="flex flex-wrap items-center justify-center gap-3">
            <button
              type="button"
              disabled={connectionState !== 'connected'}
              onClick={() => fileInputRef.current?.click()}
              className="flex items-center space-x-2 px-4 py-2.5 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition disabled:opacity-50 shadow-lg shadow-emerald-600/20"
            >
              <FileUp className="w-4 h-4" />
              <span>Выбрать файлы</span>
            </button>

            <button
              type="button"
              disabled={connectionState !== 'connected'}
              onClick={() => folderInputRef.current?.click()}
              className="flex items-center space-x-2 px-4 py-2.5 rounded-xl text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-slate-200 transition disabled:opacity-50 border border-slate-700"
            >
              <FolderUp className="w-4 h-4" />
              <span>Выбрать папку</span>
            </button>
          </div>

          {isZipping && (
            <div className="mt-4 p-2.5 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-xs text-emerald-300 flex items-center space-x-2 animate-pulse">
              <span>Упаковка папки в ZIP на лету...</span>
            </div>
          )}
        </div>

        {/* Transfer Progress List */}
        {Object.values(progresses).length > 0 && (
          <div className="space-y-3 pt-2">
            {Object.values(progresses).map((prog) => (
              <div
                key={prog.fileId}
                className="p-3.5 rounded-xl bg-slate-950/60 border border-slate-800/80 space-y-2"
              >
                <div className="flex items-center justify-between text-xs">
                  <span className="font-medium text-slate-200 truncate max-w-[220px]">
                    {prog.fileName}
                  </span>
                  <div className="flex items-center space-x-2">
                    {prog.status === 'completed' ? (
                      <span className="text-emerald-400 flex items-center space-x-1 font-semibold text-[11px]">
                        <CheckCircle2 className="w-3.5 h-3.5" />
                        <span>Готово</span>
                      </span>
                    ) : prog.status === 'error' ? (
                      <span className="text-rose-400 flex items-center space-x-1 font-semibold text-[11px]">
                        <AlertCircle className="w-3.5 h-3.5" />
                        <span>Сбой</span>
                      </span>
                    ) : (
                      <span className="text-slate-400 text-[11px] font-mono">
                        {prog.percent}% • {formatBytes(prog.speed)}/s
                      </span>
                    )}
                  </div>
                </div>

                <div className="w-full bg-slate-800 h-1.5 rounded-full overflow-hidden">
                  <div
                    className={`h-full transition-all duration-150 ${
                      prog.status === 'completed'
                        ? 'bg-emerald-500'
                        : prog.status === 'error'
                        ? 'bg-rose-500'
                        : 'bg-emerald-500'
                    }`}
                    style={{ width: `${prog.percent}%` }}
                  />
                </div>

                <div className="flex items-center justify-between text-[10px] text-slate-500">
                  <span>
                    {prog.direction === 'send' ? 'Отправка' : 'Прием'}: {formatBytes(prog.bytesTransferred)} из {formatBytes(prog.fileSize)}
                  </span>
                  {prog.eta > 0 && <span>Осталось ~{prog.eta} сек</span>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* P2P Clipboard Section */}
      <div className="p-6 rounded-2xl bg-slate-900/40 border border-slate-800 space-y-4">
        <h3 className="text-xs font-bold uppercase tracking-wider text-slate-400 flex items-center space-x-2">
          <Clipboard className="w-4 h-4 text-emerald-400" />
          <span>Прямой P2P буфер обмена</span>
        </h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Send text */}
          <div className="space-y-2">
            <textarea
              value={clipboardText}
              onChange={(e) => setClipboardText(e.target.value)}
              placeholder="Введите текст для мгновенной отправки пиру..."
              rows={3}
              disabled={connectionState !== 'connected'}
              className="w-full p-3 rounded-xl bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-emerald-500 resize-none disabled:opacity-50"
            />
            <button
              onClick={handleSendClipboard}
              disabled={connectionState !== 'connected' || !clipboardText.trim()}
              className="w-full flex items-center justify-center space-x-2 py-2 rounded-xl text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-white transition disabled:opacity-50 border border-slate-700"
            >
              <ArrowRight className="w-3.5 h-3.5 text-emerald-400" />
              <span>Отправить пиру</span>
            </button>
          </div>

          {/* Received text */}
          <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 flex flex-col justify-between space-y-2">
            <div>
              <span className="text-[10px] font-semibold uppercase text-slate-500 tracking-wider">
                Получено от пира
              </span>
              <p className="text-xs text-slate-300 mt-1 break-words font-mono min-h-[48px]">
                {receivedClipboard || 'Буфер пуст'}
              </p>
            </div>
            {receivedClipboard && (
              <button
                onClick={handleCopyReceived}
                className="self-end flex items-center space-x-1.5 px-3 py-1 rounded-lg bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 text-xs font-medium transition"
              >
                {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                <span>{copied ? 'Скопировано!' : 'Копировать'}</span>
              </button>
            )}
          </div>
        </div>
      </div>

      {/* LAN Fallback Advisory */}
      <div className="p-4 rounded-xl bg-slate-900/30 border border-slate-800/60 text-xs text-slate-500 flex items-start space-x-3">
        <Share2 className="w-4 h-4 text-slate-400 mt-0.5 shrink-0" />
        <p>
          Прямой P2P WebRTC работает напрямую между устройствами в локальной сети (LAN). Если устройства находятся в разных VLAN или брандмауэр блокирует UDP/SCTP, переключитесь на вкладку <strong className="text-slate-400">Отправить / Получить</strong> для надежной передачи через встроенный HTTPS-сервер хоста.
        </p>
      </div>
    </div>
  );
};
