import React, { useState } from 'react';
import { ShieldCheck, Trash2, Wifi, AlertTriangle } from 'lucide-react';
import { apiClient } from '../api/client';

interface HeaderProps {
  onRevoke: () => void;
  isAuthenticated: boolean;
}

export const Header: React.FC<HeaderProps> = ({ onRevoke, isAuthenticated }) => {
  const [showConfirm, setShowConfirm] = useState(false);
  const fingerprint = apiClient.getFingerprint();

  const handleRevoke = async () => {
    try {
      await apiClient.revokeAll();
      setShowConfirm(false);
      onRevoke();
    } catch {
      // ignore
    }
  };

  return (
    <>
      <header className="border-b border-slate-800 bg-slate-900/60 backdrop-blur-md sticky top-0 z-40 px-4 py-3">
        <div className="max-w-4xl mx-auto flex items-center justify-between">
          <div className="flex items-center space-x-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-emerald-500 to-teal-700 flex items-center justify-center shadow-lg shadow-emerald-500/20">
              <ShieldCheck className="w-6 h-6 text-white" />
            </div>
            <div>
              <div className="flex items-center space-x-2">
                <h1 className="font-bold text-lg text-white tracking-wide">HushDrop</h1>
                <span className="text-[10px] font-medium uppercase px-2 py-0.5 rounded-full bg-emerald-950/80 text-emerald-400 border border-emerald-800">
                  P2P LAN
                </span>
              </div>
              <p className="text-xs text-slate-400 hidden sm:block">
                Твои файлы. Твоя сеть. Никого лишнего.
              </p>
            </div>
          </div>

          <div className="flex items-center space-x-3">
            {isAuthenticated && (
              <>
                <div className="hidden md:flex items-center space-x-1.5 text-xs text-emerald-400 bg-emerald-950/40 border border-emerald-900/60 px-2.5 py-1 rounded-lg">
                  <Wifi className="w-3.5 h-3.5" />
                  <span>Шифрование TLS 1.3 (PFS)</span>
                </div>

                <button
                  onClick={() => setShowConfirm(true)}
                  title="Забыть всё: уничтожить сессии, историю и ключи в RAM"
                  className="flex items-center space-x-1.5 px-3 py-1.5 rounded-lg bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/30 text-xs font-medium transition-colors"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  <span className="hidden sm:inline">Забыть всё</span>
                </button>
              </>
            )}
          </div>
        </div>

        {fingerprint && isAuthenticated && (
          <div className="max-w-4xl mx-auto mt-2 pt-2 border-t border-slate-800/60 flex items-center justify-between text-[11px] text-slate-500">
            <span className="truncate">
              Отпечаток TLS (SHA-256): <span className="font-mono text-slate-400">{fingerprint}</span>
            </span>
          </div>
        )}
      </header>

      {/* Confirmation Modal */}
      {showConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm animate-in fade-in">
          <div className="bg-slate-900 border border-rose-500/40 rounded-2xl p-6 max-w-md w-full shadow-2xl">
            <div className="flex items-center space-x-3 text-rose-400 mb-4">
              <AlertTriangle className="w-6 h-6 flex-shrink-0" />
              <h3 className="font-bold text-lg text-white">Уничтожить все данные?</h3>
            </div>
            <p className="text-sm text-slate-300 mb-6">
              Режим <strong className="text-rose-300">«Забыть всё»</strong> мгновенно инвалидирует все токены, обнуляет ключи шифрования в RAM и удаляет переданные временные файлы.
            </p>
            <div className="flex items-center justify-end space-x-3">
              <button
                onClick={() => setShowConfirm(false)}
                className="px-4 py-2 rounded-xl text-xs font-semibold text-slate-400 hover:text-white bg-slate-800 hover:bg-slate-700 transition"
              >
                Отмена
              </button>
              <button
                onClick={handleRevoke}
                className="px-4 py-2 rounded-xl text-xs font-semibold text-white bg-rose-600 hover:bg-rose-500 transition shadow-lg shadow-rose-600/30"
              >
                Да, стереть всё
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
};
