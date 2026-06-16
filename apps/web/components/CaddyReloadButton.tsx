'use client';

import { useActionState } from 'react';
import { RefreshCw } from 'lucide-react';
import { reloadCaddyConfig } from '../app/[locale]/dashboard/actions';

export default function CaddyReloadButton() {
  const [state, action, isPending] = useActionState(reloadCaddyConfig, null);

  return (
    <div className="flex flex-col items-end gap-2">
      <form action={action}>
        <button
          type="submit"
          disabled={isPending}
          className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-4 py-3 text-xs font-black uppercase tracking-widest text-white/70 transition-all hover:border-accent-cyan/40 hover:bg-accent-cyan/10 hover:text-accent-cyan disabled:opacity-50"
        >
          <RefreshCw size={14} className={isPending ? 'animate-spin' : ''} />
          {isPending ? 'Reloading...' : 'Reload Caddy'}
        </button>
      </form>
      {state && (
        <p className={`max-w-xs text-right text-[10px] font-bold ${state.success ? 'text-green-400' : 'text-red-400'}`}>
          {state.message}
        </p>
      )}
    </div>
  );
}
