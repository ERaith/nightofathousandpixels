// lib/api-auth.ts
// Helper functions for API route authentication
import { getAdminSession } from "./auth";
import { NextResponse } from "next/server";

export async function checkAdminAuth(): Promise<{ authorized: boolean; response?: NextResponse }> {
  const session = await getAdminSession();

  if (!session) {
    return {
      authorized: false,
      response: NextResponse.json({ error: "Unauthorized - Please login" }, { status: 401 })
    };
  }

  return { authorized: true };
}
