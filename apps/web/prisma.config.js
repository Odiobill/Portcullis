// CJS version of prisma.config for production runner.
// This file is used instead of prisma.config.ts which requires TypeScript support.
// It reads DATABASE_URL from the environment, exactly like the TS version.
'use strict';

const { defineConfig } = require('./node_modules/prisma/config.js');

module.exports = defineConfig({
  schema: 'prisma/schema.prisma',
  migrations: {
    path: 'prisma/migrations',
  },
  datasource: {
    url: process.env.DATABASE_URL,
  },
});
