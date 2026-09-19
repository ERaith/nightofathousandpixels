// The provider half of sign-in, driven by a real browser.
//
// The application cannot be signed into yet - nothing mounts /auth/login - so
// what is proved here is everything on the other side of that route: that the
// stack's OIDC provider is reachable from the browser AND from inside the
// application's container at the same issuer URL, that it runs a real
// authorization-code flow with PKCE, and that the token it hands back carries
// a signature that verifies against its published JWKS.
//
// That is not a formality. Getting one issuer URL to mean the same thing to a
// containerised client and a browser on the host is the whole difficulty this
// harness exists to solve (see docker-compose.e2e.yml), and it is the part
// that will break silently if anyone changes the ports, the compose networking
// or the issuer.
//
// The last two tests are here because a provider that says yes to everything
// would pass all of the above.

import { expect, test } from "@playwright/test";

import {
  authorizeURL,
  discovery,
  exchangeCode,
  pkce,
  queueUser,
  randomToken,
  verifyIdToken,
  type TestUser,
} from "../lib/oidc";
import { stack } from "../lib/stack";

// The browser is sent here after the provider approves. It is intercepted
// rather than served, so this spec does not depend on the application having
// any particular route: the only thing under test is what the provider put in
// the URL.
const LANDING_PATH = "/e2e-oidc-landing";

const user: TestUser = {
  subject: "e2e-provider-subject",
  email: "provider-check@example.test",
  name: "Provider Check",
};

test.describe("the OIDC provider", () => {
  test("advertises the issuer the application is configured with", async () => {
    const s = stack();
    const d = await discovery();

    // Byte-for-byte. go-oidc compares this string against the `iss` claim and
    // against OAUTH_ISSUER_URL, and a trailing slash is a different issuer.
    expect(d.issuer).toBe(s.issuerURL);

    for (const endpoint of [
      d.authorization_endpoint,
      d.token_endpoint,
      d.userinfo_endpoint,
      d.jwks_uri,
    ]) {
      expect(new URL(endpoint).origin).toBe(s.oidcOrigin);
    }

    expect(d.id_token_signing_alg_values_supported).toContain("RS256");

    // "plain" PKCE is a no-op against anyone who can read the authorize
    // request. internal/auth only ever sends S256; a provider that still
    // offered plain would let a regression to it pass here even though Google
    // would reject it.
    expect(d.code_challenge_methods_supported).toEqual(["S256"]);
  });

  test("completes an authorization-code flow in a real browser", async ({ page }) => {
    const s = stack();
    const redirectURI = `${s.baseURL}${LANDING_PATH}`;
    await page.route(`${redirectURI}*`, (route) =>
      route.fulfill({ status: 200, contentType: "text/html", body: "<html><body>landed</body></html>" }),
    );

    const d = await discovery();
    const { verifier, challenge } = pkce();
    const state = randomToken();
    const nonce = randomToken();

    await queueUser(user);
    await page.goto(authorizeURL(d, { redirectURI, state, nonce, challenge }));

    const landed = new URL(page.url());
    expect(landed.pathname).toBe(LANDING_PATH);
    expect(landed.searchParams.get("state")).toBe(state);

    const code = landed.searchParams.get("code");
    expect(code).toBeTruthy();

    const tokens = await exchangeCode(d, code as string, verifier, redirectURI);
    expect(tokens.id_token).toBeTruthy();

    // Verified against the JWKS, not merely decoded: a decode would accept a
    // token signed with the wrong key, or with none.
    const claims = await verifyIdToken(d, tokens.id_token);

    expect(claims.iss).toBe(s.issuerURL);
    // `aud` is a string or an array of them, and go-oidc accepts either. The
    // assertion is that it names this client and nobody else: a token minted
    // for a second audience is a token another client would also accept.
    expect(typeof claims.aud === "string" ? [claims.aud] : claims.aud).toEqual([s.clientID]);
    expect(claims.nonce).toBe(nonce);
    expect(claims.sub).toBe(user.subject);
    expect(claims.email).toBe(user.email);
    // internal/auth rejects an identity whose address the provider will not
    // vouch for, so the mock has to assert it the way Google does.
    expect(claims.email_verified).toBe(true);
    expect(claims.name).toBe(user.name);
    expect(claims.exp).toBeGreaterThan(Math.floor(Date.now() / 1000));
  });

  test("rejects a code exchanged with the wrong PKCE verifier", async ({ page }) => {
    const s = stack();
    const redirectURI = `${s.baseURL}${LANDING_PATH}`;
    await page.route(`${redirectURI}*`, (route) =>
      route.fulfill({ status: 200, contentType: "text/html", body: "ok" }),
    );

    const d = await discovery();
    const { challenge } = pkce();
    const state = randomToken();

    // Queueing an identity is not incidental. The dev stack's provider grows a
    // click-through consent screen (nap-bml), and a queued user is what tells
    // it this caller is a test rather than a browser waiting to choose. This
    // test is about PKCE, not about who signs in, so it says so explicitly
    // instead of depending on which provider build it is pointed at.
    await queueUser(user);
    await page.goto(authorizeURL(d, { redirectURI, state, nonce: randomToken(), challenge }));
    const code = new URL(page.url()).searchParams.get("code");
    expect(code).toBeTruthy();

    // A different verifier than the challenge was derived from.
    await expect(exchangeCode(d, code as string, pkce().verifier, redirectURI)).rejects.toThrow(
      /token exchange returned 4\d\d/,
    );
  });

  test("rejects an authorize request from an unknown client", async ({ request }) => {
    // This one deliberately queues nobody, and it is a specification rather
    // than a convenience: an authorize request the provider should refuse must
    // be refused BEFORE anything renders a page.
    //
    // It matters most for the dev stack's consent screen (nap-bml). A picker
    // that renders its identity list before mockoidc has validated client_id,
    // response_type, scope and the PKCE method turns a misconfigured
    // OAUTH_CLIENT_ID into a friendly page with a list of people on it. The
    // developer clicks one, and the failure surfaces later and somewhere else.
    // Google answers an unknown client with an error, so the mock must too.
    const d = await discovery();
    const url = new URL(d.authorization_endpoint);
    url.searchParams.set("client_id", "not-the-configured-client");
    url.searchParams.set("response_type", "code");
    url.searchParams.set("scope", "openid email");
    url.searchParams.set("redirect_uri", `${stack().baseURL}${LANDING_PATH}`);
    url.searchParams.set("state", randomToken());
    url.searchParams.set("code_challenge", pkce().challenge);
    url.searchParams.set("code_challenge_method", "S256");

    const res = await request.get(url.toString(), { maxRedirects: 0 });

    expect(res.status()).toBeGreaterThanOrEqual(400);
    expect(res.status()).toBeLessThan(500);
  });
});
