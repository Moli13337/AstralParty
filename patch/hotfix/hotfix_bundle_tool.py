import argparse
import os
import sys

import UnityPy

DEFAULT_BUNDLE = r"d:/Steam/steamapps/common/Astral Party/server/data/cdn/72fd5bcb7e636bf5b2d07e961d320419.bundle"
TARGET_DLL = "AstralParty.Runtime.dll"


def script_bytes(asset):
    raw = asset.m_Script
    if isinstance(raw, str):
        raw = raw.encode("utf-8", "surrogateescape")
    return raw


def set_script_bytes(asset, data):
    asset.m_Script = data.decode("utf-8", "surrogateescape")


def extract(bundle_path, out_dir):
    env = UnityPy.load(bundle_path)
    os.makedirs(out_dir, exist_ok=True)
    names = []
    for obj in env.objects:
        if obj.type.name != "TextAsset":
            continue
        asset = obj.read()
        raw = script_bytes(asset)
        dest = os.path.join(out_dir, asset.m_Name)
        with open(dest, "wb") as f:
            f.write(raw)
        names.append(f"{asset.m_Name} {len(raw)}")
    print("\n".join(names))
    print(f"extracted {len(names)} TextAssets -> {out_dir}")


def repack(bundle_path, dll_path, out_path):
    with open(dll_path, "rb") as f:
        patched = f.read()
    env = UnityPy.load(bundle_path)
    hit = 0
    for obj in env.objects:
        if obj.type.name != "TextAsset":
            continue
        asset = obj.read()
        if asset.m_Name != TARGET_DLL:
            continue
        set_script_bytes(asset, patched)
        asset.save()
        hit += 1
    if hit != 1:
        sys.exit(f"expected exactly 1 {TARGET_DLL}, found {hit}")
    data = env.file.save()
    with open(out_path, "wb") as f:
        f.write(data)
    print(f"repacked {TARGET_DLL} ({len(patched)} bytes) -> {out_path} ({len(data)} bytes)")


def verify(bundle_path, dll_path):
    with open(dll_path, "rb") as f:
        expected = f.read()
    env = UnityPy.load(bundle_path)
    got = None
    for obj in env.objects:
        if obj.type.name != "TextAsset":
            continue
        asset = obj.read()
        if asset.m_Name == TARGET_DLL:
            got = script_bytes(asset)
    if got is None:
        sys.exit(f"{TARGET_DLL} not found in bundle")
    if got != expected:
        sys.exit(f"VERIFY FAIL: round-trip mismatch ({len(got)} vs {len(expected)})")
    print(f"VERIFY OK: {TARGET_DLL} round-trips byte-exact ({len(got)} bytes)")


def main():
    ap = argparse.ArgumentParser(description="extract/repack/verify hotfix DLL inside addressables bundle")
    ap.add_argument("cmd", choices=["extract", "repack", "verify"])
    ap.add_argument("--bundle", default=DEFAULT_BUNDLE)
    ap.add_argument("--out-dir", help="extract target dir")
    ap.add_argument("--dll", help="patched AstralParty.Runtime.dll for repack/verify")
    ap.add_argument("--out-bundle", help="repack output bundle path")
    args = ap.parse_args()

    if args.cmd == "extract":
        if not args.out_dir:
            sys.exit("--out-dir required")
        extract(args.bundle, args.out_dir)
    elif args.cmd == "repack":
        if not args.dll or not args.out_bundle:
            sys.exit("--dll and --out-bundle required")
        repack(args.bundle, args.dll, args.out_bundle)
    elif args.cmd == "verify":
        if not args.dll:
            sys.exit("--dll required")
        verify(args.bundle, args.dll)


if __name__ == "__main__":
    main()
