'use server';

import db from '../../../lib/db';
import { addRoute, deleteRoute } from '../../../lib/caddy-api';
import { provisionProjectDb, decommissionProjectDb } from '../../../lib/db-provisioning';
import { revalidatePath } from 'next/cache';

export type ActionResponse = {
  success: boolean;
  message: string;
  data?: any;
};

export async function registerService(prevState: any, formData: FormData): Promise<ActionResponse> {
  const domainsRaw = formData.get('domain') as string;
  const upstreamContainer = formData.get('upstreamContainer') as string;
  const upstreamPortStr = formData.get('upstreamPort') as string;
  const provisionDb = formData.get('provisionDb') === 'on';

  if (!domainsRaw || !upstreamContainer) {
    return { success: false, message: 'Domains and Upstream Container are required' };
  }

  // Parse multiple domains
  const domains = domainsRaw.split(',').map(d => d.trim()).filter(Boolean);
  if (domains.length === 0) {
    return { success: false, message: 'At least one valid domain is required' };
  }

  // Default port to 3000 if not specified
  const upstreamPort = upstreamPortStr ? parseInt(upstreamPortStr) : 3000;
  const primaryDomain = domains[0];

  try {
    // 1. Save to DB
    const service = await db.service.create({
      data: {
        domains,
        upstreamContainer,
        upstreamPort,
        dbName: provisionDb ? `db_${primaryDomain.replace(/[^a-zA-Z0-9]/g, '_')}`.toLowerCase() : null,
        dbUser: provisionDb ? `u_${primaryDomain.replace(/[^a-zA-Z0-9]/g, '_')}`.toLowerCase().slice(0, 16) : null,
      }
    });

    // 2. Add Caddy Route
    const upstream = `${upstreamContainer}:${upstreamPort}`;
    const caddySuccess = await addRoute(service.id, domains, upstream);

    // 3. Provision DB if requested
    let dbPassword = null;
    if (provisionDb && service.dbName && service.dbUser) {
      dbPassword = Math.random().toString(36).slice(-12);
      await provisionProjectDb(service.dbName, service.dbUser, dbPassword);
    }

    revalidatePath('/[locale]/dashboard', 'page');
    
    return { 
      success: true, 
      message: 'Service registered successfully',
      data: {
        id: service.id,
        dbName: service.dbName,
        dbUser: service.dbUser,
        dbPassword: dbPassword,
        caddySynced: caddySuccess
      }
    };
  } catch (error: any) {
    console.error('[Action] Registration failed:', error);
    return { success: false, message: error.message || 'Failed to register service' };
  }
}

export async function removeService(id: string): Promise<ActionResponse> {
  try {
    const service = await db.service.findUnique({ where: { id } });
    if (!service) {
      return { success: false, message: 'Service not found' };
    }

    // 1. Remove Caddy Route
    await deleteRoute(id);

    // 2. Decommission DB if it was provisioned
    if (service.dbName && service.dbUser) {
      await decommissionProjectDb(service.dbName, service.dbUser);
    }

    // 3. Delete from DB
    await db.service.delete({ where: { id } });

    revalidatePath('/[locale]/dashboard', 'page');
    
    return { success: true, message: 'Service removed successfully' };
  } catch (error: any) {
    console.error('[Action] Removal failed:', error);
    return { success: false, message: error.message || 'Failed to remove service' };
  }
}
