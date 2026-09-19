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
