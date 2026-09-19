// app/api/movies/my-submissions/route.ts
import { prisma } from "@/lib/db";
import { getOrCreateCurrentSeason } from "@/lib/season";
import { isValidEmail } from "@/lib/email";
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  try {
    const { searchParams } = new URL(req.url);
    const email = searchParams.get("email");

    if (!email || !isValidEmail(email)) {
      return NextResponse.json(
        { error: "Valid email is required" },
        { status: 400 }
      );
    }

    const normalizedEmail = email.toLowerCase();
    const season = await getOrCreateCurrentSeason();

    // Fetch user's submissions
    const movies = await prisma.movie.findMany({
      where: {
        seasonId: season.id,
        submittedBy: normalizedEmail,
      },
      include: {
        _count: {
          select: { votes: true },
        },
      },
      orderBy: { submittedAt: "desc" },
    });

    // Get submission limit info
    const override = await prisma.submissionOverride.findUnique({
      where: {
        seasonId_email: {
          seasonId: season.id,
          email: normalizedEmail,
        },
      },
    });

    const limit = override?.limit ?? season.defaultSubmitLimit;
    const submissionCount = movies.length;

    return NextResponse.json({
      movies,
      limitInfo: {
        email: normalizedEmail,
        limit,
        submissionCount,
      },
    });
  } catch (error) {
    console.error("GET /api/movies/my-submissions error:", error);
    return NextResponse.json(
      { error: "Failed to fetch submissions" },
      { status: 500 }
    );
  }
}
