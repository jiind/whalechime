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

test("첫 서버 추가와 모바일 메뉴가 보이며 뒤로가기·앞으로가기가 화면을 복구한다", async ({ page }) => {
  // 1. 첫 실행에서 가로 스크롤 없이 연결 추가로 이동한다.
  await page.setViewportSize({ width: 375, height: 812 });
  await login(page);
  const add = page.getByRole("button", { name: "서버 연결 추가", exact: true });
  await expect(add).toBeInViewport();
  for (const button of await page.getByRole("navigation").getByRole("button").all()) {
    await expect(button).toBeInViewport();
  }
  await add.click();
  await expect(page.getByLabel("서버 이름", { exact: true })).toBeFocused();
  await expect(page.getByRole("button", { name: "서버 연결", exact: true })).toHaveAttribute("aria-current", "page");

  // 2. 브라우저 방문 기록이 앱 안의 이전·다음 화면을 복원한다.
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "서버 연결", exact: true })).toBeFocused();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "대시보드", exact: true })).toBeFocused();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "서버 연결", exact: true })).toBeFocused();
  await expect(page).toHaveTitle("서버 연결 · Whalechime");
});

test("미저장 수신 설정은 화면 이동·조회·새로고침 뒤에도 보존하고 명시적으로 저장한다", async ({ page }) => {
  // 1. 아직 저장하지 않은 선택을 다른 화면에 다녀와도 유지한다.
  await login(page);
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  const toggle = page.getByRole("checkbox", { name: "container.oom", exact: true });
  const before = await toggle.isChecked();
  await toggle.setChecked(!before);
  await expect(page.locator("#preference-draft")).toBeVisible();
  await page.getByRole("button", { name: "대시보드", exact: true }).click();
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  await expect(toggle).toBeChecked({ checked: !before });
  await page.getByRole("button", { name: "새로고침" }).click();
  await expect(toggle).toBeChecked({ checked: !before });
  await page.reload();
  await expect(toggle).toBeChecked({ checked: !before });
  await expect(page.locator("#preference-draft")).toBeVisible();

  // 2. 설정 저장을 누른 뒤에만 임시 상태를 해제하고 영구 저장을 확인한다.
  await page.getByRole("button", { name: "설정 저장" }).click();
  await expect(page.locator("#preference-draft")).toBeHidden();
  await expect(page.getByRole("status")).toContainText("수신 항목을 저장했습니다");
  await page.reload();
  await expect(toggle).toBeChecked({ checked: !before });
  await expect(page.locator("#preference-draft")).toBeHidden();
});

test("저장 처리 중의 추가 변경을 이전 응답으로 지우지 않는다", async ({ page }) => {
  // 1. 저장 요청의 응답만 지연시켜 처리 중 추가 변경을 재현한다.
  await login(page);
  await page.getByRole("button", { name: "수신 항목", exact: true }).click();
  const toggle = page.getByRole("checkbox", { name: "container.oom", exact: true });
  const before = await toggle.isChecked();
  await toggle.setChecked(!before);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let received;
  const saving = new Promise((resolve) => {
    received = resolve;
  });
  await page.route("**/api/me/preferences", async (route) => {
    if (route.request().method() === "PUT") {
      const response = await route.fetch();
      received();
      await gate;
      await route.fulfill({ response });
    } else {
      await route.continue();
    }
  });
  await page.getByRole("button", { name: "설정 저장" }).click();
  await saving;
  await toggle.setChecked(before);
  release();

  // 2. 이후 변경은 초안으로 남기고 재저장 후에만 해제한다.
  await expect(page.getByRole("status")).toContainText("이후 변경사항");
  await expect(page.locator("#preference-draft")).toBeVisible();
  await page.reload();
  await expect(toggle).toBeChecked({ checked: before });
  await expect(page.locator("#preference-draft")).toBeVisible();
  await page.unroute("**/api/me/preferences");
  await page.getByRole("button", { name: "설정 저장" }).click();
  await expect(page.locator("#preference-draft")).toBeHidden();
});

test("모바일 저장 오류를 해당 입력에 연결하고 긴 주소와 감시 꺼짐을 정확히 표시한다", async ({ page }) => {
  // 1. 실제 API의 인증서 검증 오류가 해당 필드로 초점을 이동시킨다.
  await page.setViewportSize({ width: 375, height: 812 });
  await login(page, "#servers");
  const name = `긴 주소 검증 ${Date.now()}`;
  await page.getByLabel("서버 이름", { exact: true }).fill(name);
  await page.getByLabel("서버 주소", { exact: true }).fill("https://127.0.0.1:1");
  const ca = page.locator("#server-ca");
  await ca.fill("/tmp/whalechime-missing-ca.pem");
  await page.getByLabel("이 서버 감시하기").uncheck();
  await page.getByRole("button", { name: "연결 저장", exact: true }).click();
  await expect(ca).toBeFocused();
  await expect(ca).toHaveAttribute("aria-invalid", "true");
  await expect(ca).toHaveAttribute("aria-describedby", "server-ca-error");
  await expect(ca).toHaveAccessibleName("CA 인증서 파일 경로");
  await expect(page.locator("#server-ca-error")).toContainText("CA 인증서 파일을 읽을 수 없습니다");
  await expect(page.locator("#server-ca-error")).toBeInViewport();

  // 2. HTTP로 수정하면 인증서 없이 저장되고 긴 주소도 카드 안에서 줄바꿈된다.
  const address = `http://review-server-${"a".repeat(40)}.example.review-server-${"b".repeat(40)}.example:2375`;
  await page.getByLabel("서버 주소", { exact: true }).fill(address);
  await page.getByRole("button", { name: "연결 저장", exact: true }).click();
  const card = page.locator(".server-card").filter({ has: page.getByRole("heading", { name, exact: true }) });
  await expect(card).toContainText(address);
  await expect(card).toContainText("감시 꺼짐");
  await expect(ca).not.toHaveAttribute("aria-invalid", "true");
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.locator("body")).toHaveJSProperty("scrollWidth", width);
  }

  // 3. 비활성 서버를 연결 중으로 집계하지 않고 검증용 연결만 제거한다.
  await page.getByRole("button", { name: "대시보드", exact: true }).click();
  const stat = page.locator(".stat").filter({ hasText: "등록된 서버" });
  await expect(stat.locator("strong")).toHaveText("1");
  await expect(stat).toContainText("0대 감시 켜짐 · 1대 감시 꺼짐");
  await page.getByRole("button", { name: "서버 연결", exact: true }).click();
  page.once("dialog", (dialog) => dialog.accept());
  await card.getByRole("button", { name: "연결 삭제", exact: true }).click();
  await expect(card).toHaveCount(0);
});
