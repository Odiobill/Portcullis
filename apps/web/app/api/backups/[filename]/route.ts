import { NextRequest, NextResponse } from 'next/server';
import { promises as fs } from 'node:fs';
import { backupPathForDownload } from '../../../../lib/ops-files';

export const dynamic = 'force-dynamic';

export async function GET(
  req: NextRequest,
  { params }: { params: Promise<{ filename: string }> }
) {
  const session = req.cookies.get('portcullis_session')?.value;
  if (session !== 'authenticated') {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 });
  }

  const { filename } = await params;
  let filePath: string;
  try {
    filePath = backupPathForDownload(filename);
  } catch {
    return NextResponse.json({ error: 'Invalid filename' }, { status: 400 });
  }

  try {
    const stat = await fs.stat(filePath);
    if (!stat.isFile()) {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }

    const file = await fs.readFile(filePath);
    return new NextResponse(file, {
      status: 200,
      headers: {
        'Content-Type': 'application/octet-stream',
        'Content-Length': String(stat.size),
        'Content-Disposition': `attachment; filename="${filename.replaceAll('"', '')}"`,
      },
    });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    console.error('[backups] download failed:', error);
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 });
  }
}
