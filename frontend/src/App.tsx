import React, { useState, useEffect } from 'react';
import { Header } from './components/Header';
import { PairingModal } from './components/PairingModal';
import { SendPage } from './pages/SendPage';
import { ReceivePage } from './pages/ReceivePage';
import { apiClient } from './api/client';
import { Send, Download, WifiOff } from 'lucide-react';

export const App: React.FC = () => {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(apiClient.isAuthenticated());
  const [activeTab, setActiveTab] = useState<'send' | 'receive'>('send');
  const [isOffline, setIsOffline] = useState(!navigator.onLine);

  useEffect(() => {
    const unsub = apiClient.subscribeAuth((authed) => {
      setIsAuthenticated(authed);
    });

    const handleOnline = () => setIsOffline(false);
    const handleOffline = () => setIsOffline(true);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

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

      {!isAuthenticated ? (
        <PairingModal onSuccess={() => setIsAuthenticated(true)} />
      ) : (
        <main className="flex-1 max-w-4xl w-full mx-auto p-4 sm:p-6 space-y-6">
          {/* Navigation Tabs */}
          <div className="flex rounded-2xl bg-slate-900/80 p-1.5 border border-slate-800">
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
          </div>

          {/* Active View */}
          {activeTab === 'send' ? <SendPage /> : <ReceivePage />}
        </main>
      )}

      {/* Footer */}
      <footer className="py-4 text-center text-[11px] text-slate-600 border-t border-slate-900 mt-auto">
        <p>HushDrop • Приватная передача данных в локальной сети • Без облака и мессенджеров</p>
      </footer>
    </div>
  );
};
