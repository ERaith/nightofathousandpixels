package board

import "errors"

// ---------------------------------------------------------------------------
// DELETE THIS WHOLE FILE WHEN nap-eie (builder-14, 7d90270) MERGES.
// ---------------------------------------------------------------------------
//
// Every declaration below is a VERBATIM copy of one that commit adds to
// submit.go. Two copies in one package is a duplicate declaration and will not
// compile, which is deliberate: a loud failure at merge is the only thing that
// reliably gets a duplicate reconciled, and the alternative -- naming mine
// errEditAlreadyUp and shipping a second sentence for the same refusal -- would
// merge silently and leave the site saying two different things about one
// situation.
//
// The resolution is therefore: delete this file, keep submit.go's. Nothing
// about behaviour changes when you do, because the strings here are byte
// identical to theirs. That was the point of copying rather than writing.
//
// Why this file exists at all, given that it is born to be deleted: the edit
// path in edit.go needs the same catch, and nap-eie has not reached
// beads-setup yet. Waiting would mean the catch lands after the thing that
// makes it necessary -- see updateSubmission for why it is currently
// unreachable and why that is not a reason to leave it out.

// errAlreadyUp is the film being on the board already, from
// movie_season_tmdb_unique_idx.
//
// It could not happen before ticket nap-eie: tmdb_id was written as NULL on
// every row, and the index is partial on tmdb_id IS NOT NULL, so it has never
// once fired. Now that real ids are stored, two people picking the same film
// in one season is an ordinary Tuesday -- it is precisely what the index
// exists to stop, because two rows for one film split the ranked-choice vote
// between them.
//
// It is caught rather than prevented by a lookup first, and that is the point:
// a SELECT before the INSERT would still leave a window for two simultaneous
// submissions, and the database has the only answer that cannot be raced.
var errAlreadyUp = errors.New("board: that film is already on the board")

// uniqueViolation is Postgres's SQLSTATE for a unique index violation.
const uniqueViolation = "23505"

// alreadyUpMessage is what somebody is told when their film is already on the
// board.
//
// It is a plain message rather than a copy key because it is a fact about the
// season rather than the site's voice, and it is against the title field
// because the title is what identifies the film to the person reading. It
// deliberately does not name who submitted it: the slate is one click away and
// says so itself, and "Dave already put this up" invites a conversation the
// site should not be starting.
const alreadyUpMessage = "That one is already on the board this year — two copies would split the vote. " +
	"Pick something else, or check the slate to see whose it is."
