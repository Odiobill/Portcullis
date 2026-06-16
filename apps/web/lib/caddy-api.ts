/**
 * ⚠️ LEGACY — Caddy Admin API wrapper.
 *
 * Route management (addRoute, deleteRoute, syncRoutes) is now handled
 * by apps/web/lib/caddyfile.ts via generated Caddyfiles.
 * These functions are KEPT temporarily for:
 *   - reload-only Admin API usage
 *   - debugging / inspection
 *
 * Do NOT use addRoute, deleteRoute, or syncRoutes in new code.
 * They will be removed in a future milestone.
 */

const CADDY_ADMIN_API = process.env.CADDY_ADMIN_API || 'http://caddy:2019';

export interface CaddyRoute {
  "@id": string;
  handle: Array<{
    handler: string;
    upstreams?: Array<{
      dial: string;
    }>;
    [key: string]: unknown;
  }>;
  match: Array<{
    host: Array<string>;
  }>;
}

/**
 * @deprecated Use deployServiceCaddyfile from lib/caddyfile instead.
 */
export async function addRoute(id: string, domains: string[], upstream: string): Promise<boolean> {
  const route = {
    "@id": id,
    match: [{ host: domains }],
    handle: [{
      handler: "reverse_proxy",
      upstreams: [{ dial: upstream }]
    }]
  };

  try {
    const response = await fetch(`${CADDY_ADMIN_API}/config/apps/http/servers/srv0/routes/0`, {
      method: 'POST',
      headers: { 
        'Content-Type': 'application/json',
        'Host': 'localhost:2019',
        'Origin': 'http://localhost:2019'
      },
      body: JSON.stringify(route)
    });

    if (!response.ok) {
      console.error(`[Caddy] Failed to add route: ${response.statusText}`);
      const body = await response.text();
      console.error(`[Caddy] Error body: ${body}`);
    }

    return response.ok;
  } catch (error) {
    console.error(`[Caddy] Network error adding route:`, error);
    return false;
  }
}

/**
 * @deprecated Use removeServiceCaddyfile from lib/caddyfile instead.
 */
export async function deleteRoute(id: string): Promise<boolean> {
  try {
    const response = await fetch(`${CADDY_ADMIN_API}/id/${id}`, {
      method: 'DELETE',
      headers: {
        'Host': 'localhost:2019',
        'Origin': 'http://localhost:2019'
      }
    });

    return response.ok;
  } catch (error) {
    console.error(`[Caddy] Network error deleting route:`, error);
    return false;
  }
}

/**
 * @deprecated Use reconcileCaddyfiles from lib/caddyfile instead.
 */
export async function syncRoutes(routes: Array<{ id: string; domains: string[]; upstream: string }>): Promise<void> {
  console.log(`[Caddy] Syncing ${routes.length} routes (deprecated)...`);

  const activeRoutes = await listRoutes();
  const activeIds = activeRoutes.map(r => r["@id"]).filter(Boolean);

  const desiredIds = routes.map(r => r.id);

  for (const id of activeIds) {
    if (!desiredIds.includes(id)) {
      await deleteRoute(id);
    }
  }

  for (const route of routes) {
    await addRoute(route.id, route.domains, route.upstream);
  }
}

/**
 * Lists all active routes from the Caddy Admin API.
 * Kept as a read-only inspection tool.
 */
export async function listRoutes(): Promise<CaddyRoute[]> {
  try {
    const response = await fetch(`${CADDY_ADMIN_API}/config/apps/http/servers/srv0/routes`, {
      headers: {
        'Host': 'localhost:2019',
        'Origin': 'http://localhost:2019'
      }
    });
    if (!response.ok) return [];
    return await response.json() || [];
  } catch (error) {
    console.error(`[Caddy] Network error listing routes:`, error);
    return [];
  }
}
