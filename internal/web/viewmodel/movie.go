package viewmodel

import "strconv"

// MovieCard is one submission as the slate renders it.
//
// It is the movie row with every database concern already resolved: the
// nullable year is an int with a zero sentinel, the nullable trailer URL is a
// string with an empty sentinel, and the submitter is a name rather than a
// uuid the template would have to resolve.
type MovieCard struct {
	// ID is movie.id as a plain string. It is the value a ballot form posts
	// and the fragment a "link to this film" anchor uses. It is a string
	// rather than a uuid.UUID because a template only ever prints it, and
	// because a uuid type in here would be the first crack in the rule that
	// nothing store-shaped crosses this seam.
	ID string

	// Title is movie.title, which the schema guarantees is not blank. It can
	// be very long: nothing caps it, and people submit real films with
	// ridiculous names. See FixtureLongTitleMovie.
	Title string

	// Year is the film's release year, not the season year. Zero means
	// unknown — movie.year is nullable, because TMDB does not always have one.
	// Zero is a safe sentinel here because movie_year_range already forbids
	// anything outside 1888..2200.
	Year int

	// Description is movie.description. It is never null but is very often
	// empty, because the submit form does not require it.
	Description string

	// TrailerURL is the link as submitted. Empty means no trailer. The schema
	// guarantees a non-empty one starts with http:// or https://, so it is
	// safe to render as an anchor without re-checking the scheme.
	TrailerURL string

	// TrailerEmbedURL is an iframe-safe src for the same trailer, or "" when
	// the link cannot be embedded.
	//
	// It is a separate field rather than something derived from TrailerURL
	// because most trailer links cannot be embedded at all — a Vimeo page, an
	// IMDb page or a bare studio URL has no embed form, and a YouTube watch
	// URL has to be rewritten into one. Deciding which is which is a handler's
	// job. A card can therefore have a link and no embed, which is the common
	// case, and the template shows the link instead of an empty player.
	TrailerEmbedURL string

	// SubmittedBy is the submitter's display name. It may be blank:
	// person.display_name defaults to '' and no constraint forbids it, so a
	// whitelisted person who has never signed in has no name at all. Render
	// SubmitterLabel rather than this field.
	SubmittedBy string

	// Hidden is movie.hidden, the soft delete. A hidden movie is off the
	// slate, does not count against its submitter's quota and is not on the
	// ballot — but the ballots that ranked it survive, which is why it is a
	// flag and not a missing row.
	//
	// It is on the card at all so that an admin view can show hidden
	// submissions struck through rather than silently dropping them. A public
	// slate simply never receives one.
	Hidden bool

	// IsWinner marks the film that won a locked season, for the badge on its
	// card. The handler sets it; there is no id comparison for a template to
	// get wrong.
	IsWinner bool

	// EditHref is where the submitter goes to change or withdraw this film
	// (ticket E3). Blank renders no control, which is the case on every card
	// the public slate shows and on every card belonging to somebody else.
	//
	// It is a handler's answer, not a template's: "is this mine, and is the
	// window open" is decided once, from the season row and the session, and
	// arrives here as a link or as nothing. A template that worked it out from
	// an id comparison would be a second implementation of an authorisation
	// rule, and the server check would be the only one of the two anybody
	// tested.
	//
	// The control it renders is a LINK to a page, deliberately, and not a
	// withdraw button on the card itself. Withdrawing is a write and wants a
	// POST; a POST button sitting in a list of cards is one mis-tap away from
	// taking a film off the board, on the page where the films are closest
	// together.
	EditHref string
}

// HasOwnerControls reports whether this card offers its submitter a way to
// change it.
func (m MovieCard) HasOwnerControls() bool {
	return m.EditHref != ""
}

// HasYear reports whether the release year is known.
func (m MovieCard) HasYear() bool {
	return m.Year > 0
}

// YearLabel is the release year for display, or "" when it is unknown.
func (m MovieCard) YearLabel() string {
	if !m.HasYear() {
		return ""
	}

	return strconv.Itoa(m.Year)
}

// TitleLine is the card's heading: "The Thing (1982)", or just the title when
// the year is unknown.
func (m MovieCard) TitleLine() string {
	if !m.HasYear() {
		return m.Title
	}

	return m.Title + " (" + m.YearLabel() + ")"
}

// HasTrailer reports whether there is a trailer link to offer at all.
func (m MovieCard) HasTrailer() bool {
	return m.TrailerURL != ""
}

// CanEmbedTrailer reports whether the trailer can be played in place. When it
// is false but HasTrailer is true, show the link.
func (m MovieCard) CanEmbedTrailer() bool {
	return m.TrailerEmbedURL != ""
}

// SubmitterLabel is who to credit, with a fallback for the blank display name
// a never-signed-in whitelist entry has. It is a fallback rather than an
// omission because "submitted by" with nothing after it reads like a bug.
func (m MovieCard) SubmitterLabel() string {
	if m.SubmittedBy == "" {
		return "someone in the group"
	}

	return m.SubmittedBy
}

// HasDescription reports whether there is anything to print in the card body.
func (m MovieCard) HasDescription() bool {
	return m.Description != ""
}
