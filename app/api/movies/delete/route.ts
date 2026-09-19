// app/api/movies/delete/route.ts
import { prisma } from "@/lib/db";
import { isValidEmail } from "@/lib/email";
import { NextResponse } from "next/server";

export async function POST(req: Request) {
  try {
    const { movieId, email } = await req.json().catch(() => ({}));

    if (!movieId || !email) {
      return NextResponse.json(
        { error: "movieId and email are required" },
        { status: 400 }
      );
    }

    if (!isValidEmail(email)) {
      return NextResponse.json(
        { error: "Valid email is required" },
        { status: 400 }
      );
    }

    // Verify the movie belongs to this email
    const movie = await prisma.movie.findFirst({
      where: {
        id: movieId,
        submittedBy: email.toLowerCase(),
      },
    });

    if (!movie) {
      return NextResponse.json(
        { error: "Movie not found or you don't have permission to delete it" },
        { status: 404 }
      );
    }

    await prisma.movie.delete({
      where: { id: movieId },
    });

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("POST /api/movies/delete error:", error);
    return NextResponse.json(
      { error: "Failed to delete movie" },
      { status: 500 }
    );
  }
}
