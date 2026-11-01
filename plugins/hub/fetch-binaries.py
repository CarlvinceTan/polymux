#!/usr/bin/env python3
"""Get the pinned mautrix bridges for one target.

Linux uses the published static builds, checked against GitHub's recorded SHA-256.
The published macOS builds link libolm, so macOS builds from the tag with -tags nocrypto;
Hub runs with bridge encryption off anyway.
"""
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# repository, tag, executable name, assets per target; a missing target is skipped.
BRIDGES = [
    ('whatsapp', 'v0.2610.0', 'mautrix-whatsapp'),
    ('telegram', 'v0.2610.0', 'mautrix-telegram'),
    ('signal', 'v0.2610.0', 'mautrix-signal'),
    ('slack', 'v0.2610.0', 'mautrix-slack'),
    ('meta', 'v0.2610.0', 'mautrix-meta'),
    ('meta', 'v0.2610.0', 'mautrix-instagram'),
    ('gmessages', 'v0.2610.0', 'mautrix-gmessages'),
    ('linkedin', 'v0.2609.0', 'mautrix-linkedin'),
    ('twitter', 'v0.2609.0', 'mautrix-twitter'),
    ('bluesky', 'v0.2510.0', 'mautrix-bluesky'),
    ('discord', 'v0.7.7', 'mautrix-discord'),
]

# Upstream tags no iMessage release, and it only runs on a Mac, so it's pinned to a commit. Its main package is the repository root.
IMESSAGE = ('imessage', '300ba6d0e5566d1f841d42ee1555779a9b6fa4be', 'mautrix-imessage')


def asset(name, target):
    system, arch = target.split('-')
    return f'{name}-{arch}' if system == 'linux' else f'{name}-darwin-{arch}'


# mautrix-signal links libsignal's Rust library, kept as a submodule; the flags match upstream's build-rust.sh.
def build_libsignal(folder):
    source = Path(folder) / 'pkg/libsignalgo/libsignal'
    subprocess.check_call(['cargo', 'build', '-p', 'libsignal-ffi', '--profile=release'], cwd=source,
                          env=os.environ | {'RUSTFLAGS': '-Ctarget-feature=-crt-static', 'RUSTC_WRAPPER': ''})
    return str(source / 'target/release')


def build(repository, tag, name, out):
    with tempfile.TemporaryDirectory() as folder:
        subprocess.check_call(['git', 'clone', '--quiet', '--depth', '1', '--recurse-submodules', '--shallow-submodules', '--branch', tag, f'https://github.com/mautrix/{repository}.git', folder])
        env = os.environ | ({'LIBRARY_PATH': build_libsignal(folder)} if name == 'mautrix-signal' else {})
        # mautrix-discord's main package is the repository root.
        package = '.' if name == 'mautrix-discord' else f'./cmd/{name}'
        subprocess.check_call(['go', 'build', '-tags', 'nocrypto', '-o', str((out / name).resolve()), package], cwd=folder, env=env)


def build_imessage(out):
    repository, commit, name = IMESSAGE
    with tempfile.TemporaryDirectory() as folder:
        subprocess.check_call(['git', 'init', '--quiet', folder])
        subprocess.check_call(['git', '-C', folder, 'fetch', '--quiet', '--depth', '1', f'https://github.com/mautrix/{repository}.git', commit])
        subprocess.check_call(['git', '-C', folder, 'checkout', '--quiet', 'FETCH_HEAD'])
        subprocess.check_call(['go', 'build', '-tags', 'nocrypto', '-o', str((out / name).resolve()), '.'], cwd=folder)


def fetch(repository, tag, name, target, out):
    if target.startswith('darwin'):
        build(repository, tag, name, out)
        return
    wanted = asset(name, target)
    listing = subprocess.check_output(['gh', 'release', 'view', tag, '-R', f'mautrix/{repository}', '--json', 'assets'], text=True)
    digests = {item['name']: item.get('digest', '') for item in json.loads(listing)['assets']}
    if wanted not in digests:
        print(f'skip {wanted}')
        return
    subprocess.check_call(['gh', 'release', 'download', tag, '-R', f'mautrix/{repository}', '-p', wanted, '-O', str(out / name), '--clobber'])
    actual = 'sha256:' + hashlib.sha256((out / name).read_bytes()).hexdigest()
    if actual != digests[wanted]:
        raise SystemExit(f'{wanted}: checksum mismatch')
    (out / name).chmod(0o755)


def main():
    target, out = sys.argv[1], Path(sys.argv[2])
    out.mkdir(parents=True, exist_ok=True)
    for repository, tag, name in BRIDGES:
        fetch(repository, tag, name, target, out)
    if target.startswith('darwin'):
        build_imessage(out)


if __name__ == '__main__':
    main()
