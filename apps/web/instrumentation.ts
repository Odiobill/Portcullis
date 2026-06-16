export async function register() {
  if (process.env.NEXT_RUNTIME === 'nodejs') {
    const { reconcileCaddyfiles } = await import('./lib/caddyfile');
    const { ALLOWED_SERVICE_TYPES, ALLOWED_TLS_MODES } = await import('./lib/caddyfile');
    const { default: db } = await import('./lib/db');

    console.log('[Instrumentation] Starting generated Caddyfile reconciliation in 5s...');
    await new Promise(resolve => setTimeout(resolve, 5000));

    try {
      const services = await db.service.findMany();
      const caddyfileServices = services.map(s => {
        const tlsMode = ALLOWED_TLS_MODES.includes(s.tlsMode as typeof ALLOWED_TLS_MODES[number])
          ? s.tlsMode as typeof ALLOWED_TLS_MODES[number]
          : 'acme';
        const serviceType = ALLOWED_SERVICE_TYPES.includes(s.serviceType as typeof ALLOWED_SERVICE_TYPES[number])
          ? s.serviceType as typeof ALLOWED_SERVICE_TYPES[number]
          : 'proxy';

        return {
          id: s.id,
          domains: s.domains,
          upstreamContainer: s.upstreamContainer,
          upstreamPort: s.upstreamPort,
          tlsMode,
          serviceType,
          staticRoot: s.staticRoot,
        };
      });

      await reconcileCaddyfiles(caddyfileServices);
      console.log('[Instrumentation] Initial generated Caddyfile reconciliation complete.');
    } catch (error) {
      // During build or if DB is not ready, this might fail.
      // We log it but don't block the startup.
      console.warn('[Instrumentation] Generated Caddyfile reconciliation skipped or failed (DB might be unreachable):', error);
    }
  }
}
