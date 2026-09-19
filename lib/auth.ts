// lib/auth.ts
import { cookies } from "next/headers";
import { verify } from "./jwt";

export async function getAdminSession() {
  try {
    const cookieStore = await cookies();
    const token = cookieStore.get("admin_session")?.value;

    if (!token) {
      return null;
    }

    const payload = await verify(token);
    return payload;
  } catch (error) {
    return null;
  }
}

export async function requireAdmin() {
  const session = await getAdminSession();

  if (!session) {
    throw new Error("Unauthorized");
  }

  return session;
}
