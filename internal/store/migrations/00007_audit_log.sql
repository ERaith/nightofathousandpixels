-- +goose Up
-- Append-only. bigint identity rather than uuid because this is a log and
-- insertion order is the thing we read it by.
CREATE TABLE audit_log (
    id      bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Deliberately text and deliberately not a foreign key. An audit entry has
    -- to outlive whatever it describes -- including the person who did it and
    -- the row they touched -- and it also has to record non-person actors
    -- ('system', 'cron', 'migration').
    actor   text        NOT NULL,
    action  text        NOT NULL,
    target  text,
    payload jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT audit_log_actor_not_blank  CHECK (actor <> ''),
    CONSTRAINT audit_log_action_not_blank CHECK (action <> ''),
    CONSTRAINT audit_log_payload_is_object CHECK (jsonb_typeof(payload) = 'object')
);

-- The admin view is a reverse-chronological feed, optionally filtered.
CREATE INDEX audit_log_at_idx        ON audit_log (at DESC);
CREATE INDEX audit_log_action_at_idx ON audit_log (action, at DESC);
CREATE INDEX audit_log_target_at_idx ON audit_log (target, at DESC) WHERE target IS NOT NULL;

-- +goose Down
DROP TABLE audit_log;
