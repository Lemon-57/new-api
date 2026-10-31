"""Transport a locally built artifact using verified copies from its old binary.

No source compilation occurs when applying a delta. Both full artifact hashes,
operation bounds and the final byte count are checked. Applying never overwrites
an existing file; publish the verified output separately.
"""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import struct

MAGIC = b'NAPIDL1\n'
BLOCK = 1024 * 1024


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def apply(base, patch, output=None):
    written = 0
    checksum = hashlib.sha256()
    target = None
    try:
        with gzip.open(patch, 'rb') as encoded, base.open('rb') as original:
            if encoded.read(len(MAGIC)) != MAGIC:
                raise ValueError('Unexpected delta format')
            size = struct.unpack('>I', encoded.read(4))[0]
            if not 0 < size < 8192:
                raise ValueError('Invalid delta header size')
            header = json.loads(encoded.read(size))
            if base.stat().st_size != header['base_bytes'] or digest(base) != header['base_sha256']:
                raise ValueError('Delta base checksum mismatch')
            if output:
                if output.resolve() in [base.resolve(), patch.resolve()]:
                    raise ValueError('Output must be a new artifact path')
                target = output.open('xb')
            for _ in range(header['operations']):
                kind = encoded.read(1)
                if kind == b'C':
                    offset, length = struct.unpack('>QI', encoded.read(12))
                    if offset + length > header['base_bytes']:
                        raise ValueError('Copy exceeds the verified base')
                    original.seek(offset)
                    data = original.read(length) if length <= BLOCK else b''
                elif kind == b'D':
                    length = struct.unpack('>I', encoded.read(4))[0]
                    data = encoded.read(length) if length <= BLOCK else b''
                else:
                    raise ValueError('Unknown delta operation')
                if not 0 < length <= BLOCK or len(data) != length or written + length > header['target_bytes']:
                    raise ValueError('Invalid delta operation bounds')
                checksum.update(data)
                written += length
                if target:
                    target.write(data)
            if encoded.read(1) or written != header['target_bytes'] or checksum.hexdigest() != header['target_sha256']:
                raise ValueError('Reconstructed artifact checksum mismatch')
        return {'verified': True, 'target_bytes': written, 'target_sha256': checksum.hexdigest()}
    finally:
        if target:
            target.close()


def pack(base, target, patch):
    if patch.resolve() in [base.resolve(), target.resolve()]:
        raise ValueError('Delta must not overwrite an input artifact')
    old, new = base.read_bytes(), target.read_bytes()
    operations = []
    copied = 0
    for start in range(0, len(new), BLOCK):
        block = new[start:start + BLOCK]
        offset = start if old[start:start + len(block)] == block else -1
        if offset < 0 and len(block) >= 64:
            for anchor_start in [min(4096, len(block) - 64), len(block) // 2, 0]:
                anchor = block[anchor_start:anchor_start + 64]
                if len(anchor) != 64 or len(set(anchor)) < 6:
                    continue
                position = old.find(anchor)
                for _ in range(4):
                    if position < 0:
                        break
                    possible = position - anchor_start
                    if possible >= 0 and old[possible:possible + len(block)] == block:
                        offset = possible
                        break
                    position = old.find(anchor, position + 1)
                if offset >= 0:
                    break
        operations.append((offset, block))
        if offset >= 0:
            copied += len(block)
    header = {'base_sha256': hashlib.sha256(old).hexdigest(), 'base_bytes': len(old),
              'target_sha256': hashlib.sha256(new).hexdigest(), 'target_bytes': len(new),
              'operations': len(operations)}
    raw = json.dumps(header, separators=(',', ':')).encode()
    with gzip.open(patch, 'wb', compresslevel=6) as stream:
        stream.write(MAGIC + struct.pack('>I', len(raw)) + raw)
        for offset, block in operations:
            if offset >= 0:
                stream.write(b'C' + struct.pack('>QI', offset, len(block)))
            else:
                stream.write(b'D' + struct.pack('>I', len(block)) + block)
    verified = apply(base, patch)
    return dict(verified, base_sha256=header['base_sha256'], copied_bytes=copied,
                patch_bytes=patch.stat().st_size, patch_sha256=digest(patch))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('mode', choices=['pack', 'apply'])
    parser.add_argument('--base', required=True, type=Path)
    parser.add_argument('--target', type=Path)
    parser.add_argument('--patch', required=True, type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.mode == 'pack':
        if not args.target:
            parser.error('pack requires --target')
        result = pack(args.base, args.target, args.patch)
    else:
        if not args.output:
            parser.error('apply requires --output')
        result = apply(args.base, args.patch, args.output)
    print(json.dumps(result, indent=2))
