# MIMUS: pallini e menu nel file manager (Nautilus, Nemo, Caja).
#
# Installata dall'app MIMUS in ~/.local/share/<file manager>-python/extensions
# (serve il pacchetto python3-nautilus, python3-nemo o python-caja).
# Lo stato dei file arriva dal servizio di MIMUS (rc su 127.0.0.1:5572),
# come per l'estensione del Finder sul Mac:
#   pallino grigio   solo sul server remoto
#   pallino verde    su questo computer (con la spunta: sempre)
#   arancione        da caricare sul server remoto
#   anello blu       in scaricamento (si riempie)
# Tasto destro: «Scarica ora», «Mantieni sempre su questo computer»,
# «Togli da questo computer».

import json
import os
import threading
import time
import urllib.parse
import urllib.request

import gi

FM = None
for _name, _ver in (("Nautilus", "4.0"), ("Nautilus", "3.0"), ("Nemo", "3.0"), ("Caja", "2.0")):
    try:
        gi.require_version(_name, _ver)
        FM = __import__("gi.repository." + _name, fromlist=[_name])
        break
    except (ValueError, ImportError):
        continue
from gi.repository import GLib, GObject  # noqa: E402

RC = "http://127.0.0.1:5572/"
STEPS = 8
_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def rc(path, params, timeout=1.5):
    req = urllib.request.Request(RC + path, data=json.dumps(params).encode(),
                                 headers={"Content-Type": "application/json"})
    try:
        with _opener.open(req, timeout=timeout) as r:
            return json.load(r)
    except Exception:
        return None


def mount_path():
    cfg = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
    try:
        with open(os.path.join(cfg, "mimus", "settings.json")) as f:
            p = json.load(f).get("mount_path") or ""
    except Exception:
        p = ""
    return os.path.realpath(p or os.path.expanduser("~/MIMUS"))


def badge(entry):
    st, pin = entry.get("state"), entry.get("pinned")
    if st == "folder":
        return "pinned" if pin else ""
    if st == "upload":
        return "upload"
    if st == "local":
        return "pinned" if pin else "local"
    if st == "downloading":
        pct = int(entry.get("progress") or 0)
        return "dl%d" % min(STEPS - 1, max(0, pct * STEPS // 100))
    return "remote"


class States:
    """States of the folders shown, refreshed in the background."""

    def __init__(self):
        self.lock = threading.Lock()
        self.dirs = {}      # rel dir -> {name: badge}
        self.seen = {}      # rel dir -> last time shown
        self.files = {}     # path -> FileInfo shown
        self.mount = mount_path()
        self.up = False
        threading.Thread(target=self.loop, daemon=True).start()

    def rel(self, path):
        if path == self.mount or not path.startswith(self.mount + os.sep):
            return None
        return os.path.relpath(path, self.mount)

    def fetch(self, d):
        out = rc("vfs/mimus-states", {"dir": "" if d == "." else d})
        if not out:
            return None
        return {e["name"]: badge(e) for e in out.get("entries", []) if not e["name"].startswith(".")}

    def get(self, rel):
        d, name = os.path.split(rel)
        d = d or "."
        with self.lock:
            self.seen[d] = time.time()
            states = self.dirs.get(d)
        if states is None:
            states = self.fetch(d) or {}
            with self.lock:
                self.dirs[d] = states
        return states.get(name, "")

    def loop(self):
        while True:
            time.sleep(2)
            self.mount = mount_path()
            changed = []
            now = time.time()
            with self.lock:
                dirs = [d for d, t in self.seen.items() if now - t < 600]
                for d in [d for d in self.seen if d not in dirs]:
                    del self.seen[d]
                    self.dirs.pop(d, None)
                keep = {os.path.normpath(os.path.join(self.mount, "" if d == "." else d)) for d in dirs}
                for path in [p for p in self.files if os.path.dirname(p) not in keep]:
                    del self.files[path]
            for d in dirs:
                new = self.fetch(d)
                if new is None:
                    continue
                with self.lock:
                    old = self.dirs.get(d, {})
                    self.dirs[d] = new
                for name in set(old) | set(new):
                    if old.get(name) != new.get(name):
                        changed.append(os.path.join(self.mount, "" if d == "." else d, name))
            if changed:
                GLib.idle_add(self.invalidate, changed)

    def invalidate(self, changed):
        for path in changed:
            fi = self.files.get(os.path.normpath(path))
            if fi is not None:
                fi.invalidate_extension_info()
        return False


def file_path(fi):
    uri = fi.get_uri()
    if not uri.startswith("file://"):
        return None
    return os.path.normpath(urllib.parse.unquote(urllib.parse.urlparse(uri).path))


if FM is not None:
    _states = States()

    class MimusExtension(GObject.GObject, FM.InfoProvider, FM.MenuProvider):
        def update_file_info(self, fi):
            path = file_path(fi)
            if not path:
                return
            rel = _states.rel(path)
            if rel is None:
                return
            _states.files[path] = fi
            b = _states.get(rel)
            if b:
                fi.add_emblem("mimus-" + b)

        # Nautilus 4: (files); Nautilus 3, Nemo, Caja: (window, files)
        def get_file_items(self, *args):
            files = args[-1]
            rels = []
            for fi in files:
                path = file_path(fi)
                rel = _states.rel(path) if path else None
                if rel is None:
                    return []
                rels.append(rel)
            if not rels:
                return []
            pinned = all(_states.get(r) == "pinned" for r in rels)
            items = [
                self.item("download", "Scarica ora su questo computer", "vfs/mimus-download", {"paths": rels, "_async": True}),
                self.item("unpin", "Non mantenere più sempre su questo computer", "vfs/mimus-pin", {"paths": rels, "on": False})
                if pinned else
                self.item("pin", "Mantieni sempre su questo computer", "vfs/mimus-pin", {"paths": rels, "on": True}),
                self.item("evict", "Togli da questo computer (resta sul server)", "vfs/mimus-evict", {"paths": rels, "_async": True}),
            ]
            return items

        def get_background_items(self, *args):
            return []

        def item(self, name, label, call, params):
            mi = FM.MenuItem(name="MimusExtension::" + name, label=label)
            mi.connect("activate", lambda *_: threading.Thread(target=rc, args=(call, params, 30), daemon=True).start())
            return mi
