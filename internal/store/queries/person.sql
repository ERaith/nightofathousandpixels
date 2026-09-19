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
-- google_sub is COALESCEd so that a null subject (should not happen, but the
-- column allows it) cannot unlink an already-linked person, and display_name
-- falls back to the stored value so a provider that returns no name does not
-- blank out one we already had.
INSERT INTO person (email, email_normalized, google_sub, display_name)
VALUES (
    sqlc.arg(email),
    sqlc.arg(email_normalized),
    sqlc.narg(google_sub),
    sqlc.arg(display_name)
)
ON CONFLICT (email_normalized) DO UPDATE
SET email        = EXCLUDED.email,
    google_sub   = COALESCE(EXCLUDED.google_sub, person.google_sub),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), person.display_name)
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
