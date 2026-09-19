// app/(site)/page.tsx
import { prisma } from "@/lib/db";
import { getOrCreateCurrentSeason } from "@/lib/season";
import MovieCard from "./components/MovieCard";
import SeasonBadge from "./components/SeasonBadge";
import CountdownTimer from "./components/CountdownTimer";
import VotingRace from "./components/VotingRace";

// Force dynamic rendering (no static generation at build time)
export const dynamic = 'force-dynamic';

export default async function HomePage() {
  const season = await getOrCreateCurrentSeason();
  const movies = await prisma.movie.findMany({
    where: { seasonId: season.id },
    include: { _count: { select: { votes: true } } },
    orderBy: [{ votes: { _count: "desc" } }, { submittedAt: "asc" }],
  });

  return (
    <section className="mt-8 mb-16">
      <SeasonBadge season={season} />

      <div className="mt-8 mb-8">
        <CountdownTimer season={season} />
      </div>

      <div className="mb-8">
        <VotingRace />
      </div>

      <div className="grid gap-8 md:grid-cols-2 lg:grid-cols-3 mt-8">
        {movies.map((m) => (
          <MovieCard key={m.id} movie={m} />
        ))}
        {movies.length === 0 && (
          <p className="opacity-70 col-span-full text-center py-12">
            No movies yet. Be the first to{" "}
            <a className="underline hover:text-accent transition-colors" href="/submit">
              submit one
            </a>
            !
          </p>
        )}
      </div>
    </section>
  );
}
