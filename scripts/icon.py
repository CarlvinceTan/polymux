#!/usr/bin/env -S uv run --script
# /// script
# dependencies = ["pillow"]
# ///
"""Export app and website icons from the approved Assets/Head.png and Assets/Snow.png masks."""

import hashlib
from pathlib import Path
import re

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "apps/Polymux/Assets"
SOURCES = ROOT / "apps/Polymux/Sources"
CATALOG = SOURCES / "Assets.xcassets"
SITE = ROOT / "apps/site"
# Matches the app's connecting screen background in dark mode.
APP_BG = (30, 30, 30)
# Layer colours on AppIcon.icon's dark fill.
COLOURS = {"Head": (255, 255, 255), "Snow": (200, 222, 242)}


def rounded(image):
    size = image.width
    mask = Image.new("L", (size * 4, size * 4))
    ImageDraw.Draw(mask).rounded_rectangle(
        (0, 0, size * 4 - 1, size * 4 - 1), radius=size * 4 * 104 / 412, fill=255,
    )
    result = image.convert("RGBA")
    result.putalpha(mask.resize(image.size, Image.Resampling.LANCZOS))
    return result


def layer(mask, colour):
    image = Image.new("RGBA", mask.size, colour + (255,))
    image.putalpha(mask)
    return image


def main():
    masks = {name: Image.open(ASSETS / f"{name}.png").convert("L") for name in COLOURS}
    if any(mask.size != (1024, 1024) for mask in masks.values()):
        raise ValueError("Head.png and Snow.png must be 1024×1024")
    layers = {name: layer(mask, COLOURS[name]) for name, mask in masks.items()}
    for name, image in layers.items():
        image.save(SOURCES / f"AppIcon.icon/Assets/{name}.png")

    # Flat stand-ins for the README, website and connecting logo; the snow sits in front.
    mark = Image.new("RGBA", (1024, 1024))
    for name in ("Head", "Snow"):
        mark.alpha_composite(layers[name])
    app = Image.new("RGBA", mark.size, APP_BG + (255,))
    app.alpha_composite(mark)
    app = app.convert("RGB")
    app.save(ASSETS / "AppIcon.png")

    logo = mark.crop(mark.getchannel("A").getbbox())
    logo.thumbnail((288, 288), Image.Resampling.LANCZOS)
    logo.save(CATALOG / "Logo.imageset/Logo.png")
    rounded(app).resize((32, 32), Image.Resampling.LANCZOS).save(SITE / "public/favicon-32.png")
    app.resize((256, 256), Image.Resampling.LANCZOS).save(
        SITE / "src/assets/app-icon.webp", lossless=True,
    )
    version = hashlib.sha256((SITE / "public/favicon-32.png").read_bytes()).hexdigest()[:10]
    routes = SITE / "scripts/routes.ts"
    content, count = re.subn(
        r'href="/favicon-32\.png(?:\?v=[^"]*)?"',
        f'href="/favicon-32.png?v={version}"', routes.read_text(),
    )
    if count != 1:
        raise ValueError("Expected one favicon link in routes.ts")
    routes.write_text(content)
    print("Exported app icon layers, connecting logo, website logo, and favicon.")


if __name__ == "__main__":
    main()
