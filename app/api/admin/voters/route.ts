// app/api/admin/voters/route.ts
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

    const yearParam = searchParams.get('year');

    let season;
    if (yearParam) {
      const year = parseInt(yearParam);
      season = await prisma.season.findUnique({ where: { year } });
      if (!season) {
        return NextResponse.json({ votes: [], whitelistVoters: [] });
      }
    } else {
      season = await getOrCreateCurrentSeason();
    }

    const votes = await prisma.vote.findMany({
      where: { seasonId: season.id },
      include: {
        movie: {
          select: {
            id: true,
            title: true,
            hidden: true,
            submittedBy: true,
          },
        },
      },
      orderBy: { createdAt: "desc" },
    });

    // Get whitelist entries (those who voted via email tracking)
    const whitelistVoters = await prisma.voterWhitelist.findMany({
      where: {
        seasonId: season.id,
        hasVoted: true,
      },
      orderBy: {
        votedAt: 'desc',
      },
    });

    return NextResponse.json({ votes, whitelistVoters });
  } catch (error) {
    console.error("GET /api/admin/voters error:", error);
    return NextResponse.json(
      { error: "Failed to fetch voters" },
      { status: 500 }
    );
  }
}

export async function DELETE(req: Request) {
  try {
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    const { voteId, deviceId, email, seasonId } = await req.json();

    if (voteId) {
      // Delete specific vote by ID
      await prisma.vote.delete({
        where: { id: voteId },
      });
    } else if (deviceId && seasonId) {
      // Delete vote by device ID and season
      await prisma.vote.deleteMany({
        where: {
          deviceId,
          seasonId,
        },
      });
    } else if (email && seasonId) {
      // Reset whitelist entry
      await prisma.voterWhitelist.updateMany({
        where: {
          email: email.toLowerCase(),
          seasonId,
        },
        data: {
          hasVoted: false,
          votedAt: null,
        },
      });
    } else {
      return NextResponse.json(
        { error: 'Vote ID, device ID, or email is required' },
        { status: 400 }
      );
    }

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error('Error removing voter:', error);
    return NextResponse.json(
      { error: 'Failed to remove voter' },
      { status: 500 }
    );
  }
}
