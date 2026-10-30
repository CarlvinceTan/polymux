#!/usr/bin/env python3
"""Encode Cua native captures without changing their pixels."""

import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path

from PIL import Image


def digest(data):
    return hashlib.sha256(data).hexdigest()


def focused(image, probe):
    x, y, width, height = (probe[key] for key in ("x", "y", "width", "height"))
    if min(x, y) < 0 or min(width, height) <= 0 or x + width > image.width or y + height > image.height:
        return False
    pixels = image.convert("RGB").crop((x, y, x + width, y + height)).get_flattened_data()
    return any(red > green + 60 and red > blue + 60 for red, green, blue in pixels)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("captures", type=Path, help="Run directory containing native-<id>.png and native-geometry.json")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    fixture = root / "src/content/demo.json"
    examples = json.loads(fixture.read_text())
    manifest = json.loads((args.captures / "native-geometry.json").read_text())
    if manifest.get("windowState") != "focused" or not isinstance(manifest.get("focusProbe"), dict):
        raise SystemExit("Capture the focused native window and record its close-control focusProbe bounds.")
    output = root / "src/assets/app-demo"
    output.mkdir(parents=True, exist_ok=True)
    manifest.update({
        "source": "cua-driver.get_window_state",
        "encoding": "lossless-webp",
        "fixtureSha256": digest(fixture.read_bytes()),
        "capturedAt": datetime.now(timezone.utc).isoformat(),
        "captures": {},
    })
    for example in examples:
        name = example["id"]
        source = args.captures / f"native-{name}.png"
        target = output / f"{name}.webp"
        with Image.open(source) as image:
            if image.size != (manifest["width"], manifest["height"]):
                raise SystemExit(f"{name}: capture dimensions differ from native geometry")
            if not focused(image, manifest["focusProbe"]):
                raise SystemExit(f"{name}: inactive window capture; native close-control pixels must show the focused colour")
            pixels = image.convert("RGBA").tobytes()
            options = {"icc_profile": image.info["icc_profile"]} if "icc_profile" in image.info else {}
            image.save(target, lossless=True, quality=100, method=6, exact=True, **options)
        with Image.open(target) as encoded:
            if encoded.convert("RGBA").tobytes() != pixels:
                raise SystemExit(f"{name}: encoding changed native pixels")
        manifest["captures"][name] = {
            "file": target.name,
            "sha256": digest(target.read_bytes()),
            "nativeSha256": digest(source.read_bytes()),
            "pixelsSha256": digest(pixels),
        }
        print(f"{name}: native pixels unchanged; {target.stat().st_size:,} bytes")
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
