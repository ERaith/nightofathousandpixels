// Command mocktmdbd is a stand-in for The Movie Database, for developing and
// demonstrating the submit page's search without a TMDB API key.
//
// It exists for the same reason devtools/mockoidcd does, and it is the same
// kind of thing: TMDB_BASE_URL is already configuration rather than a constant
// (see internal/config), so pointing it at this process changes which host
// answers and nothing else. The application still builds the same requests,
// still decodes the same JSON, still picks a trailer out of a videos list and
// still re-fetches by id on POST. The only thing that is not real is the
// catalogue.
//
// Why it is worth having at all: a TMDB key belongs to a person, cannot be
// checked into this repository, and is not something every agent working on
// this project has. Without this, the search path is code that only its author
// has ever seen run, which is exactly how this project has repeatedly produced
// green suites over features that did not work.
//
//	go run ./devtools/mocktmdbd -addr :9330
//	TMDB_API_KEY=anything TMDB_BASE_URL=http://localhost:9330 make dev
//
// It is NOT a fixture for the Go tests. Those use httptest.Server with
// recorded payloads inline, which is a genuine unit test of the client; see
// internal/tmdb/tmdb_test.go. This is for a human with a browser.
//
// Like mockoidcd, it is never built into the shipped image: it is its own main
// package under devtools/ and nothing in cmd/server imports it.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// film is one entry in the fake catalogue.
//
// The fields are named for TMDB's wire format rather than for ours, because
// the whole value of this process is that it produces the shape TMDB produces.
// A stub that emitted our own field names would test nothing.
type film struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	PosterPath  string `json:"poster_path"`
	ReleaseDate string `json:"release_date"`

	// videos is not part of the search response and is attached only on a
	// detail lookup, which is how TMDB behaves.
	videos []video
}

type video struct {
	Key      string `json:"key"`
	Site     string `json:"site"`
	Type     string `json:"type"`
	ISO639_1 string `json:"iso_639_1"`
	Official bool   `json:"official"`
	Name     string `json:"name"`
}

// catalogue is deliberately small and deliberately awkward.
//
// Every entry is here to exercise a branch that is otherwise hard to reach
// with a real key, and the awkward ones are the point:
//
//   - two Blade Runners, so a partial search returns more than one plausible
//     answer and the year is what tells them apart;
//   - a film with no poster and no release date, which is the long tail this
//     group actually submits;
//   - a film whose only video is a teaser, and one with no videos at all, so
//     the trailer-choice code has something to fail to find;
//   - a film listing a featurette first and a German trailer second, which is
//     what TMDB really does and what pickTrailerKey exists for;
//   - a title with markup in it, so that anyone clicking through can see for
//     themselves that it renders as text.
//
// The poster paths point at real TMDB CDN files. Nothing else here is real.
var catalogue = []film{
	{
		ID: 78, Title: "Blade Runner", ReleaseDate: "1982-06-25",
		PosterPath: "/63N9uy8nd9j7Eog2axPQ8lbr3Wj.jpg",
		Overview:   "In the smog-choked dystopian Los Angeles of 2019, blade runner Rick Deckard is called out of retirement to terminate a quartet of replicants who have escaped to Earth.",
		videos: []video{
			{Key: "aaaaaaaaaaa", Site: "YouTube", Type: "Featurette", ISO639_1: "en", Official: true, Name: "On set with Ridley Scott"},
			{Key: "bbbbbbbbbbb", Site: "YouTube", Type: "Trailer", ISO639_1: "de", Official: false, Name: "Deutscher Trailer"},
			{Key: "eogpIG53Cis", Site: "YouTube", Type: "Trailer", ISO639_1: "en", Official: true, Name: "Theatrical Trailer"},
		},
	},
	{
		ID: 335984, Title: "Blade Runner 2049", ReleaseDate: "2017-10-04",
		PosterPath: "/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg",
		Overview:   "Thirty years after the events of the first film, a new blade runner unearths a long-buried secret that has the potential to plunge what's left of society into chaos.",
		videos: []video{
			{Key: "gCcx85zbxz4", Site: "YouTube", Type: "Trailer", ISO639_1: "en", Official: true, Name: "Trailer"},
		},
	},
	{
		ID: 11004, Title: "Suspiria", ReleaseDate: "1977-02-01",
		PosterPath: "/7mZMOxgzbG9qlMzbJdWLkDYAusk.jpg",
		Overview:   "An American newcomer to a prestigious German ballet academy comes to realize that the school is a front for something sinister amid a series of grisly murders.",
		videos: []video{
			// A teaser and nothing else: a trailer key must still come out,
			// because a teaser beats no video at all.
			{Key: "ccccccccccc", Site: "YouTube", Type: "Teaser", ISO639_1: "en", Official: true, Name: "Teaser"},
		},
	},
	{
		ID: 4977, Title: "Hausu", ReleaseDate: "1977-08-30",
		PosterPath: "/tNhKcJnBnbcM0qCXWDH1pUYo6Ry.jpg",
		Overview:   "A schoolgirl and six of her classmates travel to her ailing aunt's country home, where the house begins to devour them one by one.",
		// No videos at all: the form must come back with an empty trailer box
		// rather than a broken link.
	},
	{
		// No poster, no release date. The long tail, and the reason
		// MovieCard.Year is nullable and PosterURL can be empty.
		ID: 999001, Title: "An Undocumented Regional Horror",
		Overview: "",
	},
	{
		// Renders as text, or the escaping is broken. Worth being able to see
		// rather than only to assert.
		ID: 999002, Title: `<script>alert("xss")</script> The Movie`,
		ReleaseDate: "1999-01-01",
		Overview:    `Overview with <b>markup</b> & an ampersand.`,
	},
}

func main() {
	addr := flag.String("addr", ":9330", "address to listen on")
	delay := flag.Duration("delay", 0, "artificial latency per request, for watching the debounce work")
	fail := flag.Bool("fail", false, "answer every request with 503, to see the degraded path")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", handler(*delay, *fail, searchMovie))
	mux.HandleFunc("/movie/", handler(*delay, *fail, movieDetail))

	log.Printf("mocktmdbd listening on %s (%d films, fail=%v, delay=%s)",
		*addr, len(catalogue), *fail, *delay)
	log.Printf("point the app at it:  TMDB_API_KEY=anything TMDB_BASE_URL=http://localhost%s", *addr)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

// handler wraps a route with the two switches that make the awkward states
// reachable from a browser: latency, and total failure.
func handler(delay time.Duration, fail bool, next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())

		if delay > 0 {
			time.Sleep(delay)
		}
		if fail {
			// What a TMDB outage looks like from the application's side.
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		next(w, r)
	}
}

// searchMovie is /search/movie?query=...
//
// The matching is a substring fold, which is NOT what TMDB does -- theirs is a
// real fuzzy search. That difference is deliberate and is the one thing to
// remember about this process: it is here to exercise the plumbing, not to
// stand in for TMDB's matching. Do not tune it, and do not let a test depend
// on its ranking.
func searchMovie(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))

	results := make([]film, 0, len(catalogue))
	if q != "" {
		for _, f := range catalogue {
			if strings.Contains(strings.ToLower(f.Title), q) {
				results = append(results, f)
			}
		}
	}

	writeJSON(w, map[string]any{
		"page":          1,
		"results":       results,
		"total_pages":   1,
		"total_results": len(results),
	})
}

// movieDetail is /movie/{id}, honouring append_to_response=videos the way TMDB
// does -- the videos are attached only when they are asked for.
func movieDetail(w http.ResponseWriter, r *http.Request) {
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/movie/"), "/")

	id, err := strconv.Atoi(raw)
	if err != nil {
		notFound(w)

		return
	}

	for _, f := range catalogue {
		if f.ID != id {
			continue
		}

		body := map[string]any{
			"id":           f.ID,
			"title":        f.Title,
			"overview":     f.Overview,
			"poster_path":  emptyAsNull(f.PosterPath),
			"release_date": f.ReleaseDate,
		}
		if strings.Contains(r.URL.Query().Get("append_to_response"), "videos") {
			body["videos"] = map[string]any{"results": f.videos}
		}

		writeJSON(w, body)

		return
	}

	notFound(w)
}

// notFound is TMDB's own 404 body, status code 34 and all. The application
// turns it into tmdb.ErrNotFound, and getting the shape right is the only way
// to see that happen.
func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":        false,
		"status_code":    34,
		"status_message": "The resource you requested could not be found.",
	})
}

// emptyAsNull renders a missing poster_path as JSON null rather than "", which
// is what TMDB does and what the decoder should be seeing.
func emptyAsNull(s string) any {
	if s == "" {
		return nil
	}

	return s
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The response has already begun, so there is nothing better to do
		// than say so in the log the developer is watching.
		log.Printf("encode: %v", err)
	}
}
