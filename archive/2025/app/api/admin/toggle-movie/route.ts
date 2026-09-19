// app/api/admin/toggle-movie/route.ts
import { prisma } from "@/lib/db";
import { checkAdminAuth } from "@/lib/api-auth";
import { NextResponse } from "next/server";

export async function POST(req: Request) {
  try {
    const { movieId } = await req.json().catch(() => ({}));

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    if (!movieId) {
      return NextResponse.json(
        { error: "movieId is required" },
        { status: 400 }
      );
    }

    const movie = await prisma.movie.findUnique({
      where: { id: movieId },
    });

    if (!movie) {
      return NextResponse.json({ error: "Movie not found" }, { status: 404 });
    }

    const updated = await prisma.movie.update({
      where: { id: movieId },
      data: { hidden: !movie.hidden },
    });

    return NextResponse.json({ ok: true, hidden: updated.hidden });
  } catch (error) {
    console.error("POST /api/admin/toggle-movie error:", error);
    return NextResponse.json(
      { error: "Failed to toggle movie visibility" },
      { status: 500 }
    );
  }
}
