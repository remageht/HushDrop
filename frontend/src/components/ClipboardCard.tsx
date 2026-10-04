import React, { useState, useEffect } from 'react';
import {
  Clipboard,
  Copy,
  Check,
  Flame,
  Trash2,
  RefreshCw,
  CornerDownLeft,
  ShieldCheck,
  AlertCircle
} from 'lucide-react';
import { apiClient } from '../api/client';
import { ClipboardItem } from '../types';

export const ClipboardCard: React.FC = () => {
  const [inputText, setInputText] = useState<string>('');
  const [burnAfterRead, setBurnAfterRead] = useState<boolean>(false);
  const [remoteItem, setRemoteItem] = useState<ClipboardItem | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isSending, setIsSending] = useState<boolean>(false);
  const [copied, setCopied] = useState<boolean>(false);
  const [pasted, setPasted] = useState<boolean>(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  useEffect(() => {
    fetchClipboard();
  }, []);

  const fetchClipboard = async () => {
    setIsLoading(true);
    try {
      const item = await apiClient.getClipboard();
      if (item && !item.isEmpty && item.text) {
        setRemoteItem(item);
      } else {
        setRemoteItem(null);
      }
    } catch {
      // ignore fetch error if not paired or network glitch
    } finally {
      setIsLoading(false);
    }
  };

  const handleSend = async () => {
    if (!inputText.trim()) return;

    setIsSending(true);
    setStatusMessage(null);

    try {
      await apiClient.setClipboard(inputText, burnAfterRead);
      setStatusMessage('Текст отправлен в память устройства!');
      setInputText('');
      setTimeout(() => setStatusMessage(null), 3000);
      await fetchClipboard();
    } catch (err: any) {
      setStatusMessage(err.message || 'Ошибка отправки в буфер');
    } finally {
      setIsSending(false);
    }
  };

  const handlePasteFromDevice = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        setInputText(text);
        setPasted(true);
        setTimeout(() => setPasted(false), 2000);
      }
    } catch {
      setStatusMessage('Разрешите доступ к буферу обмена в браузере');
      setTimeout(() => setStatusMessage(null), 3000);
    }
  };

  const handleCopyToDevice = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      setStatusMessage('Не удалось скопировать текст');
      setTimeout(() => setStatusMessage(null), 3000);
    }
  };

  const handleClearServer = async () => {
    try {
      await apiClient.clearClipboard();
      setRemoteItem(null);
      setStatusMessage('Буфер занулен в оперативной памяти');
      setTimeout(() => setStatusMessage(null), 3000);
    } catch (err: any) {
      setStatusMessage(err.message || 'Ошибка очистки буфера');
    }
  };

  const formatTime = (unixSec: number) => {
    if (!unixSec) return '';
    const date = new Date(unixSec * 1000);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  };

  return (
    <div className="p-5 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
        <div className="flex items-center space-x-3">
          <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 shrink-0">
            <Clipboard className="w-5 h-5" />
          </div>
          <div>
            <div className="flex items-center space-x-2">
              <h3 className="text-sm font-bold text-white">P2P Буфер обмена</h3>
              <span className="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-300 border border-emerald-500/20">
                RAM only
              </span>
            </div>
            <p className="text-[11px] text-slate-400">
              Мгновенная передача текста, паролей и ссылок без сохранения на диск
            </p>
          </div>
        </div>

        <button
          onClick={fetchClipboard}
          disabled={isLoading}
          className="self-end sm:self-auto flex items-center space-x-1.5 px-3 py-1.5 rounded-xl text-[11px] font-medium bg-slate-800/80 hover:bg-slate-700/80 text-slate-300 border border-slate-700/60 transition disabled:opacity-50"
          title="Обновить буфер с сервера"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin text-emerald-400' : ''}`} />
          <span>Проверить</span>
        </button>
      </div>

      {statusMessage && (
        <div className="p-3 rounded-xl bg-slate-800/80 border border-slate-700 text-xs text-emerald-300 flex items-center space-x-2">
          <AlertCircle className="w-4 h-4 shrink-0 text-emerald-400" />
          <span>{statusMessage}</span>
        </div>
      )}

      {/* Received Remote Clipboard */}
      {remoteItem && remoteItem.text && (
        <div className="p-4 rounded-xl bg-slate-950/80 border border-emerald-500/30 space-y-3">
          <div className="flex items-center justify-between text-xs">
            <div className="flex items-center space-x-2">
              <span className="font-semibold text-emerald-400 flex items-center space-x-1">
                <ShieldCheck className="w-3.5 h-3.5" />
                <span>Текст в общем буфере:</span>
              </span>
              {remoteItem.updatedAt > 0 && (
                <span className="text-[10px] text-slate-500">
                  ({formatTime(remoteItem.updatedAt)})
                </span>
              )}
            </div>

            {remoteItem.burnAfterRead && (
              <span className="flex items-center space-x-1 text-[10px] font-bold text-amber-400 bg-amber-500/10 px-2 py-0.5 rounded-full border border-amber-500/20">
                <Flame className="w-3 h-3 text-amber-400" />
                <span>Сжигается при чтении</span>
              </span>
            )}
          </div>

          <div className="p-3 rounded-lg bg-slate-900 border border-slate-800 text-xs font-mono text-slate-200 whitespace-pre-wrap break-all max-h-48 overflow-y-auto selection:bg-emerald-500/30">
            {remoteItem.text}
          </div>

          <div className="flex items-center justify-between pt-1">
            <button
              onClick={() => handleCopyToDevice(remoteItem.text)}
              className="flex items-center space-x-1.5 px-3.5 py-2 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20"
            >
              {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copied ? 'Скопировано в буфер устройства!' : 'Скопировать'}</span>
            </button>

            <button
              onClick={handleClearServer}
              className="flex items-center space-x-1 px-3 py-1.5 rounded-xl text-[11px] font-medium text-slate-400 hover:text-rose-400 hover:bg-rose-500/10 transition"
              title="Стереть и занулить в памяти"
            >
              <Trash2 className="w-3.5 h-3.5" />
              <span>Стереть из RAM</span>
            </button>
          </div>
        </div>
      )}

      {/* Input / Send Section */}
      <div className="space-y-3 pt-1">
        <div className="relative">
          <textarea
            value={inputText}
            onChange={(e) => setInputText(e.target.value)}
            placeholder="Вставьте ссылку, заметку, пароль или фрагмент кода для передачи..."
            rows={3}
            className="w-full p-3.5 rounded-xl bg-slate-950/60 border border-slate-800 text-xs text-slate-100 placeholder-slate-500 focus:outline-none focus:border-emerald-500/60 transition resize-none font-mono"
          />

          <div className="absolute right-2.5 bottom-3 flex items-center space-x-1.5">
            <button
              type="button"
              onClick={handlePasteFromDevice}
              className="px-2 py-1 rounded-lg bg-slate-800/90 hover:bg-slate-700 text-[10px] font-medium text-slate-300 transition border border-slate-700/50"
              title="Вставить текущий текст из буфера устройства"
            >
              {pasted ? 'Вставлено!' : 'Вставить из буфера'}
            </button>
          </div>
        </div>

        {/* Controls row */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-1">
          {/* Burn after read toggle */}
          <label className="flex items-center space-x-2.5 cursor-pointer select-none text-xs text-slate-300">
            <input
              type="checkbox"
              checked={burnAfterRead}
              onChange={(e) => setBurnAfterRead(e.target.checked)}
              className="sr-only peer"
            />
            <div className="w-8 h-4.5 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-slate-300 after:border after:rounded-full after:h-3.5 after:w-3.5 after:transition-all peer-checked:bg-amber-600 relative"></div>
            <span className="flex items-center space-x-1">
              <Flame className="w-3.5 h-3.5 text-amber-400" />
              <span>Сжечь после прочтения (Burn after read)</span>
            </span>
          </label>

          <button
            onClick={handleSend}
            disabled={isSending || !inputText.trim()}
            className="flex items-center justify-center space-x-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <CornerDownLeft className="w-3.5 h-3.5" />
            <span>{isSending ? 'Отправка...' : 'Отправить в буфер'}</span>
          </button>
        </div>
      </div>
    </div>
  );
};
