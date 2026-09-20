package templates_test

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The TMDB picker (nap-eie) rendered as markup.
//
// These follow submit_test.go's pattern and look inside <main>: the layout now
// carries a <script> tag for htmx on every page, so "this page has no search on
// it" is a claim about the page's own content and not about the document.

// searchState is a picker with results, wired the way board.search wires one.
func searchState(query string, results ...viewmodel.SearchResult) viewmodel.SearchState {
	return viewmodel.SearchState{
		Available:    true,
		Query:        query,
		Results:      results,
		Action:       "/submit",
		FragmentHref: "/submit/search",
		ResultsID:    "tmdb-results",
	}
}

func aResult() viewmodel.SearchResult {
	return viewmodel.SearchResult{
		TMDBID:    "78",
		Title:     "Blade Runner",
		Label:     "Blade Runner (1982)",
		Overview:  "A blade runner is called out of retirement.",
		PosterURL: "https://image.tmdb.org/t/p/w154/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
		PickHref:  "/submit?tmdb_id=78",
	}
}

// A page whose Search is the zero value must render no picker at all.
//
// This is the guard on the seam with nap-7hn: internal/web/board/edit.go
// reuses viewmodel.SubmitPage and never sets Search or Picked, so the edit
// form has to come out exactly as it did before this ticket. The same zero
// value is what a stack with no TMDB key renders.
func TestSubmitPageWithoutSearchHasNoPicker(t *testing.T) {
	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmit())))

	for _, marker := range []string{
		`id="submit-search"`,
		`id="tmdb-results"`,
		`hx-get`,
		`name="tmdb_id"`,
		`class="picked`,
		"Not in there?",
	} {
		if strings.Contains(main, marker) {
			t.Errorf("a page with no TMDB search rendered %q; the edit form and the no-key "+
				"submit form must come out exactly as they did before nap-eie", marker)
		}
	}

	// And the four boxes are still there and still typeable.
	if !strings.Contains(main, `id="submit-title"`) {
		t.Error("the manual form is missing")
	}
	if strings.Contains(main, "readonly") {
		t.Error("a field is readonly on a page where nothing was picked from TMDB")
	}
}

// The search form is a real GET form and every result a real link, which is
// what makes the picker work with JavaScript switched off. htmx sits on top of
// exactly that markup and changes only when the request happens.
func TestSearchWorksWithoutJavaScript(t *testing.T) {
	page := viewmodel.FixtureSubmit()
	page.Search = searchState("blade run", aResult())

	main := mainContent(t, render(t, templates.SubmitPage(page)))

	for _, want := range []string{
		`method="get"`,              // a real form submission
		`action="/submit"`,          // back to this same page, rendered server-side
		`type="submit"`,             // a visible way to say "go"
		`href="/submit?tmdb_id=78"`, // picking is an ordinary link
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the picker is missing %q, so it would not work without htmx:\n%s", want, main)
		}
	}
}

// Posters are hotlinked to TMDB's CDN, which is a deliberate decision rather
// than an oversight (see the note at the top of search.templ). The referrer
// policy is the part that is easy to lose in an edit and silently leaks this
// site's URLs to TMDB on every thumbnail.
func TestResultPostersAreReferrerStripped(t *testing.T) {
	page := viewmodel.FixtureSubmit()
	page.Search = searchState("blade run", aResult())

	main := mainContent(t, render(t, templates.SubmitPage(page)))

	if !strings.Contains(main, `referrerpolicy="no-referrer"`) {
		t.Error("a hotlinked poster has no referrerpolicy; this site's URLs leak to TMDB's CDN")
	}
	if !strings.Contains(main, "https://image.tmdb.org/") {
		t.Error("the poster did not render")
	}
}

// A film with no poster still has to be pickable, and must not render an <img>
// pointed at the bare CDN root.
func TestResultWithNoPosterStillRenders(t *testing.T) {
	noPoster := aResult()
	noPoster.PosterURL = ""

	page := viewmodel.FixtureSubmit()
	page.Search = searchState("blade run", noPoster)

	main := mainContent(t, render(t, templates.SubmitPage(page)))

	if !strings.Contains(main, `href="/submit?tmdb_id=78"`) {
		t.Error("a film with no poster lost its pick link")
	}
	if strings.Contains(main, `src=""`) || strings.Contains(main, `src="https://image.tmdb.org/t/p/w154"`) {
		t.Error("an empty poster URL was rendered as an <img> src")
	}
}

// The three "nothing to show" states must read differently. Telling somebody
// "nothing matched that" when the truth is "we could not ask" sends them off to
// retype a title that was right the first time.
func TestSearchEmptyStatesReadDifferently(t *testing.T) {
	layout := viewmodel.FixtureLayout()

	untouched := render(t, templates.SearchResults(layout, searchState("")))
	if strings.TrimSpace(untouched) != "" {
		t.Errorf("an untouched search box rendered something:\n%s", untouched)
	}

	empty := render(t, templates.SearchResults(layout, searchState("zzzz")))
	if !strings.Contains(empty, "Nothing came back") {
		t.Errorf("a search that matched nothing did not say so:\n%s", empty)
	}

	failed := searchState("blade run")
	failed.Failed = true
	down := render(t, templates.SearchResults(layout, failed))
	if strings.Contains(down, "Nothing came back") {
		t.Error("a failed request was reported as an empty result set")
	}
	if !strings.Contains(down, "did not answer") {
		t.Errorf("a failed request did not say the search never happened:\n%s", down)
	}
}

// The query is attacker-chosen text echoed back into the page.
func TestSearchResultsEscapeTheQuery(t *testing.T) {
	layout := viewmodel.FixtureLayout()

	out := render(t, templates.SearchResults(layout, searchState(`<script>alert(1)</script>`)))

	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("the query was echoed unescaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("the query was not echoed at all, so this proves nothing:\n%s", out)
	}
}

// A picked film shows what is about to go on the board, and carries the way
// back out. The escape hatch is not decoration: the films this group submits
// are regularly ones TMDB has never heard of.
func TestPickedFilmOffersTheWayBack(t *testing.T) {
	page := viewmodel.FixtureSubmit()
	page.Search = searchState("")
	page.Picked = viewmodel.Picked{
		TMDBID:    "78",
		Label:     "Blade Runner (1982)",
		PosterURL: "https://image.tmdb.org/t/p/w154/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
		ClearHref: "/submit",
	}
	page.Form.TMDBID = "78"
	page.Form.Title = "Blade Runner"

	main := mainContent(t, render(t, templates.SubmitPage(page)))

	if !strings.Contains(main, "Blade Runner (1982)") {
		t.Error("the picked film is not named on the page")
	}
	if !strings.Contains(main, "Not this one") {
		t.Error("there is no way out of the picker")
	}
	// The id rides through in a hidden field, and it is the only thing the
	// server takes back from the browser.
	if !strings.Contains(main, `name="tmdb_id"`) {
		t.Error("the chosen film's id does not survive the form")
	}
	// The fields TMDB owns are readonly, because the server overwrites them
	// on POST and a box that discards what you type is worse than no box.
	if !strings.Contains(main, "readonly") {
		t.Error("TMDB-filled fields are editable, but the POST overwrites them")
	}
}
