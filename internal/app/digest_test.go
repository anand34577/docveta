package app

import "testing"

func TestDigestBody(t *testing.T) {
	for _, c := range []struct {
		added, inbox, failed, upcoming int
		want                           string
	}{
		{0, 0, 0, 0, ""},
		{1, 0, 0, 0, "1 document added."},
		{3, 1, 2, 1, "3 documents added · 1 waits for review in the Inbox · 2 need attention · 1 date coming up in the next two weeks."},
	} {
		if got := digestBody(c.added, c.inbox, c.failed, c.upcoming); got != c.want {
			t.Errorf("digestBody(%d,%d,%d,%d) = %q, want %q", c.added, c.inbox, c.failed, c.upcoming, got, c.want)
		}
	}
}
