-- +goose Up
-- A submission. `hidden` is a soft delete: the tally skips hidden movies but
-- ballot rows that ranked them survive, because hard-deleting a movie would
-- silently rewrite everyone's ballot.
CREATE TABLE movie (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id    uuid        NOT NULL,
    submitted_by uuid        NOT NULL,
    title        text        NOT NULL,
    -- Release year of the film, not the season year. Nullable: TMDB does not
    -- always have one and a manual entry may not know it.
    year         integer,
    tmdb_id      integer,
    trailer_url  text,
    description  text        NOT NULL DEFAULT '',
    hidden       boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),

    -- Deleting a season deletes the year wholesale.
    CONSTRAINT movie_season_id_fkey FOREIGN KEY (season_id)
        REFERENCES season (id) ON DELETE CASCADE,
    -- RESTRICT, not CASCADE and not SET NULL: deleting a person who submitted
    -- a movie would either destroy a past season's slate or leave an
    -- unattributed film. Blocked on purpose -- remove them from
    -- season_member instead.
    CONSTRAINT movie_submitted_by_fkey FOREIGN KEY (submitted_by)
        REFERENCES person (id) ON DELETE RESTRICT,

    -- FK target for ballot_entry's composite reference. This is what makes
    -- "rank a movie from a different season" structurally impossible.
    CONSTRAINT movie_season_id_id_key UNIQUE (season_id, id),

    CONSTRAINT movie_title_not_blank CHECK (title <> ''),
    CONSTRAINT movie_year_range      CHECK (year IS NULL OR year BETWEEN 1888 AND 2200),
    CONSTRAINT movie_trailer_url_shape
        CHECK (trailer_url IS NULL OR trailer_url ~ '^https?://')
);

-- The tally and the submissions page both read "visible movies in this
-- season". The UNIQUE (season_id, id) index already covers plain season_id
-- lookups, so this partial index is the one that earns its keep.
CREATE INDEX movie_season_visible_idx ON movie (season_id) WHERE NOT hidden;
-- FK index for the RESTRICT check and for "what did this person submit".
CREATE INDEX movie_submitted_by_idx ON movie (submitted_by);

-- Two people submitting the same film would split the ranked-choice vote, so
-- the same TMDB title can only be live once per season. Hidden rows are
-- excluded so a soft-deleted duplicate does not block a resubmission.
CREATE UNIQUE INDEX movie_season_tmdb_unique_idx
    ON movie (season_id, tmdb_id)
    WHERE tmdb_id IS NOT NULL AND NOT hidden;

-- +goose Down
DROP TABLE movie;
