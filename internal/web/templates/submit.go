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
func submitBlockedCopy(p Submit) blockedCopy {
	switch {
	case p.Season.State == viewmodel.SeasonLocked:
		return blockedCopy{
			Heading: "That year is finished",
			Body: p.Season.Label() + " is in the archive, and an archived year does not change. " +
				"Its slate is what it is.",
		}

	case p.Season.State == viewmodel.SeasonVoting:
		return blockedCopy{
			Heading: "The board is closed",
			Body: "Submissions are done for this year — everything that made it is on the slate. " +
				"Ranking is what decides the rest now.",
		}

	case p.Season.State == viewmodel.SeasonDraft:
		return blockedCopy{
			Heading: "This year has not opened yet",
			Body:    draftBody(p.Season),
		}

	case p.Quota.Barred():
		return blockedCopy{
			Heading: "You are on the list to vote",
			Body: p.Quota.Label() + " That is a setting on your name rather than something you " +
				"used up, so say something in the group chat if it looks wrong.",
		}

	case p.Quota.AtLimit():
		return blockedCopy{
			Heading: "That is both your picks",
			Body:    p.Quota.Label() + " " + atLimitTail(p.Season),
		}

	case !p.Quota.Known:
		return blockedCopy{
			Heading: "Submitting is for this year's group",
			Body: "The board is public to read and not to write. If you are on this year's list, " +
				"sign in with the account you gave us; if you are not, ask in the group chat.",
		}

	default:
		// CanSubmit is false for a reason this page has not been told about —
		// a handler knows something the view model does not carry. Say the
		// true, small thing rather than guessing at the reason.
		return blockedCopy{
			Heading: "Not right now",
			Body: "The board is not taking your submission at the moment. The slate is still there " +
				"to read, and the group chat is the fastest way to find out why.",
		}
	}
}

// draftBody names the opening date when there is one. A season can exist with
// every window column NULL — migration 00002 allows it — so "Submissions open"
// with nothing after it is a real possibility and not a nicety.
func draftBody(season SeasonSummary) string {
	if opens := season.SubmitOpensLabel(); opens != "" {
		return "The board opens on " + opens + ". The link lands in the group chat when it does."
	}

	return "The dates for this year are not set yet. The link lands in the group chat once they are."
}

// atLimitTail points someone who has used both picks at what happens next,
// and says nothing about a date that has not been scheduled.
func atLimitTail(season SeasonSummary) string {
	if opens := season.VoteOpensLabel(); opens != "" {
		return "They are below. Ranking opens on " + opens + "."
	}

	return "They are below, and ranking opens once submissions close."
}
