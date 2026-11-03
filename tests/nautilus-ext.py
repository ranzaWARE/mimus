#!/usr/bin/env python3
"""Prova dell'estensione del file manager (src/app/linuxfiles/mimus.py)
senza Nautilus: finti moduli gi.repository e un finto servizio rc.

  python3 tests/nautilus-ext.py
"""
import http.server
import importlib.util
import json
import os
import sys
import tempfile
import threading
import time
import types

sys.dont_write_bytecode = True

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FAILS = 0


def ok(name, cond):
    global FAILS
    print(("  OK   " if cond else "  FAIL ") + name)
    if not cond:
        FAILS += 1


# --- finto servizio rc ---
calls = []
STATES = {"": [{"name": "Doc", "state": "folder", "pinned": False},
               {"name": "a.txt", "state": "local", "pinned": False},
               {"name": "b.txt", "state": "remote", "pinned": False},
               {"name": "c.txt", "state": "upload", "pinned": False},
               {"name": "d.bin", "state": "downloading", "pinned": False, "progress": 55},
               {"name": "e.txt", "state": "local", "pinned": True},
               {"name": ".nascosto", "state": "local", "pinned": False}]}


class RC(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])) or b"{}")
        calls.append((self.path, body))
        out = {}
        if self.path == "/vfs/mimus-states":
            out = {"entries": STATES.get(body.get("dir", ""), [])}
        data = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *a):
        pass


srv = http.server.ThreadingHTTPServer(("127.0.0.1", 5572), RC)
threading.Thread(target=srv.serve_forever, daemon=True).start()

# --- finti moduli gi ---
idle = []
gi = types.ModuleType("gi")
gi.require_version = lambda name, ver: None if name == "Nautilus" and ver == "4.0" else (_ for _ in ()).throw(ValueError(name))
repo = types.ModuleType("gi.repository")
naut = types.ModuleType("gi.repository.Nautilus")


class MenuItem:
    def __init__(self, name, label):
        self.name, self.label, self.cb = name, label, None

    def connect(self, sig, cb):
        self.cb = cb


naut.InfoProvider = type("InfoProvider", (), {})
naut.MenuProvider = type("MenuProvider", (), {})
naut.MenuItem = MenuItem
glib = types.ModuleType("gi.repository.GLib")
glib.idle_add = lambda f, *a: idle.append((f, a))
gobject = types.ModuleType("gi.repository.GObject")
gobject.GObject = type("GObject", (), {})
repo.Nautilus, repo.GLib, repo.GObject = naut, glib, gobject
sys.modules.update({"gi": gi, "gi.repository": repo, "gi.repository.Nautilus": naut,
                    "gi.repository.GLib": glib, "gi.repository.GObject": gobject})

tmp = tempfile.mkdtemp()
mount = os.path.join(tmp, "Lavoro")
os.makedirs(os.path.join(tmp, "cfg", "mimus"))
os.makedirs(mount)
json.dump({"mount_path": mount}, open(os.path.join(tmp, "cfg", "mimus", "settings.json"), "w"))
os.environ["XDG_CONFIG_HOME"] = os.path.join(tmp, "cfg")

spec = importlib.util.spec_from_file_location("mimus_ext", os.path.join(ROOT, "src", "app", "linuxfiles", "mimus.py"))
ext = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ext)


class FileInfo:
    def __init__(self, path):
        self.path, self.emblems, self.invalidated = path, [], 0

    def get_uri(self):
        return "file://" + self.path.replace(" ", "%20")

    def add_emblem(self, e):
        self.emblems.append(e)

    def invalidate_extension_info(self):
        self.invalidated += 1


e = ext.MimusExtension()
files = {n: FileInfo(os.path.join(mount, n)) for n in ["Doc", "a.txt", "b.txt", "c.txt", "d.bin", "e.txt", ".nascosto"]}
for fi in files.values():
    e.update_file_info(fi)
outside = FileInfo(os.path.join(tmp, "fuori.txt"))
e.update_file_info(outside)

print("== pallini")
ok("su questo computer: verde", files["a.txt"].emblems == ["mimus-local"])
ok("solo sul server: grigio", files["b.txt"].emblems == ["mimus-remote"])
ok("da caricare: arancione", files["c.txt"].emblems == ["mimus-upload"])
ok("in scaricamento al 55%: anello a 4/8", files["d.bin"].emblems == ["mimus-dl4"])
ok("sempre su questo computer: spunta", files["e.txt"].emblems == ["mimus-pinned"])
ok("cartella non mantenuta: niente", files["Doc"].emblems == [])
ok("file nascosti: niente", files[".nascosto"].emblems == [])
ok("fuori dalla cartella collegata: niente", outside.emblems == [])
ok("una sola richiesta per cartella", sum(1 for c in calls if c[0] == "/vfs/mimus-states") == 1)
icons = os.path.join(ROOT, "src", "app", "linuxfiles", "emblems")
ok("icone per tutti i pallini", all(os.path.exists(os.path.join(icons, "emblem-" + fi.emblems[0] + ".svg"))
                                     for fi in files.values() if fi.emblems))

print("== cambi di stato")
STATES[""][3] = {"name": "c.txt", "state": "local", "pinned": False}
time.sleep(2.6)
for f, a in idle:
    f(*a)
ok("caricato: il file manager viene avvisato", files["c.txt"].invalidated == 1 and files["a.txt"].invalidated == 0)

print("== menu del tasto destro")
items = e.get_file_items([files["a.txt"], files["b.txt"]])
labels = [i.label for i in items]
ok("scarica, mantieni, togli", labels == ["Scarica ora su questo computer", "Mantieni sempre su questo computer",
                                          "Togli da questo computer (resta sul server)"])
items[1].cb(items[1])
time.sleep(0.5)
ok("«Mantieni» chiede al servizio", ("/vfs/mimus-pin", {"paths": ["a.txt", "b.txt"], "on": True}) in calls)
items = e.get_file_items(None, [files["e.txt"]])  # Nautilus 3, Nemo, Caja: (window, files)
ok("già mantenuto: «Non mantenere più»", items[1].label == "Non mantenere più sempre su questo computer")
ok("fuori dalla cartella: nessun menu", e.get_file_items([outside]) == [])

srv.shutdown()
print()
print("TUTTI I TEST OK" if FAILS == 0 else "%d TEST FALLITI" % FAILS)
sys.exit(FAILS)
