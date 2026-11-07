package main

import "fmt"

// The activation dialog describes the app grant, never the provider label or expiry.
func (a *application) subscriptionNotice() string {
	a.mu.RLock()
	lease := a.lease
	a.mu.RUnlock()
	if !lease.Allowed || lease.RemainingSeconds <= 0 {
		return ""
	}
	days := (lease.RemainingSeconds + 86399) / 86400
	return fmt.Sprintf("active-code · %d day(s) remaining", days)
}
