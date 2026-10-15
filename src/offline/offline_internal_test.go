package offline

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/rclone/rclone/backend/local"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
)

// newTestFs wraps a local directory; online state is driven by a TCP
// listener used as probe.
func newTestFs(t *testing.T) (*Fs, string, func(bool)) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	probe, setOnline := testProbe(t)
	m := configmap.Simple{
		"type":          "offline",
		"remote":        base,
		"probe":         probe,
		"probe_timeout": "200ms",
		"walk_interval": "0",
		"walk_max_rtt":  "15ms",
		"watch_dirs":    "32",
		"watch_window":  "1h",
		"ui":            "none",
		"dir":           t.TempDir(),
	}
	fsys, err := NewFs(ctx, "test", "", m)
	if err != nil {
		t.Fatal(err)
	}
	f := fsys.(*Fs)
	t.Cleanup(func() { _ = f.Shutdown(ctx) })
	return f, base, func(on bool) {
		setOnline(on)
		f.stateMu.Lock()
		f.offlineUntil = time.Time{}
		f.stateMu.Unlock()
		f.probeOnce()
	}
}

func testProbe(t *testing.T) (string, func(bool)) {
	ln, err := netListen()
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	stop := make(chan struct{})
	go acceptLoop(ln, stop)
	on := true
	return addr, func(online bool) {
		if online == on {
			return
		}
		on = online
		if !online {
			_ = ln.Close()
			close(stop)
			return
		}
		ln, err = netListenAddr(addr)
		if err != nil {
			t.Fatal(err)
		}
		stop = make(chan struct{})
		go acceptLoop(ln, stop)
	}
}

func names(entries fs.DirEntries) string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Remote())
	}
	return strings.Join(out, ",")
}

func TestOfflineListingSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	must(t, os.MkdirAll(filepath.Join(base, "a/b"), 0o755))
	must(t, os.WriteFile(filepath.Join(base, "a/b/f.txt"), []byte("x"), 0o644))

	for _, d := range []string{"", "a", "a/b"} {
		if _, err := f.List(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	setOnline(false)
	entries, err := f.List(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(entries); got != "a/b/f.txt" {
		t.Fatalf("offline listing = %q", got)
	}
	if _, err := f.NewObject(ctx, "a/b/f.txt"); err != nil {
		t.Fatalf("offline NewObject: %v", err)
	}
}

func TestJournalReplay(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	must(t, os.MkdirAll(filepath.Join(base, "d/sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(base, "d/a.txt"), []byte("a"), 0o644))
	must(t, os.WriteFile(filepath.Join(base, "d/c.txt"), []byte("c"), 0o644))
	for _, d := range []string{"", "d", "d/sub"} {
		_, _ = f.List(ctx, d)
	}
	setOnline(false)

	a, _ := f.NewObject(ctx, "d/a.txt")
	if _, err := f.Move(ctx, a, "d/a2.txt"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.NewObject(ctx, "d/c.txt")
	must(t, c.Remove(ctx))
	must(t, f.Mkdir(ctx, "new"))
	must(t, f.DirMove(ctx, f, "d/sub", "d/sub2"))

	entries, _ := f.List(ctx, "d")
	if got := names(entries); got != "d/a2.txt,d/sub2" {
		t.Fatalf("local view = %q", got)
	}
	if f.journal.len() != 4 {
		t.Fatalf("journal len = %d", f.journal.len())
	}

	setOnline(true)
	f.replay(ctx)
	if f.journal.len() != 0 {
		t.Fatalf("journal not empty after replay")
	}
	for _, p := range []string{"d/a2.txt", "new", "d/sub2"} {
		if _, err := os.Stat(filepath.Join(base, p)); err != nil {
			t.Errorf("%s missing on remote: %v", p, err)
		}
	}
	for _, p := range []string{"d/a.txt", "d/c.txt", "d/sub"} {
		if _, err := os.Stat(filepath.Join(base, p)); err == nil {
			t.Errorf("%s should be gone from remote", p)
		}
	}
}

func TestJournalRemoveConflictKeepsFile(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	p := filepath.Join(base, "x.txt")
	must(t, os.WriteFile(p, []byte("v1"), 0o644))
	_, _ = f.List(ctx, "")
	setOnline(false)
	o, _ := f.NewObject(ctx, "x.txt")
	must(t, o.Remove(ctx))
	// changed on the remote meanwhile (size and time)
	must(t, os.WriteFile(p, []byte("v2 longer"), 0o644))
	must(t, os.Chtimes(p, time.Now().Add(time.Hour), time.Now().Add(time.Hour)))
	setOnline(true)
	f.replay(ctx) // ui=none -> safe choice "Non cancellare"
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file modified on remote was deleted: %v", err)
	}
}

func TestCancelledRenameRewritesLaterOps(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	p := filepath.Join(base, "a.txt")
	must(t, os.WriteFile(p, []byte("v1"), 0o644))
	_, _ = f.List(ctx, "")
	setOnline(false)
	a, _ := f.NewObject(ctx, "a.txt")
	b, err := f.Move(ctx, a, "b.txt")
	must(t, err)
	_, err = f.Move(ctx, b, "c.txt")
	must(t, err)
	// a.txt modified on the remote: the first rename is cancelled (safe default)
	must(t, os.WriteFile(p, []byte("v2 changed"), 0o644))
	must(t, os.Chtimes(p, time.Now().Add(time.Hour), time.Now().Add(time.Hour)))
	setOnline(true)
	f.replay(ctx)
	// the second rename must now refer to a.txt, not to a colleague's b.txt
	f.journal.mu.Lock()
	defer f.journal.mu.Unlock()
	for _, op := range f.journal.ops {
		if op.Src == "b.txt" {
			t.Fatalf("later op still points at b.txt: %+v", op)
		}
	}
}

func TestPartialUploadRenameNotQueued(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	must(t, os.WriteFile(filepath.Join(base, "x.txt.1234abcd.partial"), []byte("new"), 0o644))
	_, _ = f.List(ctx, "")
	o, err := f.NewObject(ctx, "x.txt.1234abcd.partial")
	must(t, err)
	// a queued change exists: normal renames would be queued
	must(t, f.journal.add(jop{Op: "mkdir", Src: "zz", NextTry: time.Now().Add(time.Hour)}))
	if _, err := f.Move(ctx, o, "x.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "x.txt")); err != nil {
		t.Fatal("partial upload rename was queued instead of applied")
	}
	setOnline(false)
	o2 := &Object{f: f, remote: "y.partial", base: o}
	if _, err := f.Move(ctx, o2, "y"); err == nil {
		t.Fatal("offline partial rename must fail so the upload is retried")
	}
}

func TestFailingOpIsParked(t *testing.T) {
	f, _, _ := newTestFs(t)
	must(t, f.journal.add(jop{Op: "move", Src: "nope.txt", Dst: "x.txt"}))
	for i := 0; i < maxAttempts; i++ {
		f.journal.failed(f.journal.ops[0].ID, errString("access denied"))
	}
	if f.journal.len() != 0 {
		t.Fatal("op not parked after max attempts")
	}
	var parked []jop
	if !readJSON(filepath.Join(f.store.path, parkedFile), &parked) || len(parked) != 1 {
		t.Fatal("parked op not saved")
	}
}

func TestConflictName(t *testing.T) {
	for in, want := range map[string]string{
		"a/b/report.docx": "a/b/report (conflitto Mac ",
		"x":               "x (conflitto Mac ",
		".hidden":         ".hidden (conflitto Mac ",
	} {
		got := conflictName(in, 1)
		if !strings.HasPrefix(got, want) {
			t.Errorf("conflictName(%q) = %q", in, got)
		}
	}
	if got := conflictName("a/report.docx", 2); !strings.HasSuffix(got, " 2).docx") {
		t.Errorf("extension lost: %q", got)
	}
}

func TestIsAuthError(t *testing.T) {
	if !isAuthError(errString("couldn't connect SMB: response error: The attempted logon is invalid. This is either due to a bad username or authentication information.")) {
		t.Error("logon failure not detected")
	}
	if isAuthError(errString("dial tcp: connection refused")) {
		t.Error("network error taken as auth error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// must match remoteHash in src/app (used to migrate old installs)
func TestShortHashStable(t *testing.T) {
	if got := shortHash("remoto:Tecnico"); got != "5419a6e25ea787e1" {
		t.Fatalf("shortHash = %s", got)
	}
}

func TestRcStatus(t *testing.T) {
	ctx := context.Background()
	f, _, setOnline := newTestFs(t)
	get := func() map[string]any {
		out, err := rcStatus(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	st := get()
	if st["online"] != true || st["journal"] != 0 || st["problem"] != "" || st["remotes"].(int) < 1 {
		t.Fatalf("online status = %v", st)
	}
	setOnline(false)
	must(t, f.Mkdir(ctx, "nuova"))
	st = get()
	if st["online"] != false || st["journal"] != 1 {
		t.Fatalf("offline status = %v", st)
	}
	f.stateMu.Lock()
	f.authFailed = true
	f.stateMu.Unlock()
	if p := get()["problem"].(string); !strings.Contains(p, "password") {
		t.Fatalf("problem = %q", p)
	}
	f.stateMu.Lock()
	f.authFailed = false
	f.stateMu.Unlock()
}

func TestWatchNotifiesRemoteChanges(t *testing.T) {
	ctx := context.Background()
	f, base, setOnline := newTestFs(t)
	must(t, os.MkdirAll(filepath.Join(base, "a"), 0o755))
	for _, d := range []string{"", "a"} {
		if _, err := f.List(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	notify := func(p string, _ fs.EntryType) { got = append(got, p) }
	if n := f.pollWatched(ctx, notify, 1); n != 0 {
		t.Fatalf("nothing changed, notified %v", got)
	}
	// a colleague adds a file in "a"
	must(t, os.WriteFile(filepath.Join(base, "a/new.txt"), []byte("x"), 0o644))
	// "a" changed; the root too, since the folder "a" has a new date
	f.pollWatched(ctx, notify, 1)
	sort.Strings(got)
	if j := strings.Join(got, "|"); j != "a" && j != "|a" {
		t.Fatalf("new file: notified %q", j)
	}
	if saved, _ := f.store.load("a"); len(saved) != 1 || saved[0].Name != "new.txt" {
		t.Fatalf("listing not saved for offline use: %+v", saved)
	}
	got = nil
	if n := f.pollWatched(ctx, notify, 1); n != 0 {
		t.Fatalf("no new change, notified %v", got)
	}
	// folder removed on the remote: forgotten, parent changed too
	must(t, os.RemoveAll(filepath.Join(base, "a")))
	got = nil
	f.pollWatched(ctx, notify, 1)
	sort.Strings(got)
	if j := strings.Join(got, "|"); j != "|a" {
		t.Fatalf("removed folder: notified %q, want root and a", j)
	}
	if f.store.has("a") {
		t.Fatal("removed folder still saved")
	}
	// offline: no requests
	got = nil
	setOnline(false)
	must(t, os.MkdirAll(filepath.Join(base, "b"), 0o755))
	if n := f.pollWatched(ctx, notify, 1); n != 0 {
		t.Fatalf("offline: notified %v", got)
	}
	// folders not opened for a while are no longer re-read
	if l := f.watchList(time.Now().Add(2*time.Hour), 10); len(l) != 0 {
		t.Fatalf("old folders still watched: %v", l)
	}
}

func TestChangeNotifyTicks(t *testing.T) {
	ctx := context.Background()
	f, base, _ := newTestFs(t)
	if f.Features().ChangeNotify == nil {
		t.Fatal("ChangeNotify not offered to the VFS")
	}
	if _, err := f.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 10)
	intervals := make(chan time.Duration)
	f.Features().ChangeNotify(ctx, func(p string, _ fs.EntryType) { seen <- p }, intervals)
	intervals <- 50 * time.Millisecond
	must(t, os.MkdirAll(filepath.Join(base, "nuova"), 0o755))
	select {
	case p := <-seen:
		if p != "" {
			t.Fatalf("notified %q, want root", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}
	close(intervals)
}

func TestPins(t *testing.T) {
	ctx := context.Background()
	f, base, _ := newTestFs(t)
	must(t, os.MkdirAll(filepath.Join(base, "Commesse/C1"), 0o755))
	must(t, os.WriteFile(filepath.Join(base, "Commesse/C1/a.dwg"), []byte("x"), 0o644))

	must(t, f.SetPinned(ctx, "/Commesse/C1/", true))
	if !f.IsPinned("Commesse/C1/a.dwg") || !f.IsPinned("Commesse/C1") || f.IsPinned("Commesse") {
		t.Fatalf("pins = %v", f.PinnedPaths())
	}
	// a file inside a kept folder cannot be released alone
	if err := f.SetPinned(ctx, "Commesse/C1/a.dwg", false); err == nil || !strings.Contains(err.Error(), "Commesse/C1") {
		t.Fatalf("release inside kept folder: %v", err)
	}
	// kept files are never removed from the cache
	if f.CacheEvictionAllowed(ctx, "Commesse/C1/a.dwg") {
		t.Fatal("kept file removed from the cache")
	}
	// keeping the parent replaces the child
	must(t, f.SetPinned(ctx, "Commesse", true))
	if got := strings.Join(f.PinnedPaths(), ","); got != "Commesse" {
		t.Fatalf("pins = %q", got)
	}
	// saved on disk
	if p := newPinSet(f.store.path); strings.Join(p.list, ",") != "Commesse" {
		t.Fatalf("saved pins = %v", p.list)
	}
	// folder renamed and deleted from the Mac
	must(t, f.DirMove(ctx, f, "Commesse", "Lavori"))
	if got := strings.Join(f.PinnedPaths(), ","); got != "Lavori" {
		t.Fatalf("after rename pins = %q", got)
	}
	must(t, f.SetPinned(ctx, "Lavori", false))
	if len(f.PinnedPaths()) != 0 || f.IsPinned("Lavori/C1/a.dwg") {
		t.Fatalf("after release pins = %v", f.PinnedPaths())
	}
	must(t, f.SetPinned(ctx, "Lavori/C1", true))
	must(t, os.Remove(filepath.Join(base, "Lavori/C1/a.dwg")))
	must(t, f.Rmdir(ctx, "Lavori/C1"))
	if len(f.PinnedPaths()) != 0 {
		t.Fatalf("deleted folder still kept: %v", f.PinnedPaths())
	}
}
