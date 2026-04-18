export async function register() {
  if (process.env.NEXT_RUNTIME === 'nodejs') {
    const { syncRoutes } = await import('./lib/caddy-api');
    const { default: db } = await import('./lib/db');

    console.log('[Instrumentation] Starting route synchronization in 5s...');
    await new Promise(resolve => setTimeout(resolve, 5000));

    try {
      const services = await db.service.findMany();
      const routes = services.map(s => ({
        id: s.id,
        domain: s.domain,
        upstream: `${s.upstreamContainer}:${s.upstreamPort}`
      }));

      await syncRoutes(routes);
      console.log('[Instrumentation] Initial route sync complete.');
    } catch (error) {
      // During build or if DB is not ready, this might fail.
      // We log it but don't block the startup.
      console.warn('[Instrumentation] Route sync skipped or failed (DB might be unreachable):', error);
    }
  }
}
