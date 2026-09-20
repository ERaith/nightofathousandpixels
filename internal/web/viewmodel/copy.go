package viewmodel

import (
	"strconv"
	"strings"
)

// Copy is a theme pack's wording: the strings a pack is allowed to rewrite
// without anybody touching a template.
//
// It is the fourth thing a pack owns, next to colour, type and assets, and it
// is the one that does most of the work. The 2025 site was ALIEN and the part
// people reacted to was not the green — it was that the button did not say
// "Submit". A pack that can only repaint is a skin; a pack that can also
// choose the words is a theme.
//
// The rules are deliberately narrow:
//
//   - A pack sets the keys it cares about and omits the rest. Every key has a
//     base string in BaseCopy, so an empty Copy renders the whole site and a
//     half-finished pack renders the rest of it. There is no such thing as a
//     missing string.
//   - A key that no longer exists in BaseCopy is inert rather than an error. A
//     pack outlives the template that introduced it: the 2026 pack has to keep
//     rendering in 2029 when somebody reads the archive, and a stale key
//     blowing up that page would be a worse outcome than a stale key being
//     ignored.
//   - A pack never supplies a fact. Dates, counts, names and quotas are data
//     and come from the view model; copy supplies the sentence they sit in,
//     through the {placeholders} below. That is why SubmitQuota.Label is a
//     method here and not a copy key — "You have 1 pick left of 2" is arithmetic
//     about a particular person, and a pack that could rewrite it could make it
//     say the wrong number.
//
// Copy is not escaped or sanitised here, and does not need to be: templ escapes
// every interpolated string at render time. A pack cannot inject markup by
// writing <script> into a manifest. It also cannot inject markup on purpose,
// which is the point — copy is text, and anything structural is a template
// change with a review.
type Copy map[string]string

// Placeholders a copy string may contain. The renderer substitutes whatever the
// call site passes and leaves anything else alone, so a pack writing {season}
// into a string that is never given one gets the literal braces rather than a
// blank — visible, findable, and not a silently empty sentence.
//
// They are named rather than positional because a pack author rewrites these
// strings in a JSON file with no compiler: "{season} is in the archive" can be
// reordered to "the archive has {season} in it" and still work, which "%s" and
// "%[1]s" cannot survive.
const (
	// PlaceholderSeason is the season's own label: "2026" or "Night of a
	// Thousand Pixels 2026".
	PlaceholderSeason = "season"

	// PlaceholderDate is whichever date the sentence is about, already
	// formatted by the view model.
	PlaceholderDate = "date"

	// PlaceholderQuota is SubmitQuota.Label — a whole sentence about this
	// viewer's allowance, dropped into a longer one.
	PlaceholderQuota = "quota"

	// PlaceholderStatus is an HTTP status code on an error page.
	PlaceholderStatus = "status"

	// PlaceholderCount is a count already rendered with its noun: "6 films",
	// "1 test chamber".
	PlaceholderCount = "count"

	// PlaceholderTitle is a film's title.
	PlaceholderTitle = "title"

	// PlaceholderName is a person's display name.
	PlaceholderName = "name"

	// PlaceholderLimit is a viewer's whole submission allowance as a bare
	// number. It has no noun of its own: "1 pick left of 2" says the noun once.
	PlaceholderLimit = "limit"
)

// The copy keys, as constants so that a template and the catalogue cannot
// drift apart without the compiler noticing.
//
// The naming is page.thing.part, lowercase, dot-separated. Packs are written by
// hand in JSON against themes/README.md, so the keys have to read as English to
// somebody who has never opened this file.
const (
	// The shell.
	KeySiteTitle   = "site.title"
	KeySiteTagline = "site.tagline"
	KeySkipLink    = "site.skip_link"
	KeyNavLabel    = "site.nav_label"

	// Nouns. A film on the board is the single most themeable word on the
	// site: Portal calls it a test chamber, Elvira calls it a victim. They are
	// two keys rather than one because English pluralisation is not a function
	// and a pack must be able to write both.
	KeyFilmOne   = "noun.film.one"
	KeyFilmMany  = "noun.film.many"
	KeyThingOne  = "noun.thing.one"
	KeyThingMany = "noun.thing.many"
	KeyPickOne   = "noun.pick.one"
	KeyPickMany  = "noun.pick.many"

	// The three things a viewer's submission allowance can be. See
	// Theme.QuotaLabel: the numbers stay the view model's, the sentence around
	// them is the pack's.
	KeyQuotaBarred    = "quota.barred"
	KeyQuotaAtLimit   = "quota.at_limit"
	KeyQuotaRemaining = "quota.remaining"

	// Front page.
	KeyHomeLead         = "home.lead"
	KeyHomeNoticeLede   = "home.notice.lede"
	KeyHomeNoticeBody   = "home.notice.body"
	KeyHomeStepsHeading = "home.steps.heading"
	KeyHomeStep1Badge   = "home.step1.badge"
	KeyHomeStep1Title   = "home.step1.title"
	KeyHomeStep1Body    = "home.step1.body"
	KeyHomeStep2Badge   = "home.step2.badge"
	KeyHomeStep2Title   = "home.step2.title"
	KeyHomeStep2Body    = "home.step2.body"
	KeyHomeStep3Badge   = "home.step3.badge"
	KeyHomeStep3Title   = "home.step3.title"
	KeyHomeStep3Body    = "home.step3.body"
	KeyHomeArchiveHead  = "home.archive.heading"
	KeyHomeArchiveEmpty = "home.archive.empty"

	// The slate.
	KeySlateHeadingFallback = "slate.heading.fallback"
	KeySlateCountHeading    = "slate.count.heading"
	KeySlateHiddenLede      = "slate.hidden.lede"
	KeySlateHiddenBody      = "slate.hidden.body"
	KeySlateEmptyHeading    = "slate.empty.heading"
	KeySlateEmptyBody       = "slate.empty.body"
	KeySlateEmptyCTA        = "slate.empty.cta"
	KeySlateEmptySignIn     = "slate.empty.signin"
	KeySlateSubmitCTA       = "slate.actions.cta"
	KeySlateSignInCTA       = "slate.actions.signin"
	KeySlateSignInNote      = "slate.actions.signin_note"

	// Where the season is in the year. The badge and the line under the slate's
	// heading; see Theme.SeasonStateLabel.
	KeyStateDraftLabel        = "season.state.draft.label"
	KeyStateDraftSummary      = "season.state.draft.summary"
	KeyStateSubmittingLabel   = "season.state.submitting.label"
	KeyStateSubmittingSummary = "season.state.submitting.summary"
	KeyStateVotingLabel       = "season.state.voting.label"
	KeyStateVotingSummary     = "season.state.voting.summary"
	KeyStateLockedLabel       = "season.state.locked.label"
	KeyStateLockedSummary     = "season.state.locked.summary"

	// The dates on the slate.
	KeySeasonDateSubmitOpens = "season.date.submit_opens"
	KeySeasonDateVoteOpens   = "season.date.vote_opens"
	KeySeasonDateVoteCloses  = "season.date.vote_closes"
	KeySeasonDateLocked      = "season.date.locked"

	// The result of a finished season.
	KeyOutcomeWinnerLede = "outcome.winner.lede"
	KeyOutcomeWinnerBody = "outcome.winner.body"
	KeyOutcomeTiedLede   = "outcome.tied.lede"
	KeyOutcomeTiedBody   = "outcome.tied.body"
	KeyOutcomeCounted    = "outcome.counted"

	// A film card.
	KeyMovieWinnerBadge    = "movie.badge.winner"
	KeyMovieWithdrawnBadge = "movie.badge.withdrawn"
	KeyMovieSubmittedBy    = "movie.submitted_by"
	KeyMovieTrailerCTA     = "movie.trailer.cta"
	KeyMovieTrailerTitle   = "movie.trailer.title"

	// The submit form.
	KeySubmitHeading       = "submit.heading"
	KeySubmitLead          = "submit.lead"
	KeySubmitButton        = "submit.button"
	KeySubmitCancel        = "submit.cancel"
	KeySubmitBack          = "submit.back"
	KeySubmitExistingHead  = "submit.existing.heading"
	KeySubmitErrorsHeading = "submit.errors.heading"

	// The submit form's fields. Labels and hints, not validation messages:
	// what a field is called is wording, what is wrong with it is a fact.
	KeySubmitTitleLabel       = "submit.field.title.label"
	KeySubmitTitleHint        = "submit.field.title.hint"
	KeySubmitYearLabel        = "submit.field.year.label"
	KeySubmitYearHint         = "submit.field.year.hint"
	KeySubmitTrailerLabel     = "submit.field.trailer.label"
	KeySubmitTrailerHint      = "submit.field.trailer.hint"
	KeySubmitDescriptionLabel = "submit.field.description.label"
	KeySubmitDescriptionHint  = "submit.field.description.hint"

	// The seven ways the submit page says no. Each is a heading and a body,
	// and they are seven rather than one because they are seven different
	// facts about seven different situations — see submitBlockedCopy.
	KeyBlockedLockedHeading   = "submit.blocked.locked.heading"
	KeyBlockedLockedBody      = "submit.blocked.locked.body"
	KeyBlockedVotingHeading   = "submit.blocked.voting.heading"
	KeyBlockedVotingBody      = "submit.blocked.voting.body"
	KeyBlockedDraftHeading    = "submit.blocked.draft.heading"
	KeyBlockedDraftDated      = "submit.blocked.draft.body_dated"
	KeyBlockedDraftUndated    = "submit.blocked.draft.body_undated"
	KeyBlockedBarredHeading   = "submit.blocked.barred.heading"
	KeyBlockedBarredBody      = "submit.blocked.barred.body"
	KeyBlockedAtLimitHeading  = "submit.blocked.at_limit.heading"
	KeyBlockedAtLimitDated    = "submit.blocked.at_limit.body_dated"
	KeyBlockedAtLimitUndated  = "submit.blocked.at_limit.body_undated"
	KeyBlockedOutsiderHeading = "submit.blocked.outsider.heading"
	KeyBlockedOutsiderBody    = "submit.blocked.outsider.body"
	KeyBlockedUnknownHeading  = "submit.blocked.unknown.heading"
	KeyBlockedUnknownBody     = "submit.blocked.unknown.body"

	// Error pages.
	KeyErrorBadge      = "error.badge"
	KeyErrorBack       = "error.back"
	KeyError404Heading = "error.404.heading"
	KeyError404Body    = "error.404.body"
	KeyError500Heading = "error.500.heading"
	KeyError500Body    = "error.500.body"
	KeyError405Heading = "error.405.heading"
	KeyError405Body    = "error.405.body"
)

// BaseCopy is every string the site can say, in the voice it uses with no pack
// loaded at all.
//
// This is the fallback half of the contract and it is not a placeholder set:
// it is the real, finished copy of an unthemed Night of a Thousand Pixels, and
// the site ships readable and complete if every pack is deleted. A pack
// overrides what it wants to and inherits the rest, so "Portal did not write an
// error page" degrades to a good error page rather than to a key name.
//
// Adding a key here without a pack override is therefore a normal, finished
// state, not a TODO.
var BaseCopy = Copy{
	KeySiteTitle:   DefaultSiteTitle,
	KeySiteTagline: DefaultTagline,
	KeySkipLink:    "Skip to content",
	KeyNavLabel:    "Primary",

	KeyFilmOne:   "film",
	KeyFilmMany:  "films",
	KeyThingOne:  "thing",
	KeyThingMany: "things",
	KeyPickOne:   "pick",
	KeyPickMany:  "picks",

	KeyQuotaBarred:    "You can vote this year, but not put films on the board.",
	KeyQuotaAtLimit:   "You have used all {count} for this year.",
	KeyQuotaRemaining: "{count} left of {limit}.",

	KeyHomeLead: "Thirty-odd friends, two picks each, and one instant-runoff count " +
		"that decides what goes on the screen.",
	KeyHomeNoticeLede: "Coming soon.",
	KeyHomeNoticeBody: "The site is being rebuilt for this season. Submissions open in " +
		"early October — the link lands in the group chat.",
	KeyHomeStepsHeading: "How a movie gets picked",
	KeyHomeStep1Badge:   "Step 1",
	KeyHomeStep1Title:   "Submit",
	KeyHomeStep1Body: "Early October. Sign in with Google and put up to two movies on " +
		"the board. A trailer link helps everyone else decide.",
	KeyHomeStep2Badge: "Step 2",
	KeyHomeStep2Title: "Rank",
	KeyHomeStep2Body: "Then pick your top three, in order. The count is instant-runoff, " +
		"so a second choice is not a wasted vote — it is what breaks the tie.",
	KeyHomeStep3Badge: "Step 3",
	KeyHomeStep3Title: "Watch",
	KeyHomeStep3Body: "Movie night. The winner goes up on the screen, and then into the " +
		"archive next to every year before it.",
	KeyHomeArchiveHead: "Past years",
	KeyHomeArchiveEmpty: "Nothing to browse here yet. Previous seasons move in as they " +
		"are imported, and then they stay put.",

	KeySlateHeadingFallback: "The slate",
	KeySlateCountHeading:    "{count} on the board",
	KeySlateHiddenLede:      "Heads up:",
	KeySlateHiddenBody: "This list includes withdrawn submissions, marked as such. " +
		"Nobody else sees them.",
	KeySlateEmptyHeading: "Nobody has gone first yet",
	KeySlateEmptyBody: "Every year starts here. Whoever puts the first film up sets the " +
		"tone for the whole night, and everyone else picks around it.",
	KeySlateEmptyCTA:    "Be the first",
	KeySlateEmptySignIn: "Sign in to be the first",
	KeySlateSubmitCTA:   "Put a movie up",
	KeySlateSignInCTA:   "Sign in to add yours",
	KeySlateSignInNote:  "Anyone can read the board. Only this year's group can add to it.",

	KeyStateDraftLabel:        "Not open yet",
	KeyStateDraftSummary:      "This year has not started yet. The link lands in the group chat when it does.",
	KeyStateSubmittingLabel:   "Submissions open",
	KeyStateSubmittingSummary: "Put your picks on the board. Ranking opens once submissions close.",
	KeyStateVotingLabel:       "Voting open",
	KeyStateVotingSummary:     "Submissions are closed. Rank your top three — a second choice is what breaks the tie.",
	KeyStateLockedLabel:       "Finished",
	KeyStateLockedSummary:     "This year is finished and in the archive.",

	KeySeasonDateSubmitOpens: "Submissions open",
	KeySeasonDateVoteOpens:   "Ranking opens",
	KeySeasonDateVoteCloses:  "Voting closes",
	KeySeasonDateLocked:      "Locked",

	KeyOutcomeWinnerLede: "The winner:",
	KeyOutcomeWinnerBody: "{title}.",
	KeyOutcomeTiedLede:   "It tied.",
	KeyOutcomeTiedBody: "The last round of the count came out level, so no single film " +
		"won outright.",
	KeyOutcomeCounted: "Counted {date}.",

	KeyMovieWinnerBadge:    "Winner",
	KeyMovieWithdrawnBadge: "Withdrawn",
	KeyMovieSubmittedBy:    "Submitted by {name}",
	KeyMovieTrailerCTA:     "Watch the trailer",
	KeyMovieTrailerTitle:   "Trailer: {title}",

	KeySubmitHeading: "Put a movie on the board",
	KeySubmitLead: "A title is all we need. A trailer link and a line about why you " +
		"picked it is what makes somebody else rank it first.",
	KeySubmitButton:        "Put it on the board",
	KeySubmitCancel:        "Cancel",
	KeySubmitBack:          "See what is on the board",
	KeySubmitExistingHead:  "{count} from you so far",
	KeySubmitErrorsHeading: "{count} to fix before this goes up.",

	KeySubmitTitleLabel:       "Title",
	KeySubmitTitleHint:        "The name of the film. This is the only part we cannot guess.",
	KeySubmitYearLabel:        "Year",
	KeySubmitYearHint:         "Optional, and four digits — it settles which remake you meant.",
	KeySubmitTrailerLabel:     "Trailer link",
	KeySubmitTrailerHint:      "Optional. A YouTube link plays right on the board; anything else becomes a link.",
	KeySubmitDescriptionLabel: "Why this one",
	KeySubmitDescriptionHint:  "Optional, and the bit people actually read. A sentence is plenty.",

	KeyBlockedLockedHeading: "That year is finished",
	KeyBlockedLockedBody: "{season} is in the archive, and an archived year does not " +
		"change. Its slate is what it is.",
	KeyBlockedVotingHeading: "The board is closed",
	KeyBlockedVotingBody: "Submissions are done for this year — everything that made it " +
		"is on the slate. Ranking is what decides the rest now.",
	KeyBlockedDraftHeading: "This year has not opened yet",
	KeyBlockedDraftDated: "The board opens on {date}. The link lands in the group chat " +
		"when it does.",
	KeyBlockedDraftUndated: "The dates for this year are not set yet. The link lands in " +
		"the group chat once they are.",
	KeyBlockedBarredHeading: "You are on the list to vote",
	KeyBlockedBarredBody: "{quota} That is a setting on your name rather than something " +
		"you used up, so say something in the group chat if it looks wrong.",
	KeyBlockedAtLimitHeading:  "That is both your picks",
	KeyBlockedAtLimitDated:    "{quota} They are below. Ranking opens on {date}.",
	KeyBlockedAtLimitUndated:  "{quota} They are below, and ranking opens once submissions close.",
	KeyBlockedOutsiderHeading: "Submitting is for this year's group",
	KeyBlockedOutsiderBody: "The board is public to read and not to write. If you are on " +
		"this year's list, sign in with the account you gave us; if you are not, ask in " +
		"the group chat.",
	KeyBlockedUnknownHeading: "Not right now",
	KeyBlockedUnknownBody: "The board is not taking your submission at the moment. The " +
		"slate is still there to read, and the group chat is the fastest way to find out why.",

	KeyErrorBadge:      "Error {status}",
	KeyErrorBack:       "Back to the front page",
	KeyError404Heading: "That page isn't here",
	KeyError404Body: "The link may be out of date, or a season that hasn't opened yet. " +
		"Nothing is broken — there is just nothing at this address.",
	KeyError500Heading: "Something went wrong on our end",
	KeyError500Body: "The server hit an error and gave up on this page. It has been logged. " +
		"Try again in a minute, and say something in the group chat if it keeps happening.",
	KeyError405Heading: "That isn't something you can do here",
	KeyError405Body: "The page exists, but not for the kind of request that reached it. " +
		"This usually means a stale form or a reloaded submission.",
}

// Text returns the string for key with the pack's version preferred, the base
// string as the fallback, and the placeholders in vars substituted.
//
// vars is alternating name and value — Text(KeyBlockedLockedBody,
// PlaceholderSeason, "2026") — which reads at the call site like the sentence
// it fills in. An odd trailing name is ignored rather than panicking: this runs
// during a render, and a half-written call is not worth a 500 on a page that
// would otherwise be fine.
//
// A pack string that is present but empty counts as set, and renders as empty.
// That is a real choice a pack can make — Portal suppressing a lede word, say —
// and quietly reviving the base string would make it impossible.
func (c Copy) Text(key string, vars ...string) string {
	s, ok := c[key]
	if !ok {
		s, ok = BaseCopy[key]
	}
	if !ok {
		// An unknown key. Return it rather than "": a key name on the page is
		// ugly and findable, an empty heading is invisible and ships.
		return key
	}

	return substitute(s, vars)
}

// Has reports whether this copy set defines key itself, ignoring the base
// fallback. It exists for tests and for the pack linter, not for templates —
// a template asks for a string and always gets one.
func (c Copy) Has(key string) bool {
	_, ok := c[key]

	return ok
}

// substitute replaces {name} with its value for each name/value pair in vars.
//
// Unknown placeholders are left alone, which is the honest failure: a pack that
// writes {seasons} sees {seasons} on the page and fixes it, rather than seeing
// a sentence with a hole in it and never knowing.
func substitute(s string, vars []string) string {
	if len(vars) < 2 || !strings.ContainsRune(s, '{') {
		return s
	}

	// Two at a time, dropping an odd trailing name.
	pairs := make([]string, 0, len(vars))
	for i := 0; i+1 < len(vars); i += 2 {
		pairs = append(pairs, "{"+vars[i]+"}", vars[i+1])
	}

	return strings.NewReplacer(pairs...).Replace(s)
}

// Text is the theme's copy lookup: the pack's string for key, or the base one.
// The zero Theme has no Copy at all and answers entirely out of BaseCopy, which
// is what makes every template safe to write against this and nothing else.
func (t Theme) Text(key string, vars ...string) string {
	return t.Copy.Text(key, vars...)
}

// Text on the layout data, so a template that already has the page shell does
// not have to reach through to the theme. p.Text(...) rather than
// p.Theme.Text(...) is the whole of the difference, and it is the difference
// between a template that reads and one that does not.
func (d LayoutData) Text(key string, vars ...string) string {
	return d.Theme.Text(key, vars...)
}

// Count renders a number with the pack's own noun: "6 films", "1 test chamber",
// "3 victims".
//
// The count is data and the noun is copy, which is exactly the split this file
// exists to draw. one and many are copy keys rather than words so that the call
// site names the thing being counted and the pack decides what to call it.
func (t Theme) Count(n int, oneKey, manyKey string) string {
	if n == 1 {
		return "1 " + t.Text(oneKey)
	}

	return strconv.Itoa(n) + " " + t.Text(manyKey)
}

// Count on the layout data, for the same reason Text is.
func (d LayoutData) Count(n int, oneKey, manyKey string) string {
	return d.Theme.Count(n, oneKey, manyKey)
}

// QuotaLabel is SubmitQuota.Label in the pack's voice.
//
// The split is the one this file is built on. How many picks somebody has left
// is arithmetic — q.Remaining(), q.Limit, and whether "pick" needs an s — and
// it stays here where it can be tested. The sentence those numbers appear in is
// wording, and a pack that cannot rewrite it ends up saying "You have used all
// 2 picks for this year" in a voice that belongs to nothing on the page.
//
// That mattered enough to be worth the extra method: the cap refusal is one of
// the three or four sentences on this site that somebody actually reads
// carefully, because it is the one telling them no.
//
// With no pack it returns exactly what SubmitQuota.Label returns, which is why
// Label stays as it is and every existing caller of it keeps working.
func (t Theme) QuotaLabel(q SubmitQuota) string {
	switch {
	case !q.Known:
		return ""
	case q.Barred():
		return t.Text(KeyQuotaBarred)
	case q.AtLimit():
		return t.Text(KeyQuotaAtLimit, PlaceholderCount, t.Count(q.Limit, KeyPickOne, KeyPickMany))
	default:
		return t.Text(KeyQuotaRemaining,
			PlaceholderCount, t.Count(q.Remaining(), KeyPickOne, KeyPickMany),
			PlaceholderLimit, strconv.Itoa(q.Limit))
	}
}

// QuotaLabel on the layout data, for the same reason Text is.
func (d LayoutData) QuotaLabel(q SubmitQuota) string {
	return d.Theme.QuotaLabel(q)
}

// SeasonStateLabel and SeasonStateSummary are SeasonState.Label and .Summary in
// the pack's voice.
//
// These two strings sit at the top of the slate, which is the page the link in
// the group chat opens. A pack that cannot touch them has an Aperture Science
// header over a badge reading "Submissions open" and a line saying "Put your
// picks on the board" — the theme stops at the fold, and everybody can see
// where.
//
// A state this catalogue does not know — a value that reached the view model
// from a column somebody added — falls through to the state's own method, which
// prints the raw value rather than inventing one.
func (t Theme) SeasonStateLabel(s SeasonState) string {
	switch s {
	case SeasonDraft:
		return t.Text(KeyStateDraftLabel)
	case SeasonSubmitting:
		return t.Text(KeyStateSubmittingLabel)
	case SeasonVoting:
		return t.Text(KeyStateVotingLabel)
	case SeasonLocked:
		return t.Text(KeyStateLockedLabel)
	default:
		return s.Label()
	}
}

// SeasonStateSummary is the line under the slate's heading.
func (t Theme) SeasonStateSummary(s SeasonState) string {
	switch s {
	case SeasonDraft:
		return t.Text(KeyStateDraftSummary)
	case SeasonSubmitting:
		return t.Text(KeyStateSubmittingSummary)
	case SeasonVoting:
		return t.Text(KeyStateVotingSummary)
	case SeasonLocked:
		return t.Text(KeyStateLockedSummary)
	default:
		return s.Summary()
	}
}

// SeasonStateLabel and SeasonStateSummary on the layout data, for the same
// reason Text is.
func (d LayoutData) SeasonStateLabel(s SeasonState) string {
	return d.Theme.SeasonStateLabel(s)
}

// SeasonStateSummary is the line under the slate's heading, in the pack's voice.
func (d LayoutData) SeasonStateSummary(s SeasonState) string {
	return d.Theme.SeasonStateSummary(s)
}
