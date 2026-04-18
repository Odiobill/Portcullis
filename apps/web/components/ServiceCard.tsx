'use client';

import { useState } from 'react';
import { ExternalLink, Database, Trash2, Globe, Server, CheckCircle2, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { removeService } from '../app/[locale]/dashboard/actions';

interface ServiceCardProps {
  service: {
    id: string;
    domain: string;
    upstreamContainer: string;
    upstreamPort: number;
    dbName: string | null;
    dbUser: string | null;
  };
}

export default function ServiceCard({ service }: ServiceCardProps) {
  const t = useTranslations('Dashboard');
  const [isDeleting, setIsDeleting] = useState(false);

  const handleDelete = async () => {
    if (!confirm(t('confirmDelete', { domain: service.domain }))) return;
    
    setIsDeleting(true);
    try {
      await removeService(service.id);
    } catch (error) {
      console.error('Delete failed:', error);
      setIsDeleting(false);
    }
  };

  return (
    <div className="group relative overflow-hidden rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm transition-all hover:border-zinc-300 hover:shadow-md dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400">
            <Globe size={20} />
          </div>
          <div>
            <h3 className="text-lg font-semibold tracking-tight text-zinc-900 dark:text-zinc-50">
              {service.domain}
            </h3>
            <p className="text-sm text-zinc-500 dark:text-zinc-400">
              {service.id}
            </p>
          </div>
        </div>
        
        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex h-9 w-9 items-center justify-center rounded-lg text-zinc-400 transition-colors hover:bg-red-50 hover:text-red-600 focus:outline-none dark:text-zinc-500 dark:hover:bg-red-900/20 dark:hover:text-red-400"
        >
          <Trash2 size={18} className={isDeleting ? 'animate-pulse' : ''} />
        </button>
      </div>

      <div className="mt-6 space-y-4">
        <div className="flex items-center gap-3 text-sm">
          <Server size={16} className="text-zinc-400" />
          <span className="font-medium text-zinc-700 dark:text-zinc-300">
            {service.upstreamContainer}:{service.upstreamPort}
          </span>
        </div>

        {service.dbName && (
          <div className="flex items-center gap-3 text-sm text-zinc-600 dark:text-zinc-400">
            <Database size={16} className="text-zinc-400" />
            <div className="flex flex-col">
              <span className="font-mono text-xs">{service.dbName}</span>
              <span className="font-mono text-[10px] opacity-70">User: {service.dbUser}</span>
            </div>
          </div>
        )}
      </div>

      <div className="mt-6 flex items-center justify-between gap-3">
        <a
          href={`https://${service.domain}`}
          target="_blank"
          rel="noopener noreferrer"
          className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-zinc-900 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800 dark:bg-zinc-50 dark:text-zinc-900 dark:hover:bg-zinc-200"
        >
          <ExternalLink size={14} />
          {t('visit')}
        </a>
      </div>
    </div>
  );
}
