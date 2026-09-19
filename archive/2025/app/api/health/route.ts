// app/api/health/route.ts
import { prisma } from "@/lib/db";
import { NextResponse } from "next/server";

// Container healthcheck target — verifies the app is serving and the
// database is reachable. Never cached.
export const dynamic = "force-dynamic";

export async function GET() {
  try {
    await prisma.$queryRaw`SELECT 1`;
    return NextResponse.json({ status: "ok" });
  } catch (err) {
    console.error("Health check failed:", err);
    return NextResponse.json({ status: "error" }, { status: 503 });
  }
}
