-- The per-year whitelist. Membership is what gates sign-in, admin is per
-- season, and submit_limit is a per-person override of the season default.

-- name: GetSeasonMember :one
-- Returns no rows for a person who is not in this season, which is the
-- authorisation answer as well as the lookup result.
SELECT * FROM season_member
WHERE season_id = sqlc.arg(season_id)
  AND person_id = sqlc.arg(person_id);

-- name: ListSeasonMembers :many
-- The admin whitelist page. Joined to person because a list of uuids is not a
-- whitelist anyone can read; email is the column an admin actually recognises
-- and is therefore also the sort key.
SELECT
    sqlc.embed(season_member),
    person.email,
    person.email_normalized,
    person.display_name
FROM season_member
JOIN person ON person.id = season_member.person_id
WHERE season_member.season_id = $1
ORDER BY person.email;

-- name: UpsertSeasonMember :one
-- Adding someone who is already a member updates their flags rather than
-- failing, so an admin re-pasting a whitelist is idempotent.
--
-- submit_limit is written through as-is, NULL included: passing NULL is how an
-- admin removes an override and puts someone back on the season default.
INSERT INTO season_member (season_id, person_id, is_admin, submit_limit)
VALUES (
    sqlc.arg(season_id),
    sqlc.arg(person_id),
    sqlc.arg(is_admin),
    sqlc.narg(submit_limit)
)
ON CONFLICT (season_id, person_id) DO UPDATE
SET is_admin     = EXCLUDED.is_admin,
    submit_limit = EXCLUDED.submit_limit
RETURNING *;

-- name: DeleteSeasonMember :exec
-- Removing someone from a season's whitelist. This cascades to their ballot
-- for that season (ballot_entry references season_member), but not to their
-- submissions -- movie.submitted_by is ON DELETE RESTRICT against person and
-- is not reached from here at all, so a removed member's films stay on the
-- slate. Hide them explicitly if that is what is wanted.
DELETE FROM season_member
WHERE season_id = sqlc.arg(season_id)
  AND person_id = sqlc.arg(person_id);

-- name: GetEffectiveSubmitLimit :one
-- The whole reason the override lives on the membership row: NULL means "use
-- the season default". Resolving that COALESCE here, once, is what stops every
-- handler from reimplementing it -- and reimplementing it wrongly, because the
-- obvious wrong version treats a NULL override as 0 and silently bans the
-- person from submitting.
--
-- An explicit 0 override is honoured: that person may vote but may not submit.
-- No rows means not a member of this season, which is a different answer from
-- a limit of 0.
SELECT COALESCE(season_member.submit_limit, season.default_submit_limit)::integer
    AS effective_submit_limit
FROM season_member
JOIN season ON season.id = season_member.season_id
WHERE season_member.season_id = sqlc.arg(season_id)
  AND season_member.person_id = sqlc.arg(person_id);

-- name: LockSubmitLimitForUpdate :one
-- GetEffectiveSubmitLimit, plus the row lock that makes the submission cap
-- actually hold.
--
-- The cap is enforced by counting a person's live submissions and then
-- inserting one more. Those are two statements, and putting them in one
-- transaction does NOT by itself make the pair safe: at READ COMMITTED -- the
-- default, and what pgxpool hands out -- two concurrent submissions from the
-- same person both read a count of 1, both pass a limit of 2, and both
-- commit. The transaction gives atomicity, not mutual exclusion.
--
-- FOR UPDATE is what supplies the mutual exclusion. The second transaction
-- blocks here until the first commits, and then counts the row the first one
-- inserted. It is taken on the membership row because that is the thing the
-- cap is per: one lock per person per season, so two different people
-- submitting at the same moment never wait on each other.
--
-- OF season_member is load-bearing. A bare FOR UPDATE would also lock the
-- joined season row, which would serialise every submission in the season
-- behind one another and block an admin editing the season's windows for as
-- long as any submission is open.
--
-- Everything else is GetEffectiveSubmitLimit's contract, unchanged and for the
-- same reasons: the COALESCE is resolved here so no handler reimplements it,
-- an explicit 0 override is honoured ("may vote, may not submit"), and no rows
-- means not a member of this season -- which is a different answer from a
-- limit of 0, and is also the authorisation answer, since nothing in the
-- schema stops a non-member's INSERT into movie.
SELECT COALESCE(season_member.submit_limit, season.default_submit_limit)::integer
    AS effective_submit_limit
FROM season_member
JOIN season ON season.id = season_member.season_id
WHERE season_member.season_id = sqlc.arg(season_id)
  AND season_member.person_id = sqlc.arg(person_id)
FOR UPDATE OF season_member;
