// app/api/admin/delete-movie/route.ts
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

    await prisma.movie.delete({
      where: { id: movieId },
    });

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("POST /api/admin/delete-movie error:", error);
    return NextResponse.json(
      { error: "Failed to delete movie" },
      { status: 500 }
    );
  }
}
