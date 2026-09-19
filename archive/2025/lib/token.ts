// lib/token.ts
import { cookies } from "next/headers";
import crypto from "crypto";

const COOKIE_NAME = "nap_device";
const SECRET = process.env.COOKIE_SECRET || "fallback-secret-change-me";

export async function getOrSetDeviceId(): Promise<string> {
  const jar = await cookies();
  let id = jar.get(COOKIE_NAME)?.value;

  if (!id) {
    id = crypto.randomBytes(16).toString("hex");
    jar.set(COOKIE_NAME, id, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      maxAge: 60 * 60 * 24 * 365,
    });
  }

  return id;
}

export function ipHash(ip?: string): string {
  const salt = SECRET.slice(0, 16);
  return crypto
    .createHash("sha256")
    .update(salt + (ip || "unknown"))
    .digest("hex");
}
