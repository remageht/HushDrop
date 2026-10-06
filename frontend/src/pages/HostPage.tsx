import React, { useState, useEffect, useRef } from 'react';
import QRCode from 'qrcode';
import {
  Radio,
  Power,
  Copy,
  Check,
  ShieldCheck,
  Clock,
  Users,
  FileText,
  AlertTriangle,
  Upload,
  RefreshCw,
  ExternalLink,
  Smartphone,
  DownloadCloud
} from 'lucide-react';
import {
  HostStatus,
  NetworkInfo,
  isNativeHostSupported,
  startNativeHost,
  stopNativeHost,
  getNativeHostStatus,
  getNativeNetworkInfo
} from '../api/host';
import { apiClient } from '../api/client';
import { FileItem } from '../types';

export const HostPage: React.FC = () => {
  const [isSupported, setIsSupported] = useState<boolean>(true);
  const [hostStatus, setHostStatus] = useState<HostStatus>({ isRunning: false });
  const [networkInfo, setNetworkInfo] = useState<NetworkInfo>({ ips: [], selectedIp: '' });
  const [qrCodeDataUrl, setQrCodeDataUrl] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [hostedFiles, setHostedFiles] = useState<FileItem[]>([]);
  const [uploadingHosted, setUploadingHosted] = useState<boolean>(false);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const pollTimerRef = useRef<number | null>(null);
  const isRunningRef = useRef<boolean>(false);

  useEffect(() => {
    isRunningRef.current = hostStatus.isRunning;
  }, [hostStatus.isRunning]);

  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.hidden) {
        stopPolling();
      } else {
        if (isRunningRef.current) {
          refreshStatus();
          startPolling();
        }
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

  useEffect(() => {
    const supported = isNativeHostSupported();
    setIsSupported(supported);

    if (supported) {
      loadInitialStatus();
    }

    return () => {
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current);
      }
    };
  }, []);

  // Update QR Code when qrUrl changes
  useEffect(() => {
    if (hostStatus.qrUrl) {
      QRCode.toDataURL(hostStatus.qrUrl, {
        width: 256,
        margin: 2,
        color: {
          dark: '#0f172a',
          light: '#ffffff'
        }
      })
        .then((url) => setQrCodeDataUrl(url))
        .catch(() => setQrCodeDataUrl(''));
    } else {
      setQrCodeDataUrl('');
    }
  }, [hostStatus.qrUrl]);

  // Countdown timer for PIN expiration
  useEffect(() => {
    if (!hostStatus.isRunning || !hostStatus.expiresIn || hostStatus.expiresIn <= 0) return;

    const timer = setInterval(() => {
      setHostStatus((prev) => {
        if (!prev.expiresIn || prev.expiresIn <= 1) {
          // Trigger refresh if expired
          refreshStatus();
          return { ...prev, expiresIn: 0 };
        }
        return { ...prev, expiresIn: prev.expiresIn - 1 };
      });
    }, 1000);

    return () => clearInterval(timer);
  }, [hostStatus.isRunning, hostStatus.expiresIn]);

  const loadInitialStatus = async () => {
    try {
      const [status, net] = await Promise.all([
        getNativeHostStatus(),
        getNativeNetworkInfo()
      ]);
      setHostStatus(status);
      setNetworkInfo(net);

      if (status.isRunning) {
        startPolling();
        if (apiClient.isAuthenticated()) {
          fetchHostedFiles();
        }
      }
    } catch (err: any) {
      // Ignore background fetch error
    }
  };

  const startPolling = () => {
    if (pollTimerRef.current) clearInterval(pollTimerRef.current);
    pollTimerRef.current = window.setInterval(refreshStatus, 8000);
  };

  const stopPolling = () => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  };

  const refreshStatus = async () => {
    try {
      const status = await getNativeHostStatus();
      setHostStatus(status);
      isRunningRef.current = status.isRunning;
      if (status.isRunning) {
        if (apiClient.isAuthenticated()) {
          fetchHostedFiles();
        }
      } else {
        stopPolling();
      }
    } catch {
      // background poll error
    }
  };

  const fetchHostedFiles = async () => {
    if (!apiClient.isAuthenticated()) return;
    try {
      const files = await apiClient.listFiles();
      setHostedFiles(files);
    } catch {
      // ignore
    }
  };

  const handleStartHost = async () => {
    setLoading(true);
    setError(null);
    try {
      const status = await startNativeHost();
      setHostStatus(status);
      startPolling();
    } catch (err: any) {
      setError(err.message || 'Не удалось запустить мобильный хост-сервер');
    } finally {
      setLoading(false);
    }
  };

  const handleStopHost = async () => {
    setLoading(true);
    setError(null);
    try {
      await stopNativeHost();
      setHostStatus({ isRunning: false });
      stopPolling();
      setHostedFiles([]);
    } catch (err: any) {
      setError(err.message || 'Не удалось остановить хост-сервер');
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2500);
  };

  const formatExpires = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  };

  const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    setUploadingHosted(true);
    setError(null);

    try {
      const file = files[0];
      // When hosting on mobile, upload directly to local server session
      await apiClient.uploadFile(file, () => {});
      // Refresh files list
      await fetchHostedFiles();
    } catch (err: any) {
      setError(err.message || 'Не удалось добавить файл в раздачу');
    } finally {
      setUploadingHosted(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  return (
    <div className="space-y-6">
      {/* Header card */}
      <div className="p-6 rounded-2xl bg-slate-900/60 border border-slate-800 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div className="flex items-center space-x-4">
          <div
            className={`w-14 h-14 rounded-2xl flex items-center justify-center shrink-0 ${
              hostStatus.isRunning
                ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                : 'bg-slate-800/80 text-slate-400 border border-slate-700/60'
            }`}
          >
            <Radio className="w-7 h-7" />
          </div>
          <div>
            <div className="flex items-center space-x-2">
              <h2 className="text-lg font-bold text-white">Мобильный хост (Телефон → Телефон)</h2>
              <span
                className={`text-[10px] font-semibold px-2.5 py-0.5 rounded-full ${
                  hostStatus.isRunning
                    ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/30'
                    : 'bg-slate-800 text-slate-400 border border-slate-700'
                }`}
              >
                {hostStatus.isRunning ? 'Раздача активна' : 'Остановлен'}
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-1">
              Раздавайте файлы напрямую со смартфона через точку доступа Wi-Fi без ПК и интернета.
            </p>
          </div>
        </div>

        <div className="flex items-center space-x-3">
          {hostStatus.isRunning ? (
            <button
              onClick={handleStopHost}
              disabled={loading}
              className="flex items-center space-x-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-rose-600 hover:bg-rose-500 text-white transition shadow-lg shadow-rose-600/20 disabled:opacity-50"
            >
              <Power className="w-4 h-4" />
              <span>Остановить хост</span>
            </button>
          ) : (
            <button
              onClick={handleStartHost}
              disabled={loading || !isSupported}
              className="flex items-center space-x-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20 disabled:opacity-50"
            >
              <Power className="w-4 h-4" />
              <span>{loading ? 'Запуск...' : 'Запустить хост'}</span>
            </button>
          )}
        </div>
      </div>

      {error && (
        <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/20 text-xs text-rose-300 flex items-start space-x-3">
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
          <span>{error}</span>
        </div>
      )}

      {!isSupported && (
        <div className="p-5 rounded-2xl bg-amber-500/10 border border-amber-500/20 text-xs text-amber-300 space-y-2">
          <div className="flex items-center space-x-2 font-bold">
            <Smartphone className="w-4 h-4 text-amber-400" />
            <span>Режим автономного мобильного хоста</span>
          </div>
          <p className="text-slate-300 leading-relaxed">
            Встроенный HTTPS-сервер с самоподписанным TLS 1.3 сертификатом и фоновой службой
            активируется в приложении HushDrop для <b>Android</b>. При открытии в браузере подключение
            к уже работающему серверу на ПК или телефоне выполняется через вкладку «Меню» или «Отправить».
          </p>
        </div>
      )}

      {/* Host details when running */}
      {hostStatus.isRunning && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* QR Code and Connection Info */}
          <div className="p-6 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold uppercase tracking-wider text-slate-400">
                Подключение по QR-коду
              </span>
              <span className="text-xs text-emerald-400 flex items-center space-x-1">
                <ShieldCheck className="w-4 h-4" />
                <span>TLS 1.3 ECDSA</span>
              </span>
            </div>

            {qrCodeDataUrl ? (
              <div className="flex flex-col items-center justify-center p-4 bg-white rounded-2xl shadow-xl w-60 h-60 mx-auto">
                <img
                  src={qrCodeDataUrl}
                  alt="HushDrop Host QR"
                  className="w-52 h-52 object-contain"
                />
              </div>
            ) : (
              <div className="w-60 h-60 mx-auto flex items-center justify-center bg-slate-950 rounded-2xl border border-slate-800">
                <RefreshCw className="w-6 h-6 text-slate-500 animate-spin" />
              </div>
            )}

            <div className="text-center space-y-1">
              <p className="text-xs text-slate-400">
                Наведите камеру смартфона-клиента на QR-код для быстрого входа
              </p>
              {hostStatus.qrUrl && (
                <button
                  onClick={() => copyToClipboard(hostStatus.qrUrl!, 'qrUrl')}
                  className="inline-flex items-center space-x-1.5 text-xs text-emerald-400 hover:text-emerald-300 pt-1"
                >
                  {copiedKey === 'qrUrl' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                  <span>{copiedKey === 'qrUrl' ? 'Ссылка скопирована' : 'Скопировать ссылку сопряжения'}</span>
                </button>
              )}
            </div>
          </div>

          {/* Security Credentials: PIN & Fingerprint */}
          <div className="p-6 rounded-2xl bg-slate-900/60 border border-slate-800 flex flex-col justify-between space-y-6">
            <div className="space-y-4">
              <span className="text-xs font-bold uppercase tracking-wider text-slate-400">
                Данные авторизации
              </span>

              {/* PIN Code Box */}
              <div className="p-4 rounded-xl bg-slate-950/80 border border-slate-800 space-y-2">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span>Одноразовый PIN-код:</span>
                  {hostStatus.expiresIn !== undefined && (
                    <span className="flex items-center space-x-1 text-amber-400">
                      <Clock className="w-3.5 h-3.5" />
                      <span>{formatExpires(hostStatus.expiresIn)}</span>
                    </span>
                  )}
                </div>
                <div className="flex items-center justify-between">
                  <span className="font-mono text-3xl font-extrabold tracking-widest text-emerald-400">
                    {hostStatus.pin || '------'}
                  </span>
                  <button
                    onClick={() => copyToClipboard(hostStatus.pin || '', 'pin')}
                    className="p-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 transition"
                    title="Скопировать PIN"
                  >
                    {copiedKey === 'pin' ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              {/* Host IP and Port */}
              <div className="p-3.5 rounded-xl bg-slate-950/60 border border-slate-800 text-xs space-y-1.5">
                <div className="flex items-center justify-between text-slate-400">
                  <span>Адрес хоста:</span>
                  <span className="font-mono text-slate-200">
                    https://{hostStatus.ip}:{hostStatus.port}
                  </span>
                </div>
                {networkInfo.ips.length > 1 && (
                  <div className="text-[10px] text-slate-500">
                    Другие IP: {networkInfo.ips.filter((ip) => ip !== hostStatus.ip).join(', ')}
                  </div>
                )}
                <div className="flex items-center justify-between text-slate-400">
                  <span>Активные сессии:</span>
                  <span className="flex items-center space-x-1 text-slate-200">
                    <Users className="w-3 h-3 text-slate-400" />
                    <span>{hostStatus.activeSessions ?? 0}</span>
                  </span>
                </div>
              </div>

              {/* TLS Fingerprint */}
              <div className="p-3.5 rounded-xl bg-slate-950/60 border border-slate-800 text-xs space-y-1.5">
                <div className="flex items-center justify-between text-slate-400">
                  <span>SHA-256 Fingerprint:</span>
                  <button
                    onClick={() => copyToClipboard(hostStatus.fingerprint || '', 'fp')}
                    className="text-emerald-400 hover:text-emerald-300"
                  >
                    {copiedKey === 'fp' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <p className="font-mono text-[10px] text-slate-300 break-all select-all leading-relaxed">
                  {hostStatus.fingerprint || '...'}
                </p>
              </div>

              {/* Certificate Helper link */}
              <div className="pt-1">
                <a
                  href={`http://${hostStatus.ip}:${hostStatus.httpPort || 8080}/cert`}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center justify-center space-x-2 py-2 px-3 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs text-slate-300 transition"
                >
                  <DownloadCloud className="w-4 h-4 text-sky-400" />
                  <span>Скачать сертификат доверия (hushdrop.crt)</span>
                  <ExternalLink className="w-3 h-3 text-slate-500" />
                </a>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Hosted Files Section */}
      {hostStatus.isRunning && (
        <div className="p-6 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-sm font-bold text-white flex items-center space-x-2">
                <FileText className="w-4 h-4 text-emerald-400" />
                <span>Файлы для раздачи ({hostedFiles.length})</span>
              </h3>
              <p className="text-xs text-slate-400 mt-0.5">
                Клиенты смогут скачать эти файлы сразу после подключения
              </p>
            </div>
            <div>
              <input
                ref={fileInputRef}
                type="file"
                className="hidden"
                onChange={handleFileSelect}
              />
              <button
                onClick={() => fileInputRef.current?.click()}
                disabled={uploadingHosted}
                className="flex items-center space-x-2 px-4 py-2 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20 disabled:opacity-50"
              >
                <Upload className="w-3.5 h-3.5" />
                <span>{uploadingHosted ? 'Добавление...' : 'Добавить файл'}</span>
              </button>
            </div>
          </div>

          {hostedFiles.length === 0 ? (
            <div className="p-8 rounded-xl bg-slate-950/40 border border-dashed border-slate-800 text-center">
              <Upload className="w-8 h-8 text-slate-600 mx-auto mb-2" />
              <p className="text-xs text-slate-400">
                Пока нет файлов для раздачи. Нажмите «Добавить файл» чтобы поделиться.
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {hostedFiles.map((file) => (
                <div
                  key={file.id}
                  className="p-3.5 rounded-xl bg-slate-950/60 border border-slate-800/80 flex items-center justify-between"
                >
                  <div className="flex items-center space-x-3 overflow-hidden">
                    <FileText className="w-5 h-5 text-slate-400 shrink-0" />
                    <div className="overflow-hidden">
                      <p className="text-xs font-semibold text-slate-200 truncate">
                        {file.cleanName}
                      </p>
                      <p className="text-[10px] text-slate-500">
                        {(file.size / 1024 / 1024).toFixed(2)} MB • SHA-256: {file.sha256 ? file.sha256.slice(0, 8) : '...'}...
                      </p>
                    </div>
                  </div>
                  <span className="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                    Доступен клиентам
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Hotspot Instructions Card */}
      <div className="p-6 rounded-2xl bg-slate-900/30 border border-slate-800/60 text-xs text-slate-400 space-y-4">
        <p className="font-semibold text-slate-200 text-sm">
          Инструкция: как раздать файлы с телефона без ПК
        </p>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-1">
          <div className="p-4 rounded-xl bg-slate-950/50 border border-slate-800/60 space-y-2">
            <span className="inline-block px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300 font-bold text-[10px]">
              ШАГ 1
            </span>
            <p className="font-bold text-slate-200">Точка доступа или Wi-Fi</p>
            <p className="text-[11px] leading-relaxed text-slate-400">
              Включите «Точку доступа Wi-Fi» (Hotspot) на телефоне-раздатчике или подключите оба
              телефона к одной Wi-Fi сети.
            </p>
          </div>

          <div className="p-4 rounded-xl bg-slate-950/50 border border-slate-800/60 space-y-2">
            <span className="inline-block px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300 font-bold text-[10px]">
              ШАГ 2
            </span>
            <p className="font-bold text-slate-200">Запуск хоста</p>
            <p className="text-[11px] leading-relaxed text-slate-400">
              Нажмите кнопку «Запустить хост». Телефон сгенерирует TLS 1.3 сертификат, PIN-код и
              QR-код сопряжения.
            </p>
          </div>

          <div className="p-4 rounded-xl bg-slate-950/50 border border-slate-800/60 space-y-2">
            <span className="inline-block px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300 font-bold text-[10px]">
              ШАГ 3
            </span>
            <p className="font-bold text-slate-200">Подключение клиента</p>
            <p className="text-[11px] leading-relaxed text-slate-400">
              Второй телефон сканирует QR-код камерой или переходит по ссылке и подтверждает 6-значный
              PIN. Готово!
            </p>
          </div>
        </div>
      </div>
    </div>
  );
};
