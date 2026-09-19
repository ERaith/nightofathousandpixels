// app/(site)/admin/AdminPanel.tsx
"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

type Movie = {
  id: string;
  title: string;
  description: string | null;
  trailerUrl: string | null;
  hidden: boolean;
  submittedBy: string | null;
  deviceId: string | null;
  _count: { votes: number };
};

type Season = {
  id: string;
  year: number;
  name: string;
  lockAt: string | null;
  locked: boolean;
  defaultSubmitLimit: number;
};

type Vote = {
  id: string;
  deviceId: string;
  email: string | null;
  ipHash: string;
  createdAt: string;
  movie: {
    id: string;
    title: string;
    hidden: boolean;
    submittedBy: string | null;
  };
};

type SubmissionOverride = {
  id: string;
  email: string;
  limit: number;
};

type VoterWhitelist = {
  id: string;
  email: string;
  name: string | null;
  hasVoted: boolean;
  votedAt: string | null;
};

type WhitelistStats = {
  total: number;
  voted: number;
  remaining: number;
};

export default function AdminPanel() {
  const router = useRouter();

  const [season, setSeason] = useState<Season | null>(null);
  const [allSeasons, setAllSeasons] = useState<{year: number; name: string; locked: boolean}[]>([]);
  const [selectedYear, setSelectedYear] = useState<number>(new Date().getFullYear());
  const [movies, setMovies] = useState<Movie[]>([]);
  const [voters, setVoters] = useState<Vote[]>([]);
  const [whitelistVoters, setWhitelistVoters] = useState<VoterWhitelist[]>([]);
  const [overrides, setOverrides] = useState<SubmissionOverride[]>([]);
  const [whitelist, setWhitelist] = useState<VoterWhitelist[]>([]);
  const [whitelistStats, setWhitelistStats] = useState<WhitelistStats>({ total: 0, voted: 0, remaining: 0 });

  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [activeTab, setActiveTab] = useState<"movies" | "voters" | "limits" | "settings" | "account">("settings");

  // Form states
  const [seasonName, setSeasonName] = useState("");
  const [lockAt, setLockAt] = useState("");
  const [defaultLimit, setDefaultLimit] = useState(1);
  const [newLimitEmail, setNewLimitEmail] = useState("");
  const [newLimitValue, setNewLimitValue] = useState("1");
  const [newVoterEmail, setNewVoterEmail] = useState("");
  const [newVoterName, setNewVoterName] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  // Movie editing state
  const [editingMovie, setEditingMovie] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editTrailerUrl, setEditTrailerUrl] = useState("");

  const refreshData = (year?: number) => {
    const targetYear = year || selectedYear;

    // Fetch all seasons list
    fetch("/api/admin/seasons")
      .then((r) => r.json())
      .then((data) => setAllSeasons(data || []));

    // Fetch specific season
    fetch(`/api/admin/season?year=${targetYear}`)
      .then((r) => r.json())
      .then((data) => {
        setSeason(data);
        setSeasonName(data.name || "");
        setDefaultLimit(data.defaultSubmitLimit || 1);
        if (data.lockAt) {
          // Convert UTC time to MST for display
          const utcDate = new Date(data.lockAt);
          // Subtract MST offset (7 hours) from UTC to get MST
          const mstOffset = 7 * 60; // 7 hours in minutes
          const mstDate = new Date(utcDate.getTime() - mstOffset * 60 * 1000);
          setLockAt(mstDate.toISOString().slice(0, 16));
        } else {
          setLockAt("");
        }
      });

    // Fetch movies for selected season (including hidden)
    fetch(`/api/movies?showHidden=true&year=${targetYear}`)
      .then((r) => r.json())
      .then((data) => setMovies(data.movies || []));

    // Fetch voters
    fetch(`/api/admin/voters?year=${targetYear}`)
      .then((r) => r.json())
      .then((data) => {
        if (data.error) {
          router.push("/admin/login");
        } else {
          setVoters(data.votes || []);
          setWhitelistVoters(data.whitelistVoters || []);
        }
      });

    // Fetch submission limits
    fetch("/api/admin/submission-limits")
      .then((r) => r.json())
      .then((data) => setOverrides(data.overrides || []));

    // Fetch voter whitelist
    fetch("/api/admin/voter-whitelist")
      .then((r) => r.json())
      .then((data) => {
        setWhitelist(data.whitelist || []);
        setWhitelistStats(data.stats || { total: 0, voted: 0, remaining: 0 });
      });
  };

  useEffect(() => {
    refreshData();
  }, []);

  const handleLogout = async () => {
    await fetch("/api/admin/logout", { method: "POST" });
    router.push("/admin/login");
    router.refresh();
  };

  const handleUpdateSeason = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    try {
      // Convert lockAt to MST/MDT timezone
      let lockAtISO = null;
      if (lockAt) {
        // The input datetime-local gives us a local string like "2025-11-01T19:00"
        // We need to interpret this as MST (UTC-7) or MDT (UTC-6)
        // For simplicity, treat it as MST (UTC-7) and convert to UTC
        const localDate = new Date(lockAt);
        // Add MST offset (7 hours) to get UTC
        const mstOffset = 7 * 60; // 7 hours in minutes
        const utcDate = new Date(localDate.getTime() + mstOffset * 60 * 1000);
        lockAtISO = utcDate.toISOString();
      }

      const res = await fetch("/api/admin/season", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: seasonName,
          lockAt: lockAtISO,
          defaultSubmitLimit: defaultLimit
        }),
      });

      if (!res.ok) {
        setError("Failed to update season");
        return;
      }

      setSuccess("Season updated successfully");
      setTimeout(() => setSuccess(""), 2000);
      refreshData();
    } catch (err) {
      setError("Failed to update season");
    }
  };

  const handleToggleMovie = async (movieId: string) => {
    try {
      await fetch("/api/admin/toggle-movie", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ movieId }),
      });
      refreshData();
    } catch (err) {
      setError("Failed to toggle movie");
    }
  };

  const handleDeleteMovie = async (movieId: string) => {
    if (!confirm("Delete this movie? This will also delete all votes for it.")) return;
    try {
      await fetch("/api/admin/delete-movie", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ movieId }),
      });
      refreshData();
    } catch (err) {
      setError("Failed to delete movie");
    }
  };

  const handleStartEdit = (movie: Movie) => {
    setEditingMovie(movie.id);
    setEditTitle(movie.title);
    setEditDescription(movie.description || "");
    setEditTrailerUrl(movie.trailerUrl || "");
  };

  const handleCancelEdit = () => {
    setEditingMovie(null);
    setEditTitle("");
    setEditDescription("");
    setEditTrailerUrl("");
  };

  const handleSaveEdit = async (movieId: string) => {
    try {
      const res = await fetch("/api/admin/edit-movie", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          movieId,
          title: editTitle,
          description: editDescription,
          trailerUrl: editTrailerUrl,
        }),
      });

      if (!res.ok) {
        setError("Failed to save changes");
        return;
      }

      setSuccess("Movie updated successfully");
      setTimeout(() => setSuccess(""), 2000);
      handleCancelEdit();
      refreshData();
    } catch (err) {
      setError("Failed to save changes");
    }
  };

  const handleDeleteVote = async (voteId: string) => {
    if (!confirm("Delete this vote?")) return;
    try {
      await fetch("/api/admin/voters", {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ voteId }),
      });
      refreshData();
    } catch (err) {
      setError("Failed to delete vote");
    }
  };

  const handleRemoveVoter = async (email: string) => {
    if (!confirm(`Remove voter: ${email}?`)) return;
    if (!season) return;

    try {
      await fetch("/api/admin/voters", {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, seasonId: season.id }),
      });
      refreshData();
    } catch (err) {
      setError("Failed to remove voter");
    }
  };

  const handleSetLimit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    try {
      const res = await fetch("/api/admin/submission-limits", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: newLimitEmail, limit: newLimitValue }),
      });

      if (!res.ok) {
        setError("Failed to set limit");
        return;
      }

      setSuccess("Limit set successfully");
      setTimeout(() => setSuccess(""), 2000);
      setNewLimitEmail("");
      setNewLimitValue("1");
      refreshData();
    } catch (err) {
      setError("Failed to set limit");
    }
  };

  const handleAddVoter = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    try {
      const res = await fetch("/api/admin/voter-whitelist", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: newVoterEmail, name: newVoterName }),
      });

      if (!res.ok) {
        setError("Failed to add voter");
        return;
      }

      setSuccess("Voter added successfully");
      setTimeout(() => setSuccess(""), 2000);
      setNewVoterEmail("");
      setNewVoterName("");
      refreshData();
    } catch (err) {
      setError("Failed to add voter");
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    if (newPassword !== confirmPassword) {
      setError("New passwords do not match");
      return;
    }

    if (newPassword.length < 8) {
      setError("Password must be at least 8 characters long");
      return;
    }

    try {
      const res = await fetch("/api/admin/change-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          currentPassword,
          newPassword,
        }),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error || "Failed to change password");
        return;
      }

      setSuccess("Password changed successfully!");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err) {
      setError("Failed to change password");
    }
  };

  const handleSeasonChange = (year: number) => {
    setSelectedYear(year);
    refreshData(year);
  };

  return (
    <div className="mt-8 mb-16 max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-4">
          <h1 className="text-3xl font-semibold">Admin Panel</h1>
          {allSeasons.length > 0 && (
            <select
              value={selectedYear}
              onChange={(e) => handleSeasonChange(parseInt(e.target.value))}
              className="bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-sm"
            >
              {allSeasons.map((s) => (
                <option key={s.year} value={s.year}>
                  {s.year} {s.name !== String(s.year) && `- ${s.name}`} {s.locked && '(Locked)'}
                </option>
              ))}
            </select>
          )}
        </div>
        <button
          onClick={handleLogout}
          className="text-sm px-4 py-2 border border-red-700 rounded hover:border-red-500 hover:bg-red-500/10 transition-colors"
        >
          Logout
        </button>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500 text-red-300 px-4 py-3 rounded mb-4">
          {error}
        </div>
      )}
      {success && (
        <div className="bg-green-500/10 border border-green-500 text-green-300 px-4 py-3 rounded mb-4">
          {success}
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-2 border-b border-zinc-800 mb-6">
        {(["settings", "movies", "voters", "limits", "account"] as const).map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2 capitalize ${
              activeTab === tab
                ? "border-b-2 border-accent text-accent"
                : "opacity-60 hover:opacity-100"
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      {/* Settings Tab */}
      {activeTab === "settings" && (
        <div className="space-y-8">
          {/* Season Settings */}
          <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
            <h2 className="text-xl font-semibold mb-4">Season Settings</h2>
            <form onSubmit={handleUpdateSeason} className="space-y-4">
              <div>
                <label className="block text-sm mb-2">
                  Season Name
                  <input
                    type="text"
                    value={seasonName}
                    onChange={(e) => setSeasonName(e.target.value)}
                    className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                    placeholder="e.g., Winter 2025"
                  />
                </label>
              </div>
              <div>
                <label className="block text-sm mb-2">
                  Lock Date/Time (Movie Night) - MST/MDT Timezone
                  <input
                    type="datetime-local"
                    value={lockAt}
                    onChange={(e) => setLockAt(e.target.value)}
                    className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  />
                </label>
                <p className="text-xs opacity-60 mt-1">
                  Enter the date/time for movie night in Mountain Time (MST/MDT). Voting and submissions will be locked at this time.
                </p>
              </div>
              <div>
                <label className="block text-sm mb-2">
                  Default Submission Limit
                  <input
                    type="number"
                    value={defaultLimit}
                    onChange={(e) => setDefaultLimit(parseInt(e.target.value))}
                    min={0}
                    className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  />
                </label>
              </div>
              <button
                type="submit"
                className="btn-accent rounded px-4 py-2 hover:bg-accent/10 transition-colors"
              >
                Update Season
              </button>
            </form>
          </div>

          {/* Voter Whitelist */}
          <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-xl font-semibold">Voter Whitelist</h2>
              <div className="text-sm opacity-80">
                {whitelistStats.voted} / {whitelistStats.total} voted
                {whitelistStats.total > 0 && (
                  <span className="ml-2 text-accent font-semibold">
                    ({((whitelistStats.voted / whitelistStats.total) * 100).toFixed(0)}%)
                  </span>
                )}
              </div>
            </div>

            <form onSubmit={handleAddVoter} className="space-y-4 mb-6">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm mb-2">
                    Email
                    <input
                      type="email"
                      value={newVoterEmail}
                      onChange={(e) => setNewVoterEmail(e.target.value)}
                      required
                      className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                      placeholder="voter@example.com"
                    />
                  </label>
                </div>
                <div>
                  <label className="block text-sm mb-2">
                    Name (optional)
                    <input
                      type="text"
                      value={newVoterName}
                      onChange={(e) => setNewVoterName(e.target.value)}
                      className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                      placeholder="Alice"
                    />
                  </label>
                </div>
              </div>
              <button
                type="submit"
                className="btn-accent rounded px-4 py-2 hover:bg-accent/10 transition-colors"
              >
                Add Voter
              </button>
            </form>

            {whitelist.length === 0 ? (
              <p className="text-sm opacity-60 text-center py-4">No voters on whitelist</p>
            ) : (
              <div className="space-y-2">
                {whitelist.map((voter) => (
                  <div
                    key={voter.id}
                    className="flex items-center justify-between border-b border-zinc-800 py-3"
                  >
                    <div className="flex items-center gap-3">
                      <div className={`w-2 h-2 rounded-full ${voter.hasVoted ? "bg-green-500" : "bg-zinc-600"}`} />
                      <div>
                        <div className="text-sm">
                          {voter.name && <span className="font-semibold">{voter.name} </span>}
                          <span className="opacity-80">{voter.email}</span>
                        </div>
                        {voter.hasVoted && voter.votedAt && (
                          <div className="text-xs opacity-60">
                            Voted: {new Date(voter.votedAt).toLocaleString()}
                          </div>
                        )}
                      </div>
                    </div>
                    <button
                      onClick={() => handleRemoveVoter(voter.email)}
                      className="text-xs px-3 py-1 border border-red-700 rounded hover:border-red-500"
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Movies Tab */}
      {activeTab === "movies" && (
        <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
          <h2 className="text-xl font-semibold mb-4">All Movies</h2>
          {movies.length === 0 ? (
            <p className="opacity-60 text-center py-4">No movies submitted</p>
          ) : (
            <div className="space-y-4">
              {movies.map((m) => (
                <div
                  key={m.id}
                  className={`border-b border-zinc-800 pb-4 ${
                    m.hidden ? "opacity-50" : ""
                  }`}
                >
                  {editingMovie === m.id ? (
                    <div className="space-y-3">
                      <div>
                        <label className="block text-sm mb-1">
                          Title
                          <input
                            type="text"
                            value={editTitle}
                            onChange={(e) => setEditTitle(e.target.value)}
                            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                          />
                        </label>
                      </div>
                      <div>
                        <label className="block text-sm mb-1">
                          Description
                          <textarea
                            value={editDescription}
                            onChange={(e) => setEditDescription(e.target.value)}
                            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 resize-y"
                            rows={3}
                            maxLength={500}
                          />
                        </label>
                      </div>
                      <div>
                        <label className="block text-sm mb-1">
                          Trailer URL
                          <input
                            type="url"
                            value={editTrailerUrl}
                            onChange={(e) => setEditTrailerUrl(e.target.value)}
                            className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                          />
                        </label>
                      </div>
                      <div className="flex gap-2">
                        <button
                          onClick={() => handleSaveEdit(m.id)}
                          className="text-xs px-3 py-1 bg-accent/20 border border-accent rounded hover:bg-accent/30"
                        >
                          Save
                        </button>
                        <button
                          onClick={handleCancelEdit}
                          className="text-xs px-3 py-1 border border-zinc-700 rounded hover:border-zinc-500"
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-between">
                      <div className="flex-1">
                        <div className={m.hidden ? "line-through" : ""}>{m.title}</div>
                        {m.description && (
                          <div className="text-xs opacity-70 italic mt-1">{m.description}</div>
                        )}
                        <div className="text-xs opacity-60 mt-1">
                          {m.submittedBy && <span>By: {m.submittedBy}</span>}
                          {m.deviceId && <span className="ml-3">Device: {m.deviceId.slice(0, 8)}...</span>}
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-accent font-mono text-sm">{m._count.votes} votes</span>
                        <button
                          onClick={() => handleStartEdit(m)}
                          className="text-xs px-3 py-1 border border-zinc-700 rounded hover:border-zinc-500"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => handleToggleMovie(m.id)}
                          className="text-xs px-3 py-1 border border-zinc-700 rounded hover:border-zinc-500"
                        >
                          {m.hidden ? "Show" : "Hide"}
                        </button>
                        <button
                          onClick={() => handleDeleteMovie(m.id)}
                          className="text-xs px-3 py-1 border border-red-700 rounded hover:border-red-500"
                        >
                          Delete
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Voters Tab */}
      {activeTab === "voters" && (
        <div className="space-y-6">
          <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
            <h2 className="text-xl font-semibold mb-4">Device Votes</h2>
            <p className="text-sm opacity-60 mb-4">
              Votes cast by device ID (cookies/tokens)
            </p>
            {voters.length === 0 ? (
              <p className="opacity-60 text-center py-4">No device votes cast</p>
            ) : (
              <div className="space-y-2">
                {voters.map((v) => (
                  <div key={v.id} className="flex items-center justify-between border-b border-zinc-800 py-3">
                    <div className="flex-1">
                      <div className={v.movie.hidden ? "line-through opacity-60" : ""}>
                        {v.movie.title}
                      </div>
                      <div className="text-xs opacity-60 mt-1">
                        {v.email ? (
                          <>Email: {v.email} | </>
                        ) : (
                          <>Device: {v.deviceId.slice(0, 8)}... | </>
                        )}
                        IP: {v.ipHash.slice(0, 8)}... |{" "}
                        {new Date(v.createdAt).toLocaleString()}
                      </div>
                    </div>
                    <button
                      onClick={() => handleDeleteVote(v.id)}
                      className="text-xs px-3 py-1 border border-red-700 rounded hover:border-red-500"
                    >
                      Delete
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
            <h2 className="text-xl font-semibold mb-4">Email Voters</h2>
            <p className="text-sm opacity-60 mb-4">
              People who voted using their email address
            </p>
            {whitelistVoters.length === 0 ? (
              <p className="opacity-60 text-center py-4">No email votes cast</p>
            ) : (
              <div className="space-y-2">
                {whitelistVoters.map((v) => (
                  <div key={v.id} className="flex items-center justify-between border-b border-zinc-800 py-3">
                    <div className="flex-1">
                      <div className="font-semibold">{v.email}</div>
                      {v.name && <div className="text-sm opacity-80">{v.name}</div>}
                      <div className="text-xs opacity-60 mt-1">
                        Voted at: {v.votedAt ? new Date(v.votedAt).toLocaleString() : "N/A"}
                      </div>
                    </div>
                    <button
                      onClick={() => handleRemoveVoter(v.email)}
                      className="text-xs px-3 py-1 border border-red-700 rounded hover:border-red-500"
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Limits Tab */}
      {activeTab === "limits" && (
        <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
          <h2 className="text-xl font-semibold mb-4">Submission Limits</h2>
          <div className="mb-6 p-4 bg-zinc-900/50 rounded">
            <p className="text-sm">
              <span className="opacity-60">Default limit:</span>{" "}
              <span className="text-accent font-semibold">{season?.defaultSubmitLimit || 1}</span> submission(s) per email
            </p>
            <p className="text-xs opacity-60 mt-1">Change this in Settings tab</p>
          </div>

          <form onSubmit={handleSetLimit} className="space-y-4 mb-6">
            <h3 className="font-semibold">Set Custom Limit for Email</h3>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm mb-2">
                  Email
                  <input
                    type="email"
                    value={newLimitEmail}
                    onChange={(e) => setNewLimitEmail(e.target.value)}
                    required
                    className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                    placeholder="user@example.com"
                  />
                </label>
              </div>
              <div>
                <label className="block text-sm mb-2">
                  Custom Limit
                  <input
                    type="number"
                    value={newLimitValue}
                    onChange={(e) => setNewLimitValue(e.target.value)}
                    min={0}
                    required
                    className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  />
                </label>
              </div>
            </div>
            <button
              type="submit"
              className="btn-accent rounded px-4 py-2 hover:bg-accent/10 transition-colors"
            >
              Set Custom Limit
            </button>
          </form>

          {overrides.length === 0 ? (
            <p className="text-sm opacity-60 text-center py-4">No custom limits set</p>
          ) : (
            <div className="space-y-2">
              <h3 className="font-semibold mb-2">Active Custom Limits</h3>
              {overrides.map((o) => (
                <div key={o.id} className="flex items-center justify-between border-b border-zinc-800 py-3">
                  <span className="text-sm">{o.email}</span>
                  <span className="text-accent font-mono text-sm">{o.limit} submission(s)</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Account Tab */}
      {activeTab === "account" && (
        <div className="border border-chrome rounded-lg p-6 bg-zinc-950/40">
          <h2 className="text-xl font-semibold mb-4">Account Settings</h2>

          <form onSubmit={handleChangePassword} className="space-y-4 max-w-md">
            <div>
              <label className="block text-sm mb-2">
                Current Password
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  required
                />
              </label>
            </div>

            <div>
              <label className="block text-sm mb-2">
                New Password
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  required
                  minLength={8}
                />
              </label>
              <p className="text-xs opacity-60 mt-1">Minimum 8 characters</p>
            </div>

            <div>
              <label className="block text-sm mb-2">
                Confirm New Password
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  className="mt-1 block w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2"
                  required
                  minLength={8}
                />
              </label>
            </div>

            <button
              type="submit"
              className="btn-accent rounded px-4 py-2 hover:bg-accent/10 transition-colors"
            >
              Change Password
            </button>
          </form>
        </div>
      )}
    </div>
  );
}
