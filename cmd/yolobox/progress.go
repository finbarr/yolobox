package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

func runWithSpinner(message, done string, fn func() error) error {
	message = strings.TrimSpace(message)
	done = strings.TrimSpace(done)
	if message == "" {
		return fn()
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		info("%s...", message)
		if err := fn(); err != nil {
			return err
		}
		if done != "" {
			success("%s", done)
		}
		return nil
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- fn()
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	printFrame := func(frame string) {
		fmt.Fprintf(os.Stderr, "\r%s%s%s %s...", colorCyan, frame, colorReset, message)
	}
	clearLine := func() {
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}

	printFrame(frames[0])
	frameIndex := 0
	for {
		select {
		case err := <-doneCh:
			clearLine()
			if err != nil {
				return err
			}
			if done != "" {
				success("%s", done)
			}
			return nil
		case <-ticker.C:
			frameIndex = (frameIndex + 1) % len(frames)
			printFrame(frames[frameIndex])
		}
	}
}
