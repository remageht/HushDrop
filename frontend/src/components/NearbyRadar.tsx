import React, { useState, useEffect } from 'react';
import { Radar, Loader2, WifiOff, ShieldAlert, ArrowRight, Activity, Laptop, RefreshCw } from 'lucide-react';
import { apiClient, NearbyNode } from '../api/client';

interface NearbyRadarProps {
  isAuthenticated: boolean;
  onConnect: (node: NearbyNode) => void;
  onRequireAuth?: () => void;
}

export const NearbyRadar: React.FC<NearbyRadarProps> = ({
  isAuthenticated,
  onConnect,
  onRequireAuth,
}) => {
  const [nodes, setNodes] = useState<NearbyNode[]>([]);
  const [isScanning, setIsScanning] = useState(false);
  const [hasScanned, setHasScanned] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [cooldown, setCooldown] = useState<number>(0);

  // Cooldown countdown timer
  useEffect(() => {
    if (cooldown <= 0) return;
    const interval = setInterval(() => {
      setCooldown((c) => (c > 1 ? c - 1 : 0));
    }, 1000);
    return () => clearInterval(interval);
  }, [cooldown]);

  const handleScan = async () => {
    if (isScanning || cooldown > 0) return;

    if (!isAuthenticated) {
      if (onRequireAuth) {
        onRequireAuth();
      } else {
        setErrorMessage('Сначала выполните сопряжение со своим узлом.');
      }
      return;
    }

    setIsScanning(true);
    setErrorMessage(null);

    try {
      const results = await apiClient.scanNearby();
      setNodes(results);
      setHasScanned(true);
      setCooldown(15);
    } catch (err: any) {
      const msg = err.message || 'Ошибка при сканировании локальной сети.';
      setErrorMessage(msg);
      // If error indicates 429 rate limit, parse cooldown seconds
      const match = msg.match(/(\d+)\s*(сек|seconds)/i);
      if (match) {
        setCooldown(parseInt(match[1], 10));
      } else {
        setCooldown(15);
      }
    } finally {
      setIsScanning(false);
    }
  };

  return (
    <div className="p-5 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="flex items-center space-x-3.5">
          <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 shrink-0">
            <Radar className={`w-5 h-5 ${isScanning ? 'animate-spin text-emerald-300' : ''}`} />
          </div>
          <div>
            <div className="flex items-center space-x-2">
              <h3 className="text-sm font-bold text-white">Устройства рядом</h3>
              <span className="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-slate-800 text-slate-300 border border-slate-700">
                LAN Radar
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-0.5">
              Автопоиск доступных узлов HushDrop в локальной сети
            </p>
          </div>
        </div>

        {/* Scan Trigger Button */}
        <div className="flex items-center">
          <button
            type="button"
            onClick={handleScan}
            disabled={isScanning || cooldown > 0}
            className="w-full sm:w-auto flex items-center justify-center space-x-2 px-4 py-2 rounded-xl text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 disabled:bg-slate-800 disabled:text-slate-500 disabled:border disabled:border-slate-700/60 text-white transition shadow-lg shadow-emerald-600/20 shrink-0"
          >
            {isScanning ? (
              <>
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                <span>Сканирование...</span>
              </>
            ) : cooldown > 0 ? (
              <>
                <RefreshCw className="w-3.5 h-3.5" />
                <span>Повтор ({cooldown}с)</span>
              </>
            ) : (
              <>
                <Radar className="w-3.5 h-3.5" />
                <span>{hasScanned ? 'Обновить' : 'Найти рядом'}</span>
              </>
            )}
          </button>
        </div>
      </div>

      {/* Error State */}
      {errorMessage && (
        <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-400 text-xs flex items-center space-x-2.5">
          <ShieldAlert className="w-4 h-4 shrink-0" />
          <span>{errorMessage}</span>
        </div>
      )}

      {/* Loading Scanning Pulse */}
      {isScanning && (
        <div className="p-4 rounded-xl bg-slate-950/40 border border-slate-800/80 text-center space-y-2">
          <div className="flex items-center justify-center space-x-2 text-xs text-emerald-400 font-medium">
            <Loader2 className="w-4 h-4 animate-spin" />
            <span>Опрос адресов подсети (до 256 узлов)...</span>
          </div>
          <p className="text-[11px] text-slate-500">
            Безопасный поиск активных служб HushDrop без передачи отпечатков
          </p>
        </div>
      )}

      {/* Discovered Nodes List */}
      {!isScanning && hasScanned && nodes.length > 0 && (
        <div className="space-y-2 pt-1">
          <p className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
            Найдено устройств: {nodes.length}
          </p>
          <div className="grid grid-cols-1 gap-2.5">
            {nodes.map((node) => (
              <div
                key={`${node.ip}:${node.port}`}
                className="p-3.5 rounded-xl bg-slate-950/50 border border-slate-800 hover:border-slate-700 transition flex items-center justify-between gap-3"
              >
                <div className="flex items-center space-x-3 min-w-0">
                  <div className="w-9 h-9 rounded-lg bg-slate-800/80 border border-slate-700/60 flex items-center justify-center text-slate-300 shrink-0">
                    <Laptop className="w-4 h-4" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center space-x-2">
                      <span className="font-mono text-xs font-bold text-white truncate">
                        {node.ip}:{node.port}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300 border border-slate-700 font-mono">
                        v{node.version}
                      </span>
                    </div>
                    <div className="flex items-center space-x-2 mt-0.5">
                      <span className="flex items-center text-[11px] text-emerald-400 font-mono">
                        <Activity className="w-3 h-3 mr-1" />
                        {node.latencyMs} мс
                      </span>
                      <span className="text-slate-600 text-xs">•</span>
                      <span className="text-[11px] text-slate-400">Готов к передаче</span>
                    </div>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => onConnect(node)}
                  className="px-3 py-1.5 rounded-xl text-xs font-semibold bg-emerald-600/90 hover:bg-emerald-500 text-white flex items-center space-x-1.5 transition shrink-0 shadow-md shadow-emerald-600/20"
                >
                  <span>Подключить</span>
                  <ArrowRight className="w-3.5 h-3.5" />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Empty State */}
      {!isScanning && hasScanned && nodes.length === 0 && !errorMessage && (
        <div className="p-6 rounded-xl bg-slate-950/30 border border-slate-800/60 text-center space-y-2">
          <WifiOff className="w-8 h-8 text-slate-600 mx-auto" />
          <p className="text-xs font-medium text-slate-300">Рядом никого не найдено</p>
          <p className="text-[11px] text-slate-500 max-w-sm mx-auto">
            Убедитесь, что второй узел HushDrop запущен на другом устройстве или порту в той же локальной сети.
          </p>
        </div>
      )}
    </div>
  );
};
