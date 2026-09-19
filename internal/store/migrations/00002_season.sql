-- +goose Up
-- One row per year. Past seasons stay browsable forever, so a season row is
-- effectively immutable once state reaches 'locked'.
CREATE TABLE season (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    year                 integer     NOT NULL,
    name                 text        NOT NULL,
    theme_pack           text        NOT NULL DEFAULT 'default',

    -- The four windows that drive the UI. All nullable: a season can be
    -- created before the dates are decided.
    submit_opens_at      timestamptz,
    vote_opens_at        timestamptz,
    vote_closes_at       timestamptz,
    locked_at            timestamptz,

    -- state is text + CHECK rather than an enum type so that adding a phase
    -- later is a reversible ALTER. Postgres enums cannot drop a value, which
    -- would leave us with a migration that has no working down.
    state                text        NOT NULL DEFAULT 'draft',

    default_submit_limit integer     NOT NULL DEFAULT 2,
    created_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT season_year_key   UNIQUE (year),
    CONSTRAINT season_year_range CHECK (year BETWEEN 1888 AND 2200),
    CONSTRAINT season_name_not_blank CHECK (name <> ''),
    CONSTRAINT season_state_valid
        CHECK (state IN ('draft', 'submitting', 'voting', 'locked')),
    CONSTRAINT season_default_submit_limit_positive
        CHECK (default_submit_limit >= 0),
    -- Windows must not run backwards. Each comparison is null-tolerant so a
    -- partially-scheduled season is still insertable.
    CONSTRAINT season_window_order CHECK (
        (submit_opens_at IS NULL OR vote_opens_at  IS NULL OR submit_opens_at <= vote_opens_at)
    AND (vote_opens_at   IS NULL OR vote_closes_at IS NULL OR vote_opens_at   <= vote_closes_at)
    ),
    -- A locked season must actually record when it locked, and nothing else
    -- may claim a locked_at.
    CONSTRAINT season_locked_has_timestamp
        CHECK ((state = 'locked') = (locked_at IS NOT NULL))
);

-- The season list page and every "current season" lookup order by year.
CREATE INDEX season_state_year_idx ON season (state, year DESC);

-- +goose Down
DROP TABLE season;
