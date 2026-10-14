package offline

import (
	"context"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
)

// Background re-read of the whole folder tree, so every folder is known
// offline. To keep the load on the remote low it runs:
//   - only when the last complete read is older than walk_interval
//   - only on a fast link (probe answer <= walk_max_rtt, i.e. office LAN,
//     not VPN), unless the last complete read is older than walk_force_age
//   - with few parallel requests and a pause between them
// Folders browsed normally are refreshed anyway by the regular listings.

func (f *Fs) wakeWalker() {
	select {
	case f.walkWake <- struct{}{}:
	default:
	}
}

func (f *Fs) walker(ctx context.Context) {
	defer f.wg.Done()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-f.walkWake:
		}
		f.maybeWalk(ctx)
		timer.Reset(10 * time.Minute)
	}
}

func (f *Fs) maybeWalk(ctx context.Context) {
	if !f.isOnline() {
		return
	}
	meta := f.store.walkMeta()
	age := time.Since(meta.Last)
	if age < time.Duration(f.opt.WalkInterval) {
		return
	}
	rtt := f.lastRTT()
	fast := f.opt.Probe == "" || rtt <= time.Duration(f.opt.WalkMaxRTT)
	if !fast && age < time.Duration(f.opt.WalkForceAge) {
		fs.Debugf(f, "lettura completa rimandata: rete lenta (%v), probabilmente VPN", rtt)
		return
	}
	start := time.Now()
	n, err := f.walk(ctx)
	if err != nil {
		fs.Logf(f, "lettura completa delle cartelle interrotta: %v", err)
		return
	}
	took := time.Since(start)
	f.store.setWalkMeta(walkMeta{Last: start, Folders: n, Took: took})
	fs.Infof(f, "struttura cartelle salvata: %d cartelle in %v", n, took.Round(time.Second))
}

func (f *Fs) walk(ctx context.Context) (int, error) {
	start := time.Now()
	workers := max(f.opt.WalkWorkers, 1)
	pace := time.Duration(f.opt.WalkPace)
	visited := map[string]struct{}{}
	var mu sync.Mutex
	queue := []string{""}
	for len(queue) > 0 {
		batch := queue
		queue = nil
		var wg sync.WaitGroup
		var firstErr error
		sem := make(chan struct{}, workers)
		for _, dir := range batch {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if !f.isOnline() {
				return 0, errOffline
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(dir string) {
				defer wg.Done()
				defer func() {
					if pace > 0 {
						time.Sleep(pace)
					}
					<-sem
				}()
				entries, err := f.listOnline(ctx, dir)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					switch f.classify(err) {
					case errNotFound:
					case errOther: // e.g. access denied on one folder: skip it
						fs.Debugf(f, "lettura completa: salto %q: %v", dir, err)
						visited[dir] = struct{}{} // keep what we had
					default:
						if firstErr == nil {
							firstErr = err
						}
					}
					return
				}
				visited[dir] = struct{}{}
				for _, e := range entries {
					if d, ok := e.(fs.Directory); ok {
						queue = append(queue, d.Remote())
					}
				}
			}(dir)
		}
		wg.Wait()
		if firstErr != nil {
			f.netErr(firstErr)
			return 0, firstErr
		}
	}
	f.store.prune(visited, start, f.journal.touchesDir)
	return len(visited), nil
}
