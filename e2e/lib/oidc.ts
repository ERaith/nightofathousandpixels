// The provider side of the sign-in flow, spoken from the test process.
//
// This is deliberately hand-rolled against node:crypto rather than pulled from
// an OIDC client library. The point of these tests is to check that the
// application's OIDC client is correct; borrowing a second client library to
// check the first one would mostly test that the two libraries agree with each
// other. Everything here is the raw protocol.

import { createHash, createPublicKey, randomBytes, verify as cryptoVerify } from "node:crypto";
import type { JsonWebKey as CryptoJsonWebKey } from "node:crypto";

import { stack } from "./stack";

export interface Discovery {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  userinfo_endpoint: string;
  jwks_uri: string;
  id_token_signing_alg_values_supported: string[];
  code_challenge_methods_supported?: string[];
}

export interface Pkce {
  verifier: string;
  challenge: string;
}

/** A person the provider will sign a token for. */
export interface TestUser {
  subject: string;
  email: string;
  name?: string;
  emailVerified?: boolean;
}

/** The default identity, used by any test that does not care who signs in. */
export const defaultUser: TestUser = {
  subject: "e2e-subject-0001",
  email: "e2e-voter@example.test",
  name: "E2E Voter",
};

export function base64url(b: Buffer): string {
  return b.toString("base64url");
}

/** A fresh PKCE pair. S256 only: the application sends nothing else. */
export function pkce(): Pkce {
  const verifier = base64url(randomBytes(32));
  return { verifier, challenge: base64url(createHash("sha256").update(verifier).digest()) };
}

export function randomToken(): string {
  return base64url(randomBytes(16));
}

export async function discovery(): Promise<Discovery> {
  const url = `${stack().issuerURL}/.well-known/openid-configuration`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`e2e: discovery at ${url} returned ${res.status}`);
  return (await res.json()) as Discovery;
}

/**
 * Tell the provider who signs in next.
 *
 * This is a control endpoint ON THE PROVIDER. It grants nothing and touches
 * nothing in the application: it only decides which address the provider will
 * sign a token for, the way clicking an account on Google's chooser does. The
 * application still verifies that token, and still has to decide whether the
 * address is on the season's whitelist.
 *
 * The provider pops one queued user per authorize call and falls back to its
 * own default when the queue is empty, so this has to be called immediately
 * before the sign-in it is meant to apply to.
 */
export async function queueUser(user: TestUser): Promise<void> {
  const res = await fetch(`${stack().oidcOrigin}/control/user`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      subject: user.subject,
      email: user.email,
      name: user.name ?? "",
      email_verified: user.emailVerified ?? true,
    }),
  });
  if (res.status !== 204) {
    throw new Error(`e2e: queueing ${user.email} returned ${res.status}: ${await res.text()}`);
  }
}

export interface AuthorizeParams {
  redirectURI: string;
  state: string;
  nonce: string;
  challenge: string;
  scope?: string;
}

/** The URL a browser would be sent to to start a sign-in. */
export function authorizeURL(d: Discovery, p: AuthorizeParams): string {
  const u = new URL(d.authorization_endpoint);
  u.searchParams.set("client_id", stack().clientID);
  u.searchParams.set("response_type", "code");
  u.searchParams.set("scope", p.scope ?? "openid email profile");
  u.searchParams.set("redirect_uri", p.redirectURI);
  u.searchParams.set("state", p.state);
  u.searchParams.set("nonce", p.nonce);
  u.searchParams.set("code_challenge", p.challenge);
  u.searchParams.set("code_challenge_method", "S256");
  return u.toString();
}

export interface TokenResponse {
  access_token: string;
  id_token: string;
  token_type: string;
  expires_in: number;
}

/** Exchange an authorization code, exactly as the application's client does. */
export async function exchangeCode(
  d: Discovery,
  code: string,
  verifier: string,
  redirectURI: string,
): Promise<TokenResponse> {
  const { clientID, clientSecret } = stack();
  // client_secret_post, not client_secret_basic. mockoidc's discovery document
  // advertises both, but its token handler only ever reads the form - an
  // Authorization header alone gets "the request is missing the required
  // parameter: client_secret". That is a quirk of the mock and not something
  // the application has to care about: golang.org/x/oauth2 auto-detects the
  // auth style and falls back to the form on its own.
  const res = await fetch(d.token_endpoint, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectURI,
      client_id: clientID,
      client_secret: clientSecret,
      code_verifier: verifier,
    }),
  });
  if (!res.ok) {
    throw new Error(`e2e: token exchange returned ${res.status}: ${await res.text()}`);
  }
  return (await res.json()) as TokenResponse;
}

export interface IdTokenClaims {
  iss: string;
  aud: string | string[];
  sub: string;
  exp: number;
  iat: number;
  nonce?: string;
  email?: string;
  email_verified?: boolean;
  name?: string;
}

interface Jwk {
  kid?: string;
  kty: string;
  alg?: string;
  use?: string;
  n: string;
  e: string;
}

/**
 * Verify an ID token's RS256 signature against the issuer's published JWKS and
 * return its claims.
 *
 * Decoding without verifying would be the easy version and a worthless one: a
 * provider that signed with the wrong key, or did not sign at all, would pass.
 */
export async function verifyIdToken(d: Discovery, idToken: string): Promise<IdTokenClaims> {
  const parts = idToken.split(".");
  if (parts.length !== 3) throw new Error(`e2e: id_token is not a three-part JWS`);
  const [rawHeader, rawPayload, rawSignature] = parts as [string, string, string];

  const header = JSON.parse(Buffer.from(rawHeader, "base64url").toString("utf8")) as {
    alg: string;
    kid?: string;
  };
  if (header.alg !== "RS256") {
    throw new Error(`e2e: id_token is signed with ${header.alg}, expected RS256`);
  }

  const jwksRes = await fetch(d.jwks_uri);
  if (!jwksRes.ok) throw new Error(`e2e: JWKS at ${d.jwks_uri} returned ${jwksRes.status}`);
  const { keys } = (await jwksRes.json()) as { keys: Jwk[] };

  const candidates = header.kid ? keys.filter((k) => k.kid === header.kid) : keys;
  if (candidates.length === 0) {
    throw new Error(`e2e: JWKS has no key with kid ${header.kid}`);
  }

  const signed = Buffer.from(`${rawHeader}.${rawPayload}`);
  const signature = Buffer.from(rawSignature, "base64url");
  const verified = candidates.some((jwk) =>
    cryptoVerify(
      "RSA-SHA256",
      signed,
      createPublicKey({ key: jwk as unknown as CryptoJsonWebKey, format: "jwk" }),
      signature,
    ),
  );
  if (!verified) {
    throw new Error(`e2e: id_token signature does not verify against the issuer's JWKS`);
  }

  return JSON.parse(Buffer.from(rawPayload, "base64url").toString("utf8")) as IdTokenClaims;
}

/** Poll until `check` returns true, or throw with `what` in the message. */
export async function waitFor(
  what: string,
  check: () => Promise<boolean>,
  timeoutMs = 60_000,
  intervalMs = 500,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  for (;;) {
    try {
      if (await check()) return;
      lastError = undefined;
    } catch (err) {
      lastError = err;
    }
    if (Date.now() >= deadline) {
      const because = lastError instanceof Error ? `: ${lastError.message}` : "";
      throw new Error(`e2e: timed out after ${timeoutMs}ms waiting for ${what}${because}`);
    }
    await new Promise((r) => setTimeout(r, intervalMs));
  }
}
