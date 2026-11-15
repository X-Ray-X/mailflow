package ids

import (
	"errors"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		encode func(string) string
		decode func(string) (string, error)
		prefix string
		vendor string
	}{
		{"mailbox", Mailbox, ParseMailbox, mailboxPrefix, "bmaaaaal"},
		{"message", Message, ParseMessage, messagePrefix, "eaaaaab"},
		{"cursor", Cursor, ParseCursor, cursorPrefix, "biaaaaak"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := tc.encode(tc.vendor)
			if !strings.HasPrefix(encoded, tc.prefix) {
				t.Fatalf("%q does not start with %q", encoded, tc.prefix)
			}
			if encoded == tc.vendor {
				t.Fatalf("%q equals the mail server identifier", encoded)
			}
			back, err := tc.decode(encoded)
			if err != nil {
				t.Fatalf("decode %q: %v", encoded, err)
			}
			if back != tc.vendor {
				t.Fatalf("round trip = %q, want %q", back, tc.vendor)
			}
		})
	}
}

func TestStable(t *testing.T) {
	first := Message("eaaaaab")
	for i := 0; i < 3; i++ {
		if got := Message("eaaaaab"); got != first {
			t.Fatalf("encoding is not stable: %q then %q", first, got)
		}
	}
}

func TestEncodingDiffersPerValue(t *testing.T) {
	if Mailbox("one") == Mailbox("two") {
		t.Fatal("two mailboxes share an identifier")
	}
}

func TestRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   string
		kind string
	}{
		{"empty", "", "mailbox"},
		{"wrong prefix", "msg1_" + "ZQ", "mailbox"},
		{"no prefix", "bmaaaaal", "message"},
		{"bare prefix", mailboxPrefix, "mailbox"},
		{"invalid base64", mailboxPrefix + "!!!!", "mailbox"},
		{"padded base64", mailboxPrefix + "ZQ==", "mailbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch tc.kind {
			case "mailbox":
				_, err = ParseMailbox(tc.in)
			case "message":
				_, err = ParseMessage(tc.in)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCursorCannotBeUsedAsMessage(t *testing.T) {
	if _, err := ParseMessage(Cursor("e1")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
