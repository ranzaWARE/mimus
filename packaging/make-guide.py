#!/usr/bin/env python3
"""Genera packaging/Guida.html (guida utente con illustrazioni, file unico).
Uso: python3 packaging/make-guide.py [versione]"""
import base64, html, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
VERSION = sys.argv[1] if len(sys.argv) > 1 else open(os.path.join(ROOT, "VERSION")).read().strip()
ICON = "data:image/png;base64," + base64.b64encode(open(os.path.join(HERE, "AppIcon.png"), "rb").read()).decode()

E = html.escape

# icone della barra dei menu (packaging/menubar, @2x: 48x36)
def _mb(name):
    return "data:image/png;base64," + base64.b64encode(open(os.path.join(HERE, "menubar", name + "@2x.png"), "rb").read()).decode()
MB = {n: _mb(n) for n in ["run-0", "run-1", "run-2", "run-3", "sleep-0", "sleep-1", "sleep-2", "alert"]}

def mb_icon(frames, period, label):
    """icona come nella barra: colore del testo (maschera), animata con i fotogrammi"""
    n = len(frames)
    cls = "mb a" if n > 1 else "mb"
    items = "".join(f'<i style="--m:url({MB[f]});animation-duration:{period}s;animation-delay:-{(n - i) * period / n:.3f}s"></i>'
                    for i, f in enumerate(frames))
    return f'<span class="{cls}" role="img" aria-label="{E(label)}">{items}</span>'

# ---------- illustrazioni (SVG disegnati, stile macOS) ----------

def text_lines(x, y, lines, size=13, weight=400, fill="var(--ink)", lh=19):
    out = []
    for i, l in enumerate(lines):
        out.append(f'<text x="{x}" y="{y + i*lh}" font-size="{size}" font-weight="{weight}" fill="{fill}">{E(l)}</text>')
    return "".join(out)

def button(x, y, w, label, default=False, hl=False):
    fill = "var(--accent)" if default else "var(--btn)"
    ink = "#fff" if default else "var(--ink)"
    ring = f'<rect x="{x-4}" y="{y-4}" width="{w+8}" height="30" rx="9" fill="none" stroke="#ff9f0a" stroke-width="3"/>' if hl else ""
    return (f'{ring}<rect x="{x}" y="{y}" width="{w}" height="22" rx="6" fill="{fill}" stroke="var(--line)" stroke-width="{0 if default else 1}"/>'
            f'<text x="{x + w/2}" y="{y + 15.5}" font-size="12.5" text-anchor="middle" fill="{ink}" font-weight="{500 if default else 400}">{E(label)}</text>')

def window(w, h, title, body):
    return (f'<svg viewBox="0 0 {w} {h}" role="img" aria-label="{E(title)}" class="shot">'
            f'<rect x="1" y="1" width="{w-2}" height="{h-2}" rx="12" fill="var(--win)" stroke="var(--line)"/>'
            f'<circle cx="20" cy="18" r="6" fill="#ff5f57"/><circle cx="40" cy="18" r="6" fill="#febc2e"/><circle cx="60" cy="18" r="6" fill="#28c840"/>'
            f'<text x="{w/2}" y="22" font-size="12.5" font-weight="600" text-anchor="middle" fill="var(--muted)">{E(title)}</text>'
            f'{body}</svg>')

def dialog(lines, buttons, field=None, hidden=False, hl=None, w=460, default=None):
    """finestra di dialogo dell'app: icona, testo, campo opzionale, pulsanti nell'ordine
    in cui li mostra macOS (default = pulsante evidenziato, se omesso l'ultimo)"""
    default = default or buttons[-1]
    h = 70 + 19 * len(lines) + (40 if field is not None else 0) + 50
    body = '<use href="#appicon" x="22" y="44" width="52" height="52"/>'
    body += text_lines(92, 62, lines)
    y = 62 + 19 * len(lines)
    if field is not None:
        shown = "•" * len(field) if hidden else field
        body += f'<rect x="92" y="{y}" width="{w-114}" height="26" rx="5" fill="var(--field)" stroke="var(--accent)" stroke-width="1.5"/>'
        body += f'<text x="100" y="{y+17.5}" font-size="13" fill="var(--ink)">{E(shown)}</text>'
        y += 40
    widths = [max(72, 9 * len(b) + 22) for b in buttons]
    x = w - 20 - sum(widths) - 10 * (len(buttons) - 1)
    for b, bw in zip(buttons, widths):
        body += button(x, y + 8, bw, b, default=(b == default), hl=(b == hl))
        x += bw + 10
    return window(w, h, "MIMUS", body)

def choose_list(status, items, sel=0, w=460, ok_label="Esegui", cancel_label="Esci"):
    h = 60 + 19 * len(status) + 22 * len(items) + 70
    body = text_lines(22, 54, status, size=12.5)
    y = 54 + 19 * len(status)
    body += f'<rect x="22" y="{y}" width="{w-44}" height="{22*len(items)+8}" rx="6" fill="var(--field)" stroke="var(--line)"/>'
    for i, it in enumerate(items):
        yy = y + 4 + 22 * i
        if i == sel:
            body += f'<rect x="26" y="{yy}" width="{w-52}" height="22" rx="4" fill="var(--accent)"/>'
        body += f'<text x="36" y="{yy+15.5}" font-size="13" fill="{"#fff" if i == sel else "var(--ink)"}">{E(it)}</text>'
    y += 22 * len(items) + 20
    body += button(w - 20 - 84, y, 84, ok_label, default=True) + button(w - 20 - 84 - 10 - 72, y, 72, cancel_label)
    return window(w, h, "MIMUS", body)

def folder(x, y, s=1.0, label=None, color="#5ac8fa"):
    g = (f'<g transform="translate({x},{y}) scale({s})">'
         f'<path d="M0 12 a6 6 0 0 1 6 -6 h26 l8 8 h42 a6 6 0 0 1 6 6 v46 a6 6 0 0 1 -6 6 h-76 a6 6 0 0 1 -6 -6z" fill="{color}"/>'
         f'<rect x="0" y="22" width="88" height="50" rx="6" fill="{color}" opacity=".85"/>')
    if label:
        g += f'<text x="44" y="94" font-size="13" text-anchor="middle" fill="var(--ink)">{E(label)}</text>'
    return g + "</g>"

def dmg_window():
    body = ('<use href="#appicon" x="70" y="62" width="110" height="110"/>'
            '<text x="125" y="196" font-size="13" text-anchor="middle" fill="var(--ink)">MIMUS</text>'
            '<path d="M200 118 C 250 96, 300 96, 350 118" fill="none" stroke="var(--accent)" stroke-width="5" stroke-linecap="round" stroke-dasharray="2 10"/>'
            '<path d="M342 104 L360 122 L336 128 Z" fill="var(--accent)"/>'
            + folder(372, 72, 1.15, None, "#4aa3ff") +
            '<text x="423" y="196" font-size="13" text-anchor="middle" fill="var(--ink)">Applicazioni</text>'
            '<text x="260" y="232" font-size="12.5" text-anchor="middle" fill="var(--muted)">trascina l’icona sulla cartella Applicazioni</text>')
    return window(520, 250, "MIMUS", body)

def privacy_panel():
    body = ('<rect x="1" y="34" width="150" height="235" fill="var(--side)"/>'
            + text_lines(18, 62, ["Generali", "Aspetto", "Rete", "Privacy e sicurezza", "Desktop e Dock"], size=12.5, lh=26) +
            '<rect x="10" y="121" width="132" height="24" rx="5" fill="var(--accent)" opacity=".18"/>'
            '<text x="174" y="64" font-size="17" font-weight="700" fill="var(--ink)">Privacy e sicurezza</text>'
            '<text x="174" y="100" font-size="12.5" font-weight="600" fill="var(--muted)">Sicurezza</text>'
            '<rect x="174" y="112" width="370" height="96" rx="8" fill="var(--field)" stroke="var(--line)"/>'
            + text_lines(188, 136, ["«MIMUS» è stata bloccata per", "proteggere il Mac."], size=12.5) +
            button(420, 168, 110, "Apri comunque", hl=True) +
            '<text x="174" y="240" font-size="12" fill="var(--muted)">Poi conferma con la password del Mac o con Touch ID.</text>')
    return window(560, 270, "Impostazioni di Sistema", body)

def finder_window():
    body = ('<rect x="1" y="34" width="150" height="205" fill="var(--side)"/>'
            '<text x="16" y="58" font-size="11" font-weight="600" fill="var(--muted)">Preferiti</text>'
            + text_lines(30, 82, ["Inizio", "Desktop", "Documenti", "Download"], size=12.5, lh=24) +
            '<text x="16" y="186" font-size="11" font-weight="600" fill="var(--muted)">Posizioni</text>'
            '<text x="30" y="210" font-size="12.5" fill="var(--ink)">MIMUS</text>'
            + folder(180, 60, 0.75, "Desktop", "#7fb8f5") + folder(270, 60, 0.75, "Documenti", "#7fb8f5")
            + folder(360, 60, 0.75, "Download", "#7fb8f5") +
            '<rect x="444" y="52" width="86" height="96" rx="8" fill="var(--accent)" opacity=".15"/>'
            + folder(452, 60, 0.75, "Tecnico", "#2f7de1") +
            '<text x="340" y="196" font-size="12.5" text-anchor="middle" fill="var(--muted)">La cartella «Tecnico» nella tua cartella Inizio</text>')
    return window(560, 240, "Inizio", body)

def menubar_menu():
    w, h = 560, 212
    ix, iy = 290, 5
    body = (f'<defs><mask id="mbm" style="mask-type:alpha" maskContentUnits="userSpaceOnUse">'
            f'<image href="{MB["sleep-0"]}" x="{ix}" y="{iy}" width="24" height="18"/></mask></defs>'
            f'<rect x="1" y="1" width="{w-2}" height="28" rx="0" fill="var(--win)" stroke="var(--line)"/>'
            f'<rect x="{ix-6}" y="3" width="36" height="22" rx="5" fill="var(--ink)" opacity=".12"/>'
            f'<rect x="{ix}" y="{iy}" width="24" height="18" fill="var(--ink)" mask="url(#mbm)"/>'
            # altre icone di sistema (generiche) e orologio
            '<path d="M334 19 q9 -10 18 0 M337 16 q6 -6 12 0" fill="none" stroke="var(--ink)" stroke-width="2" stroke-linecap="round"/><circle cx="343" cy="20" r="1.8" fill="var(--ink)"/>'
            '<rect x="366" y="9" width="22" height="11" rx="3" fill="none" stroke="var(--ink)" stroke-width="1.5"/><rect x="368.5" y="11.5" width="14" height="6" rx="1" fill="var(--ink)"/><rect x="389" y="12" width="2" height="5" rx="1" fill="var(--ink)"/>'
            '<text x="545" y="19" font-size="12.5" text-anchor="end" fill="var(--ink)">mer 15 ott 10:42</text>'
            '<text x="16" y="19" font-size="12.5" font-weight="700" fill="var(--ink)">Finder</text>'
            '<text x="76" y="19" font-size="12.5" fill="var(--ink)">Archivio   Composizione   Vista   Vai</text>')
    items = [("MIMUS – tutto sincronizzato", "muted"), None, ("Apri cartella", ""), ("Aggiorna cartelle", ""), None,
             ("Apri MIMUS…", ""), ("Guida", ""), None, ("Nascondi l’icona (torna riaprendo MIMUS)", "")]
    mx, my, mw = ix - 6, 32, 266
    mh = 10 + sum(9 if it is None else 22 for it in items)
    body += f'<rect x="{mx}" y="{my}" width="{mw}" height="{mh}" rx="9" fill="var(--paper)" stroke="var(--line)"/>'
    y = my + 6
    for it in items:
        if it is None:
            body += f'<line x1="{mx+10}" y1="{y+4.5}" x2="{mx+mw-10}" y2="{y+4.5}" stroke="var(--line)"/>'
            y += 9
            continue
        label, kind = it
        body += f'<text x="{mx+14}" y="{y+15.5}" font-size="12.5" fill="{"var(--muted)" if kind else "var(--ink)"}">{E(label)}</text>'
        y += 22
    return f'<svg viewBox="0 0 {w} {h}" role="img" aria-label="Icona di MIMUS nella barra dei menu" class="shot">{body}</svg>'

# ---------- testo della guida ----------

FIG = {
    "dmg": dmg_window(),
    "privacy": privacy_panel(),
    "welcome": dialog(["Benvenuto in MIMUS.", "", "Configuro il collegamento alla cartella del server:",
                       "la troverai nel Finder nella tua cartella Inizio,", "con il nome che sceglierai, anche senza rete."], ["Esci", "Configura"]),
    "foldername": dialog(["Nome della cartella nel Finder", "(sarà nella tua cartella Inizio):"], ["Annulla", "OK"], field="Tecnico"),
    "server": dialog(["Indirizzo del server remoto (IP o nome):"], ["Annulla", "OK"], field="192.168.1.10"),
    "password": dialog(["Password di m.rossi:"], ["Annulla", "OK"], field="password1", hidden=True),
    "space": dialog(["Spazio da lasciare sempre libero sul disco del Mac (GB):"], ["Annulla", "OK"], field="10", w=500),
    "done": dialog(["Fatto: la cartella è collegata e si ricollegherà", "da sola a ogni accensione."], ["OK", "Apri cartella"]),
    "finder": finder_window(),
    "menubar": menubar_menu(),
    "menu": choose_list(["Stato: attivo – cartella collegata", "2 modifiche fatte offline in attesa"],
                        ["Apri cartella", "Aggiorna cartelle", "Stato dettagliato", "Riavvia collegamento",
                         "Ferma collegamento", "Ultimi errori", "Apri log", "Log dettagliato (diagnosi) on/off",
                         "Impostazioni", "Controlla aggiornamenti", "Guida", "Disinstalla"]),
    "settings": choose_list(["Impostazioni: scegli cosa cambiare.", "Le modifiche vengono applicate quando premi «Fine»."],
                            ["Server remoto: 192.168.1.10", "Cartella remota: Tecnico", "Utente: m.rossi (dominio WORKGROUP)",
                             "Password: ••••••", "Cartella nel Finder: ~/Tecnico", "File tenuti sul Mac in: ~/Library/…/MIMUS/cache",
                             "Togli dal Mac i file non usati da: 30 giorni", "Spazio minimo libero sul disco: 10 GB",
                             "Novità dei colleghi visibili entro: 5 minuti", "Aggiornamenti automatici di MIMUS: sì",
                             "Icona nella barra dei menu: sì"], sel=6, w=500, ok_label="Modifica", cancel_label="Fine"),
    "conflict": dialog(["«Commesse/C123/Offerta.docx» è stato modificato in remoto",
                        "da qualcun altro mentre lo modificavi anche tu.", "",
                        "Versione remota: 13/10/2026 15:42, 48 KB", "",
                        "• Scarta la mia: resta quella remota",
                        "• Tieni entrambe: la tua viene salvata come",
                        "   «Offerta (conflitto Mac 2026-10-13 1545).docx»",
                        "• Sovrascrivi: la tua sostituisce quella remota"],
                       ["Scarta la mia", "Tieni entrambe", "Sovrascrivi in remoto"], w=520, default="Tieni entrambe"),
    "rename": dialog(["Hai rinominato offline «Disegni/Tav01.dwg» in «Tav01-rev2.dwg»,",
                      "ma nel frattempo qualcuno ha modificato il file in remoto.", "", "Cosa faccio?"],
                     ["Annulla rinomina", "Rinomina comunque"], w=520, default="Annulla rinomina"),
    "newpass": dialog(["Il server remoto non accetta più la password di «m.rossi»", "(forse è cambiata o scaduta).", "",
                       "Per non bloccare l’account ho sospeso i tentativi:", "inserisci la nuova password."],
                      ["Annulla", "Salva"], field="password1", hidden=True),
    "disk": dialog(["Spazio quasi esaurito sul disco del Mac: liberi 8,2 GB,", "la soglia minima è 10 GB.", "",
                    "I file remoti tenuti sul Mac occupano circa 21,4 GB."], ["OK"], w=500),
}

def fig(key, caption):
    return f'<figure>{FIG[key]}<figcaption>{caption}</figcaption></figure>'

page = f"""<!doctype html>
<html lang="it">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Guida MIMUS</title>
<link rel="icon" href="{ICON}">
<style>
:root {{
  --bg: #f5f5f7; --paper: #ffffff; --ink: #1d1d1f; --muted: #6e6e73; --line: #d2d2d7;
  --accent: #0a6cdf; --soft: #eaf2fd; --warn: #fff4e5; --warn-ink: #8a4b00;
  --win: #ececec; --field: #ffffff; --btn: #ffffff; --side: #e3e3e6;
}}
@media (prefers-color-scheme: dark) {{
  :root:not([data-theme="light"]) {{
    --bg: #111113; --paper: #1c1c1e; --ink: #f2f2f7; --muted: #a1a1a6; --line: #3a3a3c;
    --accent: #3b8cff; --soft: #1b2a40; --warn: #3a2a12; --warn-ink: #ffcf8a;
    --win: #2c2c2e; --field: #1c1c1e; --btn: #3a3a3c; --side: #252527;
  }}
}}
:root[data-theme="dark"] {{
  --bg: #111113; --paper: #1c1c1e; --ink: #f2f2f7; --muted: #a1a1a6; --line: #3a3a3c;
  --accent: #3b8cff; --soft: #1b2a40; --warn: #3a2a12; --warn-ink: #ffcf8a;
  --win: #2c2c2e; --field: #1c1c1e; --btn: #3a3a3c; --side: #252527;
}}
* {{ box-sizing: border-box; }}
body {{ margin: 0; background: var(--bg); color: var(--ink);
  font: 16px/1.6 -apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica Neue", Arial, sans-serif; }}
header {{ background: var(--paper); border-bottom: 1px solid var(--line); }}
.wrap {{ max-width: 860px; margin: 0 auto; padding: 0 20px; }}
.hero {{ display: flex; gap: 22px; align-items: center; padding: 36px 0 30px; }}
.hero .appicon {{ width: 92px; height: 92px; flex: none; }}
h1 {{ font-size: 34px; line-height: 1.15; margin: 0 0 6px; letter-spacing: -.02em; }}
.lead {{ margin: 0; color: var(--muted); font-size: 17px; }}
.lead.small {{ font-size: 14px; margin-top: 4px; font-style: italic; }}
nav {{ display: flex; flex-wrap: wrap; gap: 8px; padding-bottom: 18px; }}
nav a {{ font-size: 14px; color: var(--accent); text-decoration: none; background: var(--soft);
  padding: 5px 12px; border-radius: 999px; }}
main {{ padding: 10px 0 60px; }}
section {{ background: var(--paper); border: 1px solid var(--line); border-radius: 16px;
  padding: 26px 28px; margin: 22px 0; }}
h2 {{ font-size: 24px; margin: 0 0 12px; letter-spacing: -.01em; }}
h3 {{ font-size: 18px; margin: 26px 0 8px; }}
p {{ margin: 0 0 12px; }}
ol.steps {{ counter-reset: s; list-style: none; padding: 0; margin: 0; }}
ol.steps > li {{ counter-increment: s; position: relative; padding-left: 44px; margin: 0 0 22px; }}
ol.steps > li::before {{ content: counter(s); position: absolute; left: 0; top: 0; width: 30px; height: 30px;
  border-radius: 50%; background: var(--accent); color: #fff; font-weight: 700; display: grid; place-items: center; font-size: 15px; }}
figure {{ margin: 14px 0 6px; }}
.shot {{ width: 100%; max-width: 560px; height: auto; display: block;
  font-family: -apple-system, BlinkMacSystemFont, "Helvetica Neue", Arial, sans-serif;
  filter: drop-shadow(0 8px 22px rgba(0,0,0,.14)); }}
figcaption {{ font-size: 13.5px; color: var(--muted); margin-top: 8px; }}
.note {{ background: var(--soft); border-radius: 10px; padding: 12px 16px; margin: 12px 0; font-size: 15px; }}
.warn {{ background: var(--warn); color: var(--warn-ink); border-radius: 10px; padding: 12px 16px; margin: 12px 0; font-size: 15px; }}
table {{ width: 100%; border-collapse: collapse; font-size: 15px; margin: 8px 0 4px; }}
td, th {{ text-align: left; padding: 9px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }}
th {{ font-size: 13px; color: var(--muted); font-weight: 600; }}
.ok {{ color: #1f9d55; font-weight: 600; }} .no {{ color: #d70015; font-weight: 600; }}
code {{ font: 13.5px ui-monospace, SFMono-Regular, Menlo, monospace; background: var(--soft); padding: 2px 6px; border-radius: 5px; overflow-wrap: anywhere; }}
pre {{ background: var(--soft); border-radius: 10px; padding: 12px 14px; overflow-x: auto; font-size: 13.5px; }}
details {{ border-bottom: 1px solid var(--line); padding: 10px 0; }}
summary {{ cursor: pointer; font-weight: 600; }}
details p {{ margin: 8px 0 4px; }}
a {{ color: var(--accent); }}
footer {{ color: var(--muted); font-size: 13px; text-align: center; padding: 0 0 40px; }}
.mb {{ display: inline-block; position: relative; width: 48px; height: 36px; vertical-align: middle; }}
.mb i {{ position: absolute; inset: 0; background: var(--ink);
  -webkit-mask: var(--m) center / contain no-repeat; mask: var(--m) center / contain no-repeat; }}
.mb.a i {{ opacity: 0; animation-name: mbf; animation-timing-function: steps(1, end); animation-iteration-count: infinite; }}
@keyframes mbf {{ 0% {{ opacity: 1; }} 25% {{ opacity: 0; }} 100% {{ opacity: 0; }} }}
@media (prefers-reduced-motion: reduce) {{ .mb.a i {{ animation: none; }} .mb.a i:first-child {{ opacity: 1; }} }}
td.icon {{ width: 64px; text-align: center; vertical-align: middle; }}
@media (max-width: 600px) {{
  .hero {{ flex-direction: column; align-items: flex-start; }} h1 {{ font-size: 28px; }}
  section {{ padding: 20px 16px; }} ol.steps > li {{ padding-left: 40px; }}
}}
@media print {{ nav {{ display: none; }} section {{ break-inside: avoid; border: none; }} }}
</style>
</head>
<body>
<svg width="0" height="0" style="position:absolute" aria-hidden="true"><defs><symbol id="appicon" viewBox="0 0 1024 1024"><image href="{ICON}" width="1024" height="1024"/></symbol></defs></svg>
<header><div class="wrap">
  <div class="hero">
    <svg class="appicon" viewBox="0 0 1024 1024" aria-hidden="true"><use href="#appicon"/></svg>
    <div>
      <h1>MIMUS</h1>
      <p class="lead">La cartella del server sul tuo Mac, anche quando sei fuori ufficio. Guida per la versione {E(VERSION)}.</p>
      <p class="lead small">Il nome viene dal Gallimimus, il dinosauro corridore.</p>
    </div>
  </div>
  <nav>
    <a href="#installa">Installazione</a><a href="#primo">Primo avvio</a><a href="#uso">Uso quotidiano</a><a href="#barra">Icona in alto</a>
    <a href="#offline">Lavorare offline</a><a href="#popup">I messaggi</a><a href="#menu">Il menu</a><a href="#impostazioni">Impostazioni</a>
    <a href="#aggiorna">Aggiornare e disinstallare</a><a href="#problemi">Problemi</a><a href="#it">Per l’IT</a>
  </nav>
</div></header>

<main class="wrap">

<section id="installa">
  <h2>Installazione</h2>
  <ol class="steps">
    <li><strong>Apri il file <code>MIMUS-{E(VERSION)}.dmg</code></strong> (doppio clic). Si apre una finestra come questa.
      {fig("dmg", "Trascina l’icona di MIMUS sulla cartella Applicazioni.")}</li>
    <li><strong>Trascina l’icona sulla cartella Applicazioni.</strong> Poi puoi chiudere la finestra ed espellere il disco «MIMUS» dalla barra laterale del Finder.</li>
    <li><strong>Apri MIMUS dalla cartella Applicazioni</strong> (non dalla finestra del DMG).
      <div class="note">La prima volta macOS dice che non può verificare l’app, perché non è distribuita dall’App Store. È normale: vai in
      <strong>Impostazioni di Sistema → Privacy e sicurezza</strong>, scorri in basso e premi <strong>Apri comunque</strong>. Serve solo la prima volta.</div>
      {fig("privacy", "Impostazioni di Sistema → Privacy e sicurezza → Apri comunque.")}</li>
  </ol>
</section>

<section id="primo">
  <h2>Primo avvio: configurazione</h2>
  <p>L’app fa poche domande, una alla volta. Le risposte te le dà l’ufficio IT.</p>
  {fig("welcome", "Benvenuto: premi Configura.")}
  <table>
    <tr><th>Domanda</th><th>Cosa scrivere</th></tr>
    <tr><td>Indirizzo del server remoto</td><td>indirizzo IP o nome del server, es. <code>192.168.1.10</code>. Puoi anche incollare l’indirizzo completo come lo usi nel Finder o in Windows (<code>smb://server/Cartella</code> o <code>&#92;&#92;server&#92;Cartella</code>): l’app lo divide da sola</td></tr>
    <tr><td>Cartella remota</td><td>il nome della cartella condivisa sul server, es. <code>Tecnico</code>; per usarne solo una parte: <code>Tecnico/Progetti</code></td></tr>
    <tr><td>Nome della cartella nel Finder</td><td>come vuoi vederla sul Mac: viene proposto il nome della cartella remota, puoi scrivere quello che preferisci. Nome e posizione si cambiano anche dopo, in <a href="#impostazioni">Impostazioni</a></td></tr>
    <tr><td>Utente, Dominio</td><td>le stesse credenziali del PC dell’ufficio: l’utente <strong>senza</strong> dominio (es. <code>m.rossi</code>), il dominio nella domanda dopo; se non usi un dominio lascia <code>WORKGROUP</code></td></tr>
    <tr><td>Password</td><td>la tua password di rete (resta salvata sul Mac, nascosta)</td></tr>
    <tr><td>Spazio da lasciare libero</td><td>quanti GB tenere sempre liberi sul disco: 10 va bene</td></tr>
  </table>
  {fig("server", "Ogni domanda ha un campo e i pulsanti Annulla / OK.")}
  {fig("foldername", "Il nome con cui vedrai la cartella nel Finder.")}
  {fig("password", "La password non si vede mentre la scrivi.")}
  <p>Prima di finire l’app prova il collegamento: se il server non risponde, la cartella non esiste o la password non è accettata te lo dice subito, e con <strong>Correggi</strong> ripeti le domande con i valori già inseriti.</p>
  {fig("done", "Fatto: da ora la cartella si collega da sola a ogni accensione.")}
</section>

<section id="uso">
  <h2>Uso quotidiano</h2>
  <p>Non devi fare niente: all’accensione del Mac la cartella (negli esempi <strong>Tecnico</strong>, ma ha il nome che hai scelto) si collega da sola. La trovi nel Finder dentro la tua cartella Inizio (la casetta con il tuo nome).
  Puoi trascinarla nella barra laterale del Finder per averla sempre a portata di mano.</p>
  {fig("finder", "La cartella collegata nella cartella Inizio.")}
  <ul>
    <li>I file si scaricano <strong>quando li apri</strong> e poi restano sul Mac: la seconda volta si aprono subito, anche senza rete.</li>
    <li>Quando salvi, il file viene caricato sul server remoto dopo pochi secondi.</li>
    <li>Le modifiche dei colleghi compaiono entro 5 minuti (modificabile in <a href="#impostazioni">Impostazioni</a>). Per vederle subito: app → <em>Aggiorna cartelle</em>.</li>
    <li>I file che non apri da <strong>30 giorni</strong> (modificabile) vengono tolti dal Mac per fare spazio, ma solo se sono al sicuro sul server remoto. Restano visibili e si riscaricano quando li riapri.</li>
  </ul>
</section>

<section id="barra">
  <h2>L’icona nella barra dei menu</h2>
  <p>In alto a destra, vicino all’orologio, c’è il Gallimimus di MIMUS. Ha lo stesso colore delle altre icone di sistema e ti dice a colpo d’occhio cosa sta succedendo:</p>
  <table>
    <tr><th class="icon">Icona</th><th>Cosa vuol dire</th></tr>
    <tr><td class="icon">{mb_icon(["sleep-0", "sleep-1", "sleep-2", "sleep-2"], 3.2, "dorme")}</td><td><strong>Dorme (Z Z Z):</strong> tutto sincronizzato, non c’è niente da fare. Dorme anche quando sei <strong>offline</strong>: lavori sui file già sul Mac e le modifiche partono da sole al ritorno in rete.</td></tr>
    <tr><td class="icon">{mb_icon(["run-0", "run-1", "run-2", "run-3"], 0.52, "corre")}</td><td><strong>Corre:</strong> sta sincronizzando: carica o scarica file, applica le modifiche fatte offline o aggiorna l’elenco delle cartelle. Puoi continuare a lavorare.</td></tr>
    <tr><td class="icon">{mb_icon(["alert"], 1, "punto esclamativo")}</td><td><strong>Punto esclamativo:</strong> c’è un problema da guardare: password non accettata, modifiche rifiutate dal server remoto, disco quasi pieno, collegamento fermo. Clicca l’icona per leggere cosa succede.</td></tr>
  </table>
  {fig("menubar", "Cliccando l’icona: lo stato in parole e i comandi più usati.")}
  <p><em>Apri MIMUS…</em> porta al <a href="#menu">menu completo</a> dell’app. <em>Nascondi l’icona</em> la toglie finché non riapri MIMUS (o fino al prossimo accesso); per toglierla sempre: <a href="#impostazioni">Impostazioni</a> → <em>Icona nella barra dei menu: no</em>.</p>
  <div class="note">Non la vedi? Sui Mac con la fotocamera nello schermo le icone in eccesso restano nascoste dietro la tacca: tieni premuto <strong>⌘</strong> e trascina via qualche altra icona per fare spazio (con ⌘ puoi anche spostare quella di MIMUS). Sulle versioni recenti di macOS controlla anche Impostazioni di Sistema → <strong>Barra dei menu</strong>, che MIMUS sia consentita.</div>
</section>

<section id="offline">
  <h2>Lavorare offline</h2>
  <p>Fuori ufficio, senza rete o senza VPN, la cartella continua a funzionare con quello che hai già sul Mac.</p>
  <table>
    <tr><th>Offline puoi…</th><th></th></tr>
    <tr><td>Vedere tutte le cartelle remote</td><td class="ok">sì</td></tr>
    <tr><td>Aprire e modificare i file che avevi già aperto</td><td class="ok">sì</td></tr>
    <tr><td>Creare file e cartelle, rinominare, cancellare</td><td class="ok">sì, vengono applicati quando torni in rete</td></tr>
    <tr><td>Aprire un file mai aperto prima</td><td class="no">no, va scaricato</td></tr>
    <tr><td>Vedere le novità dei colleghi</td><td class="no">no, arrivano quando torni in rete</td></tr>
  </table>
  <div class="note"><strong>Prima di partire:</strong> apri una volta i file che ti serviranno fuori ufficio, così restano sul Mac.</div>
  <div class="warn">Le modifiche fatte offline esistono solo sul tuo Mac finché non torni in rete: se il Mac si rompe o viene perso prima, si perdono. Appena puoi, collegati alla VPN.</div>
</section>

<section id="popup">
  <h2>Quando compare un messaggio</h2>
  <p>A volte l’app ti chiede cosa fare. Se non rispondi entro un’ora sceglie da sola l’opzione che <strong>non perde nessun file</strong>.</p>

  <h3>Lo stesso file modificato da te e da un collega</h3>
  {fig("conflict", "Conflitto: scegli quale versione tenere.")}
  <ul>
    <li><strong>Tieni entrambe</strong> (consigliato): la tua versione viene salvata accanto all’originale con «conflitto Mac» nel nome. Poi decidi con calma, magari insieme al collega.</li>
    <li><strong>Sovrascrivi in remoto</strong>: vale la tua, quella del collega viene sostituita.</li>
    <li><strong>Scarta la mia</strong>: vale quella del collega, le tue modifiche si perdono.</li>
  </ul>

  <h3>Hai rinominato o cancellato offline un file che un collega ha cambiato</h3>
  {fig("rename", "Rinomina in sospeso su un file modificato da altri.")}
  <p>Se in remoto esiste già un file con il nuovo nome, l’app propone di usare un nome con «conflitto Mac». Se un collega ha messo file in una cartella che hai cancellato offline, la cartella non viene cancellata e ricevi un avviso.</p>

  <h3>La password non funziona più</h3>
  {fig("newpass", "Password cambiata o scaduta.")}
  <p>Succede quando cambi la password di rete. L’app smette di provare (così l’account non si blocca) e chiede quella nuova. Dopo <em>Salva</em> si ricollega da sola.</p>

  <h3>Disco del Mac quasi pieno</h3>
  {fig("disk", "Avviso di spazio.")}
  <p>L’app libera spazio togliendo i file remoti usati meno di recente (solo se sono al sicuro sul server remoto). Se non basta, ti avvisa: libera spazio cancellando file tuoi o svuotando il Cestino.</p>
</section>

<section id="menu">
  <h2>Il menu dell’app</h2>
  <p>Riaprendo MIMUS da Applicazioni (o dall’icona in alto → <em>Apri MIMUS…</em>) compare lo stato e l’elenco delle azioni. In alto vedi se ci sono file ancora da caricare o modifiche offline in attesa.</p>
  {fig("menu", "Il menu: scegli un’azione e premi Esegui.")}
  <table>
    <tr><th>Azione</th><th>A cosa serve</th></tr>
    <tr><td>Apri cartella</td><td>apre la cartella nel Finder</td></tr>
    <tr><td>Aggiorna cartelle</td><td>mostra subito le novità dei colleghi</td></tr>
    <tr><td>Stato dettagliato</td><td>server remoto raggiungibile, spazio su disco, file tenuti sul Mac, versione</td></tr>
    <tr><td>Riavvia / Ferma / Avvia collegamento</td><td>se qualcosa non va; i file in attesa non si perdono</td></tr>
    <tr><td>Ultimi errori, Apri log</td><td>per l’ufficio IT</td></tr>
    <tr><td>Log dettagliato</td><td>da attivare solo se te lo chiede l’IT</td></tr>
    <tr><td>Impostazioni</td><td>tutte le impostazioni (vedi sotto)</td></tr>
    <tr><td>Controlla aggiornamenti</td><td>cerca subito una versione nuova di MIMUS</td></tr>
    <tr><td>Guida</td><td>apre questa guida</td></tr>
    <tr><td>Disinstalla</td><td>toglie il collegamento dal Mac</td></tr>
  </table>
</section>

<section id="impostazioni">
  <h2>Impostazioni</h2>
  <p>Menu → <em>Impostazioni</em>. Scegli una voce, premi <strong>Modifica</strong>, cambia il valore. Quando hai finito premi <strong>Fine</strong>: l’app applica tutto insieme e riavvia il collegamento (i file in attesa non si perdono).</p>
  {fig("settings", "Le impostazioni con i valori attuali.")}
  <table>
    <tr><th>Voce</th><th>Cosa cambia</th></tr>
    <tr><td>Server remoto, Cartella remota</td><td>indirizzo del server e cartella da collegare</td></tr>
    <tr><td>Utente, Password</td><td>le credenziali di rete; la password resta salvata nascosta</td></tr>
    <tr><td>Cartella nel Finder</td><td>dove compare la cartella (scegli la posizione e il nome)</td></tr>
    <tr><td>File tenuti sul Mac in</td><td>dove salvare le copie dei file remoti e le modifiche fatte offline, per esempio su un disco più capiente</td></tr>
    <tr><td>Togli dal Mac i file non usati da</td><td>dopo quanti giorni un file non aperto viene tolto dal Mac (sempre e solo se è al sicuro sul server remoto)</td></tr>
    <tr><td>Spazio minimo libero</td><td>quanti GB lasciare sempre liberi sul disco</td></tr>
    <tr><td>Aggiornamenti automatici di MIMUS</td><td>sì/no: controllo giornaliero delle nuove versioni</td></tr>
    <tr><td>Icona nella barra dei menu</td><td>sì/no: il dinosauro in alto a destra (vedi <a href="#barra">sopra</a>)</td></tr>
    <tr><td>Novità dei colleghi visibili entro</td><td>ogni quanti minuti controllare le modifiche fatte da altri nelle cartelle che non stai guardando (valori bassi = più richieste al server remoto); quelle aperte di recente si aggiornano da sole in pochi secondi</td></tr>
  </table>
  <div class="note">Server, cartella condivisa e posizione dei file si possono cambiare solo quando non ci sono modifiche in attesa di essere caricate: altrimenti l’app te lo dice e salva solo le altre impostazioni.
  Se sposti i file su un altro disco, quelli già scaricati verranno riscaricati quando li riapri.</div>
</section>

<section id="aggiorna">
  <h2>Aggiornare e disinstallare</h2>
  <h3>Nuova versione</h3>
  <p>MIMUS controlla da sola se c’è una versione nuova (quando la apri e una volta al giorno). Se c’è ti mostra le novità: premi <strong>Aggiorna</strong> e fa tutto lei: scarica, verifica che il file sia integro, sostituisce l’app e riavvia il collegamento. Configurazione, file sul Mac e modifiche in attesa restano.</p>
  <p>Puoi controllare anche a mano (menu → <em>Controlla aggiornamenti</em>) o disattivare il controllo automatico in <a href="#impostazioni">Impostazioni</a>. In alternativa scarica il DMG dalla pagina del progetto su GitHub, trascina l’app in Applicazioni scegliendo <strong>Sostituisci</strong> e aprila una volta.</p>
  <h3>Disinstallare</h3>
  <p>App → <em>Disinstalla</em>. Se ci sono modifiche non ancora caricate sul server remoto l’app si rifiuta, per non perderle: collegati al server remoto e riprova. Alla fine trascina l’app dalla cartella Applicazioni nel Cestino.</p>
</section>

<section id="problemi">
  <h2>Problemi frequenti</h2>
  <details><summary>macOS dice che l’app «è danneggiata»</summary>
    <p>Succede a volte con le app scaricate da Internet non firmate da Apple. Apri il Terminale e scrivi:</p>
    <pre>xattr -cr "/Applications/MIMUS.app"</pre>
    <p>Poi riapri l’app.</p></details>
  <details><summary>macOS chiede di consentire a MIMUS l’accesso alla rete locale</summary>
    <p>Premi <strong>Consenti</strong>: serve per raggiungere il server remoto. Se l’hai negato per sbaglio: Impostazioni di Sistema → Privacy e sicurezza → <strong>Rete locale</strong> → attiva MIMUS, poi menu → <em>Riavvia collegamento</em>.</p></details>
  <details><summary>La cartella è vuota o non si apre</summary>
    <p>App → <em>Riavvia collegamento</em>. Se non basta, guarda <em>Ultimi errori</em> e manda una foto all’IT.</p></details>
  <details><summary>Sono in ufficio ma non vedo i file nuovi dei colleghi</summary>
    <p>Nelle cartelle che stai guardando compaiono da soli entro pochi secondi (in VPN entro un minuto circa); nelle altre entro 5 minuti (modificabile nelle <a href="#impostazioni">Impostazioni</a>). Per vederli subito: <em>Aggiorna cartelle</em>, dall’icona in alto o dall’app.</p></details>
  <details><summary>Da fuori ufficio non si collega</summary>
    <p>Controlla che la VPN sia attiva. Senza VPN lavori offline con i file già sul Mac.</p></details>
  <details><summary>Un file non si apre offline</summary>
    <p>Non era mai stato aperto su questo Mac (o era stato tolto perché non usato da 30 giorni): si potrà aprire quando torni in rete.</p></details>
  <details><summary>Trovo file con «conflitto Mac» nel nome</summary>
    <p>Sono le tue versioni salvate in un conflitto (vedi <a href="#popup">messaggi</a>). Confrontale con l’originale e cancella quella che non serve.</p></details>
</section>

<section id="it">
  <h2>Per l’ufficio IT</h2>
  <table>
    <tr><th>Cosa</th><th>Dove</th></tr>
    <tr><td>App (contiene anche rclone-mimus)</td><td><code>/Applications/MIMUS.app</code></td></tr>
    <tr><td>Avvio automatico</td><td><code>~/Library/LaunchAgents/app.mimus.agent.plist</code> (scritto dall’app)</td></tr>
    <tr><td>Controllo aggiornamenti</td><td><code>~/Library/LaunchAgents/app.mimus.update.plist</code>, una volta al giorno; legge le Release di <code>github.com/ranzaWARE/mimus</code></td></tr>
    <tr><td>Icona nella barra dei menu</td><td><code>MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app</code>, avviata da <code>~/Library/LaunchAgents/app.mimus.menu.plist</code>; legge lo stato dal servizio su <code>127.0.0.1:5572</code> (rc <code>mimus/status</code>)</td></tr>
    <tr><td>Impostazioni (<code>settings.json</code>)</td><td><code>~/Library/Application Support/MIMUS/</code></td></tr>
    <tr><td>Cache e coda offline</td><td><code>~/Library/Application Support/MIMUS/cache</code> o la posizione scelta nelle Impostazioni</td></tr>
    <tr><td>Modifiche offline rifiutate dal server remoto</td><td><code>…/cache/offline/mimus/*/journal-failed.json</code></td></tr>
    <tr><td>Credenziali (password offuscata)</td><td><code>~/.config/rclone/rclone.conf</code>, sezioni <code>[remoto]</code> e <code>[mimus]</code></td></tr>
    <tr><td>Log (5 × 10 MB a rotazione)</td><td><code>~/Library/Logs/MIMUS.log</code></td></tr>
  </table>
  <p>Diagnostica da Terminale: <code>bash scripts/status.sh</code> dal pacchetto sorgente. Il server remoto è considerato raggiungibile se risponde sulla porta 445.</p>
</section>

</main>
<footer>MIMUS {E(VERSION)} · le immagini sono illustrative</footer>
</body>
</html>
"""
out = os.path.join(HERE, "Guida.html")
open(out, "w", encoding="utf-8").write(page)
print(out)
