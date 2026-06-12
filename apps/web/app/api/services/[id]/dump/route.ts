import { NextRequest, NextResponse } from 'next/server';
import { spawn } from 'child_process';
import db from '@/lib/db';
import { checkRateLimit } from '@/lib/rate-limit';

export const dynamic = 'force-dynamic';

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id } = await params;

  // 1. Authorization
  const auth = req.headers.get('authorization');
  const passcode = process.env.PORTCULLIS_PASSCODE;
  if (!passcode || auth !== `Bearer ${passcode}`) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 });
  }

  // 2. Rate limit — 1 per 5 min per service
  const waitMs = checkRateLimit(id);
  if (waitMs !== null) {
    const waitMin = Math.ceil(waitMs / 60000);
    return NextResponse.json(
      { error: `Rate limited. Try again in ${waitMin} minute(s).` },
      { status: 429, headers: { 'Retry-After': String(Math.ceil(waitMs / 1000)) } }
    );
  }

  // 3. Look up service
  let service;
  try {
    service = await db.service.findUnique({ where: { id } });
  } catch (err) {
    console.error(`[dump] DB lookup failed for ${id}:`, err);
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 });
  }

  if (!service) {
    return NextResponse.json({ error: 'Service not found' }, { status: 404 });
  }

  if (!service.dbName) {
    return NextResponse.json(
      { error: 'Service has no provisioned database' },
      { status: 400 }
    );
  }

  // 4. Stream pg_dump
  const pgHost = process.env.PGHOST || 'portcullis_db';
  const pgUser = process.env.DB_USER || 'postgres';
  const pgPass = process.env.DB_PASSWORD || '';
  const args = [
    '-Fc',                     // custom format (compressed, selective restore)
    '-h', pgHost,
    '-U', pgUser,
    '-d', service.dbName,
  ];

  // Audit log
  const clientIp = req.headers.get('x-forwarded-for') || req.headers.get('x-real-ip') || 'unknown';
  console.log(`[dump] Service=${id} DB=${service.dbName} IP=${clientIp} — dump requested`);

  const child = spawn('pg_dump', args, {
    env: { ...process.env, PGPASSWORD: pgPass, PGSSLMODE: 'prefer' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  const stream = new ReadableStream({
    start(controller) {
      child.stdout.on('data', (chunk: Buffer) => controller.enqueue(chunk));
      child.stdout.on('end', () => {
        console.log(`[dump] Service=${id} — dump complete`);
        controller.close();
      });
      child.stdout.on('error', (err: Error) => {
        console.error(`[dump] Service=${id} — stdout error:`, err);
        controller.error(err);
      });
    },
  });

  // Collect stderr for error reporting
  let stderr = '';
  child.stderr.on('data', (chunk: Buffer) => { stderr += chunk.toString(); });

  child.on('exit', (code) => {
    if (code !== 0) {
      console.error(`[dump] Service=${id} — pg_dump exited ${code}: ${stderr}`);
    }
  });

  // Clean up zombie if client disconnects
  req.signal.addEventListener('abort', () => {
    console.log(`[dump] Service=${id} — client disconnected, killing pg_dump`);
    child.kill('SIGTERM');
  });

  return new NextResponse(stream, {
    status: 200,
    headers: {
      'Content-Type': 'application/octet-stream',
      'Content-Disposition': `attachment; filename="${service.dbName}-${new Date().toISOString().slice(0, 10)}.dump"`,
      'X-Dump-Service-Id': id,
      'X-Dump-Database': service.dbName,
    },
  });
}
