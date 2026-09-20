package viewmodel

// SlatePage is the board: every live submission for a season, plus what this
// particular viewer can do about it.
//
// The same struct serves all four season states. A draft or empty season is
// not a different page, it is this page with no movies — which is what
// everybody sees on the morning the season opens, and so is the state the
// template has to look inviting in rather than broken.
type SlatePage struct {
	Layout LayoutData

	// Season is what the page is about: name, state and the window dates.
	Season SeasonSummary

	// Movies is the slate, in the order ListVisibleMoviesForSeason returns
	// them — submission time, with an id tiebreak, so it does not shuffle
	// between page loads. Empty is a normal state, not an error.
	Movies []MovieCard

	// Quota is this viewer's submission allowance. Its zero value means the
	// viewer has none — signed out, or not a member of this season.
	Quota SubmitQuota

	// CanSubmit is the handler's decision about whether to offer the submit
	// control at all. It is the season window, membership and Quota.HasRoom
	// taken together, and it is a plain bool because the template must not be
	// the place that combination is assembled — two templates assembling it
	// slightly differently is how a closed window ends up accepting a
	// submission.
	//
	// When it is false, Quota.Label says why in words the viewer can act on.
	CanSubmit bool

	// SubmitHref is where the submit control points. Blank renders no control
	// regardless of CanSubmit.
	SubmitHref string

	// Result is the frozen outcome, set only for a locked season that has been
	// tallied. Nil at every other point in the year, including a locked season
	// whose count has not been run.
	Result *SeasonResult

	// ShowingHidden is true when this is an admin view that includes
	// soft-deleted submissions. It exists so the page can say so out loud: a
	// list that quietly contains hidden films, with no note that it does, is
	// how an admin comes to believe a hidden film is still on the public
	// slate.
	ShowingHidden bool
}

// HasMovies reports whether there is a slate to show yet.
func (p SlatePage) HasMovies() bool {
	return len(p.Movies) > 0
}

// MovieCount is how many films are on the board.
func (p SlatePage) MovieCount() int {
	return len(p.Movies)
}

// IsFinished reports whether this season is the archive rather than a live
// year, which is what decides between "rank these" and "this is what won".
func (p SlatePage) IsFinished() bool {
	return p.Season.State.IsLocked()
}

// SubmitPage is the form for putting a movie on the board.
type SubmitPage struct {
	Layout LayoutData

	// Season is the season being submitted to.
	Season SeasonSummary

	// Form is the form's current values and any validation messages. On a
	// first render it is blank; on a rejected submission it carries back what
	// the person typed.
	Form SubmitForm

	// Quota is their allowance, and the source of the "one pick left" line.
	Quota SubmitQuota

	// Existing is what this person already has on the board, from
	// ListPersonMoviesForSeason. Showing it next to the form is what makes
	// "one pick left" mean something concrete, and it is the same list the
	// quota counted — both queries exclude hidden rows, so they agree.
	Existing []MovieCard

	// CanSubmit is the handler's decision, on the same terms as
	// SlatePage.CanSubmit. When it is false the page shows why instead of the
	// form — the form is not merely disabled, because a disabled form is
	// something people try to fill in anyway.
	CanSubmit bool

	// Action is where the form posts. It is a field rather than a literal in
	// the template because the same form edits an existing submission, and
	// that posts somewhere else.
	Action string

	// CancelHref is the way back out, normally the slate.
	CancelHref string

	// HeadingKey, LeadKey and ButtonKey let the same page render as the edit
	// form (ticket E3). Blank means the new-submission wording, so every
	// existing caller keeps what it had.
	//
	// They are copy KEYS rather than strings because these three lines belong
	// to a theme pack, like every other sentence on the page. A handler that
	// could pass the words themselves would be the one place on the site where
	// a pack silently stops being in charge of its own voice.
	HeadingKey string
	LeadKey    string
	ButtonKey  string

	// WithdrawAction is where the withdraw button posts, and non-blank is what
	// puts the control on the page at all.
	//
	// It is separate from Action because withdrawing is a different write from
	// saving an edit: one form cannot have two methods, and a single form with
	// two submit buttons distinguished by name is how a person on a phone
	// takes a film off the board by pressing Enter in the title field.
	//
	// It is also gated by the same CanSubmit the form is: an edit that the
	// season window refuses is a withdrawal the season window refuses too.
	WithdrawAction string

	// Search is the TMDB picker above the form: the search box, and whatever
	// the last query found. Its zero value is a page with no search on it at
	// all, which is exactly what a stack with no TMDB key should render.
	Search SearchState

	// Picked is the film chosen from that picker, or the zero value on a
	// manual submission. When it is set the form's title, year and trailer are
	// TMDB's answer rather than anything typed — and are looked up again on
	// POST, so what is shown here is a preview and not the input.
	Picked Picked
}

// HeadingCopyKey is the key for the h1: the edit wording when this page is
// editing, the submit wording otherwise.
func (p SubmitPage) HeadingCopyKey() string {
	if p.HeadingKey != "" {
		return p.HeadingKey
	}

	return KeySubmitHeading
}

// LeadCopyKey is the key for the line under the heading.
func (p SubmitPage) LeadCopyKey() string {
	if p.LeadKey != "" {
		return p.LeadKey
	}

	return KeySubmitLead
}

// ButtonCopyKey is the key for the form's submit button.
func (p SubmitPage) ButtonCopyKey() string {
	if p.ButtonKey != "" {
		return p.ButtonKey
	}

	return KeySubmitButton
}

// CanWithdraw reports whether to render the withdraw control.
//
// It is CanSubmit and an action together rather than either alone. CanSubmit
// carries the handler's decision about the season window and this viewer;
// WithdrawAction carries whether there is a film to withdraw at all. A page
// that had one without the other would be either a button that cannot work or
// a decision that never reached the markup.
func (p SubmitPage) CanWithdraw() bool {
	return p.CanSubmit && p.WithdrawAction != ""
}

// SignInPage is the page that asks someone to sign in with Google.
type SignInPage struct {
	Layout LayoutData

	// SignInHref starts the OIDC flow.
	SignInHref string

	// Reason is why they are seeing this rather than what they asked for:
	// "Sign in to put a movie on the board." Blank renders the generic copy.
	Reason string

	// ReturnTo is where they land afterwards, shown so the page can say "then
	// we will take you back to the slate". It is display only — the real
	// redirect target is carried in the flow's own state, never in a field a
	// template could round-trip into a link.
	ReturnTo string
}

// NotOnWhitelistPage is what a verified Google account sees when it is not in
// this season's season_member table.
//
// It is a dead end by design, but a polite and specific one. The most likely
// cause by far is the wrong Google account rather than genuine exclusion, so
// the page shows the address that was used and offers a way to sign out and
// try again before it offers anything else.
type NotOnWhitelistPage struct {
	Layout LayoutData

	// Email is the address they signed in with. Showing it back is the whole
	// point: "oh, that is my work account" is the answer most of the time.
	Email string

	// SeasonName is the season they are not on the list for. Membership is per
	// year, so naming the year matters — being in last year's group is not
	// being in this one.
	SeasonName string

	// ContactLabel and ContactHref are who to ask, e.g. "ask in the group
	// chat". Both blank omits the offer rather than rendering a dead link.
	ContactLabel string
	ContactHref  string

	// SignOutHref lets them drop this session and come back as someone else.
	SignOutHref string
}

// HasContact reports whether there is somewhere to send them.
func (p NotOnWhitelistPage) HasContact() bool {
	return p.ContactHref != "" && p.ContactLabel != ""
}
