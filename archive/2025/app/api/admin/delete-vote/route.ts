// app/api/admin/delete-vote/route.ts
import { prisma } from "@/lib/db";
import { checkAdminAuth } from "@/lib/api-auth";
import { NextResponse } from "next/server";

export async function POST(req: Request) {
  try {
    const { voteId } = await req.json().catch(() => ({}));

    // Check authentication
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    if (!voteId) {
      return NextResponse.json(
        { error: "voteId is required" },
        { status: 400 }
      );
    }

    await prisma.vote.delete({
      where: { id: voteId },
    });

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("POST /api/admin/delete-vote error:", error);
    return NextResponse.json(
      { error: "Failed to delete vote" },
      { status: 500 }
    );
  }
}
