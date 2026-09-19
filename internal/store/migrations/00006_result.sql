-- +goose Up
-- Written once, when a season locks. `rounds` is the frozen round-by-round
-- elimination breakdown, which is why a past year stays browsable even if the
-- underlying ballots are later removed.
CREATE TABLE result (
    season_id       uuid        NOT NULL,
    -- Nullable: a tie in the final round has no single winner.
    winner_movie_id uuid,
    rounds          jsonb       NOT NULL DEFAULT '[]'::jsonb,
    tallied_at      timestamptz NOT NULL DEFAULT now(),

    -- One result per season.
    CONSTRAINT result_pkey PRIMARY KEY (season_id),

    CONSTRAINT result_season_id_fkey FOREIGN KEY (season_id)
        REFERENCES season (id) ON DELETE CASCADE,

    -- Composite so the winner is guaranteed to be from this season. MATCH
    -- SIMPLE means a NULL winner_movie_id satisfies the constraint outright,
    -- which is exactly the tie case.
    --
    -- NO ACTION for the same reason as ballot_entry, which is NOT the reason
    -- an earlier version of this comment gave: NO ACTION and RESTRICT are both
    -- checked at end of statement, so the choice buys nothing here today. It
    -- is kept because only NO ACTION honours DEFERRABLE INITIALLY DEFERRED --
    -- RESTRICT accepts the clause and is silently downgraded, visible in
    -- pg_trigger.tgdeferrable and never in pg_constraint.condeferred. See the
    -- long note on ballot_entry_movie_fkey in 00005 before changing either.
    CONSTRAINT result_winner_movie_fkey FOREIGN KEY (season_id, winner_movie_id)
        REFERENCES movie (season_id, id) ON DELETE NO ACTION,

    CONSTRAINT result_rounds_is_array CHECK (jsonb_typeof(rounds) = 'array')
);

-- FK index for the end-of-statement winner check and for "did this movie ever
-- win" lookups on the hall-of-fame page.
CREATE INDEX result_winner_movie_id_idx ON result (winner_movie_id)
    WHERE winner_movie_id IS NOT NULL;

-- +goose Down
DROP TABLE result;
