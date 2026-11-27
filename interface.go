// Package notify provides notification functionality.
package notify

import (
	"context"
	"fmt"
	"strings"
)

// Notifier defines common interface among all notifiers
type Notifier interface {
	fmt.Stringer
	Schema() string                                           // returns the scheme supported by this client, without the colon
	Send(ctx context.Context, destination, text string) error // sends message to provided destination
}

// Send passes the destination to the first notifier whose scheme matches the scheme of the destination,
// compared case-insensitively, and returns its error. A notifier with the "http" scheme also takes "https"
// destinations. Without a match, Send returns an "unsupported destination schema" error.
func Send(ctx context.Context, notifiers []Notifier, destination, text string) error {
	scheme, _, found := strings.Cut(destination, ":")
	if !found {
		return fmt.Errorf("unsupported destination schema: %s", destination)
	}
	for _, n := range notifiers {
		if schemeMatches(scheme, n.Schema()) {
			return n.Send(ctx, destination, text)
		}
	}
	return fmt.Errorf("unsupported destination schema: %s", scheme)
}

// schemeMatches reports whether a destination with the given scheme goes to a notifier with the given schema.
// A schema reported with its separator, as "mailto:" or "https://", matches as well. A bare "http" also
// takes https, as the prefix it was; "http:" and "http://" never did.
func schemeMatches(scheme, schema string) bool {
	if strings.EqualFold(schema, "http") && strings.EqualFold(scheme, "https") {
		return true
	}
	return strings.EqualFold(scheme, strings.TrimSuffix(strings.TrimSuffix(schema, "://"), ":"))
}
