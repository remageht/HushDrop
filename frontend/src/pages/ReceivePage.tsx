import React, { useState, useEffect } from 'react';
import { Download, File, AlertTriangle, RefreshCw, Inbox } from 'lucide-react';
import { apiClient } from '../api/client';
import { FileItem } from '../types';

export const ReceivePage: React.FC = () => {
  const [files, setFiles] = useState<FileItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [confirmDangerousFile, setConfirmDangerousFile] = useState<FileItem | null>(null);

  const fetchFiles = async () => {
    setIsLoading(true);
    setErrorMessage(null);
    try {
      const items = await apiClient.listFiles();
      setFiles(items);
    } catch (err: any) {
      setErrorMessage(err.message || 'Не удалось загрузить список файлов');
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchFiles();
    const interval = setInterval(fetchFiles, 5000); // Poll for incoming files every 5s
    return () => clearInterval(interval);
  }, []);

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  const handleDownloadClick = (file: FileItem) => {
    if (file.isDangerous) {
      setConfirmDangerousFile(file);
      return;
    }
    executeDownload(file);
  };

  const executeDownload = async (file: FileItem) => {
    try {
      await apiClient.downloadFile(file.id, file.cleanName);
    } catch (err: any) {
      alert(`Ошибка загрузки: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-base font-semibold text-white">Полученные файлы</h3>
          <p className="text-xs text-slate-400">
            Файлы, переданные в этой сессии напрямую в ./data/downloads
          </p>
        </div>

        <button
          onClick={fetchFiles}
          disabled={isLoading}
          className="p-2 rounded-xl bg-slate-900 border border-slate-800 text-slate-400 hover:text-white transition disabled:opacity-50"
          title="Обновить список"
        >
          <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin text-emerald-400' : ''}`} />
        </button>
      </div>

      {errorMessage && (
        <div className="p-4 rounded-2xl bg-rose-500/10 border border-rose-500/30 text-rose-400 text-xs">
          {errorMessage}
        </div>
      )}

      {isLoading && files.length === 0 ? (
        <div className="p-12 rounded-3xl bg-slate-900/40 border border-slate-800 text-center">
          <RefreshCw className="w-8 h-8 text-emerald-400 animate-spin mx-auto mb-3" />
          <p className="text-xs text-slate-400">Загрузка файлов...</p>
        </div>
      ) : files.length === 0 ? (
        <div className="p-12 rounded-3xl bg-slate-900/40 border border-slate-800 text-center">
          <Inbox className="w-10 h-10 text-slate-600 mx-auto mb-3" />
          <h4 className="text-sm font-medium text-slate-300 mb-1">Файлы еще не получены</h4>
          <p className="text-xs text-slate-500 max-w-xs mx-auto">
            Отправьте файлы со смартфона или ПК в той же локальной сети.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {files.map((file) => (
            <div
              key={file.id}
              className="p-4 rounded-2xl bg-slate-900 border border-slate-800 flex items-center justify-between gap-4 hover:border-slate-700 transition"
            >
              <div className="flex items-center space-x-3.5 min-w-0">
                <div
                  className={`w-11 h-11 rounded-xl flex items-center justify-center flex-shrink-0 ${
                    file.isDangerous
                      ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                      : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                  }`}
                >
                  {file.isDangerous ? (
                    <AlertTriangle className="w-5 h-5 text-amber-400" />
                  ) : (
                    <File className="w-5 h-5 text-emerald-400" />
                  )}
                </div>

                <div className="min-w-0">
                  <p className="text-sm font-medium text-white truncate max-w-[200px] sm:max-w-md">
                    {file.cleanName}
                  </p>
                  <div className="flex items-center space-x-2 text-xs text-slate-400 mt-0.5">
                    <span>{formatBytes(file.size)}</span>
                    <span>•</span>
                    <span className="font-mono text-[10px] text-slate-500 truncate max-w-[120px]">
                      SHA256: {file.sha256.substring(0, 10)}...
                    </span>
                    {file.isDangerous && (
                      <span className="text-[10px] text-amber-400 font-semibold px-1.5 py-0.2 rounded bg-amber-950/60 border border-amber-800/60">
                        Опасно
                      </span>
                    )}
                  </div>
                </div>
              </div>

              <button
                onClick={() => handleDownloadClick(file)}
                className="flex items-center space-x-2 px-3.5 py-2 rounded-xl text-xs font-medium bg-slate-800 hover:bg-emerald-600 text-slate-200 hover:text-white transition shadow"
              >
                <Download className="w-3.5 h-3.5" />
                <span className="hidden sm:inline">Скачать</span>
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Dangerous File Confirmation */}
      {confirmDangerousFile && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="bg-slate-900 border border-amber-500/40 rounded-2xl p-6 max-w-md w-full shadow-2xl">
            <div className="flex items-center space-x-3 text-amber-400 mb-3">
              <AlertTriangle className="w-6 h-6 flex-shrink-0" />
              <h4 className="font-bold text-white text-base">Подтверждение скачивания</h4>
            </div>
            <p className="text-xs text-slate-300 mb-4 leading-relaxed">
              Файл <strong className="text-white">{confirmDangerousFile.cleanName}</strong> является исполняемым или скриптом.
              Убедитесь, что вы доверяете отправителю, перед его сохранением и запуском.
            </p>
            <div className="flex items-center justify-end space-x-3">
              <button
                onClick={() => setConfirmDangerousFile(null)}
                className="px-4 py-2 rounded-xl text-xs font-medium text-slate-400 hover:text-white bg-slate-800"
              >
                Отмена
              </button>
              <button
                onClick={() => {
                  const f = confirmDangerousFile;
                  setConfirmDangerousFile(null);
                  executeDownload(f);
                }}
                className="px-4 py-2 rounded-xl text-xs font-medium text-white bg-amber-600 hover:bg-amber-500 transition"
              >
                Скачать файл
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
