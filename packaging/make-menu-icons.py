#!/usr/bin/env python3
"""Genera le icone della barra dei menu (packaging/menubar/*.png, serve Pillow).

Pixel art 24x18, nero su trasparente: macOS le usa come immagini "template"
e le colora come le icone di sistema (scure su barra chiara, chiare su barra
scura). Per ogni fotogramma: nome.png (1x) e nome@2x.png.

  sleep-0..2  riposo: dorme, le Z salgono
  run-0..3    sincronizza: corre
  alert       problema: in piedi con il punto esclamativo
Anteprima ingrandita: packaging/menubar/preview.png
"""
import os
from PIL import Image

W, H = 24, 18

# --- corpo in piedi (righe 0-11), testa a destra ---
STAND = """
................XXX.....
...............XXXXXXX..
...............X.XX.....
................XX......
................XX......
...............XX.......
..............XX........
.......XXXXXXXXX........
XX..XXXXXXXXXXXX........
.XXXXXXXXXXXXXXXX.......
....XXXXXXXXXXX.XX......
........XXXXXX..........
"""

# --- gambe (righe 12-17): falcata ---
LEGS = [
"""
........XX..XX..........
.......XX....XX.........
......XX......XX........
.....XX........XX.......
....XX..........X.......
...XXX..........XXX.....
""",
"""
.........XX.XX..........
.........XX..XX.........
........XX....XX........
........XX.....X........
.......XX......XX.......
......XXX.......XX......
""",
"""
.........XXXX...........
..........XXX...........
..........XX.X..........
.........XX..XX.........
.........X....XX........
........XX.....XXX......
""",
"""
.........XX.XX..........
........XX...XX.........
.......XX.....XX........
.......X.......XX.......
......XX........X.......
.....XXX........XXX.....
""",
]

# --- sdraiato che dorme (righe 9-17), testa appoggiata a destra ---
SLEEP_BODY = """
........................
........................
.................XXX....
........XXXXX...XXXXXXX.
.....XXXXXXXXX.XXXX.....
XX.XXXXXXXXXXXXXXX......
.XXXXXXXXXXXXXXXX.......
...XXXXXXXXXXXXX........
.....XXX..XXX.XX........
"""

Z_SMALL = ["XXX", "..X", ".X.", "XXX"]
Z_BIG = ["XXXX", "...X", "..X.", ".X..", "XXXX"]

BANG = ["XX", "XX", "XX", "XX", "XX", "XX", "..", "XX", "XX"]


def grid():
    return [[0] * W for _ in range(H)]


def paste(g, art, x0, y0):
    rows = art if isinstance(art, list) else art.strip("\n").split("\n")
    for y, r in enumerate(rows):
        for x, c in enumerate(r):
            if c == "X" and 0 <= y0 + y < H and 0 <= x0 + x < W:
                g[y0 + y][x0 + x] = 1


def standing(legs, bob=0):
    g = grid()
    paste(g, STAND, 0, bob)
    paste(g, legs, 0, 12)
    return g


def frames():
    out = {}
    # corsa: il corpo sobbalza di un pixel a fotogrammi alterni
    for i, legs in enumerate(LEGS):
        out[f"run-{i}"] = standing(legs, bob=(1 if i % 2 else 0))
    # riposo: z piccola e Z grande che salgono
    zpos = [((15, 6), (19, 1)), ((15, 5), (19, 0)), ((16, 4), (20, 0))]
    for i, ((sx, sy), (bx, by)) in enumerate(zpos):
        g = grid()
        paste(g, SLEEP_BODY, 0, 9)
        paste(g, Z_SMALL, sx, sy)
        if i > 0:
            paste(g, Z_BIG, bx, by)
        out[f"sleep-{i}"] = g
    # problema: fermo, con il punto esclamativo a destra
    g = standing(LEGS[2])
    # il becco occupa le colonne 18-22 in alto: "!" più in basso
    paste(g, BANG, 21, 6)
    out["alert"] = g
    return out


def to_image(g, scale):
    im = Image.new("RGBA", (W * scale, H * scale), (0, 0, 0, 0))
    px = im.load()
    for y in range(H):
        for x in range(W):
            if g[y][x]:
                for dy in range(scale):
                    for dx in range(scale):
                        px[x * scale + dx, y * scale + dy] = (0, 0, 0, 255)
    return im


here = os.path.dirname(os.path.abspath(__file__))
outdir = os.path.join(here, "menubar")
os.makedirs(outdir, exist_ok=True)
fr = frames()
for name, g in fr.items():
    to_image(g, 1).save(os.path.join(outdir, f"{name}.png"))
    to_image(g, 2).save(os.path.join(outdir, f"{name}@2x.png"))

# anteprima: tutti i fotogrammi su barra chiara e scura, ingranditi
names = list(fr)
S = 6
prev = Image.new("RGBA", ((W * S + 12) * len(names) + 12, (H * S + 12) * 2 + 12), (0, 0, 0, 0))
for row, (bg, fg) in enumerate([((236, 236, 236, 255), (30, 30, 30, 255)), ((40, 40, 44, 255), (235, 235, 235, 255))]):
    band = Image.new("RGBA", (prev.width, H * S + 12), bg)
    prev.paste(band, (0, 12 + row * (H * S + 12)))
    for i, n in enumerate(names):
        im = to_image(fr[n], S)
        col = Image.new("RGBA", im.size, fg)
        prev.paste(col, (12 + i * (W * S + 12), 18 + row * (H * S + 12)), im)
prev.save(os.path.join(outdir, "preview.png"))
print("ok:", ", ".join(names))
