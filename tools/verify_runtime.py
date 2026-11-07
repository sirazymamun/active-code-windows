#!/usr/bin/env python3
"""Verify the independent pinned VLC input; join browser-upload parts if needed.

No executable is read, and no network access or third-party modules are used.
"""
from pathlib import Path
import hashlib
import zipfile

NAME = 'vlc-3.0.24-win64.zip'
SIZE = 82765861
SHA256 = 'fcf30850371ad10c9373cc4f0f4501e7dee49e3e9ae9f20c72fb2661a1ca6323'
PART_SIZE = 20 * 1024 * 1024
PART_COUNT = (SIZE + PART_SIZE - 1) // PART_SIZE


def validate(path):
    if not path.is_file() or path.stat().st_size != SIZE:
        raise ValueError(f'{NAME}: missing or wrong byte size (expected {SIZE})')
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b''):
            digest.update(block)
    if digest.hexdigest() != SHA256:
        raise ValueError(f'{NAME}: SHA-256 mismatch; use the supplied stable runtime')
    with zipfile.ZipFile(path) as archive:
        names = set(archive.namelist())
        required = {'vlc-3.0.24/libvlc.dll', 'vlc-3.0.24/libvlccore.dll'}
        if not required <= names or not any(n.startswith('vlc-3.0.24/plugins/') for n in names):
            raise ValueError('Runtime DLLs or codec plugins missing')
        if archive.testzip() is not None:
            raise ValueError('Runtime ZIP CRC failure')


def ensure_runtime(root):
    folder = Path(root) / 'source' / 'runtime'
    target = folder / NAME
    if target.exists():
        validate(target)
        return target
    parts = folder / 'parts'
    expected = [parts / f'{NAME}.part{i:03d}' for i in range(1, PART_COUNT + 1)]
    actual = set(parts.glob(f'{NAME}.part*')) if parts.exists() else set()
    if actual != set(expected):
        raise ValueError(f'Provide source/runtime/{NAME}, or all {PART_COUNT} browser-upload parts; old EXE is not required')
    for index, part in enumerate(expected):
        length = min(PART_SIZE, SIZE - index * PART_SIZE)
        if not part.is_file() or part.stat().st_size != length:
            raise ValueError(f'Wrong part size: {part.name}')
    staged = target.with_suffix('.zip.assembling')
    try:
        with staged.open('wb') as output:
            for part in expected:
                with part.open('rb') as source:
                    for block in iter(lambda: source.read(1024 * 1024), b''):
                        output.write(block)
        validate(staged)
        staged.replace(target)
    finally:
        if staged.exists():
            staged.unlink()
    return target


if __name__ == '__main__':
    path = ensure_runtime(Path(__file__).resolve().parent.parent)
    print(f'PASS: VLC 3.0.24 Stable input verified ({SIZE} bytes, SHA-256 {SHA256})')
    print(path)
