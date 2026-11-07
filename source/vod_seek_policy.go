package main

import (
	"fmt"
	"math"
	"time"
)

const vodSeekStepSeconds int64 = 10
const vodSeekHoldDelay = 400 * time.Millisecond
const vodSeekHoldInterval = 250 * time.Millisecond

func canUseVodArrow(item channel, listsShown bool, key uintptr) bool {
	return !listsShown && (key == 0x25 || key == 0x27) && (item.Type == "Movies" || item.Type == "Series") && item.URL != "" && item.SeriesID == ""
}

type vodSeekHold struct {
	active           bool
	key              uintptr
	requestID, token uint64
	started          time.Time
	seconds          int64
}

func newVodSeekHold(key uintptr, request, token uint64, now time.Time) vodSeekHold {
	sign := int64(1)
	if key == 0x25 {
		sign = -1
	}
	return vodSeekHold{active: true, key: key, requestID: request, token: token, started: now, seconds: sign * vodSeekStepSeconds}
}
func (h *vodSeekHold) advance(now time.Time) bool {
	if !h.active {
		return false
	}
	elapsed := now.Sub(h.started)
	steps := int64(1)
	if elapsed >= vodSeekHoldDelay {
		steps += int64((elapsed-vodSeekHoldDelay)/vodSeekHoldInterval) + 1
	}
	seconds := steps * vodSeekStepSeconds
	if h.key == 0x25 {
		seconds = -seconds
	}
	changed := seconds != h.seconds
	h.seconds = seconds
	return changed
}
func boundedVodSeek(position, totalSeconds, duration int64) int64 {
	position = max(int64(0), position)
	var delta int64
	if totalSeconds > math.MaxInt64/1000 {
		delta = math.MaxInt64
	} else if totalSeconds < math.MinInt64/1000 {
		delta = math.MinInt64
	} else {
		delta = totalSeconds * 1000
	}
	target := position
	if delta > 0 && position > math.MaxInt64-delta {
		target = math.MaxInt64
	} else if delta < 0 && position < math.MinInt64-delta {
		target = 0
	} else {
		target = position + delta
	}
	target = max(int64(0), target)
	if duration > 0 {
		target = min(target, max(int64(0), duration-1000))
	}
	return target
}

type vodSeekPlan struct {
	request, token     uint64
	origin, lastTarget int64
	valid              bool
	lastApplied        time.Time
}

func (p *vodSeekPlan) target(request, token uint64, position, totalSeconds, duration int64, now time.Time) int64 {
	if !p.valid || p.request != request || p.token != token {
		if p.request != request {
			p.lastApplied = time.Time{}
		}
		origin := max(int64(0), position)
		if p.valid && p.request == request && !p.lastApplied.IsZero() && now.Sub(p.lastApplied) < 500*time.Millisecond {
			origin = p.lastTarget
		}
		p.request = request
		p.token = token
		p.origin = origin
		p.valid = true
	}
	return boundedVodSeek(p.origin, totalSeconds, duration)
}
func (p *vodSeekPlan) applied(target int64, now time.Time) {
	p.lastTarget = target
	p.lastApplied = now
}

func seekTimeLabel(ms int64) string {
	seconds := max(int64(0), ms) / 1000
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, (seconds/60)%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
