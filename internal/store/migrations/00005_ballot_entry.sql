-- +goose Up
-- One row per (voter, rank) -- deliberately NOT one row with three columns.
-- Supporting a 4th or 5th rank later is then a config change, not a
-- migration.
CREATE TABLE ballot_entry (
    season_id  uuid        NOT NULL,
    person_id  uuid        NOT NULL,
    movie_id   uuid        NOT NULL,
    rank       integer     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- One movie per rank position. This is the PK because it is also the
    -- natural identity of a ballot entry.
    CONSTRAINT ballot_entry_pkey PRIMARY KEY (season_id, person_id, rank),
    -- Cannot rank the same movie twice.
    CONSTRAINT ballot_entry_season_person_movie_key
        UNIQUE (season_id, person_id, movie_id),

    -- Eligibility to vote IS season membership, so the voter reference goes
    -- through season_member rather than straight to person. Removing someone
    -- from a season's whitelist removes their ballot for that season; person
    -- deletion reaches here transitively (person -> season_member -> here).
    CONSTRAINT ballot_entry_voter_fkey FOREIGN KEY (season_id, person_id)
        REFERENCES season_member (season_id, person_id) ON DELETE CASCADE,

    -- This blocks a bare `DELETE FROM movie WHERE id = ...` -- soft-delete
    -- with hidden = true instead. NO ACTION rather than RESTRICT: RESTRICT is
    -- checked after each row, NO ACTION at end of statement. Both pass the
    -- `DELETE FROM season` cascade today (measured, not assumed), but only
    -- because Postgres happens to walk season -> season_member ->
    -- ballot_entry before season -> movie. That ordering is not guaranteed,
    -- and NO ACTION does not depend on it: by end of statement the ballot
    -- rows are gone either way.
    CONSTRAINT ballot_entry_movie_fkey FOREIGN KEY (season_id, movie_id)
        REFERENCES movie (season_id, id) ON DELETE NO ACTION,

    -- No upper bound. The ballot depth is config (currently 3); capping it
    -- here would make raising it a migration, which is the shape this table
    -- exists to avoid.
    CONSTRAINT ballot_entry_rank_positive CHECK (rank >= 1)
);

-- The tally reads every entry for a season ordered by voter and rank, which
-- the PK covers. This index serves the per-movie direction: first-choice
-- counts, "who ranked this", and the movie FK's end-of-statement check.
CREATE INDEX ballot_entry_season_movie_idx ON ballot_entry (season_id, movie_id);

-- +goose Down
DROP TABLE ballot_entry;
