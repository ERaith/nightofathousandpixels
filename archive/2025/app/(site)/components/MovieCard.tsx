// app/(site)/components/MovieCard.tsx
"use client";
import useSWR from "swr";
import TrailerEmbed from "./TrailerEmbed";
import VoteButton from "./VoteButton";

const fetcher = (url: string) => fetch(url).then((r) => r.json());

type Movie = {
  id: string;
  title: string;
  description: string | null;
  trailerUrl: string | null;
  _count: { votes: number };
};

export default function MovieCard({ movie }: { movie: Movie }) {
  const { data } = useSWR("/api/movies", fetcher, { refreshInterval: 8000 });
  const current = data?.movies?.find((m: Movie) => m.id === movie.id) || movie;

  const handleVote = async (email: string) => {
    const res = await fetch("/api/vote", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ movieId: current.id, email }),
    });
    return res.json();
  };

  return (
    <article className="rounded-2xl border border-chrome p-6 bg-zinc-950/60 shadow-lg hover:shadow-xl transition-all hover:border-accent/40">
      <h3 className="text-lg font-semibold mb-4">{current.title}</h3>
      {current.description && (
        <p className="text-sm opacity-80 mb-4 italic border-l-2 border-accent/30 pl-3 py-1">
          {current.description}
        </p>
      )}
      {current.trailerUrl ? (
        <TrailerEmbed url={current.trailerUrl} />
      ) : (
        <div className="aspect-video w-full bg-zinc-900/50 rounded-lg flex items-center justify-center border border-zinc-800/50">
          <p className="text-sm opacity-60">No trailer</p>
        </div>
      )}
      <div className="mt-4 flex items-center justify-between">
        <span className="text-sm text-accent font-mono">
          {current._count?.votes ?? 0} votes
        </span>
        <VoteButton onVote={handleVote} />
      </div>
    </article>
  );
}
