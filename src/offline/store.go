package offline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
)

// Persistent copy of the directory listings: one JSON file per folder.

type savedEntry struct {
	Name    string    `json:"n"`
	IsDir   bool      `json:"d,omitempty"`
	Size    int64     `json:"s,omitempty"`
	ModTime time.Time `json:"m"`
}

type savedDir struct {
	Dir     string       `json:"dir"`
	Saved   time.Time    `json:"saved"`
	Entries []savedEntry `json:"entries"`
}

type walkMeta struct {
	Last    time.Time     `json:"last"`
	Folders int           `json:"folders"`
	Took    time.Duration `json:"took"`
}

type store struct {
	mu   sync.Mutex
	path string
}

const (
	journalFile = "journal.json"
	parkedFile  = "journal-failed.json"
	walkFile    = "walk.json"
)

func newStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("offline: can't create %q: %w", dir, err)
	}
	return &store{path: dir}, nil
}

func (s *store) file(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(s.path, hex.EncodeToString(sum[:])+".json")
}

func writeJSON(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(name, data)
}

// writeFileAtomic writes via a temp file + rename, synced to disk so a
// crash or power loss cannot leave a half written file
func writeFileAtomic(name string, data []byte) (err error) {
	defer func() {
		if err != nil {
			fs.Errorf(nil, "offline: salvataggio %q fallito: %v", name, err)
		}
	}()
	tmp := name + ".tmp"
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = fh.Write(data); err == nil {
		err = fh.Sync()
	}
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, name); err != nil {
		return err
	}
	if d, derr := os.Open(filepath.Dir(name)); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func readJSON(name string, v any) bool {
	data, err := os.ReadFile(name)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

func (s *store) loadLocked(dir string) ([]savedEntry, bool) {
	var d savedDir
	if !readJSON(s.file(dir), &d) || d.Dir != dir {
		return nil, false
	}
	return d.Entries, true
}

// saveLocked writes the listing, skipping the write if nothing changed
func (s *store) saveLocked(dir string, entries []savedEntry) {
	if entries == nil {
		entries = []savedEntry{}
	}
	if old, ok := s.loadLocked(dir); ok && sameEntries(old, entries) {
		return
	}
	_ = writeJSON(s.file(dir), savedDir{Dir: dir, Saved: time.Now(), Entries: entries})
}

func sameEntries(a, b []savedEntry) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func (s *store) load(dir string) ([]savedEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(dir)
}

func (s *store) has(dir string) bool {
	_, ok := s.load(dir)
	return ok
}

func toSaved(ctx context.Context, entries fs.DirEntries) []savedEntry {
	out := make([]savedEntry, 0, len(entries))
	for _, e := range entries {
		_, leaf := parentLeaf(e.Remote())
		switch x := e.(type) {
		case fs.Directory:
			out = append(out, savedEntry{Name: leaf, IsDir: true, ModTime: x.ModTime(ctx)})
		case fs.Object:
			out = append(out, savedEntry{Name: leaf, Size: x.Size(), ModTime: x.ModTime(ctx)})
		}
	}
	return out
}

func (s *store) save(ctx context.Context, dir string, entries fs.DirEntries) {
	out := toSaved(ctx, entries)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveLocked(dir, out)
}

// upsert adds or replaces one entry in the saved listing of its parent
func (s *store) upsert(remote string, e savedEntry) {
	dir, leaf := parentLeaf(remote)
	e.Name = leaf
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.loadLocked(dir)
	if !ok {
		return // parent never listed: the next listing will pick it up
	}
	out := make([]savedEntry, 0, len(old)+1)
	for _, x := range old {
		if x.Name != leaf {
			out = append(out, x)
		}
	}
	s.saveLocked(dir, append(out, e))
}

func (s *store) upsertObject(ctx context.Context, o fs.Object) {
	if o == nil {
		return
	}
	s.upsert(o.Remote(), savedEntry{Size: o.Size(), ModTime: o.ModTime(ctx)})
}

func (s *store) upsertDir(dir string) {
	if dir == "" {
		return
	}
	s.upsert(dir, savedEntry{IsDir: true, ModTime: time.Now()})
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.loadLocked(dir); !ok {
		s.saveLocked(dir, []savedEntry{})
	}
}

// entry returns the saved entry for remote
func (s *store) entry(remote string) (savedEntry, bool) {
	dir, leaf := parentLeaf(remote)
	saved, ok := s.load(dir)
	if !ok {
		return savedEntry{}, false
	}
	for _, e := range saved {
		if e.Name == leaf {
			return e, true
		}
	}
	return savedEntry{}, false
}

func (s *store) removeEntry(remote string) {
	dir, leaf := parentLeaf(remote)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.loadLocked(dir)
	if !ok {
		return
	}
	out := make([]savedEntry, 0, len(old))
	for _, x := range old {
		if x.Name != leaf {
			out = append(out, x)
		}
	}
	s.saveLocked(dir, out)
}

func (s *store) drop(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.file(dir))
}

// dropTree forgets dir and every saved subfolder
func (s *store) dropTree(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forEachLocked(func(name string, d savedDir) {
		if isUnder(d.Dir, dir) {
			_ = os.Remove(name)
		}
	})
}

// renameTree moves the saved listings of src and its subfolders to dst
func (s *store) renameTree(src, dst string) {
	s.mu.Lock()
	type moved struct {
		old string
		d   savedDir
	}
	var list []moved
	s.forEachLocked(func(name string, d savedDir) {
		if isUnder(d.Dir, src) {
			list = append(list, moved{name, d})
		}
	})
	for _, m := range list {
		_ = os.Remove(m.old)
		m.d.Dir = dst + strings.TrimPrefix(m.d.Dir, src)
		m.d.Saved = time.Now()
		_ = writeJSON(s.file(m.d.Dir), m.d)
	}
	s.mu.Unlock()
	if e, ok := s.entry(src); ok {
		s.removeEntry(src)
		s.upsert(dst, e)
	} else {
		s.upsertDir(dst)
	}
}

// prune forgets folders not seen during a complete walk started at start,
// except those saved after the start (created meanwhile) or keep(dir)
func (s *store) prune(visited map[string]struct{}, start time.Time, keep func(dir string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forEachLocked(func(name string, d savedDir) {
		if _, ok := visited[d.Dir]; !ok && d.Saved.Before(start) && !keep(d.Dir) {
			_ = os.Remove(name)
		}
	})
}

func (s *store) forEachLocked(fn func(name string, d savedDir)) {
	items, err := os.ReadDir(s.path)
	if err != nil {
		return
	}
	for _, it := range items {
		n := it.Name()
		if it.IsDir() || !strings.HasSuffix(n, ".json") || n == journalFile || n == walkFile || n == parkedFile {
			continue
		}
		name := filepath.Join(s.path, n)
		var d savedDir
		if readJSON(name, &d) {
			fn(name, d)
		}
	}
}

func (s *store) walkMeta() (m walkMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	readJSON(filepath.Join(s.path, walkFile), &m)
	return m
}

func (s *store) setWalkMeta(m walkMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = writeJSON(filepath.Join(s.path, walkFile), m)
}

// ---------- helpers ----------

func parentLeaf(remote string) (string, string) {
	dir, leaf := path.Split(remote)
	return strings.TrimSuffix(dir, "/"), leaf
}

// isUnder reports whether p is dir or inside dir
func isUnder(p, dir string) bool {
	if dir == "" {
		return true
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}
