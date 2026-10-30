"use strict";

const output = document.querySelector("#output");
const lines = document.querySelector("#lines");
const empty = document.querySelector("#empty");
const status = document.querySelector("#status");
const decoders = { stdout: new TextDecoder(), stderr: new TextDecoder() };
const controller = new AbortController();
let received = false;
let size = 0;

// setStatus는 로그가 없을 때도 연결과 종료 상태를 구분해 표시한다.
function setStatus(text, state) {
  status.textContent = text;
  status.dataset.state = state;
}

// append는 로그를 HTML로 해석하지 않고 화면 메모리도 제한한다.
function append(text, stream) {
  // 1. 사용자가 과거 로그를 읽고 있으면 현재 스크롤 위치를 유지한다.
  const follow = output.scrollHeight - output.scrollTop - output.clientHeight < 80;
  const span = document.createElement("span");
  span.textContent = text;
  if (stream === "stderr") {
    span.className = "stderr";
  }
  lines.append(span);
  received = true;
  empty.hidden = true;
  size += text.length;

  // 2. 오래된 조각을 제거해 로그가 계속 들어와도 창이 무거워지지 않게 한다.
  while (size > 2_000_000 || lines.childNodes.length > 5000) {
    size -= lines.firstChild.textContent.length;
    lines.firstChild.remove();
  }
  if (follow) {
    output.scrollTop = output.scrollHeight;
  }
}

// frame은 전송 상태와 stdout·stderr의 멀티바이트 문자를 보존한다.
function frame(value) {
  if (value.kind === "log") {
    const stream = value.stream === "stderr" ? "stderr" : "stdout";
    const bytes = Uint8Array.from(atob(value.data || ""), (char) => char.charCodeAt(0));
    const text = decoders[stream].decode(bytes, { stream: true });
    if (text) {
      append(text, stream);
    }
    setStatus("실시간 로그", "ready");
  } else if (value.kind === "ready") {
    setStatus("실시간 로그", "ready");
    empty.textContent = "아직 출력된 로그가 없습니다. 새 로그가 생기면 자동으로 표시됩니다.";
  } else if (value.kind === "done") {
    setStatus("로그 출력 종료", "done");
    if (!received) {
      empty.textContent = "이 컨테이너에 출력된 로그가 없습니다.";
    }
  } else if (value.kind === "error") {
    setStatus(value.error || "로그 연결 실패", "error");
    if (!received) {
      empty.textContent = value.error || "로그를 읽을 수 없습니다.";
    }
  }
}

// start는 화면을 연 즉시 선택된 컨테이너의 로그를 연결한다.
async function start() {
  // 1. URL에는 중앙 토큰 없이 트레이가 발급한 로컬 화면 식별자만 사용한다.
  const response = await fetch(location.pathname + "/stream", { signal: controller.signal });
  if (!response.ok || !response.body) {
    throw new Error("로그에 연결할 수 없습니다. 서버 연결과 접근 권한을 확인하세요.");
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let pending = "";

  // 2. 네트워크 조각과 무관하게 완성된 전송 프레임만 해석한다.
  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    pending += decoder.decode(value, { stream: true });
    let index;
    while ((index = pending.indexOf("\n")) !== -1) {
      const line = pending.slice(0, index);
      pending = pending.slice(index + 1);
      if (line) {
        frame(JSON.parse(line));
      }
    }
    if (pending.length > 262_144) {
      throw new Error("로그 응답 크기를 초과했습니다.");
    }
  }
  if (status.dataset.state !== "error" && status.dataset.state !== "done") {
    setStatus("로그 연결 종료", "done");
  }
}

window.addEventListener("pagehide", () => controller.abort());
start().catch((error) => {
  if (error.name !== "AbortError") {
    setStatus(error.message || "로그 연결 실패", "error");
    if (!received) {
      empty.textContent = error.message || "로그에 연결할 수 없습니다.";
    }
  }
});
