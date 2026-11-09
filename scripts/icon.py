#!/usr/bin/env -S uv run --script
# /// script
# dependencies = ["pillow"]
# ///
"""Export app and website icons from the approved Assets/AppIcon.png art and its Assets/Figure.png mask."""

import hashlib
from pathlib import Path
import re

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "apps/Polymux/Assets"
SOURCES = ROOT / "apps/Polymux/Sources"
CATALOG = SOURCES / "Assets.xcassets"
SITE = ROOT / "apps/site"


def rounded(image):
    size = image.width
    mask = Image.new("L", (size * 4, size * 4))
    ImageDraw.Draw(mask).rounded_rectangle(
        (0, 0, size * 4 - 1, size * 4 - 1), radius=size * 4 * 104 / 412, fill=255,
    )
    result = image.convert("RGBA")
    result.putalpha(mask.resize(image.size, Image.Resampling.LANCZOS))
    return result


def main():
    art = Image.open(ASSETS / "AppIcon.png").convert("RGB")
    mask = Image.open(ASSETS / "Figure.png").convert("L")
    if art.size != (1024, 1024) or mask.size != art.size:
        raise ValueError("AppIcon.png and Figure.png must be 1024×1024")

    # AppIcon.icon: the art is the flat back layer; the sable and snow cut from it are the glass front layer.
    art.save(SOURCES / "AppIcon.icon/Assets/Art.png")
    figure = art.convert("RGBA")
    figure.putalpha(mask)
    figure.save(SOURCES / "AppIcon.icon/Assets/Figure.png")

    logo = figure.crop(mask.getbbox())
    logo.thumbnail((288, 288), Image.Resampling.LANCZOS)
    logo.save(CATALOG / "Logo.imageset/Logo.png")
    rounded(art).resize((32, 32), Image.Resampling.LANCZOS).save(SITE / "public/favicon-32.png")
    art.resize((256, 256), Image.Resampling.LANCZOS).save(
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
