import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { betterAuth } from "better-auth";
import { getMigrations } from "better-auth/db/migration";
import { bearer, deviceAuthorization } from "better-auth/plugins";
import Database from "better-sqlite3";

export type BackendAuthOptions = {
  baseURL: string;
  databasePath: string;
  secret: string;
  trustedOrigins?: string[];
  deviceVerificationURL?: string;
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
  const trustedOrigins = new Set((options.trustedOrigins || []).map((origin) => origin.trim()).filter(Boolean));
  const deviceOrigin = urlOrigin(options.deviceVerificationURL);
  if (deviceOrigin) trustedOrigins.add(deviceOrigin);
  return {
    database: new Database(options.databasePath),
    baseURL: options.baseURL,
    secret: options.secret,
    trustedOrigins: [...trustedOrigins],
    emailAndPassword: {
      enabled: true,
      requireEmailVerification: false,
    },
    plugins: [
      bearer(),
      deviceAuthorization({
        expiresIn: "15m",
        interval: "3s",
        schema: {},
        verificationUri: options.deviceVerificationURL || "/device",
        validateClient: (clientID: string) => clientID === "yolobox-cli",
      }),
    ],
  };
}

function urlOrigin(value: string | undefined): string {
  if (!value) return "";
  try {
    return new URL(value).origin;
  } catch {
    return "";
  }
}
