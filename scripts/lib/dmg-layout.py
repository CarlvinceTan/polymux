"""Write the installer window's .DS_Store without Finder.

Usage: dmg-layout.py OUT VOLUME

Finder reads the window size, icon positions and background picture from this
file when the image is opened. apps/Polymux/Installer/background.swift draws the
picture to the same geometry.
Format: https://metacpan.org/dist/Mac-Finder-DSStore/view/DSStoreFormat.pod
"""

import plistlib
import struct
import sys

WIDTH, HEIGHT = 600, 360  # the picture, in points
TITLE_BAR = 32  # Finder counts it in the window height
LEFT, TOP = 200, 120  # where the window opens on screen
BACKGROUND = ".background/Background.tiff"
ICONS = {"Polymux.app": (160, 150), "Applications": (440, 150)}  # centres
HIDDEN = {".background": (160, 520)}  # out of view when hidden files are shown
PAPER = (0.973, 0.969, 0.992)  # the picture's colour, for any edge it leaves
ICON_SIZE = 128
TEXT_SIZE = 13


def utf16(text):
    data = text.encode("utf-16-be")
    return struct.pack(">I", len(data) // 2) + data


def record(name, code, kind, data):
    return (name.lower(), code), utf16(name) + code + kind + data


def blob(data):
    return struct.pack(">I", len(data)) + data


def pascal(text, size):
    data = text.encode("mac_roman")[: size - 1]
    return bytes([len(data)]) + data.ljust(size - 1, b"\0")


def tag(number, data):
    return struct.pack(">hH", number, len(data)) + data + (b"\0" if len(data) % 2 else b"")


def alias(volume, path):
    """A classic alias record that Finder resolves by volume name and path."""
    folder, _, name = path.rpartition("/")
    fixed = struct.pack(">h", 0) + pascal(volume, 28) + struct.pack(">I2shI", 0, b"H+", 5, 0)
    fixed += pascal(name, 64) + struct.pack(">II4s4shhI2s10s", 0, 0, b"\0" * 4, b"\0" * 4, -1, -1, 0, b"\0\0", b"\0" * 10)
    extra = b"".join([
        tag(0, folder.rpartition("/")[2].encode()),
        tag(2, ":".join([volume, *path.split("/")]).encode("mac_roman")),
        tag(14, utf16(name)[2:]),  # u16 length, not u32
        tag(15, utf16(volume)[2:]),
        tag(18, ("/" + path).encode()),
        tag(19, ("/Volumes/" + volume).encode()),
        struct.pack(">hH", -1, 0),
    ])
    body = fixed + extra
    return struct.pack(">4sHH", b"\0" * 4, 8 + len(body), 2) + body


def records(volume):
    window = plistlib.dumps({
        "ContainerShowSidebar": False,
        "ShowPathbar": False,
        "ShowSidebar": False,
        "ShowStatusBar": False,
        "ShowTabView": False,
        "ShowToolbar": False,
        "SidebarWidth": 0,
        "WindowBounds": f"{{{{{LEFT}, {TOP}}}, {{{WIDTH}, {HEIGHT + TITLE_BAR}}}}}",
    }, fmt=plistlib.FMT_BINARY, sort_keys=True)
    view = plistlib.dumps({
        "arrangeBy": "none",
        "backgroundColorBlue": PAPER[2],
        "backgroundColorGreen": PAPER[1],
        "backgroundColorRed": PAPER[0],
        "backgroundImageAlias": alias(volume, BACKGROUND),
        "backgroundType": 2,
        "gridOffsetX": 0.0,
        "gridOffsetY": 0.0,
        "gridSpacing": 100.0,
        "iconSize": float(ICON_SIZE),
        "labelOnBottom": True,
        "showIconPreview": True,
        "showItemInfo": False,
        "textSize": float(TEXT_SIZE),
        "viewOptionsVersion": 1,
    }, fmt=plistlib.FMT_BINARY, sort_keys=True)
    items = [
        record(".", b"bwsp", b"blob", blob(window)),
        record(".", b"icvp", b"blob", blob(view)),
        record(".", b"vSrn", b"long", struct.pack(">I", 1)),
        record(".", b"vstl", b"type", b"icnv"),
    ]
    for name, (x, y) in {**ICONS, **HIDDEN}.items():
        items.append(record(name, b"Iloc", b"blob", blob(struct.pack(">IIII", x, y, 0xFFFFFFFF, 0xFFFF0000))))
    # Finder looks records up by name (case-insensitively), then by code.
    return [data for _, data in sorted(items)]


def store(items):
    """One leaf node in a buddy-allocated file: header, DSDB, root block and node."""
    node = struct.pack(">II", 0, len(items)) + b"".join(items)
    assert len(node) <= 4096, "too many records for one node"
    # Blocks 0-2 as (offset, log2 size), offsets relative to byte 4.
    blocks = [(2048, 11), (32, 5), (4096, 12)]  # this root, DSDB, the node
    root = struct.pack(">II", len(blocks), 0) + b"".join(struct.pack(">I", o | s) for o, s in blocks)
    root += b"\0" * 4 * (256 - len(blocks))
    root += struct.pack(">IB4sI", 1, 4, b"DSDB", 1)
    free = [[] for _ in range(32)]
    split({(0, 5), *blocks}, 0, 31, free)  # (0, 5) is the file header
    for offsets in free:
        root += struct.pack(">I", len(offsets)) + b"".join(struct.pack(">I", o) for o in offsets)
    dsdb = struct.pack(">IIIII", 2, 0, len(items), 1, 0x1000)
    data = bytearray(8192)
    data[0:32] = struct.pack(">4sIII16s", b"Bud1", 2048, 2048, 2048, b"\0" * 16)
    data[32:32 + len(dsdb)] = dsdb
    data[2048:2048 + len(root)] = root
    data[4096:4096 + len(node)] = node
    return struct.pack(">I", 1) + bytes(data)


def split(used, offset, size, free):
    """Fill free[size] with the unused buddies of the 2 GB address space."""
    if (offset, size) in used:
        return
    end = offset + (1 << size)
    if not any(o < end and offset < o + (1 << s) for o, s in used):
        free[size].append(offset)
        return
    split(used, offset, size - 1, free)
    split(used, offset + (1 << (size - 1)), size - 1, free)


def main(argv):
    if len(argv) != 3:
        sys.exit(__doc__)
    with open(argv[1], "wb") as file:
        file.write(store(records(argv[2])))


if __name__ == "__main__":
    main(sys.argv)
