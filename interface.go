package notify

import (
	"context"
	"fmt"
)

// service interface, just to verify that all notifiers implement it
type notifier interface {
	fmt.Stringer
	Send(context.Context, string) error
}
