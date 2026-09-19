// app/(site)/components/VoteButton.tsx
"use client";
import { useTransition, useState } from "react";

export default function VoteButton({
  onVote,
}: {
  onVote: (email: string) => Promise<any>;
}) {
  const [pending, startTransition] = useTransition();
  const [msg, setMsg] = useState<string | null>(null);
  const [showEmailPrompt, setShowEmailPrompt] = useState(false);
  const [email, setEmail] = useState("");

  const handleClick = () => {
    // Get saved email from localStorage
    const savedEmail = localStorage.getItem("voterEmail");
    if (savedEmail) {
      // If email is already saved, vote immediately
      submitVote(savedEmail);
    } else {
      // Otherwise, show email prompt
      setShowEmailPrompt(true);
    }
  };

  const submitVote = (voterEmail: string) => {
    startTransition(async () => {
      setMsg(null);
      setShowEmailPrompt(false);
      try {
        const result = await onVote(voterEmail);
        if (result.ok) {
          setMsg("✓ Voted");
          // Save email for next time
          localStorage.setItem("voterEmail", voterEmail);
        } else {
          setMsg(result.error || "Failed");
        }
      } catch {
        setMsg("Failed");
      }
      // Clear message after 3 seconds
      setTimeout(() => setMsg(null), 3000);
    });
  };

  const handleSubmitEmail = (e: React.FormEvent) => {
    e.preventDefault();
    if (email.trim()) {
      submitVote(email.toLowerCase());
    }
  };

  if (showEmailPrompt) {
    return (
      <form onSubmit={handleSubmitEmail} className="flex items-center gap-2">
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="your@email.com"
          className="text-xs bg-zinc-950 border border-zinc-800 rounded px-2 py-1 focus:outline-none focus:border-accent"
          autoFocus
          required
        />
        <button
          type="submit"
          disabled={pending}
          className="rounded-lg px-3 py-1 border border-accent/40 hover:bg-accent/10 disabled:opacity-50 text-xs"
        >
          Submit
        </button>
        <button
          type="button"
          onClick={() => setShowEmailPrompt(false)}
          className="text-xs opacity-60 hover:opacity-100"
        >
          ✕
        </button>
      </form>
    );
  }

  return (
    <div className="flex items-center gap-2">
      <button
        disabled={pending}
        onClick={handleClick}
        className="rounded-lg px-3 py-1.5 border border-accent/40 hover:bg-accent/10 disabled:opacity-50 disabled:cursor-not-allowed transition-colors text-sm"
      >
        {pending ? "..." : "Vote"}
      </button>
      {msg && (
        <span
          className={`text-xs ${
            msg.includes("✓") ? "text-green-400" : "text-red-400"
          }`}
        >
          {msg}
        </span>
      )}
    </div>
  );
}
