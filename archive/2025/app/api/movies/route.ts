// app/api/movies/route.ts
import { prisma } from "@/lib/db";
import { getOrCreateCurrentSeason, isLocked } from "@/lib/season";
import { isValidEmail } from "@/lib/email";
import { toEmbed } from "@/lib/trailer";
import { getOrSetDeviceId } from "@/lib/token";
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  try {
    const { searchParams } = new URL(req.url);
    const showHidden = searchParams.get("showHidden") === "true";
    const yearParam = searchParams.get("year");

    let season;
    if (yearParam) {
      const year = parseInt(yearParam);
      season = await prisma.season.findUnique({ where: { year } });
      if (!season) {
        season = await prisma.season.create({
          data: { year, name: String(year) },
        });
      }
    } else {
      season = await getOrCreateCurrentSeason();
    }

    const movies = await prisma.movie.findMany({
      where: {
        seasonId: season.id,
        ...(showHidden ? {} : { hidden: false })
      },
      include: { _count: { select: { votes: true } } },
      orderBy: [{ votes: { _count: "desc" } }, { submittedAt: "asc" }],
    });
    return NextResponse.json({ season, movies });
  } catch (error) {
    console.error("GET /api/movies error:", error);
    return NextResponse.json(
      { error: "Failed to fetch movies" },
      { status: 500 }
    );
  }
}

export async function POST(req: Request) {
  try {
    const season = await getOrCreateCurrentSeason();
    if (isLocked(season)) {
      return NextResponse.json(
        { error: "Submissions are locked" },
        { status: 403 }
      );
    }

    const body = await req.json().catch(() => ({}));
    const { title, description, trailerUrl, email } = body;

    if (!title?.trim()) {
      return NextResponse.json(
        { error: "Title is required" },
        { status: 400 }
      );
    }

    const validEmail = email && isValidEmail(email) ? email.toLowerCase() : null;

    // Check submission limits if email provided
    if (validEmail) {
      const existingSubmissions = await prisma.movie.count({
        where: {
          seasonId: season.id,
          submittedBy: validEmail,
        },
      });

      // Check if this email has voted by looking at voter whitelist
      const userVote = await prisma.voterWhitelist.findFirst({
        where: {
          seasonId: season.id,
          email: validEmail,
          hasVoted: true,
        },
      });

      let limit: number;
      if (userVote) {
        // User has voted, limit to 1 submission
        limit = 1;
      } else {
        // Check for custom limit override
        const override = await prisma.submissionOverride.findUnique({
          where: {
            seasonId_email: {
              seasonId: season.id,
              email: validEmail,
            },
          },
        });
        limit = override?.limit ?? season.defaultSubmitLimit;
      }

      if (existingSubmissions >= limit) {
        const message = userVote
          ? "You've already voted, so you're limited to 1 movie submission. Delete your existing submission to add another."
          : `You've reached your submission limit of ${limit} movie(s). Delete an existing submission to add another.`;
        return NextResponse.json(
          { error: message },
          { status: 403 }
        );
      }
    }

    const embed = trailerUrl ? toEmbed(trailerUrl) : null;
    const deviceId = await getOrSetDeviceId();

    const movie = await prisma.movie.create({
      data: {
        title: title.trim(),
        description: description?.trim() || null,
        trailerUrl: embed,
        submittedBy: validEmail,
        deviceId,
        seasonId: season.id,
      },
    });

    return NextResponse.json(movie, { status: 201 });
  } catch (error: any) {
    console.error("POST /api/movies error:", error);
    if (error.code === "P2002") {
      return NextResponse.json(
        { error: "A movie with this title already exists this season" },
        { status: 400 }
      );
    }
    return NextResponse.json(
      { error: "Failed to submit movie" },
      { status: 500 }
    );
  }
}
