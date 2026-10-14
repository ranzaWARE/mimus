package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Versions up to 1.3.1 named the rclone sections "nas" and "nasoffline".
// migrateOldNames renames them to "remoto" and "mimus" and moves the cache
// (downloaded files, folder listings, offline queue) to the new names.
// It refuses while changes are still waiting for the remote: the old
// service keeps running untouched and the user is told to retry later.

const (
	oldBase, oldOffline = "nas", "nasoffline"
	newBase, newOffline = "remoto", "mimus"
)

func migrateOldNames(p paths) error {
	out, err := exec.Command(p.rclone, "config", "dump").Output()
	if err != nil {
		return nil // no config yet
	}
	var cfg map[string]map[string]string
	if json.Unmarshal(out, &cfg) != nil {
		return nil
	}
	oldOff, hasOld := cfg[oldOffline]
	if !hasOld {
		return nil
	}
	if _, hasNew := cfg[newOffline]; hasNew {
		return nil
	}
	if n := pendingChanges(p); n > 0 {
		return fmt.Errorf("Prima di aggiornare MIMUS ci sono %d modifiche da caricare sul server remoto.\nCollegati al server remoto, aspetta che finiscano e riapri MIMUS: intanto il collegamento continua a funzionare.", n)
	}

	stopAgent(p)

	// new sections with the same values (password already obscured)
	if base, ok := cfg[oldBase]; ok {
		if err := createSection(p, newBase, base, nil); err != nil {
			return err
		}
	}
	oldRemote := oldOff["remote"]
	newRemote := oldRemote
	if strings.HasPrefix(oldRemote, oldBase+":") {
		newRemote = newBase + ":" + strings.TrimPrefix(oldRemote, oldBase+":")
	}
	if err := createSection(p, newOffline, oldOff, map[string]string{"remote": newRemote}); err != nil {
		return err
	}

	// cache: downloaded files and their metadata
	for _, d := range []string{"vfs", "vfsMeta"} {
		moveIfExists(filepath.Join(p.cache, d, oldOffline), filepath.Join(p.cache, d, newOffline))
	}
	// folder listings and offline queue (folder named after a hash of the remote)
	oldDir := filepath.Join(p.cache, "offline", oldOffline)
	newDir := filepath.Join(p.cache, "offline", newOffline, remoteHash(newRemote))
	src := filepath.Join(oldDir, remoteHash(oldRemote))
	if _, err := os.Stat(src); err != nil {
		if subs, _ := os.ReadDir(oldDir); len(subs) == 1 && subs[0].IsDir() {
			src = filepath.Join(oldDir, subs[0].Name())
		}
	}
	moveIfExists(src, newDir)
	_ = os.Remove(oldDir) // only if empty

	_, _ = run(p.rclone, "config", "delete", oldOffline)
	_, _ = run(p.rclone, "config", "delete", oldBase)
	return nil
}

func createSection(p paths, name string, values, override map[string]string) error {
	typ := values["type"]
	args := []string{"config", "create", name, typ}
	keys := make([]string, 0, len(values))
	for k := range values {
		if k != "type" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := values[k]
		if o, ok := override[k]; ok {
			v = o
		}
		args = append(args, k+"="+v)
	}
	args = append(args, "--no-obscure")
	if out, err := run(p.rclone, args...); err != nil {
		return fmt.Errorf("aggiornamento della configurazione non riuscito: %s", out)
	}
	return nil
}

func moveIfExists(from, to string) {
	if _, err := os.Stat(from); err != nil {
		return
	}
	if _, err := os.Stat(to); err == nil {
		return // never overwrite
	}
	_ = os.MkdirAll(filepath.Dir(to), 0o700)
	_ = os.Rename(from, to)
}

// remoteHash is the folder name the offline backend uses for a remote
// (first 8 bytes of sha256, as in src/offline shortHash)
func remoteHash(remote string) string {
	sum := sha256.Sum256([]byte(remote))
	return hex.EncodeToString(sum[:8])
}
