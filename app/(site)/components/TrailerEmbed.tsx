// app/(site)/components/TrailerEmbed.tsx
"use client";

export default function TrailerEmbed({ url }: { url: string | null }) {
  if (!url) return null;

  return (
    <div className="relative aspect-video w-full overflow-hidden rounded-lg bg-zinc-900">
      <iframe
        src={url}
        className="absolute inset-0 h-full w-full"
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
        allowFullScreen
        title="Trailer"
      />
    </div>
  );
}
