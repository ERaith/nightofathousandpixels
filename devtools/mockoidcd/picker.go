package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"sync"

	"github.com/oauth2-proxy/mockoidc"

	"github.com/ERaith/nightofathousandpixels/internal/devusers"
)

// userParam is how the picker tells itself which identity was chosen. It is
// not part of OIDC; mockoidc's authorize handler reads the parameters it cares
// about by name and ignores the rest, so carrying it through is harmless.
const userParam = "nap_user"

// pickUser is the consent screen Google would show.
//
// mockoidc's authorize endpoint pops one user off a queue and falls back to
// its own default (jane.doe@example.com) when the queue is empty. That is
// right for a test, which pushes the identity it wants first, but it leaves a
// person running `make dev` with no way to be anyone in particular - and being
// jane.doe means being nobody the seed has heard of, which makes every sign-in
// land on the "you're not on the list" page.
//
// So: an authorize request with no choice on it renders the list instead of
// redirecting, and each entry is a link back to this same URL with the choice
// added. Every other query parameter is preserved verbatim, which is what
// keeps state, nonce, the PKCE challenge and redirect_uri intact across the
// extra hop - the flow the application runs is unchanged, it just pauses for
// a click, exactly like a real consent screen.
func pickUser(m *mockoidc.MockOIDC, users []devusers.User) http.Handler {
	byEmail := make(map[string]devusers.User, len(users))
	for _, u := range users {
		byEmail[u.Email] = u
		log.Printf("mockoidcd: offering %s (%s) sub=%s", u.Email, u.Note, u.Subject())
	}

	// authorizing serialises the push-then-delegate pair below.
	//
	// mockoidc's queue is a FIFO and its Pop is destructive: authorize pops
	// exactly one user and falls back to DefaultUser() on an empty queue.
	// Pushing and then calling Authorize is therefore only correct if nothing
	// can pop in between. Two authorize requests in flight at once -- two
	// browser tabs, a double click, a test running in parallel with somebody
	// clicking -- would otherwise be free to interleave as push(A), push(B),
	// pop->A for B's request and pop->B for A's, signing each of them in as
	// the other.
	//
	// It also makes the queued-user check below honest: without the lock,
	// "is a user queued" could be true because another request queued it a
	// microsecond ago and is about to consume it.
	//
	// Serialising authorize costs nothing here. This is a development
	// provider; the whole point of it is that one person is clicking.
	var authorizing sync.Mutex

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chosen := r.URL.Query().Get(userParam)

		authorizing.Lock()
		defer authorizing.Unlock()

		// A queued identity wins over the picker, and silently.
		//
		// /control/user is how a test says who signs in next, and it pushes
		// onto the same queue mockoidc's authorize endpoint pops from. Without
		// this check the picker would intercept that request and render HTML
		// at a caller that is not a browser, the queued user would never be
		// popped, and the next interactive sign-in would get it instead --
		// signing somebody in as whoever a test queued minutes earlier.
		if chosen == "" && userQueued(m) {
			m.Authorize(w, r)
			return
		}

		if chosen == "" {
			// Let mockoidc decide whether this request is even valid before
			// offering anybody a list of names.
			//
			// The picker used to render as soon as it saw no choice, which
			// meant m.Authorize never ran and client_id, response_type, scope
			// and code_challenge_method were never checked. An authorize
			// request carrying a bogus client_id got a 200 and a list of real
			// people to click. Google answers an unknown client with an
			// error, and a provider that exists to be a faithful stand-in has
			// to do the same -- otherwise a mistyped OAUTH_CLIENT_ID in .env
			// shows a working-looking picker and the failure surfaces later
			// and somewhere else. Found by builder-5's e2e suite.
			//
			// The validation is DELEGATED rather than reimplemented here. A
			// copy of mockoidc's rules in this file would drift from them,
			// and the way it would drift is precisely back into this bug:
			// a request mockoidc rejects but our copy accepts gets the picker
			// again. So m.Authorize is run against a throwaway recorder and
			// asked what it thinks.
			//
			// Two things make that safe rather than clever. Every check in
			// m.Authorize runs BEFORE it pops the user queue, so a rejected
			// request consumes nothing. And this branch is only reachable
			// with an empty queue -- the check above returned false under the
			// same lock -- so even the accepted case pops nothing but
			// mockoidc's own DefaultUser, into a response that is discarded.
			probe := &capturedResponse{header: http.Header{}}
			m.Authorize(probe, r)
			if probe.status >= http.StatusBadRequest {
				probe.replayTo(w)
				return
			}

			renderPicker(w, r, users)
			return
		}

		user, ok := byEmail[chosen]
		if !ok {
			// An address that is not on the list is a typo in a hand-edited
			// URL, not something to sign a token for. Explicitly an error
			// rather than a fall-through: letting it reach m.Authorize with
			// an empty queue would sign the visitor in as mockoidc's
			// DefaultUser (jane.doe@example.com), who is on no whitelist, and
			// the refusal page that followed would name an address nobody
			// asked for.
			http.Error(w, "unknown dev user "+chosen+" - pick one from "+r.URL.Path, http.StatusBadRequest)
			return
		}

		m.QueueUser(&namedUser{
			MockUser: &mockoidc.MockUser{
				Subject:           user.Subject(),
				Email:             user.Email,
				EmailVerified:     true,
				PreferredUsername: user.Email,
			},
			// namedUser, not MockUser: mockoidc emits no "name" claim, and
			// internal/auth reads one into Identity.Name. Without the wrapper
			// every dev identity signs in with an empty display name.
			Name: user.DisplayName,
		})
		log.Printf("mockoidcd: signing in as %s (sub=%s)", user.Email, user.Subject())

		// Straight into mockoidc's own handler, still holding the lock so
		// that this push is the one it pops.
		m.Authorize(w, r)
	})
}

// capturedResponse is a minimal http.ResponseWriter used to ask m.Authorize
// what it makes of a request without letting its answer reach the browser.
//
// net/http/httptest would do this too, but it has no business being linked
// into a running binary; this is twenty lines and says exactly what it is.
type capturedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (c *capturedResponse) Header() http.Header { return c.header }

func (c *capturedResponse) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

func (c *capturedResponse) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

// replayTo writes the captured response through to a real writer, so the
// visitor gets mockoidc's own OAuth error verbatim rather than one this file
// invented.
func (c *capturedResponse) replayTo(w http.ResponseWriter) {
	for key, values := range c.header {
		w.Header()[key] = values
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body.Bytes())
}

// userQueued reports whether /control/user has pushed an identity that has not
// been consumed yet. mockoidc's Pop falls back to its own default user on an
// empty queue, so "is there one waiting" cannot be answered by popping.
func userQueued(m *mockoidc.MockOIDC) bool {
	m.UserQueue.Lock()
	defer m.UserQueue.Unlock()
	return len(m.UserQueue.Queue) > 0
}

// pickerData is what the template renders.
type pickerData struct {
	Users []pickerEntry
}

type pickerEntry struct {
	Email string
	Name  string
	Note  string
	Href  string
}

func renderPicker(w http.ResponseWriter, r *http.Request, users []devusers.User) {
	data := pickerData{Users: make([]pickerEntry, 0, len(users))}
	for _, u := range users {
		// Rebuild the URL with the choice appended, keeping every OIDC
		// parameter the application sent.
		next := *r.URL
		q := next.Query()
		q.Set(userParam, u.Email)
		next.RawQuery = q.Encode()

		data.Users = append(data.Users, pickerEntry{
			Email: u.Email,
			Name:  u.DisplayName,
			Note:  u.Note,
			Href:  next.RequestURI(),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Never cached: the query string carries this flow's state and nonce, so a
	// cached copy would offer links that mint a token for a finished login.
	w.Header().Set("Cache-Control", "no-store")
	if err := pickerTemplate.Execute(w, data); err != nil {
		log.Printf("mockoidcd: render picker: %v", err)
	}
}

// pickerTemplate is deliberately self-contained: this process serves no static
// files, and it must not borrow the application's stylesheet. Looking nothing
// like the site is the point - nobody should be able to mistake the mock
// provider for a page of the real one.
var pickerTemplate = template.Must(template.New("picker").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mock sign-in — development only</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 16px/1.5 system-ui, sans-serif; margin: 0; padding: 2rem 1rem;
         background: #1b1b1f; color: #f2f2f2; }
  main { max-width: 34rem; margin: 0 auto; }
  h1 { font-size: 1.3rem; margin: 0 0 .25rem; }
  .warn { background: #5a3d00; border: 1px solid #b07c00; border-radius: .4rem;
          padding: .75rem 1rem; margin: 1rem 0 1.5rem; font-size: .9rem; }
  ul { list-style: none; padding: 0; margin: 0; display: grid; gap: .6rem; }
  a.user { display: block; padding: .85rem 1rem; border: 1px solid #45454d;
           border-radius: .4rem; background: #26262c; color: inherit;
           text-decoration: none; }
  a.user:hover, a.user:focus { border-color: #8ab4f8; background: #2e2e36; }
  .email { font-weight: 600; }
  .note { font-size: .85rem; opacity: .75; }
</style>
</head>
<body>
<main>
  <h1>Mock sign-in</h1>
  <p class="note">This is not Google. It is the development OIDC provider.</p>
  <div class="warn">
    Nothing here is a shortcut past authentication. Choosing a name decides
    which address this provider signs an ID token for; the application still
    verifies that token and still decides whether the address is on the
    season&rsquo;s whitelist.
  </div>
  <ul>
    {{range .Users}}
    <li>
      <a class="user" href="{{.Href}}">
        <span class="email">{{.Email}}</span>
        {{if .Name}}<span class="note"> &middot; {{.Name}}</span>{{end}}
        {{if .Note}}<div class="note">{{.Note}}</div>{{end}}
      </a>
    </li>
    {{end}}
  </ul>
</main>
</body>
</html>
`))
