package models

import "testing"

// TestPersonHasMinimumIdentity exercises the Phase-1 identity-minimum
// invariant: a Person row that has neither a name nor a primary email
// must be rejected by the service layer. The repository accepts
// anything (importer staging), so this helper is the canonical gate.
func TestPersonHasMinimumIdentity(t *testing.T) {
	cases := []struct {
		name string
		p    Person
		want bool
	}{
		{
			name: "empty person fails",
			p:    Person{},
			want: false,
		},
		{
			name: "first name only is sufficient",
			p:    Person{FirstName: "Jane"},
			want: true,
		},
		{
			name: "last name only is sufficient",
			p:    Person{LastName: "Doe"},
			want: true,
		},
		{
			name: "both names is sufficient",
			p:    Person{FirstName: "Jane", LastName: "Doe"},
			want: true,
		},
		{
			name: "primary email alone is sufficient",
			p: Person{
				Emails: []EmailEntry{{Address: "j@example.com", Primary: true}},
			},
			want: true,
		},
		{
			name: "non-primary email alone is NOT sufficient",
			p: Person{
				Emails: []EmailEntry{{Address: "j@example.com", Primary: false}},
			},
			want: false,
		},
		{
			name: "primary email with empty address is NOT sufficient",
			p: Person{
				Emails: []EmailEntry{{Address: "", Primary: true}},
			},
			want: false,
		},
		{
			name: "both name and email is sufficient",
			p: Person{
				FirstName: "Jane",
				Emails:    []EmailEntry{{Address: "j@example.com", Primary: true}},
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.HasMinimumIdentity(); got != c.want {
				t.Errorf("HasMinimumIdentity() = %v, want %v", got, c.want)
			}
		})
	}
}
