package browser

import (
	"math/rand"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
)

// Humanize provides human-like input methods for browser interactions.
// All methods add random delays and jitter to mimic real typing and clicking.

// TypeHuman types text into an element character by character with realistic
// keystroke jitter (50-150ms between characters). This produces typing cadence
// that is harder for anti-bot systems to distinguish from a real user.
func TypeHuman(el *rod.Element, text string) error {
	for _, ch := range text {
		if err := el.Input(string(ch)); err != nil {
			return err
		}
		// Human keystroke interval: 50-150ms with random variation.
		jitter := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(jitter)
	}
	return nil
}

// TypeHumanAndSubmit types text with human-like jitter then presses Enter.
func TypeHumanAndSubmit(el *rod.Element, text string) error {
	if err := TypeHuman(el, text); err != nil {
		return err
	}
	// Slight pause before hitting Enter (human reaches for the key).
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
	return el.Type(input.Enter)
}

// ClickHuman clicks an element after a short random delay (100-400ms),
// mimicking the pause a real user has between deciding to click and acting.
func ClickHuman(el *rod.Element) error {
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)
	return el.Click("left", 1)
}

// HumanDelay pauses for a random duration between min and max milliseconds.
// Use between sequential actions to break up robotic timing patterns.
func HumanDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		maxMs = minMs + 1
	}
	time.Sleep(time.Duration(minMs+rand.Intn(maxMs-minMs)) * time.Millisecond)
}
