package signin

import (
	"context"

	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// Viewer is who is making the current request, as far as the application is
// concerned: the person, the season they are being considered for, and their
// membership of it.
//
// Both halves are present on purpose. Authentication answers "who is this",
// and Person carries that. Authorisation answers "may they act", and Member
// carries that -- per season, because someone who ran the 2025 season is not
// automatically an admin of 2026.
type Viewer struct {
	Person store.Person
	Season store.Season
	Member store.SeasonMember
}

// IsAdmin reports whether this person administers THIS season. It reads the
// membership row rather than anything on the person or in the session, so
// revoking admin takes effect on the next request.
func (v Viewer) IsAdmin() bool { return v.Member.IsAdmin }

// DisplayName is what to call them on a page, falling back to the address when
// the provider gave no name.
func (v Viewer) DisplayName() string {
	if v.Person.DisplayName != "" {
		return v.Person.DisplayName
	}
	return v.Person.Email
}

// viewerKey is the context key. It is an unexported type so that no other
// package can collide with it or forge a viewer into a context.
type viewerKey struct{}

// withViewer returns ctx carrying v.
func withViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// Current returns the viewer for this request.
//
// ok is false on any request that did not pass through RequireMember, which is
// the point: a handler cannot accidentally read a half-populated viewer off a
// public route. A handler mounted behind RequireMember may treat false as a
// programming error, because the middleware does not call it otherwise.
func Current(ctx context.Context) (Viewer, bool) {
	v, ok := ctx.Value(viewerKey{}).(Viewer)
	return v, ok
}

// MustCurrent is Current for handlers that are mounted behind RequireMember
// and would have nothing sensible to render without a viewer.
func MustCurrent(ctx context.Context) Viewer {
	v, ok := Current(ctx)
	if !ok {
		panic("signin: no viewer in context - this handler is not mounted behind RequireMember")
	}
	return v
}
