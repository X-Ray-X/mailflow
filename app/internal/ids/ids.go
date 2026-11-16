// Package ids mints the identifiers the public contract exposes.
//
// A Mailflow identifier is an opaque, prefixed, reversible encoding of the
// account and the mail server identifier it names. Encoding the account matters:
// the mail server assigns identifiers from a per-account sequence, so two
// accounts routinely hold the same raw identifier for different objects. An
// identifier that did not carry its account would silently resolve to a
// different message for a different caller.
//
// The version digit in the prefix allows a future mapping table to coexist with
// identifiers already issued. Nothing here is a security boundary: the mail
// server still decides what the caller may read, so a forged or foreign
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

	// segmentSeparator is outside the base64url alphabet, so the two encoded
	// segments can never be confused with each other.
	segmentSeparator = "."
)

// Mailbox encodes a mail server mailbox identifier for one account.
func Mailbox(account, vendor string) string { return encode(mailboxPrefix, account, vendor) }

// Message encodes a mail server message identifier for one account.
func Message(account, vendor string) string { return encode(messagePrefix, account, vendor) }

// Cursor encodes a pagination cursor, which points at the last message of the
// page the caller has seen.
func Cursor(account, vendor string) string { return encode(cursorPrefix, account, vendor) }

// ParseMailbox returns the account and mail server identifier behind a Mailflow
// mailbox identifier.
func ParseMailbox(s string) (account, vendor string, err error) {
	return decode(mailboxPrefix, s, "mailbox")
}

// ParseMessage returns the account and mail server identifier behind a Mailflow
// message identifier.
func ParseMessage(s string) (account, vendor string, err error) {
	return decode(messagePrefix, s, "message")
}

// ParseCursor returns the account and mail server identifier behind a
// pagination cursor.
func ParseCursor(s string) (account, vendor string, err error) {
	return decode(cursorPrefix, s, "cursor")
}

func encode(prefix, account, vendor string) string {
	return prefix + encodeSegment(account) + segmentSeparator + encodeSegment(vendor)
}

func encodeSegment(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decode(prefix, s, kind string) (string, string, error) {
	payload, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", "", fmt.Errorf("%w: not a Mailflow %s identifier", ErrInvalid, kind)
	}
	segments := strings.Split(payload, segmentSeparator)
	if len(segments) != 2 {
		return "", "", fmt.Errorf("%w: malformed %s identifier", ErrInvalid, kind)
	}
	account, err := decodeSegment(segments[0], kind)
	if err != nil {
		return "", "", err
	}
	vendor, err := decodeSegment(segments[1], kind)
	if err != nil {
		return "", "", err
	}
	return account, vendor, nil
}

func decodeSegment(segment, kind string) (string, error) {
	if segment == "" {
		return "", fmt.Errorf("%w: empty %s identifier", ErrInvalid, kind)
	}
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return "", fmt.Errorf("%w: malformed %s identifier", ErrInvalid, kind)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("%w: empty %s identifier", ErrInvalid, kind)
	}
	return string(raw), nil
}
