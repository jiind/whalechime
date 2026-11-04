"""플랫폼별 트레이 실행 파일과 배포 문서를 하나의 압축 파일로 묶는다."""

import argparse
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile


VERSION = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
)
TARGETS = {("linux", "amd64"), ("windows", "amd64"), ("darwin", "amd64"), ("darwin", "arm64")}


# validate_version은 파일 이름과 태그에 사용할 정확한 SemVer 형식을 검사한다.
def validate_version(version):
    match = VERSION.fullmatch(version)
    if match is None:
        raise ValueError("Use a version such as v0.1.0 or v0.1.0-rc.1")
    for item in (match.group(4) or "").split("."):
        if item.isdigit() and len(item) > 1 and item.startswith("0"):
            raise ValueError("Numeric prerelease identifiers cannot start with zero")
    return match


# app_plist는 macOS의 트레이 전용 앱 번들 정보를 만든다.
def app_plist(version):
    match = validate_version(version)
    numeric = ".".join(match.group(index) for index in (1, 2, 3))
    return {
        "CFBundleExecutable": "whalechime",
        "CFBundleIdentifier": "io.github.jiind.whalechime",
        "CFBundleName": "Whalechime",
        "CFBundleDisplayName": "Whalechime",
        "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": numeric,
        "CFBundleVersion": numeric,
        "LSUIElement": True,
        "NSHighResolutionCapable": True,
    }


# archive_directory는 최상위 디렉터리를 유지하며 플랫폼별 형식으로 압축한다.
def archive_directory(directory, output, windows):
    if windows:
        with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
            for path in sorted(directory.rglob("*")):
                if path.is_file():
                    archive.write(path, path.relative_to(directory.parent))
    else:
        with tarfile.open(output, "w:gz") as archive:
            archive.add(directory, arcname=directory.name)


# package는 대상 바이너리·앱 번들·문서를 사용자 설정 없이 배포 파일로 만든다.
def package(root, output, version, goos, goarch):
    # 1. 지원 대상과 버전을 검증하고 임시 패키지 디렉터리를 준비한다.
    validate_version(version)
    if (goos, goarch) not in TARGETS:
        raise ValueError("Unsupported release target")
    if goos == "darwin" and sys.platform != "darwin":
        raise ValueError("Build macOS packages on macOS with Xcode command line tools")
    output.mkdir(parents=True, exist_ok=True)
    name = f"whalechime_{version}_{goos}_{goarch}"
    suffix = ".zip" if goos == "windows" else ".tar.gz"
    destination = output / (name + suffix)
    if destination.exists():
        raise FileExistsError("The release archive already exists")
    with tempfile.TemporaryDirectory(prefix="whalechime-package-") as temporary:
        stage = Path(temporary) / name
        stage.mkdir()
        binary = stage / ("whalechime.exe" if goos == "windows" else "whalechime")
        if goos == "darwin":
            contents = stage / "Whalechime.app" / "Contents"
            (contents / "MacOS").mkdir(parents=True)
            (contents / "Resources").mkdir()
            binary = contents / "MacOS" / "whalechime"
            with (contents / "Info.plist").open("wb") as target:
                plistlib.dump(app_plist(version), target)

        # 2. 사용자 환경과 분리해 트레이 바이너리와 배포 메타데이터를 만든다.
        environment = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="1" if goos == "darwin" else "0")
        command = ["go", "build", "-trimpath"]
        if goos == "windows":
            command += ["-ldflags", "-H=windowsgui"]
        subprocess.run(command + ["-o", str(binary), "./desktop"], cwd=root, env=environment, check=True)
        if goos != "windows":
            binary.chmod(0o755)
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
        (stage / "release.json").write_text(json.dumps({
            "version": version, "os": goos, "arch": goarch,
            "revision": revision, "signed": False,
        }, indent=2) + "\n", encoding="utf-8")
        for filename in ("README.md", "LICENSE", "SECURITY.md"):
            shutil.copy2(root / filename, stage / filename)
        shutil.copytree(root / "docs", stage / "docs")

        # 3. 완성되지 않은 압축 파일이 남지 않도록 임시 이름으로 만든 뒤 교체한다.
        partial = output / (destination.name + ".partial")
        try:
            archive_directory(stage, partial, goos == "windows")
            partial.replace(destination)
        finally:
            partial.unlink(missing_ok=True)
    return destination


# main은 검증 전용 실행 또는 지정한 플랫폼의 패키징 명령을 처리한다.
def main():
    # 1. 버전과 지원 플랫폼 인자를 확인한다.
    parser = argparse.ArgumentParser(description="Build a Whalechime release archive")
    parser.add_argument("--version", required=True)
    parser.add_argument("--os", choices=("linux", "windows", "darwin"))
    parser.add_argument("--arch", choices=("amd64", "arm64"))
    parser.add_argument("--output", type=Path, default=Path("dist"))
    parser.add_argument("--validate-only", action="store_true")
    args = parser.parse_args()
    try:
        validate_version(args.version)
        if args.validate_only:
            return 0
        if args.os is None or args.arch is None:
            parser.error("--os and --arch are required for packaging")

        # 2. 저장소 루트를 기준으로 패키지를 만들고 산출물 경로만 출력한다.
        root = Path(__file__).resolve().parent.parent
        archive = package(root, args.output.resolve(), args.version, args.os, args.arch)
        print(archive)
        return 0
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f"Packaging failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
