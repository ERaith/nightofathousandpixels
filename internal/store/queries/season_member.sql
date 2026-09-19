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
