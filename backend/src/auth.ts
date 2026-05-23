import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { betterAuth } from "better-auth";
import { getMigrations } from "better-auth/db/migration";
import { bearer } from "better-auth/plugins";
import Database from "better-sqlite3";

export type BackendAuthOptions = {
  baseURL: string;
  databasePath: string;
  secret: string;
  trustedOrigins?: string[];
};

export function createBackendAuth(options: BackendAuthOptions) {
  return betterAuth(authConfig(options));
}

export type BackendAuth = ReturnType<typeof createBackendAuth>;

export async function migrateBackendAuth(options: BackendAuthOptions): Promise<void> {
  await (await getMigrations(authConfig(options))).runMigrations();
}

function authConfig(options: BackendAuthOptions) {
  mkdirSync(dirname(options.databasePath), { recursive: true });
  return {
    database: new Database(options.databasePath),
    baseURL: options.baseURL,
    secret: options.secret,
    trustedOrigins: options.trustedOrigins || [],
    emailAndPassword: {
      enabled: true,
      requireEmailVerification: false,
    },
    plugins: [bearer()],
  };
}
