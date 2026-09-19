// app/api/admin/seasons/route.ts
import { prisma } from "@/lib/db";
import { checkAdminAuth } from "@/lib/api-auth";
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  try {
    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    // Fetch all seasons ordered by year descending
    const seasons = await prisma.season.findMany({
      orderBy: { year: 'desc' },
      select: {
        id: true,
        year: true,
        name: true,
        locked: true,
        lockAt: true,
        _count: {
          select: {
            movies: true,
            votes: true,
          },
        },
      },
    });

    return NextResponse.json(seasons);
  } catch (error) {
    console.error("GET /api/admin/seasons error:", error);
    return NextResponse.json(
      { error: "Failed to fetch seasons" },
      { status: 500 }
    );
  }
}
