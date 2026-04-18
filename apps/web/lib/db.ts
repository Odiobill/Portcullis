import { PrismaClient } from '@prisma/client';

import { PrismaPg } from '@prisma/adapter-pg';
import { Pool } from 'pg';

const prismaClientSingleton = () => {
  const pool = new Pool({ connectionString: process.env.DATABASE_URL });
  const adapter = new PrismaPg(pool);
  return new PrismaClient({ adapter });
};

declare global {
  var prisma: undefined | ReturnType<typeof prismaClientSingleton>;
}

// During Next.js build, we want to avoid initializing the DB connection.
// We use a lazy initialization pattern.
let _db: ReturnType<typeof prismaClientSingleton> | undefined;

const db = new Proxy({} as ReturnType<typeof prismaClientSingleton>, {
  get(target, prop, receiver) {
    if (prop === 'then') return undefined;
    if (!_db) {
      _db = globalThis.prisma ?? prismaClientSingleton();
      if (process.env.NODE_ENV !== 'production') globalThis.prisma = _db;
    }
    return Reflect.get(_db, prop, receiver);
  }
});

export default db;
