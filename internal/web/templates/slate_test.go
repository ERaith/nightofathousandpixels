package templates_test

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The ordinary case: six films, all six on the board, each credited.
func TestSlateRendersEveryFilm(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureFullSlate()
	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, "6 films on the board") {
		t.Error("the slate does not say how many films are on it")
	}

	if cards := strings.Count(main, `<li class="card">`); cards != len(page.Movies) {
		t.Errorf("rendered %d cards for %d films", cards, len(page.Movies))
	}

	for _, movie := range page.Movies {
		if !strings.Contains(main, "Submitted by") {
			t.Fatal("no card credits its submitter")
		}
		if title := movie.YearLabel(); title != "" && !strings.Contains(main, title) {
			t.Errorf("the year %s is missing from the board", title)
		}
	}
}

// The state of the site on the morning of 1 October, and the one everybody
// sees first. "No movies yet" reads as a page that failed to load; this has to
// read as an invitation with something to press.
func TestEmptySlateInvitesRatherThanApologises(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureEmptySlate()
	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, "Nobody has gone first yet") {
		t.Error("the empty slate does not name the situation")
	}
	if !strings.Contains(main, "Be the first") {
		t.Error("the empty slate does not ask anybody to go first")
	}

	// The invitation is a control, not a sentence about one.
	if !strings.Contains(main, `class="btn btn--primary" href="`+page.SubmitHref+`"`) {
		t.Error("the empty slate's invitation is not a button pointing at the submit page")
	}

	// And it is an empty board, not an empty page: the season, its state and
	// the date it closes are all still there to orient somebody who has just
	// followed a link from the group chat.
	for _, want := range []string{
		page.Season.Label(),
		page.Season.State.Label(),
		page.Season.VoteOpensLabel(),
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the empty slate dropped %q, which is context somebody arriving needs", want)
		}
	}

	if strings.Contains(main, "films on the board") {
		t.Error("the empty slate counted films it does not have")
	}
}

// An empty slate seen by somebody who is signed out still has to offer the
// next step, which for them is signing in rather than submitting.
func TestEmptySlateSignedOutOffersTheWayIn(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureEmptySlate()
	page.Layout = viewmodel.FixtureLayoutSignedOut()
	page.Quota = viewmodel.FixtureQuotaUnknown()
	page.CanSubmit = false

	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, "Sign in to be the first") {
		t.Error("a signed-out visitor on an empty board is offered nothing to do")
	}
	if strings.Contains(main, page.SubmitHref) {
		t.Error("a signed-out visitor is pointed straight at a form they cannot use")
	}
}

// A film with no trailer and no description is the minimum a movie row can
// carry. The card has to look deliberate rather than half-loaded, which means
// no empty player and no dangling "watch" link.
func TestSlateCardWithoutATrailer(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureEmptySlate()
	page.Movies = []viewmodel.MovieCard{viewmodel.FixtureNoTrailerMovie()}

	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, "Nosferatu (1922)") {
		t.Error("the card did not render at all")
	}
	if !strings.Contains(main, "Submitted by Priya Raman") {
		t.Error("the card does not credit its submitter")
	}

	for _, unwanted := range []string{"<iframe", "card__media", "Watch the trailer"} {
		if strings.Contains(main, unwanted) {
			t.Errorf("a film with no trailer rendered %s", unwanted)
		}
	}
}

// A trailer that cannot be embedded is the common case, not the exotic one.
// The card offers the link instead of an empty player, and the link says which
// film it is for so that six of them are distinguishable in a link list.
func TestSlateCardWithAnUnembeddableTrailer(t *testing.T) {
	t.Parallel()

	movie := viewmodel.FixtureUnembeddableTrailerMovie()

	page := viewmodel.FixtureEmptySlate()
	page.Movies = []viewmodel.MovieCard{movie}

	main := mainContent(t, render(t, templates.SlatePage(page)))

	if strings.Contains(main, "<iframe") {
		t.Error("a trailer with no embed form was put in an iframe anyway")
	}
	if !strings.Contains(main, `href="`+movie.TrailerURL+`"`) {
		t.Error("the trailer link is missing, so the trailer is unreachable")
	}
	if !strings.Contains(main, "for "+movie.Title+" (opens in a new tab)") {
		t.Error("the trailer link does not name its film or warn about the new tab")
	}
	if !strings.Contains(main, `rel="noopener noreferrer"`) {
		t.Error("a target=_blank link without rel=noopener")
	}
}

// An embeddable trailer plays in place, and lazily: a full board is six
// players, and six eager ones on a phone is most of a megabyte before anybody
// has pressed anything.
func TestSlateEmbedsPlayableTrailersLazily(t *testing.T) {
	t.Parallel()

	movie := viewmodel.FixtureMovie()

	page := viewmodel.FixtureEmptySlate()
	page.Movies = []viewmodel.MovieCard{movie}

	main := mainContent(t, render(t, templates.SlatePage(page)))

	for _, want := range []string{
		`<div class="card__media"><iframe src="` + movie.TrailerEmbedURL + `"`,
		`title="Trailer: ` + movie.Title + `"`,
		`loading="lazy"`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the embedded trailer is missing %s", want)
		}
	}
}

// A real title long enough to overflow a card, with no year and no submitter
// name on the same card. It has to render whole: base.css wraps it, and
// nothing here truncates it, because a film's name is not decoration.
func TestSlateRendersALongTitleWhole(t *testing.T) {
	t.Parallel()

	movie := viewmodel.FixtureLongTitleMovie()

	page := viewmodel.FixtureEmptySlate()
	page.Movies = []viewmodel.MovieCard{movie}

	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, movie.Title) {
		t.Error("the long title was cut short somewhere in the template")
	}
	if strings.Contains(main, "…") || strings.Contains(main, "...") {
		t.Error("the long title was truncated")
	}

	// No year, so no empty brackets after the name.
	if strings.Contains(main, movie.Title+" (") {
		t.Error("a film with no year rendered empty brackets")
	}

	// No submitter name, so the credit falls back rather than trailing off.
	if !strings.Contains(main, "Submitted by someone in the group") {
		t.Error("a blank submitter name rendered as a credit with nothing after it")
	}
}

// An apostrophe has to be escaped on the way out and an accented character has
// to survive as itself. Half the group has one in their name and a good third
// of the films have one in the title, so this is ordinary data, not an edge
// case anybody gets to skip.
func TestSlateEscapesApostrophesAndKeepsAccents(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureFullSlate()
	page.Movies = []viewmodel.MovieCard{viewmodel.FixtureApostropheMovie()}

	main := mainContent(t, render(t, templates.SlatePage(page)))

	if strings.Contains(main, "Rosemary's Baby") {
		t.Error("an apostrophe reached the browser unescaped")
	}
	if !strings.Contains(main, "Rosemary&#39;s Baby (1968)") {
		t.Error("the escaped title is missing, so it did not render at all")
	}
	if !strings.Contains(main, "Submitted by Siobhán O&#39;Doherty") {
		t.Error("the submitter's name lost its accent or its apostrophe")
	}

	// The same name goes through an attribute, where a raw apostrophe would be
	// a different and worse bug.
	if !strings.Contains(main, `title="Trailer: Rosemary&#39;s Baby"`) {
		t.Error("the apostrophe is unescaped inside an attribute")
	}
}

// A finished season shows what won and offers nothing to press. Migration
// 00008 makes a locked season immutable, and a page should not offer what the
// database will refuse.
func TestLockedSlateShowsItsResultAndNothingToDo(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureLockedSlate()
	main := mainContent(t, render(t, templates.SlatePage(page)))

	for _, want := range []string{
		"The winner:",
		page.Result.WinnerTitle,
		"Counted " + page.Result.TalliedLabel(),
		`<span class="badge badge--accent">Winner</span>`,
		"Finished",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the locked slate is missing %s", want)
		}
	}

	if strings.Contains(main, "Put a movie up") {
		t.Error("a locked season offers a submit control")
	}

	// Nothing on the page changes the season. The trailer links are exempt and
	// deliberately so — watching a trailer for a film that won four years ago
	// is reading the archive, not editing it — so the check is for a primary
	// action, which on this site is always something that writes.
	if strings.Contains(main, "btn--primary") {
		t.Error("a locked season offers a primary action; an archived year is immutable")
	}
}

// A tie is a real outcome — result.winner_movie_id is nullable for exactly
// this case — so the page says so rather than showing an empty winner.
func TestLockedSlateTiedSaysSo(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SlatePage(viewmodel.FixtureLockedSlateTied())))

	if !strings.Contains(main, "It tied.") {
		t.Error("a tied season does not say it tied")
	}
	if strings.Contains(main, "The winner:") {
		t.Error("a tied season announced a winner")
	}
	if strings.Contains(main, ">Winner<") {
		t.Error("a tied season badged a card as the winner")
	}
}

// Same board, two people who cannot add to it, two different reasons. One is a
// setting somebody chose and the other is progress the viewer made, and the
// copy has to tell them apart — that is what SubmitQuota.Barred and
// SubmitQuota.AtLimit exist for.
func TestSlateTellsBarredAndAtLimitApart(t *testing.T) {
	t.Parallel()

	barred := mainContent(t, render(t, templates.SlatePage(viewmodel.FixtureSlateBarred())))
	atLimit := mainContent(t, render(t, templates.SlatePage(viewmodel.FixtureSlateAtLimit())))

	barredLine := viewmodel.FixtureQuotaBarred().Label()
	atLimitLine := viewmodel.FixtureQuotaAtLimit().Label()

	if barredLine == atLimitLine {
		t.Fatal("the two quota sentences are identical; there is nothing for the page to tell apart")
	}

	if !strings.Contains(barred, barredLine) {
		t.Errorf("the barred viewer is not told %q", barredLine)
	}
	if strings.Contains(barred, atLimitLine) {
		t.Error("the barred viewer is told they used up picks they never had")
	}

	if !strings.Contains(atLimit, atLimitLine) {
		t.Errorf("the at-limit viewer is not told %q", atLimitLine)
	}
	if strings.Contains(atLimit, barredLine) {
		t.Error("the at-limit viewer is told they are voting-only")
	}
}

// Whenever the handler says no, there is no control to press. The nav's Submit
// link lives outside <main>, which is what makes this assertable: the claim is
// about the page's own content.
func TestSlateOffersNoSubmitControlWhenItCannotBeUsed(t *testing.T) {
	t.Parallel()

	for name, page := range viewmodel.FixtureSlates() {
		if page.CanSubmit {
			continue
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			main := mainContent(t, render(t, templates.SlatePage(page)))

			if page.SubmitHref != "" && strings.Contains(main, page.SubmitHref) {
				t.Errorf("a viewer who cannot submit is still pointed at %s", page.SubmitHref)
			}
			if strings.Contains(main, "Put a movie up") {
				t.Error("a viewer who cannot submit is still offered the control")
			}
		})
	}
}

// A signed-out visitor reads the board and is told, plainly, why they cannot
// add to it.
func TestSlateSignedOutReadsButCannotWrite(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureSlateSignedOut()
	main := mainContent(t, render(t, templates.SlatePage(page)))

	if !strings.Contains(main, "The Thing (1982)") {
		t.Error("the films are not public to a signed-out visitor")
	}
	if !strings.Contains(main, `href="`+page.Layout.SignInHref+`"`) {
		t.Error("a signed-out visitor is offered no way in")
	}
	if strings.Contains(main, "Put a movie up") {
		t.Error("a signed-out visitor is offered a control that would refuse them")
	}
}

// The admin view says out loud that it contains withdrawn films. A list that
// quietly includes them is how an admin comes to believe a hidden film is
// still on the public slate.
func TestAdminSlateFlagsWithdrawnFilms(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SlatePage(viewmodel.FixtureAdminSlate())))

	for _, want := range []string{
		"This list includes withdrawn submissions",
		`<span class="badge">Withdrawn</span>`,
		"<s>The Wicker Man (2006)</s>",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the admin slate is missing %s", want)
		}
	}
}

// The deadline line has to say what the date is, and never print a label with
// nothing after it — all four window columns are nullable, so an unscheduled
// season is a real state and not a hypothetical.
func TestSlateDeadlineFollowsTheSeasonState(t *testing.T) {
	t.Parallel()

	submitting := viewmodel.FixtureFullSlate()
	main := mainContent(t, render(t, templates.SlatePage(submitting)))

	if want := submitting.Season.VoteOpensLabel(); !strings.Contains(main, want) {
		t.Errorf("a season in its submission window does not show when it closes (%s)", want)
	}

	voting := viewmodel.FixtureFullSlate()
	voting.Season = viewmodel.FixtureSeasonVoting()
	voting.CanSubmit = false

	votingMain := mainContent(t, render(t, templates.SlatePage(voting)))
	if want := voting.Season.VoteClosesLabel(); !strings.Contains(votingMain, want) {
		t.Errorf("a season in its voting window does not show when voting closes (%s)", want)
	}

	unscheduled := viewmodel.FixtureEmptySlate()
	unscheduled.Season = viewmodel.FixtureSeasonUnscheduled()
	unscheduled.CanSubmit = false

	bare := mainContent(t, render(t, templates.SlatePage(unscheduled)))
	if strings.Contains(bare, "Submissions open</p>") {
		t.Error("an unscheduled season printed a date label with no date after it")
	}
	if strings.Contains(bare, `class="countdown`) {
		t.Error("an unscheduled season rendered an empty countdown")
	}
}

// Every slate fixture has to produce a whole document with a heading, whatever
// state it is in.
func TestSlateRendersEveryFixture(t *testing.T) {
	t.Parallel()

	for name, page := range viewmodel.FixtureSlates() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rendered := render(t, templates.SlatePage(page))

			if !strings.Contains(rendered, "</html>") {
				t.Errorf("fixture %q rendered an incomplete document", name)
			}

			heading := "<h1>" + page.Season.Label() + "</h1>"
			if !strings.Contains(rendered, heading) {
				t.Errorf("fixture %q does not head the page with its season", name)
			}
		})
	}
}

// A zero SlatePage is not a state a handler should reach, but it is the state
// a half-finished handler reaches, and it must not render a page with an empty
// <h1> — a document with no heading is unnavigable for anybody moving by
// heading.
func TestZeroSlateStillHasAHeading(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SlatePage(viewmodel.SlatePage{})))

	if !strings.Contains(main, "<h1>The slate</h1>") {
		t.Error("a zero slate rendered an empty heading")
	}
	if strings.Contains(main, "<h1></h1>") {
		t.Error("a zero slate rendered an empty heading")
	}
}
