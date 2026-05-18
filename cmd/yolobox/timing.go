package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type timingTracer struct {
	enabled bool
	start   time.Time
	last    time.Time
	mu      sync.Mutex
}

var startupTiming = newTimingTracer()

func newTimingTracer() *timingTracer {
	now := time.Now()
	return &timingTracer{
		enabled: timingEnabledValue(os.Getenv("YOLOBOX_TIMING")),
		start:   now,
		last:    now,
	}
}

func timingEnabledValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && value != "0" && value != "false" && value != "off" && value != "no"
}

func timingEnvForContainer() string {
	if !startupTiming.enabled {
		return ""
	}
	if value := os.Getenv("YOLOBOX_TIMING"); value != "" {
		return value
	}
	return "1"
}

func traceTiming(label string) {
	if !startupTiming.enabled {
		return
	}
	startupTiming.mu.Lock()
	defer startupTiming.mu.Unlock()

	now := time.Now()
	delta := now.Sub(startupTiming.last)
	total := now.Sub(startupTiming.start)
	startupTiming.last = now
	fmt.Fprintf(os.Stderr, "%s[timing]%s +%.3fs total %.3fs %s\n", colorPurple, colorReset, delta.Seconds(), total.Seconds(), label)
}

func traceDuration(label string, started time.Time) {
	if !startupTiming.enabled {
		return
	}
	traceTiming(fmt.Sprintf("%s (%.3fs)", label, time.Since(started).Seconds()))
}

func traceTimed(label string, fn func() error) error {
	started := time.Now()
	err := fn()
	traceDuration(label, started)
	return err
}
