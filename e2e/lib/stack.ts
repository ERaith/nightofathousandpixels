// Where the suite points, and where the numbers come from.
//
// Several agents run their own stack on this laptop at once, so every port is
// derived from AGENT_SLOT. That arithmetic lives in the Makefile and is asked
// for here rather than re-derived: two copies of it would agree right up until
// somebody changed one, and the symptom would be a suite testing another
// agent's application.

import { execFileSync } from "node:child_process";
import * as path from "node:path";

export interface StackConfig {
  /** Repository root, i.e. the directory the Makefile lives in. */
  repoRoot: string;
  /** Which agent slot's stack this is. */
  slot: string;
  /** The application under test, e.g. http://localhost:8175 */
  baseURL: string;
  /** The OIDC issuer, e.g. http://localhost:9175/oidc */
  issuerURL: string;
  /** The provider's origin, i.e. issuerURL without the /oidc path. */
  oidcOrigin: string;
  clientID: string;
  clientSecret: string;
  composeProject: string;
}

export const repoRoot = path.resolve(__dirname, "..", "..");

/** Where the global setup leaves the signed-in browser state. */
export const storageStatePath = path.join(__dirname, "..", ".auth", "session.json");

/** Where the global setup records what it found, for the specs to read. */
export const harnessStatePath = path.join(__dirname, "..", ".auth", "harness.json");

/** What the global setup learned while bringing the stack up. */
export interface HarnessState {
  /** True when the application mounts a sign-in route and setup completed it. */
  authWired: boolean;
  /** The status GET /auth/login returned during setup. */
  loginStatus: number;
  /** The email the stored session belongs to, when there is one. */
  email: string | null;
}

let cached: StackConfig | undefined;

/**
 * Resolve the stack's addresses, preferring anything already in the
 * environment (which is how `make e2e` passes them) and falling back to asking
 * make directly (which is what makes a bare `npx playwright test` work).
 */
export function stack(): StackConfig {
  if (cached) return cached;

  const fromMake = needsMake() ? readMakeEnv() : {};
  const value = (key: string): string => {
    const v = process.env[key] ?? fromMake[key];
    if (!v) {
      throw new Error(
        `e2e: ${key} is not set and \`make e2e-env\` did not report it. ` +
          `Run the suite through \`make e2e\`, or from a checkout with a .env file.`,
      );
    }
    return v;
  };

  const issuerURL = value("E2E_ISSUER_URL");
  cached = {
    repoRoot,
    slot: process.env.AGENT_SLOT ?? fromMake.AGENT_SLOT ?? "0",
    baseURL: value("E2E_BASE_URL"),
    issuerURL,
    oidcOrigin: new URL(issuerURL).origin,
    clientID: value("E2E_CLIENT_ID"),
    clientSecret: value("E2E_CLIENT_SECRET"),
    composeProject: value("E2E_COMPOSE_PROJECT"),
  };
  return cached;
}

const MAKE_KEYS = [
  "AGENT_SLOT",
  "E2E_BASE_URL",
  "E2E_ISSUER_URL",
  "E2E_CLIENT_ID",
  "E2E_CLIENT_SECRET",
  "E2E_COMPOSE_PROJECT",
] as const;

function needsMake(): boolean {
  return MAKE_KEYS.some((k) => !process.env[k]);
}

function readMakeEnv(): Record<string, string> {
  // stdio for stderr is inherited on purpose: make's own warnings about a .env
  // that fights the slot scheme are worth seeing, and swallowing them here is
  // how someone spends an afternoon on the wrong database.
  const out = execFileSync("make", ["-s", "e2e-env"], {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  });

  const parsed: Record<string, string> = {};
  for (const line of out.split("\n")) {
    const eq = line.indexOf("=");
    if (eq > 0) parsed[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return parsed;
}

/** Run a make target in the repository root, streaming its output. */
export function make(target: string): void {
  execFileSync("make", [target], {
    cwd: repoRoot,
    stdio: "inherit",
    env: process.env,
  });
}
