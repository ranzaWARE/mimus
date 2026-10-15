// Package offline is an overlay backend for a remote share used through the
// VFS (rclone nfsmount / mount with --vfs-cache-mode full). It adds:
//
//   - a persistent copy of every directory listing, so the folder tree is
//     browsable offline, even after a restart
//   - a journal of changes made offline (new folders, renames, deletions)
//     replayed on the remote when it is reachable again, asking the user what
//     to do when the remote changed meanwhile
//   - conflict checks before uploads and safe cache eviction (through a
//     small patch to vfs/vfscache, see src/patches)
//   - login failure handling: stop retrying (no account lockout) and ask
//     for the new password
//   - a free disk space warning
//
// File contents are not stored here: the VFS cache keeps opened files.
package offline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/fspath"
	"github.com/rclone/rclone/fs/hash"
)

func init() {
	fs.Register(&fs.RegInfo{
		Name:        "offline",
		Description: "Remote folder with offline folder tree, offline changes queue and conflict dialogs",
		NewFs:       NewFs,
		Options: []fs.Option{{
			Name:     "remote",
			Required: true,
			Help:     "Remote to wrap (e.g. remoto:Share).",
		}, {
			Name:    "probe",
			Default: "",
			Help:    "host:port checked with a quick TCP connect to decide online/offline (e.g. 192.168.1.10:445).\nEmpty = always online.",
		}, {
			Name:    "probe_timeout",
			Default: fs.Duration(2 * time.Second),
			Help:    "Timeout of the probe connection.",
		}, {
			Name:    "walk_interval",
			Default: fs.Duration(2 * time.Hour),
			Help:    "Minimum time between two complete reads of the folder tree (0 = never).",
		}, {
			Name:    "walk_max_rtt",
			Default: fs.Duration(15 * time.Millisecond),
			Help:    "Complete reads only when the probe answers within this time (office LAN, not VPN).",
		}, {
			Name:    "walk_force_age",
			Default: fs.Duration(7 * 24 * time.Hour),
			Help:    "Do a complete read even on a slow link if the last one is older than this.",
		}, {
			Name:     "walk_workers",
			Default:  2,
			Advanced: true,
			Help:     "Parallel folder reads during a complete read.",
		}, {
			Name:     "walk_pace",
			Default:  fs.Duration(10 * time.Millisecond),
			Advanced: true,
			Help:     "Pause after each folder read during a complete read.",
		}, {
			Name:    "watch_dirs",
			Default: 32,
			Help:    "Folders opened recently that are re-read in background to show at once the changes made by others\n(every --poll-interval of the mount; 0 = off).",
		}, {
			Name:    "watch_window",
			Default: fs.Duration(time.Hour),
			Help:    "A folder is re-read in background for this long after it was last opened.",
		}, {
			Name:    "min_free",
			Default: fs.SizeSuffix(0),
			Help:    "Warn the user when the free space of the cache disk stays below this (0 = off).\nUse the same value as --vfs-cache-min-free-space.",
		}, {
			Name:    "ui",
			Default: "auto",
			Help:    "Dialogs: auto (macOS dialogs), none, or a program (see ui.go).",
		}, {
			Name:     "ask_timeout",
			Default:  fs.Duration(time.Hour),
			Advanced: true,
			Help:     "After this time without an answer the safe choice is used.",
		}, {
			Name:     "restart_on_password",
			Default:  true,
			Advanced: true,
			Help:     "Restart (launchd brings us back) after a new password has been saved.",
		}, {
			Name:     "low_level_retries",
			Default:  3,
			Advanced: true,
			Help:     "Low level retries of the wrapped remote (few, to avoid account lockout on a wrong password).",
		}, {
			Name:     "dir",
			Default:  "",
			Advanced: true,
			Help:     "Where to store listings and journal (default: <cache-dir>/offline/<name>).",
		}},
	})
}

// Options of the backend
type Options struct {
	Remote            string        `config:"remote"`
	Probe             string        `config:"probe"`
	ProbeTimeout      fs.Duration   `config:"probe_timeout"`
	WalkInterval      fs.Duration   `config:"walk_interval"`
	WalkMaxRTT        fs.Duration   `config:"walk_max_rtt"`
	WalkForceAge      fs.Duration   `config:"walk_force_age"`
	WalkWorkers       int           `config:"walk_workers"`
	WalkPace          fs.Duration   `config:"walk_pace"`
	WatchDirs         int           `config:"watch_dirs"`
	WatchWindow       fs.Duration   `config:"watch_window"`
	MinFree           fs.SizeSuffix `config:"min_free"`
	UI                string        `config:"ui"`
	AskTimeout        fs.Duration   `config:"ask_timeout"`
	RestartOnPassword bool          `config:"restart_on_password"`
	LowLevelRetries   int           `config:"low_level_retries"`
	Dir               string        `config:"dir"`
}

// Fs wraps another Fs
type Fs struct {
	fs.Fs
	name     string
	root     string
	opt      Options
	features *fs.Features
	wrapper  fs.Fs
	store    *store
	journal  *journal

	stateMu      sync.Mutex
	online       bool // last probe result
	known        bool
	lastLogged   bool
	rtt          time.Duration
	offlineUntil time.Time // network backoff
	lastNetErr   time.Time
	authFailed   bool
	probeWake    chan struct{}
	reconnect    func() // set by the VFS cache: retry uploads now

	replayMu   sync.Mutex
	replayWake chan struct{}
	walkWake   chan struct{}
	watch      watchSet
	pins       *pinSet
	lnOnce     sync.Once // «Rete locale» hint shown once
	replaying  atomic.Bool
	walking    atomic.Bool
	diskLow    atomic.Bool

	bgCtx  context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFs builds the Fs
func NewFs(ctx context.Context, name, rpath string, m configmap.Mapper) (fs.Fs, error) {
	var opt Options
	if err := configstruct.Set(m, &opt); err != nil {
		return nil, err
	}
	if strings.HasPrefix(opt.Remote, name+":") {
		return nil, errors.New("can't point remote at itself")
	}
	// few low level retries: a wrong password must not lock the account
	bctx, ci := fs.AddConfig(ctx)
	if opt.LowLevelRetries > 0 {
		ci.LowLevelRetries = opt.LowLevelRetries
	}
	// trailing "/": the root is a folder, so backends like SMB don't need to
	// contact the server to check it (offline startup must work)
	basePath := fspath.JoinRootPath(opt.Remote, rpath)
	if !strings.HasSuffix(basePath, "/") && !strings.HasSuffix(basePath, ":") {
		basePath += "/"
	}
	baseFs, err := cache.Get(bctx, basePath)
	if err != nil && err != fs.ErrorIsFile {
		return nil, fmt.Errorf("failed to make base remote %q: %w", opt.Remote, err)
	}
	dir := opt.Dir
	if dir == "" {
		dir = filepath.Join(config.GetCacheDir(), "offline", name)
	}
	st, serr := newStore(filepath.Join(dir, shortHash(fspath.JoinRootPath(opt.Remote, rpath))))
	if serr != nil {
		return nil, serr
	}
	jr, corrupt := newJournal(st.path)
	f := &Fs{
		Fs: baseFs, name: name, root: rpath, opt: opt, store: st,
		journal:    jr,
		pins:       newPinSet(st.path),
		replayWake: make(chan struct{}, 1),
		walkWake:   make(chan struct{}, 1),
		probeWake:  make(chan struct{}, 1),
	}
	f.bgCtx, f.cancel = context.WithCancel(fs.CopyConfig(context.Background(), ctx))
	f.features = (&fs.Features{
		CanHaveEmptyDirectories: true,
		PartialUploads:          true,
	}).Fill(ctx, f).Mask(ctx, baseFs).WrapsFs(f, baseFs)
	// SMB has no change notifications: ours re-reads the folders in use
	f.features.ChangeNotify = f.ChangeNotify
	cache.PinUntilFinalized(baseFs, f)

	if err == nil {
		active.Store(f, struct{}{})
		bg := f.bgCtx
		f.probeOnce() // know the state before the first operation
		if opt.Probe != "" {
			f.wg.Add(1)
			go f.prober(bg)
		}
		f.wg.Add(1)
		go f.replayer(bg)
		f.wg.Add(1)
		go f.pinKeeper(bg)
		// first look for missing pinned files and leftover copies soon after the start
		time.AfterFunc(45*time.Second, f.wakePins)
		if opt.WalkInterval > 0 {
			f.wg.Add(1)
			go f.walker(bg)
		}
		if opt.MinFree > 0 {
			f.wg.Add(1)
			go f.diskMonitor(bg)
		}
		if n := f.journal.len(); n > 0 {
			fs.Logf(f, "%d modifiche fatte offline in attesa di essere applicate sul server remoto", n)
		}
		if corrupt != "" {
			f.notify(fmt.Sprintf("Il registro delle modifiche fatte offline era danneggiato ed è stato messo da parte (%s): alcune rinomine o cancellazioni fatte offline potrebbero non essere state applicate sul server remoto. Controlla le ultime modifiche.", filepath.Base(corrupt)))
		}
	}
	return f, err
}

// Name of the remote
func (f *Fs) Name() string { return f.name }

// Root of the remote
func (f *Fs) Root() string { return f.root }

// String description
func (f *Fs) String() string { return fmt.Sprintf("offline::%s:%s", f.name, f.root) }

// Features of the Fs
func (f *Fs) Features() *fs.Features { return f.features }

// UnWrap returns the wrapped Fs
func (f *Fs) UnWrap() fs.Fs { return f.Fs }

// WrapFs returns the Fs wrapping this one
func (f *Fs) WrapFs() fs.Fs { return f.wrapper }

// SetWrapper sets the Fs wrapping this one
func (f *Fs) SetWrapper(w fs.Fs) { f.wrapper = w }

// ---------- listing ----------

// listOnline lists dir on the remote and saves the listing, unless queued
// changes touch it (the saved copy is then the Mac's view and must stay)
func (f *Fs) listOnline(ctx context.Context, dir string) (fs.DirEntries, error) {
	entries, err := f.Fs.List(ctx, dir)
	if err == nil {
		if !f.journal.touchesDir(dir) {
			f.store.save(ctx, dir, entries)
		}
		return entries, nil
	}
	if errors.Is(err, fs.ErrorDirNotFound) && !f.journal.touchesDir(dir) {
		f.store.drop(dir)
	}
	return nil, err
}

// List the directory
func (f *Fs) List(ctx context.Context, dir string) (fs.DirEntries, error) {
	f.watchMark(dir)
	var err error
	if f.isOnline() {
		// folders with queued changes are shown as the Mac sees them
		if f.journal.touchesDir(dir) {
			if saved, ok := f.store.load(dir); ok {
				return f.ghostEntries(dir, saved), nil
			}
		}
		var entries fs.DirEntries
		entries, err = f.listOnline(ctx, dir)
		if err == nil {
			return f.wrapEntries(entries), nil
		}
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, err
		}
		f.netErr(err)
	}
	saved, ok := f.store.load(dir)
	if !ok {
		if err == nil {
			err = errOffline
		}
		return nil, fmt.Errorf("cartella non disponibile offline: %w", err)
	}
	return f.ghostEntries(dir, saved), nil
}

func (f *Fs) wrapEntries(in fs.DirEntries) fs.DirEntries {
	out := make(fs.DirEntries, 0, len(in))
	for _, e := range in {
		if o, ok := e.(fs.Object); ok {
			out = append(out, f.wrapObject(o))
		} else {
			out = append(out, e)
		}
	}
	return out
}

func (f *Fs) ghostEntries(dir string, saved []savedEntry) fs.DirEntries {
	out := make(fs.DirEntries, 0, len(saved))
	for _, s := range saved {
		remote := path.Join(dir, s.Name)
		if s.IsDir {
			out = append(out, fs.NewDir(remote, s.ModTime))
		} else {
			out = append(out, f.ghost(remote, s))
		}
	}
	return out
}

func (f *Fs) ghost(remote string, s savedEntry) *Object {
	return &Object{f: f, remote: remote, size: s.Size, modTime: s.ModTime}
}

// NewObject finds an object
func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	if f.isOnline() && !f.journal.touchesPath(remote) {
		o, err := f.Fs.NewObject(ctx, remote)
		if err == nil {
			return f.wrapObject(o), nil
		}
		if errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorIsDir) {
			return nil, err
		}
		f.netErr(err)
	}
	if e, ok := f.store.entry(remote); ok && !e.IsDir {
		return f.ghost(remote, e), nil
	}
	return nil, fs.ErrorObjectNotFound
}

// ---------- writes ----------

// readyToWrite: uploads need the remote and the queued changes applied first
func (f *Fs) readyToWrite(ctx context.Context) error {
	if !f.isOnline() {
		return errOffline
	}
	return f.waitJournal(ctx)
}

// Put uploads a new object
func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	if err := f.readyToWrite(ctx); err != nil {
		return nil, err
	}
	o, err := f.Fs.Put(ctx, in, src, options...)
	if err != nil {
		f.netErr(err)
		return nil, err
	}
	f.store.upsertObject(ctx, o)
	return f.wrapObject(o), nil
}

// PutStream uploads an object of unknown size
func (f *Fs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	do := f.Fs.Features().PutStream
	if do == nil {
		return nil, errors.New("PutStream not supported")
	}
	if err := f.readyToWrite(ctx); err != nil {
		return nil, err
	}
	o, err := do(ctx, in, src, options...)
	if err != nil {
		f.netErr(err)
		return nil, err
	}
	f.store.upsertObject(ctx, o)
	return f.wrapObject(o), nil
}

// Mkdir makes a directory (queued while offline)
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	if !f.queueing() {
		err := f.Fs.Mkdir(ctx, dir)
		if err == nil {
			f.store.upsertDir(dir)
			return nil
		}
		f.netErr(err)
		if f.isOnline() {
			return err // a real error, not the network
		}
	}
	if err := f.enqueue(jop{Op: "mkdir", Src: dir}); err != nil {
		return err
	}
	f.store.upsertDir(dir)
	return nil
}

// Rmdir removes an empty directory (queued while offline)
func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	if f.queueing() {
		if err := f.enqueue(jop{Op: "rmdir", Src: dir}); err != nil {
			return err
		}
	} else if err := f.Fs.Rmdir(ctx, dir); err != nil {
		f.netErr(err)
		return err
	}
	f.store.removeEntry(dir)
	f.store.drop(dir)
	f.pinsRemoved(dir)
	return nil
}

// Purge deletes a directory and its contents (online only)
func (f *Fs) Purge(ctx context.Context, dir string) error {
	do := f.Fs.Features().Purge
	if do == nil {
		return fs.ErrorCantPurge
	}
	if f.queueing() {
		return errOffline
	}
	if err := do(ctx, dir); err != nil {
		return err
	}
	f.store.removeEntry(dir)
	f.store.dropTree(dir)
	f.pinsRemoved(dir)
	return nil
}

// Move renames a file (queued while offline)
func (f *Fs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	do := f.Fs.Features().Move
	o, ok := src.(*Object)
	if do == nil || !ok {
		return nil, fs.ErrorCantMove
	}
	// last step of an upload (temp ".partial" file renamed to its name):
	// part of the upload, never queued
	if o.hasBase() && isPartial(ctx, o.remote) {
		base, err := o.resolve(ctx)
		if err != nil {
			return nil, err
		}
		res, err := do(ctx, base, remote)
		if err != nil {
			f.netErr(err)
			return nil, err
		}
		f.store.removeEntry(o.remote)
		f.store.upsertObject(ctx, res)
		return f.wrapObject(res), nil
	}
	if f.queueing() {
		size, mt := o.version()
		if err := f.enqueue(jop{Op: "move", Src: o.remote, Dst: remote, Size: size, ModTime: mt}); err != nil {
			return nil, err
		}
		e := savedEntry{Size: size, ModTime: mt}
		f.store.removeEntry(o.remote)
		f.store.upsert(remote, e)
		return f.ghost(remote, e), nil
	}
	base, err := o.resolve(ctx)
	if err != nil {
		return nil, err
	}
	res, err := do(ctx, base, remote)
	if err != nil {
		f.netErr(err)
		return nil, err
	}
	f.store.removeEntry(o.remote)
	f.store.upsertObject(ctx, res)
	return f.wrapObject(res), nil
}

// DirMove renames a directory (queued while offline)
func (f *Fs) DirMove(ctx context.Context, src fs.Fs, srcRemote, dstRemote string) error {
	do := f.Fs.Features().DirMove
	srcFs, ok := src.(*Fs)
	if do == nil || !ok || srcFs != f {
		return fs.ErrorCantDirMove
	}
	if f.queueing() {
		if err := f.enqueue(jop{Op: "dirmove", Src: srcRemote, Dst: dstRemote}); err != nil {
			return err
		}
	} else if err := do(ctx, f.Fs, srcRemote, dstRemote); err != nil {
		f.netErr(err)
		return err
	}
	f.store.renameTree(srcRemote, dstRemote)
	f.pinsRenamed(srcRemote, dstRemote)
	return nil
}

// About returns quota (Finder free space)
func (f *Fs) About(ctx context.Context) (*fs.Usage, error) {
	do := f.Fs.Features().About
	if do == nil {
		return nil, errors.New("not supported")
	}
	if !f.isOnline() {
		return nil, errOffline
	}
	return do(ctx)
}

// Shutdown stops the background tasks
func (f *Fs) Shutdown(ctx context.Context) error {
	active.Delete(f)
	if f.cancel != nil {
		f.cancel()
		f.wg.Wait()
	}
	if do := f.Fs.Features().Shutdown; do != nil {
		return do(ctx)
	}
	return nil
}

// ---------- object ----------

// Object is a file of the remote, possibly known only from the saved listing
// (base == nil)
type Object struct {
	f       *Fs
	remote  string
	size    int64
	modTime time.Time
	mu      sync.Mutex
	base    fs.Object
}

func (f *Fs) wrapObject(o fs.Object) *Object {
	return &Object{f: f, remote: o.Remote(), size: o.Size(), modTime: o.ModTime(context.Background()), base: o}
}

func (o *Object) resolve(ctx context.Context) (fs.Object, error) {
	// even with a known remote object: never touch the remote while offline or
	// after a login failure (would hang, or lock the account)
	if !o.f.isOnline() {
		return nil, errOffline
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.base != nil {
		return o.base, nil
	}
	b, err := o.f.Fs.NewObject(ctx, o.remote)
	if err != nil {
		o.f.netErr(err)
		return nil, err
	}
	o.base = b
	return b, nil
}

// version returns size and modification time as known
func (o *Object) version() (int64, time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.base != nil {
		return o.base.Size(), o.base.ModTime(context.Background())
	}
	return o.size, o.modTime
}

// Fs returns the parent Fs
func (o *Object) Fs() fs.Info { return o.f }

// String returns the remote path
func (o *Object) String() string {
	if o == nil {
		return "<nil>"
	}
	return o.remote
}

// Remote returns the remote path
func (o *Object) Remote() string { return o.remote }

// ModTime returns the modification time
func (o *Object) ModTime(ctx context.Context) time.Time {
	_, mt := o.version()
	return mt
}

// Size returns the size
func (o *Object) Size() int64 {
	size, _ := o.version()
	return size
}

// Storable says whether the object can be stored
func (o *Object) Storable() bool { return true }

// Hash returns the hash of the object
func (o *Object) Hash(ctx context.Context, ht hash.Type) (string, error) {
	o.mu.Lock()
	b := o.base
	o.mu.Unlock()
	if b == nil {
		return "", hash.ErrUnsupported
	}
	return b.Hash(ctx, ht)
}

// SetModTime sets the modification time
func (o *Object) SetModTime(ctx context.Context, t time.Time) error {
	if o.f.queueing() {
		return errOffline
	}
	b, err := o.resolve(ctx)
	if err != nil {
		return err
	}
	if err = b.SetModTime(ctx, t); err == nil {
		o.f.store.upsertObject(ctx, b)
	}
	return err
}

// Open opens the object for reading
func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	b, err := o.resolve(ctx)
	if err != nil {
		return nil, err
	}
	rc, err := b.Open(ctx, options...)
	if err != nil {
		o.f.netErr(err)
	}
	return rc, err
}

// Update replaces the contents of the object
func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	if err := o.f.readyToWrite(ctx); err != nil {
		return err
	}
	b, err := o.resolve(ctx)
	if errors.Is(err, fs.ErrorObjectNotFound) {
		// gone from the remote meanwhile: upload as new
		nb, perr := o.f.Fs.Put(ctx, in, src, options...)
		if perr != nil {
			o.f.netErr(perr)
			return perr
		}
		o.mu.Lock()
		o.base = nb
		o.mu.Unlock()
		o.f.store.upsertObject(ctx, nb)
		return nil
	}
	if err != nil {
		return err
	}
	if err = b.Update(ctx, in, src, options...); err != nil {
		o.f.netErr(err)
		return err
	}
	o.f.store.upsertObject(ctx, b)
	return nil
}

// Remove deletes the object (queued while offline)
func (o *Object) Remove(ctx context.Context) error {
	if o.f.queueing() {
		size, mt := o.version()
		if err := o.f.enqueue(jop{Op: "remove", Src: o.remote, Size: size, ModTime: mt}); err != nil {
			return err
		}
		o.f.store.removeEntry(o.remote)
		return nil
	}
	b, err := o.resolve(ctx)
	if err != nil {
		return err
	}
	if err = b.Remove(ctx); err != nil {
		o.f.netErr(err)
		return err
	}
	o.f.store.removeEntry(o.remote)
	return nil
}

// SetReconnectHook is called by the (patched) VFS cache
func (f *Fs) SetReconnectHook(fn func()) {
	f.stateMu.Lock()
	f.reconnect = fn
	f.stateMu.Unlock()
}

func (o *Object) hasBase() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.base != nil
}

// isPartial reports whether remote is a temporary upload name
func isPartial(ctx context.Context, remote string) bool {
	suffix := fs.GetConfig(ctx).PartialSuffix
	return suffix != "" && strings.HasSuffix(remote, suffix)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// Check the interfaces are satisfied
var (
	_ fs.Fs          = (*Fs)(nil)
	_ fs.Purger      = (*Fs)(nil)
	_ fs.Mover       = (*Fs)(nil)
	_ fs.DirMover    = (*Fs)(nil)
	_ fs.PutStreamer = (*Fs)(nil)
	_ fs.Abouter     = (*Fs)(nil)
	_ fs.Shutdowner  = (*Fs)(nil)
	_ fs.UnWrapper   = (*Fs)(nil)
	_ fs.Wrapper     = (*Fs)(nil)
	_ fs.Object      = (*Object)(nil)
	_ interface {
		CacheEvictionAllowed(context.Context, string) bool
		ResolveUploadConflict(context.Context, string, string, bool) (string, string, fs.Object, error)
		SetReconnectHook(func())
	} = (*Fs)(nil)
)
