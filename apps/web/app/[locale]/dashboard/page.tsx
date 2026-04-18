import { getTranslations, setRequestLocale } from 'next-intl/server';
import { headers } from 'next/headers';
import db from '../../../lib/db';
import ServiceCard from '../../../components/ServiceCard';
import RegisterServiceForm from '../../../components/RegisterServiceForm';
import { LayoutDashboard } from 'lucide-react';
import { Service } from '@prisma/client';

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
      <header className="mb-12 flex flex-col items-start gap-2">
        <div className="flex items-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-900 text-white dark:bg-zinc-50 dark:text-zinc-900 shadow-lg dark:shadow-zinc-900/40">
            <LayoutDashboard size={24} />
          </div>
          <div>
            <h1 className="text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50">
              {t('welcome')}
            </h1>
            <p className="text-sm font-medium text-zinc-400 dark:text-zinc-500">
              PORTCULLIS v0.1.0-alpha
            </p>
          </div>
        </div>
        <p className="mt-2 max-w-xl text-lg text-zinc-500 dark:text-zinc-400">
          {t('subtitle')}
        </p>
      </header>

      <div className="grid grid-cols-1 gap-12 lg:grid-cols-3">
        {/* Registration Section */}
        <div className="lg:col-span-1">
          <div className="sticky top-8 rounded-3xl border border-zinc-200 bg-white p-8 shadow-sm transition-all hover:shadow-md dark:border-zinc-800 dark:bg-zinc-900">
            <RegisterServiceForm />
          </div>
        </div>

        {/* List Section */}
        <div className="lg:col-span-2">
          {services.length === 0 ? (
            <div className="flex min-h-[400px] flex-col items-center justify-center rounded-3xl border-2 border-dashed border-zinc-200 p-12 text-center dark:border-zinc-800">
              <div className="mb-4 rounded-full bg-zinc-100 p-4 dark:bg-zinc-800">
                <LayoutDashboard size={32} className="text-zinc-400 dark:text-zinc-500" />
              </div>
              <p className="text-lg font-medium text-zinc-500 dark:text-zinc-400">{t('noServices')}</p>
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
