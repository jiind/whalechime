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

const token = process.env.WHALECHIME_TEST_ADMIN_TOKEN;
test.skip(!token, "전용 테스트 서버의 WHALECHIME_TEST_ADMIN_TOKEN이 필요합니다.");

// login은 실제 로그인 폼을 사용해 관리자 계정으로 진입한다.
async function login(page) {
  await page.goto("/");
  await page.getByLabel("접속 토큰").fill(token);
  await page.getByRole("button", { name: "시작하기" }).click();
  await expect(page.getByRole("heading", { name: "대시보드", exact: true })).toBeVisible();
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

test("관리자가 팀·서버·사용자를 연결하고 일반 사용자는 자기 설정만 변경한다", async ({ page, browser }) => {
  // 1. 실제 관리자 화면에서 사용자·서버·팀과 두 연결을 등록한다.
  await login(page);
  await page.getByRole("button", { name: "관리", exact: true }).click();
  const suffix = Date.now();
  const userName = `사용자 ${suffix}`;
  const serverName = `서버 ${suffix}`;
  const teamName = `팀 ${suffix}`;
  await page.getByLabel("사용자 이름", { exact: true }).fill(userName);
  await page.getByRole("button", { name: "계정 발급" }).click();
  await expect(page.locator("#issued-token-value")).toContainText("wc_user_");
  const userToken = (await page.locator("#issued-token-value").textContent()).split("\n").at(-1);
  await page.getByLabel("서버 이름", { exact: true }).fill(serverName);
  await page.getByRole("button", { name: "서버 등록", exact: true }).click();
  await expect(page.locator("#issued-token-value")).toContainText("wc_agent_");
  await page.getByLabel("새 팀 이름").fill(teamName);
  await page.getByRole("button", { name: "팀 등록", exact: true }).click();
  await page.locator("#team-member-list").getByRole("checkbox", { name: userName, exact: true }).check();
  await expect(page.getByRole("status")).toContainText("연결 설정을 변경했습니다");
  await page.getByLabel("감시 서버", { exact: true }).selectOption({ label: serverName });
  await page.locator("#server-team-list").getByRole("checkbox", { name: teamName, exact: true }).check();
  await expect(page.getByRole("status")).toContainText("연결 설정을 변경했습니다");

  // 2. 별도 브라우저 문맥에서 발급받은 사용자로 로그인한다.
  const context = await browser.newContext();
  try {
    const userPage = await context.newPage();
    await userPage.goto(new URL("/", page.url()).toString());
    await userPage.getByLabel("접속 토큰").fill(userToken);
    await userPage.getByRole("button", { name: "시작하기" }).click();
    await expect(userPage.getByText(serverName, { exact: true })).toBeVisible();
    await expect(userPage.getByRole("button", { name: "관리", exact: true })).toBeHidden();
    await userPage.getByRole("button", { name: "수신 항목", exact: true }).click();
    await userPage.getByRole("checkbox", { name: "image.pull", exact: true }).check();
    await userPage.getByRole("button", { name: "설정 저장" }).click();
    await expect(userPage.getByRole("status")).toContainText("수신 항목을 저장했습니다");
  } finally {
    await context.close();
  }

  // 3. 팀 연결은 새로고침 후에도 유지된다.
  await page.reload();
  await page.getByLabel("감시 서버", { exact: true }).selectOption({ label: serverName });
  await expect(page.locator("#server-team-list").getByRole("checkbox", { name: teamName, exact: true })).toBeChecked();
});

test("모바일 설정 바로가기와 로그아웃을 사용할 수 있다", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#destinations");
  await page.getByLabel("접속 토큰").fill(token);
  await page.getByRole("button", { name: "시작하기" }).click();
  await expect(page.getByRole("heading", { name: "내 수신처", exact: true })).toBeVisible();
  await expect(page.getByLabel("알림 서비스")).toBeVisible();
  await expect(page.locator("body")).toHaveJSProperty("scrollWidth", 390);
  await page.locator("#logout-mobile").click();
  await expect(page.getByLabel("접속 토큰")).toBeVisible();
});
