"use strict";

const $ = (selector) => document.querySelector(selector);
const state = { view: "overview", preferences: new Map(), keys: [], defaultEnabled: false, destinations: [], editID: null, servers: [], serverEditID: null };
const labels = { desktop: "이 PC 알림", email: "이메일", slack: "Slack", teams: "Microsoft Teams", discord: "Discord", telegram: "Telegram", "google-chat": "Google Chat", mattermost: "Mattermost", ntfy: "ntfy", gotify: "Gotify", pushover: "Pushover", webhook: "사용자 정의 Webhook" };
const groupLabels = { container: "컨테이너", image: "이미지", network: "네트워크", volume: "볼륨", daemon: "Docker 데몬", plugin: "플러그인", service: "Swarm 서비스", node: "Swarm 노드", secret: "시크릿", config: "설정", server: "서버 연결" };
const statusLabels = { sent: "발송 완료", pending: "재시도 대기", processing: "발송 중", failed: "발송 실패", cancelled: "발송 취소" };
const pages = { overview: ["AT A GLANCE", "대시보드", "연결된 서버와 최근 Docker 이벤트를 확인하세요."], events: ["ACTIVITY", "이벤트 이력", "수신 설정과 관계없이 수집한 이벤트를 기록합니다. 최근 최대 500건을 표시합니다."], preferences: ["YOUR PREFERENCES", "수신 항목", "어떤 소식을 받을지 직접 선택하세요."], destinations: ["YOUR CONNECTIONS", "내 수신처", "알림을 받을 곳을 등록하세요. 활성 수신처 모두에 같은 알림을 보냅니다."], deliveries: ["DELIVERY LOG", "발송 이력", "내 수신처별 최근 발송 결과와 재시도를 확인하세요."], servers: ["CONNECTIONS", "서버 연결", "Docker API 또는 기존 SSH 연결로 서버를 추가하세요." ] };
const fields = {
 desktop: [],
  email: [["host", "SMTP 호스트"], ["port", "SMTP 포트", "number", "587"], ["username", "SMTP 사용자 이름", "text", "", false], ["password", "SMTP 비밀번호", "password", "", false], ["from", "발신 이메일", "email"], ["to", "수신 이메일", "email"]],
  telegram: [["token", "Bot 토큰", "password"], ["chat_id", "채팅 ID"]],
  pushover: [["token", "애플리케이션 토큰", "password"], ["user_key", "사용자 키", "password"]],
  gotify: [["url", "Gotify 서버 주소", "url"], ["token", "애플리케이션 토큰", "password"]],
  ntfy: [["url", "구독 토픽 URL", "url"], ["token", "접근 토큰", "password", "", false]],
  webhook: [["url", "Webhook URL", "url"], ["token", "Bearer 토큰", "password", "", false]]
};

// element는 외부 데이터를 HTML로 해석하지 않고 DOM 요소를 만든다.
function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) {
    node.className = className;
  }
  if (text !== undefined) {
    node.textContent = text;
  }
  return node;
}

// request는 현재 로컬 세션으로 앱 API를 호출한다.
async function request(path, method = "GET", body) {
  // 1. 브라우저가 관리하는 HttpOnly 쿠키로 인증한다.
  const response = await fetch(path, { method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = response.status === 204 ? null : await response.json();

  // 2. API가 제공한 사용자용 오류만 화면에 표시한다.
  if (!response.ok) {
    throw new Error(data?.error || "요청을 처리하지 못했습니다.");
  }
  return data;
}

// on은 사용자 동작의 실패를 해당 화면에 표시하고 중복 클릭을 막는다.
function on(target, event, action) {
  const node = typeof target === "string" ? $(target) : target;
  node.addEventListener(event, async (e) => {
    e.preventDefault();
    const button = e.submitter || (node.tagName === "BUTTON" ? node : null);
    if (button) {
      button.disabled = true;
    }
    $("#page-error").textContent = "";
    try {
      await action(e);
    } catch (error) {
      $("#page-error").textContent = error.message;
    } finally {
      if (button) {
        button.disabled = false;
      }
    }
  });
}

// toast는 저장이나 테스트 발송 결과를 접근 가능한 상태 메시지로 표시한다.
function toast(message) {
  const node = $("#toast");
  node.textContent = message;
  node.hidden = false;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => {
    node.hidden = true;
  }, 4000);
}

// navigate는 화면을 전환하고 해당 업무의 최신 데이터를 불러온다.
async function navigate(view) {
  // 1. 선택한 로컬 화면을 확인한다.
  if (!pages[view]) {
    return;
  }
  state.view = view;
  history.replaceState(null, "", `#${view}`);
  $("#page-error").textContent = "";
  for (const node of document.querySelectorAll(".view")) {
    node.hidden = node.id !== `view-${view}`;
  }
  for (const node of document.querySelectorAll("[data-view]")) {
    node.classList.toggle("active", node.dataset.view === view);
  }
  const [kicker, title, description] = pages[view];
  $("#page-kicker").textContent = kicker;
  $("#page-title").textContent = title;
  $("#page-description").textContent = description;

  // 2. 화면 종류에 맞는 API를 조회한다.
  await refresh();
}

// refresh는 현재 화면의 데이터를 새로 조회한다.
async function refresh() {
  switch (state.view) {
    case "overview": return overview();
    case "events": return loadEvents(true);
    case "preferences": return loadPreferences();
    case "destinations": return loadDestinations();
    case "deliveries": return loadDeliveries();
    case "servers": return loadServers();
  }
}

// time는 서버 시각을 사용자 브라우저의 지역 시각으로 표시한다.
function time(value) {
  return value ? new Date(value).toLocaleString("ko-KR") : "아직 연결되지 않음";
}

// empty는 데이터가 없는 화면에 다음 행동을 안내한다.
function empty(text) {
  return element("div", "empty", text);
}

// table은 안전한 텍스트나 DOM 셀로 목록을 구성한다.
function table(headers, rows) {
  // 1. 제목 행과 스크롤 가능한 표를 만든다.
  const wrapper = element("div", "table-wrap");
  const node = element("table");
  const head = element("thead");
  const titleRow = element("tr");
  for (const title of headers) {
    titleRow.append(element("th", "", title));
  }
  head.append(titleRow);
  const body = element("tbody");

  // 2. 셀에 데이터 자체를 HTML로 삽입하지 않는다.
  for (const cells of rows) {
    const row = element("tr");
    for (const value of cells) {
      const cell = element("td");
      cell.append(value instanceof Node ? value : document.createTextNode(value ?? "—"));
      row.append(cell);
    }
    body.append(row);
  }
  node.append(head, body);
  wrapper.append(node);
  return wrapper;
}

// eventTable은 이벤트 요약과 펼쳐 볼 수 있는 원본 상세를 표시한다.
function eventTable(items) {
  // 1. 최근 이벤트의 공통 필드를 표시한다.
  if (!items.length) {
    return empty("아직 수집된 이벤트가 없습니다. 서버 연결을 추가하면 변경 이력이 여기에 표시됩니다.");
  }
  const node = table(["이벤트", "대상", "발생 시각", "상세"], items.map((item) => {
    const details = element("button", "secondary", "상세 보기");
    const code = element("code", "", item.event_key);
    return [code, item.payload.attributes?.name || item.payload.actor_id?.slice(0, 25) || "—", time(item.occurred_at), details];
  }));

  // 2. 상세 JSON은 명시적으로 펼친 행에만 표시한다.
  [...node.querySelectorAll("tbody tr")].forEach((row, index) => {
    const button = row.querySelector("button");
    button.setAttribute("aria-expanded", "false");
    on(button, "click", () => {
      if (row.nextSibling?.classList.contains("event-detail")) {
        row.nextSibling.remove();
        button.setAttribute("aria-expanded", "false");
        return;
      }
      const detail = element("tr", "event-detail");
      const cell = element("td");
      cell.colSpan = 4;
      cell.append(element("pre", "detail", JSON.stringify(items[index], null, 2)));
      detail.append(cell);
      row.after(detail);
      button.setAttribute("aria-expanded", "true");
    });
  });
  return node;
}

// overview는 연결 서버와 개인 수신처의 현재 상태를 요약한다.
async function overview() {
  // 1. 서로 독립적인 요약 데이터를 함께 조회한다.
  const [servers, events, destinations] = await Promise.all([request("/api/servers"), request("/api/events?limit=8"), request("/api/me/destinations")]);
  const root = $("#view-overview");
  root.replaceChildren();
  const stats = element("div", "stats");
  for (const [title, count, hint] of [["연결된 서버", servers.length, "이 PC에 등록한 Docker 서버"], ["활성 수신처", destinations.filter((d) => d.enabled).length, "알림이 도착하는 곳"], ["최근 이벤트", events.length, "최근 수집 이력"]]) {
    const card = element("div", "stat");
    card.append(element("span", "muted small", title), element("strong", "", count), element("small", "", hint));
    stats.append(card);
  }
  root.append(stats, element("h2", "section-heading", "내 서버"));

  // 2. 연결 상태와 최근 이벤트를 실제 조회 결과로 표시한다.
  if (servers.length) {
    root.append(table(["서버", "연결 방식", "상태"], servers.map((s) => [s.name, s.address.startsWith("ssh:") ? "SSH" : s.address.startsWith("https:") ? (s.mtls ? "HTTPS · mTLS" : "HTTPS") : "HTTP", s.status])));
  } else {
    root.append(empty("연결된 서버가 없습니다. 서버 연결 메뉴에서 첫 서버를 추가하세요."));
  }
  root.append(element("h2", "section-heading", "최근 Docker 이벤트"), eventTable(events));
}

// options는 선택 요소의 항목을 현재 선택값을 유지하며 교체한다.
function options(select, items, allLabel) {
  const previous = select.value;
  select.replaceChildren();
  if (allLabel) {
    select.append(new Option(allLabel, ""));
  }
  for (const [value, label] of items) {
    select.append(new Option(label, value));
  }
  if ([...select.options].some((option) => option.value === previous)) {
    select.value = previous;
  }
}

// loadEvents는 서버와 이벤트 종류 필터로 최근 이력을 조회한다.
async function loadEvents(updateFilters) {
  // 1. 화면 진입 시 접근 가능한 필터를 갱신한다.
  if (updateFilters) {
    const [servers, keys] = await Promise.all([request("/api/servers"), request("/api/event-types")]);
    options($("#event-server"), servers.map((s) => [s.id, s.name]), "전체 서버");
    options($("#event-key"), keys.map((key) => [key, key]), "전체 이벤트");
  }

  // 2. 선택한 조건으로만 목록을 갱신한다.
  const params = new URLSearchParams({ server_id: $("#event-server").value, event_key: $("#event-key").value });
  $("#event-list").replaceChildren(eventTable(await request(`/api/events?${params}`)));
}

// loadPreferences는 개인 기본값과 명시적인 항목 선택을 불러온다.
async function loadPreferences() {
  const [prefs, keys] = await Promise.all([request("/api/me/preferences"), request("/api/event-types")]);
  state.defaultEnabled = prefs.default_enabled;
  state.preferences = new Map(prefs.items.map((item) => [item.event_key, item.enabled]));
  state.keys = keys;
  $("#default-enabled").checked = state.defaultEnabled;
  renderPreferences();
}

// renderPreferences는 검색 조건과 개인 기본값을 반영한 항목 스위치를 표시한다.
function renderPreferences() {
  // 1. 검색된 이벤트를 Docker 객체 종류별로 묶는다.
  const root = $("#preference-groups");
  root.replaceChildren();
  const query = $("#preference-search").value.toLowerCase();
  const groups = new Map();
  for (const key of state.keys.filter((key) => key.toLowerCase().includes(query))) {
    const group = key.split(".")[0];
    if (!groups.has(group)) {
      groups.set(group, []);
    }
    groups.get(group).push(key);
  }

  // 2. 각 스위치는 수신처 정보 없이 항목의 수신 여부만 변경한다.
  for (const [group, keys] of groups) {
    const panel = element("div", "preference-group");
    panel.append(element("h2", "", `${groupLabels[group] || group} · ${keys.length}`));
    const items = element("div", "preference-items");
    for (const key of keys) {
      const label = element("label", "preference-item");
      const toggle = element("input");
      toggle.type = "checkbox";
      toggle.checked = state.preferences.has(key) ? state.preferences.get(key) : state.defaultEnabled;
      toggle.setAttribute("aria-label", key);
      toggle.addEventListener("change", () => state.preferences.set(key, toggle.checked));
      label.append(document.createTextNode(key), toggle);
      items.append(label);
    }
    panel.append(items);
    root.append(panel);
  }
  if (!groups.size) {
    root.append(empty("검색 조건에 맞는 항목이 없습니다."));
  }
}

// changeAll은 현재 알려진 모든 수신 항목을 일괄 변경한다.
function changeAll(enabled) {
  for (const key of state.keys) {
    state.preferences.set(key, enabled);
  }
  renderPreferences();
}

// destinationFields는 선택한 서비스에 필요한 연결 입력만 표시한다.
function destinationFields() {
  const root = $("#destination-fields");
  root.replaceChildren();
  const kind = $("#destination-kind").value;
  const configFields = fields[kind] || [["url", kind === "teams" ? "Teams Workflows Webhook URL" : "Webhook URL", "url"]];
  for (const [key, title, type = "text", initial = "", required = true] of configFields) {
    const label = element("label", "", title);
    const input = element("input");
    input.name = key;
    input.type = type;
    input.required = required;
    input.value = initial;
    input.autocomplete = "off";
    if (type === "number") {
      input.min = "1";
      input.max = "65535";
    }
    label.append(input);
    root.append(label);
  }
}

// resetDestination은 입력한 비밀값을 지우고 추가 폼으로 돌아간다.
function resetDestination() {
  state.editID = null;
  $("#destination-form").reset();
  $("#destination-form-title").textContent = "수신처 추가";
  $("#cancel-destination").hidden = true;
  destinationFields();
}

// loadDestinations는 본인의 수신처와 연결 확인 동작을 표시한다.
async function loadDestinations() {
  // 1. 연결 정보 원문을 포함하지 않는 목록과 지원 채널을 조회한다.
  const [items, kinds] = await Promise.all([request("/api/me/destinations"), request("/api/destination-types")]);
  state.destinations = items;
  options($("#destination-kind"), kinds.map((kind) => [kind, labels[kind] || kind]));
  if (!$("#destination-fields").children.length) {
    destinationFields();
  }
  const root = $("#destination-list");
  root.replaceChildren();

  // 2. 수신처마다 활성화·테스트·수정·삭제를 독립적으로 제공한다.
  for (const d of items) {
    const card = element("div", "panel destination-card");
    const heading = element("div", "heading");
    const title = element("div");
    title.append(element("span", "eyebrow", labels[d.kind] || d.kind), element("h2", "", d.name));
    const toggle = element("button", "secondary", d.enabled ? "사용 중" : "사용 안 함");
    toggle.setAttribute("aria-label", `${d.name} ${d.enabled ? "끄기" : "켜기"}`);
    on(toggle, "click", async () => {
      await request(`/api/me/destinations/${d.id}`, "PATCH", { enabled: !d.enabled });
      await loadDestinations();
      toast("수신처 사용 여부를 변경했습니다.");
    });
    heading.append(title, toggle);
    const actions = element("div", "actions");
    const test = element("button", "secondary", "테스트 발송");
    on(test, "click", async () => {
      await request(`/api/me/destinations/${d.id}/test`, "POST", {});
      toast("테스트 알림을 발송했습니다.");
    });
    const edit = element("button", "secondary", "연결 정보 수정");
    on(edit, "click", () => {
      state.editID = d.id;
      $("#destination-name").value = d.name;
      $("#destination-kind").value = d.kind;
      $("#destination-enabled").checked = d.enabled;
      $("#destination-form-title").textContent = "수신처 수정";
      $("#cancel-destination").hidden = false;
      destinationFields();
      $("#destination-name").focus();
    });
    const remove = element("button", "secondary danger", "삭제");
    on(remove, "click", async () => {
      if (!confirm(`'${d.name}' 수신처를 삭제할까요?`)) {
        return;
      }
      await request(`/api/me/destinations/${d.id}`, "DELETE");
      resetDestination();
      await loadDestinations();
      toast("수신처를 삭제했습니다.");
    });
    actions.append(test, edit, remove);
    card.append(heading, element("p", "small muted", "선택한 모든 알림 항목에 공통 적용"), actions);
    root.append(card);
  }
  if (!items.length) {
    root.append(empty("등록된 수신처가 없습니다. 아래에서 첫 수신처를 추가하세요."));
  }
}

// loadDeliveries는 수신처별 발송 상태와 안전한 오류 요약을 표시한다.
async function loadDeliveries() {
  const items = await request("/api/me/deliveries");
  $("#delivery-list").replaceChildren(items.length ? table(["수신처", "상태", "시도", "발송 시각", "오류"], items.map((item) => [item.destination_name, element("span", `badge ${item.status}`, statusLabels[item.status] || item.status), item.attempts, item.sent_at ? time(item.sent_at) : "—", item.last_error || "—"])) : empty("아직 발송 이력이 없습니다. 수신 항목과 수신처를 활성화하면 알림이 발송됩니다."));
}

// bind는 화면 조작을 API 업무 흐름에 연결한다.
function bind() {
  // 1. 접속과 화면 이동을 연결한다.
  on("#refresh", "click", refresh);
  for (const node of document.querySelectorAll("[data-view]")) {
    on(node, "click", () => navigate(node.dataset.view));
  }
  on("#filter-events", "click", () => loadEvents(false));

  // 2. 개인 수신 항목과 수신처를 독립적으로 저장한다.
  on("#default-enabled", "change", () => {
    state.defaultEnabled = $("#default-enabled").checked;
    renderPreferences();
  });
  on("#preference-search", "input", renderPreferences);
  on("#all-on", "click", () => changeAll(true));
  on("#all-off", "click", () => changeAll(false));
  on("#save-preferences", "click", async () => {
    await request("/api/me/preferences", "PUT", { default_enabled: state.defaultEnabled, items: [...state.preferences].map(([event_key, enabled]) => ({ event_key, enabled })) });
    toast("수신 항목을 저장했습니다.");
  });
  on("#destination-kind", "change", destinationFields);
  on("#cancel-destination", "click", resetDestination);
  on("#destination-form", "submit", async () => {
    const config = {};
    for (const input of $("#destination-fields").querySelectorAll("input")) {
      config[input.name] = input.type === "number" ? Number(input.value) : input.value;
    }
    await request(state.editID ? `/api/me/destinations/${state.editID}` : "/api/me/destinations", state.editID ? "PUT" : "POST", { name: $("#destination-name").value.trim(), kind: $("#destination-kind").value, enabled: $("#destination-enabled").checked, config });
    resetDestination();
    await loadDestinations();
    toast("수신처를 저장했습니다.");
  });

  // 3. 연결 방식에 필요한 인증 설정만 저장한다.
  on("#server-transport", "change", () => {
    const ssh = $("#server-transport").value === "ssh";
    $("#server-address").value = ssh ? "ssh://" : "http://";
    connectionFields();
  });
  on("#server-address", "input", connectionFields);
  on("#server-mtls", "change", connectionFields);
  on("#cancel-server", "click", resetServer);
  on("#server-form", "submit", async () => {
    const value = serverInput();
    await request(state.serverEditID ? `/api/servers/${state.serverEditID}` : "/api/servers", state.serverEditID ? "PUT" : "POST", value);
    resetServer();
    await loadServers();
    toast("서버 연결을 저장했습니다.");
  });
}

// connectionFields는 주소에 해당하는 TLS·mTLS·SSH 입력만 표시한다.
function connectionFields() {
  const ssh = $("#server-transport").value === "ssh";
  const tls = !ssh && $("#server-address").value.startsWith("https://");
  $("#tls-fields").hidden = !tls;
  $("#ssh-fields").hidden = !ssh;
  $("#mtls-fields").hidden = !tls || !$("#server-mtls").checked;
  $("#server-cert").required = tls && $("#server-mtls").checked;
  $("#server-key").required = tls && $("#server-mtls").checked;
  $("#connection-note").textContent = ssh ? "기존 SSH 키·설정으로 연결합니다. 서버에 Docker 조회 권한이 필요합니다." : tls ? "서버 인증서를 검증하는 HTTPS 연결입니다." : "HTTP는 전송을 암호화하지 않습니다. 서버의 접근 허용 범위를 확인하세요.";
}

// serverInput은 선택한 연결 방식에 필요한 값만 요청으로 구성한다.
function serverInput() {
  const ssh = $("#server-transport").value === "ssh";
  const address = $("#server-address").value.trim();
  if ((ssh && !address.startsWith("ssh://")) || (!ssh && !/^https?:\/\//.test(address))) {
    throw new Error("선택한 연결 방식과 서버 주소를 맞춰주세요.");
  }
  const tls = !ssh && address.startsWith("https://");
  const mtls = tls && $("#server-mtls").checked;
  return { name: $("#server-name").value.trim(), address, enabled: $("#server-enabled").checked, mtls, ca_file: tls ? $("#server-ca").value.trim() : "", cert_file: mtls ? $("#server-cert").value.trim() : "", key_file: mtls ? $("#server-key").value.trim() : "", identity_file: ssh ? $("#server-identity").value.trim() : "" };
}

// resetServer는 이전 인증서 경로를 새 서버에 복사하지 않도록 폼을 초기화한다.
function resetServer() {
  state.serverEditID = null;
  $("#server-form").reset();
  $("#server-form-title").textContent = "서버 연결 추가";
  $("#cancel-server").hidden = true;
  connectionFields();
}

// loadServers는 저장한 연결과 상태를 표시하고 로컬 연결만 수정·삭제한다.
async function loadServers() {
  // 1. 서버 주소·연결 방식·수집 상태를 구분해 표시한다.
  state.servers = await request("/api/servers");
  const root = $("#server-list");
  root.replaceChildren();
  for (const s of state.servers) {
    const card = element("article", "panel server-card");
    card.append(element("h2", "", s.name), element("p", "small", s.address), element("p", "muted small", s.status));
    const actions = element("div", "actions");
    const test = element("button", "secondary", "연결 확인");
    on(test, "click", async () => {
      await request(`/api/servers/${s.id}/test`, "POST", {});
      toast("Docker 연결을 확인했습니다.");
    });
    const edit = element("button", "secondary", "연결 수정");
    on(edit, "click", () => {
      state.serverEditID = s.id;
      $("#server-form-title").textContent = "서버 연결 수정";
      $("#server-name").value = s.name;
      $("#server-address").value = s.address;
      $("#server-transport").value = s.address.startsWith("ssh:") ? "ssh" : "api";
      $("#server-enabled").checked = s.enabled;
      $("#server-mtls").checked = s.mtls;
      for (const [id, key] of [["server-ca", "ca_file"], ["server-cert", "cert_file"], ["server-key", "key_file"], ["server-identity", "identity_file"]]) {
        document.getElementById(id).value = s[key] || "";
      }
      $("#cancel-server").hidden = false;
      connectionFields();
      $("#server-name").focus();
    });
    const remove = element("button", "secondary danger", "연결 삭제");
    on(remove, "click", async () => {
      if (!confirm(`'${s.name}' 연결을 삭제할까요? Docker의 컨테이너는 변경하지 않습니다.`)) {
        return;
      }
      await request(`/api/servers/${s.id}`, "DELETE");
      resetServer();
      await loadServers();
      toast("서버 연결을 삭제했습니다.");
    });
    actions.append(test, edit, remove);
    card.append(actions);
    root.append(card);
  }

  // 2. 처음 실행한 사용자에게 바로 연결을 추가하도록 안내한다.
  if (!state.servers.length) {
    root.append(empty("등록된 서버가 없습니다. 아래에서 Docker API 또는 SSH 연결을 추가하세요."));
  }
}

bind();
connectionFields();
window.addEventListener("hashchange", () => {
  navigate(location.hash.slice(1)).catch((error) => {
    $("#page-error").textContent = error.message;
  });
});
navigate(pages[location.hash.slice(1)] ? location.hash.slice(1) : "overview").catch((error) => {
  $("#page-error").textContent = error.message;
});
