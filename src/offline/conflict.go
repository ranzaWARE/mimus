package offline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/lib/diskusage"
)

// ---------- upload conflicts (called by the patched VFS cache) ----------

// ResolveUploadConflict is called before a locally modified file is
// uploaded. If the remote copy is not the version the local edit started
// from, the user chooses what to do.
func (f *Fs) ResolveUploadConflict(ctx context.Context, name, baseFingerprint string, fast bool) (action, newName string, current fs.Object, err error) {
	if !f.isOnline() {
		return "", "", nil, errOffline
	}
	// renames/deletes made offline go first
	if err := f.waitJournal(ctx); err != nil {
		return "", "", nil, err
	}
	base, err := f.Fs.NewObject(ctx, name)
	if errors.Is(err, fs.ErrorObjectNotFound) {
		if baseFingerprint == "" {
			return "", "", nil, nil // new file
		}
		const discard, recreate = "Scarta le mie modifiche", "Ricrea il file"
		msg := fmt.Sprintf("Hai modificato «%s», ma nel frattempo qualcuno l'ha cancellato (o spostato) sul server remoto.\n\nCosa faccio?", name)
		ans, err := f.ask(ctx, msg, recreate, discard, recreate)
		if err != nil {
			return "", "", nil, err
		}
		if ans == discard {
			return "discard", "", nil, nil
		}
		return "", "", nil, nil
	}
	if err != nil {
		f.netErr(err)
		return "", "", nil, err
	}
	cur := f.wrapObject(base)
	curFingerprint := fs.Fingerprint(ctx, cur, fast)
	if curFingerprint == baseFingerprint {
		return "", "", cur, nil
	}
	// a new local file over a remote file the Mac already knew, unchanged since
	// (Finder "replace", or an app's safe-save): the user replaced it on purpose
	if baseFingerprint == "" {
		if e, ok := f.store.entry(name); ok && !e.IsDir && f.sameVersion(cur, e.Size, e.ModTime) {
			return "", "", cur, nil
		}
	}
	const (
		discard   = "Scarta la mia"
		keepBoth  = "Tieni entrambe"
		overwrite = "Sovrascrivi in remoto"
	)
	var msg string
	if baseFingerprint == "" {
		msg = fmt.Sprintf("Hai creato «%s», ma nel frattempo qualcuno ha messo sul server remoto un file con lo stesso nome.", name)
	} else {
		msg = fmt.Sprintf("«%s» è stato modificato sul server remoto da qualcun altro mentre lo modificavi anche tu.", name)
	}
	alt, err := f.uniqueConflictName(ctx, name, false)
	if err != nil {
		f.netErr(err)
		return "", "", nil, err
	}
	msg += fmt.Sprintf("\n\nVersione remota: %s, %s\n\n• Scarta la mia: resta quella remota\n• Tieni entrambe: la tua viene salvata come «%s»\n• Sovrascrivi: la tua sostituisce quella remota",
		cur.ModTime(ctx).Local().Format("02/01/2006 15:04"), humanSize(cur.Size()), path.Base(alt))
	ans, err := f.ask(ctx, msg, keepBoth, discard, keepBoth, overwrite)
	if err != nil {
		return "", "", nil, err
	}
	switch ans {
	case discard:
		return "discard", "", cur, nil
	case overwrite:
		return "overwrite", "", cur, nil
	}
	// the name may have been taken while the dialog was open
	if exists, err := f.fileExists(ctx, alt); err != nil || exists {
		if alt, err = f.uniqueConflictName(ctx, name, false); err != nil {
			return "", "", nil, err
		}
	}
	return "keepboth", alt, cur, nil
}

// ---------- cache eviction (called by the patched VFS cache) ----------

// CacheEvictionAllowed is asked before a local copy is removed (not used
// for vfs-cache-max-age, or disk below the free space threshold): allowed
// only if the remote is reachable and still holds the file.
func (f *Fs) CacheEvictionAllowed(ctx context.Context, remote string) bool {
	if f.IsPinned(remote) {
		return false // «Mantieni sempre su questo Mac»
	}
	if !f.isOnline() || f.journal.touchesPath(remote) {
		return false
	}
	_, err := f.Fs.NewObject(ctx, remote)
	if err != nil {
		if errors.Is(err, fs.ErrorObjectNotFound) {
			if _, warned := f.orphans.LoadOrStore(remote, true); !warned {
				fs.Logf(f, "%q non è più sul server remoto: copia locale mantenuta in cache", remote)
			}
		} else {
			f.netErr(err)
		}
		return false
	}
	fs.Infof(f, "%q è sul server remoto: tolto dalla cache locale", remote)
	return true
}

// ---------- free disk space ----------

// diskMonitor warns the user when the free space on the cache disk stays
// below min_free (the VFS cache already removes old files when it can).
func (f *Fs) diskMonitor(ctx context.Context) {
	defer f.wg.Done()
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	below := 0
	var lastWarn time.Time
	for first := true; ; first = false {
		if !first {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
		du, err := diskusage.New(config.GetCacheDir())
		if err != nil {
			continue
		}
		if du.Available >= uint64(f.opt.MinFree) {
			below = 0
			f.diskLow.Store(false)
			continue
		}
		f.diskLow.Store(true)
		below++
		if below < 2 || time.Since(lastWarn) < 2*time.Hour {
			continue
		}
		lastWarn = time.Now()
		cache := dirSize(filepath.Join(config.GetCacheDir(), "vfs"))
		msg := fmt.Sprintf("Spazio quasi esaurito sul disco del Mac: liberi %s, la soglia minima è %s.\n\nI file remoti tenuti sul Mac occupano circa %s.",
			humanSize(int64(du.Available)), humanSize(int64(f.opt.MinFree)), humanSize(cache))
		if f.isOnline() {
			msg += "\n\nHo già tolto dal Mac i file che non servono e che sono al sicuro sul server remoto: per liberare altro spazio cancella file tuoi o svuota il Cestino."
		} else {
			msg += "\n\nSei offline: i file verranno tolti dal Mac quando torni in rete. Nel frattempo evita di aprire file grandi."
		}
		f.notify(msg)
	}
}

func dirSize(root string) (n int64) {
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d byte", n)
}
