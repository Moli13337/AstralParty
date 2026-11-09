import json
import os
import sys
import threading
from concurrent.futures import ThreadPoolExecutor

import requests

CATALOG = r"d:/Steam/steamapps/common/Astral Party/server/data/cdn/com.unity.addressables/catalog_3.3.0.json"
LOCAL_DIR = r"d:/Steam/steamapps/common/Astral Party/INT/hotfix_bundles"
OUT_DIR = r"d:/Steam/steamapps/common/Astral Party/server/data/cdn"
BASE = "https://seresjp.feimogames.com/INT_STEAM/3.3.0/026"

done = 0
done_lock = threading.Lock()
failed = []


def bundle_names():
    cat = json.load(open(CATALOG, encoding="utf-8"))
    names = set()
    for i in cat["m_InternalIds"]:
        if ".bundle" not in i:
            continue
        name = re.split(r"[\\/]", i)[-1]
        names.add(name)
    return sorted(names)


import re


def fetch(name):
    global done
    dest = os.path.join(OUT_DIR, name)
    if os.path.exists(dest) and os.path.getsize(dest) > 0:
        pass
    else:
        local = os.path.join(LOCAL_DIR, name)
        if os.path.exists(local):
            data = open(local, "rb").read()
        else:
            try:
                r = requests.get(f"{BASE}/{name}", timeout=60)
                if r.status_code != 200:
                    failed.append((name, f"http {r.status_code}"))
                    return
                data = r.content
            except Exception as e:
                failed.append((name, str(e)[:80]))
                return
        with open(dest, "wb") as f:
            f.write(data)
    with done_lock:
        done += 1
        if done % 100 == 0:
            print(f"progress {done}")


def main():
    names = bundle_names()
    print(f"total bundles in catalog: {len(names)}")
    with ThreadPoolExecutor(max_workers=16) as pool:
        list(pool.map(fetch, names))
    print(f"done={done} failed={len(failed)}")
    if failed:
        for name, err in failed[:30]:
            print("FAIL", name, err)
        with open(r"d:/Steam/steamapps/common/Astral Party/server/data/download_failed.txt", "w") as f:
            for name, err in failed:
                f.write(f"{name}\t{err}\n")


if __name__ == "__main__":
    main()
