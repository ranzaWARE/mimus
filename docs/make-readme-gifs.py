#!/usr/bin/env python3
"""GIF animate per il README (docs/img), dai fotogrammi di packaging/menubar.

  hero-{light,dark}.gif   barra dei menu + dinosauro grande che cambia stato
  sleep|run|alert-{light,dark}.gif   i tre stati, da soli

Serve Pillow e il font Inter (o DejaVu Sans). Uso:
  python3 packaging/make-menu-icons.py && python3 docs/make-readme-gifs.py
"""
import os
from PIL import Image, ImageDraw, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
SRC = os.path.join(ROOT, "packaging", "menubar")
OUT = os.path.join(HERE, "img")
os.makedirs(OUT, exist_ok=True)

THEMES = {
    "light": {"bg": (246, 246, 248), "bar": (232, 232, 236), "ink": (28, 28, 30), "muted": (110, 110, 115),
              "card": (255, 255, 255), "line": (210, 210, 215)},
    "dark": {"bg": (22, 22, 24), "bar": (44, 44, 48), "ink": (238, 238, 242), "muted": (160, 160, 166),
             "card": (32, 32, 35), "line": (60, 60, 64)},
}
STATES = {
    "sleep": (["sleep-0", "sleep-1", "sleep-2", "sleep-2"], 700,
              "Tutto sincronizzato", "dorme: niente da fare, anche offline"),
    "run": (["run-0", "run-1", "run-2", "run-3"], 120,
            "Sincronizzazione in corso", "corre: carica, scarica, applica le modifiche"),
    "alert": (["alert"], 1000, "C'è un problema", "«!»: password, spazio, collegamento fermo"),
}


def font(size, bold=False):
    for name in (["Inter-SemiBold.otf", "Inter-Bold.otf"] if bold else ["Inter-Regular.otf", "Inter-Medium.otf"]) + \
            ["DejaVuSans-Bold.ttf" if bold else "DejaVuSans.ttf"]:
        for d in ("/usr/share/fonts/opentype/inter", "/usr/share/fonts/truetype/dejavu",
                  "/System/Library/Fonts", "/Library/Fonts"):
            p = os.path.join(d, name)
            if os.path.exists(p):
                return ImageFont.truetype(p, size)
    return ImageFont.load_default()


def glyph(name, scale, color):
    """fotogramma (nero su trasparente) ricolorato e ingrandito a pixel"""
    im = Image.open(os.path.join(SRC, name + ".png")).convert("RGBA")
    im = im.resize((im.width * scale, im.height * scale), Image.NEAREST)
    out = Image.new("RGBA", im.size, color + (0,))
    out.putalpha(im.getchannel("A"))
    solid = Image.new("RGBA", im.size, color + (255,))
    return Image.composite(solid, out, im.getchannel("A"))


def menubar(draw, img, w, t, frame, x_dino):
    """barra dei menu stile macOS, alta 48 px (2x)"""
    draw.rectangle([0, 0, w, 48], fill=t["bar"])
    f = font(26, True)
    draw.text((28, 9), "Finder", font=f, fill=t["ink"])
    f2 = font(26)
    x = 140
    for m in ["Archivio", "Composizione", "Vista"]:
        draw.text((x, 9), m, font=f2, fill=t["ink"])
        x += draw.textlength(m, font=f2) + 34
    # dinosauro (2x), evidenziato come un'icona attiva
    g = glyph(frame, 2, t["ink"])
    img.alpha_composite(g, (x_dino, 6))
    # wifi, batteria, orologio
    cx = x_dino + 76
    draw.arc([cx, 12, cx + 34, 46], 225, 315, fill=t["ink"], width=4)
    draw.arc([cx + 8, 20, cx + 26, 38], 225, 315, fill=t["ink"], width=4)
    draw.ellipse([cx + 14, 31, cx + 20, 37], fill=t["ink"])
    bx = cx + 58
    draw.rounded_rectangle([bx, 15, bx + 44, 35], radius=6, outline=t["ink"], width=3)
    draw.rounded_rectangle([bx + 5, 20, bx + 32, 30], radius=2, fill=t["ink"])
    draw.rectangle([bx + 46, 21, bx + 49, 29], fill=t["ink"])
    clock = "gio 15 ott  18:20"
    draw.text((w - 28 - draw.textlength(clock, font=f2), 9), clock, font=f2, fill=t["ink"])


def save(frames, durations, path):
    pal = [f.convert("RGB").quantize(colors=64, method=Image.Quantize.MEDIANCUT) for f in frames]
    pal[0].save(path, save_all=True, append_images=pal[1:], duration=durations, loop=0, optimize=True, disposal=2)


def hero(theme):
    t = THEMES[theme]
    W, H = 960, 300
    seq = [("sleep", 3), ("run", 5), ("sleep", 1), ("alert", 1)]
    frames, durations = [], []
    for state, repeat in seq:
        names, ms, title, sub = STATES[state]
        hold = 1800 if state == "alert" else ms
        for _ in range(repeat):
            for n in names:
                img = Image.new("RGBA", (W, H), t["bg"] + (255,))
                d = ImageDraw.Draw(img)
                menubar(d, img, W, t, n, 560)
                # scheda con il dinosauro grande e lo stato
                d.rounded_rectangle([40, 84, W - 40, H - 32], radius=22, fill=t["card"], outline=t["line"], width=2)
                g = glyph(n, 7, t["ink"])
                img.alpha_composite(g, (90, 84 + (H - 116 - g.height) // 2))
                d.text((330, 128), title, font=font(46, True), fill=t["ink"])
                d.text((330, 192), sub, font=font(28), fill=t["muted"])
                frames.append(img)
                durations.append(hold)
    save(frames, durations, os.path.join(OUT, f"hero-{theme}.gif"))


def single(state, theme):
    t = THEMES[theme]
    names, ms, _, _ = STATES[state]
    frames = []
    for n in names:
        img = Image.new("RGBA", (220, 170), t["bg"] + (255,))
        d = ImageDraw.Draw(img)
        d.rounded_rectangle([6, 6, 214, 164], radius=24, fill=t["card"], outline=t["line"], width=2)
        g = glyph(n, 6, t["ink"])
        img.alpha_composite(g, ((220 - g.width) // 2, (170 - g.height) // 2))
        frames.append(img)
    if len(frames) == 1:
        frames[0].convert("RGB").save(os.path.join(OUT, f"{state}-{theme}.gif"))
        return
    save(frames, [ms] * len(frames), os.path.join(OUT, f"{state}-{theme}.gif"))


for theme in THEMES:
    hero(theme)
    for state in STATES:
        single(state, theme)
print("ok:", ", ".join(sorted(os.listdir(OUT))))
