/**
 * Caddyfile engine for Portcullis v2.
 *
 * Generates, writes, validates, and reloads Caddy site blocks.
 * All generated files live under sites/generated/<id>.caddy.
 * Manual operator config lives under sites/manual/ and is never touched.
 */

import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, renameSync, unlinkSync, existsSync, mkdirSync } from 'node:fs';
import path from 'node:path';

// ── Configuration ──────────────────────────────────────────────

const CADDY_CONFIG_PATH  = process.env.CADDY_CONFIG_PATH  ?? '/etc/caddy/Caddyfile';
const CADDY_SITES_DIR    = process.env.CADDY_SITES_DIR    ?? '/etc/caddy/sites/generated';
const CADDY_ADMIN_ADDRESS = process.env.CADDY_ADMIN_ADDRESS ?? 'http://caddy:2019';

// ── Types ──────────────────────────────────────────────────────

export const ALLOWED_TLS_MODES = ['acme', 'internal', 'namecheap_tls', 'cloudflare_tls', 'route53_tls'] as const;
export type TlsMode = (typeof ALLOWED_TLS_MODES)[number];

export const ALLOWED_SERVICE_TYPES = ['proxy', 'static'] as const;
export type ServiceType = (typeof ALLOWED_SERVICE_TYPES)[number];

export type CaddyfileService = {
  id: string;
  domains: string[];
  upstreamContainer?: string | null;
  upstreamPort?: number | null;
  tlsMode: TlsMode;
  serviceType: ServiceType;
  staticRoot?: string | null;
};

// ── Regex validation constants ─────────────────────────────────

const DOCKER_NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;
const HOSTNAME_RE    = /^(\*\.)?([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/;
const SERVICE_ID_RE  = /^[a-zA-Z0-9][a-zA-Z0-9_-]*$/;

// Characters that must not appear in domain or container fields
const DISALLOWED_CHARS_RE = /[\s{}"'`;\\\n\r]/;

// ── Validation helpers (P0.6) ──────────────────────────────────

export class ValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ValidationError';
  }
}

function rejectIfContains(value: string, field: string): void {
  if (DISALLOWED_CHARS_RE.test(value)) {
    throw new ValidationError(
      `${field} contains disallowed characters (whitespace, braces, quotes, semicolons, newlines)`
    );
  }
}

/**
 * Validates a single service input and returns nothing (throws on failure).
 */
export function validateServiceInput(service: CaddyfileService): void {
  // TLS mode
  if (!ALLOWED_TLS_MODES.includes(service.tlsMode)) {
    throw new ValidationError(
      `Invalid TLS mode "${service.tlsMode}". Allowed: ${ALLOWED_TLS_MODES.join(', ')}`
    );
  }

  // Service type
  if (!ALLOWED_SERVICE_TYPES.includes(service.serviceType)) {
    throw new ValidationError(
      `Invalid service type "${service.serviceType}". Allowed: ${ALLOWED_SERVICE_TYPES.join(', ')}`
    );
  }

  // ID
  if (!service.id || typeof service.id !== 'string') {
    throw new ValidationError('Service ID is required');
  }
  if (!SERVICE_ID_RE.test(service.id)) {
    throw new ValidationError(
      `Invalid service ID "${service.id}". Only alphanumeric characters, hyphens, and underscores are allowed, must start with alphanumeric.`
    );
  }
  if (service.id.includes('\x00')) {
    throw new ValidationError('Service ID contains null byte');
  }
  rejectIfContains(service.id, 'Service ID');

  // Domains
  if (!service.domains || service.domains.length === 0) {
    throw new ValidationError('At least one domain is required');
  }
  for (const domain of service.domains) {
    if (!HOSTNAME_RE.test(domain)) {
      throw new ValidationError(`Invalid domain: "${domain}"`);
    }
    rejectIfContains(domain, `Domain "${domain}"`);
  }

  // Proxy-specific
  if (service.serviceType === 'proxy') {
    if (!service.upstreamContainer) {
      throw new ValidationError('Upstream container is required for proxy services');
    }
    if (!DOCKER_NAME_RE.test(service.upstreamContainer)) {
      throw new ValidationError(`Invalid upstream container name: "${service.upstreamContainer}"`);
    }
    rejectIfContains(service.upstreamContainer, 'Upstream container');

    if (service.upstreamPort == null) {
      throw new ValidationError('Upstream port is required for proxy services');
    }
    const port = Number(service.upstreamPort);
    if (!Number.isSafeInteger(port) || port < 1 || port > 65535) {
      throw new ValidationError(`Upstream port must be between 1 and 65535, got: ${service.upstreamPort}`);
    }
  }

  // Static-specific
  if (service.serviceType === 'static') {
    if (!service.staticRoot) {
      throw new ValidationError('Static root path is required for static services');
    }
    if (!service.staticRoot.startsWith('/srv/sites/')) {
      throw new ValidationError(`Static root must start with /srv/sites/, got: "${service.staticRoot}"`);
    }
    rejectIfContains(service.staticRoot, 'Static root');
    if (service.staticRoot.includes('\x00')) {
      throw new ValidationError('Static root contains null byte');
    }
  }
}

// ── Site block generation (P0.7) ───────────────────────────────

/**
 * Generates a Caddyfile site block for a service.
 */
export function generateSiteBlock(service: CaddyfileService): string {
  validateServiceInput(service);

  const domainsLine = service.domains.join(' ');
  const tlsImport   = service.tlsMode;
  const lines: string[] = [];

  // Header comments
  lines.push('# Generated by Portcullis. Do not edit manually.');
  lines.push(`# Service ID: ${service.id}`);
  lines.push(`# Domains: ${service.domains.join(', ')}`);
  lines.push('');

  // Site block
  lines.push(`${domainsLine} {`);
  lines.push(`  import ${tlsImport}`);

  if (service.serviceType === 'proxy') {
    lines.push(`  reverse_proxy ${service.upstreamContainer}:${service.upstreamPort}`);
  } else {
    lines.push(`  root * ${service.staticRoot}`);
    lines.push('  file_server');
  }

  lines.push('}');
  lines.push('');

  return lines.join('\n');
}

// ── Caddy command helpers ──────────────────────────────────────

/** Extract stderr/stdout from a child_process error safely. */
function execErrorOutput(err: unknown): string {
  if (!(err instanceof Error)) return String(err);
  const e = err as { stderr?: Buffer | string; stdout?: Buffer | string };
  if (e.stderr) return Buffer.isBuffer(e.stderr) ? e.stderr.toString() : e.stderr;
  if (e.stdout) return Buffer.isBuffer(e.stdout) ? e.stdout.toString() : e.stdout;
  return err.message;
}

/**
 * Runs `caddy validate` on the full Caddyfile.
 * Throws on validation failure with stderr included.
 */
export function validateCaddyConfig(): void {
  try {
    execFileSync(
      'caddy',
      ['validate', '--config', CADDY_CONFIG_PATH, '--adapter', 'caddyfile'],
      { stdio: 'pipe', timeout: 30_000 }
    );
  } catch (err: unknown) {
    throw new Error(`Caddy validation failed:\n${execErrorOutput(err)}`);
  }
}

/**
 * Runs `caddy reload` against the running Caddy instance.
 * Throws on reload failure.
 */
export function reloadCaddy(): void {
  try {
    execFileSync(
      'caddy',
      ['reload', '--config', CADDY_CONFIG_PATH, '--adapter', 'caddyfile', '--address', CADDY_ADMIN_ADDRESS],
      { stdio: 'pipe', timeout: 30_000 }
    );
  } catch (err: unknown) {
    throw new Error(`Caddy reload failed:\n${execErrorOutput(err)}`);
  }
}

// ── File path helpers ──────────────────────────────────────────

function generatedFilePath(serviceId: string): string {
  const resolvedDir = path.resolve(CADDY_SITES_DIR);
  const resolvedFile = path.resolve(resolvedDir, `${serviceId}.caddy`);

  if (!resolvedFile.startsWith(resolvedDir + path.sep)) {
    throw new ValidationError(
      `Generated Caddyfile path escapes generated directory: ${serviceId}`
    );
  }

  return resolvedFile;
}

function tempFilePath(serviceId: string): string {
  return path.join(CADDY_SITES_DIR, `${serviceId}.caddy.tmp`);
}

function backupFilePath(serviceId: string): string {
  return path.join(CADDY_SITES_DIR, `${serviceId}.caddy.bak`);
}

function ensureSitesDir(): void {
  if (!existsSync(CADDY_SITES_DIR)) {
    mkdirSync(CADDY_SITES_DIR, { recursive: true });
  }
}

// ── Atomic deploy & remove (P0.8) ──────────────────────────────

/**
 * Deploys a generated Caddyfile for a service.
 *
 * 1. Validate service input
 * 2. Generate block text
 * 3. Read previous file if it exists
 * 4. Write <id>.caddy.tmp
 * 5. Rename to <id>.caddy
 * 6. Validate full Caddy config
 * 7. Reload Caddy
 * 8. If validate/reload fails, restore previous file
 */
export async function deployServiceCaddyfile(service: CaddyfileService): Promise<void> {
  ensureSitesDir();

  // 1-2. Validate input and generate
  const content = generateSiteBlock(service);
  const filePath = generatedFilePath(service.id);
  const tmpPath = tempFilePath(service.id);

  // 3. Read previous file
  let previousContent: string | null = null;
  if (existsSync(filePath)) {
    previousContent = readFileSync(filePath, 'utf-8');
  }

  try {
    // 4. Write to temp file
    writeFileSync(tmpPath, content, 'utf-8');

    // 5. Atomic rename
    renameSync(tmpPath, filePath);

    // 6. Validate full config
    validateCaddyConfig();

    // 7. Reload
    reloadCaddy();
  } catch (err: unknown) {
    // 8. Rollback: restore previous or remove new file
    try {
      if (previousContent !== null) {
        writeFileSync(filePath, previousContent, 'utf-8');
        // Try to validate/reload the restored config
        validateCaddyConfig();
        reloadCaddy();
      } else {
        // No previous file — remove what we just wrote
        if (existsSync(filePath)) {
          unlinkSync(filePath);
        }
        // Validate/reload without this service
        validateCaddyConfig();
        reloadCaddy();
      }
    } catch (rollbackErr: unknown) {
      console.error('[Caddyfile] Rollback also failed:', rollbackErr instanceof Error ? rollbackErr.message : String(rollbackErr));
    }

    // Always throw original error
    throw err;
  } finally {
    // Clean up temp file if it still exists
    if (existsSync(tmpPath)) {
      try { unlinkSync(tmpPath); } catch { /* ignore */ }
    }
  }
}

/**
 * Removes a generated Caddyfile for a service with rollback safety.
 *
 * 1. Rename <id>.caddy to <id>.caddy.bak
 * 2. Validate
 * 3. Reload
 * 4. If validation/reload fails, restore .bak
 * 5. Delete .bak
 */
export async function removeServiceCaddyfile(serviceId: string): Promise<void> {
  const filePath = generatedFilePath(serviceId);
  const bakPath  = backupFilePath(serviceId);

  if (!existsSync(filePath)) {
    return; // Nothing to remove
  }

  // 1. Rename to .bak (preserve for rollback)
  renameSync(filePath, bakPath);

  try {
    // 2-3. Validate and reload
    validateCaddyConfig();
    reloadCaddy();

    // 4. Success — delete .bak
    unlinkSync(bakPath);
  } catch (err: unknown) {
    // 5. Rollback: restore .bak
    try {
      renameSync(bakPath, filePath);
      validateCaddyConfig();
      reloadCaddy();
    } catch (rollbackErr: unknown) {
      console.error('[Caddyfile] Rollback restore also failed:', rollbackErr instanceof Error ? rollbackErr.message : String(rollbackErr));
    }

    throw err;
  }
}

/**
 * Validates the Caddy config without deploying anything.
 * Returns true on success, throws on failure with details.
 */
export async function checkCaddyConfig(): Promise<{ valid: boolean; error?: string }> {
  try {
    validateCaddyConfig();
    return { valid: true };
  } catch (err: unknown) {
    return { valid: false, error: execErrorOutput(err) };
  }
}

/**
 * Reloads Caddy (no validation — caller should validate first).
 */
export async function reloadCaddyAsync(): Promise<void> {
  reloadCaddy();
}

// ── TLS mode availability detection (P2) ───────────────────────

export type TlsModeOption = {
  value: TlsMode;
  label: string;
  available: boolean;
};

/**
 * Detects which TLS modes are available based on environment variables.
 * Returns all modes with their availability status.
 */
export function availableTlsModes(): TlsModeOption[] {
  const modes: TlsModeOption[] = [
    { value: 'acme', label: "HTTP-01, Let's Encrypt", available: true },
    { value: 'internal', label: 'Internal CA', available: true },
  ];

  if (process.env.NAMECHEAP_API_KEY && process.env.NAMECHEAP_API_USER) {
    modes.push({ value: 'namecheap_tls', label: 'DNS-01, Namecheap', available: true });
  } else {
    modes.push({ value: 'namecheap_tls', label: 'DNS-01, Namecheap', available: false });
  }

  if (process.env.CLOUDFLARE_API_TOKEN) {
    modes.push({ value: 'cloudflare_tls', label: 'DNS-01, Cloudflare', available: true });
  } else {
    modes.push({ value: 'cloudflare_tls', label: 'DNS-01, Cloudflare', available: false });
  }

  if (process.env.CADDY_DNS_PROVIDER === 'route53') {
    modes.push({ value: 'route53_tls', label: 'DNS-01, Route53', available: true });
  } else {
    modes.push({ value: 'route53_tls', label: 'DNS-01, Route53', available: false });
  }

  return modes;
}

/**
 * Returns only the modes that are currently available (env vars configured).
 */
export function getConfiguredTlsModes(): TlsModeOption[] {
  return availableTlsModes().filter(m => m.available);
}

/**
 * Returns the default TLS mode to pre-select.
 * Priority: namecheap_tls > acme > first available
 */
export function getDefaultTlsMode(): TlsMode {
  const configured = getConfiguredTlsModes();
  if (configured.find(m => m.value === 'namecheap_tls' && m.available)) return 'namecheap_tls';
  if (configured.find(m => m.value === 'acme' && m.available)) return 'acme';
  return configured.length > 0 ? configured[0].value : 'acme';
}

/**
 * Validates that a TLS mode is both in the allowed list AND currently configured.
 * Returns an error message string, or null if valid.
 */
export function validateTlsModeConfigured(tlsMode: string): string | null {
  if (!ALLOWED_TLS_MODES.includes(tlsMode as TlsMode)) {
    return `Invalid TLS mode "${tlsMode}". Allowed: ${ALLOWED_TLS_MODES.join(', ')}`;
  }
  const configured = getConfiguredTlsModes();
  if (!configured.find(m => m.value === tlsMode)) {
    return `TLS mode "${tlsMode}" is not configured on this server. Check environment variables.`;
  }
  return null;
}

// ── Batch reconciliation (P1.5) ────────────────────────────────

/**
 * Batch-reconciles generated Caddyfiles against the current DB state.
 *
 * 1. Reads all services from DB (caller provides the list).
 * 2. Generates/updates missing or changed files under `sites/generated`.
 * 3. Removes generated files whose service ID no longer exists in DB.
 * 4. Validates and reloads once after the batch.
 *
 * Never touches `sites/manual`.
 */
export async function reconcileCaddyfiles(services: CaddyfileService[]): Promise<void> {
  ensureSitesDir();

  const { readdirSync } = await import('node:fs');

  // Build a Set of desired service IDs
  const desiredIds = new Set(services.map(s => s.id));

  // Track which files we need to write/update
  const writeTargets = new Map<string, string>(); // path -> content

  for (const service of services) {
    const content = generateSiteBlock(service);
    const filePath = generatedFilePath(service.id);

    // Only write if content has changed
    let existingContent: string | null = null;
    try {
      existingContent = readFileSync(filePath, 'utf-8');
    } catch {
      // File doesn't exist — needs creation
    }

    if (existingContent !== content) {
      writeTargets.set(service.id, content);
    }
  }

  // Find generated files that should no longer exist
  let staleFiles: string[] = [];
  try {
    const allFiles = readdirSync(CADDY_SITES_DIR);
    staleFiles = allFiles
      .filter(f => f.endsWith('.caddy') && !f.endsWith('.bak') && !f.endsWith('.tmp'))
      .filter(f => {
        const id = f.replace(/\.caddy$/, '');
        return !desiredIds.has(id);
      });
  } catch {
    // Directory may not exist yet — no stale files to clean
  }

  const hasChanges = writeTargets.size > 0 || staleFiles.length > 0;

  if (!hasChanges) {
    return; // Nothing to do
  }

  // Apply writes atomically
  for (const [id, content] of writeTargets) {
    const filePath = generatedFilePath(id);
    const tmpPath = tempFilePath(id);
    writeFileSync(tmpPath, content, 'utf-8');
    renameSync(tmpPath, filePath);
  }

  // Remove stale files
  for (const staleFile of staleFiles) {
    try {
      unlinkSync(path.join(CADDY_SITES_DIR, staleFile));
    } catch {
      // Best-effort removal
    }
  }

  // Single validate + reload for the whole batch
  validateCaddyConfig();
  reloadCaddy();
}
