// Take the stack down again.
//
// `make e2e-down` is `compose down -v`: the test database is a tmpfs and is
// meant to be gone, so that the next run cannot inherit anything from this
// one. E2E_KEEP_STACK=1 leaves it standing, which is what `make e2e-ui` wants.

import { make } from "./lib/stack";

export default async function globalTeardown(): Promise<void> {
  if (process.env.E2E_KEEP_STACK === "1" || process.env.E2E_SKIP_STACK === "1") {
    console.log("e2e: leaving the stack up (E2E_KEEP_STACK / E2E_SKIP_STACK)");
    return;
  }
  make("e2e-down");
}
