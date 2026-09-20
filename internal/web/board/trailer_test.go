package board

import (
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// TestTrailerEmbedAcceptsTheShapesPeopleActuallyPaste.
//
// The list is the point rather than the coverage number: every entry is a way
// somebody in the group chat has produced a link. The share button on a
// desktop gives a watch URL, the share button on a phone gives youtu.be, the
// app gives /shorts/, and somebody who has done this before pastes an /embed/
// URL straight out of an iframe. All of them mean "play this film's trailer",
// so all of them have to end at the same player.
func TestTrailerEmbedAcceptsTheShapesPeopleActuallyPaste(t *testing.T) {
	t.Parallel()

	const embed = "https://www.youtube-nocookie.com/embed/4sDyy2Ndm5k"

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a watch URL", "https://www.youtube.com/watch?v=4sDyy2Ndm5k", embed},
		{"a watch URL with no www", "https://youtube.com/watch?v=4sDyy2Ndm5k", embed},
		{"a watch URL off a phone", "https://m.youtube.com/watch?v=4sDyy2Ndm5k", embed},
		{"a short link", "https://youtu.be/4sDyy2Ndm5k", embed},
		{"an embed URL somebody copied out of an iframe", "https://www.youtube.com/embed/4sDyy2Ndm5k", embed},
		{"a nocookie embed URL", "https://www.youtube-nocookie.com/embed/4sDyy2Ndm5k", embed},
		{"a short", "https://www.youtube.com/shorts/4sDyy2Ndm5k", embed},
		{"http rather than https", "http://www.youtube.com/watch?v=4sDyy2Ndm5k", embed},
		// A film shared out of a playlist means the film. The &list= is the
		// chat client's or the sharer's, not an instruction.
		{"a watch URL inside a playlist", "https://www.youtube.com/watch?v=4sDyy2Ndm5k&list=PLabc123", embed},
		{"surrounding whitespace", "  https://youtu.be/4sDyy2Ndm5k  ", embed},

		// "Watch this bit" is the one parameter worth carrying over, and the
		// watch page and the player spell it differently.
		{"a start time in seconds", "https://www.youtube.com/watch?v=4sDyy2Ndm5k&t=90", embed + "?start=90"},
		{"a start time with a unit", "https://youtu.be/4sDyy2Ndm5k?t=90s", embed + "?start=90"},
		{"a start time in minutes and seconds", "https://www.youtube.com/watch?v=4sDyy2Ndm5k&t=1m30s", embed + "?start=90"},
		{"a start time in hours", "https://www.youtube.com/watch?v=4sDyy2Ndm5k&t=1h0m5s", embed + "?start=3605"},
		{"an embed URL that already has start", "https://www.youtube.com/embed/4sDyy2Ndm5k?start=90", embed + "?start=90"},

		{"a vimeo link", "https://vimeo.com/266967456", "https://player.vimeo.com/video/266967456"},
		{"a vimeo channel link", "https://vimeo.com/channels/staffpicks/266967456", "https://player.vimeo.com/video/266967456"},
		{"a vimeo player link", "https://player.vimeo.com/video/266967456", "https://player.vimeo.com/video/266967456"},
		// An unlisted trailer without its hash is a player that refuses to
		// play, which on a card looks exactly like a broken embed.
		{"an unlisted vimeo link", "https://vimeo.com/266967456/abc123def4", "https://player.vimeo.com/video/266967456?h=abc123def4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := TrailerEmbed(tc.raw); got != tc.want {
				t.Errorf("TrailerEmbed(%q)\n got %q\nwant %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestTrailerEmbedRefusesWhatIsNotAVideo.
//
// Each of these is a way a link goes wrong quietly. The submit form turns
// every one of them into a message beside the field, which is the difference
// between somebody fixing their own typo in ten seconds and thirty people
// finding a dead player on movie night.
func TestTrailerEmbedRefusesWhatIsNotAVideo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"nothing at all", ""},
		{"not a URL", "the one with the rabbit"},
		{"no scheme", "youtube.com/watch?v=4sDyy2Ndm5k"},
		{"a scheme that is not the web", "javascript:alert(1)"},
		{"a page about the film rather than the trailer", "https://www.imdb.com/title/tt0084787/"},
		{"a channel", "https://www.youtube.com/@sometrailerchannel"},
		{"a playlist", "https://www.youtube.com/playlist?list=PLabc123"},
		{"a search", "https://www.youtube.com/results?search_query=the+thing+trailer"},
		{"a watch URL that lost its id", "https://www.youtube.com/watch"},
		{"a truncated id", "https://youtu.be/4sDyy2Nd"},
		{"an id with a character that is not in one", "https://youtu.be/4sDyy2Ndm5!"},
		// The one that matters most, because it looks right: a hostname that
		// merely contains the real one.
		{"a lookalike host", "https://youtube.com.example.test/watch?v=4sDyy2Ndm5k"},
		{"a vimeo page that is not a video", "https://vimeo.com/channels/staffpicks"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := TrailerEmbed(tc.raw); got != "" {
				t.Errorf("TrailerEmbed(%q) = %q, want %q", tc.raw, got, "")
			}
			if Embeddable(tc.raw) {
				t.Errorf("Embeddable(%q) = true; the submit form would have accepted it", tc.raw)
			}
		})
	}
}

// TestValidateStoresTheLinkAsTyped is the half of E5 that is easy to undo.
//
// 2025 stored toEmbed(url) and threw the original away, which is why its
// archive holds rows whose "trailer link" is an /embed/ URL nobody typed.
// Here the row keeps what the person pasted and the embed is derived on the
// way out, so changing the embed host is a deploy rather than a migration.
func TestValidateStoresTheLinkAsTyped(t *testing.T) {
	t.Parallel()

	const typed = "https://www.youtube.com/watch?v=4sDyy2Ndm5k&t=90"

	d, errs := validate(viewmodel.SubmitForm{Title: "The Thing", TrailerURL: typed})
	if errs.Any() {
		t.Fatalf("rejected a YouTube watch URL: %+v", errs)
	}
	if d.trailerURL == nil || *d.trailerURL != typed {
		t.Fatalf("stored %v, want the link exactly as typed (%q)", d.trailerURL, typed)
	}
}
