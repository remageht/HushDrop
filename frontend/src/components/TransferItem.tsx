import React from 'react';
import { ActiveTransfer } from '../types';
import { File, AlertTriangle, CheckCircle2, XCircle, X, Loader2 } from 'lucide-react';

interface TransferItemProps {
  transfer: ActiveTransfer;
  onCancel: (id: string) => void;
}

export const TransferItem: React.FC<TransferItemProps> = ({ transfer, onCancel }) => {
  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  const formatSpeed = (bytesPerSec: number): string => {
    return `${formatBytes(bytesPerSec)}/с`;
  };

  const formatEta = (seconds: number): string => {
    if (seconds <= 0) return 'завершение...';
    if (seconds < 60) return `осталось ${seconds} сек`;
    const min = Math.floor(seconds / 60);
    const sec = seconds % 60;
    return `осталось ${min} мин ${sec} сек`;
  };

  return (
    <div className="p-4 rounded-2xl bg-slate-900 border border-slate-800 shadow-md transition-all">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div className="flex items-center space-x-3 min-w-0">
          <div
            className={`w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0 ${
              transfer.isDangerous
                ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                : 'bg-slate-800 text-slate-300'
            }`}
          >
            {transfer.isDangerous ? (
              <AlertTriangle className="w-5 h-5 text-amber-400" />
            ) : (
              <File className="w-5 h-5" />
            )}
          </div>
          <div className="min-w-0">
            <p className="text-sm font-medium text-white truncate max-w-[220px] sm:max-w-md">
              {transfer.name}
            </p>
            <div className="flex items-center space-x-2 text-xs text-slate-400">
              <span>{formatBytes(transfer.size)}</span>
              {transfer.isDangerous && (
                <span className="text-[10px] text-amber-400 font-medium px-1.5 py-0.2 rounded bg-amber-950/60 border border-amber-800/60">
                  Исполняемый файл
                </span>
              )}
            </div>
          </div>
        </div>

        <div>
          {transfer.status === 'uploading' && (
            <button
              onClick={() => onCancel(transfer.id)}
              className="p-1.5 rounded-lg text-slate-400 hover:text-rose-400 hover:bg-slate-800 transition"
              title="Отменить передачу"
            >
              <X className="w-4 h-4" />
            </button>
          )}
          {transfer.status === 'completed' && (
            <div className="flex items-center space-x-1 text-xs text-emerald-400 font-medium">
              <CheckCircle2 className="w-4 h-4" />
              <span>Готово</span>
            </div>
          )}
          {transfer.status === 'error' && (
            <div className="flex items-center space-x-1 text-xs text-rose-400 font-medium">
              <XCircle className="w-4 h-4" />
              <span>Ошибка</span>
            </div>
          )}
          {transfer.status === 'cancelled' && (
            <span className="text-xs text-slate-500 font-medium">Отменено</span>
          )}
        </div>
      </div>

      {/* Progress Bar */}
      {transfer.status === 'uploading' && (
        <div className="space-y-1.5 mt-3">
          <div className="w-full bg-slate-950 h-2 rounded-full overflow-hidden border border-slate-800">
            <div
              className="bg-emerald-500 h-full rounded-full transition-all duration-300 ease-out"
              style={{ width: `${transfer.progressPercent}%` }}
            />
          </div>
          <div className="flex items-center justify-between text-[11px] text-slate-400 font-mono">
            <span>{transfer.progressPercent}%</span>
            <span>{formatSpeed(transfer.speedBytesPerSec)}</span>
            <span>{formatEta(transfer.etaSeconds)}</span>
          </div>
        </div>
      )}

      {transfer.status === 'pending' && (
        <div className="flex items-center space-x-2 text-xs text-slate-400 mt-2">
          <Loader2 className="w-3.5 h-3.5 animate-spin text-emerald-400" />
          <span>Подготовка чанков (4MB)...</span>
        </div>
      )}

      {transfer.errorMessage && (
        <p className="text-xs text-rose-400 mt-2">{transfer.errorMessage}</p>
      )}
    </div>
  );
};
