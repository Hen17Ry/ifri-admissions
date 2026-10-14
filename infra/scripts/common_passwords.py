"""Regenerate the embedded list of common passwords refused for administrator accounts.

Sources are pinned to one SecLists commit (MIT licence) and checked by SHA-256 before use:
the NCSC top 100k (derived from Have I Been Pwned) and the xato top 1M. Only entries of 12 to
128 characters matter (shorter passwords are already refused), compared in lower case.
Usage: python infra/scripts/common_passwords.py
"""

import gzip
import hashlib
import sys
from pathlib import Path
from urllib.request import urlopen

COMMIT = "1a7bb9127eca9e6ff2fc0301c597fe6e16a0cb56"
BASE = f"https://raw.githubusercontent.com/danielmiessler/SecLists/{COMMIT}/Passwords/Common-Credentials/"
SOURCES = {
    "100k-most-used-passwords-NCSC.txt": (
        "c2e5696882c603b76bb67a47ee970897e5a76fc4c3f5547abe3d0ca340c576e0"
    ),
    "xato-net-10-million-passwords-1000000.txt": (
        "424a3e03a17df0a2bc2b3ca749d81b04e79d59cb7aeec8876a5a3f308d0caf51"
    ),
}
OUTPUT = (
    Path(__file__).resolve().parents[2]
    / "services/api/internal/platform/passwords/common-passwords.txt.gz"
)


def main() -> int:
    entries: set[str] = set()
    for name, digest in SOURCES.items():
        with urlopen(BASE + name, timeout=120) as response:
            body = response.read()
        if hashlib.sha256(body).hexdigest() != digest:
            print(f"{name}: SHA-256 mismatch, refusing to use it", file=sys.stderr)
            return 1
        for line in body.decode("utf-8", errors="replace").splitlines():
            if 12 <= len(line) <= 128 and line.isprintable():
                entries.add(line.lower())
    text = "".join(f"{entry}\n" for entry in sorted(entries)).encode()
    # mtime=0 keeps the archive byte-identical across runs.
    OUTPUT.write_bytes(gzip.compress(text, compresslevel=9, mtime=0))
    print(f"{len(entries)} entries, {OUTPUT.stat().st_size} bytes -> {OUTPUT.name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
