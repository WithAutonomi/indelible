package services

import "testing"

func TestIsValidEmail(t *testing.T) {
	valid := []string{"user@example.com", "a@b.co", "first.last@sub.domain.org"}
	for _, e := range valid {
		if !IsValidEmail(e) {
			t.Errorf("IsValidEmail(%q) = false, want true", e)
		}
	}
	invalid := []string{"", "notanemail", "no-at-sign.com", "foo <bar@baz.com>", "two@@at.com", "spaces in@email.com"}
	for _, e := range invalid {
		if IsValidEmail(e) {
			t.Errorf("IsValidEmail(%q) = true, want false", e)
		}
	}
}
