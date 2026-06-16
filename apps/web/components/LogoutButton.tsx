'use client';

import { LogOut } from 'lucide-react';
import { useLocale } from 'next-intl';
import { logout } from '../app/[locale]/dashboard/actions';

export default function LogoutButton() {
  const locale = useLocale();

  return (
    <form action={logout}>
      <input type="hidden" name="locale" value={locale} />
      <button
        type="submit"
        className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-4 py-3 text-xs font-black uppercase tracking-widest text-white/70 transition-all hover:border-red-400/40 hover:bg-red-500/10 hover:text-red-300"
      >
        <LogOut size={14} />
        Logout
      </button>
    </form>
  );
}
