package offline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fspath"
)

var errOffline = errors.New("server remoto non raggiungibile")

// errChanged: the file is no longer the version being read
var errChanged = errors.New("il file è stato modificato sul server remoto da qualcun altro")

// Online state.
//
// A background prober connects to the probe address every few seconds;
// isOnline only reads the result, so no operation ever waits for the
// network to decide. After a network error the remote is considered offline
// for a short backoff, after a login failure until the password is fixed
// (repeated failed logins would lock the account).

const (
	probeEvery     = 3 * time.Second
	networkBackoff = 10 * time.Second
)

func (f *Fs) isOnline() bool {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	return f.effectiveLocked(time.Now())
}

func (f *Fs) effectiveLocked(now time.Time) bool {
	if f.authFailed || now.Before(f.offlineUntil) {
		return false
	}
	return f.opt.Probe == "" || f.online
}

func (f *Fs) lastRTT() time.Duration {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	return f.rtt
}

func (f *Fs) dialProbe() (bool, time.Duration, error) {
	if f.opt.Probe == "" {
		return true, 0, nil
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", f.opt.Probe, time.Duration(f.opt.ProbeTimeout))
	if err != nil {
		return false, 0, err
	}
	rtt := time.Since(start)
	_ = conn.Close()
	return true, rtt, nil
}

// probeOnce updates the state; the network call is made without locks
func (f *Fs) probeOnce() {
	up, rtt, err := f.dialProbe()
	now := time.Now()
	f.stateMu.Lock()
	before := f.effectiveLocked(now)
	wasKnown := f.known
	f.online, f.known = up, true
	if up {
		f.rtt = rtt
	}
	after := f.effectiveLocked(now)
	logIt := up != f.lastLogged || !wasKnown
	f.lastLogged = up
	f.stateMu.Unlock()
	if logIt {
		if up {
			fs.Logf(f, "server remoto ONLINE (risposta in %v)", rtt.Round(time.Millisecond))
		} else {
			fs.Logf(f, "server remoto OFFLINE: uso le cartelle salvate (%v)", err)
			if runtime.GOOS == "darwin" && err != nil && strings.Contains(err.Error(), "no route to host") {
				f.lnOnce.Do(func() {
					f.notify("Il server remoto non è raggiungibile: macOS potrebbe non permettere a MIMUS di usare la rete locale.\n\nControlla Impostazioni di Sistema → Privacy e sicurezza → Rete locale: MIMUS deve essere attivo.")
				})
			}
		}
	}
	if after && !before && wasKnown {
		go f.reconnected()
	}
}

// reconnected runs when the remote is reachable again: first a cheap listing
// flushes the SMB connections that died while offline (they fail once with
// EOF), then queued changes and uploads restart at once
func (f *Fs) reconnected() {
	for i := 0; i < 3; i++ {
		_, err := f.Fs.List(f.bgCtx, "")
		if err == nil || f.classify(err) != errNetwork {
			if err != nil && isAuthError(err) {
				f.loginFailed(err)
				return
			}
			break
		}
	}
	f.wakeReplay()
	f.wakeWalker()
	f.wakePins()
	f.stateMu.Lock()
	hook := f.reconnect
	f.stateMu.Unlock()
	if hook != nil && f.journal.len() == 0 {
		hook() // with queued changes the uploads wait for them anyway
	}
}

func (f *Fs) prober(ctx context.Context) {
	defer f.wg.Done()
	tick := time.NewTicker(probeEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.probeWake:
		}
		f.probeOnce()
	}
}

// markOffline: a network error happened, stay offline for a while
func (f *Fs) markOffline() {
	f.stateMu.Lock()
	f.offlineUntil = time.Now().Add(networkBackoff)
	f.stateMu.Unlock()
	select {
	case f.probeWake <- struct{}{}:
	default:
	}
}

// errKind classifies an error returned by the remote
type errKind int

const (
	errNone errKind = iota
	errNotFound
	errAuthKind
	errNetwork
	errOther // the remote answered with an error (access denied, file in use...)
)

func (f *Fs) classify(err error) errKind {
	switch {
	case err == nil:
		return errNone
	case errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) ||
		errors.Is(err, fs.ErrorIsDir) || errors.Is(err, fs.ErrorIsFile):
		return errNotFound
	case errors.Is(err, errOffline) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return errNetwork
	case isAuthError(err):
		return errAuthKind
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isNetMessage(err):
		return errNetwork
	}
	if up, _, _ := f.dialProbe(); !up {
		return errNetwork
	}
	return errOther
}

func isNetMessage(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection", "eof", "timeout", "timed out", "broken pipe", "reset by peer", "no route", "network is unreachable"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// netErr reacts to an error returned by the remote and returns its kind
func (f *Fs) netErr(err error) errKind {
	k := f.classify(err)
	switch k {
	case errAuthKind:
		f.loginFailed(err)
	case errNetwork:
		if !errors.Is(err, context.Canceled) {
			f.networkError()
		}
	}
	return k
}

// networkError: offline backoff if the remote does not answer, or if errors
// repeat while it answers (SMB hung). A single error with the remote
// answering is usually a stale connection: retry at once.
func (f *Fs) networkError() {
	up, _, _ := f.dialProbe()
	f.stateMu.Lock()
	since := time.Since(f.lastNetErr)
	// errors of concurrent operations in the same second are one incident
	repeated := since > 2*time.Second && since < 30*time.Second
	f.lastNetErr = time.Now()
	f.stateMu.Unlock()
	if !up || repeated {
		f.markOffline()
	}
}

func isAuthError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"attempted logon is invalid",
		"password has expired",
		"password must be changed",
		"account is currently disabled",
		"account has been automatically locked",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// loginFailed stops all access to the remote and asks for a new password
func (f *Fs) loginFailed(err error) {
	f.stateMu.Lock()
	if f.authFailed {
		f.stateMu.Unlock()
		return
	}
	f.authFailed = true
	f.stateMu.Unlock()
	fs.Errorf(f, "login al server remoto rifiutato, sospendo i tentativi per non bloccare l'account: %v", err)
	go f.askNewPassword(err)
}

func (f *Fs) askNewPassword(cause error) {
	parsed, perr := fspath.Parse(f.opt.Remote)
	section := parsed.Name
	user := config.GetValue(section, "user")
	msg := fmt.Sprintf("Il server remoto non accetta più la password di «%s» (forse è cambiata o scaduta).\n\nPer non bloccare l'account ho sospeso i tentativi: inserisci la nuova password.", user)
	if strings.Contains(strings.ToLower(cause.Error()), "locked") {
		msg = fmt.Sprintf("L'account «%s» risulta bloccato sul server. Fallo sbloccare dall'amministratore, poi inserisci la password.", user)
	}
	pw, ok := f.askPassword(f.bgCtx, msg)
	if !ok || perr != nil {
		fs.Logf(f, "nuova password non inserita: riprovo tra un'ora con un solo tentativo")
		time.AfterFunc(time.Hour, f.retryLogin)
		return
	}
	if err := config.SetValueAndSave(section, "pass", obscure.MustObscure(pw)); err != nil {
		fs.Errorf(f, "salvataggio password: %v", err)
		return
	}
	fs.Logf(f, "nuova password salvata: riavvio il collegamento")
	if !f.opt.RestartOnPassword {
		return
	}
	// launchd (KeepAlive) restarts us with the new password
	if p, err := os.FindProcess(os.Getpid()); err == nil {
		_ = p.Signal(syscall.SIGTERM)
	}
}

// retryLogin makes ONE login attempt (the password may have been fixed on
// the server); only if it works the remote is used again
func (f *Fs) retryLogin() {
	if f.bgCtx.Err() != nil {
		return
	}
	if up, _, _ := f.dialProbe(); !up {
		time.AfterFunc(10*time.Minute, f.retryLogin)
		return
	}
	_, err := f.Fs.List(f.bgCtx, "")
	switch {
	case err == nil || errors.Is(err, fs.ErrorDirNotFound):
		f.stateMu.Lock()
		f.authFailed = false
		f.stateMu.Unlock()
		fs.Logf(f, "login al server remoto di nuovo accettato")
		f.wakeReplay()
	case isAuthError(err):
		go f.askNewPassword(err)
	default:
		time.AfterFunc(10*time.Minute, f.retryLogin)
	}
}
