package templates

import "github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"

// blockedCopy is what the submit page says instead of the form.
//
// It is copy, not a decision. Whether this viewer may submit has already been
// answered by the handler and arrives as SubmitPage.CanSubmit; all that is
// left is to say why, in a way the person can act on. Nothing below can turn
// a no into a yes.
//
// It lives in this package for the same reason ErrorContent does: it is fixed
// copy that no handler assembles, and a handler picks the situation rather
// than filling in the words.
type blockedCopy struct {
	// Heading is the h2 that replaces the form.
	Heading string

	// Body is the explanation, one or two sentences.
	Body string
}

// submitBlockedCopy picks the explanation for a submit page with no form on it.
//
// The order of the cases is the point. The season window is asked about first,
// because a closed window overrules everything else: somebody arriving on the
// submit page in the voting week still has a pick left, and telling them "1
// pick left of 2" next to a page with no form on it is how a site gets a
// reputation for being broken.
//
// Barred and AtLimit are then kept apart, which is the whole reason
// SubmitQuota has both methods. A limit of 0 is a setting somebody chose and
// is not going to change this year; a used-up limit of 2 is progress the
// viewer made themselves, and the films they made it with are on the same
// page. "You have used all your picks" told to a voting-only member is both
// wrong and unanswerable.
func submitBlockedCopy(t Theme, p Submit) blockedCopy {
	switch {
	case p.Season.State == viewmodel.SeasonLocked:
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedLockedHeading),
			Body:    t.Text(viewmodel.KeyBlockedLockedBody, viewmodel.PlaceholderSeason, p.Season.Label()),
		}

	case p.Season.State == viewmodel.SeasonVoting:
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedVotingHeading),
			Body:    t.Text(viewmodel.KeyBlockedVotingBody),
		}

	case p.Season.State == viewmodel.SeasonDraft:
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedDraftHeading),
			Body:    draftBody(t, p.Season),
		}

	case p.Quota.Barred():
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedBarredHeading),
			Body:    t.Text(viewmodel.KeyBlockedBarredBody, viewmodel.PlaceholderQuota, t.QuotaLabel(p.Quota)),
		}

	case p.Quota.AtLimit():
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedAtLimitHeading),
			Body:    atLimitBody(t, t.QuotaLabel(p.Quota), p.Season),
		}

	case !p.Quota.Known:
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedOutsiderHeading),
			Body:    t.Text(viewmodel.KeyBlockedOutsiderBody),
		}

	default:
		// CanSubmit is false for a reason this page has not been told about —
		// a handler knows something the view model does not carry. Say the
		// true, small thing rather than guessing at the reason.
		return blockedCopy{
			Heading: t.Text(viewmodel.KeyBlockedUnknownHeading),
			Body:    t.Text(viewmodel.KeyBlockedUnknownBody),
		}
	}
}

// draftBody names the opening date when there is one. A season can exist with
// every window column NULL — migration 00002 allows it — so "Submissions open"
// with nothing after it is a real possibility and not a nicety.
func draftBody(t Theme, season SeasonSummary) string {
	if opens := season.SubmitOpensLabel(); opens != "" {
		return t.Text(viewmodel.KeyBlockedDraftDated, viewmodel.PlaceholderDate, opens)
	}

	return t.Text(viewmodel.KeyBlockedDraftUndated)
}

// atLimitBody points someone who has used both picks at what happens next, and
// says nothing about a date that has not been scheduled.
//
// The quota sentence goes in as a placeholder rather than being prepended, so a
// pack can put "You have used all 2 picks for this year" where its own sentence
// wants it — or, if the voice calls for it, lead with its own line and let the
// fact follow. What it cannot do is change the fact, which is why this is the
// view model's Label and not a copy key.
func atLimitBody(t Theme, quota string, season SeasonSummary) string {
	if opens := season.VoteOpensLabel(); opens != "" {
		return t.Text(viewmodel.KeyBlockedAtLimitDated,
			viewmodel.PlaceholderQuota, quota,
			viewmodel.PlaceholderDate, opens)
	}

	return t.Text(viewmodel.KeyBlockedAtLimitUndated, viewmodel.PlaceholderQuota, quota)
}
