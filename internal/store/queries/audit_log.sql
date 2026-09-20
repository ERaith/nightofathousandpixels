-- Append-only. Nothing here reads the log yet: the admin feed is its own
-- ticket. What exists is the write, because an action that changes a season or
-- takes a film off the board has to leave a trace at the moment it happens --
-- a logger writes to stdout and stdout is rotated away, and migration 00007
-- exists precisely so these events outlive the rows they describe.

-- name: CreateAuditLog :one
-- actor is text and deliberately not a foreign key (see migration 00007): it
-- records the email of whoever did it, or 'system' when nobody did. target is
-- a free string rather than a uuid column for the same reason -- it has to
-- keep naming a row that may later be gone.
--
-- payload is jsonb and arrives as raw bytes, so a caller marshals its own
-- struct. That keeps the shape of a payload next to the action that writes it
-- instead of in a union type here that every new action widens.
INSERT INTO audit_log (actor, action, target, payload)
VALUES (
    sqlc.arg(actor),
    sqlc.arg(action),
    sqlc.narg(target),
    sqlc.arg(payload)
)
RETURNING *;
