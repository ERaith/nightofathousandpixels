package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TMDB is a third-party HTTP service, so it is the right thing to fake: there
// is no container to run and no key to check in, and a recorded payload is a
// genuine test of the decoder, the trailer choice and the cache. What is NOT
// faked anywhere in this package is our own database -- nothing here touches
// one, which is why these tests carry no build tag.
//
// The payloads below are the shapes TMDB actually returns, trimmed to the
// fields this client reads plus a few it deliberately ignores.

const searchBladeRunner = `{
  "page": 1,
  "results": [
    {
      "adult": false,
      "backdrop_path": "/9Wt5zjQkTVVTmsX5ZqNaxqXFIUn.jpg",
      "genre_ids": [878, 18],
      "id": 78,
      "original_language": "en",
      "original_title": "Blade Runner",
      "overview": "In the smog-choked dystopian Los Angeles of 2019, blade runner Rick Deckard is called out of retirement to terminate a quartet of replicants.",
      "popularity": 45.2,
      "poster_path": "/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
      "release_date": "1982-06-25",
      "title": "Blade Runner",
      "video": false,
      "vote_average": 7.9,
      "vote_count": 13241
    },
    {
      "adult": false,
      "id": 335984,
      "overview": "Thirty years after the events of the first film...",
      "poster_path": "/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg",
      "release_date": "2017-10-04",
      "title": "Blade Runner 2049"
    },
    {
      "adult": false,
      "id": 14329,
      "overview": "",
      "poster_path": null,
      "release_date": "",
      "title": "Blade Runner: The Final Cut"
    }
  ],
  "total_pages": 1,
  "total_results": 3
}`

const detailBladeRunner = `{
  "adult": false,
  "budget": 28000000,
  "id": 78,
  "imdb_id": "tt0083658",
  "original_title": "Blade Runner",
  "overview": "In the smog-choked dystopian Los Angeles of 2019, blade runner Rick Deckard is called out of retirement to terminate a quartet of replicants.",
  "poster_path": "/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
  "release_date": "1982-06-25",
  "runtime": 118,
  "title": "Blade Runner",
  "videos": {
    "results": [
      {
        "iso_639_1": "en", "iso_3166_1": "US",
        "name": "Behind the scenes with Ridley Scott",
        "key": "aaaaaaaaaaa", "site": "YouTube",
        "size": 1080, "type": "Featurette", "official": true,
        "id": "5a3af7ff0e0a2670c5039b21"
      },
      {
        "iso_639_1": "de", "iso_3166_1": "DE",
        "name": "Deutscher Trailer",
        "key": "bbbbbbbbbbb", "site": "YouTube",
        "size": 720, "type": "Trailer", "official": false,
        "id": "5a3af7ff0e0a2670c5039b22"
      },
      {
        "iso_639_1": "en", "iso_3166_1": "US",
        "name": "Theatrical Trailer",
        "key": "eogpIG53Cis", "site": "YouTube",
        "size": 1080, "type": "Trailer", "official": true,
        "id": "5a3af7ff0e0a2670c5039b27"
      },
      {
        "iso_639_1": "en", "iso_3166_1": "US",
        "name": "Vimeo cut",
        "key": "123456789", "site": "Vimeo",
        "size": 1080, "type": "Trailer", "official": true,
        "id": "5a3af7ff0e0a2670c5039b28"
      }
    ]
  }
}`

// stub is a fake TMDB. It records what was asked of it so a test can assert on
// the request as well as the response.
type stub struct {
	*httptest.Server

	calls atomic.Int64

	// lastQuery is the query string of the most recent request.
	lastQuery url.Values
	lastPath  string
}

func newStub(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *stub {
	t.Helper()

	s := &stub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		s.lastPath = r.URL.Path
		s.lastQuery = r.URL.Query()
		handler(w, r)
	}))
	t.Cleanup(s.Close)

	return s
}

// serving answers every request with one body.
func serving(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func newTestClient(t *testing.T, s *stub) *Client {
	t.Helper()

	return New(Options{APIKey: "test-key", BaseURL: s.URL})
}

func TestSearchDecodesResults(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := newTestClient(t, s)

	got, err := c.Search(context.Background(), "blade run")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}

	first := got[0]
	if first.ID != 78 {
		t.Errorf("ID = %d, want 78", first.ID)
	}
	if first.Title != "Blade Runner" {
		t.Errorf("Title = %q, want %q", first.Title, "Blade Runner")
	}
	if first.Year != 1982 {
		t.Errorf("Year = %d, want 1982", first.Year)
	}
	if !strings.HasPrefix(first.Overview, "In the smog-choked") {
		t.Errorf("Overview = %q, want TMDB's synopsis", first.Overview)
	}
	if want := imageBaseURL + "/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg"; first.PosterURL() != want {
		t.Errorf("PosterURL() = %q, want %q", first.PosterURL(), want)
	}
	if first.Label() != "Blade Runner (1982)" {
		t.Errorf("Label() = %q", first.Label())
	}
}

// A partial, badly typed title is the whole point of the feature, and the
// matching is TMDB's: this asserts the query reaches them unmangled rather
// than that we found anything.
func TestSearchSendsTheQueryVerbatim(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := newTestClient(t, s)

	if _, err := c.Search(context.Background(), "  blade   run  "); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got, want := s.lastPath, "/search/movie"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	// Collapsed, not reinterpreted: TMDB does the fuzzy matching.
	if got, want := s.lastQuery.Get("query"), "blade run"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if got := s.lastQuery.Get("include_adult"); got != "false" {
		t.Errorf("include_adult = %q, want false", got)
	}
}

// A result with no poster and no release date is common in TMDB's long tail,
// and is exactly the kind of film this group submits. It must survive with
// empty sentinels rather than being dropped or rendered as a broken image.
func TestSearchTolleratesMissingPosterAndDate(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := newTestClient(t, s)

	got, err := c.Search(context.Background(), "blade run")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	third := got[2]
	if third.Year != 0 {
		t.Errorf("Year = %d, want 0 for an empty release_date", third.Year)
	}
	if third.PosterURL() != "" {
		t.Errorf("PosterURL() = %q, want empty for a null poster_path", third.PosterURL())
	}
	if third.Label() != "Blade Runner: The Final Cut" {
		t.Errorf("Label() = %q, want no parenthesised year", third.Label())
	}
}

func TestSearchCapsResults(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString(`{"results":[`)
	for i := range 20 {
		if i > 0 {
			body.WriteString(",")
		}
		body.WriteString(`{"id":`)
		body.WriteString(string(rune('1' + i%9)))
		body.WriteString(`,"title":"Film","release_date":"1999-01-01"}`)
	}
	body.WriteString(`]}`)

	s := newStub(t, serving(body.String()))
	c := newTestClient(t, s)

	got, err := c.Search(context.Background(), "film")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != maxResults {
		t.Errorf("got %d results, want the cap of %d", len(got), maxResults)
	}
}

func TestSearchBlankQueryMakesNoRequest(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := newTestClient(t, s)

	for _, q := range []string{"", "   ", "\t\n"} {
		got, err := c.Search(context.Background(), q)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(got) != 0 {
			t.Errorf("Search(%q) returned %d results, want none", q, len(got))
		}
	}

	if n := s.calls.Load(); n != 0 {
		t.Errorf("made %d requests for blank queries, want 0", n)
	}
}

func TestLookupReturnsTheFactsAndTheTrailer(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(detailBladeRunner))
	c := newTestClient(t, s)

	got, err := c.Lookup(context.Background(), 78)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if got.ID != 78 || got.Title != "Blade Runner" || got.Year != 1982 {
		t.Errorf("Lookup = %+v, want id 78 / Blade Runner / 1982", got.Movie)
	}
	if got.Overview == "" {
		t.Error("Overview is empty; the synopsis is half the point of the ticket")
	}

	// The official English trailer, not the featurette listed first, not the
	// German trailer, and not the Vimeo entry.
	if got.TrailerKey != "eogpIG53Cis" {
		t.Errorf("TrailerKey = %q, want the official English YouTube trailer", got.TrailerKey)
	}

	// One round trip, not two: the videos come back on the same request.
	if n := s.calls.Load(); n != 1 {
		t.Errorf("made %d requests, want 1 (append_to_response)", n)
	}
	if got := s.lastQuery.Get("append_to_response"); got != "videos" {
		t.Errorf("append_to_response = %q, want videos", got)
	}
	if got, want := s.lastPath, "/movie/78"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestPickTrailerKey(t *testing.T) {
	t.Parallel()

	yt := func(key, typ, lang string, official bool) video {
		return video{Key: key, Site: "YouTube", Type: typ, ISO639_1: lang, Official: official}
	}

	cases := []struct {
		name   string
		videos []video
		want   string
	}{
		{"nothing at all", nil, ""},
		{
			"a clip is not a trailer",
			[]video{yt("aaaaaaaaaaa", "Clip", "en", true)},
			"",
		},
		{
			"a teaser beats nothing",
			[]video{yt("bbbbbbbbbbb", "Teaser", "en", false)},
			"bbbbbbbbbbb",
		},
		{
			"a trailer beats a teaser, official or not",
			[]video{yt("aaaaaaaaaaa", "Teaser", "en", true), yt("ccccccccccc", "Trailer", "en", false)},
			"ccccccccccc",
		},
		{
			"English beats not-English",
			[]video{yt("aaaaaaaaaaa", "Trailer", "fr", true), yt("ddddddddddd", "Trailer", "en", false)},
			"ddddddddddd",
		},
		{
			"official breaks a tie between two English trailers",
			[]video{yt("aaaaaaaaaaa", "Trailer", "en", false), yt("eeeeeeeeeee", "Trailer", "en", true)},
			"eeeeeeeeeee",
		},
		{
			"the first of two equally good trailers wins",
			[]video{yt("fffffffffff", "Trailer", "en", true), yt("ggggggggggg", "Trailer", "en", true)},
			"fffffffffff",
		},
		{
			"Vimeo is not a candidate",
			[]video{{Key: "123456789", Site: "Vimeo", Type: "Trailer", ISO639_1: "en", Official: true}},
			"",
		},
		{
			// The key reaches an href and, after nap-0z8, an iframe src.
			"a key that is not a YouTube id is refused",
			[]video{
				yt("short", "Trailer", "en", true),
				yt("way-too-long-to-be-real", "Trailer", "en", true),
				yt("abc<script>", "Trailer", "en", true),
			},
			"",
		},
		{
			"a bad key does not shadow a good one",
			[]video{yt("<script>xxx", "Trailer", "en", true), yt("hhhhhhhhhhh", "Trailer", "en", true)},
			"hhhhhhhhhhh",
		},
		{
			// TMDB's casing is not guaranteed.
			"site and type are matched case-insensitively",
			[]video{yt("iiiiiiiiiii", "trailer", "EN", true)},
			"iiiiiiiiiii",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := pickTrailerKey(tc.videos); got != tc.want {
				t.Errorf("pickTrailerKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLookupNotFound(t *testing.T) {
	t.Parallel()

	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"status_code":34,"status_message":"The resource you requested could not be found."}`))
	})
	c := newTestClient(t, s)

	if _, err := c.Lookup(context.Background(), 999999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup on a 404 = %v, want ErrNotFound", err)
	}
}

// A tmdb_id off a form is not a promise that TMDB has such a film; a
// non-positive one is not even a promise that it is an id.
func TestLookupRejectsNonPositiveIDWithoutARequest(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(detailBladeRunner))
	c := newTestClient(t, s)

	for _, id := range []int{0, -1, -78} {
		if _, err := c.Lookup(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%d) = %v, want ErrNotFound", id, err)
		}
	}

	if n := s.calls.Load(); n != 0 {
		t.Errorf("made %d requests for impossible ids, want 0", n)
	}
}

func TestNoAPIKeyIsASupportedState(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := New(Options{BaseURL: s.URL})

	if c.Enabled() {
		t.Error("Enabled() is true with no API key")
	}
	if _, err := c.Search(context.Background(), "blade runner"); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Search = %v, want ErrNoAPIKey", err)
	}
	if _, err := c.Lookup(context.Background(), 78); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Lookup = %v, want ErrNoAPIKey", err)
	}
	if n := s.calls.Load(); n != 0 {
		t.Errorf("made %d requests with no key, want 0", n)
	}
}

// A nil *Client is the "search is switched off" value a caller may hold in one
// field. It must not panic.
func TestNilClient(t *testing.T) {
	t.Parallel()

	var c *Client

	if c.Enabled() {
		t.Error("nil client reports Enabled()")
	}
	if _, err := c.Search(context.Background(), "x"); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Search on nil client = %v, want ErrNoAPIKey", err)
	}
	if _, err := c.Lookup(context.Background(), 78); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Lookup on nil client = %v, want ErrNoAPIKey", err)
	}
}

// The API key travels in the query string, so it is in every request URL. An
// error that wrapped a *url.Error -- which is what http.Client returns -- would
// print that URL, and the first place these errors go is a log line.
func TestErrorsDoNotLeakKey(t *testing.T) {
	t.Parallel()

	const key = "super-secret-tmdb-key"

	// A server that is closed before use: Do fails with a *url.Error whose
	// message contains the whole URL.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	broken := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	garbage := newStub(t, serving(`{"results": [[[[`))

	for name, c := range map[string]*Client{
		"transport failure": New(Options{APIKey: key, BaseURL: deadURL}),
		"bad status":        New(Options{APIKey: key, BaseURL: broken.URL}),
		"malformed body":    New(Options{APIKey: key, BaseURL: garbage.URL}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := c.Search(context.Background(), "blade runner")
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), key) {
				t.Errorf("error leaks the API key: %q", err)
			}
			if strings.Contains(err.Error(), "api_key") {
				t.Errorf("error leaks the query string: %q", err)
			}
		})
	}
}

func TestSearchCachesByNormalisedQuery(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(searchBladeRunner))
	c := newTestClient(t, s)

	// The same search typed three ways is one question.
	for _, q := range []string{"blade run", "  blade run", "blade   run  "} {
		if _, err := c.Search(context.Background(), q); err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
	}

	if n := s.calls.Load(); n != 1 {
		t.Errorf("made %d requests for one normalised query, want 1", n)
	}
}

func TestLookupCachesByID(t *testing.T) {
	t.Parallel()

	s := newStub(t, serving(detailBladeRunner))
	c := newTestClient(t, s)

	for range 5 {
		if _, err := c.Lookup(context.Background(), 78); err != nil {
			t.Fatalf("Lookup: %v", err)
		}
	}

	if n := s.calls.Load(); n != 1 {
		t.Errorf("made %d requests for one id, want 1", n)
	}
}

// A failure must not be cached: TMDB being down for a minute should not mean
// an empty result list for ten.
func TestFailuresAreNotCached(t *testing.T) {
	t.Parallel()

	var fail atomic.Bool
	fail.Store(true)

	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchBladeRunner))
	})
	c := newTestClient(t, s)

	if _, err := c.Search(context.Background(), "blade run"); err == nil {
		t.Fatal("want an error from a 502")
	}

	fail.Store(false)

	got, err := c.Search(context.Background(), "blade run")
	if err != nil {
		t.Fatalf("Search after recovery: %v", err)
	}
	if len(got) == 0 {
		t.Error("a cached failure survived the recovery")
	}
}

func TestCacheExpires(t *testing.T) {
	t.Parallel()

	now := time.Now()
	c := newCache[string](time.Minute, 10)
	c.now = func() time.Time { return now }

	c.put("k", "v")

	if got, ok := c.get("k"); !ok || got != "v" {
		t.Fatalf("get = %q, %v; want the stored value", got, ok)
	}

	now = now.Add(time.Minute)

	if _, ok := c.get("k"); ok {
		t.Error("an entry survived exactly its TTL; the boundary should expire")
	}
	if c.len() != 0 {
		t.Error("an expired entry was not dropped on the way past")
	}
}

func TestCacheStaysUnderItsLimit(t *testing.T) {
	t.Parallel()

	const limit = 50

	c := newCache[int](time.Hour, limit)
	for i := range limit * 10 {
		c.put(string(rune(i)), i)
		if c.len() > limit {
			t.Fatalf("cache grew to %d, over its limit of %d", c.len(), limit)
		}
	}
}

func TestReleaseYear(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"1982-06-25": 1982,
		"2017-10-04": 2017,
		"":           0,
		"19":         0,
		"not-a-date": 0,
		// Outside movie_year_range: becomes "unknown" here rather than a
		// constraint violation at the moment somebody presses the button.
		"1600-01-01":  0,
		"12002-01-01": 0,
	}

	for date, want := range cases {
		if got := releaseYear(date); got != want {
			t.Errorf("releaseYear(%q) = %d, want %d", date, got, want)
		}
	}
}

// A poster path is concatenated onto the image CDN base, so a path that does
// not start at the root must not produce a URL one directory up.
func TestPosterURLRefusesARelativePath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "no-leading-slash.jpg", "../../etc/passwd"} {
		m := Movie{PosterPath: path}
		if got := m.PosterURL(); got != "" {
			t.Errorf("PosterURL() for %q = %q, want empty", path, got)
		}
	}
}
