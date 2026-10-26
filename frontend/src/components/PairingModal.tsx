import React, { useState, useEffect } from 'react';
import { KeyRound, ShieldAlert, ShieldCheck, Loader2, QrCode, X, Radio } from 'lucide-react';
import { apiClient } from '../api/client';
import { PairInfoResponse } from '../types';

interface PairingModalProps {
  onSuccess: () => void;
  onClose?: () => void;
  targetHost?: string;
  /** Pairing token from a native deep link; window.location.search is empty inside the app WebView. */
  initialToken?: string;
  /** Expected TLS fingerprint from a native deep link. */
  initialFp?: string;
}

export const PairingModal: React.FC<PairingModalProps> = ({ onSuccess, onClose, targetHost, initialToken, initialFp }) => {
  const [pin, setPin] = useState('');
  const [token, setToken] = useState('');
  const [urlFp, setUrlFp] = useState<string | null>(null);
  const [serverInfo, setServerInfo] = useState<PairInfoResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isFetchingInfo, setIsFetchingInfo] = useState(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    if (targetHost) {
      apiClient.setServerBaseUrl(targetHost);
    }

    // Token/fingerprint, in priority order:
    // 1. initialToken/initialFp — native deep link (hushdrop://pair) handled by App.tsx
    // 2. window.location.search — PWA opened in a browser by scanning the QR code
    const params = new URLSearchParams(window.location.search);
    const paramToken = initialToken || params.get('token');
    const paramFp = initialFp || params.get('fp');

    if (paramToken) setToken(paramToken);
    if (paramFp) setUrlFp(paramFp);

    // Strip pairing secrets from the address bar and browser history
    // (browser QR flow only — deep-link tokens never touch the address bar)
    if (!initialToken && (paramToken || paramFp)) {
      params.delete('token');
      params.delete('fp');
      const rest = params.toString();
      window.history.replaceState(
        {},
        '',
        `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`
      );
    }

    // Fetch pair info from server to verify availability
    apiClient
      .getPairInfo()
      .then((info) => {
        if (cancelled) return;
        setServerInfo(info);
        if (!paramToken && info.token) {
          setToken(info.token);
        }
      })
      .catch((err) => {
        if (cancelled) return;
        setErrorMessage(err.message || 'Не удалось подключиться к узлу');
      })
      .finally(() => {
        if (!cancelled) setIsFetchingInfo(false);
      });

    return () => {
      cancelled = true;
    };
  }, [targetHost]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (pin.length !== 6) {
      setErrorMessage('Введите 6-значный PIN-код');
      return;
    }

    setIsLoading(true);
    setErrorMessage(null);

    try {
      await apiClient.pair(pin, token);
      onSuccess();
    } catch (err: any) {
      setErrorMessage(err.message || 'Ошибка сопряжения');
    } finally {
      setIsLoading(false);
    }
  };

  const isFpMatched = urlFp && serverInfo?.fingerprint && urlFp === serverInfo.fingerprint;
  const shortFp = (fp: string) => {
    const groups = fp.split(':');
    if (groups.length < 8) return fp;
    return `${groups.slice(0, 4).join(':')}…${groups.slice(-4).join(':')}`;
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/85 backdrop-blur-md">
      <div className="bg-slate-900 border border-slate-800 rounded-3xl p-6 sm:p-8 max-w-md w-full shadow-2xl relative overflow-hidden">
        {/* Close Button */}
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            aria-label="Закрыть"
            className="absolute top-4 right-4 z-10 p-2 text-slate-400 hover:text-white rounded-xl hover:bg-slate-800/80 transition"
          >
            <X className="w-5 h-5" />
          </button>
        )}

        {/* Glow accent */}
        <div className="absolute top-0 left-1/2 -translate-x-1/2 w-64 h-24 bg-emerald-500/10 blur-3xl pointer-events-none" />

        <div className="text-center mb-6">
          <div className="w-14 h-14 mx-auto mb-3 rounded-2xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400">
            <KeyRound className="w-7 h-7" />
          </div>
          <h2 className="text-xl font-bold text-white">Сопряжение устройств</h2>
          <p className="text-xs text-slate-400 mt-1">
            Введите PIN-код, отображаемый на экране второго узла
          </p>
          {targetHost && (
            <div className="mt-2.5 inline-flex items-center space-x-1.5 px-3 py-1 rounded-full bg-slate-800/80 border border-slate-700/80 text-[11px] text-emerald-400 font-mono">
              <Radio className="w-3 h-3 animate-pulse" />
              <span>Узел: {targetHost.replace(/^https?:\/\//, '')}</span>
            </div>
          )}
        </div>

        {/* TLS Fingerprint verification */}
        {serverInfo && (
          <div className="mb-6 p-3.5 rounded-xl bg-slate-950/60 border border-slate-800 text-xs">
            <div className="flex items-center justify-between mb-1.5">
              <span className="text-slate-400 font-medium">Отпечаток TLS 1.3:</span>
              {isFpMatched ? (
                <span className="flex items-center text-emerald-400 font-medium text-[11px]">
                  <ShieldCheck className="w-3.5 h-3.5 mr-1" /> Сверен с QR
                </span>
              ) : (
                <span className="flex items-center text-amber-400 font-medium text-[11px]">
                  <ShieldAlert className="w-3.5 h-3.5 mr-1" /> Сверьте вручную
                </span>
              )}
            </div>
            <p className="font-mono text-[11px] text-slate-300 break-all bg-slate-900/90 p-2 rounded-lg border border-slate-800/80">
              {serverInfo.fingerprint}
            </p>
            {!isFpMatched && (
              <p className="mt-2 text-[11px] leading-relaxed text-amber-400/90">
                Сравните с экраном {targetHost ? 'найденного узла' : 'второго устройства'} — достаточно первые и последние 4 группы:{' '}
                <span className="font-mono text-amber-300">{shortFp(serverInfo.fingerprint)}</span>.
                Совпало — вводите PIN.
              </p>
            )}
          </div>
        )}

        {errorMessage && (
          <div className="mb-5 p-3 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-400 text-xs flex items-center space-x-2">
            <ShieldAlert className="w-4 h-4 flex-shrink-0" />
            <span>{errorMessage}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider mb-2 text-center">
              6-значный PIN
            </label>
            <input
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={6}
              value={pin}
              onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
              placeholder="••••••"
              disabled={isLoading || isFetchingInfo}
              autoFocus
              className="w-full text-center text-3xl tracking-[0.5em] font-mono py-3.5 px-4 rounded-xl bg-slate-950 border border-slate-700 text-white placeholder-slate-600 focus:outline-none focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500 transition disabled:opacity-50"
            />
          </div>

          <button
            type="submit"
            disabled={isLoading || isFetchingInfo || pin.length !== 6}
            className="w-full py-3.5 px-4 rounded-xl font-medium text-sm text-white bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 disabled:pointer-events-none transition shadow-lg shadow-emerald-600/20 flex items-center justify-center space-x-2"
          >
            {isLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                <span>Проверка PIN...</span>
              </>
            ) : (
              <span>Подключиться к сессии</span>
            )}
          </button>
        </form>

        <div className="mt-6 pt-4 border-t border-slate-800/80 flex items-center justify-center space-x-2 text-[11px] text-slate-500">
          <QrCode className="w-3.5 h-3.5" />
          <span>Передача строго в локальной сети (LAN Only)</span>
        </div>
      </div>
    </div>
  );
};
