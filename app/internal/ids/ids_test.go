package ids

import (
	"errors"
	"strings"
	"testing"
)

const testAccount = "c"

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		encode func(string, string) string
		decode func(string) (string, string, error)
		prefix string
		vendor string
	}{
		{"mailbox", Mailbox, ParseMailbox, mailboxPrefix, "bmaaaaal"},
		{"message", Message, ParseMessage, messagePrefix, "eaaaaab"},
		{"cursor", Cursor, ParseCursor, cursorPrefix, "biaaaaak"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := tc.encode(testAccount, tc.vendor)
			if !strings.HasPrefix(encoded, tc.prefix) {
				t.Fatalf("%q does not start with %q", encoded, tc.prefix)
			}
			if encoded == tc.vendor {
				t.Fatalf("%q equals the mail server identifier", encoded)
			}
			account, back, err := tc.decode(encoded)
			if err != nil {
				t.Fatalf("decode %q: %v", encoded, err)
			}
			if back != tc.vendor {
				t.Fatalf("round trip = %q, want %q", back, tc.vendor)
			}
			if account != testAccount {
				t.Fatalf("account = %q, want %q", account, testAccount)
			}
		})
	}
}

func TestSameVendorIDInDifferentAccountsDiffers(t *testing.T) {
	// The mail server reuses identifiers across accounts, so the account is
	// what keeps two different objects apart.
	if Mailbox("c", "e") == Mailbox("d", "e") {
		t.Fatal("the same raw identifier in two accounts produced one Mailflow identifier")
	}
	if Message("c", "eaaaaab") == Message("d", "eaaaaab") {
		t.Fatal("the same raw message identifier in two accounts produced one Mailflow identifier")
	}
}

func TestStable(t *testing.T) {
	first := Message(testAccount, "eaaaaab")
	for i := 0; i < 3; i++ {
		if got := Message(testAccount, "eaaaaab"); got != first {
			t.Fatalf("encoding is not stable: %q then %q", first, got)
		}
	}
}

func TestEncodingDiffersPerValue(t *testing.T) {
	if Mailbox(testAccount, "one") == Mailbox(testAccount, "two") {
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
		{"wrong prefix", "msg1_" + encodeSegment(testAccount) + "." + encodeSegment("ZQ"), "mailbox"},
		{"no prefix", "bmaaaaal", "message"},
		{"bare prefix", mailboxPrefix, "mailbox"},
		{"missing separator", mailboxPrefix + encodeSegment("ZQ"), "mailbox"},
		{"missing account", mailboxPrefix + "." + encodeSegment("ZQ"), "mailbox"},
		{"missing object", mailboxPrefix + encodeSegment("ZQ") + ".", "mailbox"},
		{"extra segment", mailboxPrefix + encodeSegment("ZQ") + "." + encodeSegment("ZQ") + "." + encodeSegment("ZQ"), "mailbox"},
		{"invalid base64", mailboxPrefix + "!!!!." + encodeSegment("ZQ"), "mailbox"},
		{"padded base64", mailboxPrefix + "ZQ==." + encodeSegment("ZQ"), "mailbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch tc.kind {
			case "mailbox":
				_, _, err = ParseMailbox(tc.in)
			case "message":
				_, _, err = ParseMessage(tc.in)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCursorCannotBeUsedAsMessage(t *testing.T) {
	if _, _, err := ParseMessage(Cursor(testAccount, "e1")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
