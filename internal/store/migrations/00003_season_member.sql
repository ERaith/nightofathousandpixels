-- +goose Up
-- The per-year whitelist. Membership is what gates sign-in, and admin is a
-- per-season flag -- there is no global admin, so running a season does not
-- hand anyone the keys to every other year.
CREATE TABLE season_member (
    season_id    uuid        NOT NULL,
    person_id    uuid        NOT NULL,
    is_admin     boolean     NOT NULL DEFAULT false,
    -- NULL means "use season.default_submit_limit". A concrete value is a
    -- per-person override (0 = may vote but may not submit).
    submit_limit integer,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT season_member_pkey PRIMARY KEY (season_id, person_id),

    -- Deleting a season deletes the year wholesale, memberships included.
    CONSTRAINT season_member_season_id_fkey FOREIGN KEY (season_id)
        REFERENCES season (id) ON DELETE CASCADE,
    -- A membership is meaningless without the person it names.
    CONSTRAINT season_member_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES person (id) ON DELETE CASCADE,

    CONSTRAINT season_member_submit_limit_non_negative
        CHECK (submit_limit IS NULL OR submit_limit >= 0)
);

-- The PK already indexes season_id (leading column); person_id needs its own
-- index for "which seasons was this person in" and for the FK delete check.
CREATE INDEX season_member_person_id_idx ON season_member (person_id);
-- Admin lookups are per season and the admin set is tiny.
CREATE INDEX season_member_admin_idx ON season_member (season_id) WHERE is_admin;

-- +goose Down
DROP TABLE season_member;
