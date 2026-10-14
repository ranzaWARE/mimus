package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// Dialogs with osascript (standard macOS windows). For the tests the
// environment variable MIMUS_UI can name a program that answers instead:
//
//	program dialog   MESSAGE DEFAULT BUTTON...      -> chosen button ("" = cancelled)
//	program input    MESSAGE DEFAULT hidden|plain   -> text, or nothing if cancelled
//	program choose   PROMPT ITEM...                 -> chosen item ("" = cancelled)

const title = "MIMUS"

func osa(script string, args ...string) (string, bool) {
	cmd := exec.Command("/usr/bin/osascript", append([]string{"-e", script}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimRight(out.String(), "\r\n"), err == nil
}

func testUI(args ...string) (string, bool) {
	cmd := exec.Command(os.Getenv("MIMUS_UI"), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimRight(out.String(), "\r\n"), err == nil
}

// dialog shows message with buttons (max 3); returns the chosen button,
// "" if closed with Esc / cancel
func dialog(message, def string, buttons ...string) string {
	if os.Getenv("MIMUS_UI") != "" {
		out, _ := testUI(append([]string{"dialog", message, def}, buttons...)...)
		return out
	}
	const script = `on run argv
	activate
	set b to {}
	repeat with i from 3 to count of argv
		set end of b to item i of argv
	end repeat
	try
		set r to display dialog (item 1 of argv) with title "MIMUS" buttons b default button (item 2 of argv)
		return button returned of r
	on error
		return ""
	end try
end run`
	out, _ := osa(script, append([]string{message, def}, buttons...)...)
	return out
}

// info shows a message with a single OK button
func info(message string) { dialog(message, "OK", "OK") }

// input asks for a text; ok is false if cancelled
func input(message, def string, hidden bool) (string, bool) {
	mode := "plain"
	if hidden {
		mode = "hidden"
	}
	if os.Getenv("MIMUS_UI") != "" {
		out, ok := testUI("input", message, def, mode)
		return out, ok && out != "\x00cancel"
	}
	const script = `on run argv
	activate
	try
		if item 3 of argv is "hidden" then
			set r to display dialog (item 1 of argv) with title "MIMUS" default answer (item 2 of argv) with hidden answer buttons {"Annulla", "OK"} default button "OK" cancel button "Annulla"
		else
			set r to display dialog (item 1 of argv) with title "MIMUS" default answer (item 2 of argv) buttons {"Annulla", "OK"} default button "OK" cancel button "Annulla"
		end if
		return "OK:" & text returned of r
	on error
		return ""
	end try
end run`
	out, _ := osa(script, message, def, mode)
	if !strings.HasPrefix(out, "OK:") {
		return "", false
	}
	return strings.TrimPrefix(out, "OK:"), true
}

// askText repeats input until a non-empty answer; ok false if cancelled
func askText(message, def string, hidden bool) (string, bool) {
	for {
		v, ok := input(message, def, hidden)
		if !ok {
			return "", false
		}
		if strings.TrimSpace(v) != "" {
			return v, true
		}
	}
}

// choose shows a list; returns the chosen item or "" if cancelled
func choose(prompt string, items []string, okLabel, cancelLabel string) string {
	if os.Getenv("MIMUS_UI") != "" {
		out, _ := testUI(append([]string{"choose", prompt}, items...)...)
		return out
	}
	const script = `on run argv
	activate
	set l to {}
	repeat with i from 4 to count of argv
		set end of l to item i of argv
	end repeat
	set r to choose from list l with title "MIMUS" with prompt (item 1 of argv) default items {item 4 of argv} OK button name (item 2 of argv) cancel button name (item 3 of argv)
	if r is false then return ""
	return item 1 of r
end run`
	out, _ := osa(script, append([]string{prompt, okLabel, cancelLabel}, items...)...)
	return out
}

// chooseFolder asks for a folder; returns its path or "" if cancelled
func chooseFolder(prompt, def string) string {
	if os.Getenv("MIMUS_UI") != "" {
		out, _ := testUI("folder", prompt, def)
		return out
	}
	const script = `on run argv
	activate
	try
		try
			set f to choose folder with prompt (item 1 of argv) default location (POSIX file (item 2 of argv) as alias)
		on error number n
			if n is -128 then error number -128
			set f to choose folder with prompt (item 1 of argv)
		end try
		return POSIX path of f
	on error
		return ""
	end try
end run`
	out, _ := osa(script, prompt, def)
	return strings.TrimSuffix(out, "/")
}
