package offline

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/rc"
)

// File e cartelle da tenere sempre su questo Mac («Mantieni sempre su
// questo Mac» nel Finder). La cache non li toglie mai (CacheEvictionAllowed)
// e, quando il server remoto è raggiungibile, un controllo in background
// scarica quello che manca: anche i file nuovi messi da altri in una
// cartella mantenuta. L'elenco è in pinned.json, accanto alla coda.
// I metodi IsPinned/SetPinned/PinnedPaths sono quelli che la VFS (patch
// MIMUS, interfaccia vfs.Pinner) usa per l'estensione del Finder.

const (
	pinnedFile = "pinned.json"
	pinEvery   = 10 * time.Minute
)

type pinSet struct {
	mu      sync.Mutex
	path    string
	list    []string // sorted, none inside another
	wake    chan struct{}
	running atomic.Bool
}

func newPinSet(dir string) *pinSet {
	p := &pinSet{path: filepath.Join(dir, pinnedFile), wake: make(chan struct{}, 1)}
	readJSON(p.path, &p.list)
	return p
}

// under: remote is p or inside it ("" = the whole remote folder)
func under(remote, p string) bool {
	return p == "" || remote == p || strings.HasPrefix(remote, p+"/")
}

func (p *pinSet) _covering(remote string) (string, bool) {
	for _, q := range p.list {
		if under(remote, q) {
			return q, true
		}
	}
	return "", false
}

func (p *pinSet) _without(remote string) []string {
	out := p.list[:0:0]
	for _, q := range p.list {
		if !under(q, remote) {
			out = append(out, q)
		}
	}
	return out
}

func (p *pinSet) _save() {
	sort.Strings(p.list)
	if err := writeJSON(p.path, p.list); err != nil {
		fs.Errorf(nil, "elenco dei file da tenere sul Mac non salvato: %v", err)
	}
}

// IsPinned: remote, or a folder that contains it, is kept always here
func (f *Fs) IsPinned(remote string) bool {
	f.pins.mu.Lock()
	defer f.pins.mu.Unlock()
	_, ok := f.pins._covering(strings.Trim(remote, "/"))
	return ok
}

// SetPinned keeps (on) or stops keeping remote and what it contains
func (f *Fs) SetPinned(ctx context.Context, remote string, on bool) error {
	remote = strings.Trim(remote, "/")
	p := f.pins
	p.mu.Lock()
	if on {
		if _, ok := p._covering(remote); !ok {
			p.list = append(p._without(remote), remote)
			p._save()
			fs.Infof(f, "da tenere sempre su questo Mac: %q", remote)
		}
	} else {
		if q, ok := p._covering(remote); ok && q != remote {
			p.mu.Unlock()
			return fmt.Errorf("«%s» è dentro «%s», che è da tenere sempre su questo Mac: togli quella", remote, q)
		}
		if n := len(p.list); len(p._without(remote)) != n {
			p.list = p._without(remote)
			p._save()
			fs.Infof(f, "non più da tenere sempre su questo Mac: %q", remote)
		}
	}
	p.mu.Unlock()
	if on {
		f.wakePins()
	}
	return nil
}

// PinnedPaths lists what is kept always here
func (f *Fs) PinnedPaths() []string {
	f.pins.mu.Lock()
	defer f.pins.mu.Unlock()
	return append([]string(nil), f.pins.list...)
}

// pinsRenamed follows a folder renamed from the Mac. Single files keep
// their pin by name: apps save by writing a new file and renaming, and the
// pin must stay on the name (a file really deleted is forgotten by
// keepPinned).
func (f *Fs) pinsRenamed(src, dst string) {
	p := f.pins
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := false
	for i, q := range p.list {
		if under(q, src) && src != "" {
			p.list[i] = dst + strings.TrimPrefix(q, src)
			changed = true
		}
	}
	if changed {
		p._save()
	}
}

// pinsRemoved forgets a folder deleted from the Mac
func (f *Fs) pinsRemoved(remote string) {
	p := f.pins
	p.mu.Lock()
	defer p.mu.Unlock()
	if remote != "" && len(p._without(remote)) != len(p.list) {
		p.list = p._without(remote)
		p._save()
	}
}

func (f *Fs) wakePins() {
	select {
	case f.pins.wake <- struct{}{}:
	default:
	}
}

// pinKeeper downloads what is kept always and is missing from the cache
func (f *Fs) pinKeeper(ctx context.Context) {
	defer f.wg.Done()
	tick := time.NewTicker(pinEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.pins.wake:
		}
		f.keepPinned(ctx)
	}
}

// keepPinned asks the VFS (rc vfs/mimus-download, same process) to
// download the missing files; one run at a time
func (f *Fs) keepPinned(ctx context.Context) {
	paths := f.PinnedPaths()
	if len(paths) == 0 || !f.isOnline() || f.replaying.Load() {
		return
	}
	call := rc.Calls.Get("vfs/mimus-download")
	if call == nil || !f.pins.running.CompareAndSwap(false, true) {
		return
	}
	defer f.pins.running.Store(false)
	out, err := call.Fn(ctx, rc.Params{"paths": paths})
	if err != nil {
		fs.Debugf(f, "file da tenere sul Mac: %v", err) // e.g. no VFS (not a mount)
		return
	}
	if n, _ := out["downloaded"].(int); n > 0 {
		fs.Infof(f, "scaricati %d file da tenere sempre su questo Mac", n)
	}
	missing, _ := out["missing"].([]string)
	for _, m := range missing {
		fs.Infof(f, "%q non c'è più: non è più da tenere su questo Mac", m)
		f.pinsRemoved(m)
	}
}
