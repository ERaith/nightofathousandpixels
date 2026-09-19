// app/api/admin/submission-limits/route.ts
import { prisma } from "@/lib/db";
import { checkAdminAuth } from "@/lib/api-auth";
import { getOrCreateCurrentSeason } from "@/lib/season";
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  try {
    const { searchParams } = new URL(req.url);
    

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    const season = await getOrCreateCurrentSeason();
    const overrides = await prisma.submissionOverride.findMany({
      where: { seasonId: season.id },
      orderBy: { email: "asc" },
    });

    return NextResponse.json({ overrides, defaultLimit: season.defaultSubmitLimit });
  } catch (error) {
    console.error("GET /api/admin/submission-limits error:", error);
    return NextResponse.json(
      { error: "Failed to fetch submission limits" },
      { status: 500 }
    );
  }
}

export async function POST(req: Request) {
  try {
    const { email, limit } = await req.json().catch(() => ({}));

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    if (!email || limit === undefined) {
      return NextResponse.json(
        { error: "email and limit are required" },
        { status: 400 }
      );
    }

    const season = await getOrCreateCurrentSeason();

    const override = await prisma.submissionOverride.upsert({
      where: {
        seasonId_email: {
          seasonId: season.id,
          email: email.toLowerCase(),
        },
      },
      update: { limit: parseInt(limit) },
      create: {
        seasonId: season.id,
        email: email.toLowerCase(),
        limit: parseInt(limit),
      },
    });

    return NextResponse.json({ ok: true, override });
  } catch (error) {
    console.error("POST /api/admin/submission-limits error:", error);
    return NextResponse.json(
      { error: "Failed to set submission limit" },
      { status: 500 }
    );
  }
}
