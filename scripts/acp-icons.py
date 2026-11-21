#!/usr/bin/env python3
"""Refresh the external agent logos in the app from the ACP registry.

Each registry agent's icon becomes an image named agent-<id>, which task rows show for
work running on that agent. Where Lobe Icons (MIT) has the brand's coloured logo, that is
used as is; otherwise the registry's monochrome icon is a template that takes the text
colour. Agents that left the registry lose their logo.
"""
import json
import pathlib
import re
import shutil
import sys
import urllib.request

REGISTRY = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"
COLOURED = "https://unpkg.com/@lobehub/icons-static-svg@latest/icons/{}-color.svg"
# Registry IDs whose brand has a coloured Lobe Icons logo that renders in an asset catalog:
# no currentColor, masks or filters.
BRANDS = {
    "amp-acp": "amp", "claude-acp": "claude", "codebuddy-code": "codebuddy", "codex-acp": "codex",
    "cortex-code": "snowflake", "deepagents": "langchain", "devin": "devin", "gemini": "geminicli",
    "github-copilot-cli": "copilot", "glm-acp-agent": "zhipu", "junie": "junie", "kiro": "kiro",
    "minimax-code": "minimax", "mistral-vibe": "mistral", "qwen-code": "qwen",
}
ASSETS = pathlib.Path(__file__).resolve().parent.parent / "apps/Polymux/Sources/Assets.xcassets"
CONTENTS = {
    "images": [{"filename": "", "idiom": "universal"}],
    "info": {"author": "xcode", "version": 1},
    "properties": {"preserves-vector-representation": True, "template-rendering-intent": "template"},
}


def fetch(url: str) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "polymux-acp-icons"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read()


def sized(svg: bytes) -> bytes:
    """Gives a 1em web icon the pixel size of its viewBox, which asset catalogs need."""
    text = svg.decode()
    box = re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', text)
    if box:
        text = re.sub(r'\s(width|height)="1em"', "", text, count=2)
        text = text.replace("<svg ", f'<svg width="{box[1]}" height="{box[2]}" ', 1)
    text = re.sub(r'\sstyle="[^"]*"', "", text, count=1)
    return text.encode()


def main() -> int:
    agents = json.loads(fetch(REGISTRY))["agents"]
    kept = set()
    for agent in agents:
        agent_id, icon = agent["id"], agent.get("icon")
        if not icon or not re.fullmatch(r"[a-z0-9-]+", agent_id):
            continue
        brand = BRANDS.get(agent_id)
        svg = sized(fetch(COLOURED.format(brand))) if brand else fetch(icon)
        if b"<svg" not in svg[:512]:
            print(f"skip {agent_id}: icon is not an SVG", file=sys.stderr)
            continue
        folder = ASSETS / f"agent-{agent_id}.imageset"
        folder.mkdir(exist_ok=True)
        (folder / f"{agent_id}.svg").write_bytes(svg)
        contents = json.loads(json.dumps(CONTENTS))
        contents["images"][0]["filename"] = f"{agent_id}.svg"
        if brand:
            contents["properties"]["template-rendering-intent"] = "original"
        (folder / "Contents.json").write_text(json.dumps(contents, indent=2) + "\n")
        kept.add(folder.name)
    for folder in ASSETS.glob("agent-*.imageset"):
        if folder.name not in kept:
            shutil.rmtree(folder)
    print(f"{len(kept)} agent logos in {ASSETS.relative_to(ASSETS.parent.parent.parent.parent)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
