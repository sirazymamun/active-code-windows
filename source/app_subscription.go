package main

import (
	"errors"
	"fmt"
)

var errSubscriptionRejected = errors.New("Activation unavailable. Please check your code or contact your subscription vendor.")

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

// Only a definitive server rejection latches manual activation mode.
func (a *application) shouldMonitorSession() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.token != "" && !a.activationRequired
}
