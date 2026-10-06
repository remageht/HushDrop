import React, { useState, useRef, useEffect } from 'react';
import { UploadCloud, FolderUp, FileUp, AlertTriangle, ShieldCheck } from 'lucide-react';
import { apiClient } from '../api/client';
import { subscribeFilesReady } from '../../../mobile/src/share';
import { ActiveTransfer } from '../types';
import { TransferItem } from '../components/TransferItem';
import { ClipboardCard } from '../components/ClipboardCard';

const dangerousExts = ['.exe', '.bat', '.cmd', '.ps1', '.sh', '.msi', '.vbs', '.com'];

export const SendPage: React.FC = () => {
  const [transfers, setTransfers] = useState<ActiveTransfer[]>([]);
  const [isDragging, setIsDragging] = useState(false);
  const [pendingWarningFiles, setPendingWarningFiles] = useState<File[] | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const cleanup = subscribeFilesReady((files) => {
      processFiles(files);
    });
    return cleanup;
  }, []);

  const isDangerous = (filename: string): boolean => {
    const lower = filename.toLowerCase();
    return dangerousExts.some((ext) => lower.endsWith(ext));
  };

  const processFiles = (fileList: FileList | File[]) => {
    const files = Array.from(fileList);
    if (files.length === 0) return;

    // Check if any files are potentially executable
    const hasDangerous = files.some((f) => isDangerous(f.name));
    if (hasDangerous) {
      setPendingWarningFiles(files);
      return;
    }

    startUploadQueue(files);
  };

  const startUploadQueue = (files: File[]) => {
    files.forEach((file) => {
      const id = `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
      const abortController = new AbortController();

      const newTransfer: ActiveTransfer = {
        id,
        file,
        name: file.name,
        size: file.size,
        isDangerous: isDangerous(file.name),
        totalChunks: Math.ceil(file.size / (4 * 1024 * 1024)) || 1,
        uploadedChunks: 0,
        progressPercent: 0,
        speedBytesPerSec: 0,
        etaSeconds: 0,
        status: 'uploading',
        abortController,
      };

      setTransfers((prev) => [newTransfer, ...prev]);

      // Upload via ApiClient
      apiClient
        .uploadFile(
          file,
          (percent, speed, eta) => {
            setTransfers((prev) =>
              prev.map((t) =>
                t.id === id
                  ? {
                      ...t,
                      progressPercent: percent,
                      speedBytesPerSec: speed,
                      etaSeconds: eta,
                    }
                  : t
              )
            );
          },
          abortController.signal
        )
        .then(() => {
          setTransfers((prev) =>
            prev.map((t) =>
              t.id === id ? { ...t, status: 'completed', progressPercent: 100 } : t
            )
          );
        })
        .catch((err) => {
          setTransfers((prev) =>
            prev.map((t) =>
              t.id === id
                ? {
                    ...t,
                    status: abortController.signal.aborted ? 'cancelled' : 'error',
                    errorMessage: err.message,
                  }
                : t
            )
          );
        });
    });
  };

  const handleCancel = (id: string) => {
    setTransfers((prev) =>
      prev.map((t) => {
        if (t.id === id) {
          t.abortController?.abort();
          return { ...t, status: 'cancelled' };
        }
        return t;
      })
    );
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer.files) {
      processFiles(e.dataTransfer.files);
    }
  };

  return (
    <div className="space-y-6">
      {/* Drag & Drop Zone */}
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setIsDragging(true);
        }}
        onDragLeave={() => setIsDragging(false)}
        onDrop={handleDrop}
        className={`border-2 border-dashed rounded-3xl p-8 text-center transition-all cursor-pointer relative overflow-hidden ${
          isDragging
            ? 'border-emerald-500 bg-emerald-500/10 scale-[1.01]'
            : 'border-slate-800 hover:border-slate-700 bg-slate-900/40 hover:bg-slate-900/70'
        }`}
        onClick={() => fileInputRef.current?.click()}
      >
        <div className="w-16 h-16 mx-auto mb-4 rounded-2xl bg-slate-800 flex items-center justify-center text-emerald-400 group-hover:scale-110 transition">
          <UploadCloud className="w-8 h-8" />
        </div>

        <h3 className="text-base font-semibold text-white mb-1">
          Перетащите файлы или папки сюда
        </h3>
        <p className="text-xs text-slate-400 mb-6 max-w-sm mx-auto">
          Чанковая передача (4MB), сквозной стриминг на диск. До 5GB на файл.
        </p>

        <div className="flex flex-wrap items-center justify-center gap-3" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            className="flex items-center space-x-2 px-4 py-2.5 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20"
          >
            <FileUp className="w-4 h-4" />
            <span>Выбрать файлы</span>
          </button>

          <button
            type="button"
            onClick={() => folderInputRef.current?.click()}
            className="flex items-center space-x-2 px-4 py-2.5 rounded-xl text-xs font-medium bg-slate-800 hover:bg-slate-700 text-slate-200 transition"
          >
            <FolderUp className="w-4 h-4" />
            <span>Выбрать папку</span>
          </button>
        </div>

        {/* Hidden inputs */}
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => e.target.files && processFiles(e.target.files)}
        />
        <input
          ref={folderInputRef}
          type="file"
          // @ts-expect-error webkitdirectory is standard in browser engines
          webkitdirectory=""
          multiple
          className="hidden"
          onChange={(e) => e.target.files && processFiles(e.target.files)}
        />
      </div>

      {/* Dangerous File Confirmation Modal */}
      {pendingWarningFiles && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="bg-slate-900 border border-amber-500/40 rounded-2xl p-6 max-w-md w-full shadow-2xl">
            <div className="flex items-center space-x-3 text-amber-400 mb-3">
              <AlertTriangle className="w-6 h-6 flex-shrink-0" />
              <h4 className="font-bold text-white text-base">Предупреждение о безопасности</h4>
            </div>
            <p className="text-xs text-slate-300 mb-4 leading-relaxed">
              Вы пытаетесь отправить исполняемый файл (.exe / .ps1 / .sh / .bat).
              Такие файлы требуют явного подтверждения на стороне получателя для предотвращения случайного запуска.
            </p>
            <div className="flex items-center justify-end space-x-3">
              <button
                onClick={() => setPendingWarningFiles(null)}
                className="px-4 py-2 rounded-xl text-xs font-medium text-slate-400 hover:text-white bg-slate-800"
              >
                Отмена
              </button>
              <button
                onClick={() => {
                  startUploadQueue(pendingWarningFiles);
                  setPendingWarningFiles(null);
                }}
                className="px-4 py-2 rounded-xl text-xs font-medium text-white bg-amber-600 hover:bg-amber-500 transition"
              >
                Отправить в любом случае
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Active Transfers Section */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400">
            Очередь передач ({transfers.length})
          </h4>
          {transfers.length > 0 && (
            <button
              onClick={() => setTransfers([])}
              className="text-xs text-slate-500 hover:text-slate-400"
            >
              Очистить список
            </button>
          )}
        </div>

        {transfers.length === 0 ? (
          <div className="p-8 rounded-2xl bg-slate-900/30 border border-slate-800/60 text-center">
            <ShieldCheck className="w-8 h-8 text-slate-600 mx-auto mb-2" />
            <p className="text-xs text-slate-500">Нет активных передач</p>
          </div>
        ) : (
          <div className="space-y-3">
            {transfers.map((item) => (
              <TransferItem key={item.id} transfer={item} onCancel={handleCancel} />
            ))}
          </div>
        )}
      </div>

      <ClipboardCard />
    </div>
  );
};
