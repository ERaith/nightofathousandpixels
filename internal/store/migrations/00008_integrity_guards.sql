-- +goose Up
-- Two integrity holes in the v1 schema, both about deletion. Neither changes
-- any table's column shape, so sqlc's generated output is untouched.
--
-- Correction to a comment in 00005_ballot_entry.sql, which this migration
-- supersedes: that comment says RESTRICT is checked per row while NO ACTION is
-- checked "at end of statement", and concludes that NO ACTION does not depend
-- on the order Postgres walks the cascade out of season. That is false, and it
-- was measured: two schemas differing only in RESTRICT vs NO ACTION behave
-- identically, and both fail once the constraint order is flipped. Inside a
-- cascading delete both are checked within the cascade, not after it. Only
-- DEFERRABLE INITIALLY DEFERRED genuinely postpones the check to COMMIT, which
-- is what part 1 below does.

-- ---------------------------------------------------------------------------
-- 1. DELETE FROM season must not depend on constraint creation order.
-- ---------------------------------------------------------------------------
-- Deleting a season fans out down two cascade paths that overlap on movie:
--
--     season -> season_member -> ballot_entry     (removes the ballot rows)
--     season -> movie                             (checked by ballot_entry)
--
-- The movie reference only passes if the ballot rows are already gone, i.e.
-- only if the season_member path runs first. Which path runs first is trigger
-- firing order, which is constraint OID order. The migrations happen to create
-- season_member's FK (00003) before movie's (00004), so a freshly-migrated
-- database works. pg_dump recreates constraints in table-name order, which
-- puts ballot_entry's movie FK first and flips it -- so `DELETE FROM season`
-- fails on any restored database and succeeds on any fresh one. Since the
-- homelab recovery path is `goose down` plus the nightly dump, the broken case
-- is exactly the database being recovered.
--
-- Making the two movie-referencing constraints DEFERRABLE INITIALLY DEFERRED
-- moves the check to COMMIT, after every cascade has run, so the order stops
-- mattering. The protection this constraint exists for is unaffected: a bare
-- `DELETE FROM movie` that a ballot ranked still fails, just at COMMIT rather
-- than at the statement -- soft-delete with hidden = true is still the only
-- way to take a ranked movie out of the running.
ALTER TABLE ballot_entry DROP CONSTRAINT ballot_entry_movie_fkey;
ALTER TABLE ballot_entry ADD CONSTRAINT ballot_entry_movie_fkey
    FOREIGN KEY (season_id, movie_id) REFERENCES movie (season_id, id)
    ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE result DROP CONSTRAINT result_winner_movie_fkey;
ALTER TABLE result ADD CONSTRAINT result_winner_movie_fkey
    FOREIGN KEY (season_id, winner_movie_id) REFERENCES movie (season_id, id)
    ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED;

-- ---------------------------------------------------------------------------
-- 2. A locked season is the archive. Nothing may delete out of it piecemeal.
-- ---------------------------------------------------------------------------
-- season_member -> person and ballot_entry -> season_member are both
-- ON DELETE CASCADE, so deleting a person, or removing a member, silently
-- deletes that voter's ballots in *every* season they were ever in, locked
-- ones included. 00001 calls a season_member delete the routine way someone
-- leaves the group, so this is a normal admin action aimed at a past year.
--
-- It is invisible rather than loud: result.rounds is a frozen snapshot, so the
-- archive keeps publishing the old counts while the ballots behind them are
-- gone. "Past years stay browsable forever" is the reason this project exists.
-- The comments in 00001 and 00002 assert that person rows are never deleted
-- and that a locked season is immutable; nothing enforced either. This does.
--
-- The guard has to let one thing through: deliberately deleting a whole season
-- must still work, locked or not, and that runs down the same cascade. The
-- discriminator is that during `DELETE FROM season` the season row has already
-- been deleted by the time the cascade fires, so it is not visible to the
-- child's trigger -- while on the person and season_member paths it is still
-- there. So: raise only when the parent season is still present AND locked.
-- (Measured on 16.15 for all three origins, and it holds at every isolation
-- level, because a transaction always sees its own earlier commands' effects.)
--
-- Known limit: TRUNCATE does not fire row-level DELETE triggers, so
-- `TRUNCATE ballot_entry` still bypasses this. TRUNCATE is a wholesale wipe
-- requiring table ownership, not something the app or a routine admin does.

-- +goose StatementBegin
CREATE FUNCTION nap_reject_locked_season_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_year integer;
BEGIN
    -- Invisible parent => the season itself is being deleted => allow.
    SELECT year INTO v_year
      FROM season
     WHERE id = OLD.season_id AND state = 'locked';

    IF FOUND THEN
        RAISE EXCEPTION
            'cannot delete from % in locked season %', TG_TABLE_NAME, v_year
            USING ERRCODE = 'NAPLK',
                  DETAIL  = format(
                      'Season %s is locked; its ballots, roster, slate and '
                      'result are the published archive.', v_year),
                  HINT    = CASE TG_TABLE_NAME
                      WHEN 'movie' THEN
                          'Set hidden = true instead of deleting.'
                      WHEN 'season_member' THEN
                          'Remove them from the current season instead; past '
                          'membership is part of the record.'
                      WHEN 'result' THEN
                          'A locked season is not re-tallied.'
                      ELSE
                          'Delete the whole season if you really mean to '
                          'discard the year.'
                  END;
    END IF;

    RETURN OLD;
END $$;
-- +goose StatementEnd

COMMENT ON FUNCTION nap_reject_locked_season_delete() IS
    'Rejects piecemeal deletes out of a locked season. Raises SQLSTATE NAPLK. '
    'Allows the cascade from DELETE FROM season, where the parent row is '
    'already gone and therefore invisible to this trigger.';

-- ballot_entry is the one that matters: it is what the person and
-- season_member cascades destroy.
CREATE TRIGGER ballot_entry_locked_season_guard
    BEFORE DELETE ON ballot_entry
    FOR EACH ROW EXECUTE FUNCTION nap_reject_locked_season_delete();

-- season_member gets its own so that removing a member of a past year fails
-- with the reason, rather than with a confusing error from a table the admin
-- did not name -- and so that it fails even for a member who cast no ballot,
-- whose presence on the roster is itself part of the archive.
CREATE TRIGGER season_member_locked_season_guard
    BEFORE DELETE ON season_member
    FOR EACH ROW EXECUTE FUNCTION nap_reject_locked_season_delete();

-- movie closes the gap the FK above cannot: a movie nobody ranked has nothing
-- referencing it, so a locked year's slate could be edited after the fact. It
-- also turns the ranked case from an FK message about ballot_entry into one
-- that names the real problem.
CREATE TRIGGER movie_locked_season_guard
    BEFORE DELETE ON movie
    FOR EACH ROW EXECUTE FUNCTION nap_reject_locked_season_delete();

-- result is the frozen snapshot itself -- the last thing standing if the
-- ballots ever do go. Deleting it is what actually erases a year.
CREATE TRIGGER result_locked_season_guard
    BEFORE DELETE ON result
    FOR EACH ROW EXECUTE FUNCTION nap_reject_locked_season_delete();

-- +goose Down
DROP TRIGGER result_locked_season_guard        ON result;
DROP TRIGGER movie_locked_season_guard         ON movie;
DROP TRIGGER season_member_locked_season_guard ON season_member;
DROP TRIGGER ballot_entry_locked_season_guard  ON ballot_entry;
DROP FUNCTION nap_reject_locked_season_delete();

ALTER TABLE result DROP CONSTRAINT result_winner_movie_fkey;
ALTER TABLE result ADD CONSTRAINT result_winner_movie_fkey
    FOREIGN KEY (season_id, winner_movie_id) REFERENCES movie (season_id, id)
    ON DELETE NO ACTION;

ALTER TABLE ballot_entry DROP CONSTRAINT ballot_entry_movie_fkey;
ALTER TABLE ballot_entry ADD CONSTRAINT ballot_entry_movie_fkey
    FOREIGN KEY (season_id, movie_id) REFERENCES movie (season_id, id)
    ON DELETE NO ACTION;
