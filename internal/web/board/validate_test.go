package board

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// TestValidateAcceptsTheMinimumAndTheMaximum: a title on its own is a whole
// submission, and a fully filled form is too.
func TestValidateAcceptsTheMinimumAndTheMaximum(t *testing.T) {
	t.Run("a title alone", func(t *testing.T) {
		d, errs := validate(viewmodel.SubmitForm{Title: "  The Thing  "})

		if errs.Any() {
			t.Fatalf("rejected a valid submission: %+v", errs)
		}
		if d.title != "The Thing" {
			t.Errorf("title = %q, want %q (surrounding whitespace trimmed for storage)", d.title, "The Thing")
		}
		// The three optional columns are nullable, and nil is SQL NULL. A
		// pointer to the empty string would write '' into trailer_url, which
		// the movie_trailer_url_shape CHECK then rejects.
		if d.year != nil {
			t.Errorf("year = %v, want nil for a blank field", *d.year)
		}
		if d.trailerURL != nil {
			t.Errorf("trailerURL = %q, want nil for a blank field", *d.trailerURL)
		}
	})

	t.Run("every field", func(t *testing.T) {
		d, errs := validate(viewmodel.SubmitForm{
			Title:       "The Thing",
			Year:        "1982",
			TrailerURL:  "https://www.youtube.com/watch?v=4rgXFbZjTU8",
			Description: "Practical effects, no CGI, and nobody trusts anybody.",
		})

		if errs.Any() {
			t.Fatalf("rejected a valid submission: %+v", errs)
		}
		if d.year == nil || *d.year != 1982 {
			t.Errorf("year = %v, want 1982", d.year)
		}
		if d.trailerURL == nil || *d.trailerURL != "https://www.youtube.com/watch?v=4rgXFbZjTU8" {
			t.Errorf("trailerURL = %v", d.trailerURL)
		}
	})
}

// TestValidateFieldErrors walks the ways a form goes wrong, one field at a
// time, and checks that the message lands against the field it is about --
// which is what puts it next to the right input and in the right place in the
// error summary.
func TestValidateFieldErrors(t *testing.T) {
	cases := []struct {
		name  string
		form  viewmodel.SubmitForm
		field string
	}{
		{"a blank title", viewmodel.SubmitForm{Title: ""}, viewmodel.FieldTitle},
		{"a whitespace title", viewmodel.SubmitForm{Title: "   \t\n "}, viewmodel.FieldTitle},
		{"an enormous title", viewmodel.SubmitForm{Title: strings.Repeat("a", maxTitleLen+1)}, viewmodel.FieldTitle},
		{"a year in words", viewmodel.SubmitForm{Title: "Alien", Year: "nineteen eighty-four"}, viewmodel.FieldYear},
		{"a year before film existed", viewmodel.SubmitForm{Title: "Alien", Year: "1887"}, viewmodel.FieldYear},
		{"a year past the CHECK", viewmodel.SubmitForm{Title: "Alien", Year: "2201"}, viewmodel.FieldYear},
		{"a trailer with no scheme", viewmodel.SubmitForm{Title: "Alien", TrailerURL: "youtube.com/watch?v=x"}, viewmodel.FieldTrailerURL},
		{"a trailer with the wrong scheme", viewmodel.SubmitForm{Title: "Alien", TrailerURL: "javascript:alert(1)"}, viewmodel.FieldTrailerURL},
		// "http://" satisfies the movie_trailer_url_shape regex and is still
		// not a link. The CHECK cannot tell; this can.
		{"a scheme and nothing else", viewmodel.SubmitForm{Title: "Alien", TrailerURL: "http://"}, viewmodel.FieldTrailerURL},
		{"an enormous description", viewmodel.SubmitForm{Title: "Alien", Description: strings.Repeat("d", maxDescriptionLen+1)}, viewmodel.FieldDescription},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := validate(tc.form)

			if !errs.Has(tc.field) {
				t.Fatalf("no message against %q; got %+v", tc.field, errs)
			}
			if msg := errs.For(tc.field); msg == "" || !strings.HasSuffix(strings.TrimSpace(msg), ".") {
				t.Errorf("message against %q is not a sentence: %q", tc.field, msg)
			}
		})
	}
}

// TestValidateReportsEveryBadFieldAtOnce: a form with three problems comes
// back with three messages, not the first one.
//
// One round trip per mistake is the thing that makes people give up on a form.
func TestValidateReportsEveryBadFieldAtOnce(t *testing.T) {
	_, errs := validate(viewmodel.SubmitForm{
		Title:      "",
		Year:       "nope",
		TrailerURL: "not a url",
	})

	for _, field := range []string{viewmodel.FieldTitle, viewmodel.FieldYear, viewmodel.FieldTrailerURL} {
		if !errs.Has(field) {
			t.Errorf("no message against %q", field)
		}
	}
	if len(errs) != 3 {
		t.Errorf("got %d messages, want 3: %+v", len(errs), errs)
	}
}

// TestValidateDoesNotTouchTheForm is the promise the submit page depends on:
// what goes back in the boxes is what was typed.
//
// viewmodel.SubmitForm keeps every value as a string precisely so that
// "nineteen eighty-four" has somewhere to live on the way back. A validator
// that tidied the form as it checked it would redisplay the field as empty,
// which reads as the site having eaten the input.
func TestValidateDoesNotTouchTheForm(t *testing.T) {
	typed := viewmodel.SubmitForm{
		Title:       "  The Thing  ",
		Year:        "nineteen eighty-four",
		TrailerURL:  " not a url ",
		Description: "  leading space is theirs  ",
	}
	before := typed

	if _, errs := validate(typed); !errs.Any() {
		t.Fatal("expected this form to be rejected")
	}

	// Field by field rather than with ==, because SubmitForm carries a slice
	// of errors and is not a comparable type.
	for _, f := range []struct {
		name      string
		got, want string
	}{
		{"Title", typed.Title, before.Title},
		{"Year", typed.Year, before.Year},
		{"TrailerURL", typed.TrailerURL, before.TrailerURL},
		{"Description", typed.Description, before.Description},
	} {
		if f.got != f.want {
			t.Errorf("validate modified %s: got %q, want %q", f.name, f.got, f.want)
		}
	}
	if typed.Errors.Any() {
		t.Error("validate wrote its messages into the form it was given")
	}
}

// TestValidateCountsCharactersNotBytes: a title of emoji or of Japanese is a
// real film title, and a byte-length cap would reject a third of one.
func TestValidateCountsCharactersNotBytes(t *testing.T) {
	// Well under maxTitleLen characters, well over it in bytes.
	title := strings.Repeat("七", maxTitleLen-1)

	if _, errs := validate(viewmodel.SubmitForm{Title: title}); errs.Any() {
		t.Errorf("rejected a %d-character title: %+v", len([]rune(title)), errs)
	}
}
