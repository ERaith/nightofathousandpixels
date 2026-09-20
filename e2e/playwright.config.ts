import { defineConfig, devices } from "@playwright/test";

import { projectNames } from "./lib/people";
import { stack, storageStatePath } from "./lib/stack";

const s = stack();

// The projects below and lib/people.ts have to name the same set. Every
// journey that writes signs in as a person derived from its project name, and
// those people are whitelisted by the seed from the same list - so a project
// that only exists here is a project whose identities nobody creates, and its
// journeys would fail on the "You're not on the list yet" page a long way from
// the cause. Checked at config load, where the message can say so.
function checkedProjectNames(names: string[]): string[] {
  const missing = names.filter((n) => !(projectNames as readonly string[]).includes(n));
  const unused = projectNames.filter((n) => !names.includes(n));
  if (missing.length > 0 || unused.length > 0) {
    throw new Error(
      `e2e: playwright.config.ts and lib/people.ts disagree about the projects. ` +
        `Only in the config: [${missing.join(", ")}]. Only in people.ts: [${unused.join(", ")}]. ` +
        `Both lists have to name the same projects, or a project's people are never seeded.`,
    );
  }
  return names;
}

checkedProjectNames(["mobile", "desktop"]);

export default defineConfig({
  testDir: "./tests",
  globalSetup: require.resolve("./global-setup"),
  globalTeardown: require.resolve("./global-teardown"),

  // One application in front of one database. Tests that submit a movie or
  // cast a ballot write to shared state, so running them in parallel would buy
  // a few seconds and cost the ability to trust a red run. Thirty friends and
  // a handful of journeys do not need the speed.
  fullyParallel: false,
  workers: 1,

  // No retries, deliberately. A retry turns a flake into a pass and moves the
  // evidence into a report nobody opens. If something here is flaky, that is
  // the finding.
  retries: 0,

  // `test.only` left in a file is a suite that silently stops testing most of
  // itself, which is the exact failure this project keeps hitting.
  forbidOnly: !!process.env.CI,

  timeout: 30_000,
  expect: { timeout: 5_000 },

  reporter: [
    // The list reporter prints a line per test, skips included, so a skipped
    // spec is visible in the terminal and not only in the HTML.
    ["list"],
    ["html", { open: "never", outputFolder: "playwright-report" }],
  ],

  use: {
    baseURL: s.baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },

  projects: [
    // Most voters are on a phone, so the mobile project is not an afterthought
    // to the desktop one - anything that only works at 1280px wide is broken
    // for most of the people who will use it.
    {
      name: "mobile",
      use: { ...devices["Pixel 5"], storageState: storageStatePath },
    },
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"], storageState: storageStatePath },
    },
  ],
});
