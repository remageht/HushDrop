import React, { useState, useEffect } from 'react';
import { Header } from './components/Header';
import { PairingModal } from './components/PairingModal';
import { SendPage } from './pages/SendPage';
import { ReceivePage } from './pages/ReceivePage';
import { HostPage } from './pages/HostPage';
import { ClipboardCard } from './components/ClipboardCard';
import { apiClient } from './api/client';
import { Send, Download, WifiOff, KeyRound, ShieldCheck, Home, Radio } from 'lucide-react';

export const App: React.FC = () => {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(apiClient.isAuthenticated());
  const [activeTab, setActiveTab] = useState<'home' | 'send' | 'receive' | 'host'>('home');
  const [showPairing, setShowPairing] = useState(false);
  const [isOffline, setIsOffline] = useState(!navigator.onLine);

  useEffect(() => {
    const unsub = apiClient.subscribeAuth((authed) => {
      setIsAuthenticated(authed);
    });

    const handleOnline = () => setIsOffline(false);
    const handleOffline = () => setIsOffline(true);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

    // Auto-open pairing modal if token query parameter is provided (e.g. from QR scan)
    const params = new URLSearchParams(window.location.search);
    if (params.get('token')) {
      setShowPairing(true);
    }
    if (params.get('tab') === 'host') {
      setActiveTab('host');
    }

    // Register service worker if supported
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch(() => {
        // SW registration fallback
      });
    }

    return () => {
      unsub();
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  return (
    <div className="min-h-screen bg-[#090d16] text-slate-100 flex flex-col font-sans">
      <Header
        isAuthenticated={isAuthenticated}
        onRevoke={() => setIsAuthenticated(false)}
      />

      {isOffline && (
        <div className="bg-amber-500/20 border-b border-amber-500/30 px-4 py-2 text-center text-xs text-amber-300 flex items-center justify-center space-x-2">
          <WifiOff className="w-3.5 h-3.5" />
          <span>Нет подключения к локальной сети. Подключитесь к Wi-Fi для передачи.</span>
        </div>
      )}

      {!isAuthenticated && showPairing && (
        <PairingModal
          onSuccess={() => {
            setIsAuthenticated(true);
            setShowPairing(false);
          }}
          onClose={() => setShowPairing(false)}
        />
      )}
      <main className="flex-1 max-w-4xl w-full mx-auto p-4 sm:p-6 space-y-6">
        {/* Navigation Tabs — functional menu, pairing is opt-in */}
        <div className="flex rounded-2xl bg-slate-900/80 p-1.5 border border-slate-800">
          <button
            onClick={() => setActiveTab('home')}
            className={`flex-1 flex items-center justify-center space-x-2 py-2.5 rounded-xl text-xs font-semibold transition ${
              activeTab === 'home'
                ? 'bg-emerald-600 text-white shadow-lg shadow-emerald-600/20'
                : 'text-slate-400 hover:text-white'
            }`}
          >
            <Home className="w-4 h-4" />
            <span>Меню</span>
          </button>
          <button
            onClick={() => setActiveTab('send')}
            className={`flex-1 flex items-center justify-center space-x-2 py-2.5 rounded-xl text-xs font-semibold transition ${
              activeTab === 'send'
                ? 'bg-emerald-600 text-white shadow-lg shadow-emerald-600/20'
                : 'text-slate-400 hover:text-white'
            }`}
          >
            <Send className="w-4 h-4" />
            <span>Отправить</span>
          </button>

          <button
            onClick={() => setActiveTab('receive')}
            className={`flex-1 flex items-center justify-center space-x-2 py-2.5 rounded-xl text-xs font-semibold transition ${
              activeTab === 'receive'
                ? 'bg-emerald-600 text-white shadow-lg shadow-emerald-600/20'
                : 'text-slate-400 hover:text-white'
            }`}
          >
            <Download className="w-4 h-4" />
            <span>Получить</span>
          </button>

          <button
            onClick={() => setActiveTab('host')}
            className={`flex-1 flex items-center justify-center space-x-2 py-2.5 rounded-xl text-xs font-semibold transition ${
              activeTab === 'host'
                ? 'bg-emerald-600 text-white shadow-lg shadow-emerald-600/20'
                : 'text-slate-400 hover:text-white'
            }`}
          >
            <Radio className="w-4 h-4" />
            <span>Хост</span>
          </button>
        </div>

        {/* Active View */}
        {activeTab === 'home' && (
          <div className="space-y-4">
            <div className="p-5 rounded-2xl bg-slate-900/60 border border-slate-800 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
              <div className="flex items-center space-x-3.5">
                <div
                  className={`w-12 h-12 rounded-2xl flex items-center justify-center shrink-0 ${
                    isAuthenticated
                      ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                      : 'bg-slate-800/80 text-slate-400 border border-slate-700/60'
                  }`}
                >
                  <ShieldCheck className="w-6 h-6" />
                </div>
                <div>
                  <div className="flex items-center space-x-2">
                    <p className="text-sm font-bold text-white">
                      {isAuthenticated ? 'Подключено к ПК' : 'Не подключено'}
                    </p>
                    <span
                      className={`text-[10px] font-semibold px-2 py-0.5 rounded-full ${
                        isAuthenticated
                          ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/30'
                          : 'bg-slate-800 text-slate-400 border border-slate-700'
                      }`}
                    >
                      {isAuthenticated ? 'Онлайн' : 'Офлайн'}
                    </span>
                  </div>
                  <p className="text-xs text-slate-400 mt-0.5">
                    {isAuthenticated
                      ? 'Сессия активна. Доступна защищенная передача файлов.'
                      : 'Подключитесь к компьютеру через PIN и сверку отпечатка.'}
                  </p>
                </div>
              </div>
              <div className="flex items-center space-x-2">
                {!isAuthenticated ? (
                  <button
                    onClick={() => setShowPairing(true)}
                    className="w-full sm:w-auto flex items-center justify-center space-x-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20"
                  >
                    <KeyRound className="w-4 h-4" />
                    <span>Подключить</span>
                  </button>
                ) : (
                  <button
                    onClick={() => setActiveTab('send')}
                    className="w-full sm:w-auto flex items-center justify-center space-x-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 text-white transition shadow-lg shadow-emerald-600/20"
                  >
                    <Send className="w-4 h-4" />
                    <span>Отправить файлы</span>
                  </button>
                )}
              </div>
            </div>

            {isAuthenticated && <ClipboardCard />}

            <div className="p-5 rounded-2xl bg-slate-900/30 border border-slate-800/60 text-xs text-slate-400 space-y-3 leading-relaxed">
              <p className="font-semibold text-slate-200 text-sm">Как это работает:</p>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 pt-1">
                <div className="p-3 rounded-xl bg-slate-950/40 border border-slate-800/60">
                  <p className="font-bold text-slate-300 mb-1">1. Один Wi-Fi</p>
                  <p className="text-[11px] text-slate-400">ПК и смартфон должны находиться в одной локальной сети.</p>
                </div>
                <div className="p-3 rounded-xl bg-slate-950/40 border border-slate-800/60">
                  <p className="font-bold text-slate-300 mb-1">2. QR на ПК</p>
                  <p className="text-[11px] text-slate-400">На компьютере запущен HushDrop — отображаются QR-код и PIN.</p>
                </div>
                <div className="p-3 rounded-xl bg-slate-950/40 border border-slate-800/60">
                  <p className="font-bold text-slate-300 mb-1">3. PIN + Fingerprint</p>
                  <p className="text-[11px] text-slate-400">Нажмите «Подключить», введите 6 цифр PIN и сверьте отпечаток TLS 1.3.</p>
                </div>
              </div>
            </div>

            {/* Mobile Host Promo */}
            <div className="p-5 rounded-2xl bg-emerald-950/20 border border-emerald-500/20 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
              <div className="flex items-center space-x-3.5">
                <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 shrink-0">
                  <Radio className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-xs font-bold text-white">Передача Телефон → Телефон без ПК</p>
                  <p className="text-[11px] text-slate-400 mt-0.5">
                    Включите точку доступа на телефоне и запустите раздачу во вкладке «Хост».
                  </p>
                </div>
              </div>
              <button
                onClick={() => setActiveTab('host')}
                className="px-4 py-2 rounded-xl text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-emerald-400 border border-emerald-500/30 transition shrink-0"
              >
                Открыть Хост
              </button>
            </div>
          </div>
        )}
        {activeTab === 'send' &&
          (isAuthenticated ? (
            <SendPage />
          ) : (
            <div className="p-8 rounded-2xl bg-slate-900/30 border border-slate-800/60 text-center">
              <KeyRound className="w-8 h-8 text-slate-600 mx-auto mb-2" />
              <p className="text-xs text-slate-400 mb-4">Сначала подключись к ПК, потом отправляй файлы.</p>
              <button
                onClick={() => setShowPairing(true)}
                className="px-4 py-2.5 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white transition"
              >
                Подключить
              </button>
            </div>
          ))}
        {activeTab === 'receive' &&
          (isAuthenticated ? (
            <ReceivePage />
          ) : (
            <div className="p-8 rounded-2xl bg-slate-900/30 border border-slate-800/60 text-center">
              <KeyRound className="w-8 h-8 text-slate-600 mx-auto mb-2" />
              <p className="text-xs text-slate-400 mb-4">Сначала подключись к ПК, потом забирай файлы.</p>
              <button
                onClick={() => setShowPairing(true)}
                className="px-4 py-2.5 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white transition"
              >
                Подключить
              </button>
            </div>
          ))}
        {activeTab === 'host' && <HostPage />}
      </main>

      {/* Footer */}
      <footer className="py-4 text-center text-[11px] text-slate-600 border-t border-slate-900 mt-auto">
        <p>HushDrop • Приватная передача данных в локальной сети • Без облака и мессенджеров</p>
      </footer>
    </div>
  );
};
