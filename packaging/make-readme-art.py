#!/usr/bin/env python3
"""Immagini del README (docs/img), in versione chiara e scura.

  dino-*.gif        il dinosauro grande: corre, dorme, fa il cuoricino
  hero-*.gif        barra dei menu con il dinosauro animato e il suo menu
  sleep/run/alert-* il dinosauro nei tre stati (pixel art ingrandita)
  finder-*.png      pallini e menu nel Finder (dalla guida)
  badge-*.svg       i pallini dell'estensione del Finder
  icon.png          icona dell'app

Serve Pillow; per finder/badges anche Playwright con Chromium.
  python3 packaging/make-readme-art.py
"""
import importlib.util
import os

from PIL import Image, ImageDraw, ImageFilter, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
OUT = os.path.join(ROOT, "docs", "img")
os.makedirs(OUT, exist_ok=True)

spec = importlib.util.spec_from_file_location("icons", os.path.join(HERE, "make-menu-icons.py"))
icons = importlib.util.module_from_spec(spec)
spec.loader.exec_module(icons)
FR = icons.frames()  # nome -> griglia 24x18 di 0/1

FONT_DIR = "/usr/share/fonts/opentype/inter"
def font(weight, size):
    return ImageFont.truetype(os.path.join(FONT_DIR, f"Inter-{weight}.otf"), size)

THEMES = {
    "light": dict(bar=(236, 236, 238), ink=(28, 28, 30), desk=(214, 224, 236), menu=(250, 250, 252),
                  line=(214, 214, 218), muted=(128, 128, 134), hl=(10, 108, 223)),
    "dark": dict(bar=(42, 42, 46), ink=(240, 240, 245), desk=(22, 26, 32), menu=(44, 44, 48),
                 line=(70, 70, 76), muted=(150, 150, 158), hl=(59, 140, 255)),
}

# sequenza dell'animazione: (fotogramma, millisecondi, titolo, riga di dettaglio)
SEQ = []
for _ in range(2):
    for f in ["sleep-0", "sleep-1", "sleep-2", "sleep-2"]:
        SEQ.append((f, 700, "tutto sincronizzato", ""))
for _ in range(9):
    for f in ["run-0", "run-1", "run-2", "run-3"]:
        SEQ.append((f, 130, "sincronizzazione in corso", "Scarico 1 file dal server remoto"))
SEQ.append(("alert", 2600, "il server remoto non accetta la password", "Apri MIMUS per i dettagli."))
for f in ["sleep-0", "sleep-1", "sleep-2", "sleep-2"]:
    SEQ.append((f, 700, "tutto sincronizzato", ""))


def paste_dino(img, grid, x, y, scale, color):
    d = ImageDraw.Draw(img)
    for gy, row in enumerate(grid):
        for gx, v in enumerate(row):
            if v:
                d.rectangle([x + gx * scale, y + gy * scale, x + (gx + 1) * scale - 1, y + (gy + 1) * scale - 1], fill=color)


def hero_frame(t, name, title, detail):
    W, S = 1200, 2
    rows = [("MIMUS – " + title, "muted")] + ([(detail, "muted")] if detail else [("", "muted")]) + \
        [None, ("Apri cartella", ""), ("Aggiorna cartelle", ""), None, ("Apri MIMUS…", "")]
    mh = 16 + sum(18 if r is None else 44 for r in rows)
    my, mw = 58, 600
    H = my + mh + 36
    img = Image.new("RGB", (W, H), t["desk"])
    d = ImageDraw.Draw(img)
    # barra dei menu
    d.rectangle([0, 0, W, 48], fill=t["bar"])
    d.text((28, 11), "Finder", font=font("Bold", 26), fill=t["ink"])
    x = 132
    for w in ["Archivio", "Composizione", "Vista", "Vai"]:
        d.text((x, 11), w, font=font("Regular", 26), fill=t["ink"])
        x += font("Regular", 26).getlength(w) + 36
    # a destra: orologio, batteria, rete, MIMUS
    f = font("Medium", 26)
    clock = "gio 15 ott  18:20"
    cx = W - 28 - f.getlength(clock)
    d.text((cx, 11), clock, font=f, fill=t["ink"])
    bx = cx - 92
    d.rounded_rectangle([bx, 14, bx + 46, 34], radius=6, outline=t["ink"], width=3)
    d.rounded_rectangle([bx + 6, 19, bx + 32, 29], radius=2, fill=t["ink"])
    d.rectangle([bx + 49, 20, bx + 52, 28], fill=t["ink"])
    wx = bx - 40
    for r in (16, 10):
        d.arc([wx - r, 30 - r, wx + r, 30 + r], 225, 315, fill=t["ink"], width=4)
    d.ellipse([wx - 3, 27, wx + 3, 33], fill=t["ink"])
    dx = wx - 100
    hl = tuple(min(255, max(0, c + (22 if t is THEMES["dark"] else -24))) for c in t["bar"])
    d.rounded_rectangle([dx - 12, 5, dx + 60, 43], radius=10, fill=hl)
    paste_dino(img, FR[name], dx, 6, S, t["ink"])
    # menu aperto sotto il dinosauro
    mx = min(dx - 18, W - mw - 16)
    shadow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    ImageDraw.Draw(shadow).rounded_rectangle([mx, my + 6, mx + mw, my + mh + 6], radius=18, fill=(0, 0, 0, 90))
    shadow = shadow.filter(ImageFilter.GaussianBlur(10))
    img = Image.alpha_composite(img.convert("RGBA"), shadow).convert("RGB")
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([mx, my, mx + mw, my + mh], radius=16, fill=t["menu"], outline=t["line"], width=2)
    y = my + 8
    for r in rows:
        if r is None:
            d.line([mx + 20, y + 9, mx + mw - 20, y + 9], fill=t["line"], width=2)
            y += 18
            continue
        txt, kind = r
        d.text((mx + 28, y + 8), txt, font=font("Medium" if kind else "Regular", 25),
               fill=t["muted"] if kind else t["ink"])
        y += 44
    return img


def save_gif(frames, durations, path):
    pal = [f.convert("P", palette=Image.ADAPTIVE, colors=64) for f in frames]
    pal[0].save(path, save_all=True, append_images=pal[1:], duration=durations, loop=0, optimize=False, disposal=2)


def state_gif(names, durations, color, path, scale=5):
    W, H = 24 * scale + 8, 18 * scale + 8
    frames = []
    for n in names:
        im = Image.new("P", (W, H), 0)
        im.putpalette([255, 0, 255] + list(color) + [0] * (254 * 3))
        d = ImageDraw.Draw(im)
        for gy, row in enumerate(FR[n]):
            for gx, v in enumerate(row):
                if v:
                    d.rectangle([4 + gx * scale, 4 + gy * scale, 4 + (gx + 1) * scale - 1, 4 + (gy + 1) * scale - 1], fill=1)
        frames.append(im)
    if len(frames) == 1:
        frames[0].save(path, transparency=0)
    else:
        frames[0].save(path, save_all=True, append_images=frames[1:], duration=durations, loop=0, transparency=0, disposal=2)


def shots():
    """pallini e menu del Finder, presi dalla guida"""
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        print("Playwright mancante: finder/badges non rigenerati")
        return
    guide = os.path.join(HERE, "Guida.html")
    exe = "/opt/pw-browsers/chromium" if os.path.exists("/opt/pw-browsers/chromium") else None
    with sync_playwright() as p:
        b = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch()
        for scheme in ("light", "dark"):
            pg = b.new_page(viewport={"width": 820, "height": 1400}, color_scheme=scheme, device_scale_factor=2)
            pg.goto("file://" + guide + "#sulmac")
            pg.wait_for_timeout(300)
            pg.locator("#sulmac figure svg").first.screenshot(path=os.path.join(OUT, f"finder-{scheme}.png"), omit_background=True)
        b.close()


HEART_BIG = [".XX.XX.", "XXXXXXX", "XXXXXXX", ".XXXXX.", "..XXX..", "...X..."]
HEART_SMALL = [".X.X.", "XXXXX", ".XXX.", "..X.."]


def mascot(ink, path, scale=8):
    """il dinosauro grande del README: corre, dorme, fa il cuoricino"""
    W, H, ox, oy = 34, 26, 2, 7
    red = (255, 59, 48)

    def frame(grid, heart=None, ground_shift=None):
        im = Image.new("P", (W * scale, H * scale), 0)
        im.putpalette([255, 0, 255] + list(ink) + list(red) + [0] * (253 * 3))
        d = ImageDraw.Draw(im)

        def px(x, y, c):
            d.rectangle([x * scale, y * scale, (x + 1) * scale - 1, (y + 1) * scale - 1], fill=c)
        for gy, row in enumerate(grid):
            for gx, v in enumerate(row):
                if v:
                    px(ox + gx, oy + gy, 1)
        # terreno tratteggiato (scorre mentre corre)
        sh = ground_shift or 0
        for x in range(W):
            if (x + sh) % 4 < 2:
                px(x, H - 1, 1)
        if heart:
            art, hx, hy = heart
            for gy, row in enumerate(art):
                for gx, c in enumerate(row):
                    if c == "X":
                        px(hx + gx, hy + gy, 2)
        return im

    frames, durations = [], []
    # corre
    for i in range(24):
        frames.append(frame(FR[f"run-{i % 4}"], ground_shift=i))
        durations.append(110)
    # dorme
    for _ in range(2):
        for n, ms in (("sleep-0", 600), ("sleep-1", 600), ("sleep-2", 900)):
            frames.append(frame(FR[n]))
            durations.append(ms)
    # cuoricino che batte
    # in piedi, senza il punto esclamativo (colonne 21-22 dalla riga 6)
    stand = [[0 if (y >= 6 and x in (21, 22)) else v for x, v in enumerate(row)]
             for y, row in enumerate(FR["alert"])]
    for i in range(6):
        big = i % 2 == 0
        frames.append(frame(stand, heart=(HEART_BIG, 26, 1) if big else (HEART_SMALL, 27, 2)))
        durations.append(320)
    frames.append(frame(stand, heart=(HEART_BIG, 26, 1)))
    durations.append(900)
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=durations, loop=0, transparency=0, disposal=2)


def badge_svgs():
    """i pallini dell'estensione del Finder, come SVG"""
    for kind in ("remote", "local", "pinned", "upload"):
        g = '<circle cx="32" cy="32" r="30" fill="#fff"/>'
        if kind == "remote":
            g += '<circle cx="32" cy="32" r="21.5" fill="none" stroke="#8e8e93" stroke-width="9"/>'
        else:
            g += f'<circle cx="32" cy="32" r="26" fill="{"#ff9500" if kind == "upload" else "#34c759"}"/>'
        if kind == "pinned":
            g += '<path d="M19 31 L28 41 L45 22" fill="none" stroke="#fff" stroke-width="8" stroke-linecap="round" stroke-linejoin="round"/>'
        if kind == "upload":
            g += '<path d="M32 46 L32 18 M21 29 L32 18 L43 29" fill="none" stroke="#fff" stroke-width="8" stroke-linecap="round" stroke-linejoin="round"/>'
        svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">{g}</svg>\n'
        open(os.path.join(OUT, f"badge-{kind}.svg"), "w").write(svg)


if __name__ == "__main__":
    badge_svgs()
    mascot((28, 28, 30), os.path.join(OUT, "dino-light.gif"))
    mascot((240, 240, 245), os.path.join(OUT, "dino-dark.gif"))
    for name, t in THEMES.items():
        frames = [hero_frame(t, f, title, detail) for f, _, title, detail in SEQ]
        save_gif(frames, [ms for _, ms, _, _ in SEQ], os.path.join(OUT, f"hero-{name}.gif"))
        ink = t["ink"]
        state_gif(["sleep-0", "sleep-1", "sleep-2", "sleep-2"], [700] * 4, ink, os.path.join(OUT, f"sleep-{name}.gif"))
        state_gif(["run-0", "run-1", "run-2", "run-3"], [130] * 4, ink, os.path.join(OUT, f"run-{name}.gif"))
        state_gif(["alert"], [0], ink, os.path.join(OUT, f"alert-{name}.png"))
    Image.open(os.path.join(HERE, "AppIcon.png")).resize((256, 256), Image.LANCZOS).save(os.path.join(OUT, "icon.png"))
    shots()
    print("immagini in", OUT)
