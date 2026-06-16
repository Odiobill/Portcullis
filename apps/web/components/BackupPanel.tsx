import { Download } from 'lucide-react';
import { listBackups } from '../lib/ops-files';

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KiB`;
  const mb = kb / 1024;
  if (mb < 1024) return `${mb.toFixed(1)} MiB`;
  return `${(mb / 1024).toFixed(1)} GiB`;
}

export default async function BackupPanel() {
  let backups = [] as Awaited<ReturnType<typeof listBackups>>;
  let error: string | null = null;

  try {
    backups = await listBackups();
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to list backups';
  }

  return (
    <section className="rounded-3xl border border-white/5 bg-card/40 p-6 backdrop-blur-xl">
      <div className="mb-4">
        <h2 className="text-lg font-black text-white">Backups</h2>
        <p className="text-xs text-white/40">Read-only listing from /backups. Downloads are restricted to listed filenames.</p>
      </div>

      {error ? (
        <div className="rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-xs font-bold text-red-400">{error}</div>
      ) : backups.length === 0 ? (
        <div className="rounded-xl border border-white/5 bg-white/[0.03] p-4 text-xs text-white/40">No backups found.</div>
      ) : (
        <div className="space-y-2">
          {backups.map((backup) => (
            <div key={backup.name} className="flex items-center justify-between gap-4 rounded-xl border border-white/5 bg-white/[0.03] p-3">
              <div className="min-w-0">
                <p className="truncate font-mono text-xs font-bold text-white/80">{backup.name}</p>
                <p className="mt-1 text-[10px] uppercase tracking-wider text-white/30">
                  {formatSize(backup.size)} · {new Date(backup.modifiedAt).toLocaleString()}
                </p>
              </div>
              <a
                href={`/api/backups/${encodeURIComponent(backup.name)}`}
                className="inline-flex shrink-0 items-center gap-2 rounded-xl bg-white px-3 py-2 text-[10px] font-black uppercase tracking-widest text-black transition-all hover:bg-accent-cyan"
              >
                <Download size={12} />
                Download
              </a>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
