package board

import (
	"net/url"
	"strconv"
	"strings"
)

// Trailer link normalisation: ticket E5 (nap-0z8), ported from
// archive/2025/lib/trailer.ts.
//
// Two jobs, and keeping them apart is the whole design:
//
//   - TrailerEmbed turns a link somebody pasted into an iframe src. It is
//     called at RENDER time, from movieCard, and never at write time. What is
//     in movie.trailer_url is what the person typed, character for character,
//     so that a future change of embed host (or of mind) re-renders every card
//     correctly instead of needing a migration over rows that were rewritten
//     on the way in. 2025 did the opposite — its POST handler stored
//     toEmbed(url) and the original was gone — and that is why the archive has
//     rows whose trailer link is an /embed/ URL nobody ever typed.
//
//   - Embeddable is the same question asked at submit time, so that a typo is
//     caught by the person who made it rather than discovered on movie night.
//
// Both are exported: internal/web/admin's movie editor is the second caller,
// exactly as 2025's admin/edit-movie route was the second caller of toEmbed.
//
// What this does NOT do is check that the video exists. A link to a video that
// has been taken down, made private or geo-blocked is a well-formed YouTube
// URL and passes everything here. The fix for that is nap-eie (TMDB search),
// where the trailer comes back from TMDB and nobody types a URL at all.

// embedHost is where a YouTube embed is served from.
//
// youtube-nocookie.com rather than youtube.com: the slate is a public page
// that thirty people open on their phones, and the -nocookie host does not set
// tracking cookies until the viewer actually presses play. It serves the same
// player from the same /embed/<id> path, so nothing else here changes. The
// view-model fixtures already assume this host, which is where it came from.
const embedHost = "https://www.youtube-nocookie.com"

// vimeoPlayerHost is Vimeo's equivalent.
const vimeoPlayerHost = "https://player.vimeo.com"

// youTubeIDLen is how long a YouTube video id is. It has been eleven
// characters for the whole life of the site, and checking it is what turns a
// mistyped link into a message on the form instead of a dead player on the
// board — which is the entire point of doing this at submit time.
const youTubeIDLen = 11

// Embeddable reports whether a trailer link is one the board can play.
//
// It is what the submit form validates against. "Not embeddable" and "not a
// video link" are the same answer here on purpose: every host this recognises
// has an embed form, so a recognised host with no video id in it — a channel
// page, a playlist, a search result — is a link that would sit on a card doing
// nothing, and the person who pasted it is the only one who can fix it.
func Embeddable(raw string) bool {
	return TrailerEmbed(raw) != ""
}

// TrailerEmbed returns an iframe src for a trailer link, or "" when the link
// is not one that can be played in place.
//
// "" is a supported answer rather than a failure: the card template treats a
// link with no embed as a real case and renders the link inside the media box.
// Rows written before submit-time validation existed are exactly that case,
// and they still have to render.
func TrailerEmbed(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	// One canonical host to switch on. m. and www. are the same site, and
	// stripping them here is what makes a link copied off a phone behave like
	// one copied off a desktop.
	host = strings.TrimPrefix(strings.TrimPrefix(host, "www."), "m.")

	switch {
	case host == "youtu.be", host == "youtube.com", host == "youtube-nocookie.com",
		strings.HasSuffix(host, ".youtube.com"):
		return youTubeEmbed(u, host)
	case host == "vimeo.com", host == "player.vimeo.com", strings.HasSuffix(host, ".vimeo.com"):
		return vimeoEmbed(u)
	default:
		return ""
	}
}

// youTubeEmbed handles the four shapes a YouTube link arrives in.
//
// A watch URL is what the share button gives you on a desktop; youtu.be is
// what it gives you on a phone; /embed/ is what somebody pastes when they have
// already copied it out of an iframe; /shorts/ and /live/ are what the app
// gives you and are ordinary videos underneath. All five end at the same
// /embed/<id>.
func youTubeEmbed(u *url.URL, host string) string {
	var id string

	segs := pathSegments(u.Path)
	switch {
	case host == "youtu.be":
		// youtu.be/<id>, and nothing else is a video.
		if len(segs) > 0 {
			id = segs[0]
		}
	case len(segs) > 0 && segs[0] == "watch":
		// The ?v= that every other parameter hangs off. A &list= alongside it
		// is ignored rather than refused: somebody sharing one film out of a
		// playlist means the film.
		id = u.Query().Get("v")
	case len(segs) > 1 && (segs[0] == "embed" || segs[0] == "shorts" || segs[0] == "live" || segs[0] == "v"):
		id = segs[1]
	}

	if !validYouTubeID(id) {
		return ""
	}

	embed := embedHost + "/embed/" + id
	// start= is the embed player's parameter; t= is the watch page's. A link
	// shared from partway through ("watch this bit") keeps its offset, which
	// is the one query parameter worth carrying over.
	if start := startSeconds(u.Query().Get("t"), u.Query().Get("start")); start > 0 {
		embed += "?start=" + strconv.Itoa(start)
	}

	return embed
}

// vimeoEmbed handles vimeo.com/<id>, the channel and showcase paths that end
// in the same id, and player.vimeo.com/video/<id>.
//
// The unlisted-video hash is carried across. A Vimeo link of the form
// vimeo.com/<id>/<hash> is how a trailer that is not publicly listed is
// shared, and an embed without the hash is a player that refuses to play —
// which would look exactly like a broken embed rather than like a missing
// permission.
func vimeoEmbed(u *url.URL) string {
	segs := pathSegments(u.Path)

	id, hash := "", ""
	for i, seg := range segs {
		if isDigits(seg) {
			id = seg
			if i+1 < len(segs) && isVimeoHash(segs[i+1]) {
				hash = segs[i+1]
			}

			break
		}
	}
	if id == "" {
		return ""
	}

	embed := vimeoPlayerHost + "/video/" + id
	if hash != "" {
		embed += "?h=" + hash
	}
	// Vimeo's player takes its offset in the fragment rather than the query,
	// so this cannot be folded into the branch above.
	if start := startSeconds(u.Query().Get("t"), u.Fragment); start > 0 {
		embed += "#t=" + strconv.Itoa(start) + "s"
	}

	return embed
}

// startSeconds reads a start offset out of the first of the given values that
// carries one, in seconds. Zero means "from the beginning", which includes
// every value it could not make sense of: an offset nobody asked for is a
// worse outcome than losing one somebody did.
//
// YouTube writes these four ways — 90, 90s, 1m30s, 1h2m3s — and Vimeo writes
// the fragment "t=1m30s", so the leading "t=" is tolerated too.
func startSeconds(values ...string) int {
	for _, v := range values {
		v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "t=")
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			if n > 0 {
				return n
			}

			continue
		}
		if n := hmsSeconds(v); n > 0 {
			return n
		}
	}

	return 0
}

// hmsSeconds parses "1h2m3s", "1m30s" or "90s". It returns 0 on anything it
// does not fully understand rather than on a best effort: half a parsed
// timestamp is an offset into the wrong part of the film.
func hmsSeconds(v string) int {
	total, digits := 0, ""
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			digits += string(r)
		case r == 'h' || r == 'm' || r == 's':
			if digits == "" {
				return 0
			}
			n, err := strconv.Atoi(digits)
			if err != nil {
				return 0
			}
			switch r {
			case 'h':
				total += n * 3600
			case 'm':
				total += n * 60
			case 's':
				total += n
			}
			digits = ""
		default:
			return 0
		}
	}
	// Trailing digits with no unit, after at least one unit, is not a shape
	// either player emits.
	if digits != "" {
		return 0
	}

	return total
}

// pathSegments splits a URL path into its non-empty segments.
func pathSegments(p string) []string {
	segs := make([]string, 0, 4)
	for _, seg := range strings.Split(p, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}

	return segs
}

// validYouTubeID reports whether a string is the right shape for a video id.
//
// Eleven characters of the URL-safe alphabet. This is the check that catches a
// truncated paste and a link to a channel that happened to land in the id
// slot, and it is the reason a typed link gets a message on the form rather
// than a player that says "Video unavailable" on movie night.
func validYouTubeID(id string) bool {
	if len(id) != youTubeIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}

	return true
}

// isDigits reports whether s is a non-empty run of ASCII digits, which is what
// a Vimeo video id is.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// isVimeoHash reports whether s looks like the unlisted-video token that
// follows an id: lower-case hex, nothing else.
func isVimeoHash(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}

	return true
}
