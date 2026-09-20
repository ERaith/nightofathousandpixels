// Package tmdb is a small read-only client for The Movie Database.
//
// It exists so that putting a film on the board is a search rather than four
// empty boxes (ticket E10, nap-eie). Two questions are asked of TMDB and no
// others:
//
//	Search  "what films match roughly this text"  -> a list to pick from
//	Lookup  "what is film 78"                     -> the facts we store
//
// Three properties of this package are load-bearing, and each one is a rule
// somewhere else in the codebase leans on:
//
//   - Fuzzy matching is TMDB's, not ours. /search/movie already handles typos,
//     partial titles and alternate titles in thirty languages. There is no
//     matcher in this package and there should never be one.
//
//   - Lookup is the authority, Search is a menu. A browser can post any
//     tmdb_id it likes, so the handler re-asks Lookup for the title, year and
//     trailer rather than believing the hidden fields that came back with the
//     form. Search results are never what gets written to a row.
//
//   - A trailer key is a key, never a URL. TMDB hands back an eleven-character
//     YouTube video id; it is validated against that shape here and handed on
//     as a key, so the one place that builds a URL out of it is the caller.
//     See TrailerKey.
//
// Absence is a supported state. A Client built with no API key answers every
// call with ErrNoAPIKey and the submit form falls back to the four boxes it
// has always had -- see internal/web/board.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is TMDB's v3 API root.
const DefaultBaseURL = "https://api.themoviedb.org/3"

// imageBaseURL is TMDB's image CDN, with the poster size the result list uses.
//
// TMDB documents a /configuration endpoint that returns this base and the list
// of valid sizes, and the correct-by-the-book thing is to call it at startup.
// It is deliberately not called. The value has not changed in the lifetime of
// the v3 API, and calling it would mean either a network round trip before the
// server can serve its first page or a second failure mode on a path whose
// whole point is to degrade quietly. A hard-coded base that is wrong shows
// broken thumbnails on one fragment; a startup call that hangs is a site that
// does not come up.
//
// w154 is chosen against the CSS rather than by eye: the result list renders
// posters in a 92px-wide column, and w154 is the next size up, so a 2x display
// has real pixels to use and a 1x display downloads about 8KB.
const imageBaseURL = "https://image.tmdb.org/t/p/w154"

// maxResults is how many matches a search fragment shows.
//
// TMDB returns twenty per page. Eight is what fits on a phone without the
// picker becoming a page of its own, and the ninth result is almost never the
// film somebody meant -- if it is, the words they typed were the problem and
// another eight rows will not fix it.
const maxResults = 8

// requestTimeout bounds one call to TMDB.
//
// It is short on purpose. This runs while somebody is typing, inside a request
// the server's own writeTimeout will cut off at thirty seconds, and a search
// that has not answered in four seconds has already failed at its job. The
// fallback when it expires is the manual form, which is not a bad place to be.
const requestTimeout = 4 * time.Second

// Errors a caller is expected to tell apart.
var (
	// ErrNoAPIKey is every call on a Client built without a key. It is the
	// configured-off state rather than a failure: the handler turns it into a
	// submit page with no search box on it, and says so.
	ErrNoAPIKey = errors.New("tmdb: no API key configured")

	// ErrNotFound is a tmdb_id TMDB does not have. It is separated from a
	// transport failure because the two mean opposite things on the submit
	// path: "that film does not exist" is the person's problem and is worth
	// saying, and "TMDB is down" is ours and is not.
	ErrNotFound = errors.New("tmdb: no such movie")
)

// trailerKeyShape is what a YouTube video id looks like: eleven characters of
// the URL-safe base64 alphabet.
//
// Every key TMDB returns is checked against it, and that is not paranoia about
// TMDB. The key travels from here into movie.trailer_url and from there into
// an href and, once nap-0z8 lands, into an iframe src. A field that reaches an
// iframe src should be proved to be eleven harmless characters at the point it
// enters the system, not trusted because of where it came from.
var trailerKeyShape = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Movie is one search result: enough to recognise a film in a list, and
// nothing that would be worth writing to a row.
//
// It is a separate type from Details for that reason. Nothing that comes back
// from Search is ever stored -- the handler re-asks Lookup by id before it
// writes -- and giving the two the same shape would make it easy to forget.
type Movie struct {
	// ID is tmdb_id: TMDB's own identifier, and the only field here the
	// server will still trust after the form comes back.
	ID int

	// Title is the film's title in the requested language.
	Title string

	// Year is the release year, or 0 when TMDB has no release date. Zero is
	// common on very obscure and very new entries, and it is why movie.year is
	// nullable.
	Year int

	// Overview is TMDB's synopsis. It is frequently empty, and the submit form
	// treats an empty one as "nothing to prefill" rather than an error.
	Overview string

	// PosterPath is TMDB's own path, like "/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
	// or "" when the film has no poster. Use PosterURL to render it.
	PosterPath string
}

// PosterURL is an absolute URL for this film's poster, or "" when there is no
// poster to show.
//
// The empty string matters: a template that rendered an <img> at the image
// base with nothing after it would ask the CDN for a directory and draw a
// broken-image icon in a list whose whole job is to be recognisable.
func (m Movie) PosterURL() string {
	if m.PosterPath == "" {
		return ""
	}

	// TMDB's paths carry their own leading slash. Concatenating rather than
	// using url.JoinPath keeps a path that has somehow lost it from silently
	// producing a URL one directory up.
	if !strings.HasPrefix(m.PosterPath, "/") {
		return ""
	}

	return imageBaseURL + m.PosterPath
}

// Label is how a result reads in the picker: "Blade Runner (1982)", or just
// the title when TMDB has no release date.
func (m Movie) Label() string {
	if m.Year <= 0 {
		return m.Title
	}

	return m.Title + " (" + strconv.Itoa(m.Year) + ")"
}

// Details is a film looked up by id: a Movie plus the trailer.
//
// This -- and only this -- is what a submission is built from. See the package
// comment on why Search results are not.
type Details struct {
	Movie

	// TrailerKey is the YouTube video id of the best trailer TMDB lists, or ""
	// when it lists none that is usable.
	//
	// It is the bare key rather than a URL because the caller decides what to
	// build from it, and because it has been checked against trailerKeyShape:
	// a URL would have to be re-parsed by everyone downstream to get back to
	// the eleven characters that were actually validated. See ticket nap-0z8,
	// which owns turning trailers into embeds -- this package deliberately
	// does not.
	TrailerKey string
}

// Client talks to TMDB. Build one with New. The zero value is not usable; a
// nil *Client is, and answers ErrNoAPIKey, which is what lets a caller hold
// one field instead of a field and a bool.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client

	// Two caches rather than one, because the two questions go stale at
	// completely different rates. See cache.go.
	searches *cache[[]Movie]
	details  *cache[Details]
}

// Options configures a Client.
type Options struct {
	// APIKey is a TMDB v3 API key. Blank builds a Client that answers
	// ErrNoAPIKey, which is the supported "search is switched off" state.
	APIKey string

	// BaseURL overrides DefaultBaseURL. It exists so the test suite and a
	// local dev stack can point at a stub that serves recorded payloads
	// instead of at TMDB. Blank means DefaultBaseURL.
	BaseURL string

	// HTTPClient overrides the default. Blank means a client with
	// requestTimeout.
	HTTPClient *http.Client
}

// New returns a Client.
//
// It never fails and never dials: a bad key is not detectable without a
// request, and a server that refused to start because TMDB was unreachable
// would be trading a working manual form for an outage.
func New(opts Options) *Client {
	base := strings.TrimSuffix(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}

	return &Client{
		apiKey:   strings.TrimSpace(opts.APIKey),
		baseURL:  base,
		http:     httpClient,
		searches: newCache[[]Movie](searchTTL, maxCacheEntries),
		details:  newCache[Details](detailTTL, maxCacheEntries),
	}
}

// Enabled reports whether this Client can reach TMDB at all.
//
// It is what the submit page asks before it renders a search box, so that the
// page a person gets on a stack with no key is the plain four-box form rather
// than a search field that answers every query with an apology.
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

// Search returns the films matching q, best match first, at most maxResults.
//
// A blank or whitespace-only query returns no results and no error, and makes
// no request: it is what the search box sends while somebody is deleting what
// they typed, and it is not a question TMDB can answer.
func (c *Client) Search(ctx context.Context, q string) ([]Movie, error) {
	if !c.Enabled() {
		return nil, ErrNoAPIKey
	}

	key := searchKey(q)
	if key == "" {
		return nil, nil
	}

	if hit, ok := c.searches.get(key); ok {
		return hit, nil
	}

	var payload searchResponse
	if err := c.get(ctx, "/search/movie", url.Values{
		"query": {key},
		// The group watches horror in October; TMDB's adult flag is
		// pornography, not horror, and nothing behind it belongs on a board
		// that gets pasted into a group chat.
		"include_adult": {"false"},
		"language":      {"en-US"},
		"page":          {"1"},
	}, &payload); err != nil {
		return nil, err
	}

	results := make([]Movie, 0, maxResults)
	for _, r := range payload.Results {
		if len(results) == maxResults {
			break
		}
		// A result with no title is not something a person can pick out of a
		// list, and movie_title_not_blank would refuse it anyway.
		if strings.TrimSpace(r.Title) == "" {
			continue
		}

		results = append(results, r.movie())
	}

	c.searches.put(key, results)

	return results, nil
}

// Lookup returns the facts about one film.
//
// This is the call that decides what goes in a row. It asks for the videos in
// the same request -- TMDB's append_to_response does it server-side -- because
// the alternative is two round trips on the POST path while somebody waits to
// find out whether their film went up.
func (c *Client) Lookup(ctx context.Context, id int) (Details, error) {
	var zero Details

	if !c.Enabled() {
		return zero, ErrNoAPIKey
	}
	if id <= 0 {
		// TMDB ids are positive. Asking anyway would spend a round trip to be
		// told so, on a value that came off a form.
		return zero, ErrNotFound
	}

	key := strconv.Itoa(id)
	if hit, ok := c.details.get(key); ok {
		return hit, nil
	}

	var payload detailResponse
	if err := c.get(ctx, "/movie/"+key, url.Values{
		"language":           {"en-US"},
		"append_to_response": {"videos"},
	}, &payload); err != nil {
		return zero, err
	}

	// TMDB answers 200 with a null id for some malformed requests rather than
	// a 404, so identity is checked rather than assumed.
	if payload.ID <= 0 {
		return zero, ErrNotFound
	}

	details := Details{
		Movie:      payload.movie(),
		TrailerKey: pickTrailerKey(payload.Videos.Results),
	}

	c.details.put(key, details)

	return details, nil
}

// get performs one GET against TMDB and decodes the body into out.
//
// Two things about it are deliberate. The API key goes in the query string,
// because that is what a TMDB v3 key is -- and so no error, log line or
// wrapped message in this package may ever contain the URL. Every error below
// names the path and the status and nothing else, and TestErrorsDoNotLeakKey
// holds that. The second is the body limit: an unbounded json.Decode over a
// response from a third party is an allocation somebody else controls.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	params.Set("api_key", c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		// Only a malformed base URL reaches here, and the message from
		// url.Parse would quote the whole URL, key included.
		return fmt.Errorf("tmdb: build request for %s", path)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// http.Client wraps a *url.Error, whose Error method prints the URL.
		// So the transport error is described rather than wrapped.
		return fmt.Errorf("tmdb: GET %s: request failed", path)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("tmdb: GET %s: status %d", path, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("tmdb: GET %s: malformed response", path)
	}

	return nil
}

// maxResponseBytes caps a TMDB response body. A search page is a few KB and a
// movie with its videos is smaller; 1MB is far above any real answer and far
// below a problem.
const maxResponseBytes = 1 << 20

// searchKey normalises a query for both the request and the cache.
//
// Collapsing internal whitespace is what makes "blade  runner" and "blade
// runner" one cache entry rather than two, which matters more than it sounds:
// the search box fires on every keystroke, so a person typing a space twice
// would otherwise cost a round trip to learn what we already knew.
func searchKey(q string) string {
	return strings.Join(strings.Fields(q), " ")
}

// How a trailer candidate is scored. See pickTrailerKey.
//
// The three weights are banded rather than adjacent, and that is the whole
// design: each one must outrank every combination of the ones below it, so
// that adding a fourth signal later cannot silently reorder the first three.
// Scored as adjacent small numbers instead, "official French trailer" and
// "unofficial English trailer" come out equal, and which one a person gets
// depends on the order TMDB happened to list them in.
const (
	// scoreTrailer and scoreTeaser: what kind of thing it is. This dominates,
	// because a teaser is not what somebody clicking "trailer" wants, in any
	// language and however official.
	scoreTrailer = 10
	scoreTeaser  = 1

	// scoreEnglish: outranks official, because everyone involved speaks
	// English and nobody is helped by an official German cut.
	scoreEnglish = 4

	// scoreOfficial: the tiebreak, and only the tiebreak.
	scoreOfficial = 1
)

// pickTrailerKey chooses the one trailer worth storing, or "".
//
// The order is the point. TMDB lists teasers, clips, featurettes, behind-the-
// scenes and half a dozen trailers for a big film, in no useful order, and the
// first YouTube entry in the list is regularly a thirty-second teaser or a
// foreign-language cut. So candidates are scored and the best one wins:
//
//	an official English trailer  beats
//	any English trailer          beats
//	any trailer                  beats
//	an English teaser            beats
//	any teaser
//
// Anything that is not a trailer or a teaser is not a candidate at all -- a
// clip is a spoiler and a featurette is twenty minutes of interviews.
//
// Every candidate must be on YouTube and must have a key of exactly the right
// shape. Vimeo entries exist in TMDB's data and have no embed story here, and
// a key that is not eleven URL-safe characters is not a YouTube id whatever
// TMDB says it is.
func pickTrailerKey(videos []video) string {
	best, bestScore := "", 0

	for _, v := range videos {
		if !strings.EqualFold(v.Site, "YouTube") || !trailerKeyShape.MatchString(v.Key) {
			continue
		}

		var score int
		switch {
		case strings.EqualFold(v.Type, "Trailer"):
			score = scoreTrailer
		case strings.EqualFold(v.Type, "Teaser"):
			score = scoreTeaser
		default:
			continue
		}

		if strings.EqualFold(v.ISO639_1, "en") {
			score += scoreEnglish
		}
		if v.Official {
			score += scoreOfficial
		}

		// Strictly greater, so the first of two equally good trailers wins.
		// TMDB returns these newest-first for most films, and the newest cut
		// is the one people have not seen.
		if score > bestScore {
			best, bestScore = v.Key, score
		}
	}

	return best
}

// ---------------------------------------------------------------------------
// The wire types. They are unexported and exist only to be decoded into: every
// field TMDB sends that this site does not use is deliberately absent, so that
// a new field in their response is not a new field in ours.
// ---------------------------------------------------------------------------

type searchResponse struct {
	Results []movieResponse `json:"results"`
}

type movieResponse struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	PosterPath  string `json:"poster_path"`
	ReleaseDate string `json:"release_date"`
}

type detailResponse struct {
	movieResponse

	Videos struct {
		Results []video `json:"results"`
	} `json:"videos"`
}

type video struct {
	Key      string `json:"key"`
	Site     string `json:"site"`
	Type     string `json:"type"`
	ISO639_1 string `json:"iso_639_1"`
	Official bool   `json:"official"`
}

// movie converts a wire result into the shape the rest of the site uses.
func (r movieResponse) movie() Movie {
	return Movie{
		ID:         r.ID,
		Title:      strings.TrimSpace(r.Title),
		Year:       releaseYear(r.ReleaseDate),
		Overview:   strings.TrimSpace(r.Overview),
		PosterPath: r.PosterPath,
	}
}

// releaseYear pulls the year out of TMDB's "2006-01-02", or returns 0.
//
// Empty is the common case and not an error: TMDB has thousands of entries
// with no release date, and a handful with "" where a date should be. Anything
// outside the range movie_year_range allows is also 0, so that a bad date in
// their data becomes a missing year here rather than a constraint violation
// three layers down, at the moment somebody presses the button.
func releaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}

	year, err := strconv.Atoi(date[:4])
	if err != nil || year < MinYear || year > MaxYear {
		return 0
	}

	return year
}

// The range movie.year accepts, from movie_year_range in migration 00004.
//
// They are duplicated here rather than imported because this package must not
// depend on the web layer, and because the check has to happen at the edge: a
// year TMDB reports as 12002 should become "unknown" while it is still a
// number on a wire, not after it has been put in a form field.
const (
	MinYear = 1888
	MaxYear = 2200
)
