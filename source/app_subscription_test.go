package main

import "testing"

func TestAppSubscriptionNotice(t *testing.T) {
	for _, tc := range []struct {
		seconds int64
		allowed bool
		want    string
	}{
		{13 * 86400, true, "active-code · 13 day(s) remaining"},
		{12*86400 + 1, true, "active-code · 13 day(s) remaining"},
		{1, true, "active-code · 1 day(s) remaining"},
		{0, true, ""},
		{13 * 86400, false, ""},
	} {
		a := &application{lease: grant{Allowed: tc.allowed, RemainingSeconds: tc.seconds, Label: "amdertv"}}
		if got := a.subscriptionNotice(); got != tc.want {
			t.Fatalf("notice = %q; want %q", got, tc.want)
		}
	}
}
