#!/usr/bin/env python3
"""Build the standalone Windows EXE from source and pinned runtime inputs.

Run from any directory: python tools/build_windows.py
Requires stable Go on PATH; never reads an existing active-code.exe.
"""
from pathlib import Path
import hashlib
import os
import shutil
import struct
import subprocess
import sys
import tempfile
from verify_runtime import ensure_runtime, SIZE

ROOT = Path(__file__).resolve().parent.parent
VERSION = '5.1.8'


def run(args, cwd, env=None):
    print('+ ' + ' '.join(map(str, args)), flush=True)
    subprocess.run(list(map(str, args)), cwd=cwd, env=env, check=True)


def build():
    go = shutil.which('go')
    if not go:
        raise RuntimeError('Go toolchain not found. Install stable Go or use GitHub Actions.')
    source = ROOT / 'source'
    if (ROOT / 'app-version.txt').read_text(encoding='utf-8-sig').strip() != VERSION:
        raise ValueError('app-version.txt must match this source release: ' + VERSION)
    for name in ['main.go', 'native_ui_windows.go', 'libvlc_native_windows.go',
                 'libvlc_runtime_windows.go', 'libvlc_resources_embed_windows.go',
                 'appicon_windows_amd64.syso', 'assets/app-icon.ico']:
        if not (source / name).is_file():
            raise ValueError('Missing Windows source input: ' + name)
    if f'const appVersion = "{VERSION}"' not in (source/'main.go').read_text():
        raise ValueError('Source app version does not match ' + VERSION)
    runtime_source = (source/'libvlc_runtime_windows.go').read_text()
    if 'vlcStableVersion = "3.0.24"' not in runtime_source or 'net/http' in runtime_source:
        raise ValueError('Expected pinned offline VLC 3.0.24 integration')
    if (source/'launch_windows.go').exists() or (source/'web').exists():
        raise ValueError('Legacy browser launcher must not be used')
    runtime = ensure_runtime(ROOT)
    print('PASS: independent runtime verified; old EXE is not an input', flush=True)
    run([go, 'version'], source)
    run([sys.executable, '-m', 'unittest', 'discover', '-s', 'tools', '-p', 'test_*.py', '-v'], ROOT)
    run([go, 'test', '-count=1', './...'], source)
    run([go, 'vet', './...'], source)
    target_env = os.environ.copy()
    target_env.update(GOOS='windows', GOARCH='amd64', CGO_ENABLED='0')
    with tempfile.TemporaryDirectory(prefix='active-code-win-tests-') as folder:
        test_exe = Path(folder)/'native-tests.exe'
        run([go, 'test', '-c', '-o', test_exe, '.'], source, target_env)
        if test_exe.read_bytes()[:2] != b'MZ':
            raise ValueError('Windows native test executable was not generated')
    artifacts = ROOT/'artifacts'
    artifacts.mkdir(exist_ok=True)
    output = artifacts/'active-code.exe'
    # Remove a prior output so a failed build can never be mistaken for a new EXE.
    if output.exists():
        output.unlink()
    run([go, 'build', '-buildvcs=false', '-trimpath', '-ldflags=-s -w -H windowsgui',
         '-o', output, '.'], source, target_env)
    binary = output.read_bytes()
    if len(binary) <= SIZE or binary[:2] != b'MZ':
        raise ValueError('Standalone runtime or Windows executable header missing')
    pe = struct.unpack_from('<I', binary, 0x3c)[0]
    if binary[pe:pe+4] != b'PE\0\0' or struct.unpack_from('<H', binary, pe+4)[0] != 0x8664:
        raise ValueError('Expected x64 Windows PE executable')
    if struct.unpack_from('<H', binary, pe+24+68)[0] != 2:
        raise ValueError('Expected Windows GUI subsystem')
    if runtime.read_bytes() not in binary:
        raise ValueError('Exact pinned runtime was not embedded into the new EXE')
    if b'msedge.exe' in binary.lower():
        raise ValueError('Browser launcher must not be embedded')
    digest = hashlib.sha256(binary).hexdigest()
    (artifacts/'SHA256SUMS.txt').write_text(digest + '  active-code.exe\n', encoding='ascii')
    print(f'PASS: {output} ({len(binary)} bytes); SHA-256 {digest}', flush=True)


if __name__ == '__main__':
    try:
        build()
    except Exception as error:
        print('BUILD FAILED: ' + str(error), file=sys.stderr)
        sys.exit(1)
