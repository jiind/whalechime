import { execFileSync, spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

// setup은 사용자 설정과 분리된 로컬 앱을 실행하고 테스트 종료 시 정리한다.
export default async function setup() {
  // 1. 전용 실행 파일과 데이터 디렉터리를 준비한다.
  const dir = await mkdtemp(join(tmpdir(), "whalechime-ui-"));
  const binary = join(dir, process.platform === "win32" ? "whalechime.exe" : "whalechime");
  const session = join(dir, "session.json");
  execFileSync("go", ["build", "-o", binary, "./cmd/whalechime"], { stdio: "pipe" });
  const child = spawn(binary, ["-data-dir", join(dir, "data"), "-session-file", session], { env: { ...process.env, WHALECHIME_ALLOWED_NOTIFICATION_HOSTS: "127.0.0.1" }, stdio: "ignore" });
  let ready = false;
  for (let attempt = 0; attempt < 150; attempt++) {
    if (child.exitCode !== null) {
      break;
    }
    try {
      const value = JSON.parse(await readFile(session, "utf8"));
      process.env.WHALECHIME_TEST_WEB_URL = value.web_url;
      ready = true;
      break;
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
  }
  if (!ready) {
    child.kill("SIGTERM");
    throw new Error("테스트 전용 로컬 앱을 시작하지 못했습니다.");
  }

  // 2. 트레이 없는 테스트 프로세스만 종료하고 임시 파일을 제거한다.
  return async () => {
    if (child.exitCode === null) {
      child.kill("SIGTERM");
      await new Promise((resolve) => {
        const timer = setTimeout(() => {
          child.kill("SIGKILL");
          resolve();
        }, 8000);
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
      });
    }
    await rm(dir, { recursive: true, force: true });
  };
}
