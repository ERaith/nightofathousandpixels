// Package integrity holds the database-level integrity tests for the schema:
// the properties that live in constraints and triggers rather than in Go.
//
// They are in their own package because they are about the migrations, not
// about the generated query set, and because they need to run statements the
// query set deliberately does not expose -- a bare DELETE FROM movie, a
// DELETE FROM season, a constraint rebuild in the order a restore produces.
package integrity
