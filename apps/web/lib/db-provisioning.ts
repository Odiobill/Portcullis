import db from './db';

/**
 * Provisions a new PostgreSQL database and user for a project.
 * @param dbName Name of the database to create
 * @param dbUser Name of the user to create
 * @param dbPass Password for the new user
 */
export async function provisionProjectDb(dbName: string, dbUser: string, dbPass: string): Promise<boolean> {
  try {
    // 1. Check if database exists
    const dbExists = await db.$queryRaw`SELECT 1 FROM pg_database WHERE datname = ${dbName}`;
    if (Array.isArray(dbExists) && dbExists.length > 0) {
      console.log(`[DB] Database "${dbName}" already exists.`);
    } else {
      // Postgres does not support parameterized identifiers in CREATE DATABASE
      // We must use unsafe execution, but we wrap in double quotes to prevent SQL injection 
      // of reserved words or spaces.
      await db.$executeRawUnsafe(`CREATE DATABASE "${dbName.replace(/"/g, '""')}"`);
      console.log(`[DB] Created database "${dbName}".`);
    }

    // 2. Check if user exists
    const userExists = await db.$queryRaw`SELECT 1 FROM pg_roles WHERE rolname = ${dbUser}`;
    if (Array.isArray(userExists) && userExists.length > 0) {
      console.log(`[DB] User "${dbUser}" already exists.`);
      // Update password just in case
      await db.$executeRawUnsafe(`ALTER USER "${dbUser.replace(/"/g, '""')}" WITH PASSWORD '${dbPass.replace(/'/g, "''")}'`);
    } else {
      await db.$executeRawUnsafe(`CREATE USER "${dbUser.replace(/"/g, '""')}" WITH PASSWORD '${dbPass.replace(/'/g, "''")}'`);
      console.log(`[DB] Created user "${dbUser}".`);
    }

    // 3. Grant privileges
    await db.$executeRawUnsafe(`GRANT ALL PRIVILEGES ON DATABASE "${dbName.replace(/"/g, '""')}" TO "${dbUser.replace(/"/g, '""')}"`);
    await db.$executeRawUnsafe(`ALTER DATABASE "${dbName.replace(/"/g, '""')}" OWNER TO "${dbUser.replace(/"/g, '""')}"`);
    
    return true;
  } catch (error) {
    console.error(`[DB] Provisioning failed:`, error);
    return false;
  }
}

/**
 * Decommissions a project database and user.
 * @param dbName Name of the database to drop
 * @param dbUser Name of the user to drop
 */
export async function decommissionProjectDb(dbName: string, dbUser: string): Promise<boolean> {
  try {
    // 1. Terminate active connections to the database
    await db.$executeRawUnsafe(`
      SELECT pg_terminate_backend(pid)
      FROM pg_stat_activity
      WHERE datname = '${dbName.replace(/'/g, "''")}'
        AND pid <> pg_backend_pid()
    `);

    // 2. Drop database
    await db.$executeRawUnsafe(`DROP DATABASE IF EXISTS "${dbName.replace(/"/g, '""')}"`);
    console.log(`[DB] Dropped database "${dbName}".`);

    // 3. Drop user (might fail if they have objects in other databases)
    await db.$executeRawUnsafe(`DROP USER IF EXISTS "${dbUser.replace(/"/g, '""')}"`);
    console.log(`[DB] Dropped user "${dbUser}".`);

    return true;
  } catch (error) {
    console.error(`[DB] Decommissioning failed:`, error);
    return false;
  }
}
