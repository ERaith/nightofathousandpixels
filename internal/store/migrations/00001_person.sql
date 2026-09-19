-- +goose Up
-- person is the identity spine: one row per human, forever. Rows are never
-- deleted in normal operation -- membership in a given year lives in
-- season_member, so someone leaving the group is a season_member delete.
CREATE TABLE person (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- email is the human-facing key (what an admin types into the whitelist).
    email            text        NOT NULL,
    -- email_normalized is what we actually match on. The app owns the
    -- normalization policy (case, plus-tags, gmail dots), so this is a plain
    -- column rather than a generated one.
    email_normalized text        NOT NULL,
    -- google_sub is the stable OIDC subject. Null until first sign-in, and it
    -- is what lets a person change their email without becoming a new person.
    google_sub       text,
    display_name     text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT person_email_normalized_key UNIQUE (email_normalized),
    CONSTRAINT person_google_sub_key       UNIQUE (google_sub),
    CONSTRAINT person_email_not_blank      CHECK (email <> ''),
    CONSTRAINT person_email_normalized_not_blank CHECK (email_normalized <> '')
);

-- +goose Down
DROP TABLE person;
