import { promises as fs } from 'node:fs';
import path from 'node:path';

const CADDY_LOG_PATH = process.env.CADDY_LOG_PATH ?? '/var/log/caddy/portcullis.log';
const BACKUP_DIR = process.env.BACKUP_DIR ?? '/backups';

export type CaddyLogLine = {
  raw: string;
  ts?: string;
  level?: string;
  logger?: string;
  msg?: string;
};

export type BackupFile = {
  name: string;
  size: number;
  modifiedAt: string;
};

function parseLogLine(raw: string): CaddyLogLine {
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    return {
      raw,
      ts: typeof parsed.ts === 'number' ? new Date(parsed.ts * 1000).toISOString() : undefined,
      level: typeof parsed.level === 'string' ? parsed.level : undefined,
      logger: typeof parsed.logger === 'string' ? parsed.logger : undefined,
      msg: typeof parsed.msg === 'string' ? parsed.msg : undefined,
    };
  } catch {
    return { raw };
  }
}

export async function readRecentCaddyLogs(limit = 50): Promise<CaddyLogLine[]> {
  try {
    const content = await fs.readFile(CADDY_LOG_PATH, 'utf8');
    return content
      .split(/\r?\n/)
      .filter(Boolean)
      .slice(-limit)
      .map(parseLogLine);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return [];
    throw error;
  }
}

export async function listBackups(): Promise<BackupFile[]> {
  try {
    const entries = await fs.readdir(BACKUP_DIR, { withFileTypes: true });
    const files = await Promise.all(entries
      .filter(entry => entry.isFile())
      .map(async entry => {
        const fullPath = path.join(BACKUP_DIR, entry.name);
        const stat = await fs.stat(fullPath);
        return {
          name: entry.name,
          size: stat.size,
          modifiedAt: stat.mtime.toISOString(),
        };
      }));

    return files.sort((a, b) => b.modifiedAt.localeCompare(a.modifiedAt));
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return [];
    throw error;
  }
}

export function backupPathForDownload(filename: string): string {
  if (!filename || filename !== path.basename(filename) || filename.includes('\0')) {
    throw new Error('Invalid backup filename');
  }

  const backupDir = path.resolve(BACKUP_DIR);
  const candidate = path.resolve(backupDir, filename);
  if (!candidate.startsWith(backupDir + path.sep)) {
    throw new Error('Invalid backup filename');
  }

  return candidate;
}
