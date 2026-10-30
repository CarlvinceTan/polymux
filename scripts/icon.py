#!/usr/bin/env -S uv run --script
# /// script
# dependencies = ["pillow"]
# ///
"""Export app and website icons from the approved Assets/Logo.png master."""

import hashlib
from pathlib import Path
import re

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "apps/Polymux/Assets"
CATALOG = ROOT / "apps/Polymux/Sources/Assets.xcassets"
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
    with Image.open(ASSETS / "Logo.png") as source:
        source = source.convert("L").point(lambda value: 0 if value <= 4 else value)
        bounds = source.point(lambda value: 255 if value > 64 else 0).getbbox()
        if bounds is None:
            raise ValueError("Logo.png has no visible mark")
        left, top, right, bottom = bounds
        mark = source.crop((max(0, left - 2), max(0, top - 2),
                            min(source.width, right + 2), min(source.height, bottom + 2)))
        mark.thumbnail((704, 704), Image.Resampling.LANCZOS)
    image = Image.new("RGB", (1024, 1024), "black")
    image.paste(mark, ((1024 - mark.width) // 2, (1024 - mark.height) // 2))
    image.save(ASSETS / "AppIcon.png")
    layer = Image.new("RGBA", image.size, "white")
    layer.putalpha(image.convert("L"))
    layer.save(CATALOG.parent / "AppIcon.icon/Assets/P.png")

    logo = layer.crop(layer.getchannel("A").getbbox())
    logo.thumbnail((288, 288), Image.Resampling.LANCZOS)
    logo.save(CATALOG / "Logo.imageset/Logo.png")
    rounded(image).resize((32, 32), Image.Resampling.LANCZOS).save(SITE / "public/favicon-32.png")
    image.resize((256, 256), Image.Resampling.LANCZOS).save(
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
    print("Exported app icons, connecting logo, website logo, and favicon.")


if __name__ == "__main__":
    main()
