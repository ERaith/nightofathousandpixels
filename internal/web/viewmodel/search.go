package viewmodel

import "strconv"

// SearchResult is one film in the picker on the submit page.
//
// It is the view-model twin of tmdb.Movie, and it is a separate type for the
// same reason no store row reaches a template: internal/web/templates must not
// import a client for a third-party API to render a list. Everything a
// template needs is already resolved here -- the poster is an absolute URL or
// an empty string, the year is folded into the label, and the id is a string
// because a template only ever prints it.
type SearchResult struct {
	// TMDBID is the film's TMDB id, as a string for the href. It is the only
	// field of this struct that survives the round trip through the browser:
	// everything else on this page is looked up again, server-side, from this
	// id. See board.handleSubmitPost.
	TMDBID string

	// Title is the film's title on its own, for the accessible name of the
	// control that picks it.
	Title string

	// Label is "Blade Runner (1982)", or just the title when TMDB has no
	// release date.
	Label string

	// Overview is TMDB's synopsis, often empty. The picker shows the first
	// line or two of it as the thing that tells two remakes apart.
	Overview string

	// PosterURL is an absolute URL on TMDB's image CDN, or "" when the film
	// has no poster.
	//
	// It is a remote URL rather than one of ours, and that is a deliberate
	// decision rather than an omission -- see the note on hotlinking in
	// internal/web/templates/search.templ.
	PosterURL string

	// PickHref is where the control that chooses this film goes: the submit
	// page again, with this id on it.
	PickHref string
}

// HasPoster reports whether there is an image to show. A picker row without
// one still renders -- it gets a placeholder tile, so the rows stay aligned
// and a film with no poster is not mistaken for a broken page.
func (r SearchResult) HasPoster() bool {
	return r.PosterURL != ""
}

// HasOverview reports whether there is a synopsis to show under the title.
func (r SearchResult) HasOverview() bool {
	return r.Overview != ""
}

// SearchState is everything the search half of the submit page renders, and it
// is one struct rather than a list plus three booleans because the states are
// mutually exclusive and a page that got two of them at once would show a
// spinner over an apology.
//
// The five states, in the order the fragment tests them:
//
//	!Available          no API key: no search box at all, just the four boxes.
//	Failed              TMDB did not answer. The manual form is still there.
//	Query == ""         nothing typed yet: no list, no "no results".
//	len(Results) == 0   typed something, found nothing.
//	len(Results) > 0    the picker.
//
// The distinction between the last three is the one that matters. An empty
// list and an empty query look identical in a slice, and rendering "no films
// match" at somebody who has not typed anything yet is how a search box tells
// people it is broken.
type SearchState struct {
	// Available is whether search is configured at all. False is the state
	// every dev stack without a TMDB key is in, and it is not an error.
	Available bool

	// Query is the text that was searched for, which goes back in the box.
	Query string

	// Results are the matches, best first, already capped by the client.
	Results []SearchResult

	// Failed is true when TMDB was asked and did not answer. The page says so
	// and points at the manual form; it never shows an empty list, because
	// "no films match that" is a different and much more misleading thing to
	// tell somebody whose search simply did not happen.
	Failed bool

	// Action is where the search form posts -- the submit page itself, so
	// that pressing enter with no JavaScript runs the same search
	// server-side. Blank renders no search form.
	Action string

	// FragmentHref is the endpoint htmx asks for the result list as somebody
	// types. Blank leaves the form a plain one, which still works.
	FragmentHref string

	// ResultsID is the DOM id of the element the fragment replaces. It is
	// here rather than a literal in the template because the form's hx-target
	// and the container's id have to agree, and a typo in either is a search
	// box that silently does nothing.
	ResultsID string
}

// Searched reports whether there was a query to answer at all.
func (s SearchState) Searched() bool {
	return s.Query != ""
}

// FoundNothing reports the state that needs saying out loud: a real search
// that matched nothing. It is deliberately false for an untouched box and for
// a failed request, which are the two things it would otherwise be confused
// with.
func (s SearchState) FoundNothing() bool {
	return s.Available && !s.Failed && s.Searched() && len(s.Results) == 0
}

// HasResults reports whether there is a picker to draw.
func (s SearchState) HasResults() bool {
	return len(s.Results) > 0
}

// Picked is the film a person has chosen, shown above the form so that what is
// about to go on the board is visible before the button is pressed.
//
// It exists because the alternative -- four boxes that silently filled
// themselves in -- is indistinguishable from a bug the first time you see it.
// The zero value means nothing has been picked and the form is a manual one.
type Picked struct {
	// TMDBID is what the form posts back, and what the server looks the film
	// up by again before it writes anything.
	TMDBID string

	// Label is "Blade Runner (1982)".
	Label string

	// PosterURL is the poster, or "".
	PosterURL string

	// ClearHref goes back to an empty submit form. It is the escape hatch, and
	// it is not optional: a search you cannot get out of is worse than the
	// four boxes, because the film this group wants to submit is regularly one
	// TMDB has never heard of.
	ClearHref string
}

// IsSet reports whether a film has been picked.
func (p Picked) IsSet() bool {
	return p.TMDBID != ""
}

// HasPoster reports whether there is an image to show beside the label.
func (p Picked) HasPoster() bool {
	return p.PosterURL != ""
}

// FieldTMDBID is the hidden field carrying the chosen film's id through the
// form.
//
// It is the one value the server takes back from the browser, and it is taken
// back as an identifier and nothing else: the title, the year and the trailer
// are looked up again from it server-side. A tampered id therefore cannot
// forge a submission -- at worst it submits a different real film, which is
// something the person could have done by searching for it.
const FieldTMDBID = "tmdb_id"

// FieldQuery is the search box's own field name. It is not part of the
// submission -- it never reaches a movie row -- but the handler and the
// template have to agree on it for the same reason every other field name here
// is a constant.
const FieldQuery = "q"

// TMDBIDString renders a TMDB id for a form value or an href, or "" for a
// non-positive one. TMDB ids are positive, so zero is the "no film picked"
// sentinel and must not render as "0".
func TMDBIDString(id int) string {
	if id <= 0 {
		return ""
	}

	return strconv.Itoa(id)
}
