// app/api/admin/season/route.ts
import { prisma } from "@/lib/db";
import { getOrCreateCurrentSeason } from "@/lib/season";
import { checkAdminAuth } from "@/lib/api-auth";
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  try {
    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    const { searchParams } = new URL(req.url);
    const yearParam = searchParams.get("year");

    if (yearParam) {
      // Fetch specific year
      const year = parseInt(yearParam);
      let season = await prisma.season.findUnique({ where: { year } });
      if (!season) {
        // Create season if it doesn't exist
        season = await prisma.season.create({
          data: { year, name: String(year) },
        });
      }
      return NextResponse.json(season);
    } else {
      // Return current season
      const season = await getOrCreateCurrentSeason();
      return NextResponse.json(season);
    }
  } catch (error) {
    console.error("GET /api/admin/season error:", error);
    return NextResponse.json(
      { error: "Failed to fetch season" },
      { status: 500 }
    );
  }
}

export async function PATCH(req: Request) {
  try {
    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    const { name, lockAt, locked, defaultSubmitLimit } = await req.json();

    const season = await getOrCreateCurrentSeason();

    const updateData: any = {};

    if (name !== undefined) {
      updateData.name = name;
    }

    if (lockAt !== undefined) {
      updateData.lockAt = lockAt ? new Date(lockAt) : null;
    }

    if (locked !== undefined) {
      updateData.locked = locked;
    }

    if (defaultSubmitLimit !== undefined) {
      updateData.defaultSubmitLimit = parseInt(defaultSubmitLimit);
    }

    const updatedSeason = await prisma.season.update({
      where: { id: season.id },
      data: updateData,
    });

    return NextResponse.json({ ok: true, season: updatedSeason });
  } catch (error) {
    console.error("PATCH /api/admin/season error:", error);
    return NextResponse.json(
      { error: "Failed to update season" },
      { status: 500 }
    );
  }
}
