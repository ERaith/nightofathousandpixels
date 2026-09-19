// app/(site)/components/SubmitForm.tsx
"use client";
import { useState } from "react";
import { isLocked } from "@/lib/season";

type Season = {
  year: number;
  lockAt: Date | string | null;
  locked: boolean;
};

export default function SubmitForm({ season }: { season: Season }) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [trailerUrl, setTrailerUrl] = useState("");
  const [email, setEmail] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);

  const locked = isLocked(season);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess(false);
    setLoading(true);

    try {
      const res = await fetch("/api/movies", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title, description, trailerUrl, email }),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error || "Failed to submit");
        return;
      }

      setSuccess(true);
      setTitle("");
      setDescription("");
      setTrailerUrl("");
      setEmail("");
      setTimeout(() => {
        window.location.href = "/";
      }, 2000);
    } catch (err) {
      setError("Network error. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  if (locked) {
    return (
      <div className="mt-8 border border-red-500/50 bg-red-500/10 rounded-lg p-8 text-center">
        <p className="text-red-400 text-lg">
          Submissions are locked for Season {season.year}
        </p>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit} className="mt-8 space-y-6">
      <h2 className="text-2xl font-semibold">Submit a Movie</h2>

      {error && (
        <div className="bg-red-500/10 border border-red-500 text-red-300 px-4 py-2 rounded">
          {error}
        </div>
      )}

      {success && (
        <div className="bg-green-500/10 border border-green-500 text-green-300 px-4 py-2 rounded">
          Movie submitted! Redirecting...
        </div>
      )}

      <div>
        <label className="block text-sm mb-2">
          Movie Title *
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 focus:outline-none focus:border-accent"
            placeholder="e.g., Alien (1979)"
          />
        </label>
      </div>

      <div>
        <label className="block text-sm mb-2">
          Description
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 focus:outline-none focus:border-accent resize-y"
            placeholder="Why should we watch this?"
            rows={3}
            maxLength={500}
          />
        </label>
        <p className="text-xs opacity-60 mt-1">
          Optional. Max 500 characters.
        </p>
      </div>

      <div>
        <label className="block text-sm mb-2">
          Trailer URL (YouTube or Vimeo)
          <input
            type="url"
            value={trailerUrl}
            onChange={(e) => setTrailerUrl(e.target.value)}
            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 focus:outline-none focus:border-accent"
            placeholder="https://www.youtube.com/watch?v=..."
          />
        </label>
      </div>

      <div>
        <label className="block text-sm mb-2">
          Your Email *
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 focus:outline-none focus:border-accent"
            placeholder="you@example.com"
          />
        </label>
        <p className="text-xs opacity-60 mt-1">
          Required to track your submissions and enforce submission limits.
        </p>
      </div>

      <button
        type="submit"
        disabled={loading}
        className="w-full btn-accent rounded-lg px-4 py-3 hover:bg-accent/10 disabled:opacity-50 disabled:cursor-not-allowed transition-colors font-semibold"
      >
        {loading ? "Submitting..." : "Submit Movie"}
      </button>
    </form>
  );
}
