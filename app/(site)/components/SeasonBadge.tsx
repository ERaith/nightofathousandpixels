// app/(site)/components/SeasonBadge.tsx
import { isLocked } from "@/lib/season";

type Season = {
  year: number;
  lockAt: Date | string | null;
  locked: boolean;
};

export default function SeasonBadge({ season }: { season: Season }) {
  const locked = isLocked(season);

  return (
    <div className="inline-flex items-center gap-2 rounded-full border border-chrome px-3 py-1 text-xs">
      <span className="font-mono">Season {season.year}</span>
      {locked ? (
        <span className="text-red-400 font-semibold">🔒 Locked</span>
      ) : (
        <span className="text-accent font-semibold">✓ Open</span>
      )}
    </div>
  );
}
