'use client';

import { useMemo, useState, useTransition } from 'react';
import { ArrowDownUp, RefreshCw } from 'lucide-react';
import { useRouter } from 'next/navigation';
import type { CaddyLogLine } from '../lib/ops-files';

interface CaddyLogViewerProps {
  logs: CaddyLogLine[];
  error: string | null;
}

const levelClasses: Record<string, string> = {
  error: 'border-red-500/30 bg-red-500/10 text-red-300',
  warn: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
  warning: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
  info: 'border-accent-cyan/30 bg-accent-cyan/10 text-accent-cyan',
  debug: 'border-purple-500/30 bg-purple-500/10 text-purple-300',
};

function levelBadgeClass(level: string) {
  return levelClasses[level.toLowerCase()] ?? 'border-white/10 bg-white/5 text-white/50';
}

export default function CaddyLogViewer({ logs, error }: CaddyLogViewerProps) {
  const router = useRouter();
  const [newestFirst, setNewestFirst] = useState(true);
  const [isPending, startTransition] = useTransition();

  const visibleLogs = useMemo(() => {
    return newestFirst ? [...logs].reverse() : logs;
  }, [logs, newestFirst]);

  const refresh = () => {
    startTransition(() => router.refresh());
  };

  return (
    <section className="rounded-3xl border border-white/5 bg-card/40 p-6 backdrop-blur-xl">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-lg font-black text-white">Caddy logs</h2>
          <p className="text-xs text-white/40">Last 50 JSON log lines from /var/log/caddy/portcullis.log.</p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button
            type="button"
            onClick={() => setNewestFirst((value) => !value)}
            className="inline-flex items-center gap-2 rounded-xl border border-white/5 bg-white/5 px-3 py-2 text-[10px] font-black uppercase tracking-widest text-white/50 transition-colors hover:border-white/10 hover:bg-white/10 hover:text-white"
            title="Reverse log order"
          >
            <ArrowDownUp size={13} />
            {newestFirst ? 'Newest' : 'Oldest'}
          </button>
          <button
            type="button"
            onClick={refresh}
            disabled={isPending}
            className="inline-flex items-center gap-2 rounded-xl border border-accent-cyan/20 bg-accent-cyan/10 px-3 py-2 text-[10px] font-black uppercase tracking-widest text-accent-cyan transition-colors hover:bg-accent-cyan/20 disabled:cursor-not-allowed disabled:opacity-50"
            title="Refresh Caddy logs"
          >
            <RefreshCw size={13} className={isPending ? 'animate-spin' : ''} />
            Refresh
          </button>
        </div>
      </div>

      {error ? (
        <div className="rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-xs font-bold text-red-400">{error}</div>
      ) : visibleLogs.length === 0 ? (
        <div className="rounded-xl border border-white/5 bg-white/[0.03] p-4 text-xs text-white/40">No Caddy log file found yet.</div>
      ) : (
        <div className="max-h-96 space-y-2 overflow-auto rounded-xl border border-white/5 bg-black/30 p-4 font-mono text-[11px]">
          {visibleLogs.map((line, index) => (
            <div key={`${line.raw}-${index}`} className="border-b border-white/5 pb-2 last:border-b-0 last:pb-0">
              <div className="flex flex-wrap items-center gap-2 text-white/30">
                {line.ts && <span>{line.ts}</span>}
                {line.level && (
                  <span className={`rounded-md border px-1.5 py-0.5 text-[9px] font-black uppercase tracking-wider ${levelBadgeClass(line.level)}`}>
                    {line.level}
                  </span>
                )}
                {line.logger && <span className="truncate">{line.logger}</span>}
              </div>
              <div className="mt-1 break-words text-white/70">{line.msg ?? line.raw}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
