#!/usr/bin/env python3
"""Build reproducible herdr-notes release archives and SHA256SUMS."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import zipfile

TARGETS = (
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("linux", "arm64"),
    ("linux", "amd64"),
    ("windows", "amd64"),
)


def build(root: Path, dist: Path, version: str, goos: str, goarch: str) -> Path:
    exe = ".exe" if goos == "windows" else ""
    binary = dist / f"herdr-notes{exe}"
    env = os.environ.copy()
    env.update({"CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch})
    subprocess.run(
        [
            "go",
            "build",
            "-trimpath",
            "-buildvcs=false",
            "-ldflags",
            f"-s -w -X main.version={version}",
            "-o",
            str(binary),
            "./cmd/herdr-notes",
        ],
        cwd=root,
        env=env,
        check=True,
    )
    return binary


def tar_gz(binary: Path, archive: Path) -> None:
    data = binary.read_bytes()
    with archive.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as gz:
            with tarfile.open(fileobj=gz, mode="w", format=tarfile.PAX_FORMAT) as tf:
                info = tarfile.TarInfo("herdr-notes")
                info.size = len(data)
                info.mode = 0o755
                info.mtime = 0
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                tf.addfile(info, io.BytesIO(data))


def zip_exe(binary: Path, archive: Path) -> None:
    info = zipfile.ZipInfo("herdr-notes.exe", date_time=(1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = 0o755 << 16
    info.create_system = 3
    with zipfile.ZipFile(archive, "w") as zf:
        zf.writestr(info, binary.read_bytes())


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--dist", default="dist")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent.parent
    dist = (root / args.dist).resolve()
    dist.mkdir(parents=True, exist_ok=True)
    for old in dist.glob("herdr-notes-*"):
        old.unlink()
    sums: list[tuple[str, str]] = []

    with tempfile.TemporaryDirectory(prefix="herdr-notes-package-") as tmp:
        temp = Path(tmp)
        for goos, goarch in TARGETS:
            binary = build(root, temp, args.version, goos, goarch)
            target = f"{goos}-{goarch}"
            if goos == "windows":
                archive = dist / f"herdr-notes-{args.version}-{target}.zip"
                zip_exe(binary, archive)
            else:
                archive = dist / f"herdr-notes-{args.version}-{target}.tar.gz"
                tar_gz(binary, archive)
            sums.append((sha256(archive), archive.name))
            binary.unlink()

    sums.sort(key=lambda pair: pair[1])
    (dist / "SHA256SUMS").write_text(
        "".join(f"{digest}  {name}\n" for digest, name in sums), encoding="utf-8"
    )


if __name__ == "__main__":
    main()
