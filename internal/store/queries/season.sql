-- One row per year. Past seasons stay browsable forever, so the list queries
-- here are what the archive is built from.

-- name: GetSeason :one
SELECT * FROM season
WHERE id = $1;

-- name: GetSeasonByYear :one
SELECT * FROM season
WHERE year = $1;

-- name: GetCurrentSeason :one
-- "Current" is the newest season that has left draft. A draft season is not
-- visible to anyone but an admin, and it must not shadow last year's locked
-- season on the front page -- so a half-configured 2027 cannot hide 2026.
-- Admins reach a draft season through GetSeasonByYear or ListSeasons.
SELECT * FROM season
WHERE state <> 'draft'
ORDER BY year DESC
LIMIT 1;

-- name: ListSeasons :many
-- Every season including drafts. Admin only -- the public archive uses
-- ListPublishedSeasons.
SELECT * FROM season
ORDER BY year DESC;

-- name: ListPublishedSeasons :many
-- The archive. Drafts are excluded here rather than filtered in a handler so
-- that forgetting the filter is not a way to leak next year's season.
SELECT * FROM season
WHERE state <> 'draft'
ORDER BY year DESC;

-- name: UpdateSeasonState :one
-- state and locked_at have to move together: the season_locked_has_timestamp
-- CHECK says a season is locked if and only if it records when it locked.
-- Deriving locked_at here means no caller can construct a state change the
-- database will reject. COALESCE preserves the original lock time if a locked
-- season is re-locked.
UPDATE season
SET state = sqlc.arg(state),
    locked_at = CASE
        WHEN sqlc.arg(state) = 'locked' THEN COALESCE(locked_at, now())
        ELSE NULL
    END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateSeasonWindows :one
-- All three are nullable: a season can be created before its dates are
-- decided, and clearing a window back to NULL is a legitimate edit.
UPDATE season
SET submit_opens_at = sqlc.narg(submit_opens_at),
    vote_opens_at   = sqlc.narg(vote_opens_at),
    vote_closes_at  = sqlc.narg(vote_closes_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateSeason :one
-- The production season-creation path (ticket D3). Production is never seeded:
-- an admin types a year, a name and two dates into a form, and this is what
-- that form runs.
--
-- state is a parameter rather than a default because the admin screen decides
-- it: creating a season IS the act of opening it, so the form posts
-- 'submitting'. 'draft' stays reachable for a season being prepared ahead of
-- time, which is what season.state's own DEFAULT would have given -- but a
-- default here would mean the ordinary case had to remember to override it,
-- and the ordinary case is the one that must not need remembering.
--
-- No locked_at parameter, and none is possible: season_locked_has_timestamp
-- says a season is locked if and only if it records when, so a season created
-- directly into 'locked' would be an archive with no ballots in it. The CHECK
-- refuses it, which is the right answer.
--
-- year is UNIQUE (season_year_key), so a second attempt for the same year
-- comes back as SQLSTATE 23505 and the handler turns it into a sentence.
INSERT INTO season (
    year,
    name,
    state,
    submit_opens_at,
    vote_opens_at,
    vote_closes_at,
    default_submit_limit
) VALUES (
    sqlc.arg(year),
    sqlc.arg(name),
    sqlc.arg(state),
    sqlc.narg(submit_opens_at),
    sqlc.narg(vote_opens_at),
    sqlc.narg(vote_closes_at),
    sqlc.arg(default_submit_limit)
)
RETURNING *;

-- name: UpdateSeasonDetails :one
-- Re-dating a season after it exists, which is the half of ticket D3 that gets
-- used more than once: the submission deadline slips every year.
--
-- Deliberately NOT a superset of UpdateSeasonState. state and locked_at have to
-- move together and that query derives locked_at so no caller can construct a
-- combination the CHECK refuses; folding the two together here would hand every
-- date edit the ability to lock a season by accident.
--
-- year is not updatable either. It is the season's identity -- the UNIQUE key
-- the archive is browsed by -- and "the 2026 season is now 2027" is a different
-- operation from "the 2026 deadline moved", with different consequences for
-- every link already pasted into the group chat.
UPDATE season
SET name                 = sqlc.arg(name),
    submit_opens_at      = sqlc.narg(submit_opens_at),
    vote_opens_at        = sqlc.narg(vote_opens_at),
    vote_closes_at       = sqlc.narg(vote_closes_at),
    default_submit_limit = sqlc.arg(default_submit_limit)
WHERE id = sqlc.arg(id)
RETURNING *;
