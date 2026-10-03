package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"golang.org/x/term"

	"github.com/wavever/CCLimitPing/internal/update"
)

// The update notice runs before a command's own output, the way the Codex CLI
// does it, so a new release is seen rather than scrolled past. It is offered
// only on an interactive terminal and only from commands a person is watching:
// `hook` must stay fast and silent, `watch`/`bg start` may have no terminal at
// all, and `--json` output has to stay machine-readable.

var updateHTTPClient = http.DefaultClient

// updateNotice asks about a newer release, if there is one. Every failure path
// is silent: a version check must never get in the way of the command the user
// actually ran.
func updateNotice(ctx context.Context, out io.Writer, text cliText, in *os.File) {
	if !isTerminal(os.Stdout) || !isTerminal(in) || !isReleaseVersion() {
		return
	}
	latest := update.Latest(ctx, updateHTTPClient)
	next := update.Available(version(), latest, update.Load().DismissedVersion)
	if next == "" {
		return
	}
	promptUpdate(out, text, in, next)
}

// updateChoice is what the notice's menu resolves to.
type updateChoice int

const (
	updateChoiceUpgrade updateChoice = iota
	updateChoiceSkip
	updateChoiceDismiss
)

// promptUpdate renders the notice and applies the choice.
func promptUpdate(out io.Writer, text cliText, in io.Reader, next string) {
	fmt.Fprintf(out, text.updateAvailableFmt, update.Normalize(version()), next)
	fmt.Fprintf(out, text.updateNotesFmt, update.ReleaseNotesURL)

	switch selectUpdateOption(out, text, in, invokedName()+" upgrade") {
	case updateChoiceUpgrade:
		fmt.Fprintln(out)
		if err := runUpgrade(context.Background(), out, out); err != nil {
			fmt.Fprintf(out, text.updateFailedFmt, err)
		}
	case updateChoiceDismiss:
		if err := update.Dismiss(next); err != nil {
			fmt.Fprintf(out, text.updateFailedFmt, err)
		} else {
			fmt.Fprintf(out, text.updateDismissedFmt, next)
		}
	}
	fmt.Fprintln(out)
}

// selectUpdateOption runs the arrow-key menu and returns the chosen option. The
// cursor starts on Skip: this notice interrupts a command the user actually
// asked for, so the keypress that costs the least thought has to be the one
// that changes nothing. Digits still pick an option outright, and anything that
// reads as "get out of my way" — Ctrl-C, Esc, a closed input — lands on Skip
// too.
func selectUpdateOption(out io.Writer, text cliText, in io.Reader, upgradeCmd string) updateChoice {
	labels := []string{
		fmt.Sprintf(text.updateOptionUpgrade, upgradeCmd),
		text.updateOptionSkip,
		text.updateOptionSkipVersion,
	}
	// Raw mode is what makes a keypress arrive without Enter behind it. Off a
	// real terminal (tests) the same decoding just runs over the bytes given.
	if f, ok := in.(*os.File); ok {
		if state, err := term.MakeRaw(int(f.Fd())); err == nil {
			defer func() { _ = term.Restore(int(f.Fd()), state) }()
		}
	}

	keys := bufio.NewReader(in)
	sel := int(updateChoiceSkip)
	for first := true; ; first = false {
		drawUpdateMenu(out, text, labels, sel, !first)
		key, digit := readMenuKey(keys)
		switch key {
		case menuKeyUp:
			sel = (sel + len(labels) - 1) % len(labels)
		case menuKeyDown:
			sel = (sel + 1) % len(labels)
		case menuKeyDigit:
			if digit >= len(labels) {
				continue // no such option; keep the menu up
			}
			sel = digit
			fallthrough
		case menuKeyEnter:
			collapseUpdateMenu(out, labels, sel)
			return updateChoice(sel)
		case menuKeyCancel:
			sel = int(updateChoiceSkip)
			collapseUpdateMenu(out, labels, sel)
			return updateChoiceSkip
		}
	}
}

// updateMenuHeight is how many terminal lines the menu occupies: one per
// option, a blank line, and the key hint.
func updateMenuHeight(options int) int { return options + 2 }

// drawUpdateMenu paints the option list with the cursor on sel. A redraw first
// walks back over the previous frame and clears it, so the menu updates in
// place instead of scrolling a new copy into the terminal on every keypress.
// Lines end in CRLF because raw mode has turned off the newline translation
// that would otherwise return the carriage for us.
func drawUpdateMenu(out io.Writer, text cliText, labels []string, sel int, redraw bool) {
	if redraw {
		fmt.Fprintf(out, "\r\x1b[%dA\x1b[J", updateMenuHeight(len(labels)))
	}
	for i, label := range labels {
		fmt.Fprintf(out, "   %s%d. %s\r\n", menuCursor(i == sel), i+1, label)
	}
	fmt.Fprintf(out, "\r\n   %s\r\n", text.updateChooseHint)
}

// collapseUpdateMenu replaces the menu with the single line that was chosen, so
// the scrollback records the decision instead of a dead list of options.
func collapseUpdateMenu(out io.Writer, labels []string, sel int) {
	fmt.Fprintf(out, "\r\x1b[%dA\x1b[J", updateMenuHeight(len(labels)))
	fmt.Fprintf(out, "   %s%d. %s\r\n", menuCursor(true), sel+1, labels[sel])
}

func menuCursor(selected bool) string {
	if selected {
		return "❯ "
	}
	return "  "
}

type menuKey int

const (
	menuKeyOther menuKey = iota
	menuKeyUp
	menuKeyDown
	menuKeyEnter
	menuKeyDigit
	menuKeyCancel
)

// readMenuKey decodes one keypress. The digit it returns is zero-based and only
// meaningful for menuKeyDigit.
func readMenuKey(keys *bufio.Reader) (menuKey, int) {
	b, err := keys.ReadByte()
	if err != nil {
		return menuKeyCancel, 0
	}
	switch b {
	case '\r', '\n':
		return menuKeyEnter, 0
	case 0x03, 0x04: // Ctrl-C, Ctrl-D
		return menuKeyCancel, 0
	case 0x1b:
		return readEscapeSequence(keys)
	}
	if b >= '1' && b <= '9' {
		return menuKeyDigit, int(b - '1')
	}
	return menuKeyOther, 0
}

// readEscapeSequence resolves what followed an Esc byte. An arrow key arrives
// as one burst, so by the time the Esc has been read its remaining bytes are
// already buffered; nothing behind it means Esc was pressed on its own, which
// is a request to dismiss rather than the start of a sequence to wait for.
func readEscapeSequence(keys *bufio.Reader) (menuKey, int) {
	if keys.Buffered() == 0 {
		return menuKeyCancel, 0
	}
	// CSI (\x1b[) in normal cursor mode, SS3 (\x1bO) in application mode.
	if intro, err := keys.ReadByte(); err != nil || (intro != '[' && intro != 'O') {
		return menuKeyOther, 0
	}
	final, err := keys.ReadByte()
	if err != nil {
		return menuKeyCancel, 0
	}
	switch final {
	case 'A':
		return menuKeyUp, 0
	case 'B':
		return menuKeyDown, 0
	}
	return menuKeyOther, 0
}
