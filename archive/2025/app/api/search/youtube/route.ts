// app/api/search/youtube/route.ts
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  const key = process.env.YOUTUBE_API_KEY;
  if (!key) {
    return NextResponse.json({ items: [] });
  }

  try {
    const { searchParams } = new URL(req.url);
    const q = searchParams.get("q") || "";

    if (!q.trim()) {
      return NextResponse.json({ items: [] });
    }

    const response = await fetch(
      `https://www.googleapis.com/youtube/v3/search?part=snippet&type=video&maxResults=8&q=${encodeURIComponent(
        q
      )}&key=${key}`
    );

    if (!response.ok) {
      console.error("YouTube API error:", await response.text());
      return NextResponse.json({ items: [] });
    }

    const data = await response.json();
    const items = (data.items || []).map((it: any) => ({
      id: it.id.videoId,
      title: it.snippet.title,
      url: `https://www.youtube.com/watch?v=${it.id.videoId}`,
    }));

    return NextResponse.json({ items });
  } catch (error) {
    console.error("GET /api/search/youtube error:", error);
    return NextResponse.json({ items: [] });
  }
}
