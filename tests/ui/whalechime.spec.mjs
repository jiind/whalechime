import { test as base, expect } from "@playwright/test";
import { createServer } from "node:http";

const test = base.extend({
  receiver: async ({}, use) => {
    const messages = [];
    const server = createServer(async (request, response) => {
      const chunks = [];
      for await (const chunk of request) {
        chunks.push(chunk);
      }
      messages.push(Buffer.concat(chunks).toString());
      response.writeHead(204).end();
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
      await use({ url: `http://127.0.0.1:${server.address().port}`, messages });
    } finally {
      await new Promise((resolve) => server.close(resolve));
    }
  }
});

// login은 트레이에서 사용하는 로컬 진입 주소로 로그인 폼 없이 화면을 연다.
async function login(page, hash = "") {
  await page.goto(process.env.WHALECHIME_TEST_WEB_URL + hash);
  await expect(page.getByRole("heading", { name: hash ? "서버 연결" : "대시보드", exact: true })).toBeVisible();
}

test("개인 항목 저장과 수신처 관리는 독립적이며 다시 열어도 유지된다", async ({ page, receiver }) => {
  // 1. 실제 설정 폼에서 두 이벤트를 켜고 새로고침 후 유지 여부를 확인한다.
  await login(page);
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  await page.getByRole("button", { name: "전체 끄기", exact: true }).click();
  await page.getByRole("checkbox", { name: "image.pull", exact: true }).check();
  await page.getByRole("checkbox", { name: "image.push", exact: true }).check();
  await page.getByRole("button", { name: "설정 저장" }).click();
  await expect(page.getByRole("status")).toContainText("수신 항목을 저장했습니다");
  await page.reload();
  await expect(page.getByRole("checkbox", { name: "image.pull", exact: true })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: "image.push", exact: true })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: "container.start", exact: true })).not.toBeChecked();

  // 2. 개인 수신처 추가·비활성화·재활성화·수정·삭제를 UI로 수행한다.
  await page.getByRole("button", { name: "내 수신처", exact: true }).click();
  const name = `브라우저 수신처 ${Date.now()}`;
  await page.getByLabel("수신처 이름").fill(name);
  await page.getByLabel("알림 서비스").selectOption("webhook");
  await page.getByLabel("Webhook URL", { exact: true }).fill(receiver.url);
  await page.getByRole("button", { name: "수신처 저장" }).click();
  const card = page.locator(".destination-card").filter({ has: page.getByRole("heading", { name, exact: true }) });
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "테스트 발송" }).click();
  await expect(page.getByRole("status")).toContainText("테스트 알림을 발송했습니다");
  expect(receiver.messages).toHaveLength(1);
  expect(JSON.parse(receiver.messages[0]).title).toBe("Whalechime 연결 확인");
  await card.getByRole("button", { name: `${name} 끄기`, exact: true }).click();
  await expect(card.getByRole("button", { name: `${name} 켜기`, exact: true })).toBeVisible();
  await page.reload();
  await expect(card.getByRole("button", { name: `${name} 켜기`, exact: true })).toBeVisible();
  await card.getByRole("button", { name: `${name} 켜기`, exact: true }).click();
  await card.getByRole("button", { name: "연결 정보 수정" }).click();
  await expect(page.getByLabel("Webhook URL", { exact: true })).toHaveValue("");
  await page.getByLabel("Webhook URL", { exact: true }).fill(receiver.url);
  await page.getByRole("button", { name: "수신처 저장" }).click();
  page.once("dialog", (dialog) => dialog.accept());
  await card.getByRole("button", { name: "삭제", exact: true }).click();
  await expect(card).toHaveCount(0);

  // 3. 수신처 조작 뒤에도 이벤트별 수신 설정이 유지된다.
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  await expect(page.getByRole("checkbox", { name: "image.push", exact: true })).toBeChecked();
});

test("서버별 HTTP·HTTPS·mTLS·SSH 설정을 선택하고 저장·수정·삭제한다", async ({ page, receiver }) => {
  // 1. HTTP 연결을 저장하고 새로고침 후에도 주소와 선택이 유지되는지 확인한다.
  await login(page, "#servers");
  const name = `LAN 서버 ${Date.now()}`;
  await page.getByLabel("서버 이름", { exact: true }).fill(name);
  await page.getByLabel("서버 주소", { exact: true }).fill(receiver.url);
  await page.getByLabel("이 서버 감시하기").uncheck();
  await page.getByRole("button", { name: "연결 저장", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("서버 연결을 저장했습니다");
  const card = page.locator(".server-card").filter({ has: page.getByRole("heading", { name, exact: true }) });
  await expect(card).toContainText(receiver.url);
  await page.reload();
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "연결 수정", exact: true }).click();
  await expect(page.getByLabel("서버 주소", { exact: true })).toHaveValue(receiver.url);
  await expect(page.getByLabel("이 서버 감시하기")).not.toBeChecked();

  // 2. HTTPS에서만 CA·mTLS를 표시하고 인증서 입력을 필수로 전환한다.
  await page.getByLabel("서버 주소", { exact: true }).fill("https://docker.example:2376");
  await expect(page.getByLabel("CA 인증서 파일 경로", { exact: true })).toBeVisible();
  await page.getByLabel("mTLS 사용", { exact: true }).check();
  await expect(page.getByLabel("클라이언트 인증서 파일 경로", { exact: true })).toHaveAttribute("required", "");
  await expect(page.getByLabel("클라이언트 개인키 파일 경로", { exact: true })).toBeVisible();
  await page.getByLabel("서버 주소", { exact: true }).fill(receiver.url);
  await expect(page.getByLabel("mTLS 사용", { exact: true })).toBeHidden();
  await page.getByRole("button", { name: "연결 저장", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("서버 연결을 저장했습니다");

  // 3. SSH로 전환하면 TLS 입력을 숨기고 기존 키 사용 안내를 표시한다.
  await card.getByRole("button", { name: "연결 수정", exact: true }).click();
  await page.getByLabel("연결 방식", { exact: true }).selectOption("ssh");
  await page.getByLabel("서버 주소", { exact: true }).fill("ssh://test-user@my-server:2222");
  await expect(page.getByLabel("SSH 개인키 파일 경로", { exact: true })).toBeVisible();
  await expect(page.getByLabel("CA 인증서 파일 경로", { exact: true })).toBeHidden();
  await page.getByRole("button", { name: "연결 저장", exact: true }).click();
  await expect(card).toContainText("ssh://test-user@my-server:2222");
  page.once("dialog", (dialog) => dialog.accept());
  await card.getByRole("button", { name: "연결 삭제", exact: true }).click();
  await expect(card).toHaveCount(0);
});

test("모바일에서 로그인 입력 없이 로컬 서버 연결 설정을 연다", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, "#servers");
  await expect(page.getByLabel("서버 주소", { exact: true })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "접속 토큰" })).toHaveCount(0);
  await expect(page.locator("body")).toHaveJSProperty("scrollWidth", 390);
});
