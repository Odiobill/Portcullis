'use client';

import { useActionState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { login } from './actions';
import { Shield, Loader2, AlertCircle } from 'lucide-react';
import Image from 'next/image';
import { useRouter } from '../../../i18n/routing';
import LanguageSwitcher from '../../../components/LanguageSwitcher';
import { useLocale } from 'next-intl';

export default function LoginPage() {
  const t = useTranslations('Login');
  const locale = useLocale();
  const [state, action, isPending] = useActionState(login, null);
  const router = useRouter();

  useEffect(() => {
    if (state?.success) {
      router.push('/dashboard');
      router.refresh();
    }
  }, [state, router]);

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="mb-8 flex justify-end">
          <LanguageSwitcher />
        </div>
        
        <div className="mb-12 text-center">
          <div className="relative mx-auto mb-6 h-24 w-24 overflow-hidden rounded-3xl border border-white/10 bg-card p-4 shadow-2xl shadow-accent-cyan/20">
            <Image 
              src="/logo.png" 
              alt="Portcullis" 
              fill 
              className="object-contain p-2"
            />
          </div>
          <h1 className="text-3xl font-black tracking-tight text-white">
            <span className="bg-gradient-to-r from-accent-purple to-accent-cyan bg-clip-text text-transparent">
              PORTCULLIS
            </span>
          </h1>
          <p className="mt-2 text-sm font-medium text-white/40 uppercase tracking-widest">
            Control Plane
          </p>
        </div>

        <div className="rounded-3xl border border-white/5 bg-card/50 p-8 backdrop-blur-2xl shadow-2xl">
          <div className="mb-8 flex flex-col items-center text-center">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-accent-cyan/10 text-accent-cyan">
              <Shield size={24} />
            </div>
            <h2 className="text-xl font-bold text-white">{t('title')}</h2>
            <p className="mt-2 text-sm text-white/50">{t('subtitle')}</p>
          </div>

          <form action={action} className="space-y-6">
            <input type="hidden" name="locale" value={locale} />
            <div className="space-y-2">
              <label htmlFor="passcode" className="text-xs font-bold uppercase tracking-wider text-white/40">
                {t('passcode')}
              </label>
              <input
                type="password"
                id="passcode"
                name="passcode"
                required
                autoFocus
                placeholder={t('passcodePlaceholder')}
                className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-center text-lg font-mono tracking-widest text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10"
              />
            </div>

            {state && !state.success && (
              <div className="flex items-center gap-3 rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-sm text-red-400">
                <AlertCircle size={18} />
                {t('error')}
              </div>
            )}

            <button
              type="submit"
              disabled={isPending}
              className="group relative flex w-full items-center justify-center gap-2 overflow-hidden rounded-xl bg-gradient-to-r from-accent-purple to-accent-cyan py-4 text-sm font-black uppercase tracking-widest text-white transition-all hover:opacity-90 active:scale-[0.98] disabled:opacity-70"
            >
              <div className="absolute inset-0 bg-white/10 opacity-0 transition-opacity group-hover:opacity-100" />
              {isPending ? (
                <Loader2 size={18} className="animate-spin" />
              ) : (
                t('submit')
              )}
            </button>
          </form>
        </div>

        <p className="mt-8 text-center text-xs text-white/20">
          Portcullis v0.1.0-alpha &bull; Built for total control.
        </p>
      </div>
    </div>
  );
}
