package offline

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
)

// Novità fatte da altri sul server remoto. SMB non avvisa dei cambiamenti:
// in background si rileggono le cartelle aperte di recente (quelle che la
// VFS ha chiesto di elencare) e, se sono cambiate, si avvisa la VFS
// (ChangeNotify), che dimentica l'elenco in memoria: la volta dopo che il
// Finder guarda la cartella vede le novità, senza aspettare dir-cache-time.
//
// Per non caricare il server: solo online, al massimo watch_dirs cartelle
// usate negli ultimi watch_window; su rete lenta (VPN) meno cartelle e un
// giro ogni tre.

type watchSet struct {
	mu   sync.Mutex
	dirs map[string]time.Time // cartella -> ultimo elenco chiesto dalla VFS
}

func (f *Fs) watchMark(dir string) {
	if f.opt.WatchDirs <= 0 {
		return
	}
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	if f.watch.dirs == nil {
		f.watch.dirs = map[string]time.Time{}
	}
	f.watch.dirs[dir] = time.Now()
}

func (f *Fs) watchForget(dir string) {
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	delete(f.watch.dirs, dir)
}

// watchList returns the folders to check, most recently used first
func (f *Fs) watchList(now time.Time, max int) []string {
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	type item struct {
		dir string
		at  time.Time
	}
	var list []item
	for d, at := range f.watch.dirs {
		if now.Sub(at) > time.Duration(f.opt.WatchWindow) {
			delete(f.watch.dirs, d)
			continue
		}
		list = append(list, item{d, at})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	if len(list) > max {
		list = list[:max]
	}
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.dir
	}
	return out
}

// ChangeNotify implements fs.ChangeNotifier for the VFS (--poll-interval)
func (f *Fs) ChangeNotify(ctx context.Context, notify func(string, fs.EntryType), intervals <-chan time.Duration) {
	go func() {
		var ticker *time.Ticker
		var tick <-chan time.Time
		stop := func() {
			if ticker != nil {
				ticker.Stop()
				ticker, tick = nil, nil
			}
		}
		defer stop()
		round := 0
		for {
			select {
			case <-f.bgCtx.Done():
				return
			case d, ok := <-intervals:
				stop()
				if !ok {
					return
				}
				if d > 0 {
					ticker = time.NewTicker(d)
					tick = ticker.C
				}
			case <-tick:
				round++
				f.pollWatched(f.bgCtx, notify, round)
			}
		}
	}()
}

// pollWatched re-reads the watched folders; returns how many changed
func (f *Fs) pollWatched(ctx context.Context, notify func(string, fs.EntryType), round int) int {
	if !f.isOnline() || f.replaying.Load() {
		return 0
	}
	max := f.opt.WatchDirs
	if f.opt.Probe != "" && f.lastRTT() > time.Duration(f.opt.WalkMaxRTT) {
		if round%3 != 0 {
			return 0
		}
		max = min(max, 8)
	}
	changed := 0
	for _, dir := range f.watchList(time.Now(), max) {
		if ctx.Err() != nil || !f.isOnline() {
			break
		}
		if f.journal.touchesDir(dir) {
			continue // shown as the Mac sees it until the queue is applied
		}
		old, had := f.store.load(dir)
		entries, err := f.Fs.List(ctx, dir)
		if err != nil {
			if errors.Is(err, fs.ErrorDirNotFound) {
				f.store.drop(dir)
				f.watchForget(dir)
				fs.Debugf(f, "cartella %q tolta sul server remoto", dir)
				notify(dir, fs.EntryDirectory)
				changed++
				continue
			}
			if f.netErr(err) == errOther {
				continue // e.g. access denied on this folder only
			}
			break
		}
		cur := toSaved(ctx, entries)
		if had && sameSet(old, cur) {
			continue
		}
		f.store.save(ctx, dir, entries)
		fs.Debugf(f, "novità sul server remoto in %q", dir)
		notify(dir, fs.EntryDirectory)
		changed++
	}
	return changed
}

// sameSet compares two listings ignoring the order
func sameSet(a, b []savedEntry) bool {
	if len(a) != len(b) {
		return false
	}
	sorted := func(x []savedEntry) []savedEntry {
		y := append([]savedEntry(nil), x...)
		sort.Slice(y, func(i, j int) bool { return y[i].Name < y[j].Name })
		return y
	}
	return sameEntries(sorted(a), sorted(b))
}
