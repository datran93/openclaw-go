// Package cli provides a stdin/stdout channel adapter for OpenClaw.
// It reads user input line-by-line and prints streamed responses to stdout.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/openclaw/openclaw-go/internal/channels"
)

const adapterName = "cli"

// scanResult carries a scanned line or a termination signal.
type scanResult struct {
	text string
	ok   bool
}

// Adapter implements channels.Adapter for a terminal (stdin → stdout) session.
type Adapter struct {
	sessionID string
	in        io.Reader
	out       io.Writer
}

// New creates a CLI Adapter.
// sessionID is a stable key for the conversation (default: "cli-default").
// Pass nil for in/out to use os.Stdin / os.Stdout.
func New(sessionID string, in io.Reader, out io.Writer) *Adapter {
	if sessionID == "" {
		sessionID = "cli-default"
	}
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &Adapter{sessionID: sessionID, in: in, out: out}
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string { return adapterName }

// Start reads lines from the input reader and pushes them as IncomingMessages.
// It returns when ctx is cancelled or the input reaches EOF.
//
// Goroutine leak prevention: a single scanner goroutine is started once and
// sends all lines into a buffered channel. The main loop selects between
// ctx.Done() and that channel. When ctx is cancelled we return immediately;
// the scanner goroutine will unblock on its next Scan() call (which returns
// false on EOF / pipe close) and exit naturally when the process finishes.
// For tests, the caller should close the write end of the pipe so Scan() exits.
func (a *Adapter) Start(ctx context.Context, in chan<- channels.IncomingMessage) error {
	scanner := bufio.NewScanner(a.in)
	// Buffer the scan results so the scanner goroutine never blocks on send.
	scanCh := make(chan scanResult, 4)

	// Single scanner goroutine — lives for the duration of Start().
	go func() {
		defer close(scanCh)
		for scanner.Scan() {
			scanCh <- scanResult{text: scanner.Text(), ok: true}
		}
		if err := scanner.Err(); err != nil {
			slog.Error("cli: scanner error", "error", err)
		}
		// Signal EOF / error.
		scanCh <- scanResult{ok: false}
	}()

	fmt.Fprintln(a.out, "OpenClaw CLI ready. Type a message and press Enter. Ctrl+C to quit.")

	for {
		fmt.Fprint(a.out, "> ")

		select {
		case <-ctx.Done():
			return nil
		case res, chanOpen := <-scanCh:
			if !chanOpen || !res.ok {
				return nil // EOF or scanner closed
			}
			text := strings.TrimSpace(res.text)
			if text == "" {
				continue
			}
			select {
			case in <- channels.IncomingMessage{
				ChannelID: adapterName,
				SessionID: a.sessionID,
				UserID:    "local-user",
				Text:      text,
			}:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// Send writes a response chunk to the output writer.
// Streaming chunks are printed inline; the final sentinel (Streaming=false)
// prints a trailing newline to complete the line.
func (a *Adapter) Send(_ context.Context, msg channels.OutgoingMessage) error {
	if msg.Streaming {
		fmt.Fprint(a.out, msg.Text)
		return nil
	}
	// Final message — end the current line.
	fmt.Fprintln(a.out)
	return nil
}

// Stop is a no-op for the CLI adapter (stdin is closed by the process or test).
func (a *Adapter) Stop() error { return nil }
