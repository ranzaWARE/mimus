package main

// Dialoghi su Linux: zenity (GNOME, Cinnamon, MATE, XFCE…) o kdialog (KDE).
// MIMUS_DIALOG=zenity|kdialog sceglie lo strumento.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func dialogTool() string {
	if t := os.Getenv("MIMUS_DIALOG"); t != "" {
		return t
	}
	kde := strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE")
	_, errK := exec.LookPath("kdialog")
	_, errZ := exec.LookPath("zenity")
	switch {
	case kde && errK == nil:
		return "kdialog"
	case errZ == nil:
		return "zenity"
	case errK == nil:
		return "kdialog"
	}
	return ""
}

// lxRun returns stdout and the exit code (-1: the program did not start)
func lxRun(name string, args ...string) (string, int) {
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return strings.TrimRight(out.String(), "\r\n"), code
}

// zenity reads the text as Pango markup
func markupEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func noDialogTool(message string) {
	os.Stderr.WriteString("MIMUS: serve zenity o kdialog per mostrare le finestre.\n" + message + "\n")
}

// lxDialog: buttons left to right; Esc or closing the window gives ""
func lxDialog(message, def string, buttons ...string) string {
	message = forLinux(message)
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}
	switch dialogTool() {
	case "zenity":
		if len(buttons) == 1 {
			if _, code := lxRun("zenity", "--info", "--title", title, "--width", "460", "--text", markupEscape(message), "--ok-label", buttons[0]); code == 0 {
				return buttons[0]
			}
			return ""
		}
		args := []string{"--question", "--title", title, "--width", "460", "--text", markupEscape(message), "--switch"}
		for _, b := range buttons {
			args = append(args, "--extra-button", b)
		}
		out, code := lxRun("zenity", args...)
		if code == -1 || (code != 0 && code != 1) {
			// zenity without --switch: OK is the default button
			args = []string{"--question", "--title", title, "--width", "460", "--text", markupEscape(message), "--ok-label", def}
			first := true
			for _, b := range buttons {
				if b == def {
					continue
				}
				if first {
					args = append(args, "--cancel-label", b)
					first = false
				} else {
					args = append(args, "--extra-button", b)
				}
			}
			out, code = lxRun("zenity", args...)
			if code == 0 {
				return def
			}
		}
		for _, b := range buttons {
			if out == b {
				return b
			}
		}
		return ""
	case "kdialog":
		if len(buttons) == 1 {
			if _, code := lxRun("kdialog", "--title", title, "--msgbox", message, "--ok-label", buttons[0]); code == 0 {
				return buttons[0]
			}
			return ""
		}
		if len(buttons) > 2 {
			// three answers: a list (Esc closes it without choosing)
			args := []string{"--title", title, "--menu", message}
			for _, b := range buttons {
				args = append(args, b, b)
			}
			out, code := lxRun("kdialog", args...)
			if code == 0 {
				for _, b := range buttons {
					if out == b {
						return b
					}
				}
			}
			return ""
		}
		// the third button "Chiudi" is what Esc gives as well
		_, code := lxRun("kdialog", "--title", title, "--yesnocancel", message,
			"--yes-label", buttons[0], "--no-label", buttons[1], "--cancel-label", "Chiudi")
		switch code {
		case 0:
			return buttons[0]
		case 1:
			return buttons[1]
		}
		return ""
	}
	noDialogTool(message)
	return ""
}

func lxInput(message, def string, hidden bool) (string, bool) {
	message = forLinux(message)
	switch dialogTool() {
	case "zenity":
		args := []string{"--entry", "--title", title, "--width", "460", "--text", markupEscape(message), "--entry-text", def}
		if hidden {
			args = append(args, "--hide-text")
		}
		out, code := lxRun("zenity", args...)
		return out, code == 0
	case "kdialog":
		if hidden {
			out, code := lxRun("kdialog", "--title", title, "--password", message)
			return out, code == 0
		}
		out, code := lxRun("kdialog", "--title", title, "--inputbox", message, def)
		return out, code == 0
	}
	noDialogTool(message)
	return "", false
}

func lxChoose(prompt string, items []string, okLabel, cancelLabel string) string {
	prompt = forLinux(prompt)
	shown := make([]string, len(items))
	for i, it := range items {
		shown[i] = forLinux(it)
	}
	pick := func(out string) string {
		for i, it := range shown {
			if out == it {
				return items[i]
			}
		}
		return ""
	}
	switch dialogTool() {
	case "zenity":
		args := []string{"--list", "--title", title, "--width", "560", "--height", "560", "--text", markupEscape(prompt),
			"--column", "MIMUS", "--hide-header", "--ok-label", okLabel, "--cancel-label", cancelLabel}
		out, code := lxRun("zenity", append(args, shown...)...)
		if code != 0 {
			return ""
		}
		return pick(strings.Split(out, "|")[0])
	case "kdialog":
		args := []string{"--title", title, "--geometry", "560x560", "--menu", prompt}
		for _, it := range shown {
			args = append(args, it, it)
		}
		out, code := lxRun("kdialog", args...)
		if code != 0 {
			return ""
		}
		return pick(out)
	}
	noDialogTool(prompt)
	return ""
}

func lxFolder(prompt, def string) string {
	prompt = forLinux(prompt)
	switch dialogTool() {
	case "zenity":
		out, code := lxRun("zenity", "--file-selection", "--directory", "--title", prompt, "--filename", strings.TrimSuffix(def, "/")+"/")
		if code != 0 {
			return ""
		}
		return strings.TrimSuffix(out, "/")
	case "kdialog":
		out, code := lxRun("kdialog", "--title", prompt, "--getexistingdirectory", def)
		if code != 0 {
			return ""
		}
		return strings.TrimSuffix(out, "/")
	}
	noDialogTool(prompt)
	return ""
}

// lxNotify: a desktop notification, without waiting
func lxNotify(message string) {
	_ = startDetached("notify-send", "--app-name", title, "--icon", "mimus", title, forLinux(message))
}

// backendUI answers the dialogs of the MIMUS service (option ui= of the
// offline backend): "mimus ask|password|info TITLE MESSAGE ..."
func backendUI(args []string) int {
	if len(args) < 3 {
		return 2
	}
	kind, message := args[0], args[2]
	switch kind {
	case "ask":
		if len(args) < 5 {
			return 2
		}
		os.Stdout.WriteString(lxDialog(message, args[3], args[4:]...) + "\n")
	case "password":
		if v, ok := lxInput(message, "", true); ok && v != "" {
			os.Stdout.WriteString("OK:" + v + "\n")
		}
	default:
		lxDialog(message, "OK", "OK")
	}
	return 0
}
