#!/usr/bin/env python3
"""Generate the picker catalogue from Unicode 17.0 keyboard data."""

import json
from pathlib import Path
import re
import urllib.request

SOURCE = "https://www.unicode.org/Public/17.0.0/emoji/emoji-test.txt"
ICONS = {
    "Smileys & Emotion": "face.smiling",
    "People & Body": "hand.wave",
    "Animals & Nature": "leaf",
    "Food & Drink": "fork.knife",
    "Travel & Places": "car",
    "Activities": "sportscourt",
    "Objects": "lightbulb",
    "Symbols": "heart",
    "Flags": "flag",
}


def catalogue(text):
    groups = []
    group = None
    for line in text.splitlines():
        if line.startswith("# group: "):
            name = line.removeprefix("# group: ")
            group = None
            if name in ICONS:
                group = {"name": name, "icon": ICONS[name], "emoji": []}
                groups.append(group)
            continue
        match = re.match(r"^([0-9A-F ]+)\s*; fully-qualified\s*# \S+ E[0-9.]+ (.+)$", line)
        if match and group is not None:
            points = [int(value, 16) for value in match[1].split()]
            group["emoji"].append({
                "emoji": "".join(map(chr, points)),
                "name": match[2],
                "tone": any(0x1F3FB <= point <= 0x1F3FF for point in points),
            })
    if len(groups) != len(ICONS) or sum(len(group["emoji"]) for group in groups) != 3944:
        raise ValueError("Unexpected Unicode keyboard catalogue")
    return groups


if __name__ == "__main__":
    text = urllib.request.urlopen(SOURCE, timeout=30).read().decode("utf-8")
    target = Path(__file__).resolve().parents[1] / "apps/Polymux/Sources/Resources/Emoji.json"
    target.write_text(json.dumps(catalogue(text), ensure_ascii=False, separators=(",", ":")) + "\n")
    print("Generated 3944 emoji in 9 categories")
