-- Submissions. `hidden` is a soft delete everywhere: a hidden movie is not on
-- the slate, does not count against its submitter's cap, and does not block a
-- resubmission of the same TMDB id -- but the ballot rows that ranked it
-- survive.

-- name: GetMovie :one
SELECT * FROM movie
WHERE id = $1;

-- name: CreateMovie :one
INSERT INTO movie (
    season_id,
    submitted_by,
    title,
    year,
    tmdb_id,
    trailer_url,
    description
) VALUES (
    sqlc.arg(season_id),
    sqlc.arg(submitted_by),
    sqlc.arg(title),
    sqlc.narg(year),
    sqlc.narg(tmdb_id),
    sqlc.narg(trailer_url),
    sqlc.arg(description)
)
RETURNING *;

-- name: ListVisibleMoviesForSeason :many
-- The slate: what the submissions page shows and what the ballot is built
-- from. Ordered by submission time with an id tiebreak so the list is stable
-- across page loads.
SELECT * FROM movie
WHERE season_id = $1
  AND NOT hidden
ORDER BY created_at, id;

-- name: ListPersonMoviesForSeason :many
-- A person's own live submissions. Hidden rows are excluded so that this list
-- and CountPersonMoviesInSeason always agree -- if the page shows two films,
-- the cap has counted two.
SELECT * FROM movie
WHERE season_id = sqlc.arg(season_id)
  AND submitted_by = sqlc.arg(submitted_by)
  AND NOT hidden
ORDER BY created_at, id;

-- name: CountPersonMoviesInSeason :one
-- Backs the per-person submission cap. Hidden rows must not count: otherwise
-- withdrawing a film and submitting a different one would permanently burn a
-- slot.
SELECT count(*) FROM movie
WHERE season_id = sqlc.arg(season_id)
  AND submitted_by = sqlc.arg(submitted_by)
  AND NOT hidden;

-- name: UpdateMovie :one
-- Scoped by id alone; who may edit a submission is an authorisation question
-- the handler answers, not something to smuggle into a WHERE clause.
UPDATE movie
SET title       = sqlc.arg(title),
    year        = sqlc.narg(year),
    tmdb_id     = sqlc.narg(tmdb_id),
    trailer_url = sqlc.narg(trailer_url),
    description = sqlc.arg(description)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetMovieHidden :one
-- The soft delete, and its undo. There is deliberately no DELETE FROM movie:
-- ballot_entry's foreign key would block it anyway, which is the point.
UPDATE movie
SET hidden = sqlc.arg(hidden)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListVisibleMoviesWithSubmitterForSeason :many
-- The slate as a page renders it: every live submission, with the name to
-- credit it to.
--
-- ListVisibleMoviesForSeason returns the same rows without the join and stays
-- as it is -- the tally has no use for a display name. This one exists because
-- the only alternative on the page side is a person lookup per card.
--
-- An INNER JOIN is safe here, and that is a property of the schema rather than
-- an assumption: movie.submitted_by is NOT NULL and ON DELETE RESTRICT against
-- person, so the submitter row cannot be missing and the join cannot silently
-- drop a film off the slate.
--
-- Joined to person and deliberately NOT to season_member. Someone removed from
-- the season keeps their films on the slate (see DeleteSeasonMember), and
-- joining through the roster would make those films lose their name -- or,
-- inner-joined, vanish.
--
-- display_name may be '': a whitelisted person who has never signed in has no
-- name at all. That blank is the page's to handle (MovieCard.SubmitterLabel),
-- not this query's to paper over -- substituting the email here would put an
-- address on a public page.
SELECT sqlc.embed(movie), person.display_name
FROM movie
JOIN person ON person.id = movie.submitted_by
WHERE movie.season_id = $1
  AND NOT movie.hidden
ORDER BY movie.created_at, movie.id;
