'use client';

import { useLocale } from 'next-intl';
import { useRouter, usePathname } from '../i18n/routing';
import { Languages } from 'lucide-react';

export default function LanguageSwitcher() {
  const locale = useLocale();
  const router = useRouter();
  const pathname = usePathname();

  const toggleLocale = () => {
    const nextLocale = locale === 'en' ? 'it' : 'en';
    router.replace(pathname, { locale: nextLocale });
  };

  return (
    <button
      onClick={toggleLocale}
      className="flex items-center gap-2 rounded-xl border border-white/5 bg-white/5 px-4 py-2 text-xs font-bold uppercase tracking-widest text-white/60 transition-all hover:bg-white/10 hover:text-white"
    >
      <Languages size={14} className="text-accent-cyan" />
      <span>{locale === 'en' ? 'English' : 'Italiano'}</span>
    </button>
  );
}
