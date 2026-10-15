package offline

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/rclone/rclone/fs/rc"
)

// Status for the menu bar app: rc call "mimus/status".

var active sync.Map // *Fs of this process

func init() {
	rc.Add(rc.Call{
		Path:  "mimus/status",
		Fn:    rcStatus,
		Title: "Stato dei collegamenti MIMUS",
		Help: `Restituisce lo stato dei remote "offline" attivi in questo processo:

- remotes: quanti
- online: il server remoto risponde (e il login è accettato)
- authFailed: login rifiutato, tentativi sospesi
- journal: modifiche fatte offline in attesa di essere applicate
- parked: modifiche messe da parte perché il server le ha rifiutate
- replaying: sta applicando le modifiche in coda
- walking: sta rileggendo tutta la struttura delle cartelle
- diskLow: spazio libero sotto la soglia
- problem: descrizione breve del problema più importante ("" se nessuno)
`,
	})
}

type status struct {
	Online, AuthFailed, Replaying, Walking, DiskLow bool
	Journal, Parked                                 int
}

func (f *Fs) status() status {
	f.stateMu.Lock()
	authFailed := f.authFailed
	f.stateMu.Unlock()
	return status{
		Online:     f.isOnline(),
		AuthFailed: authFailed,
		Replaying:  f.replaying.Load(),
		Walking:    f.walking.Load(),
		DiskLow:    f.diskLow.Load(),
		Journal:    f.journal.len(),
		Parked:     f.journal.parkedCount(),
	}
}

func rcStatus(ctx context.Context, in rc.Params) (rc.Params, error) {
	var total status
	n := 0
	allOnline := true
	active.Range(func(k, _ any) bool {
		st := k.(*Fs).status()
		n++
		allOnline = allOnline && st.Online
		total.AuthFailed = total.AuthFailed || st.AuthFailed
		total.Replaying = total.Replaying || st.Replaying
		total.Walking = total.Walking || st.Walking
		total.DiskLow = total.DiskLow || st.DiskLow
		total.Journal += st.Journal
		total.Parked += st.Parked
		return true
	})
	problem := ""
	switch {
	case total.AuthFailed:
		problem = "Il server remoto non accetta la password"
	case total.Parked > 0:
		problem = "Alcune modifiche sono state rifiutate dal server remoto"
	case total.DiskLow:
		problem = "Spazio quasi esaurito sul disco"
	}
	return rc.Params{
		"remotes":    n,
		"online":     n > 0 && allOnline,
		"authFailed": total.AuthFailed,
		"replaying":  total.Replaying,
		"walking":    total.Walking,
		"diskLow":    total.DiskLow,
		"journal":    total.Journal,
		"parked":     total.Parked,
		"problem":    problem,
	}, nil
}

// parkedCount counts the changes set aside in journal-failed.json
func (j *journal) parkedCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	var ops []jop
	readJSON(filepath.Join(j.dir, parkedFile), &ops)
	return len(ops)
}
