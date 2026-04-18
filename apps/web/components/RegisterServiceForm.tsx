'use client';

import { useActionState, useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { registerService } from '../app/[locale]/dashboard/actions';
import { Globe, Database, Loader2, Copy, Check, X, Server, ShieldCheck } from 'lucide-react';

export default function RegisterServiceForm() {
  const t = useTranslations('Dashboard');
  const [state, action, isPending] = useActionState(registerService, null);
  const [showCredentials, setShowCredentials] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (state?.success && state.data?.dbPassword) {
      setShowCredentials(true);
    }
  }, [state]);

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="w-full">
      <form action={action} className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <div className="space-y-4 md:col-span-2">
          <div className="flex items-center gap-2 border-b border-white/5 pb-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent-purple/10 text-accent-purple">
              <Globe size={18} />
            </div>
            <h2 className="text-xl font-bold text-white">{t('registerService')}</h2>
          </div>
          <p className="text-sm text-white/40 leading-relaxed">{t('registerDescription')}</p>
        </div>

        <div className="space-y-2 md:col-span-2">
          <label htmlFor="domain" className="text-xs font-bold uppercase tracking-wider text-white/40">
            {t('domain')} <span className="text-accent-cyan/50 italic font-normal normal-case ml-2">(comma-separated for multiple)</span>
          </label>
          <input
            type="text"
            id="domain"
            name="domain"
            required
            placeholder={t('domainPlaceholder')}
            className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10"
          />
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3 md:col-span-2">
          <div className="space-y-2 sm:col-span-2">
            <label htmlFor="upstreamContainer" className="text-xs font-bold uppercase tracking-wider text-white/40">
              {t('upstreamContainer')}
            </label>
            <div className="relative">
              <input
                type="text"
                id="upstreamContainer"
                name="upstreamContainer"
                required
                placeholder={t('upstreamPlaceholder')}
                className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10"
              />
            </div>
          </div>
          <div className="space-y-2">
            <label htmlFor="upstreamPort" className="text-xs font-bold uppercase tracking-wider text-white/40">
              {t('upstreamPort')}
            </label>
            <input
              type="number"
              id="upstreamPort"
              name="upstreamPort"
              defaultValue="3000"
              placeholder="3000"
              className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10"
            />
          </div>
        </div>

        <label className="flex items-center gap-4 rounded-2xl border border-white/5 bg-white/[0.02] p-5 md:col-span-2 group transition-all hover:bg-white/[0.04] cursor-pointer">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-accent-cyan/10 text-accent-cyan shadow-inner">
            <Database size={24} />
          </div>
          <div className="flex-1">
            <span className="block text-sm font-bold text-white">
              {t('provisionDb')}
            </span>
            <p className="text-xs text-white/40 mt-0.5">{t('provisionDbDescription')}</p>
          </div>
          <div className="relative inline-flex items-center">
            <input
              type="checkbox"
              id="provisionDb"
              name="provisionDb"
              className="peer sr-only"
            />
            <div className="w-11 h-6 bg-white/10 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-accent-cyan"></div>
          </div>
        </label>

        <div className="md:col-span-2 pt-2">
          <button
            type="submit"
            disabled={isPending}
            className="group relative flex w-full items-center justify-center gap-2 overflow-hidden rounded-xl bg-gradient-to-r from-accent-purple to-accent-cyan py-5 text-sm font-black uppercase tracking-widest text-white transition-all hover:opacity-90 active:scale-[0.98] disabled:opacity-70"
          >
            <div className="absolute inset-0 bg-white/10 opacity-0 transition-opacity group-hover:opacity-100" />
            {isPending ? (
              <>
                <Loader2 size={18} className="animate-spin" />
                {t('submitting')}
              </>
            ) : (
              t('submit')
            )}
          </button>
        </div>

        {state && !state.success && (
          <div className="flex items-center gap-3 rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-sm text-red-400 md:col-span-2">
            <X size={18} />
            {state.message}
          </div>
        )}
      </form>

      {/* Database Credentials Modal */}
      {showCredentials && state?.data && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4 backdrop-blur-md">
          <div className="w-full max-w-md rounded-[2.5rem] border border-white/10 bg-card p-10 shadow-2xl shadow-accent-cyan/10">
            <div className="mb-8 flex flex-col items-center text-center">
              <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-green-500/10 text-green-400">
                <ShieldCheck size={32} />
              </div>
              <h3 className="text-2xl font-black text-white">{t('credentialsTitle')}</h3>
              <p className="mt-2 text-sm text-white/50">{t('credentialsDescription')}</p>
            </div>

            <div className="space-y-4">
              <div className="space-y-2 rounded-2xl bg-white/5 p-5 border border-white/5">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/30">{t('dbName')}</p>
                <code className="block text-sm font-mono font-bold text-accent-cyan">{state.data.dbName}</code>
              </div>
              <div className="space-y-2 rounded-2xl bg-white/5 p-5 border border-white/5">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/30">{t('dbUser')}</p>
                <code className="block text-sm font-mono font-bold text-accent-cyan">{state.data.dbUser}</code>
              </div>
              <div className="relative space-y-2 rounded-2xl bg-white/5 p-5 border border-white/5 group">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/30">{t('dbPassword')}</p>
                <div className="flex items-center justify-between">
                  <code className="text-base font-mono font-black text-white">{state.data.dbPassword}</code>
                  <button
                    onClick={() => copyToClipboard(state.data.dbPassword)}
                    className="rounded-lg p-2 text-white/40 transition-colors hover:bg-white/10 hover:text-white"
                  >
                    {copied ? <Check size={18} className="text-green-400" /> : <Copy size={18} />}
                  </button>
                </div>
              </div>
            </div>

            <button
              onClick={() => setShowCredentials(false)}
              className="mt-10 w-full rounded-xl bg-white py-4 text-sm font-black uppercase tracking-widest text-black transition-all hover:bg-white/90 active:scale-[0.98]"
            >
              {t('close')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
