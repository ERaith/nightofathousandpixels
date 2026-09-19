// app/api/vote/route.ts
import { prisma } from "@/lib/db";
import { getOrCreateCurrentSeason, isLocked } from "@/lib/season";
import { getOrSetDeviceId, ipHash } from "@/lib/token";
import { NextResponse } from "next/server";

export async function POST(req: Request) {
  try {
    const season = await getOrCreateCurrentSeason();
    if (isLocked(season)) {
      return NextResponse.json(
        { error: "Voting is locked" },
        { status: 403 }
      );
    }

    const { movieId, email } = await req.json().catch(() => ({}));
    if (!movieId) {
      return NextResponse.json(
        { error: "movieId is required" },
        { status: 400 }
      );
    }

    if (!email) {
      return NextResponse.json(
        { error: "Email is required to vote" },
        { status: 400 }
      );
    }

    const normalizedEmail = email.toLowerCase();

    // Verify movie exists and belongs to current season
    const movie = await prisma.movie.findFirst({
      where: { id: movieId, seasonId: season.id },
    });
    if (!movie) {
      return NextResponse.json(
        { error: "Movie not found in current season" },
        { status: 404 }
      );
    }

    const deviceId = await getOrSetDeviceId();
    const ip = req.headers.get("x-forwarded-for")?.split(",")[0] || "";

    // Use upsert with email - this automatically handles vote changing
    // If they've already voted with this email, it updates their vote to the new movie
    await prisma.vote.upsert({
      where: {
        seasonId_email: {
          seasonId: season.id,
          email: normalizedEmail,
        },
      },
      update: {
        movieId,
        deviceId, // Update device in case they switched browsers
      },
      create: {
        seasonId: season.id,
        movieId,
        email: normalizedEmail,
        deviceId,
        ipHash: ipHash(ip),
      },
    });

    // Track email as voted (upsert to ensure entry exists)
    await prisma.voterWhitelist.upsert({
      where: {
        seasonId_email: {
          seasonId: season.id,
          email: normalizedEmail,
        },
      },
      update: {
        hasVoted: true,
        votedAt: new Date(),
      },
      create: {
        seasonId: season.id,
        email: normalizedEmail,
        hasVoted: true,
        votedAt: new Date(),
      },
    });

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("POST /api/vote error:", error);
    return NextResponse.json({ error: "Failed to vote" }, { status: 500 });
  }
}
