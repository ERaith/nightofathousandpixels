// Package store is the database layer. It is a thin skin over the sqlc output
// in internal/store/sqlc: a constructor, and type aliases so that the rest of
// the application imports one package instead of reaching into generated code.
//
// There is deliberately no business logic here. Rules about who may submit,
// when a season opens, and how a ballot is tallied live in the packages that
// own those rules -- this package only knows how to talk to Postgres.
//
// Regenerate the sqlc output with: go tool sqlc generate
package store

import (
	sqlc "github.com/ERaith/nightofathousandpixels/internal/store/sqlc"
)

// New returns the query set bound to db.
//
// db is anything that can run a query: a *pgxpool.Pool in the server, or a
// pgx.Tx in a test that wraps each scenario in BEGIN/ROLLBACK. That is the
// whole reason the parameter is DBTX and not a concrete pool type.
func New(db DBTX) *Queries { return sqlc.New(db) }

// The plumbing. DBTX is the "can run a query" interface; Querier is the full
// set of queries, which is what a test double implements.
type (
	DBTX    = sqlc.DBTX
	Querier = sqlc.Querier
	Queries = sqlc.Queries
)

// Row types, one per table.
type (
	AuditLog     = sqlc.AuditLog
	BallotEntry  = sqlc.BallotEntry
	Movie        = sqlc.Movie
	Person       = sqlc.Person
	Result       = sqlc.Result
	Season       = sqlc.Season
	SeasonMember = sqlc.SeasonMember
)

// Query argument and result types. These are aliases rather than wrappers, so
// a caller can pass store.CreateMovieParams straight to Queries.CreateMovie.
// The list is mechanical: it tracks whatever sqlc emits, and a query that is
// renamed or removed fails this file's compile rather than rotting silently.
type (
	CountPersonMoviesInSeasonParams = sqlc.CountPersonMoviesInSeasonParams
	CreateAuditLogParams            = sqlc.CreateAuditLogParams
	CreateMovieParams               = sqlc.CreateMovieParams
	CreateSeasonParams              = sqlc.CreateSeasonParams
	DeleteSeasonMemberParams        = sqlc.DeleteSeasonMemberParams
	GetEffectiveSubmitLimitParams   = sqlc.GetEffectiveSubmitLimitParams
	GetSeasonMemberParams           = sqlc.GetSeasonMemberParams
	ListPersonMoviesForSeasonParams = sqlc.ListPersonMoviesForSeasonParams
	ListSeasonMembersRow            = sqlc.ListSeasonMembersRow
	LockSubmitLimitForUpdateParams  = sqlc.LockSubmitLimitForUpdateParams

	// ListVisibleMoviesWithSubmitterForSeasonRow is a slate row: the movie
	// plus the display name to credit it to.
	ListVisibleMoviesWithSubmitterForSeasonRow = sqlc.ListVisibleMoviesWithSubmitterForSeasonRow

	SetMovieHiddenParams       = sqlc.SetMovieHiddenParams
	UpdateMovieParams          = sqlc.UpdateMovieParams
	UpdatePersonIdentityParams = sqlc.UpdatePersonIdentityParams
	UpdateSeasonDetailsParams  = sqlc.UpdateSeasonDetailsParams
	UpdateSeasonStateParams    = sqlc.UpdateSeasonStateParams
	UpdateSeasonWindowsParams  = sqlc.UpdateSeasonWindowsParams
	UpsertPersonOnSignInParams = sqlc.UpsertPersonOnSignInParams
	UpsertSeasonMemberParams   = sqlc.UpsertSeasonMemberParams
)
