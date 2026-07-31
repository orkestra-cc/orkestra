package blob

import "testing"

func TestContentDispositionAttachment(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
	}{
		{
			name: "plain ascii with spaces",
			in:   "Atlante Perugia 2025.pdf",
			want: `attachment; filename="Atlante Perugia 2025.pdf"; filename*=UTF-8''Atlante%20Perugia%202025.pdf`,
		},
		{
			name: "accented italian falls back to _ in ascii, pct-encoded in utf8",
			in:   "Relazione Attività.pdf",
			// "à" (U+00E0) is 0xC3 0xA0 in UTF-8 -> %C3%A0; it becomes '_' in the ASCII form.
			want: `attachment; filename="Relazione Attivit_.pdf"; filename*=UTF-8''Relazione%20Attivit%C3%A0.pdf`,
		},
		{
			name: "quote and backslash are neutralised in the ascii form",
			in:   `a"b\c.pdf`,
			want: `attachment; filename="a_b_c.pdf"; filename*=UTF-8''a%22b%5Cc.pdf`,
		},
		{
			name: "blank returns empty (caller presigns without a disposition)",
			in:   "   ",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contentDispositionAttachment(tc.in); got != tc.want {
				t.Fatalf("contentDispositionAttachment(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}
