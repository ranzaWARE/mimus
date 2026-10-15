package offline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
)

// Journal of the changes made while offline (or while earlier changes are
// still being applied). They are replayed in order on the remote; if the remote
// changed meanwhile the user is asked what to do. A change the remote keeps
// refusing (access denied, file locked...) is retried a few times, then
// set aside in journal-failed.json and the user is told.

type jop struct {
	ID       int64     `json:"id"`
	Op       string    `json:"op"` // mkdir rmdir move remove dirmove
	Src      string    `json:"src"`
	Dst      string    `json:"dst,omitempty"`
	Size     int64     `json:"size,omitempty"`
	ModTime  time.Time `json:"mtime,omitempty"`
	When     time.Time `json:"when"`
	Attempts int       `json:"attempts,omitempty"`
	NextTry  time.Time `json:"next,omitempty"`
	LastErr  string    `json:"err,omitempty"`
}

func (op jop) describe() string {
	switch op.Op {
	case "mkdir":
		return fmt.Sprintf("creare la cartella «%s»", op.Src)
	case "rmdir":
		return fmt.Sprintf("cancellare la cartella «%s»", op.Src)
	case "remove":
		return fmt.Sprintf("cancellare «%s»", op.Src)
	case "move":
		return fmt.Sprintf("rinominare «%s» in «%s»", op.Src, op.Dst)
	case "dirmove":
		return fmt.Sprintf("rinominare la cartella «%s» in «%s»", op.Src, op.Dst)
	}
	return op.Op + " " + op.Src
}

const maxAttempts = 4

type journal struct {
	mu     sync.Mutex
	dir    string
	ops    []jop
	nextID int64
}

// newJournal loads the journal; a damaged file is moved aside (corrupt != "")
func newJournal(dir string) (j *journal, corrupt string) {
	j = &journal{dir: dir}
	name := filepath.Join(dir, journalFile)
	if data, err := os.ReadFile(name); err == nil && len(data) > 0 {
		if !readJSON(name, &j.ops) {
			corrupt = fmt.Sprintf("%s.damaged-%s", name, time.Now().Format("20060102-150405"))
			_ = os.Rename(name, corrupt)
			j.ops = nil
		}
	}
	for _, op := range j.ops {
		if op.ID >= j.nextID {
			j.nextID = op.ID + 1
		}
	}
	return j, corrupt
}

func (j *journal) saveLocked() error {
	ops := j.ops
	if ops == nil {
		ops = []jop{}
	}
	return writeJSON(filepath.Join(j.dir, journalFile), ops)
}

// add appends op; on error the change is not queued and must fail
func (j *journal) add(op jop) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	op.ID = j.nextID
	op.When = time.Now()
	j.ops = append(j.ops, op)
	if err := j.saveLocked(); err != nil {
		j.ops = j.ops[:len(j.ops)-1]
		return fmt.Errorf("impossibile salvare la modifica offline: %w", err)
	}
	j.nextID++
	return nil
}

func (j *journal) first() (jop, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.ops) == 0 {
		return jop{}, false
	}
	return j.ops[0], true
}

func (j *journal) pop(id int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.ops) > 0 && j.ops[0].ID == id {
		j.ops = j.ops[1:]
		_ = j.saveLocked()
	}
}

// failed records a failed attempt; returns true if op has been set aside
func (j *journal) failed(id int64, err error) (parked bool, op jop) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.ops) == 0 || j.ops[0].ID != id {
		return false, jop{}
	}
	o := &j.ops[0]
	o.Attempts++
	o.LastErr = err.Error()
	o.NextTry = time.Now().Add(time.Duration(o.Attempts*o.Attempts) * 15 * time.Second) // 15s 1m 2m15s
	op = *o
	if o.Attempts >= maxAttempts {
		var parkedOps []jop
		pf := filepath.Join(j.dir, parkedFile)
		readJSON(pf, &parkedOps)
		_ = writeJSON(pf, append(parkedOps, op))
		j.ops = j.ops[1:]
		parked = true
	}
	_ = j.saveLocked()
	return parked, op
}

// rewrite changes the paths of the ops after the first one: used when a
// rename has been cancelled or applied with another name, so that later
// changes still refer to the user's file
func (j *journal) rewrite(from, to string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := 1; i < len(j.ops); i++ {
		j.ops[i].Src = rebase(j.ops[i].Src, from, to)
		j.ops[i].Dst = rebase(j.ops[i].Dst, from, to)
	}
	_ = j.saveLocked()
}

func rebase(p, from, to string) string {
	if p == "" || !isUnder(p, from) {
		return p
	}
	return to + strings.TrimPrefix(p, from)
}

func (j *journal) len() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.ops)
}

// touchesDir reports whether a pending op changes the content of dir
func (j *journal) touchesDir(dir string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, op := range j.ops {
		for _, p := range []string{op.Src, op.Dst} {
			if p == "" {
				continue
			}
			if parent, _ := parentLeaf(p); parent == dir {
				return true
			}
			if (op.Op == "dirmove" || op.Op == "mkdir" || op.Op == "rmdir") && isUnder(dir, p) {
				return true
			}
		}
	}
	return false
}

// touchesPath reports whether a pending op involves remote (or a parent of it)
func (j *journal) touchesPath(remote string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, op := range j.ops {
		for _, p := range []string{op.Src, op.Dst} {
			if p != "" && isUnder(remote, p) {
				return true
			}
		}
	}
	return false
}

// ---------- queueing (called instead of the remote operation) ----------

// queueing reports whether changes must go to the journal instead of the remote
func (f *Fs) queueing() bool {
	return f.journal.len() > 0 || !f.isOnline()
}

func (f *Fs) enqueue(op jop) error {
	if err := f.journal.add(op); err != nil {
		fs.Errorf(f, "%v", err)
		return err
	}
	fs.Logf(f, "in coda: %s", op.describe())
	f.wakeReplay()
	return nil
}

func (f *Fs) wakeReplay() {
	select {
	case f.replayWake <- struct{}{}:
	default:
	}
}

// waitJournal blocks until all queued changes are on the remote
func (f *Fs) waitJournal(ctx context.Context) error {
	if f.journal.len() == 0 {
		return nil
	}
	f.wakeReplay()
	for f.journal.len() > 0 {
		if !f.isOnline() {
			return errOffline
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil
}

// ---------- replay ----------

func (f *Fs) replayer(ctx context.Context) {
	defer f.wg.Done()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.replayWake:
		}
		if f.journal.len() > 0 && f.isOnline() {
			f.replay(ctx)
		}
	}
}

func (f *Fs) replay(ctx context.Context) {
	f.replayMu.Lock()
	defer f.replayMu.Unlock()
	f.replaying.Store(true)
	defer f.replaying.Store(false)
	n := 0
	retries := 0
	for ctx.Err() == nil && f.isOnline() {
		op, ok := f.journal.first()
		if !ok {
			break
		}
		if time.Now().Before(op.NextTry) {
			break
		}
		err := f.applyOp(ctx, op)
		if err == nil {
			f.journal.pop(op.ID)
			n++
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if k := f.netErr(err); k != errOther {
			if k == errNetwork && retries < 3 && f.isOnline() {
				retries++ // probably a stale connection: try again at once
				continue
			}
			fs.Logf(f, "coda ferma su %s: %v (riprovo appena il server remoto risponde)", op.describe(), err)
			break
		}
		parked, op := f.journal.failed(op.ID, err)
		if parked {
			f.notify(fmt.Sprintf("Non sono riuscito a %s sul server remoto:\n%v\n\nHo tolto questa modifica dalla coda: controlla il file sul server remoto (forse è aperto da qualcun altro o non hai i permessi).", op.describe(), err))
			continue
		}
		fs.Logf(f, "tentativo %d di %s fallito: %v (riprovo tra %v)", op.Attempts, op.describe(), err, time.Until(op.NextTry).Round(time.Second))
		break
	}
	if n > 0 {
		fs.Logf(f, "modifiche fatte offline applicate sul server remoto: %d", n)
		if f.journal.len() == 0 {
			f.stateMu.Lock()
			hook := f.reconnect
			f.stateMu.Unlock()
			if hook != nil {
				go hook() // uploads waiting for the queue can go now
			}
		}
	}
}

func (f *Fs) sameVersion(o fs.Object, size int64, modTime time.Time) bool {
	if modTime.IsZero() {
		return true // version unknown
	}
	if o.Size() != size {
		return false
	}
	prec := f.Fs.Precision()
	if prec == fs.ModTimeNotSupported {
		return true
	}
	if prec < time.Second {
		prec = time.Second // SMB timestamps can be rounded
	}
	d := o.ModTime(context.Background()).Sub(modTime)
	return d <= prec && d >= -prec
}

func (f *Fs) fileExists(ctx context.Context, remote string) (bool, error) {
	_, err := f.Fs.NewObject(ctx, remote)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrorObjectNotFound):
		return false, nil
	case errors.Is(err, fs.ErrorIsDir):
		return true, nil
	}
	return false, err
}

func (f *Fs) dirExists(ctx context.Context, remote string) (bool, error) {
	_, err := f.Fs.List(ctx, remote)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrorDirNotFound):
		return false, nil
	}
	return false, err
}

// uniqueConflictName returns a "(conflitto Mac ...)" name not used on the remote
func (f *Fs) uniqueConflictName(ctx context.Context, remote string, isDir bool) (string, error) {
	for i := 1; i < 100; i++ {
		name := conflictName(remote, i)
		var exists bool
		var err error
		if isDir {
			exists, err = f.dirExists(ctx, name)
		} else {
			exists, err = f.fileExists(ctx, name)
		}
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", errors.New("troppi file in conflitto con lo stesso nome")
}

// applyOp replays one change. A non-nil error means "retry later".
func (f *Fs) applyOp(ctx context.Context, op jop) error {
	const cancelRename = "Annulla rinomina"
	switch op.Op {
	case "mkdir":
		return f.Fs.Mkdir(ctx, op.Src)

	case "rmdir":
		err := f.Fs.Rmdir(ctx, op.Src)
		if err == nil {
			return nil
		}
		entries, lerr := f.Fs.List(ctx, op.Src)
		switch {
		case errors.Is(lerr, fs.ErrorDirNotFound):
			return nil
		case lerr != nil:
			return err
		case len(entries) > 0:
			f.notify(fmt.Sprintf("La cartella «%s» che hai cancellato offline non è stata cancellata sul server remoto: nel frattempo qualcuno ci ha messo dei file.", op.Src))
			f.store.save(ctx, op.Src, entries)
			return nil
		}
		return err

	case "remove":
		cur, err := f.Fs.NewObject(ctx, op.Src)
		if errors.Is(err, fs.ErrorObjectNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !f.sameVersion(cur, op.Size, op.ModTime) {
			const keep, del = "Non cancellare", "Cancella comunque"
			msg := fmt.Sprintf("Hai cancellato offline «%s», ma nel frattempo qualcuno l'ha modificato sul server remoto.\n\nCosa faccio?", op.Src)
			ans, err := f.ask(ctx, msg, keep, keep, del)
			if err != nil {
				return err
			}
			if ans == keep {
				f.store.upsertObject(ctx, cur)
				return nil
			}
		}
		return cur.Remove(ctx)

	case "move":
		cur, err := f.Fs.NewObject(ctx, op.Src)
		if errors.Is(err, fs.ErrorObjectNotFound) {
			f.notify(fmt.Sprintf("Non ho potuto rinominare «%s» in «%s» sul server remoto: nel frattempo il file è stato spostato o cancellato da qualcun altro.", op.Src, op.Dst))
			return nil
		}
		if err != nil {
			return err
		}
		if !f.sameVersion(cur, op.Size, op.ModTime) {
			const doIt = "Rinomina comunque"
			msg := fmt.Sprintf("Hai rinominato offline «%s» in «%s», ma nel frattempo qualcuno ha modificato il file sul server remoto.\n\nCosa faccio?", op.Src, path.Base(op.Dst))
			ans, err := f.ask(ctx, msg, cancelRename, cancelRename, doIt)
			if err != nil {
				return err
			}
			if ans == cancelRename {
				f.journal.rewrite(op.Dst, op.Src)
				return nil
			}
		}
		dst := op.Dst
		for try := 0; ; try++ {
			exists, err := f.fileExists(ctx, dst)
			if err != nil {
				return err
			}
			if exists {
				if try > 2 {
					return errors.New("il nome di destinazione continua a esistere")
				}
				alt, err := f.uniqueConflictName(ctx, op.Dst, false)
				if err != nil {
					return err
				}
				both := "Rinomina in «" + path.Base(alt) + "»"
				msg := fmt.Sprintf("Hai rinominato offline «%s» in «%s», ma sul server remoto esiste già un file con quel nome.\n\nCosa faccio?", op.Src, path.Base(op.Dst))
				ans, err := f.ask(ctx, msg, both, cancelRename, both)
				if err != nil {
					return err
				}
				if ans == cancelRename {
					f.journal.rewrite(op.Dst, op.Src)
					return nil
				}
				dst = alt
				continue // check again right before renaming
			}
			break
		}
		do := f.Fs.Features().Move
		if do == nil {
			return nil
		}
		res, err := do(ctx, cur, dst)
		if err != nil {
			return err
		}
		if dst != op.Dst {
			f.journal.rewrite(op.Dst, dst)
		}
		f.store.removeEntry(op.Src)
		f.store.upsertObject(ctx, res)
		return nil

	case "dirmove":
		exists, err := f.dirExists(ctx, op.Src)
		if err != nil {
			return err
		}
		if !exists {
			f.notify(fmt.Sprintf("Non ho potuto rinominare la cartella «%s» in «%s» sul server remoto: nel frattempo è stata spostata o cancellata da qualcun altro.", op.Src, op.Dst))
			return nil
		}
		dst := op.Dst
		for try := 0; ; try++ {
			exists, err := f.dirExists(ctx, dst)
			if err != nil {
				return err
			}
			if exists {
				if try > 2 {
					return errors.New("il nome di destinazione continua a esistere")
				}
				alt, err := f.uniqueConflictName(ctx, op.Dst, true)
				if err != nil {
					return err
				}
				both := "Rinomina in «" + path.Base(alt) + "»"
				msg := fmt.Sprintf("Hai rinominato offline la cartella «%s» in «%s», ma sul server remoto esiste già una cartella con quel nome.\n\nCosa faccio?", op.Src, path.Base(op.Dst))
				ans, err := f.ask(ctx, msg, both, cancelRename, both)
				if err != nil {
					return err
				}
				if ans == cancelRename {
					f.journal.rewrite(op.Dst, op.Src)
					return nil
				}
				dst = alt
				continue
			}
			break
		}
		do := f.Fs.Features().DirMove
		if do == nil {
			return nil
		}
		if err := do(ctx, f.Fs, op.Src, dst); err != nil {
			return err
		}
		if dst != op.Dst {
			f.journal.rewrite(op.Dst, dst)
		}
		f.store.renameTree(op.Src, dst)
		return nil
	}
	return nil
}

// conflictName returns "dir/name (conflitto Mac 2026-10-13 1430).ext",
// with " n" added for n > 1
func conflictName(remote string, n int) string {
	dir, leaf := parentLeaf(remote)
	ext := path.Ext(leaf)
	base := strings.TrimSuffix(leaf, ext)
	if base == "" {
		base, ext = leaf, ""
	}
	tag := "conflitto Mac " + time.Now().Format("2006-01-02 1504")
	if n > 1 {
		tag += fmt.Sprintf(" %d", n)
	}
	return path.Join(dir, fmt.Sprintf("%s (%s)%s", base, tag, ext))
}
