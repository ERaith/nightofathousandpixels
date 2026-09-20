package signin

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// ReadAccount is RequireMember's non-gating twin: it answers "who is reading
// this" for a page that is going to render either way (ticket nap-bus).
//
// The two are deliberately the same lookup and deliberately not the same
// function. RequireMember decides; this one only describes. Every difference
// between them follows from that:
//
//   - It never redirects. A visitor with no session falls through to the
//     signed-out header, which is the whole reason it exists.
//   - It never clears a cookie. A cookie that is expired, tampered with or
//     sealed under a rotated secret means "not signed in" on a page anybody
//     may read; clearing it is RequireMember's job, on the next request that
//     actually needs an identity. A front page that deleted cookies would be a
//     surprising thing for a front page to do.
//   - It never fails. A database that is down means the signed-out header, not
//     an error page — and the 500 page is itself one of the pages this fills
//     in, so a version of this that could fail would fail hardest exactly when
//     the site was already broken.
//
// Cost: it is up to three queries, and it is mounted on pages that previously
// made none. The first line is what keeps that honest — with no session cookie
// it returns before touching the database at all, so an anonymous reader, a
// link preview scraper and a scanner walking 404s cost nothing. Only a request
// that actually carries a session pays.
func (s *Service) ReadAccount(r *http.Request) *viewmodel.Account {
	session, err := s.opts.Sessions.Read(r)
	if err != nil {
		return nil
	}

	person, err := s.opts.Store.GetPerson(r.Context(), session.PersonID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			// A cookie naming a person who is gone is unremarkable and silent.
			// Anything else is the database, and it is worth a line, because
			// the visible symptom -- the header quietly reverting to "Sign in"
			// for everybody -- looks like an auth bug rather than an outage.
			s.opts.Logger.Warn("optional auth: could not load person",
				slog.String("person_id", session.PersonID.String()),
				slog.Any("error", err),
			)
		}

		return nil
	}

	account := &viewmodel.Account{
		User: &viewmodel.CurrentUser{
			DisplayName: person.DisplayName,
			Email:       person.Email,
		},
	}

	// From here on every failure returns what has already been established
	// rather than giving up on it. Somebody whose membership could not be read
	// is still signed in, and a header that greets them by name and leaves the
	// member-only links off is a smaller wrong answer than one that tells them
	// to sign in again.
	season, err := s.opts.Store.GetCurrentSeason(r.Context())
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.opts.Logger.Warn("optional auth: could not load current season", slog.Any("error", err))
		}

		return account
	}

	member, err := s.opts.Store.GetSeasonMember(r.Context(), store.GetSeasonMemberParams{
		SeasonID: season.ID,
		PersonID: person.ID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.opts.Logger.Warn("optional auth: could not load membership",
				slog.String("person_id", person.ID.String()),
				slog.Any("error", err),
			)
		}

		return account
	}

	account.IsMember = true
	account.User.IsAdmin = member.IsAdmin

	return account
}
