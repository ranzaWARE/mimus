#!/usr/bin/env python3
"""Immagini dell'app per Linux in src/app/linuxfiles (serve Pillow).

  tray/*.png      icona nel vassoio: il dinosauro della barra dei menu del Mac
                  (packaging/menubar/*@2x.png, nero) in bianco con il bordo
                  scuro, leggibile su pannelli chiari e scuri
  emblems/*.svg   pallini nel file manager, come quelli del Finder
  mimus.png       icona dell'app (256 px)
"""
import math
import os
from PIL import Image, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "src", "app", "linuxfiles")
MENU = os.path.join(ROOT, "packaging", "menubar")
FRAMES = ["sleep-0", "sleep-1", "sleep-2", "run-0", "run-1", "run-2", "run-3", "alert"]


def tray():
    os.makedirs(os.path.join(OUT, "tray"), exist_ok=True)
    for name in FRAMES:
        src = Image.open(os.path.join(MENU, name + "@2x.png")).convert("RGBA")
        alpha = src.getchannel("A")
        outline = alpha.filter(ImageFilter.MaxFilter(5))
        size = max(src.size) + 8
        img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
        ox, oy = (size - src.width) // 2, (size - src.height) // 2
        dark = Image.new("RGBA", src.size, (40, 40, 40, 255))
        white = Image.new("RGBA", src.size, (250, 250, 250, 255))
        img.paste(dark, (ox, oy), outline)
        img.paste(white, (ox, oy), alpha)
        img.save(os.path.join(OUT, "tray", name + ".png"))


def svg(body):
    return ('<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">'
            + body + "</svg>\n")


def emblems():
    d = os.path.join(OUT, "emblems")
    os.makedirs(d, exist_ok=True)
    green, orange, grey, blue = "#30b350", "#f08c00", "#8e8e93", "#0a84ff"
    halo = '<circle cx="16" cy="16" r="14" fill="#ffffff"/>'
    files = {
        "remote": halo + f'<circle cx="16" cy="16" r="11.5" fill="none" stroke="{grey}" stroke-width="3"/>',
        "local": halo + f'<circle cx="16" cy="16" r="12" fill="{green}"/>',
        "pinned": halo + f'<circle cx="16" cy="16" r="12" fill="{green}"/>'
                  '<path d="M10 16.5l4 4 8-9" fill="none" stroke="#fff" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round"/>',
        "upload": halo + f'<circle cx="16" cy="16" r="12" fill="{orange}"/>'
                  '<path d="M16 23V10M10.5 15.5L16 10l5.5 5.5" fill="none" stroke="#fff" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round"/>',
    }
    steps = 8
    for i in range(steps):
        frac = i / steps
        pie = ""
        if frac > 0:
            a = 2 * math.pi * frac
            x, y = 16 + 8.5 * math.sin(a), 16 - 8.5 * math.cos(a)
            large = 1 if frac > 0.5 else 0
            pie = f'<path d="M16 16V7.5A8.5 8.5 0 0 1 {x:.2f} {y:.2f}Z" fill="{blue}"/>' if large == 0 else \
                f'<path d="M16 16V7.5A8.5 8.5 0 1 1 {x:.2f} {y:.2f}Z" fill="{blue}"/>'
        files[f"dl{i}"] = halo + f'<circle cx="16" cy="16" r="11.5" fill="none" stroke="{blue}" stroke-width="3"/>' + pie
    for name, body in files.items():
        with open(os.path.join(d, f"emblem-mimus-{name}.svg"), "w") as f:
            f.write(svg(body))


def app_icon():
    Image.open(os.path.join(ROOT, "packaging", "AppIcon.png")).convert("RGBA").resize((256, 256), Image.LANCZOS) \
        .save(os.path.join(OUT, "mimus.png"))


if __name__ == "__main__":
    tray()
    emblems()
    app_icon()
    print("immagini per Linux in", OUT)
