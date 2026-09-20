package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ERaith/nightofathousandpixels/internal/web"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These are the four pages whose handler has no session and no database
// (nap-bus). What they know about the reader arrives as Options.Reader, so the
// whole of this file needs no Postgres and no OIDC provider: the lookup is a
// function, and here it is a literal.

const (
	signInPath  = "/auth/login"
	signOutPath = "/auth/logout"
	submitPath  = "/submit"
)

func nav() []templates.NavItem {
	return []templates.NavItem{{Label: "Home", Href: "/"}, {Label: "The slate", Href: "/slate"}}
}

func memberNav() []templates.NavItem {
	return append(nav(), templates.NavItem{Label: "Submit", Href: submitPath})
}

// reader returns an Options.Reader that always gives the same answer.
func reader(a *viewmodel.Account) func(*http.Request) *viewmodel.Account {
	return func(*http.Request) *viewmodel.Account { return a }
}

func member(name string) *viewmodel.Account {
	return &viewmodel.Account{User: &viewmodel.CurrentUser{DisplayName: name}, IsMember: true}
}

// get renders a path through a Site built with opts and returns the body.
func get(t *testing.T, opts web.Options, path string) string {
	t.Helper()

	opts.Nav = nav()
	opts.MemberNav = memberNav()
	opts.SignInHref = signInPath
	opts.SignOutHref = signOutPath

	r := chi.NewRouter()
	web.New(opts).Routes(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec.Body.String()
}

// The front page is the address that lands in the group chat, so it is the
// page on which "Sign in" shown to somebody who is already signed in does the
// most damage: the one control that looks like progress sends them back
// through OAuth to arrive exactly where they started.
func TestFrontPageGreetsASignedInReader(t *testing.T) {
	t.Parallel()

	body := get(t, web.Options{Reader: reader(member("Alice Voter"))}, "/")

	if !strings.Contains(body, "Alice Voter") {
		t.Error("the front page does not say who is reading it")
	}
	if !strings.Contains(body, `action="`+signOutPath+`"`) {
		t.Error("a signed-in reader is offered no way out of the session")
	}
	if strings.Contains(body, `href="`+signInPath+`"`) {
		t.Error("a signed-in reader is still offered a sign-in, which is the whole of nap-bus")
	}
}

// And the other half: nothing leaks to a reader who is not signed in.
func TestFrontPageOffersTheWayInToAVisitor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		read func(*http.Request) *viewmodel.Account
	}{
		{"no reader configured at all", nil},
		{"a reader that recognises nobody", reader(nil)},
		{"a reader that returns an account with no user on it", reader(&viewmodel.Account{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := get(t, web.Options{Reader: tc.read}, "/")

			if !strings.Contains(body, `href="`+signInPath+`"`) {
				t.Error("a visitor is not offered a way to sign in")
			}
			for _, leaked := range []string{"Signed in as", `action="` + signOutPath + `"`, `href="` + submitPath + `"`} {
				if strings.Contains(body, leaked) {
					t.Errorf("a signed-out visitor was shown %q", leaked)
				}
			}
		})
	}
}

// nap-dbu: from the front page a member could not reach the submit form
// without already knowing the URL. A member gets the link; nobody else does,
// because a header link that bounces a signed-out visitor into an OAuth flow
// they did not ask for is still worse than no link.
func TestSubmitIsInTheHeaderForMembersOnly(t *testing.T) {
	t.Parallel()

	signedInNonMember := &viewmodel.Account{User: &viewmodel.CurrentUser{DisplayName: "Nina Newcomer"}}

	for _, tc := range []struct {
		name    string
		account *viewmodel.Account
		want    bool
	}{
		{"a member", member("Alice Voter"), true},
		{"signed in but not on this season's list", signedInNonMember, false},
		{"a visitor", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := get(t, web.Options{Reader: reader(tc.account)}, "/")

			if got := strings.Contains(body, `href="`+submitPath+`"`); got != tc.want {
				t.Errorf("Submit in the header = %v, want %v", got, tc.want)
			}
		})
	}
}

// The error pages are rendered by the same handler-with-no-session, and the
// 500 is rendered when things are already going wrong. A reader who is signed
// in is still signed in on a 404.
func TestErrorPagesKnowTheReaderToo(t *testing.T) {
	t.Parallel()

	body := get(t, web.Options{Reader: reader(member("Alice Voter"))}, "/no-such-page")

	if !strings.Contains(body, "Alice Voter") {
		t.Error("the 404 page reverted to the signed-out header")
	}
}

// A Reader that cannot answer -- the database is down, which is exactly when
// the 500 page is being rendered -- must degrade to the signed-out header
// rather than failing. signin.ReadAccount is written not to fail; this is the
// page's half of that promise.
func TestAReaderThatKnowsNothingStillRendersAPage(t *testing.T) {
	t.Parallel()

	body := get(t, web.Options{Reader: func(*http.Request) *viewmodel.Account { return nil }}, "/")

	if !strings.Contains(body, "<h1>") {
		t.Fatal("the page did not render")
	}
	if !strings.Contains(body, `href="`+signInPath+`"`) {
		t.Error("the fallback is not the signed-out header")
	}
}

// The account lookup costs up to three queries, so it must not run for things
// that are not pages. /static/ and the theme assets never build one; page() is
// the only caller, which is what makes that structural rather than a rule.
func TestStaticRequestsNeverAskWhoIsReading(t *testing.T) {
	t.Parallel()

	asked := 0
	opts := web.Options{
		StaticDir: t.TempDir(),
		Reader: func(*http.Request) *viewmodel.Account {
			asked++

			return nil
		},
	}
	opts.Nav = nav()

	r := chi.NewRouter()
	web.New(opts).Routes(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/base.css", nil))

	if asked != 0 {
		t.Errorf("a static asset request ran the account lookup %d time(s)", asked)
	}
}
