// Bring the stack up, sign in once, and leave the session on disk.
//
// Signing in is the expensive part of a browser test: a redirect to the
// provider, a code exchange and a JWKS fetch, per test. Doing it once here and
// handing every spec the resulting cookies reduces the per-test cost to
// reading a file.
//
// This file fails loudly by design. Every wait has a deadline, and every
// deadline ends in a thrown error with the container logs attached, because a
// global setup that shrugs and carries on produces a suite that reports green
// against nothing at all.

import { chromium, type FullConfig } from "@playwright/test";
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";

import {
  authorizeURL,
  defaultUser,
  discovery,
  pkce,
  queueUser,
  randomToken,
  waitFor,
} from "./lib/oidc";
import { harnessStatePath, make, stack, storageStatePath, type HarnessState } from "./lib/stack";

/** Where the application will mount sign-in, once ticket C3 wires it up. */
const LOGIN_PATH = "/auth/login";
const CALLBACK_PATH = "/auth/callback";

export default async function globalSetup(_config: FullConfig): Promise<void> {
  const s = stack();

  console.log(`\ne2e: slot ${s.slot} - app ${s.baseURL}, issuer ${s.issuerURL}`);

  if (process.env.E2E_SKIP_STACK === "1") {
    console.log("e2e: E2E_SKIP_STACK=1, assuming the stack is already up");
  } else {
    make("e2e-up");
  }

  try {
    await waitForApp(s.baseURL);
    await waitForProvider(s.issuerURL);
    const state = await signIn(s.baseURL);
    writeState(state);
  } catch (err) {
    dumpLogs(s.composeProject);
    throw err;
  }
}

async function waitForApp(baseURL: string): Promise<void> {
  await waitFor(`the application to answer on ${baseURL}/healthz`, async () => {
    const res = await fetch(`${baseURL}/healthz`);
    return res.status === 200;
  });
  console.log("e2e: application is up");
}

async function waitForProvider(issuerURL: string): Promise<void> {
  await waitFor(`the OIDC provider to publish ${issuerURL}`, async () => {
    const d = await discovery();
    if (d.issuer !== issuerURL) {
      // Not a retryable condition: a provider that answers with a DIFFERENT
      // issuer will never become the right one, and waiting for it to would
      // hide the misconfiguration behind a timeout.
      throw new Error(
        `the provider advertises issuer ${d.issuer} but the application is configured with ${issuerURL}`,
      );
    }
    return true;
  }, 30_000);
  console.log("e2e: OIDC provider is up and its issuer matches");
}

/**
 * Sign in through the real flow, if there is one to sign in through.
 *
 * Today there is not: internal/auth implements the flow but nothing mounts it
 * on the router, so GET /auth/login is a 404. That is recorded rather than
 * worked around - no session is invented, and the specs that need one skip
 * with the reason in the report. When C3 mounts the route this branch stops
 * being taken, and a sign-in that then fails is a failure, not a skip.
 */
async function signIn(baseURL: string): Promise<HarnessState> {
  const res = await fetch(`${baseURL}${LOGIN_PATH}`, { redirect: "manual" });

  if (res.status === 404) {
    console.log(
      `e2e: ${LOGIN_PATH} is a 404 - the application does not mount sign-in yet (ticket C3).\n` +
        `e2e: no session saved; specs needing one will report as skipped.`,
    );
    return { authWired: false, loginStatus: 404, email: null };
  }

  console.log(`e2e: ${LOGIN_PATH} returned ${res.status} - signing in for real`);

  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({ baseURL });
    const page = await context.newPage();

    await queueUser(defaultUser);
    await page.goto(LOGIN_PATH);
    await page.waitForURL((url) => url.origin === new URL(baseURL).origin && !url.pathname.startsWith("/auth/"), {
      timeout: 15_000,
    });

    const cookies = await context.cookies(baseURL);
    if (cookies.length === 0) {
      throw new Error(
        `sign-in through ${LOGIN_PATH} completed but set no cookie on ${baseURL}, so there is no session to save`,
      );
    }

    await context.storageState({ path: storageStatePath });
    console.log(`e2e: signed in as ${defaultUser.email}, ${cookies.length} cookie(s) saved`);
    return { authWired: true, loginStatus: res.status, email: defaultUser.email };
  } finally {
    await browser.close();
  }
}

function writeState(state: HarnessState): void {
  fs.mkdirSync(path.dirname(harnessStatePath), { recursive: true });
  fs.writeFileSync(harnessStatePath, `${JSON.stringify(state, null, 2)}\n`);

  // Playwright needs the storage-state file to exist even when there is no
  // session in it, because the projects reference it unconditionally.
  if (!state.authWired) {
    fs.writeFileSync(storageStatePath, `${JSON.stringify({ cookies: [], origins: [] })}\n`);
  }
}

/** Container logs, so that a setup failure arrives with its cause attached. */
function dumpLogs(project: string): void {
  try {
    const logs = execFileSync("docker", ["compose", "-p", project, "logs", "--tail=40"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    console.error(`\ne2e: last 40 log lines from ${project}:\n${logs}`);
  } catch {
    console.error(`e2e: could not read logs for compose project ${project}`);
  }
}
