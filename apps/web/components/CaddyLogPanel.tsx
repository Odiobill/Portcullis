import { readRecentCaddyLogs } from '../lib/ops-files';
import CaddyLogViewer from './CaddyLogViewer';

export default async function CaddyLogPanel() {
  let logs = [] as Awaited<ReturnType<typeof readRecentCaddyLogs>>;
  let error: string | null = null;

  try {
    logs = await readRecentCaddyLogs(50);
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to read Caddy logs';
  }

  return <CaddyLogViewer logs={logs} error={error} />;
}
