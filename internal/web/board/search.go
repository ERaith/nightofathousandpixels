package board

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ERaith/nightofathousandpixels/internal/tmdb"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// SearchPath is the htmx fragment endpoint: the result list on its own, with
// no page around it.
//
// It sits behind the same whitelist gate as the form it belongs to, and that
// is not incidental. It spends our TMDB quota on every request, so an open one
// would be a rate limit anybody on the internet could exhaust for us, and the
// page it serves a fragment of is not public either.
const SearchPath = SubmitPath + "/search"

// searchResultsID is the DOM id of the element htmx swaps the fragment into.
//
// It is a constant shared by the container and the input's hx-target rather
// than a literal in each, because the two agreeing is the whole mechanism and
// a typo in either is a search box that does nothing at all, silently.
const searchResultsID = "tmdb-results"

// maxQueryLen bounds the search box.
//
// No film has a title this long, the value goes straight into an outbound URL,
// and this is a member-authenticated endpoint that costs us an API call. It is
// clamped rather than rejected: somebody who pasted a paragraph gets the
// search they probably meant instead of an error about a box.
const maxQueryLen = 120

// handleSearch is the htmx fragment.
//
// It answers with a list, a "nothing matched" line or a "TMDB did not answer"
// line, and with 200 in all three cases. That is deliberate: htmx does not
// swap a non-2xx response by default, so returning 502 when TMDB is down would
// leave the previous results on screen under a search box that had visibly
// stopped responding. The status of THIS request is fine — the server was
// asked for a fragment and produced the correct fragment, which happens to say
// that the upstream is unavailable.
func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) {
	state := s.search(r.Context(), r.URL.Query().Get(viewmodel.FieldQuery))

	// A fragment, not a page: no layout, no <html>. The page shell is already
	// in the browser.
	s.render(w, r, http.StatusOK, templates.SearchResults(s.page(r, ""), state))
}

// search runs one query and turns it into the state a template renders.
//
// Every failure becomes Failed rather than an error returned upward, because
// there is exactly one thing to do about any of them and it is not a 500: show
// the manual form and say the search did not happen. A TMDB outage must not be
// able to take the submit page down with it -- the four boxes worked before
// this ticket and they still work.
func (s *Service) search(ctx context.Context, rawQuery string) viewmodel.SearchState {
	state := viewmodel.SearchState{
		Available:    s.opts.TMDB.Enabled(),
		Query:        clampQuery(rawQuery),
		Action:       SubmitPath,
		FragmentHref: SearchPath,
		ResultsID:    searchResultsID,
	}

	if !state.Available || state.Query == "" {
		return state
	}

	found, err := s.opts.TMDB.Search(ctx, state.Query)
	if err != nil {
		// The query is logged and the error is not wrapped with it: see
		// tmdb.Client.get on why nothing from that package may carry a URL.
		s.opts.Logger.Warn("tmdb search failed",
			slog.String("query", state.Query),
			slog.Any("error", err),
		)
		state.Failed = true

		return state
	}

	state.Results = make([]viewmodel.SearchResult, 0, len(found))
	for _, m := range found {
		state.Results = append(state.Results, searchResult(m))
	}

	return state
}

// searchResult turns one TMDB match into a row in the picker.
func searchResult(m tmdb.Movie) viewmodel.SearchResult {
	id := viewmodel.TMDBIDString(m.ID)

	return viewmodel.SearchResult{
		TMDBID:    id,
		Title:     m.Title,
		Label:     m.Label(),
		Overview:  m.Overview,
		PosterURL: m.PosterURL(),
		PickHref:  pickHref(id),
	}
}

// pickHref is where a result's link goes: this same page, with the id on it.
//
// Choosing a film is therefore an ordinary GET to an ordinary URL. Nothing
// about it needs JavaScript, it can be bookmarked and shared, and the server
// does the lookup -- which is the same lookup it will do again on POST. A
// design where the click filled the boxes in the browser would have put the
// facts in the client's hands, and they would have had to be distrusted right
// back out of them a moment later.
func pickHref(tmdbID string) string {
	if tmdbID == "" {
		return SubmitPath
	}

	return SubmitPath + "?" + viewmodel.FieldTMDBID + "=" + url.QueryEscape(tmdbID)
}

// clampQuery normalises what came out of the search box.
//
// Truncation is by rune rather than by byte so that cutting a pasted title
// short cannot split a multi-byte character and send half of one to TMDB.
func clampQuery(raw string) string {
	q := strings.Join(strings.Fields(raw), " ")
	if runes := []rune(q); len(runes) > maxQueryLen {
		q = strings.TrimSpace(string(runes[:maxQueryLen]))
	}

	return q
}

// parseTMDBID reads a tmdb_id off a query string or a form.
//
// Anything that is not a plain positive integer is 0, meaning "no film
// picked", and 0 is handled everywhere as the manual path. There is
// deliberately no error: this value is never typed by a person, so there is no
// field to put a message beside, and the honest response to a malformed one is
// the empty form rather than a complaint about a parameter nobody chose.
func parseTMDBID(raw string) int {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return 0
	}

	return id
}

// lookupPicked resolves a chosen tmdb_id into a prefilled form.
//
// This is the function the whole "never trust the returned fields" rule rests
// on, and it is called from BOTH the GET that fills the form in and the POST
// that writes the row. The browser is handed a filled-in form by the first
// call and its answer is discarded and re-derived by the second, so what
// actually reaches the database is always TMDB's answer to an id, never the
// page's.
//
// Its three outcomes are all normal:
//
//	found      the form, filled in, and a Picked to show above it.
//	not found  a zero form: the id names no film, so there is nothing to
//	           prefill and the person gets the manual boxes.
//	failed     the same, plus a logged warning. TMDB being down must not stop
//	           somebody submitting.
//
// The distinction between the last two is only in the log, and that is on
// purpose: from the page's side both mean "this id gave us nothing", and the
// recovery -- type it in yourself -- is identical.
func (s *Service) lookupPicked(ctx context.Context, id int) (viewmodel.SubmitForm, viewmodel.Picked, *tmdb.Details) {
	var (
		form   viewmodel.SubmitForm
		picked viewmodel.Picked
	)

	if id <= 0 || !s.opts.TMDB.Enabled() {
		return form, picked, nil
	}

	details, err := s.opts.TMDB.Lookup(ctx, id)
	if err != nil {
		if !errors.Is(err, tmdb.ErrNotFound) {
			s.opts.Logger.Warn("tmdb lookup failed",
				slog.Int("tmdb_id", id),
				slog.Any("error", err),
			)
		}

		return form, picked, nil
	}

	idStr := viewmodel.TMDBIDString(details.ID)

	form = viewmodel.SubmitForm{
		TMDBID: idStr,
		Title:  details.Title,
		Year:   yearString(details.Year),
		// The trailer is a watch URL rather than an embed URL, and building
		// the embed is deliberately not done here: that is ticket nap-0z8's,
		// and it has to handle hand-pasted watch URLs anyway. What this side
		// guarantees is that the key inside it is eleven validated characters
		// -- see tmdb.trailerKeyShape.
		TrailerURL: trailerWatchURL(details.TrailerKey),
		// TMDB's synopsis, as a starting point. It is the one field that stays
		// editable and the one field the POST does NOT re-fetch, because the
		// box is labelled "Why this one" -- somebody replacing the synopsis
		// with their own sentence is using it correctly, and a server that
		// overwrote that would be deleting the only part of the submission
		// that was theirs.
		Description: details.Overview,
	}

	picked = viewmodel.Picked{
		TMDBID:    idStr,
		Label:     details.Label(),
		PosterURL: details.PosterURL(),
		ClearHref: SubmitPath,
	}

	return form, picked, &details
}

// youTubeWatchPrefix is the canonical watch URL a TMDB trailer key becomes.
//
// It is the watch page rather than the embed URL on purpose. nap-0z8 owns
// turning a trailer link into something that plays in place, and it has to
// cope with watch URLs regardless -- people paste them by hand, and will keep
// doing so for every film TMDB has never heard of. Producing a second shape
// here would give that normalizer two inputs to handle instead of one, and the
// hand-typed branch would still be the one that had to work.
const youTubeWatchPrefix = "https://www.youtube.com/watch?v="

// trailerWatchURL builds the stored trailer link from a TMDB video key, or ""
// when there is no trailer.
//
// The key has already been checked against tmdb.trailerKeyShape, so no
// escaping is needed and none is done -- eleven characters of [A-Za-z0-9_-]
// are safe in a query string by construction. If that ever stops being true,
// this is the line that breaks, which is why the check lives at the edge where
// the value enters the system rather than here.
func trailerWatchURL(key string) string {
	if key == "" {
		return ""
	}

	return youTubeWatchPrefix + key
}

// yearString renders a release year for a form field, or "" when TMDB has
// none. Zero is the unknown sentinel and must not render as "0" -- that would
// fail validation as a year outside movie_year_range, on a field the person
// never touched.
func yearString(year int) string {
	if year <= 0 {
		return ""
	}

	return strconv.Itoa(year)
}
