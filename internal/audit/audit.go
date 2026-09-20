// Package audit records the things somebody did that outlive the rows they
// did them to.
//
// It is one thin function over one query, and it exists because the callers
// are in two different packages and the mistakes are the same in both: an
// action name invented at the call site so the log cannot be filtered, a
// target that is a uuid with no idea what kind of thing it names, and a
// payload that is a fmt.Sprintf nobody can query.
//
// Three rules hold here, and all three come from migration 00007:
//
//   - The actor is text and never a foreign key. An audit row has to keep
//     naming a person after their row is gone, and has to be able to name
//     things that are not people at all ('system', 'cron').
//   - The action names are constants below. A log that cannot be filtered by
//     action is a log nobody reads.
//   - An audit row goes in the SAME transaction as the write it describes, so
//     a failure here rolls that write back. The alternative -- log-and-carry-on
//     -- sounds kinder and is worse: it produces exactly the silent gap the
//     log exists to close, and the gap appears on the runs where something was
//     already going wrong. Writer takes the caller's transaction for this
//     reason, not for convenience.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// The actions this application records. They are dotted and past tense --
// "the thing that happened", not "the handler that ran" -- so that a filter on
// 'season.' or on 'movie.' is a meaningful slice of the log.
const (
	// ActionSeasonCreated is an admin opening a year (ticket D3).
	ActionSeasonCreated = "season.created"

	// ActionSeasonUpdated is an admin re-dating one. It is the event that
	// answers "why did the deadline move", which is the question actually
	// asked in October.
	ActionSeasonUpdated = "season.updated"

	// ActionMovieEdited is a submitter changing their own film (ticket E3).
	ActionMovieEdited = "movie.edited"

	// ActionMovieWithdrawn is a submitter taking one off the board. It is a
	// soft delete -- movie.hidden -- so the row it names is still there to
	// look at, which is the one case where the log and the data agree.
	ActionMovieWithdrawn = "movie.withdrawn"
)

// SystemActor is the actor for something no person did.
const SystemActor = "system"

// Writer is the one query this package needs. It is an interface so that a
// caller inside a transaction passes its own store.Queries built on the tx,
// which is the normal case: an audit row belongs in the transaction it
// describes, or it records something that did not happen.
type Writer interface {
	CreateAuditLog(ctx context.Context, arg store.CreateAuditLogParams) (store.AuditLog, error)
}

// Record writes one audit row.
//
// actor is normally the person's email address: it is what an admin reading
// the log recognises, and unlike a uuid it stays meaningful once the person
// row is gone. target names the row this was done to, as a plain string.
//
// payload is marshalled to jsonb. A nil payload is the empty object, which the
// audit_log_payload_is_object CHECK requires -- 'null' is valid jsonb and is
// not an object, so a caller passing an untyped nil must not reach the column
// as literal null.
func Record(ctx context.Context, w Writer, actor, action, target string, payload any) error {
	encoded := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("audit: marshal %s payload: %w", action, err)
		}
		encoded = b
	}

	params := store.CreateAuditLogParams{
		Actor:   actor,
		Action:  action,
		Payload: encoded,
	}
	// target is nullable, and a blank string is not the same as "no target":
	// the column has no not-blank CHECK, so an empty string would be stored
	// and would then match a filter looking for rows with a target.
	if target != "" {
		params.Target = &target
	}

	if _, err := w.CreateAuditLog(ctx, params); err != nil {
		return fmt.Errorf("audit: record %s: %w", action, err)
	}

	return nil
}
