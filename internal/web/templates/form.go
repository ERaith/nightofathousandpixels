package templates

import (
	"strings"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// formField is one control's labelling, as the markup needs it.
//
// It exists because the accessible parts of a form field are not the input —
// they are the four ids that have to agree about it. The label points at the
// control, the control points back at its hint and at its error, and the error
// summary at the top of the form points at the control again. Written out four
// times per form by hand, one of those four eventually disagrees with the other
// three, and the failure is silent: the error is on screen, correctly coloured,
// and simply never announced.
//
// So the ids are derived from one stem, in one place, and every template that
// needs one asks for it.
type formField struct {
	// ID is the control's id attribute and the stem for the ids of its hint
	// and its error message. It is also what the error summary links to.
	ID string

	// Name is the form field name that posts, one of viewmodel's Field*
	// constants.
	Name string

	// Label is the visible label. The error summary reuses it, so it has to
	// read on its own: "Title", not "It".
	Label string

	// Hint is the line under the label. Every field on this form has one,
	// which is what keeps DescribedBy from ever being empty.
	Hint string

	// Value is what goes back in the box — the text the person typed, not a
	// parsed value. See viewmodel.SubmitForm.
	Value string

	// Error is the validation message against this field, or "" when it is
	// fine.
	Error string
}

// HintID is the id of the hint paragraph.
func (f formField) HintID() string {
	if f.Hint == "" {
		return ""
	}

	return f.ID + "-hint"
}

// ErrorID is the id of the error paragraph, or "" when there is no error and
// so no element to point at.
func (f formField) ErrorID() string {
	if f.Error == "" {
		return ""
	}

	return f.ID + "-error"
}

// Invalid reports whether this field carries a validation message.
func (f formField) Invalid() bool {
	return f.Error != ""
}

// AriaInvalid is the aria-invalid attribute value.
//
// It renders "false" rather than omitting the attribute, because the attribute
// is then always present and always current. An input that keeps a stale
// aria-invalid="true" is worse than one that says "false" out loud.
func (f formField) AriaInvalid() string {
	if f.Invalid() {
		return "true"
	}

	return "false"
}

// DescribedBy is the aria-describedby value: the hint, and the error when
// there is one.
//
// Order matters to a screen reader, which reads these in the order given. The
// hint comes first because it is what the field is; the error second because
// it is what is wrong with it this time.
//
// It never returns "" for this form's fields, since all four carry a hint. An
// empty aria-describedby is not harmful, but an attribute pointing at nothing
// is the kind of thing that survives into a form where it does matter.
func (f formField) DescribedBy() string {
	ids := make([]string, 0, 2)
	for _, id := range []string{f.HintID(), f.ErrorID()} {
		if id != "" {
			ids = append(ids, id)
		}
	}

	return strings.Join(ids, " ")
}

// ErrorHref is the link target the error summary uses to jump to this control.
func (f formField) ErrorHref() string {
	return "#" + f.ID
}

// submitFields is the submit form's four controls, in the order they appear on
// the page.
//
// It is a struct rather than a slice so the template names each field instead
// of indexing it. The markup for a year is not the markup for a description —
// different input type, different keyboard on a phone — so they cannot come
// out of a loop, and fields[1] in a template is exactly the sort of thing that
// breaks when somebody reorders the form.
type submitFields struct {
	Title       formField
	Year        formField
	TrailerURL  formField
	Description formField
}

// newSubmitFields builds the four fields from the form's values and errors.
//
// This is the only place a field name is looked up in FieldErrors, which is
// what the viewmodel.Field* constants are for: the handler records against the
// constant and the template reads back with the same constant, so a typo is a
// compile error rather than a message that silently never appears.
func newSubmitFields(t Theme, form SubmitForm) submitFields {
	return submitFields{
		Title: formField{
			ID:    "submit-title",
			Name:  viewmodel.FieldTitle,
			Label: t.Text(viewmodel.KeySubmitTitleLabel),
			Hint:  t.Text(viewmodel.KeySubmitTitleHint),
			Value: form.Title,
			Error: form.Errors.For(viewmodel.FieldTitle),
		},
		Year: formField{
			ID:    "submit-year",
			Name:  viewmodel.FieldYear,
			Label: t.Text(viewmodel.KeySubmitYearLabel),
			Hint:  t.Text(viewmodel.KeySubmitYearHint),
			Value: form.Year,
			Error: form.Errors.For(viewmodel.FieldYear),
		},
		TrailerURL: formField{
			ID:    "submit-trailer-url",
			Name:  viewmodel.FieldTrailerURL,
			Label: t.Text(viewmodel.KeySubmitTrailerLabel),
			Hint:  t.Text(viewmodel.KeySubmitTrailerHint),
			Value: form.TrailerURL,
			Error: form.Errors.For(viewmodel.FieldTrailerURL),
		},
		Description: formField{
			ID:    "submit-description",
			Name:  viewmodel.FieldDescription,
			Label: t.Text(viewmodel.KeySubmitDescriptionLabel),
			Hint:  t.Text(viewmodel.KeySubmitDescriptionHint),
			Value: form.Description,
			Error: form.Errors.For(viewmodel.FieldDescription),
		},
	}
}

// All is the four fields in page order.
func (f submitFields) All() []formField {
	return []formField{f.Title, f.Year, f.TrailerURL, f.Description}
}

// Invalid is the fields carrying a message, in page order.
//
// The order is the form's own, top to bottom, which is what an error summary
// has to read in (WCAG 3.3.1): the list should walk down the form the way the
// eye does. viewmodel.FieldErrors is a slice for the same reason — so that
// nothing here has to sort a map to get a stable order.
func (f submitFields) Invalid() []formField {
	bad := make([]formField, 0, 4)
	for _, field := range f.All() {
		if field.Invalid() {
			bad = append(bad, field)
		}
	}

	return bad
}
