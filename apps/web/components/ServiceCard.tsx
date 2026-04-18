'use client';

import { useState } from 'react';
import { ExternalLink, Database, Trash2, Globe, Server, Hash } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { removeService } from '../app/[locale]/dashboard/actions';

interface ServiceCardProps {
  service: {
    id: string;
    domains: string[];
    upstreamContainer: string;
    upstreamPort: number;
    dbName: string | null;
    dbUser: string | null;
  };
}

export default function ServiceCard({ service }: ServiceCardProps) {
  const t = useTranslations('Dashboard');
  const [isDeleting, setIsDeleting] = useState(false);

  const primaryDomain = service.domains[0];

  const handleDelete = async () => {
    if (!confirm(t('confirmDelete', { domain: primaryDomain }))) return;
    
    setIsDeleting(true);
    try {
      await removeService(service.id);
    } catch (error) {
      console.error('Delete failed:', error);
      setIsDeleting(false);
    }
  };

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
          </div>
        </div>
        
        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex h-10 w-10 items-center justify-center rounded-xl text-white/40 transition-all hover:bg-red-500 hover:text-white focus:outline-none active:scale-90"
          title={t('confirmDelete', { domain: primaryDomain })}
        >
          <Trash2 size={20} className={isDeleting ? 'animate-pulse' : ''} />
        </button>
      </div>

      <div className="mt-8 space-y-6">
        {/* Domains List */}
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
          <div className="flex items-center gap-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
            <Server size={16} className="text-accent-purple" />
            <span className="text-xs font-bold text-white/70 font-mono">
              {service.upstreamContainer}:{service.upstreamPort}
            </span>
          </div>

          {service.dbName && (
            <div className="flex items-center gap-3 rounded-xl bg-white/[0.03] p-3 border border-white/5">
              <Database size={16} className="text-accent-cyan" />
              <div className="flex flex-col">
                <span className="text-[11px] font-mono font-bold text-white/70 leading-none">{service.dbName}</span>
                <span className="text-[9px] font-mono font-bold text-white/30 uppercase mt-1">User: {service.dbUser}</span>
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
    </div>
  );
}
