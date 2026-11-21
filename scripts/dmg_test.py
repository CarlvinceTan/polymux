"""Check the installer window layout Finder reads from the disk image."""

import plistlib
import struct
import tempfile
import unittest
from pathlib import Path
from runpy import run_path

ROOT = Path(__file__).resolve().parent.parent
LAYOUT = run_path(str(ROOT / "scripts/lib/dmg-layout.py"))
ART = ROOT / "apps/Polymux/Installer/Background.tiff"
VOLUME = "Polymux 0.4.0"


def written():
    with tempfile.TemporaryDirectory() as folder:
        path = Path(folder) / ".DS_Store"
        LAYOUT["main"](["dmg-layout.py", str(path), VOLUME])
        return path.read_bytes()


def allocator(data):
    """Block addresses and free lists from the buddy allocator's root block."""
    assert data[:8] == b"\0\0\0\1Bud1"
    offset, size = struct.unpack_from(">II", data, 8)
    root = data[4 + offset:4 + offset + size]
    count = struct.unpack_from(">I", root)[0]
    blocks = list(struct.unpack_from(f">{count}I", root, 8))
    at = 8 + 4 * ((count + 255) // 256 * 256)
    toc = {}
    for _ in range(struct.unpack_from(">I", root, at)[0]):
        length = root[at + 4]
        toc[root[at + 5:at + 5 + length]] = struct.unpack_from(">I", root, at + 5 + length)[0]
        at += 5 + length
    at += 4
    free = []
    for _ in range(32):
        n = struct.unpack_from(">I", root, at)[0]
        free.append(list(struct.unpack_from(f">{n}I", root, at + 4)))
        at += 4 + 4 * n
    return blocks, toc, free


def entries(data):
    """(name, code) -> raw value, in file order, from the single leaf node."""
    blocks, toc, _ = allocator(data)
    block = lambda number: 4 + (blocks[number] & ~31)
    tree, levels, total, nodes, _ = struct.unpack_from(">IIIII", data, block(toc[b"DSDB"]))
    assert (levels, nodes) == (0, 1)
    at = block(tree)
    kind, count = struct.unpack_from(">II", data, at)
    assert kind == 0 and count == total
    at += 8
    found = []
    for _ in range(count):
        length = struct.unpack_from(">I", data, at)[0]
        name = data[at + 4:at + 4 + 2 * length].decode("utf-16-be")
        at += 4 + 2 * length
        code, kind = data[at:at + 4], data[at + 4:at + 8]
        at += 8
        if kind == b"blob":
            size = struct.unpack_from(">I", data, at)[0]
            value, at = data[at + 4:at + 4 + size], at + 4 + size
        elif kind in (b"long", b"type"):
            value, at = data[at:at + 4], at + 4
        else:
            raise AssertionError(f"unexpected type {kind}")
        found.append(((name, code), value))
    return found


def tiff_sizes(data):
    """Width and height of every image in a big- or little-endian TIFF."""
    order = {b"MM": ">", b"II": "<"}[data[:2]]
    sizes, ifd = [], struct.unpack_from(order + "I", data, 4)[0]
    while ifd:
        count = struct.unpack_from(order + "H", data, ifd)[0]
        tags = {}
        for i in range(count):
            tag, kind, _, value = struct.unpack_from(order + "HHI4s", data, ifd + 2 + 12 * i)
            tags[tag] = struct.unpack_from(order + ("H" if kind == 3 else "I"), value)[0]
        sizes.append((tags[256], tags[257]))
        ifd = struct.unpack_from(order + "I", data, ifd + 2 + 12 * count)[0]
    return sizes


class LayoutTests(unittest.TestCase):
    def test_finder_finds_window_icons_and_background(self):
        found = entries(written())
        keys = [key for key, _ in found]
        self.assertEqual(keys, sorted(keys, key=lambda key: (key[0].lower(), key[1])))
        values = dict(found)
        for name, point in LAYOUT["ICONS"].items():
            self.assertEqual(struct.unpack(">II", values[name, b"Iloc"][:8]), point)
        window = plistlib.loads(values[".", b"bwsp"])
        width, height = LAYOUT["WIDTH"], LAYOUT["HEIGHT"] + LAYOUT["TITLE_BAR"]
        self.assertTrue(window["WindowBounds"].endswith(f"{{{width}, {height}}}}}"))
        self.assertFalse(window["ShowToolbar"] or window["ShowSidebar"])
        view = plistlib.loads(values[".", b"icvp"])
        self.assertEqual((view["backgroundType"], view["arrangeBy"]), (2, "none"))
        alias = view["backgroundImageAlias"]
        self.assertEqual(struct.unpack_from(">H", alias, 4)[0], len(alias))
        self.assertIn(b"/" + LAYOUT["BACKGROUND"].encode(), alias)
        self.assertIn(b"/Volumes/" + VOLUME.encode(), alias)

    def test_allocator_accounts_for_every_byte_once(self):
        blocks, _, free = allocator(written())
        spans = [(0, 32)] + [(b & ~31, 1 << (b & 31)) for b in blocks]
        spans += [(offset, 1 << size) for size, offsets in enumerate(free) for offset in offsets]
        end = 0
        for offset, size in sorted(spans):
            self.assertEqual(offset, end)
            self.assertEqual(offset % size, 0)
            end = offset + size
        self.assertEqual(end, 1 << 31)

    def test_background_matches_the_window(self):
        width, height = LAYOUT["WIDTH"], LAYOUT["HEIGHT"]
        self.assertEqual(tiff_sizes(ART.read_bytes()), [(width, height), (2 * width, 2 * height)])
        build = (ROOT / "scripts/build.sh").read_text()
        self.assertIn(f'cp "$ROOT/apps/Polymux/Installer/{Path(LAYOUT["BACKGROUND"]).name}" "$STAGE/.background/"', build)


if __name__ == "__main__":
    unittest.main()
