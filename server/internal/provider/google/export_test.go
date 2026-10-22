package google

import "time"

// SetMeetPoll shortens the wait for Meet links in tests.
func SetMeetPoll(d time.Duration) { meetPoll = d }
