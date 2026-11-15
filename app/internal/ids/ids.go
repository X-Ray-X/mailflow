// Package ids mints the identifiers the public contract exposes.
//
// A Mailflow identifier is an opaque, prefixed, reversible encoding of a mail
// server identifier. The version digit in the prefix allows a future mapping
// table to coexist with identifiers already issued. Nothing here is a security
// boundary: the mail server still decides what the caller may read, so a forged
// identifier only produces a not-found.
package ids

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid is returned for anything that is not a well-formed identifier of
// the expected kind.
var ErrInvalid = errors.New("invalid identifier")

const (
	mailboxPrefix = "mb1_"
	messagePrefix = "msg1_"
	cursorPrefix  = "cur1_"
)

// Mailbox encodes a mail server mailbox identifier.
func Mailbox(vendor string) string { return encode(mailboxPrefix, vendor) }

// Message encodes a mail server message identifier.
func Message(vendor string) string { return encode(messagePrefix, vendor) }

// Cursor encodes a pagination cursor, which points at the last message of the
// page the caller has seen.
func Cursor(vendor string) string { return encode(cursorPrefix, vendor) }

// ParseMailbox returns the mail server identifier behind a Mailflow mailbox
// identifier.
func ParseMailbox(s string) (string, error) { return decode(mailboxPrefix, s, "mailbox") }

// ParseMessage returns the mail server identifier behind a Mailflow message
// identifier.
func ParseMessage(s string) (string, error) { return decode(messagePrefix, s, "message") }

// ParseCursor returns the mail server identifier behind a pagination cursor.
func ParseCursor(s string) (string, error) { return decode(cursorPrefix, s, "cursor") }

func encode(prefix, vendor string) string {
	return prefix + base64.RawURLEncoding.EncodeToString([]byte(vendor))
}

func decode(prefix, s, kind string) (string, error) {
	payload, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", fmt.Errorf("%w: not a Mailflow %s identifier", ErrInvalid, kind)
	}
	if payload == "" {
		return "", fmt.Errorf("%w: empty %s identifier", ErrInvalid, kind)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("%w: malformed %s identifier", ErrInvalid, kind)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("%w: empty %s identifier", ErrInvalid, kind)
	}
	return string(raw), nil
}
