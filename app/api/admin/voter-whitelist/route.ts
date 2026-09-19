// app/api/admin/voter-whitelist/route.ts
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
    const whitelist = await prisma.voterWhitelist.findMany({
      where: { seasonId: season.id },
      orderBy: [{ hasVoted: "asc" }, { createdAt: "asc" }],
    });

    const totalCount = whitelist.length;
    const votedCount = whitelist.filter((v) => v.hasVoted).length;

    return NextResponse.json({
      whitelist,
      stats: {
        total: totalCount,
        voted: votedCount,
        remaining: totalCount - votedCount,
      },
    });
  } catch (error) {
    console.error("GET /api/admin/voter-whitelist error:", error);
    return NextResponse.json(
      { error: "Failed to fetch whitelist" },
      { status: 500 }
    );
  }
}

export async function POST(req: Request) {
  try {
    const { email, name } = await req.json();

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    if (!email) {
      return NextResponse.json(
        { error: "Email is required" },
        { status: 400 }
      );
    }

    const season = await getOrCreateCurrentSeason();
    const voter = await prisma.voterWhitelist.upsert({
      where: {
        seasonId_email: {
          seasonId: season.id,
          email: email.toLowerCase(),
        },
      },
      update: { name: name || null },
      create: {
        seasonId: season.id,
        email: email.toLowerCase(),
        name: name || null,
      },
    });

    return NextResponse.json({ ok: true, voter });
  } catch (error) {
    console.error("POST /api/admin/voter-whitelist error:", error);
    return NextResponse.json(
      { error: "Failed to add voter" },
      { status: 500 }
    );
  }
}

export async function DELETE(req: Request) {
  try {
    const { searchParams } = new URL(req.url);
    
    const email = searchParams.get("email");

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    if (!email) {
      return NextResponse.json(
        { error: "Email is required" },
        { status: 400 }
      );
    }

    const season = await getOrCreateCurrentSeason();
    await prisma.voterWhitelist.deleteMany({
      where: {
        seasonId: season.id,
        email: email.toLowerCase(),
      },
    });

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("DELETE /api/admin/voter-whitelist error:", error);
    return NextResponse.json(
      { error: "Failed to remove voter" },
      { status: 500 }
    );
  }
}
