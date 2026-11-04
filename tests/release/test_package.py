"""버전 입력 경계와 배포 압축 파일의 실제 구조를 검증한다."""

import importlib.util
from pathlib import Path
import plistlib
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "package-release.py"
SPEC = importlib.util.spec_from_file_location("package_release", SCRIPT)
RELEASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RELEASE)


# PackageTests는 사용자 설정 없이 버전·아카이브·앱 번들 경계를 검사한다.
class PackageTests(unittest.TestCase):
    # test_versions는 정상 SemVer와 태그·파일 경로 주입 입력을 구분한다.
    def test_versions(self):
        for value in ("v0.1.0", "v1.2.3-rc.1", "v1.2.3+build.01", "v1.2.3-beta+build-1"):
            with self.subTest(value=value):
                RELEASE.validate_version(value)
        for value in ("1.2.3", "v01.2.3", "v1.2.3-01", "v1.2.3\n", "../../v1.2.3", "v1.2.3;echo", "v1.2.3-"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                RELEASE.validate_version(value)

    # test_platform_archives는 Windows·Linux·macOS의 배포 구조와 문서 포함을 확인한다.
    def test_platform_archives(self):
        # 1. Go 컴파일 경계만 대체하고 실제 파일·plist·압축 형식을 사용한다.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "repo"
            root.mkdir()
            (root / "docs").mkdir()
            for name in ("README.md", "LICENSE", "SECURITY.md", "docs/releases.md"):
                (root / name).write_text("Release documentation\n", encoding="utf-8")

            # build는 컴파일 대상 경로에 테스트 실행 파일을 생성한다.
            def build(command, **kwargs):
                binary = Path(command[command.index("-o") + 1])
                binary.write_bytes(b"test executable")
                binary.chmod(0o755)

            for goos, goarch in sorted(RELEASE.TARGETS):
                with self.subTest(goos=goos, goarch=goarch):
                    with mock.patch.object(RELEASE.sys, "platform", "darwin"), mock.patch.object(RELEASE.subprocess, "run", side_effect=build), mock.patch.object(RELEASE.subprocess, "check_output", return_value="012345\n"):
                        archive = RELEASE.package(root, Path(temporary) / "dist", "v0.1.0-rc.1", goos, goarch)

                    # 2. 압축 파일의 진입 실행 파일과 macOS 번들 정보를 확인한다.
                    prefix = f"whalechime_v0.1.0-rc.1_{goos}_{goarch}/"
                    if goos == "windows":
                        with zipfile.ZipFile(archive) as package:
                            names = set(package.namelist())
                            self.assertIn(prefix + "whalechime.exe", names)
                    else:
                        with tarfile.open(archive) as package:
                            names = set(package.getnames())
                            if goos == "darwin":
                                self.assertIn(prefix + "Whalechime.app/Contents/MacOS/whalechime", names)
                                metadata = plistlib.load(package.extractfile(prefix + "Whalechime.app/Contents/Info.plist"))
                                self.assertTrue(metadata["LSUIElement"])
                                self.assertEqual(metadata["CFBundleShortVersionString"], "0.1.0")
                            else:
                                self.assertIn(prefix + "whalechime", names)
                    self.assertIn(prefix + "LICENSE", names)
                    self.assertIn(prefix + "docs/releases.md", names)
                    self.assertIn(prefix + "release.json", names)

    # test_unsupported_target은 지원하지 않는 CPU·OS 조합을 빌드 전에 거부한다.
    def test_unsupported_target(self):
        with tempfile.TemporaryDirectory() as temporary, self.assertRaises(ValueError):
            RELEASE.package(Path(temporary), Path(temporary) / "dist", "v0.1.0", "windows", "arm64")


if __name__ == "__main__":
    unittest.main()
