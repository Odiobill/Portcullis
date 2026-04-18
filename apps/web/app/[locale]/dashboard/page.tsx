import { getTranslations, setRequestLocale } from 'next-intl/server';
import { headers } from 'next/headers';
import db from '../../../lib/db';
import ServiceCard from '../../../components/ServiceCard';
import RegisterServiceForm from '../../../components/RegisterServiceForm';
import LanguageSwitcher from '../../../components/LanguageSwitcher';
import { Service } from '@prisma/client';
import Image from 'next/image';

export const dynamic = 'force-dynamic';

export default async function DashboardPage({
  params
}: {
  params: Promise<{ locale: string }>;
}) {
  await headers(); // Force dynamic execution
  const { locale } = await params;
  setRequestLocale(locale);
  const t = await getTranslations('Dashboard');

  let services: Service[] = [];
  try {
    services = await db.service.findMany({
      orderBy: { createdAt: 'desc' }
    });
  } catch (error) {
    // During Next.js build (static worker), the DB might not be reachable.
    // We catch the error to allow the build to complete.
    console.warn('[Dashboard] Could not fetch services, possibly during build:', error);
  }

  return (
    <div className="mx-auto w-full max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
      {/* Header */}
      <header className="mb-12 flex flex-col items-start justify-between gap-6 sm:flex-row sm:items-center">
        <div className="flex items-center gap-5">
          <div className="relative h-16 w-16 overflow-hidden rounded-2xl border border-white/10 bg-card p-2 shadow-2xl shadow-accent-cyan/10">
            <Image 
              src="/logo.png" 
              alt="Portcullis" 
              fill 
              className="object-contain p-1"
            />
          </div>
          <div>
            <h1 className="text-4xl font-black tracking-tight text-white sm:text-5xl">
              <span className="bg-gradient-to-r from-accent-purple to-accent-cyan bg-clip-text text-transparent">
                PORTCULLIS
              </span>
            </h1>
            <p className="text-xs font-bold tracking-[0.2em] text-accent-cyan uppercase">
              {t('subtitle')}
            </p>
          </div>
        </div>

        <LanguageSwitcher />
      </header>

      <div className="grid grid-cols-1 gap-12 lg:grid-cols-3">
        {/* Registration Section */}
        <div className="lg:col-span-1">
          <div className="sticky top-8 rounded-3xl border border-white/5 bg-card/50 p-8 backdrop-blur-xl shadow-2xl transition-all hover:border-white/10">
            <RegisterServiceForm />
          </div>
        </div>

        {/* List Section */}
        <div className="lg:col-span-2">
          {services.length === 0 ? (
            <div className="flex min-h-[400px] flex-col items-center justify-center rounded-3xl border border-white/5 bg-card/20 p-12 text-center backdrop-blur-sm">
              <div className="mb-4 rounded-full bg-white/5 p-4">
                <div className="h-8 w-8 text-accent-cyan opacity-20">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                  </svg>
                </div>
              </div>
              <p className="text-lg font-medium text-white/40">{t('noServices')}</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
              {services.map((service) => (
                <ServiceCard key={service.id} service={service} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
