'use client';

import { useActionState, useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { registerService, ActionResponse } from '../app/[locale]/dashboard/actions';
import { Server, Globe, Database, Loader2, Copy, Check, X } from 'lucide-react';

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
          <div className="flex items-center gap-2 border-b border-zinc-200 pb-2 dark:border-zinc-800">
            <Globe size={18} className="text-zinc-500" />
            <h2 className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">{t('registerService')}</h2>
          </div>
          <p className="text-sm text-zinc-500 dark:text-zinc-400">{t('registerDescription')}</p>
        </div>

        <div className="space-y-2">
          <label htmlFor="domain" className="text-sm font-medium text-zinc-700 dark:text-zinc-300">
            {t('domain')}
          </label>
          <div className="relative">
            <input
              type="text"
              id="domain"
              name="domain"
              required
              placeholder={t('domainPlaceholder')}
              className="w-full rounded-xl border border-zinc-200 bg-white py-3 pl-4 pr-4 text-sm transition-all focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-zinc-800 dark:bg-zinc-950 dark:focus:border-zinc-50 dark:focus:ring-zinc-50"
            />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="space-y-2 sm:col-span-2">
            <label htmlFor="upstreamContainer" className="text-sm font-medium text-zinc-700 dark:text-zinc-300">
              {t('upstreamContainer')}
            </label>
            <input
              type="text"
              id="upstreamContainer"
              name="upstreamContainer"
              required
              placeholder={t('upstreamPlaceholder')}
              className="w-full rounded-xl border border-zinc-200 bg-white py-3 px-4 text-sm transition-all focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-zinc-800 dark:bg-zinc-950 dark:focus:border-zinc-50 dark:focus:ring-zinc-50"
            />
          </div>
          <div className="space-y-2">
            <label htmlFor="upstreamPort" className="text-sm font-medium text-zinc-700 dark:text-zinc-300">
              {t('upstreamPort')}
            </label>
            <input
              type="number"
              id="upstreamPort"
              name="upstreamPort"
              required
              defaultValue="3000"
              placeholder={t('upstreamPortPlaceholder')}
              className="w-full rounded-xl border border-zinc-200 bg-white py-3 px-4 text-sm transition-all focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-zinc-800 dark:bg-zinc-950 dark:focus:border-zinc-50 dark:focus:ring-zinc-50"
            />
          </div>
        </div>

        <div className="flex items-center gap-3 rounded-2xl border border-zinc-100 bg-zinc-50 p-4 dark:border-zinc-800 dark:bg-zinc-900/50 md:col-span-2">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white text-zinc-600 shadow-sm dark:bg-zinc-800 dark:text-zinc-400">
            <Database size={20} />
          </div>
          <div className="flex-1">
            <label htmlFor="provisionDb" className="text-sm font-semibold text-zinc-900 dark:text-zinc-50">
              {t('provisionDb')}
            </label>
            <p className="text-xs text-zinc-500 dark:text-zinc-400">{t('provisionDbDescription')}</p>
          </div>
          <input
            type="checkbox"
            id="provisionDb"
            name="provisionDb"
            className="h-5 w-5 rounded border-zinc-300 text-zinc-900 focus:ring-zinc-900 dark:border-zinc-700 dark:bg-zinc-800 dark:ring-offset-zinc-900 dark:focus:ring-zinc-50"
          />
        </div>

        <div className="md:col-span-2">
          <button
            type="submit"
            disabled={isPending}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-zinc-900 py-4 text-sm font-semibold text-white transition-all hover:bg-zinc-800 active:scale-[0.98] disabled:opacity-70 dark:bg-zinc-50 dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
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
          <div className="flex items-center gap-3 rounded-xl border border-red-100 bg-red-50 p-4 text-sm text-red-600 dark:border-red-900/30 dark:bg-red-900/10 dark:text-red-400 md:col-span-2">
            <X size={18} />
            {state.message}
          </div>
        )}
      </form>

      {/* Database Credentials Modal */}
      {showCredentials && state?.data && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-3xl border border-zinc-200 bg-white p-8 shadow-2xl dark:border-zinc-800 dark:bg-zinc-900">
            <div className="mb-6 flex items-center gap-3">
              <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-green-50 text-green-600 dark:bg-green-900/20 dark:text-green-400">
                <CheckCircle2 size={24} />
              </div>
              <div>
                <h3 className="text-xl font-bold text-zinc-900 dark:text-zinc-50">{t('credentialsTitle')}</h3>
                <p className="text-sm text-zinc-500 dark:text-zinc-400">{t('credentialsDescription')}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div className="space-y-1.5 rounded-2xl bg-zinc-50 p-4 dark:bg-zinc-950">
                <p className="text-[10px] font-bold uppercase tracking-wider text-zinc-400">{t('dbName')}</p>
                <code className="text-sm font-semibold text-zinc-900 dark:text-zinc-50">{state.data.dbName}</code>
              </div>
              <div className="space-y-1.5 rounded-2xl bg-zinc-50 p-4 dark:bg-zinc-950">
                <p className="text-[10px] font-bold uppercase tracking-wider text-zinc-400">{t('dbUser')}</p>
                <code className="text-sm font-semibold text-zinc-900 dark:text-zinc-50">{state.data.dbUser}</code>
              </div>
              <div className="group relative space-y-1.5 rounded-2xl bg-zinc-50 p-4 dark:bg-zinc-950">
                <p className="text-[10px] font-bold uppercase tracking-wider text-zinc-400">{t('dbPassword')}</p>
                <div className="flex items-center justify-between">
                  <code className="text-sm font-bold text-blue-600 dark:text-blue-400">{state.data.dbPassword}</code>
                  <button
                    onClick={() => copyToClipboard(state.data.dbPassword)}
                    className="ml-2 rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-zinc-200 dark:text-zinc-500 dark:hover:bg-zinc-800"
                  >
                    {copied ? <Check size={16} className="text-green-600" /> : <Copy size={16} />}
                  </button>
                </div>
              </div>
            </div>

            <button
              onClick={() => setShowCredentials(false)}
              className="mt-8 w-full rounded-xl bg-zinc-900 py-3 text-sm font-semibold text-white dark:bg-zinc-50 dark:text-zinc-900"
            >
              {t('close')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function CheckCircle2(props: any) {
  return (
    <svg
      {...props}
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
}
