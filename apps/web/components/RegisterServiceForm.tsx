'use client';

import { useActionState, useState, useRef } from 'react';
import { useTranslations } from 'next-intl';
import { registerService } from '../app/[locale]/dashboard/actions';
import { Globe, Database, Loader2, Copy, Check, X, ShieldCheck, ShieldQuestion, Server } from 'lucide-react';

export type TlsModeOption = {
  value: string;
  label: string;
  available: boolean;
};

export default function RegisterServiceForm({ tlsModes }: { tlsModes: TlsModeOption[] }) {
  const t = useTranslations('Dashboard');
  const [state, action, isPending] = useActionState(registerService, null);
  const [dismissedCredentialId, setDismissedCredentialId] = useState<string | null>(null);
  const [provisionDb, setProvisionDb] = useState(false);
  const [copied, setCopied] = useState(false);
  const [serviceType, setServiceType] = useState<'proxy' | 'static'>('proxy');
  const [domainInput, setDomainInput] = useState('');
  const [staticRoot, setStaticRoot] = useState('');
  const formRef = useRef<HTMLFormElement>(null);

  // Compute default TLS mode: namecheap_tls > acme > first available
  const defaultTlsMode = tlsModes.find(m => m.value === 'namecheap_tls' && m.available)
    ?? tlsModes.find(m => m.value === 'acme' && m.available)
    ?? tlsModes.find(m => m.available)
    ?? tlsModes[0];


  const credentialId = state?.data?.id ?? null;
  const credentialDbName = state?.data?.dbName ?? '';
  const credentialDbUser = state?.data?.dbUser ?? '';
  const credentialPassword = state?.data?.dbPassword ?? '';
  const showCredentials = Boolean(state?.success && credentialPassword && credentialId !== dismissedCredentialId);

  const handleCloseModal = () => {
    setDismissedCredentialId(credentialId);
    setProvisionDb(false);
    setDomainInput('');
    setStaticRoot('');
    formRef.current?.reset();
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="w-full">
      <form ref={formRef} action={action} className="grid grid-cols-1 gap-6 md:grid-cols-2">
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
            value={domainInput}
            onChange={(e) => {
              const nextDomain = e.target.value;
              setDomainInput(nextDomain);
              if (serviceType === 'static') {
                const primaryDomain = nextDomain.split(',')[0].trim();
                setStaticRoot(primaryDomain ? `/srv/sites/${primaryDomain}` : '');
              }
            }}
            placeholder={t('domainPlaceholder')}
            className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10"
          />
        </div>

        {/* Service Type Toggle */}
        <div className="space-y-2 md:col-span-2">
          <label className="text-xs font-bold uppercase tracking-wider text-white/40">Service Type</label>
          <div className="flex gap-4">
            <label className={`flex flex-1 cursor-pointer items-center justify-center gap-2 rounded-xl border py-4 px-5 text-sm font-bold transition-all ${
              serviceType === 'proxy'
                ? 'border-accent-purple/50 bg-accent-purple/10 text-accent-purple'
                : 'border-white/5 bg-white/5 text-white/40 hover:border-white/10'
            }`}>
              <input
                type="radio"
                name="serviceType"
                value="proxy"
                checked={serviceType === 'proxy'}
                onChange={() => setServiceType('proxy')}
                className="sr-only"
              />
              <Server size={18} />
              Reverse Proxy
            </label>
            <label className={`flex flex-1 cursor-pointer items-center justify-center gap-2 rounded-xl border py-4 px-5 text-sm font-bold transition-all ${
              serviceType === 'static'
                ? 'border-accent-cyan/50 bg-accent-cyan/10 text-accent-cyan'
                : 'border-white/5 bg-white/5 text-white/40 hover:border-white/10'
            }`}>
              <input
                type="radio"
                name="serviceType"
                value="static"
                checked={serviceType === 'static'}
                onChange={() => {
                  setServiceType('static');
                  const primaryDomain = domainInput.split(',')[0].trim();
                  setStaticRoot(primaryDomain ? `/srv/sites/${primaryDomain}` : '');
                }}
                className="sr-only"
              />
              <Globe size={18} />
              Static Site
            </label>
          </div>
        </div>

        {/* Proxy-specific fields */}
        {serviceType === 'proxy' && (
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
        )}

        {/* Static-specific fields */}
        {serviceType === 'static' && (
          <div className="space-y-2 md:col-span-2">
            <label htmlFor="staticRoot" className="text-xs font-bold uppercase tracking-wider text-white/40">
              Static Root Path
            </label>
            <input
              type="text"
              id="staticRoot"
              name="staticRoot"
              value={staticRoot}
              onChange={(e) => setStaticRoot(e.target.value)}
              placeholder="/srv/sites/your.domain.com"
              className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 placeholder:text-white/10 font-mono"
            />
            <p className="text-[10px] text-white/30 mt-1">
              Path on the host where static files are served from. Must start with <code className="text-accent-cyan/50">/srv/sites/</code>.
            </p>
          </div>
        )}

        <div className="space-y-2 md:col-span-2">
          <label htmlFor="tlsMode" className="text-xs font-bold uppercase tracking-wider text-white/40">
            {t('tlsMode')}
          </label>
          <div className="relative">
            <select
              id="tlsMode"
              name="tlsMode"
              defaultValue={defaultTlsMode?.value || ''}
              className="w-full rounded-xl border border-white/5 bg-white/5 py-4 px-5 text-sm text-white transition-all focus:border-accent-cyan/50 focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 appearance-none"
            >
              <option value="" disabled className="bg-card text-white/40">
                {t('tlsModePlaceholder')}
              </option>
              {tlsModes.map((mode) => (
                <option
                  key={mode.value}
                  value={mode.value}
                  disabled={!mode.available}
                  className="bg-card text-white"
                >
                  {mode.label}{!mode.available ? ` (${t('tlsModeNotConfigured')})` : ''}
                </option>
              ))}
            </select>
            <div className="pointer-events-none absolute inset-y-0 right-4 flex items-center">
              {tlsModes.some(m => !m.available) ? (
                <ShieldQuestion size={16} className="text-white/20" />
              ) : (
                <ShieldCheck size={16} className="text-accent-cyan/50" />
              )}
            </div>
          </div>
          <p className="text-[10px] text-white/30 mt-1">
            {t('tlsModeDescription')}
          </p>
        </div>

        <div className="md:col-span-2">
          <label className="flex items-center gap-4 rounded-2xl border border-white/5 bg-white/[0.02] p-5 group transition-all hover:bg-white/[0.04] cursor-pointer">
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
                checked={provisionDb}
                className="peer sr-only"
                onChange={(e) => setProvisionDb(e.target.checked)}
              />
              <div className="w-11 h-6 bg-white/10 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-accent-cyan"></div>
            </div>
          </label>

          <div 
            id="advanced-db-settings" 
            className={`mt-4 grid grid-cols-1 gap-4 rounded-2xl border border-white/5 bg-white/[0.01] p-5 animate-in fade-in slide-in-from-top-2 duration-300 ${provisionDb ? 'grid' : 'hidden'}`}
          >
            <p className="text-[10px] font-black uppercase tracking-widest text-accent-cyan/50 mb-1">{t('advancedSettings')}</p>
            
            <div className="space-y-2">
              <label htmlFor="dbName" className="text-[10px] font-bold uppercase tracking-wider text-white/30">
                {t('dbName')}
              </label>
              <input
                type="text"
                id="dbName"
                name="dbName"
                placeholder={t('dbNamePlaceholder')}
                className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white transition-all focus:border-accent-cyan/50 focus:outline-none placeholder:text-white/5"
              />
            </div>

            <div className="space-y-2">
              <label htmlFor="dbUser" className="text-[10px] font-bold uppercase tracking-wider text-white/30">
                {t('dbUser')}
              </label>
              <input
                type="text"
                id="dbUser"
                name="dbUser"
                placeholder={t('dbUserPlaceholder')}
                className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white transition-all focus:border-accent-cyan/50 focus:outline-none placeholder:text-white/5"
              />
            </div>

            <div className="space-y-2">
              <label htmlFor="dbPassword" className="text-[10px] font-bold uppercase tracking-wider text-white/30">
                {t('dbPassword')}
              </label>
              <input
                type="password"
                id="dbPassword"
                name="dbPassword"
                placeholder={t('dbPasswordPlaceholder')}
                className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white transition-all focus:border-accent-cyan/50 focus:outline-none placeholder:text-white/5"
              />
            </div>
          </div>
        </div>

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
                <code className="block text-sm font-mono font-bold text-accent-cyan">{credentialDbName}</code>
              </div>
              <div className="space-y-2 rounded-2xl bg-white/5 p-5 border border-white/5">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/30">{t('dbUser')}</p>
                <code className="block text-sm font-mono font-bold text-accent-cyan">{credentialDbUser}</code>
              </div>
              <div className="relative space-y-2 rounded-2xl bg-white/5 p-5 border border-white/5 group">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/30">{t('dbPassword')}</p>
                <div className="flex items-center justify-between">
                  <code className="text-base font-mono font-black text-white">{credentialPassword}</code>
                  <button
                    onClick={() => copyToClipboard(credentialPassword)}
                    className="rounded-lg p-2 text-white/40 transition-colors hover:bg-white/10 hover:text-white"
                  >
                    {copied ? <Check size={18} className="text-green-400" /> : <Copy size={18} />}
                  </button>
                </div>
              </div>
            </div>

            <button
              onClick={handleCloseModal}
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
