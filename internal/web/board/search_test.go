package board

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/tmdb"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These are in-package tests, like validate_test.go and for the same reason:
// what is being checked is the handler's own decisions -- what it trusts, what
// it re-derives -- and those are unexported. They touch no database. The one
// thing faked is TMDB, which is a third-party HTTP service; see the note at
// the top of internal/tmdb/tmdb_test.go.

const stubSearchBody = `{"results":[
  {"id":78,"title":"Blade Runner","release_date":"1982-06-25",
   "overview":"A blade runner is called out of retirement.",
   "poster_path":"/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg"},
  {"id":335984,"title":"Blade Runner 2049","release_date":"2017-10-04",
   "overview":"Thirty years later.","poster_path":null}
]}`

const stubDetailBody = `{"id":78,"title":"Blade Runner","release_date":"1982-06-25",
  "overview":"A blade runner is called out of retirement.",
  "poster_path":"/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
  "videos":{"results":[
    {"key":"eogpIG53Cis","site":"YouTube","type":"Trailer","iso_639_1":"en","official":true}
  ]}}`

// serviceWithTMDB returns a Service whose only working part is the TMDB
// client. Nothing here reaches the store or the database.
func serviceWithTMDB(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Service{opts: Options{
		Logger: slog.New(slog.DiscardHandler),
		TMDB:   tmdb.New(tmdb.Options{APIKey: "test-key", BaseURL: srv.URL}),
	}}
}

// stubTMDB answers /search/movie and /movie/{id} with the recorded payloads.
func stubTMDB(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if strings.HasPrefix(r.URL.Path, "/search/") {
		_, _ = w.Write([]byte(stubSearchBody))

		return
	}
	_, _ = w.Write([]byte(stubDetailBody))
}

func TestSearchBuildsPickableResults(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	state := s.search(context.Background(), "blade run")

	if !state.Available {
		t.Fatal("Available is false with a key configured")
	}
	if state.Failed {
		t.Fatal("Failed is true on a successful search")
	}
	if !state.HasResults() {
		t.Fatal("no results")
	}
	if state.FoundNothing() {
		t.Error("FoundNothing is true when there are results")
	}

	first := state.Results[0]
	if first.TMDBID != "78" {
		t.Errorf("TMDBID = %q, want 78", first.TMDBID)
	}
	if first.Label != "Blade Runner (1982)" {
		t.Errorf("Label = %q", first.Label)
	}
	// The link that picks this film is an ordinary GET to an ordinary URL, so
	// the picker works with no JavaScript at all.
	if want := "/submit?tmdb_id=78"; first.PickHref != want {
		t.Errorf("PickHref = %q, want %q", first.PickHref, want)
	}
	if !strings.HasPrefix(first.PosterURL, "https://image.tmdb.org/") {
		t.Errorf("PosterURL = %q, want a TMDB CDN URL", first.PosterURL)
	}

	// The second result has no poster, and must still be pickable.
	if state.Results[1].HasPoster() {
		t.Error("a null poster_path produced a poster URL")
	}
	if state.Results[1].PickHref == "" {
		t.Error("a film with no poster got no pick link")
	}
}

// The three "no list" states are different things and must not be confused.
// Telling somebody "nothing matches that" when the truth is "we could not ask"
// sends them off to retype a title that was right.
func TestSearchKeepsTheEmptyStatesApart(t *testing.T) {
	t.Parallel()

	t.Run("nothing typed yet", func(t *testing.T) {
		t.Parallel()

		s := serviceWithTMDB(t, stubTMDB)
		state := s.search(context.Background(), "   ")

		if state.Searched() {
			t.Error("Searched is true for a blank query")
		}
		if state.FoundNothing() {
			t.Error(`an untouched box reports "nothing matched"`)
		}
		if state.Failed {
			t.Error("Failed is true for a query that was never sent")
		}
	})

	t.Run("searched and matched nothing", func(t *testing.T) {
		t.Parallel()

		s := serviceWithTMDB(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"results":[]}`))
		})
		state := s.search(context.Background(), "zzzzz")

		if !state.FoundNothing() {
			t.Error("a real search that matched nothing does not report it")
		}
		if state.Failed {
			t.Error("an empty result set was reported as a failure")
		}
	})

	t.Run("TMDB did not answer", func(t *testing.T) {
		t.Parallel()

		s := serviceWithTMDB(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		})
		state := s.search(context.Background(), "blade run")

		if !state.Failed {
			t.Error("an upstream failure was not reported as one")
		}
		if state.FoundNothing() {
			t.Error(`a failed request reported "nothing matched", which sends people off to retype a correct title`)
		}
	})
}

// A TMDB outage must not be able to take the submit page down with it: the
// four boxes worked before this ticket and they still do.
func TestSearchNeverReturnsAnErrorUpward(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	state := s.search(context.Background(), "blade run")

	// The search form is still offered, so the page still renders whole.
	if state.Action == "" || state.ResultsID == "" {
		t.Error("a failed search dropped the form's own wiring")
	}
}

// With no key there is no search box at all -- not a box that apologises. This
// is the state every agent's dev stack is in.
func TestSearchUnavailableWithoutAKey(t *testing.T) {
	t.Parallel()

	s := &Service{opts: Options{Logger: slog.New(slog.DiscardHandler)}}

	state := s.search(context.Background(), "blade run")

	if state.Available {
		t.Error("Available is true with a nil TMDB client")
	}
	if state.HasResults() || state.Failed || state.FoundNothing() {
		t.Error("a disabled search reported a result state")
	}
}

func TestLookupPickedFillsTheForm(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	form, picked, details := s.lookupPicked(context.Background(), 78)

	if details == nil {
		t.Fatal("no details for a film the stub has")
	}
	if !form.FromTMDB() {
		t.Error("FromTMDB is false on a TMDB-filled form")
	}
	if form.TMDBID != "78" {
		t.Errorf("TMDBID = %q, want 78", form.TMDBID)
	}
	if form.Title != "Blade Runner" {
		t.Errorf("Title = %q", form.Title)
	}
	if form.Year != "1982" {
		t.Errorf("Year = %q", form.Year)
	}
	// A clean watch URL, which is what makes a reliable embed possible
	// downstream. The key inside it is eleven validated characters.
	if want := "https://www.youtube.com/watch?v=eogpIG53Cis"; form.TrailerURL != want {
		t.Errorf("TrailerURL = %q, want %q", form.TrailerURL, want)
	}
	if form.Description == "" {
		t.Error("the synopsis did not reach the description box")
	}

	if !picked.IsSet() || picked.Label != "Blade Runner (1982)" {
		t.Errorf("Picked = %+v", picked)
	}
	// The escape hatch. This group submits films TMDB has never heard of.
	if picked.ClearHref != SubmitPath {
		t.Errorf("ClearHref = %q, want %q", picked.ClearHref, SubmitPath)
	}
}

// An id naming no film is the manual form, not an error: there is no field to
// put a message beside, because nobody typed it.
func TestLookupPickedFallsBackToManual(t *testing.T) {
	t.Parallel()

	cases := map[string]http.HandlerFunc{
		"TMDB has no such film": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
		"TMDB did not answer": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := serviceWithTMDB(t, handler)

			form, picked, details := s.lookupPicked(context.Background(), 999999999)

			if details != nil {
				t.Error("details returned for a film that could not be confirmed")
			}
			if form.FromTMDB() {
				t.Error("the form claims a TMDB id that was never confirmed")
			}
			if picked.IsSet() {
				t.Error("a film was shown as picked that could not be looked up")
			}
		})
	}
}

func TestLookupPickedWithoutAKey(t *testing.T) {
	t.Parallel()

	s := &Service{opts: Options{Logger: slog.New(slog.DiscardHandler)}}

	form, picked, details := s.lookupPicked(context.Background(), 78)

	if details != nil || form.FromTMDB() || picked.IsSet() {
		t.Error("a nil TMDB client produced a picked film")
	}
}

func TestParseTMDBID(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"78":     78,
		" 78 ":   78,
		"335984": 335984,
		// Everything below is "no film picked", handled as the manual path.
		"":                      0,
		"0":                     0,
		"-1":                    0,
		"78; DROP":              0,
		"1e3":                   0,
		"78.0":                  0,
		"nine":                  0,
		"999999999999999999999": 0,
	}

	for raw, want := range cases {
		if got := parseTMDBID(raw); got != want {
			t.Errorf("parseTMDBID(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestClampQuery(t *testing.T) {
	t.Parallel()

	if got := clampQuery("  blade   run  "); got != "blade run" {
		t.Errorf("clampQuery = %q, want %q", got, "blade run")
	}

	// Truncation is by rune, so a pasted title cannot be cut in half through a
	// multi-byte character and sent to TMDB as invalid UTF-8.
	long := strings.Repeat("é", maxQueryLen*2)
	got := clampQuery(long)

	if n := len([]rune(got)); n > maxQueryLen {
		t.Errorf("clamped to %d runes, want at most %d", n, maxQueryLen)
	}
	if !strings.ContainsRune(got, 'é') || strings.ContainsRune(got, '�') {
		t.Errorf("clamping split a multi-byte character: %q", got)
	}
}

func TestTrailerWatchURL(t *testing.T) {
	t.Parallel()

	if got := trailerWatchURL(""); got != "" {
		t.Errorf("trailerWatchURL(\"\") = %q, want empty for a film with no trailer", got)
	}
	// A canonical watch URL, which is the seam agreed with nap-0z8: its
	// normalizer has to handle hand-pasted watch URLs anyway, so producing a
	// second shape here would give it two inputs instead of one.
	if got, want := trailerWatchURL("eogpIG53Cis"), "https://www.youtube.com/watch?v=eogpIG53Cis"; got != want {
		t.Errorf("trailerWatchURL = %q, want %q", got, want)
	}
	// And it survives the form's own validation, which is what actually
	// decides whether it reaches a row.
	if !validTrailerURL(trailerWatchURL("eogpIG53Cis")) {
		t.Error("a TMDB trailer URL does not pass the submit form's own validation")
	}
}

func TestYearString(t *testing.T) {
	t.Parallel()

	// Zero is the "TMDB has no release date" sentinel. Rendering it as "0"
	// would fail validation as a year outside movie_year_range, on a field
	// nobody touched.
	for _, year := range []int{0, -1} {
		if got := yearString(year); got != "" {
			t.Errorf("yearString(%d) = %q, want empty", year, got)
		}
	}
	if got := yearString(1982); got != "1982" {
		t.Errorf("yearString(1982) = %q", got)
	}
}

func TestPickHref(t *testing.T) {
	t.Parallel()

	if got := pickHref(""); got != SubmitPath {
		t.Errorf("pickHref(\"\") = %q, want the bare submit path", got)
	}
	if got, want := pickHref("78"), "/submit?tmdb_id=78"; got != want {
		t.Errorf("pickHref = %q, want %q", got, want)
	}
}

// The fragment endpoint answers 200 whatever TMDB did, because htmx does not
// swap a non-2xx response by default: a 502 here would leave stale results on
// screen under a search box that had visibly stopped responding.
func TestHandleSearchAlwaysAnswers200(t *testing.T) {
	t.Parallel()

	cases := map[string]http.HandlerFunc{
		"results":    stubTMDB,
		"no results": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"results":[]}`)) },
		"upstream down": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := serviceWithTMDB(t, handler)

			req := httptest.NewRequest(http.MethodGet, SearchPath+"?"+viewmodel.FieldQuery+"=blade+run", nil)
			rec := httptest.NewRecorder()

			s.handleSearch(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			// A fragment, not a page: the shell is already in the browser.
			if strings.Contains(rec.Body.String(), "<html") {
				t.Error("the fragment endpoint returned a whole page")
			}
		})
	}
}

// The query is attacker-chosen text and it is echoed back into the fragment.
// templ escapes it; this is the test that says so out loud, because the day
// somebody renders it into an href instead is the day it matters.
func TestSearchFragmentEscapesTheQuery(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	})

	const payload = `<script>alert(1)</script>`

	req := httptest.NewRequest(http.MethodGet, SearchPath+"?"+viewmodel.FieldQuery+"="+
		"%3Cscript%3Ealert%281%29%3C%2Fscript%3E", nil)
	rec := httptest.NewRecorder()

	s.handleSearch(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, payload) {
		t.Errorf("the query was echoed unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("the query was not echoed at all; the test proves nothing:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// The rule the whole feature rests on: a search having happened does not make
// the form's contents true.
// ---------------------------------------------------------------------------

// A POST can claim anything. This is the test that says what happens when it
// does: the id is honoured as an identifier and every fact is re-fetched, so
// the row that gets written is the film the id names, not the film the browser
// described.
func TestAuthoritativeDiscardsWhatTheBrowserClaims(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	// A form as a tampered client might send it: the real id for Blade Runner,
	// and lies in every other box.
	tampered := viewmodel.SubmitForm{
		TMDBID:      "78",
		Title:       "Free Money Click Here",
		Year:        "2200",
		TrailerURL:  "https://evil.example.com/not-a-trailer",
		Description: "the submitter's own sentence",
	}

	got, details := s.authoritative(context.Background(), tampered, tampered.TMDBID)

	if details == nil {
		t.Fatal("no details for a film the stub has")
	}
	if got.Title != "Blade Runner" {
		t.Errorf("Title = %q; the posted title was not discarded", got.Title)
	}
	if got.Year != "1982" {
		t.Errorf("Year = %q; the posted year was not discarded", got.Year)
	}
	if got.TrailerURL != "https://www.youtube.com/watch?v=eogpIG53Cis" {
		t.Errorf("TrailerURL = %q; the posted trailer was not discarded", got.TrailerURL)
	}
	if strings.Contains(got.TrailerURL, "evil.example.com") {
		t.Error("a client-supplied trailer URL survived to be stored and rendered as an href")
	}

	// The one field that is deliberately the person's: "Why this one" is prose
	// they wrote, and a server that overwrote it with a synopsis would be
	// deleting the only part of the submission that was theirs.
	if got.Description != "the submitter's own sentence" {
		t.Errorf("Description = %q; the person's own sentence was overwritten", got.Description)
	}
}

// An id that cannot be confirmed must not reach the row. tmdb_id is the dedupe
// key for "have we watched this before" — an unconfirmed value in it is worse
// than no value, because it makes a film look deduplicated when it is not.
func TestAuthoritativeDropsAnUnconfirmableID(t *testing.T) {
	t.Parallel()

	cases := map[string]http.HandlerFunc{
		"TMDB has no such film": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
		"TMDB did not answer": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := serviceWithTMDB(t, handler)

			// A manual submission that also carries an id, which is what a
			// stale form or a hand-crafted POST looks like.
			posted := viewmodel.SubmitForm{TMDBID: "424242", Title: "Some Obscure Thing", Year: "1974"}

			got, details := s.authoritative(context.Background(), posted, posted.TMDBID)

			if details != nil {
				t.Error("details returned for a film that could not be confirmed")
			}
			if got.TMDBID != "" {
				t.Errorf("TMDBID = %q; an unconfirmed id survived to the row", got.TMDBID)
			}
			// Demoted to manual, not refused: the person typed a title and it
			// is still a perfectly good submission.
			if got.Title != "Some Obscure Thing" {
				t.Errorf("Title = %q; the typed title was lost", got.Title)
			}
			if got.Year != "1974" {
				t.Errorf("Year = %q; the typed year was lost", got.Year)
			}
		})
	}
}

// The manual path is the one that has to keep working: it is how this group
// submits the films TMDB has never heard of, which is half the fun.
func TestAuthoritativeLeavesAManualSubmissionAlone(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	manual := viewmodel.SubmitForm{
		Title:       "Hausu",
		Year:        "1977",
		TrailerURL:  "https://example.com/trailer",
		Description: "It has a piano that eats someone.",
	}

	got, details := s.authoritative(context.Background(), manual, "")

	if details != nil {
		t.Error("a manual submission was given TMDB details")
	}
	if got.TMDBID != "" {
		t.Errorf("TMDBID = %q, want empty for a manual submission", got.TMDBID)
	}
	if got.Title != manual.Title || got.Year != manual.Year ||
		got.TrailerURL != manual.TrailerURL || got.Description != manual.Description {
		t.Errorf("a manual submission was modified:\n got %+v\nwant %+v", got, manual)
	}
}

// A garbage tmdb_id is the manual path, not an error: nobody typed it, so
// there is no field to put a message beside.
func TestAuthoritativeTreatsAGarbageIDAsManual(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	for _, raw := range []string{"", "0", "-5", "78; DROP TABLE movie", "../../78", "nine"} {
		posted := viewmodel.SubmitForm{TMDBID: raw, Title: "Typed By Hand"}

		got, details := s.authoritative(context.Background(), posted, raw)

		if details != nil {
			t.Errorf("tmdb_id %q produced details", raw)
		}
		if got.TMDBID != "" {
			t.Errorf("tmdb_id %q survived as %q", raw, got.TMDBID)
		}
		if got.Title != "Typed By Hand" {
			t.Errorf("tmdb_id %q cost the typed title", raw)
		}
	}
}

// The trailer TMDB gives us has to survive the form's own validation, or the
// picker would produce submissions the server then rejects.
func TestTMDBSubmissionPassesValidation(t *testing.T) {
	t.Parallel()

	s := serviceWithTMDB(t, stubTMDB)

	form, _ := s.authoritative(context.Background(), viewmodel.SubmitForm{TMDBID: "78"}, "78")

	d, errs := validate(form)
	if errs.Any() {
		t.Fatalf("a TMDB-filled form failed the form's own validation: %+v", errs)
	}
	if d.title != "Blade Runner" {
		t.Errorf("title = %q", d.title)
	}
	if d.year == nil || *d.year != 1982 {
		t.Errorf("year = %v", d.year)
	}
	if d.trailerURL == nil || *d.trailerURL != "https://www.youtube.com/watch?v=eogpIG53Cis" {
		t.Errorf("trailerURL = %v", d.trailerURL)
	}
}
