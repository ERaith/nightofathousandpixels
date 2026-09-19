package signin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// fakeStore is a hand-written Store. It is hand-written rather than generated
// because the behaviour that matters here is which queries return
// pgx.ErrNoRows and what that is taken to mean, and that is exactly what a
// permissive mock would let a test paper over.
type fakeStore struct {
	personBySub   map[string]store.Person
	personByID    map[uuid.UUID]store.Person
	upsertResult  store.Person
	upsertErr     error
	season        store.Season
	seasonErr     error
	member        store.SeasonMember
	memberErr     error
	updateCalled  bool
	upsertCalled  bool
	lastUpsertArg store.UpsertPersonOnSignInParams
}

func (f *fakeStore) GetPersonByGoogleSub(_ context.Context, sub string) (store.Person, error) {
	if p, ok := f.personBySub[sub]; ok {
		return p, nil
	}
	return store.Person{}, pgx.ErrNoRows
}

func (f *fakeStore) UpdatePersonIdentity(_ context.Context, arg store.UpdatePersonIdentityParams) (store.Person, error) {
	f.updateCalled = true
	p := f.personByID[arg.ID]
	p.Email = arg.Email
	p.EmailNormalized = arg.EmailNormalized
	if arg.DisplayName != "" {
		p.DisplayName = arg.DisplayName
	}
	return p, nil
}

func (f *fakeStore) UpsertPersonOnSignIn(_ context.Context, arg store.UpsertPersonOnSignInParams) (store.Person, error) {
	f.upsertCalled = true
	f.lastUpsertArg = arg
	return f.upsertResult, f.upsertErr
}

func (f *fakeStore) GetPerson(_ context.Context, id uuid.UUID) (store.Person, error) {
	if p, ok := f.personByID[id]; ok {
		return p, nil
	}
	return store.Person{}, pgx.ErrNoRows
}

func (f *fakeStore) GetCurrentSeason(_ context.Context) (store.Season, error) {
	return f.season, f.seasonErr
}

func (f *fakeStore) GetSeasonMember(_ context.Context, _ store.GetSeasonMemberParams) (store.SeasonMember, error) {
	return f.member, f.memberErr
}

// compile-time proof the fake still satisfies the real interface.
var _ Store = (*fakeStore)(nil)

func newPerson(t *testing.T, email, sub string) store.Person {
	t.Helper()
	var subPtr *string
	if sub != "" {
		subPtr = &sub
	}
	return store.Person{
		ID:              uuid.New(),
		Email:           email,
		EmailNormalized: NormalizeEmail(email),
		GoogleSub:       subPtr,
		DisplayName:     "Test Person",
	}
}
