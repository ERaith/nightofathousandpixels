// The mock provider must not be in the shipped image.
//
// This is the assertion that makes the whole approach safe to use. A mock OIDC
// provider compiled into the production binary would be a signing key an
// attacker could reach; the argument that it is "test-only" is worth exactly
// what is checked.
//
// So it is checked twice, and both checks look at artifacts rather than at
// source: what the compiler says cmd/server depends on, and what the module
// list baked into the binary INSIDE THE RUNNING CONTAINER says it was built
// from. The second is the one that matters - it is the actual thing that would
// be deployed.

import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { expect, test } from "@playwright/test";

import { repoRoot, stack } from "../lib/stack";

const MODULE_PATH = "github.com/ERaith/nightofathousandpixels/cmd/server";

// Only once: this inspects a build artifact, not a rendered page, so running
// it per browser project would say the same thing twice.
test.describe.configure({ mode: "default" });

test("mockoidc is absent from the shipped image", async ({}, testInfo) => {
  test.skip(testInfo.project.name !== "desktop", "artifact check, not a browser check");

  // 1. What the compiler says. `go list -deps` is the transitive import graph
  //    of the binary's package, so an import anywhere under it would show up.
  const deps = execFileSync("go", ["list", "-deps", "./cmd/server"], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  // Guard against a vacuous pass: an empty or failed listing would contain no
  // mockoidc either.
  expect(deps.split("\n").length).toBeGreaterThan(50);
  // A package that is definitely in there, so that "mockoidc is not in this
  // list" cannot be satisfied by an empty list. go-oidc would be the more
  // pointed choice, but it is not in cmd/server's graph yet either: nothing
  // mounts the auth package (nap-9jw).
  expect(deps).toContain("github.com/go-chi/chi/v5");
  expect(deps).not.toContain("mockoidc");

  // 2. What is actually in the container. `go version -m` reads the module
  //    list the linker recorded, so this is the built artifact's own account
  //    of itself and does not depend on the source tree being clean.
  const container = execFileSync(
    "docker",
    ["compose", "-p", stack().composeProject, "ps", "-q", "app"],
    { encoding: "utf8" },
  ).trim();
  expect(container, "the app container is running").not.toBe("");

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nap-e2e-"));
  try {
    const binary = path.join(dir, "server");
    execFileSync("docker", ["cp", `${container}:/server`, binary]);

    const modules = execFileSync("go", ["version", "-m", binary], { encoding: "utf8" });

    // Same guard: prove we are reading the right binary before believing what
    // is missing from it.
    expect(modules).toContain(`path\t${MODULE_PATH}`);
    expect(modules).toContain("dep\tgithub.com/jackc/pgx/v5");
    expect(modules).not.toContain("mockoidc");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
