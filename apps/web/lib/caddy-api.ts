/**
 * Canonical wrapper for the Caddy Admin API.
 * This is the ONLY place where the Caddy API should be called.
 */

const CADDY_ADMIN_API = process.env.CADDY_ADMIN_API || 'http://caddy:2019';

export interface CaddyRoute {
  "@id": string;
  handle: Array<{
    handler: string;
    upstreams?: Array<{
      dial: string;
    }>;
    [key: string]: any;
  }>;
  match: Array<{
    host: Array<string>;
  }>;
}

/**
 * Adds a new reverse proxy route to Caddy.
 * @param id Unique identifier for the route (used in @id)
 * @param domains The domain names to map
 * @param upstream The upstream address (e.g., "container_name:port")
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
 * Deletes a route by its @id.
 * @param id The unique identifier of the route
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
 * Lists all active routes.
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

/**
 * Syncs the Caddy state with a provided list of routes.
 * Caddy state is ephemeral; call this on startup.
 */
export async function syncRoutes(routes: Array<{ id: string; domains: string[]; upstream: string }>): Promise<void> {
  console.log(`[Caddy] Syncing ${routes.length} routes...`);

  // Simple approach: list existing routes with IDs, remove those not in the list, then add/update
  const activeRoutes = await listRoutes();
  const activeIds = activeRoutes.map(r => r["@id"]).filter(Boolean);

  const desiredIds = routes.map(r => r.id);

  // Delete routes that should not exist
  for (const id of activeIds) {
    if (!desiredIds.includes(id)) {
      await deleteRoute(id);
    }
  }

  // Add/Update missing or changed routes
  for (const route of routes) {
    await addRoute(route.id, route.domains, route.upstream);
  }
}
