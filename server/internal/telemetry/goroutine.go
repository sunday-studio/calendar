package telemetry

import (
	"context"
	"runtime/debug"
	"time"
)

// Safe runs fn and recovers a panic: the panic is recorded (exception event with stack, calendar.errors
// kind=panic, calendar.goroutine.panics{name}) and Safe returns true. Use it around the body of one loop
// iteration so a bug in one pass does not end the loop.
func Safe(ctx context.Context, name string, fn func()) (panicked bool) {
	defer func() {
		if v := recover(); v != nil {
			panicked = true
			RecordPanic(ctx, name, v, debug.Stack())
			Inc(ctx, "goroutine.panics", Attr("goroutine", name))
			if Enabled() {
				logger().ErrorContext(ctx, "background goroutine panicked; recovered", "component", word(name, "other"), "kind", KindPanic)
			}
		}
	}()
	fn()
	return false
}

// Go starts fn in a goroutine that cannot take the process down: a panic is recovered and recorded (see
// Safe) and the goroutine ends. Use Supervise for loops that must keep running.
func Go(name string, fn func()) {
	go Safe(context.Background(), name, fn)
}

// Supervise runs fn until ctx is cancelled or fn returns normally. If fn panics it is recorded and
// restarted after a backoff (1 s doubling to 1 min, reset after a run that lasted over a minute).
// It blocks; call it as `go telemetry.Supervise(...)`.
func Supervise(ctx context.Context, name string, fn func(ctx context.Context)) {
	delay := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		if !Safe(ctx, name, func() { fn(ctx) }) {
			return
		}
		Inc(ctx, "goroutine.restarts", Attr("goroutine", name))
		if time.Since(start) > time.Minute {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < time.Minute {
			delay *= 2
		}
	}
}
