'use server';

import db from '../../../lib/db';
import {
  deployServiceCaddyfile,
  removeServiceCaddyfile,
  validateTlsModeConfigured,
  validateServiceInput,
  validateCaddyConfig,
  reloadCaddy,
  type CaddyfileService,
  type ServiceType,
  type TlsMode,
} from '../../../lib/caddyfile';
import { provisionProjectDb, decommissionProjectDb } from '../../../lib/db-provisioning';
import { revalidatePath } from 'next/cache';

export type ActionResponse = {
  success: boolean;
  message: string;
  data?: {
    id?: string;
    dbName?: string | null;
    dbUser?: string | null;
    dbPassword?: string | null;
  };
};

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

type ParsedServiceInput = {
  domains: string[];
  upstreamContainer: string | null;
  upstreamPort: number | null;
  tlsMode: TlsMode;
  serviceType: ServiceType;
  staticRoot: string | null;
};

function parseDomains(domainsRaw: string | null): string[] {
  if (!domainsRaw) return [];
  return domainsRaw.split(',').map(d => d.trim()).filter(Boolean);
}

function parseServiceInput(formData: FormData): ParsedServiceInput {
  const domainsRaw = formData.get('domain') as string | null;
  const domains = parseDomains(domainsRaw);
  if (domains.length === 0) {
    throw new Error('At least one valid domain is required');
  }

  const tlsMode = ((formData.get('tlsMode') as string) || 'acme') as TlsMode;
  const tlsModeValidation = validateTlsModeConfigured(tlsMode);
  if (tlsModeValidation) {
    throw new Error(tlsModeValidation);
  }

  const serviceType = ((formData.get('serviceType') as string) || 'proxy') as ServiceType;
  if (serviceType !== 'proxy' && serviceType !== 'static') {
    throw new Error(`Invalid service type "${serviceType}"`);
  }

  const primaryDomain = domains[0];
  const upstreamContainerRaw = (formData.get('upstreamContainer') as string | null)?.trim() || null;
  const upstreamPortRaw = (formData.get('upstreamPort') as string | null)?.trim() || '';
  const staticRootRaw = (formData.get('staticRoot') as string | null)?.trim() || '';

  const upstreamPort = serviceType === 'proxy'
    ? (upstreamPortRaw ? Number.parseInt(upstreamPortRaw, 10) : 3000)
    : null;

  const parsed: ParsedServiceInput = {
    domains,
    upstreamContainer: serviceType === 'proxy' ? upstreamContainerRaw : null,
    upstreamPort,
    tlsMode,
    serviceType,
    staticRoot: serviceType === 'static' ? (staticRootRaw || `/srv/sites/${primaryDomain}`) : null,
  };

  validateServiceInput({
    id: 'validation-placeholder',
    ...parsed,
  });

  return parsed;
}

function toCaddyfileService(service: {
  id: string;
  domains: string[];
  upstreamContainer: string | null;
  upstreamPort: number | null;
  tlsMode: string;
  serviceType: string;
  staticRoot: string | null;
}): CaddyfileService {
  return {
    id: service.id,
    domains: service.domains,
    upstreamContainer: service.upstreamContainer,
    upstreamPort: service.upstreamPort,
    tlsMode: (service.tlsMode || 'acme') as TlsMode,
    serviceType: (service.serviceType || 'proxy') as ServiceType,
    staticRoot: service.staticRoot,
  };
}

export async function registerService(prevState: ActionResponse | null, formData: FormData): Promise<ActionResponse> {
  void prevState;
  let parsed: ParsedServiceInput;
  try {
    parsed = parseServiceInput(formData);
  } catch (error: unknown) {
    return { success: false, message: errorMessage(error, 'Invalid service input') };
  }

  const primaryDomain = parsed.domains[0];
  const provisionDb = formData.get('provisionDb') === 'on';

  // Custom DB settings
  const customDbName = formData.get('dbName') as string;
  const customDbUser = formData.get('dbUser') as string;
  const customDbPass = formData.get('dbPassword') as string;

  try {
    // 1. Save to DB
    const service = await db.service.create({
      data: {
        domains: parsed.domains,
        upstreamContainer: parsed.upstreamContainer,
        upstreamPort: parsed.upstreamPort,
        tlsMode: parsed.tlsMode,
        serviceType: parsed.serviceType,
        staticRoot: parsed.staticRoot,
        dbName: provisionDb
          ? (customDbName || `db_${primaryDomain.replace(/[^a-zA-Z0-9]/g, '_')}`.toLowerCase())
          : null,
        dbUser: provisionDb
          ? (customDbUser || `u_${primaryDomain.replace(/[^a-zA-Z0-9]/g, '_')}`.toLowerCase().slice(0, 16))
          : null,
      }
    });

    // 2. Deploy generated Caddyfile
    try {
      await deployServiceCaddyfile(toCaddyfileService(service));
    } catch (caddyError: unknown) {
      // Caddy deployment failed, rollback: delete the DB row
      await db.service.delete({ where: { id: service.id } }).catch(() => {});
      return { success: false, message: `Caddy deployment failed: ${errorMessage(caddyError, 'Unknown error')}` };
    }

    // 3. Provision DB if requested
    let dbPassword = null;
    if (provisionDb && service.dbName && service.dbUser) {
      dbPassword = customDbPass || Math.random().toString(36).slice(-12);
      try {
        await provisionProjectDb(service.dbName, service.dbUser, dbPassword);
      } catch (dbError: unknown) {
        // DB provisioning failed, rollback: remove Caddyfile and delete DB row
        await removeServiceCaddyfile(service.id).catch(() => {});
        await db.service.delete({ where: { id: service.id } }).catch(() => {});
        return { success: false, message: `DB provisioning failed: ${errorMessage(dbError, 'Unknown error')}` };
      }
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
      }
    };
  } catch (error: unknown) {
    console.error('[Action] Registration failed:', error);
    return { success: false, message: errorMessage(error, 'Failed to register service') };
  }
}

export async function updateService(id: string, formData: FormData): Promise<ActionResponse> {
  let parsed: ParsedServiceInput;
  try {
    parsed = parseServiceInput(formData);
  } catch (error: unknown) {
    return { success: false, message: errorMessage(error, 'Invalid service input') };
  }

  try {
    const previous = await db.service.findUnique({ where: { id } });
    if (!previous) {
      return { success: false, message: 'Service not found' };
    }

    validateServiceInput({ id, ...parsed });

    const updated = await db.service.update({
      where: { id },
      data: {
        domains: parsed.domains,
        upstreamContainer: parsed.upstreamContainer,
        upstreamPort: parsed.upstreamPort,
        tlsMode: parsed.tlsMode,
        serviceType: parsed.serviceType,
        staticRoot: parsed.staticRoot,
      },
    });

    try {
      await deployServiceCaddyfile(toCaddyfileService(updated));
    } catch (caddyError: unknown) {
      // Restore DB values if the regenerated Caddyfile cannot be validated or reloaded.
      await db.service.update({
        where: { id },
        data: {
          domains: previous.domains,
          upstreamContainer: previous.upstreamContainer,
          upstreamPort: previous.upstreamPort,
          tlsMode: previous.tlsMode,
          serviceType: previous.serviceType,
          staticRoot: previous.staticRoot,
        },
      }).catch((restoreError: unknown) => {
        console.error('[Action] Failed to restore service after Caddy deploy failure:', restoreError);
      });

      return { success: false, message: `Caddy update failed, service restored: ${errorMessage(caddyError, 'Unknown error')}` };
    }

    revalidatePath('/[locale]/dashboard', 'page');
    return { success: true, message: 'Service updated successfully' };
  } catch (error: unknown) {
    console.error('[Action] Update failed:', error);
    return { success: false, message: errorMessage(error, 'Failed to update service') };
  }
}

export async function removeService(id: string): Promise<ActionResponse> {
  try {
    const service = await db.service.findUnique({ where: { id } });
    if (!service) {
      return { success: false, message: 'Service not found' };
    }

    // 1. Remove generated Caddyfile (rollback-safe)
    await removeServiceCaddyfile(id);

    // 2. Decommission DB if it was provisioned
    if (service.dbName && service.dbUser) {
      await decommissionProjectDb(service.dbName, service.dbUser);
    }

    // 3. Delete from DB
    await db.service.delete({ where: { id } });

    revalidatePath('/[locale]/dashboard', 'page');

    return { success: true, message: 'Service removed successfully' };
  } catch (error: unknown) {
    console.error('[Action] Removal failed:', error);
    return { success: false, message: errorMessage(error, 'Failed to remove service') };
  }
}

export async function reloadCaddyConfig(prevState?: ActionResponse | null): Promise<ActionResponse> {
  void prevState;

  try {
    validateCaddyConfig();
    reloadCaddy();
    return { success: true, message: 'Caddy config validated and reloaded successfully' };
  } catch (error: unknown) {
    console.error('[Action] Caddy reload failed:', error);
    return { success: false, message: errorMessage(error, 'Failed to reload Caddy') };
  }
}
