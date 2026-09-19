// app/(site)/my-submissions/page.tsx
"use client";
import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";

type Movie = {
  id: string;
  title: string;
  trailerUrl: string | null;
  hidden: boolean;
  _count: { votes: number };
};

type LimitInfo = {
  email: string;
  limit: number;
  submissionCount: number;
};

function MySubmissionsContent() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const emailParam = searchParams.get("email");

  const [email, setEmail] = useState(emailParam || "");
  const [movies, setMovies] = useState<Movie[]>([]);
  const [limitInfo, setLimitInfo] = useState<LimitInfo | null>(null);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [loading, setLoading] = useState(false);

  const fetchSubmissions = async (emailToFetch: string) => {
    if (!emailToFetch) return;

    setLoading(true);
    setError("");

    try {
      const res = await fetch(`/api/movies/my-submissions?email=${encodeURIComponent(emailToFetch)}`);
      const data = await res.json();

      if (!res.ok) {
        setError(data.error || "Failed to fetch submissions");
        setMovies([]);
        setLimitInfo(null);
        setLoading(false);
        return;
      }

      setMovies(data.movies || []);
      setLimitInfo(data.limitInfo || null);
      setLoading(false);
    } catch (err) {
      setError("Failed to fetch submissions");
      setMovies([]);
      setLimitInfo(null);
      setLoading(false);
    }
  };

  useEffect(() => {
    if (emailParam) {
      fetchSubmissions(emailParam);
    }
  }, [emailParam]);

  const handleViewSubmissions = (e: React.FormEvent) => {
    e.preventDefault();
    if (!email) return;
    router.push(`/my-submissions?email=${encodeURIComponent(email)}`);
  };

  const handleDeleteMovie = async (movieId: string) => {
    if (!confirm("Are you sure you want to delete this submission? This will also remove all votes for it.")) {
      return;
    }

    setError("");
    setSuccess("");

    try {
      const res = await fetch("/api/movies/delete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ movieId, email }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || "Failed to delete submission");
        return;
      }

      setSuccess("Submission deleted successfully");
      setTimeout(() => setSuccess(""), 2000);
      fetchSubmissions(email);
    } catch (err) {
      setError("Failed to delete submission");
    }
  };

  return (
    <div className="mt-8 space-y-6 max-w-4xl mx-auto mb-16">
      <h2 className="text-3xl font-semibold">My Submissions</h2>

      {/* Email Input Form */}
      <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
        <form onSubmit={handleViewSubmissions} className="space-y-4">
          <div>
            <label className="block text-sm mb-2">
              Enter your email to view your submissions:
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 focus:border-accent focus:outline-none"
                placeholder="your@email.com"
              />
            </label>
          </div>
          <button
            type="submit"
            className="btn-accent rounded px-4 py-2 hover:bg-accent/10 transition-colors"
          >
            View My Submissions
          </button>
        </form>
      </div>

      {/* Error/Success Messages */}
      {error && (
        <div className="bg-red-500/10 border border-red-500 text-red-300 px-4 py-3 rounded">
          {error}
        </div>
      )}
      {success && (
        <div className="bg-green-500/10 border border-green-500 text-green-300 px-4 py-3 rounded">
          {success}
        </div>
      )}

      {/* Loading State */}
      {loading && (
        <div className="text-center py-8 opacity-60">Loading submissions...</div>
      )}

      {/* Limit Info */}
      {limitInfo && !loading && (
        <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
          <h3 className="font-semibold mb-2">Submission Limit</h3>
          <p className="text-sm">
            You have submitted{" "}
            <span className="text-accent font-semibold">{limitInfo.submissionCount}</span> of{" "}
            <span className="text-accent font-semibold">{limitInfo.limit}</span> allowed movie(s).
          </p>
          {limitInfo.submissionCount >= limitInfo.limit && (
            <p className="text-xs opacity-60 mt-2">
              You've reached your limit. Delete a submission below to add another.
            </p>
          )}
        </div>
      )}

      {/* Submissions List */}
      {!loading && emailParam && movies.length > 0 && (
        <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
          <h3 className="font-semibold mb-4 text-lg">Your Submissions</h3>
          <div className="space-y-2">
            {movies.map((m) => (
              <div
                key={m.id}
                className={`flex items-center justify-between border-b border-zinc-800 py-3 ${
                  m.hidden ? "opacity-50" : ""
                }`}
              >
                <div className="flex items-center gap-4 flex-1">
                  <span className={m.hidden ? "line-through" : ""}>{m.title}</span>
                  {m.hidden && (
                    <span className="text-xs bg-red-500/20 text-red-300 px-2 py-1 rounded">
                      Hidden by Admin
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-4">
                  <span className="text-accent font-mono text-sm">{m._count.votes} votes</span>
                  <button
                    onClick={() => handleDeleteMovie(m.id)}
                    className="text-xs px-3 py-1 border border-red-700 rounded hover:border-red-500 transition-colors text-red-400"
                  >
                    Delete
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* No Submissions */}
      {!loading && emailParam && movies.length === 0 && !error && (
        <div className="text-center py-8 opacity-60">
          No submissions found for this email address.
        </div>
      )}
    </div>
  );
}

export default function MySubmissionsPage() {
  return (
    <Suspense fallback={<div className="mt-8 text-center">Loading...</div>}>
      <MySubmissionsContent />
    </Suspense>
  );
}
