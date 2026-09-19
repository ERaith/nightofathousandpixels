package viewmodel

import "time"

// Sample data for every struct in this package.
//
// These are exported, and they live in the package rather than in a _test.go
// file, because their job is to let a frontend ticket render real pages with
// no database, no handlers and no backend running at all. A template author
// builds against FixtureFullSlate, sees the real thing, and the handler that
// eventually fills the same struct changes nothing about the markup.
//
// They are functions rather than package-level variables on purpose. Every one
// returns a fresh value, so a test that appends to a fixture's slice or edits
// a field cannot poison the next test that asks for it.
//
// The cases here are the ones that actually break layouts, not a pretty demo:
// an empty slate, a viewer at their limit, a viewer barred from submitting, a
// locked season, a title long enough to overflow a card, a name with an
// apostrophe and a non-ASCII character, a film with no trailer, a film with a
// trailer that cannot be embedded, a film with no release year, a submitter
// with no display name at all, and a form carrying per-field errors. If a page
// survives all of them it will survive October.

// fixtureYear is the season every fixture belongs to.
const fixtureYear = 2026

// fixtureAt builds a timestamp in the fixture season. UTC keeps the rendered
// strings identical on every machine, which matters because a golden-file test
// would otherwise pass in one timezone and fail in another.
func fixtureAt(month time.Month, day, hour, minute int) time.Time {
	return time.Date(fixtureYear, month, day, hour, minute, 0, 0, time.UTC)
}

// FixtureTheme is a theme pack's worth of presentation values, including the
// share image, so the <head> renders with every optional tag present.
func FixtureTheme() Theme {
	return Theme{
		PackCSS:    "/static/themes/vhs/pack.css",
		SiteTitle:  DefaultSiteTitle,
		Tagline:    DefaultTagline,
		OGImage:    "/static/themes/vhs/og-2026.png",
		OGImageAlt: "A stack of worn VHS tapes lit by a television.",
	}
}

// FixtureNav is the header navigation as it looks once every page exists.
func FixtureNav() []NavItem {
	return []NavItem{
		{Label: "Home", Href: "/"},
		{Label: "The slate", Href: "/2026"},
		{Label: "Submit", Href: "/2026/submit"},
		{Label: "Past years", Href: "/archive"},
	}
}

// FixtureUser is an ordinary signed-in member.
func FixtureUser() *CurrentUser {
	return &CurrentUser{
		DisplayName: "Marcus Webb",
		Email:       "marcus.webb@example.com",
	}
}

// FixtureAdminUser is a member who runs this season. IsAdmin is per season,
// so this is admin of 2026 and nothing else.
func FixtureAdminUser() *CurrentUser {
	return &CurrentUser{
		DisplayName: "Siobhán O'Doherty",
		Email:       "siobhan.odoherty@example.com",
		IsAdmin:     true,
	}
}

// FixtureNamelessUser is a whitelisted person who has signed in but whose
// provider gave us no display name — person.display_name defaults to the empty
// string and no constraint forbids it. Anything that greets them by name has
// to cope.
func FixtureNamelessUser() *CurrentUser {
	return &CurrentUser{Email: "new.member@example.com"}
}

// FixtureLayout is the shell for a signed-in viewer, with a flash of each
// kind so the notice styles are all on screen at once.
func FixtureLayout() LayoutData {
	return LayoutData{
		Theme:       FixtureTheme(),
		Title:       "The slate",
		Path:        "/2026",
		Origin:      "https://pixels.example.com",
		Nav:         FixtureNav(),
		SeasonYear:  fixtureYear,
		CurrentUser: FixtureUser(),
		SignOutHref: "/sign-out",
		Flashes: []Flash{
			{Level: FlashSuccess, Message: "The Thing is on the board."},
			{Level: FlashInfo, Message: "Ranking opens on 18 October."},
		},
	}
}

// FixtureLayoutSignedOut is the shell as a visitor who has not signed in sees
// it: no user, a sign-in link instead of a sign-out one, and no flashes.
func FixtureLayoutSignedOut() LayoutData {
	return LayoutData{
		Theme:      FixtureTheme(),
		Title:      "The slate",
		Path:       "/2026",
		Origin:     "https://pixels.example.com",
		Nav:        FixtureNav(),
		SeasonYear: fixtureYear,
		SignInHref: "/sign-in",
	}
}

// FixtureLayoutBare is the worst case for the shell: the zero Theme, no
// origin, no nav, no user and no season. Every optional element is absent at
// once, which is what an error page rendered before anything is configured
// actually looks like.
func FixtureLayoutBare() LayoutData {
	return LayoutData{Title: "That page isn't here"}
}

// FixtureLayoutNoisy is the shell carrying a warning and an error together,
// with a long message, for checking that the notice region does not wreck the
// page rhythm.
func FixtureLayoutNoisy() LayoutData {
	layout := FixtureLayout()
	layout.Flashes = []Flash{
		{Level: FlashWarning, Message: "Submissions close in under an hour."},
		{
			Level: FlashError,
			Message: "That film is already on the board — somebody else submitted it " +
				"about ten minutes ago, so pick something else rather than splitting the vote.",
		},
	}

	return layout
}

// FixtureLayouts is every shell fixture by name, for a test or a gallery that
// wants to render all of them without knowing what they are.
func FixtureLayouts() map[string]LayoutData {
	return map[string]LayoutData{
		"signed in":  FixtureLayout(),
		"signed out": FixtureLayoutSignedOut(),
		"bare":       FixtureLayoutBare(),
		"noisy":      FixtureLayoutNoisy(),
	}
}

// FixtureSeason is a season in its submission window, fully scheduled.
func FixtureSeason() SeasonSummary {
	return SeasonSummary{
		Name:          "The 2026 Season",
		Year:          fixtureYear,
		State:         SeasonSubmitting,
		SubmitOpensAt: fixtureAt(time.October, 1, 9, 0),
		VoteOpensAt:   fixtureAt(time.October, 18, 9, 0),
		VoteClosesAt:  fixtureAt(time.October, 28, 21, 0),
	}
}

// FixtureSeasonVoting is the same season once the board is closed and the
// ballots are open.
func FixtureSeasonVoting() SeasonSummary {
	season := FixtureSeason()
	season.State = SeasonVoting

	return season
}

// FixtureSeasonUnscheduled is a season that exists but whose dates have not
// been decided — every window column is NULL, which migration 00002 allows on
// purpose. Every deadline line on the page has to disappear rather than
// printing a label with nothing after it.
func FixtureSeasonUnscheduled() SeasonSummary {
	return SeasonSummary{
		Name:  "The 2027 Season",
		Year:  fixtureYear + 1,
		State: SeasonDraft,
	}
}

// FixtureSeasonLocked is a finished year: the archive.
func FixtureSeasonLocked() SeasonSummary {
	return SeasonSummary{
		Name:          "The 2025 Season",
		Year:          fixtureYear - 1,
		State:         SeasonLocked,
		SubmitOpensAt: time.Date(2025, time.October, 2, 9, 0, 0, 0, time.UTC),
		VoteOpensAt:   time.Date(2025, time.October, 19, 9, 0, 0, 0, time.UTC),
		VoteClosesAt:  time.Date(2025, time.October, 29, 21, 0, 0, 0, time.UTC),
		LockedAt:      time.Date(2025, time.October, 30, 8, 30, 0, 0, time.UTC),
	}
}

// FixtureMovie is an unremarkable card with everything filled in: year,
// description, and a trailer that plays in place.
func FixtureMovie() MovieCard {
	return MovieCard{
		ID:              "6e6b1a2c-6f4a-4f6d-9d4e-2b1f0a5c7d31",
		Title:           "The Thing",
		Year:            1982,
		Description:     "Twelve men in Antarctica and no way to tell which of them is still a man. The practical effects have not aged a day.",
		TrailerURL:      "https://www.youtube.com/watch?v=4sDyy2Ndm5k",
		TrailerEmbedURL: "https://www.youtube-nocookie.com/embed/4sDyy2Ndm5k",
		SubmittedBy:     "Marcus Webb",
	}
}

// FixtureApostropheMovie has an apostrophe in the title and an apostrophe plus
// a non-ASCII character in the submitter's name. Both are ordinary data and
// both must survive escaping without turning into &#39; on screen.
func FixtureApostropheMovie() MovieCard {
	return MovieCard{
		ID:              "1c7d9f3b-8a52-4d1e-b0c6-9e2a4f8b6d07",
		Title:           "Rosemary's Baby",
		Year:            1968,
		Description:     "A young couple move into a beautiful old building and everybody in it is extremely helpful. That is the problem.",
		TrailerURL:      "https://www.youtube.com/watch?v=Ni2DxavAe8g",
		TrailerEmbedURL: "https://www.youtube-nocookie.com/embed/Ni2DxavAe8g",
		SubmittedBy:     "Siobhán O'Doherty",
	}
}

// FixtureNoTrailerMovie has no trailer link and no description: the minimum a
// movie row can carry, since only title is required. The card has to look
// deliberate rather than half-loaded.
func FixtureNoTrailerMovie() MovieCard {
	return MovieCard{
		ID:          "b2f4a8c1-3d5e-4a7b-8c9d-0e1f2a3b4c5d",
		Title:       "Nosferatu",
		Year:        1922,
		SubmittedBy: "Priya Raman",
	}
}

// FixtureUnembeddableTrailerMovie has a trailer link that cannot go in an
// iframe. This is the common case, not the exotic one — anything that is not a
// YouTube watch URL ends up here — so the card must offer the link rather than
// an empty player.
func FixtureUnembeddableTrailerMovie() MovieCard {
	return MovieCard{
		ID:          "9a8b7c6d-5e4f-4a3b-2c1d-0f9e8d7c6b5a",
		Title:       "Hereditary",
		Year:        2018,
		Description: "A family grieves. Do not read anything else about it before Friday.",
		TrailerURL:  "https://vimeo.com/266967456",
		SubmittedBy: "Dani Okonkwo",
	}
}

// FixtureLongTitleMovie is the layout-breaker, and the title is real: a 1991
// film that holds the record more or less on purpose. It also has no release
// year — movie.year is nullable because TMDB does not always have one — and no
// submitter name, because a whitelisted person who has just signed in may have
// a blank display_name. Three edge cases on one card, which is roughly how
// they arrive in practice.
func FixtureLongTitleMovie() MovieCard {
	return MovieCard{
		ID: "4d3c2b1a-9f8e-4d7c-6b5a-4f3e2d1c0b9a",
		Title: "Night of the Day of the Dawn of the Son of the Bride of the Return of the " +
			"Revenge of the Terror of the Attack of the Evil, Mutant, Alien, Flesh Eating, " +
			"Hellbound, Zombified Living Dead Part 2: In Shocking 2-D",
		Description:     "Somebody redubbed a public-domain zombie film and it is genuinely the funniest thing on the list.",
		TrailerURL:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		TrailerEmbedURL: "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
	}
}

// FixtureHiddenMovie is a soft-deleted submission. It never appears on a
// public slate — ListVisibleMoviesForSeason filters it out — so it exists here
// only for the admin view, where it must be visibly withdrawn rather than
// looking like an ordinary card.
func FixtureHiddenMovie() MovieCard {
	return MovieCard{
		ID:          "0f1e2d3c-4b5a-4968-8776-655443322110",
		Title:       "The Wicker Man",
		Year:        2006,
		Description: "Withdrawn: submitted as a joke, and then somebody pointed out we would have to watch it.",
		SubmittedBy: "Marcus Webb",
		Hidden:      true,
	}
}

// FixtureMovies is a full slate: six cards covering every shape a card comes
// in. No hidden entries, because a public slate never receives one.
func FixtureMovies() []MovieCard {
	return []MovieCard{
		FixtureMovie(),
		FixtureApostropheMovie(),
		FixtureNoTrailerMovie(),
		FixtureUnembeddableTrailerMovie(),
		FixtureLongTitleMovie(),
		{
			ID:              "aa11bb22-cc33-4d44-8e55-ff6677889900",
			Title:           "Häxan",
			Year:            1922,
			Description:     "A silent Swedish documentary about witchcraft that turns into something else about twenty minutes in.",
			TrailerURL:      "https://www.youtube.com/watch?v=T8HKUiFcW5Q",
			TrailerEmbedURL: "https://www.youtube-nocookie.com/embed/T8HKUiFcW5Q",
			SubmittedBy:     "Priya Raman",
		},
	}
}

// FixtureQuota is an ordinary member partway through: one pick used, one left.
func FixtureQuota() SubmitQuota {
	return SubmitQuota{Known: true, Used: 1, Limit: 2}
}

// FixtureQuotaAtLimit is a member who has used both picks. The submit control
// goes away and the reason is stated.
func FixtureQuotaAtLimit() SubmitQuota {
	return SubmitQuota{Known: true, Used: 2, Limit: 2}
}

// FixtureQuotaBarred is the case this whole type exists for: a member whose
// effective submit limit is an explicit 0. They may vote and may not submit.
//
// Known is true. The difference between this and FixtureQuotaUnknown is not
// visible in Limit — both have Limit 0 — which is exactly why a bare int would
// not do.
func FixtureQuotaBarred() SubmitQuota {
	return SubmitQuota{Known: true, Used: 0, Limit: 0}
}

// FixtureQuotaUnknown is a viewer with no quota in this season at all: signed
// out, or signed in but not on this year's whitelist. It is the zero value, on
// purpose — an unpopulated quota reads as "no allowance" rather than as "may
// submit".
func FixtureQuotaUnknown() SubmitQuota {
	return SubmitQuota{}
}

// FixtureQuotaOverLimit is a member whose limit was lowered after they had
// already submitted. Used exceeds Limit, which the schema permits and an admin
// editing a whitelist can cause. Remaining has to clamp at zero.
func FixtureQuotaOverLimit() SubmitQuota {
	return SubmitQuota{Known: true, Used: 3, Limit: 2}
}

// FixtureFullSlate is the ordinary slate: a season in its submission window,
// six films up, a signed-in member with one pick left.
func FixtureFullSlate() SlatePage {
	return SlatePage{
		Layout:     FixtureLayout(),
		Season:     FixtureSeason(),
		Movies:     FixtureMovies(),
		Quota:      FixtureQuota(),
		CanSubmit:  true,
		SubmitHref: "/2026/submit",
	}
}

// FixtureEmptySlate is the morning of 1 October: the season has just opened
// and nobody has submitted anything.
//
// This is the most-seen state of the whole site and the easiest one to ship
// broken, because it is the one nobody builds against. It has to read as an
// invitation — the window is open, here is the button — rather than as a page
// that failed to load.
func FixtureEmptySlate() SlatePage {
	layout := FixtureLayout()
	layout.Flashes = nil

	return SlatePage{
		Layout:     layout,
		Season:     FixtureSeason(),
		Quota:      SubmitQuota{Known: true, Used: 0, Limit: 2},
		CanSubmit:  true,
		SubmitHref: "/2026/submit",
	}
}

// FixtureSlateSignedOut is the slate as a visitor who has not signed in sees
// it: the films are public, the controls are not.
func FixtureSlateSignedOut() SlatePage {
	return SlatePage{
		Layout: FixtureLayoutSignedOut(),
		Season: FixtureSeason(),
		Movies: FixtureMovies(),
		Quota:  FixtureQuotaUnknown(),
	}
}

// FixtureSlateAtLimit is a member who has used both picks. The films stay, the
// submit control goes, and Quota.Label explains why.
func FixtureSlateAtLimit() SlatePage {
	page := FixtureFullSlate()
	page.Layout.Flashes = []Flash{
		{Level: FlashWarning, Message: "That would be your third pick, and you get two."},
	}
	page.Quota = FixtureQuotaAtLimit()
	page.CanSubmit = false

	return page
}

// FixtureSlateBarred is a member with an explicit limit of 0: they may rank
// every film on this page and may not add one. The copy has to distinguish
// that from having run out of picks, because one is a setting somebody chose
// and the other is progress the viewer made.
func FixtureSlateBarred() SlatePage {
	page := FixtureFullSlate()
	page.Layout.Flashes = nil
	page.Quota = FixtureQuotaBarred()
	page.CanSubmit = false

	return page
}

// FixtureLockedSlate is a finished season showing its result. Nothing on it is
// actionable: migration 00008 makes a locked season immutable in the database,
// and the page should not offer what the database will refuse.
func FixtureLockedSlate() SlatePage {
	movies := FixtureMovies()
	movies[0].IsWinner = true

	layout := FixtureLayout()
	layout.Title = "The 2025 Season"
	layout.Path = "/2025"
	layout.SeasonYear = fixtureYear - 1
	layout.Flashes = nil

	return SlatePage{
		Layout: layout,
		Season: FixtureSeasonLocked(),
		Movies: movies,
		Quota:  FixtureQuotaUnknown(),
		Result: &SeasonResult{
			WinnerTitle: movies[0].Title,
			TalliedAt:   time.Date(2025, time.October, 30, 8, 30, 0, 0, time.UTC),
		},
	}
}

// FixtureLockedSlateTied is a finished season where the final round tied, so
// result.winner_movie_id is NULL. There is no winning card to badge.
func FixtureLockedSlateTied() SlatePage {
	page := FixtureLockedSlate()
	for i := range page.Movies {
		page.Movies[i].IsWinner = false
	}
	page.Result = &SeasonResult{
		Tied:      true,
		TalliedAt: time.Date(2025, time.October, 30, 8, 30, 0, 0, time.UTC),
	}

	return page
}

// FixtureAdminSlate is the admin view: the same slate with the withdrawn
// submissions visible and flagged as such.
func FixtureAdminSlate() SlatePage {
	page := FixtureFullSlate()
	page.Layout.CurrentUser = FixtureAdminUser()
	page.Movies = append(page.Movies, FixtureHiddenMovie())
	page.ShowingHidden = true

	return page
}

// FixtureSubmit is a blank submit form for a member with a pick left.
func FixtureSubmit() SubmitPage {
	layout := FixtureLayout()
	layout.Title = "Submit a movie"
	layout.Path = "/2026/submit"
	layout.Flashes = nil

	return SubmitPage{
		Layout:     layout,
		Season:     FixtureSeason(),
		Quota:      FixtureQuota(),
		Existing:   []MovieCard{FixtureMovie()},
		CanSubmit:  true,
		Action:     "/2026/submit",
		CancelHref: "/2026",
	}
}

// FixtureSubmitWithErrors is a rejected submission coming back.
//
// Every field still holds the text the person typed, including a year that is
// not a number — which is the reason SubmitForm.Year is a string. If it were
// an int, "nineteen eighty four" would have nowhere to go and the box would
// come back empty, which looks like the site threw the whole form away.
func FixtureSubmitWithErrors() SubmitPage {
	page := FixtureSubmit()
	page.Layout.Flashes = []Flash{
		{Level: FlashError, Message: "That did not go up. Two things need fixing."},
	}
	page.Form = SubmitForm{
		Title:       "",
		Year:        "nineteen eighty four",
		TrailerURL:  "youtube.com/watch?v=abc",
		Description: "It is the one with the rabbit.",
		Errors: FieldErrors{
			{Field: FieldTitle, Message: "A title is the one thing we cannot guess."},
			{Field: FieldYear, Message: "Use four digits, like 1984 — or leave it blank."},
			{Field: FieldTrailerURL, Message: "Start the link with https:// so it opens."},
		},
	}

	return page
}

// FixtureSubmitAtLimit is the submit page for someone with no picks left. The
// form is absent rather than disabled: a disabled form is something people
// fill in anyway and then get annoyed about.
func FixtureSubmitAtLimit() SubmitPage {
	page := FixtureSubmit()
	page.Quota = FixtureQuotaAtLimit()
	page.Existing = []MovieCard{FixtureMovie(), FixtureApostropheMovie()}
	page.CanSubmit = false

	return page
}

// FixtureSubmitBarred is the submit page for a member whose effective limit is
// an explicit 0. They have submitted nothing and never will this year, so
// "you have used your picks" would be both wrong and confusing.
func FixtureSubmitBarred() SubmitPage {
	page := FixtureSubmit()
	page.Quota = FixtureQuotaBarred()
	page.Existing = nil
	page.CanSubmit = false

	return page
}

// FixtureSubmitClosed is the submit page reached after the window shut. The
// quota is untouched — they still have a pick — and they still cannot submit,
// which is why CanSubmit is a separate decision from Quota.HasRoom.
func FixtureSubmitClosed() SubmitPage {
	page := FixtureSubmit()
	page.Season = FixtureSeasonVoting()
	page.CanSubmit = false
	page.Layout.Flashes = []Flash{
		{Level: FlashWarning, Message: "Submissions closed on 18 October. Ranking is open instead."},
	}

	return page
}

// FixtureSignIn is the sign-in page as somebody reaches it by trying to submit
// while signed out.
func FixtureSignIn() SignInPage {
	layout := FixtureLayoutSignedOut()
	layout.Title = "Sign in"
	layout.Path = "/sign-in"

	return SignInPage{
		Layout:     layout,
		SignInHref: "/auth/google",
		Reason:     "Sign in to put a movie on the board.",
		ReturnTo:   "/2026/submit",
	}
}

// FixtureSignInPlain is the sign-in page reached directly, with no errand to
// explain and nowhere particular to go afterwards.
func FixtureSignInPlain() SignInPage {
	page := FixtureSignIn()
	page.Reason = ""
	page.ReturnTo = ""

	return page
}

// FixtureNotOnWhitelist is a verified Google account that is not in this
// season's season_member table. Almost always the wrong account rather than
// genuine exclusion, which is why the address is shown back first.
func FixtureNotOnWhitelist() NotOnWhitelistPage {
	// This viewer IS signed in — Google verified them — they are simply not on
	// the list, so the shell offers sign-out rather than sign-in. Getting that
	// backwards is the bug this fixture exists to catch: a page telling
	// somebody to sign in when the whole problem is who they signed in as.
	layout := FixtureLayoutSignedOut()
	layout.Title = "Not on this year's list"
	layout.Path = "/not-on-list"
	layout.Nav = nil
	layout.CurrentUser = FixtureNamelessUser()
	layout.SignInHref = ""
	layout.SignOutHref = "/sign-out"

	return NotOnWhitelistPage{
		Layout:       layout,
		Email:        "marcus.webb@work.example.com",
		SeasonName:   "The 2026 Season",
		ContactLabel: "ask in the group chat",
		ContactHref:  "https://chat.example.com/nap",
		SignOutHref:  "/sign-out",
	}
}

// FixtureNotOnWhitelistNoContact is the same page with nowhere to send them.
// The offer has to disappear rather than render a dead link.
func FixtureNotOnWhitelistNoContact() NotOnWhitelistPage {
	page := FixtureNotOnWhitelist()
	page.ContactLabel = ""
	page.ContactHref = ""

	return page
}

// FixtureSlates is every slate fixture by name, for a test or a gallery that
// renders all of them.
func FixtureSlates() map[string]SlatePage {
	return map[string]SlatePage{
		"full":        FixtureFullSlate(),
		"empty":       FixtureEmptySlate(),
		"signed out":  FixtureSlateSignedOut(),
		"at limit":    FixtureSlateAtLimit(),
		"barred":      FixtureSlateBarred(),
		"locked":      FixtureLockedSlate(),
		"locked tied": FixtureLockedSlateTied(),
		"admin":       FixtureAdminSlate(),
	}
}

// FixtureSubmitPages is every submit-page fixture by name.
func FixtureSubmitPages() map[string]SubmitPage {
	return map[string]SubmitPage{
		"blank":       FixtureSubmit(),
		"with errors": FixtureSubmitWithErrors(),
		"at limit":    FixtureSubmitAtLimit(),
		"barred":      FixtureSubmitBarred(),
		"closed":      FixtureSubmitClosed(),
	}
}
