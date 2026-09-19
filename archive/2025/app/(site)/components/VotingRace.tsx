// app/(site)/components/VotingRace.tsx
"use client";
import { useEffect, useState } from "react";
import useSWR from "swr";

type Movie = {
  id: string;
  title: string;
  _count: { votes: number };
};

const fetcher = (url: string) => fetch(url).then((r) => r.json());

export default function VotingRace() {
  const { data, error } = useSWR("/api/movies", fetcher, {
    refreshInterval: 3000, // Poll every 3 seconds
  });

  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  if (!mounted || error || !data?.movies) {
    return null;
  }

  const movies: Movie[] = data.movies;
  const topMovies = movies.slice(0, 3);

  if (topMovies.length === 0) {
    return null;
  }

  const maxVotes = Math.max(...topMovies.map((m) => m._count.votes), 1);
  const totalVotes = movies.reduce((sum, m) => sum + m._count.votes, 0);

  // Check if top 2 are close (within 1 vote)
  const isCloseRace =
    topMovies.length >= 2 &&
    Math.abs(topMovies[0]._count.votes - topMovies[1]._count.votes) <= 1;

  return (
    <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-xl font-semibold">🏁 The Race</h3>
        {isCloseRace && (
          <span className="text-xs bg-accent/20 text-accent px-3 py-1 rounded-full animate-pulse">
            CLOSE RACE!
          </span>
        )}
      </div>

      {totalVotes === 0 ? (
        <p className="text-sm opacity-60 text-center py-4">
          No votes yet. Be the first to vote!
        </p>
      ) : (
        <div className="space-y-4">
          {topMovies.map((movie, idx) => {
            const percentage = (movie._count.votes / maxVotes) * 100;
            const isLeader = idx === 0 && movie._count.votes > 0;

            return (
              <div key={movie.id} className="space-y-2">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2 flex-1 min-w-0">
                    {idx === 0 && <span className="text-lg">🥇</span>}
                    {idx === 1 && <span className="text-lg">🥈</span>}
                    {idx === 2 && <span className="text-lg">🥉</span>}
                    <span
                      className={`truncate ${isLeader ? "font-semibold text-accent" : ""}`}
                    >
                      {movie.title}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-xs font-mono">
                    <span className={isLeader ? "text-accent font-bold" : "opacity-80"}>
                      {movie._count.votes} vote{movie._count.votes !== 1 ? "s" : ""}
                    </span>
                    <span className="opacity-50">
                      ({((movie._count.votes / totalVotes) * 100).toFixed(0)}%)
                    </span>
                  </div>
                </div>

                {/* Animated progress bar */}
                <div className="relative h-8 bg-zinc-900 rounded-lg overflow-hidden border border-zinc-800">
                  <div
                    className={`absolute inset-y-0 left-0 transition-all duration-700 ease-out ${
                      isLeader
                        ? "bg-gradient-to-r from-accent/80 to-accent"
                        : "bg-gradient-to-r from-zinc-700 to-zinc-600"
                    }`}
                    style={{ width: `${percentage}%` }}
                  >
                    {isLeader && (
                      <div className="absolute inset-0 bg-gradient-to-r from-transparent via-white/10 to-transparent animate-shimmer" />
                    )}
                  </div>

                  {/* Segmented overlay */}
                  <div className="absolute inset-0 flex">
                    {Array.from({ length: 20 }).map((_, i) => (
                      <div
                        key={i}
                        className="flex-1 border-r border-zinc-950/30"
                      />
                    ))}
                  </div>
                </div>
              </div>
            );
          })}

          {movies.length > 3 && (
            <div className="text-xs opacity-60 text-center pt-2">
              +{movies.length - 3} more movie{movies.length - 3 !== 1 ? "s" : ""} in the running
            </div>
          )}
        </div>
      )}

      <style jsx>{`
        @keyframes shimmer {
          0% {
            transform: translateX(-100%);
          }
          100% {
            transform: translateX(100%);
          }
        }
        .animate-shimmer {
          animation: shimmer 2s infinite;
        }
      `}</style>
    </div>
  );
}
