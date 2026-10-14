package offline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
)

// User dialogs.
//
// ui = "auto": osascript dialogs on macOS, none elsewhere
// ui = "none": never ask, the safe default answer is used
// ui = /path/to/program: called as
//
//	program ask      TITLE MESSAGE DEFAULT BUTTON...   -> prints the chosen button
//	program password TITLE MESSAGE                     -> prints "OK:<password>" or nothing
//	program info     TITLE MESSAGE
//
// (used by the tests)
//
// Dialogs follow the caller's context: if the operation waiting for an
// answer is cancelled (e.g. the VFS cancels an upload because the file is
// being written again) the dialog is closed, so the Finder never waits.

const appTitle = "MIMUS"

var uiSem = make(chan struct{}, 1) // one dialog at a time

const osaAsk = `on run argv
	set t to item 1 of argv
	set m to item 2 of argv
	set d to item 3 of argv
	set b to {}
	repeat with i from 4 to count of argv
		set end of b to item i of argv
	end repeat
	set r to display dialog m with title t buttons b default button d giving up after %d with icon caution
	if gave up of r then return ""
	return button returned of r
end run`

const osaPassword = `on run argv
	set r to display dialog (item 2 of argv) with title (item 1 of argv) default answer "" with hidden answer buttons {"Annulla", "Salva"} default button "Salva" giving up after 3600 with icon caution
	if gave up of r then return ""
	if button returned of r is "Salva" then return "OK:" & text returned of r
	return ""
end run`

const osaInfo = `on run argv
	display dialog (item 2 of argv) with title (item 1 of argv) buttons {"OK"} default button "OK" giving up after 900 with icon caution
end run`

func (f *Fs) uiMode() string {
	switch f.opt.UI {
	case "", "auto":
		if runtime.GOOS == "darwin" {
			return "osascript"
		}
		return "none"
	default:
		return f.opt.UI
	}
}

func lockUI(ctx context.Context) error {
	select {
	case uiSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func unlockUI() { <-uiSem }

func (f *Fs) runUI(ctx context.Context, timeout time.Duration, kind string, args ...string) (string, error) {
	mode := f.uiMode()
	if mode == "none" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+time.Minute)
	defer cancel()
	if mode == "osascript" {
		var script string
		switch kind {
		case "ask":
			script = strings.Replace(osaAsk, "%d", strconv.Itoa(int(timeout.Seconds())), 1)
		case "password":
			script = osaPassword
		default:
			script = osaInfo
		}
		out, stderr, err := runCmd(ctx, "/usr/bin/osascript", append([]string{"-e", script}, args...)...)
		if err != nil && ctx.Err() == nil && (strings.Contains(stderr, "-1713") || strings.Contains(stderr, "user interaction")) {
			// no UI allowed from this background process: ask System Events
			// to show it (macOS asks once for the Automation permission)
			script = strings.ReplaceAll(script, "set r to display dialog", `tell application "System Events" to set r to display dialog`)
			script = strings.ReplaceAll(script, "\tdisplay dialog", "\t"+`tell application "System Events" to display dialog`)
			out, _, err = runCmd(ctx, "/usr/bin/osascript", append([]string{"-e", script}, args...)...)
		}
		return out, err
	}
	out, _, err := runCmd(ctx, mode, append([]string{kind}, args...)...)
	return out, err
}

func runCmd(ctx context.Context, name string, args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	if err != nil && errb.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\r\n"), errb.String(), err
}

// ask shows a dialog and returns the chosen button, or def when there is
// no answer (no UI, timeout, dialog error). At most 3 buttons. It returns
// an error only if ctx is cancelled: the caller must then not act.
func (f *Fs) ask(ctx context.Context, message, def string, buttons ...string) (string, error) {
	if err := lockUI(ctx); err != nil {
		return "", err
	}
	defer unlockUI()
	args := append([]string{appTitle, message, def}, buttons...)
	answer, err := f.runUI(ctx, time.Duration(f.opt.AskTimeout), "ask", args...)
	if ctx.Err() != nil {
		fs.Logf(f, "domanda annullata (operazione interrotta), verrà ripetuta")
		return "", ctx.Err()
	}
	if err != nil {
		fs.Errorf(f, "dialog: %v", err)
	}
	for _, b := range buttons {
		if answer == b {
			fs.Logf(f, "scelta utente: %q", answer)
			return answer, nil
		}
	}
	fs.Logf(f, "nessuna risposta, uso la scelta sicura: %q", def)
	return def, nil
}

// askPassword asks for a new password. ok is false if cancelled.
func (f *Fs) askPassword(ctx context.Context, message string) (password string, ok bool) {
	if lockUI(ctx) != nil {
		return "", false
	}
	defer unlockUI()
	answer, err := f.runUI(ctx, time.Hour, "password", appTitle, message)
	if err != nil {
		fs.Errorf(f, "dialog password: %v", err)
	}
	if !strings.HasPrefix(answer, "OK:") {
		return "", false
	}
	password = strings.TrimPrefix(answer, "OK:")
	return password, password != ""
}

// notify shows an informative dialog without waiting for it
func (f *Fs) notify(message string) {
	fs.Logf(f, "avviso: %s", strings.ReplaceAll(message, "\n", " "))
	go func() {
		if lockUI(f.bgCtx) != nil {
			return
		}
		defer unlockUI()
		if _, err := f.runUI(f.bgCtx, 15*time.Minute, "info", appTitle, message); err != nil && f.bgCtx.Err() == nil {
			fs.Errorf(f, "dialog: %v", err)
		}
	}()
}
