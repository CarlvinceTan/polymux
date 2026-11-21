#!/usr/bin/env python3
"""Add a plugin version to the registry index, keeping every other plugin and version.

Usage: plugins/scripts/index.py <plugin> <packages dir> <base address> --commit SHA --index index.json
The packages are the <plugin>-<version>-<target>.tgz files package.sh wrote, each with a .sha256 beside it.
"""
import argparse
import json
import re
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FIELDS = ('name', 'title', 'description', 'symbol', 'kind', 'surface')


def release(name, manifest, packages, base, repository, commit):
    artifacts = {}
    for path in sorted(Path(packages).glob(f'{name}-*.tgz')):
        target = re.fullmatch(rf"{re.escape(name)}-{re.escape(manifest['version'])}-(.+)\.tgz", path.name)
        if target:
            sha = path.with_name(path.name + '.sha256').read_text().strip()
            artifacts[target[1]] = {'url': f'{base.rstrip("/")}/{path.name}', 'sha256': sha}
    if not artifacts:
        raise SystemExit(f'No {name} packages found for version {manifest["version"]}.')
    return {'version': manifest['version'], 'publishTimestamp': int(time.time()), 'engines': manifest['engines'],
            'source': {'repository': repository, 'commit': commit, 'artifacts': artifacts}}


def merge(index, name, manifest, new):
    entries = index.setdefault('plugins', [])
    entry = next((item for item in entries if item['name'] == name), None)
    if entry is None:
        entry = {key: manifest.get(key, '') for key in FIELDS} | {'author': 'Polymux', 'versions': []}
        entries.append(entry)
        entries.sort(key=lambda item: item['name'])
    else:
        entry.update({key: manifest.get(key, '') for key in FIELDS})
    entry['homepage'] = f'https://github.com/{new["source"]["repository"]}'
    entry['versions'] = [item for item in entry['versions'] if item['version'] != new['version']] + [new]
    return index


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('plugin')
    parser.add_argument('packages')
    parser.add_argument('base', help='address the packages are served from')
    parser.add_argument('--repository', default='CarlvinceTan/polymux')
    parser.add_argument('--commit', default='')
    parser.add_argument('--index', required=True)
    options = parser.parse_args()
    manifest = json.loads((ROOT / options.plugin / 'plugin.json').read_text())
    path = Path(options.index)
    index = json.loads(path.read_text()) if path.exists() else {'format': 1, 'title': 'Polymux plugins', 'plugins': []}
    new = release(options.plugin, manifest, options.packages, options.base, options.repository, options.commit)
    path.write_text(json.dumps(merge(index, options.plugin, manifest, new), indent=2) + '\n')


if __name__ == '__main__':
    main()
