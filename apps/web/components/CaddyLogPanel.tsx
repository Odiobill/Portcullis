import { readRecentCaddyLogs } from '../lib/ops-files';

export default async function CaddyLogPanel() {
  let logs = [] as Awaited<ReturnType<typeof readRecentCaddyLogs>>;
  let error: string | null = null;

  try {
    logs = await readRecentCaddyLogs(50);
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to read Caddy logs';
  }

  return (
    <section className="rounded-3xl border border-white/5 bg-card/40 p-6 backdrop-blur-xl">
      <div className="mb-4">
        <h2 className="text-lg font-black text-white">Caddy logs</h2>
        <p className="text-xs text-white/40">Last 50 JSON log lines from /var/log/caddy/portcullis.log.</p>
      </div>

      {error ? (
        <div className="rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-xs font-bold text-red-400">{error}</div>
      ) : logs.length === 0 ? (
        <div className="rounded-xl border border-white/5 bg-white/[0.03] p-4 text-xs text-white/40">No Caddy log file found yet.</div>
      ) : (
        <div className="max-h-96 space-y-2 overflow-auto rounded-xl border border-white/5 bg-black/30 p-4 font-mono text-[11px]">
          {logs.map((line, index) => (
            <div key={`${line.raw}-${index}`} className="border-b border-white/5 pb-2 last:border-b-0 last:pb-0">
              <div className="flex flex-wrap gap-2 text-white/30">
                {line.ts && <span>{line.ts}</span>}
                {line.level && <span className="text-accent-cyan">{line.level}</span>}
                {line.logger && <span>{line.logger}</span>}
              </div>
              <div className="mt-1 break-words text-white/70">{line.msg ?? line.raw}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
