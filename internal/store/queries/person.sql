-- person is the identity spine. These queries cover sign-in only; everything
-- that decides whether a person may do something lives in season_member.

-- name: GetPerson :one
SELECT * FROM person
WHERE id = $1;

-- name: GetPersonByEmailNormalized :one
-- The whitelist lookup. Admins add people by email before those people have
-- ever signed in, so this is how a brand new Google identity finds the row
-- that was already waiting for it.
SELECT * FROM person
WHERE email_normalized = $1;

-- name: GetPersonByGoogleSub :one
-- The returning-visitor lookup, and the reason a person can change their email
-- address without becoming a new person. Sign-in tries this first.
--
-- The cast forces a non-null parameter. The column is nullable, but looking a
-- person up by a null subject is not a thing we ever want to do -- and in SQL
-- it would quietly match nothing rather than fail, so making it unrepresentable
-- in Go is the honest version.
SELECT * FROM person
WHERE google_sub = sqlc.arg(google_sub)::text;

-- name: UpsertPersonOnSignIn :one
-- The email path of sign-in: used when no row matched google_sub, which means
-- either a first sign-in against a whitelisted email or a genuinely new person.
--
-- The WHERE on the DO UPDATE is an account-takeover guard, not a tidy-up.
-- Without it, a Google identity that has never signed in here misses the
-- google_sub lookup, falls through to this query, conflicts on a whitelisted
-- email and overwrites the stored google_sub -- inheriting that person's
-- submissions, ballots and is_admin flag. The victim's next sign-in then also
-- misses the sub lookup and clobbers it back, so two identities ping-pong on
-- one row. Google ties sub to the account object rather than to the address,
-- so a recreated account or a recycled address is enough to trigger it.
-- Reproduced end to end; TestSignInTakeoverIsBlocked replays it.
--
-- person_google_sub_key UNIQUE (google_sub) does NOT prevent this. That is the
-- trap in reading the constraint list and assuming it is covered: the incoming
-- sub exists nowhere else in the table, so the unique index is satisfied by
-- MOVING it onto the victim's row. It only stops two ROWS holding the same sub.
--
-- So the update applies to a row only when it is unclaimed (google_sub IS NULL:
-- whitelisted, never signed in) or already owned by this same subject. A
-- genuine collision updates nothing, RETURNING yields no row, and :one surfaces
-- pgx.ErrNoRows. THE CALLER MUST NOT READ THAT AS "person not found": it means
-- "this email belongs to a different Google identity", which is refuse-sign-in
-- and escalate to an admin.
--
-- google_sub is a non-null parameter even though the column is nullable. The
-- column must allow null for whitelisted rows that have never signed in, but a
-- sign-in always carries a subject: internal/auth rejects an ID token whose sub
-- is absent or empty (ErrNoSubject) before it can reach here. That matters,
-- because with a nullable parameter `person.google_sub = EXCLUDED.google_sub`
-- evaluates to NULL rather than true for an already-linked person, the update
-- silently skips, and a legitimate sign-in is misreported as a collision. If
-- the ErrNoSubject gate is ever removed, this parameter has to go back to
-- sqlc.narg and the guard needs a third arm, `OR EXCLUDED.google_sub IS NULL`.
INSERT INTO person (email, email_normalized, google_sub, display_name)
VALUES (
    sqlc.arg(email),
    sqlc.arg(email_normalized),
    sqlc.arg(google_sub)::text,
    sqlc.arg(display_name)
)
ON CONFLICT (email_normalized) DO UPDATE
SET email        = EXCLUDED.email,
    google_sub   = EXCLUDED.google_sub,
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), person.display_name)
WHERE person.google_sub IS NULL
   OR person.google_sub = EXCLUDED.google_sub
RETURNING *;

-- name: UpdatePersonIdentity :one
-- The google_sub path of sign-in: the person is already known, but Google may
-- have handed us a new email address or display name. Without this the
-- GetPersonByGoogleSub lookup would be read-only and an email change would
-- never be recorded.
UPDATE person
SET email            = sqlc.arg(email),
    email_normalized = sqlc.arg(email_normalized),
    display_name     = COALESCE(NULLIF(sqlc.arg(display_name)::text, ''), display_name)
WHERE id = sqlc.arg(id)
RETURNING *;
