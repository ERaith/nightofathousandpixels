// app/(site)/components/CountdownTimer.tsx
"use client";
import { useEffect, useState } from "react";

type TimeLeft = {
  days: number;
  hours: number;
  minutes: number;
  seconds: number;
  total: number;
};

function calculateTimeLeft(lockAt: string | Date | null): TimeLeft {
  if (!lockAt) {
    return { days: 0, hours: 0, minutes: 0, seconds: 0, total: 0 };
  }

  // Get current time in MST (UTC-7)
  const now = new Date();
  const targetDate = new Date(lockAt);

  const difference = +targetDate - +now;

  if (difference <= 0) {
    return { days: 0, hours: 0, minutes: 0, seconds: 0, total: 0 };
  }

  return {
    days: Math.floor(difference / (1000 * 60 * 60 * 24)),
    hours: Math.floor((difference / (1000 * 60 * 60)) % 24),
    minutes: Math.floor((difference / 1000 / 60) % 60),
    seconds: Math.floor((difference / 1000) % 60),
    total: difference,
  };
}

type Season = {
  year: number;
  lockAt: string | Date | null;
  locked: boolean;
};

export default function CountdownTimer({ season }: { season: Season }) {
  const [timeLeft, setTimeLeft] = useState<TimeLeft>(calculateTimeLeft(season.lockAt));
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
    const timer = setInterval(() => {
      setTimeLeft(calculateTimeLeft(season.lockAt));
    }, 1000);

    return () => clearInterval(timer);
  }, [season.lockAt]);

  if (!mounted || !season.lockAt) {
    return null;
  }

  if (season.locked || timeLeft.total <= 0) {
    return (
      <div className="border border-red-500/50 bg-red-500/10 rounded-lg p-6 text-center">
        <div className="text-red-400 text-lg font-semibold">
          🔒 SEASON LOCKED
        </div>
        <p className="text-sm opacity-60 mt-2">Voting and submissions are closed</p>
      </div>
    );
  }

  const segments = [
    { label: "DAYS", value: timeLeft.days, max: 365 },
    { label: "HRS", value: timeLeft.hours, max: 24 },
    { label: "MIN", value: timeLeft.minutes, max: 60 },
    { label: "SEC", value: timeLeft.seconds, max: 60 },
  ];

  // Determine urgency level
  const isUrgent = timeLeft.total < 1000 * 60 * 60; // Less than 1 hour
  const isWarning = timeLeft.total < 1000 * 60 * 60 * 24; // Less than 1 day

  return (
    <div
      className={`border rounded-lg p-6 ${
        isUrgent
          ? "border-red-500/70 bg-red-500/10 animate-pulse"
          : isWarning
          ? "border-orange-500/70 bg-orange-500/10"
          : "border-chrome bg-zinc-950/40"
      }`}
    >
      <div className="text-center mb-4">
        <div className="text-sm opacity-60 uppercase tracking-wider">
          Time Until Movie Night {season.year}
        </div>
      </div>

      <div className="flex items-center justify-center gap-2 md:gap-4">
        {segments.map((segment, idx) => (
          <div key={segment.label} className="flex items-center gap-2 md:gap-4">
            {idx > 0 && (
              <div className="text-2xl md:text-4xl opacity-40 font-mono">:</div>
            )}
            <div className="flex flex-col items-center">
              <div
                className={`font-mono text-3xl md:text-5xl font-bold tabular-nums ${
                  isUrgent ? "text-red-400" : isWarning ? "text-orange-400" : "text-accent"
                }`}
              >
                {String(segment.value).padStart(2, "0")}
              </div>
              <div className="text-xs uppercase tracking-wider opacity-60 mt-1">
                {segment.label}
              </div>
              {/* Segmented progress bar */}
              <div className="flex gap-0.5 mt-2">
                {Array.from({ length: 10 }).map((_, i) => {
                  const filled = (segment.value / segment.max) * 10 > i;
                  return (
                    <div
                      key={i}
                      className={`w-1.5 h-1 rounded-sm transition-colors ${
                        filled
                          ? isUrgent
                            ? "bg-red-500"
                            : isWarning
                            ? "bg-orange-500"
                            : "bg-accent"
                          : "bg-zinc-800"
                      }`}
                    />
                  );
                })}
              </div>
            </div>
          </div>
        ))}
      </div>

      {isUrgent && (
        <div className="text-center mt-4 text-red-400 text-sm font-semibold animate-pulse">
          ⚠️ LESS THAN 1 HOUR REMAINING
        </div>
      )}
      {isWarning && !isUrgent && (
        <div className="text-center mt-4 text-orange-400 text-sm">
          ⏰ Less than 24 hours remaining
        </div>
      )}
    </div>
  );
}
