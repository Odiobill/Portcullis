'use client';

import { useState, useTransition } from 'react';
import { ExternalLink, Database, Trash2, Globe, Server, Hash, Pencil, Save, X, Copy, Check } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { removeService, updateService } from '../app/[locale]/dashboard/actions';
import type { TlsModeOption } from './RegisterServiceForm';

interface ServiceCardProps {
  service: {
    id: string;
    domains: string[];
    upstreamContainer: string | null;
    upstreamPort: number | null;
    dbName: string | null;
    dbUser: string | null;
    tlsMode: string;
    serviceType: string;
    staticRoot: string | null;
  };
  tlsModes: TlsModeOption[];
}

export default function ServiceCard({ service, tlsModes }: ServiceCardProps) {
  const t = useTranslations('Dashboard');
  const [isDeleting, setIsDeleting] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [isPending, startTransition] = useTransition();
  const [message, setMessage] = useState<{ success: boolean; text: string } | null>(null);
  const [copiedValue, setCopiedValue] = useState<string | null>(null);

  const primaryDomain = service.domains[0];
  const isStatic = service.serviceType === 'static';
  const dbConnectionTemplate = service.dbName && service.dbUser
    ? `postgresql://${service.dbUser}:***@portcullis_db:5432/${service.dbName}`
    : null;

  const [serviceType, setServiceType] = useState<'proxy' | 'static'>(isStatic ? 'static' : 'proxy');
  const [domainInput, setDomainInput] = useState(service.domains.join(', '));
  const [staticRoot, setStaticRoot] = useState(service.staticRoot ?? `/srv/sites/${primaryDomain}`);

  const handleDelete = async () => {
    if (!confirm(t('confirmDelete', { domain: primaryDomain }))) return;

    setIsDeleting(true);
    try {
      const result = await removeService(service.id);
      if (!result.success) {
        setMessage({ success: false, text: result.message });
        setIsDeleting(false);
      }
    } catch (error) {
      console.error('Delete failed:', error);
      setMessage({ success: false, text: 'Delete failed' });
      setIsDeleting(false);
    }
  };

  const handleEditSubmit = (formData: FormData) => {
    setMessage(null);
    startTransition(async () => {
      const result = await updateService(service.id, formData);
      setMessage({ success: result.success, text: result.message });
      if (result.success) {
        setIsEditing(false);
      }
    });
  };

  const copyToClipboard = (value: string) => {
    navigator.clipboard.writeText(value);
    setCopiedValue(value);
    setTimeout(() => setCopiedValue(null), 2000);
  };

  const renderCopyButton = (value: string) => (
    <button
      type="button"
      onClick={() => copyToClipboard(value)}
      className="rounded-lg p-1.5 text-white/30 transition-colors hover:bg-white/10 hover:text-white"
      title="Copy"
    >
      {copiedValue === value ? <Check size={14} className="text-green-400" /> : <Copy size={14} />}
    </button>
  );

  return (
    <div className="group relative overflow-hidden rounded-[2rem] border border-white/5 bg-card/40 p-8 backdrop-blur-xl transition-all hover:border-white/10 hover:shadow-2xl hover:shadow-accent-cyan/5">
      {/* Glow Effect */}
      <div className="pointer-events-none absolute -right-10 -top-10 h-32 w-32 rounded-full bg-accent-cyan/10 blur-3xl transition-opacity opacity-0 group-hover:opacity-100" />

      <div className="relative z-10 flex items-start justify-between">
        <div className="flex items-center gap-4">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-white/5 text-accent-cyan border border-white/5 shadow-inner">
            <Globe size={24} />
          </div>
          <div className="flex flex-col">
            <h3 className="text-xl font-black tracking-tight text-white leading-tight">
              {primaryDomain}
            </h3>
            <div className="flex items-center gap-1.5 mt-1 text-[10px] font-black uppercase tracking-widest text-white/20">
              <Hash size={10} />
              {service.id.slice(0, 8)}
            </div>
            <div className={`mt-1.5 inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[9px] font-black uppercase tracking-wider ${
              isStatic
                ? 'bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/20'
                : 'bg-accent-purple/10 text-accent-purple border border-accent-purple/20'
            }`}>
              {isStatic ? <Globe size={10} /> : <Server size={10} />}
              {isStatic ? 'Static' : 'Proxy'}
            </div>
          </div>
        </div>

        <div className="flex gap-2">
          <button
            onClick={() => {
              setIsEditing(!isEditing);
              setMessage(null);
            }}
            disabled={isDeleting || isPending}
            className="flex h-10 w-10 items-center justify-center rounded-xl text-white/40 transition-all hover:bg-white/10 hover:text-white focus:outline-none active:scale-90"
            title="Edit service"
          >
            {isEditing ? <X size={20} /> : <Pencil size={18} />}
          </button>
          <button
            onClick={handleDelete}
            disabled={isDeleting || isPending}
            className="flex h-10 w-10 items-center justify-center rounded-xl text-white/40 transition-all hover:bg-red-500 hover:text-white focus:outline-none active:scale-90"
            title={t('confirmDelete', { domain: primaryDomain })}
          >
            <Trash2 size={20} className={isDeleting ? 'animate-pulse' : ''} />
          </button>
        </div>
      </div>

      {message && (
        <div className={`relative z-10 mt-5 rounded-xl border p-3 text-xs font-bold ${message.success ? 'border-green-500/20 bg-green-500/10 text-green-400' : 'border-red-500/20 bg-red-500/10 text-red-400'}`}>
          {message.text}
        </div>
      )}

      {isEditing ? (
        <form action={handleEditSubmit} className="relative z-10 mt-8 space-y-4">
          <input type="hidden" name="serviceType" value={serviceType} />

          <div className="space-y-2">
            <label className="text-[10px] font-black uppercase tracking-widest text-white/30">Domains</label>
            <input
              name="domain"
              value={domainInput}
              onChange={(e) => {
                const nextDomain = e.target.value;
                setDomainInput(nextDomain);
                if (serviceType === 'static') {
                  const domain = nextDomain.split(',')[0].trim();
                  setStaticRoot(domain ? `/srv/sites/${domain}` : '');
                }
              }}
              required
              className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white focus:border-accent-cyan/50 focus:outline-none"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <button
              type="button"
              onClick={() => setServiceType('proxy')}
              className={`rounded-xl border py-3 text-xs font-black uppercase tracking-wider ${serviceType === 'proxy' ? 'border-accent-purple/50 bg-accent-purple/10 text-accent-purple' : 'border-white/5 bg-white/5 text-white/40'}`}
            >
              Proxy
            </button>
            <button
              type="button"
              onClick={() => {
                setServiceType('static');
                const domain = domainInput.split(',')[0].trim();
                setStaticRoot(domain ? `/srv/sites/${domain}` : '');
              }}
              className={`rounded-xl border py-3 text-xs font-black uppercase tracking-wider ${serviceType === 'static' ? 'border-accent-cyan/50 bg-accent-cyan/10 text-accent-cyan' : 'border-white/5 bg-white/5 text-white/40'}`}
            >
              Static
            </button>
          </div>

          {serviceType === 'proxy' ? (
            <div className="grid grid-cols-3 gap-3">
              <div className="col-span-2 space-y-2">
                <label className="text-[10px] font-black uppercase tracking-widest text-white/30">Container</label>
                <input
                  name="upstreamContainer"
                  defaultValue={service.upstreamContainer ?? ''}
                  required
                  className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white focus:border-accent-cyan/50 focus:outline-none"
                />
              </div>
              <div className="space-y-2">
                <label className="text-[10px] font-black uppercase tracking-widest text-white/30">Port</label>
                <input
                  name="upstreamPort"
                  type="number"
                  defaultValue={service.upstreamPort ?? 3000}
                  required
                  className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white focus:border-accent-cyan/50 focus:outline-none"
                />
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              <label className="text-[10px] font-black uppercase tracking-widest text-white/30">Static root</label>
              <input
                name="staticRoot"
                value={staticRoot}
                onChange={(e) => setStaticRoot(e.target.value)}
                required
                className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 font-mono text-xs text-white focus:border-accent-cyan/50 focus:outline-none"
              />
            </div>
          )}

          <div className="space-y-2">
            <label className="text-[10px] font-black uppercase tracking-widest text-white/30">TLS mode</label>
            <select
              name="tlsMode"
              defaultValue={service.tlsMode || 'acme'}
              className="w-full rounded-xl border border-white/5 bg-white/5 py-3 px-4 text-xs text-white focus:border-accent-cyan/50 focus:outline-none"
            >
              {tlsModes.map((mode) => (
                <option key={mode.value} value={mode.value} disabled={!mode.available} className="bg-card text-white">
                  {mode.label}{!mode.available ? ' (not configured)' : ''}
                </option>
              ))}
            </select>
          </div>

          <button
            type="submit"
            disabled={isPending}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-white py-3.5 text-xs font-black uppercase tracking-widest text-black transition-all hover:bg-accent-cyan active:scale-[0.98] disabled:opacity-60"
          >
            <Save size={14} />
            {isPending ? 'Saving...' : 'Save changes'}
          </button>
        </form>
      ) : (
        <>
          <div className="mt-8 space-y-6">
            {service.domains.length > 1 && (
              <div className="space-y-2">
                <p className="text-[10px] font-black uppercase tracking-widest text-white/20">Hostnames</p>
                <div className="flex flex-wrap gap-2">
                  {service.domains.map((d, i) => (
                    <span key={i} className="rounded-lg bg-white/5 px-2.5 py-1 text-[11px] font-bold text-white/60 border border-white/5">
                      {d}
                    </span>
                  ))}
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 gap-4">
              {isStatic && service.staticRoot ? (
                <div className="flex items-center gap-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
                  <Globe size={16} className="text-accent-cyan" />
                  <div className="flex flex-col">
                    <span className="text-xs font-bold text-white/70 font-mono">{service.staticRoot}</span>
                    <span className="text-[9px] uppercase tracking-wider text-white/30">Static files</span>
                  </div>
                </div>
              ) : service.upstreamContainer && service.upstreamPort ? (
                <div className="flex items-center gap-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
                  <Server size={16} className="text-accent-purple" />
                  <span className="text-xs font-bold text-white/70 font-mono">
                    {service.upstreamContainer}:{service.upstreamPort}
                  </span>
                </div>
              ) : null}

              <div className="flex items-center gap-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
                <Globe size={16} className="text-accent-cyan" />
                <span className="text-[9px] uppercase tracking-wider text-white/30">TLS: {service.tlsMode}</span>
              </div>

              {service.dbName && service.dbUser && dbConnectionTemplate && (
                <div className="space-y-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
                  <div className="flex items-center gap-3">
                    <Database size={16} className="text-accent-cyan" />
                    <div className="min-w-0 flex-1">
                      <span className="block truncate text-[11px] font-mono font-bold text-white/70 leading-none">{service.dbName}</span>
                      <span className="text-[9px] font-mono font-bold text-white/30 uppercase mt-1">User: {service.dbUser}</span>
                    </div>
                  </div>
                  <div className="grid gap-2 text-[10px]">
                    <div className="flex items-center justify-between gap-2 rounded-lg bg-black/20 px-2 py-1.5">
                      <span className="truncate font-mono text-white/50">host: portcullis_db</span>
                      {renderCopyButton('portcullis_db')}
                    </div>
                    <div className="flex items-center justify-between gap-2 rounded-lg bg-black/20 px-2 py-1.5">
                      <span className="truncate font-mono text-white/50">port: 5432</span>
                      {renderCopyButton('5432')}
                    </div>
                    <div className="flex items-center justify-between gap-2 rounded-lg bg-black/20 px-2 py-1.5">
                      <span className="truncate font-mono text-white/50">{dbConnectionTemplate}</span>
                      {renderCopyButton(dbConnectionTemplate)}
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>

          <div className="mt-8">
            <a
              href={`https://${primaryDomain}`}
              target="_blank"
              rel="noopener noreferrer"
              className="group/btn flex items-center justify-center gap-2 rounded-xl bg-white py-3.5 text-xs font-black uppercase tracking-widest text-black transition-all hover:bg-accent-cyan hover:text-black active:scale-[0.98]"
            >
              <ExternalLink size={14} className="transition-transform group-hover/btn:-translate-y-0.5 group-hover/btn:translate-x-0.5" />
              {t('visit')}
            </a>
          </div>
        </>
      )}
    </div>
  );
}
