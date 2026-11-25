"use strict";

const el = (id) => document.getElementById(id);
const all = (selector) => [...document.querySelectorAll(selector)];
const mobile = document.body.classList.contains("platform-android");
const windowsReview = document.documentElement.dataset.reviewShortcuts === "windows";
const shell = document.querySelector(".shell");
const controls = window.WordReviewControls;
const wordPreview = el("word-preview");
const flashcard = el("flashcard");
const swipeContent = el("swipe-content");
const cardFront = swipeContent.querySelector(".card-front");
const swipeCue = el("swipe-cue");
const answerPanel = el("answer");
const voteDock = el("vote-dock");
const voteActions = el("vote-actions");
const barRaised = {transform: "translateY(0%)", opacity: 1};
const barLowered = {transform: "translateY(100%)", opacity: 0};
const barLeaving = {transform: "translateY(62%)", opacity: .25};
const navigation = document.querySelector(".nav");
const navigationButtons = [...navigation.querySelectorAll("button[data-view]")];
const navigationIndicator = navigation.querySelector(".nav-indicator");
const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");
const views = ["review", "add", "library", "stats", "settings"];
const editFields = ["word", "meaning", "pos", "ipa", "ipaUK", "ipaUS", "examples",
  "exampleTranslation", "etymology", "collocations", "note", "personalNote"];
const dayFormat = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric", month: "long", day: "numeric", weekday: "long",
});

let state, revealed, practice, editing, confirmation;
let view = "review";
let viewRequest = 0;
let pendingNavigation;
let pageTransition;
let fallbackPageAnimation;
let answerAnimation;
let revealRequest = 0;
let voteBarAnimation;
let voteBarVisible = false;
let listAnimation;
let settingsFrom = "review";
let settingsOffset = 0;
let studying = false;
let busy = false;
let swiping = false;
let editTimer, editJob;
let settingsDraft, settingsJob, settingsSaved;
let savedNote = "";
let noteClock = 0;
let noteTimer;
let fileFormat = "words";
let imported = [];
let importPending = false;
let listPage = 0;
let listRequest = 0;
let listTotal = 0;
let listSignature;
let searchTimer;
let preview, previewStart, previewDismiss;
let selectedAll = false;
const selected = new Set();

async function request(path, data, options = {}) {
  if (data !== undefined) Object.assign(options, {
    method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(data),
  });
  return fetch(path, options).catch(() => {
    throw new Error(mobile ? "无法连接到LexiGo。请退出并重新打开应用。" : "无法连接到LexiGo。请双击 LexiGo.exe 重新打开。");
  });
}

async function api(path, data, options) {
  const response = await request(path, data, options);
  const content = await response.json();
  if (!response.ok) {
    const error = new Error(content.error || "操作未成功，请重试。");
    error.status = response.status;
    throw error;
  }
  return content;
}

function message(id, text = "") {
  const target = el(id);
  target.textContent = text;
  target.hidden = !text;
}

function showError(error) { message("error", error?.message || ""); }
function keyPath(key) { return encodeURIComponent(key); }

function button(label, activate, extra = "") {
  const item = document.createElement("button");
  item.type = "button";
  item.className = "quiet " + extra;
  item.textContent = label;
  item.addEventListener("click", activate);
  return item;
}

function link(label, url) {
  const item = document.createElement("a");
  item.textContent = label;
  item.href = url;
  item.target = "_blank";
  item.rel = "noopener noreferrer";
  return item;
}

function sameCard(a, b) {
  return a?.key === b?.key && a?.order === b?.order && a?.reviews === b?.reviews;
}

function motionOptions(token, easing = "--motion-ease") {
  const style = getComputedStyle(document.documentElement);
  return {duration: parseFloat(style.getPropertyValue(token)), easing: style.getPropertyValue(easing).trim()};
}

function applyState(next, force = false) {
  const changed = !sameCard(state?.current, next.current) || state?.date !== next.date;
  const displayChanged = state?.settings.showIPA !== next.settings.showIPA;
  const onlineChanged = state?.settings.online !== next.settings.online;
  const retained = !force && (state?.waitingForNext || next.waitingForNext) && state?.date === next.date && revealed?.key === next.current?.key;
  state = next;
  if ((changed || force) && !practice && !retained) clearAnswer();
  renderReview();
  if ((displayChanged || onlineChanged) && revealed) renderDetails(revealed);
  if (onlineChanged && next.settings.online && revealed && answerExpanded()) refreshRevealedDetails().catch(showError);
  if (next.waitingForNext && studying && view === "review" && !practice && !busy && !answerExpanded() && !el("reveal").disabled) toggleReveal();
}

async function refreshState(intent = viewRequest) {
  const old = state;
  let next = await api("/api/state");
  if (intent !== viewRequest) return;
  const reviewing = view === "review" || (view === "settings" && settingsFrom === "review");
  if (old && (old.date !== next.date || !old.current && next.current) && studying && reviewing && !practice && !noteDirty()) {
    next = await api("/api/review/start", {});
  }
  if (intent === viewRequest && state === old && !noteDirty()) applyState(next);
}

function renderReview() {
  if (!state) return;
  const next = state;
  const days = next.exam?.days;
  const title = el("review-title");
  title.classList.toggle("exam-countdown", !practice);
  title.textContent = practice ? "提前复习" : days === 0 ? "今天考试"
    : days < 0 ? "考试日期已过" : `距考试还有 ${days} 天`;
  el("date").textContent = dayFormat.format(new Date(next.date + "T12:00:00"));
  el("study-summary").hidden = !!practice || !next.taskTotal;
  const completion = `今日进度 ${next.taskDone} / ${next.taskTotal}`;
  if (el("day-completion").textContent !== completion) el("day-completion").textContent = completion;
  el("day-meter").max = Math.max(1, next.taskTotal);
  el("day-meter").value = next.taskDone;
  el("review-empty").removeAttribute("aria-busy");
  el("session-tools").hidden = mobile || !studying || !!practice;
  const waiting = !practice && next.waitingForNext;
  el("session-hint").textContent = next.current
    ? waiting ? "1 下一单词" : "Enter 查看释义 · 1 没记住 · 2 记住了"
    : "";
  el("session-hint").hidden = !next.current;
  el("forgot-label").textContent = waiting ? "下一单词" : "没记住";
  voteActions.classList.toggle("awaiting-next", !!waiting);
  updateReviewControls();
  el("pass-today").hidden = !studying || !!practice || !next.current || !!next.current.recall;
  const current = practice || (studying && next.current);
  flashcard.hidden = !current;
  if (mobile) cardFront.toggleAttribute("data-swipe-reveal", !!current && !answerExpanded());
  el("review-start").hidden = !!practice || !!studying || !next.current;
  el("review-empty").hidden = !!practice || !!next.current;
  const regularDue = next.planned - next.remembered;
  const recallDue = next.due - regularDue;
  el("start-summary").textContent = regularDue
    ? `还有 ${regularDue} 个词待记忆${recallDue ? " · " + recallDue + " 个易错词待回顾" : ""}`
    : recallDue ? `${recallDue} 个易错词待回顾` : "";
  syncVoteBar();
  if (current) {
    el("word").textContent = current.word;
    el("word").classList.toggle("long-word", current.word.length > 18);
    el("streak").hidden = !!practice;
    const regularProgress = `今日连对 ${current.dailyCount} / ${current.dailyTarget}`;
    const recallProgress = current.recallRetesting
      ? `易错词回顾 · 今日连对 ${current.recallCount || 0} / 3`
      : "易错词回顾 · 答对即通过";
    el("streak").textContent = practice ? "" : (current.recallPending || current.recall)
      ? (current.recall ? recallProgress : `${recallProgress} · 普通复习 ${current.dailyCount} / ${current.dailyTarget}`)
      : regularProgress + " · " + masteryProgress(current.streak, next.settings.goal);
    updateVoice();
  }
  if (!next.current) {
    if (!next.total) empty("词库还是空的", "先添加一批单词，再开始复习。", "add");
    else if (next.total === next.mastered && !next.taskTotal) empty("词库里的单词都掌握了", "可以在词库提前查看，也可以选词重新学习。", "library");
    else empty("今天的任务已完成", "", "", true);
  }
}

function updateReviewControls() {
  const blocked = busy || swiping;
  const waiting = !practice && state?.waitingForNext;
  const concealed = !answerExpanded();
  el("undo").disabled = !state?.canUndo || blocked;
  el("pass").disabled = blocked || concealed || !!waiting;
  el("forgot").disabled = blocked || concealed;
  el("pass-today").disabled = blocked || !!waiting;
}

function masteryProgress(days, goal, mastered = false) {
  if (mastered) return `累计完成 ${days} 天`;
  if (days >= goal) return `累计完成 ${days} 天 · 需一天首答记住`;
  return `累计完成 ${days} / ${goal} 天`;
}

function empty(title, text, action, complete = false) {
  el("review-empty").classList.toggle("is-day-complete", complete);
  el("empty-title").textContent = title;
  message("empty-text", text);
  el("empty-action").hidden = !action;
  if (action) {
    el("empty-action").textContent = {add: "添加单词", library: "打开词库"}[action];
    el("empty-action").onclick = () => changeView(action);
  }
}

function clearAnswer() {
  revealRequest++;
  el("reveal").disabled = false;
  clearTimeout(noteTimer);
  revealed = undefined;
  savedNote = "";
  setAnswerExpanded(false, false);
  el("personal-note").value = "";
  el("note-status").textContent = "";
  el("details").replaceChildren();
  el("card-metadata").replaceChildren();
  fitNote();
  el("details").scrollTop = 0;
  el("card-metadata").parentElement.scrollTop = 0;
  el("personal-note").scrollTop = 0;
}

function group(title, value, fallback, kind = "") {
  const wrapper = document.createElement("div");
  wrapper.className = "detail-group";
  const heading = document.createElement("h3");
  heading.textContent = title;
  const text = document.createElement("p");
  text.className = value ? kind : "placeholder";
  if (value && kind === "example") text.lang = "en";
  text.textContent = value || fallback;
  wrapper.append(heading, text);
  return wrapper;
}

function ipa(text) { return "/" + text.replace(/^\/+|\/+$/g, "") + "/"; }

function cardDetailSections(card, showIPA) {
  const missing = card.lookup === "pending" && state?.settings.online
    ? "等待在线词典补充。" : "暂无资料，可手动编辑。";
  const sound = [];
  if (showIPA) {
    if (card.ipaUK) sound.push("英 " + ipa(card.ipaUK));
    if (card.ipaUS) sound.push("美 " + ipa(card.ipaUS));
    if (!sound.length && card.ipa) sound.push(ipa(card.ipa));
  }
  const meaning = group("中文释义", card.meaning, missing, "meaning");
  if (card.memoryHint) {
    const hint = document.createElement("p");
    hint.className = "construction";
    hint.textContent = "记忆联想：" + card.memoryHint;
    meaning.append(hint);
    if (card.moreMemory) {
      const more = document.createElement("details");
      more.className = "construction construction-details";
      const title = document.createElement("summary");
      title.textContent = "更多联想";
      const content = document.createElement("p");
      content.textContent = card.moreMemory;
      more.append(title, content);
      meaning.append(more);
    }
  }
  const exampleSource = card.exampleBy || "";
  const machine = card.exampleTranslation && (card.exampleTranslationBy === "mymemory" ||
    card.exampleTranslationBy !== "manual" &&
      /^(simple-mt$|tatoeba-mt\||wiktionary-data\||simple-wikipedia\|)/.test(exampleSource));
  const fields = [
    meaning,
    group("英文例句", card.examples, missing, "example"),
    group(machine ? "例句翻译（机译）" : "例句翻译", card.exampleTranslation,
      card.translationPending && state?.settings.online ? "等待联网补译。" : "暂无对应译文。", "translation"),
  ];
  const sources = document.createElement("div");
  sources.className = "sources";
  if (card.ecdict) sources.append(link("ECDICT", "https://github.com/skywind3000/ECDICT"));
  if (/^https:\/\/en\.wiktionary\.org\/wiki\/Special:PermanentLink\/\d+#English$/.test(card.wikiSource || "")) {
    sources.append(link("Wiktionary", card.wikiSource), link("CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0/"));
  }
  if (["simple", "simple-mt", "simple-edited"].includes(card.exampleBy)) {
    sources.append(link("例句 · Simple Wiktionary", "https://simple.wiktionary.org/wiki/" + encodeURIComponent(card.key)),
      link("CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0/"));
  }
  if (/^tatoeba(?:-mt|-online)?\|/.test(exampleSource)) {
    for (const entry of exampleSource.split("|").slice(1)) {
      const [id, ...author] = entry.split(":");
      if (/^\d+$/.test(id)) sources.append(link("例句 · " + author.join(":"), "https://tatoeba.org/en/sentences/show/" + id));
    }
    sources.append(link("CC BY 2.0 FR", "https://creativecommons.org/licenses/by/2.0/fr/"));
  }
  if (exampleSource.startsWith("wiktionary-data|")) {
    sources.append(link("例句 · Wiktionary",
      "https://en.wiktionary.org/wiki/" + encodeURIComponent(exampleSource.split("|")[1]) + "#English"),
      link("CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0/"));
  }
  const article = exampleSource.match(/^(simple-wikipedia|wikipedia-(?:online|context))\|(\d+)$/);
  if (article) {
    const simplified = article[1] === "simple-wikipedia";
    sources.append(link(simplified ? "例句 · Simple Wikipedia" : "例句 · Wikipedia",
      (simplified ? "https://simple.wikipedia.org/" : "https://en.wikipedia.org/") + "?curid=" + article[2]),
      simplified ? link("CC BY-SA 3.0", "https://creativecommons.org/licenses/by-sa/3.0/") :
        link("CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0/"));
  }
  if (card.exampleTranslationBy === "mymemory") {
    sources.append(link("译文 · MyMemory", "https://mymemory.translated.net/"));
  }
  return {fields, sound, sources};
}

function renderDetails(card) {
  const {fields, sound, sources} = cardDetailSections(card, state?.settings.showIPA);
  sources.append(button("编辑卡片资料", async () => {
    try {
      await flushNote();
      await openEditor(card.key);
    } catch (error) { showError(error); }
  }));
  el("details").replaceChildren(...fields);
  el("card-metadata").replaceChildren(...(sound.length ? [group("音标", sound.join(" · "))] : []), sources);
}

async function refreshRevealedDetails() {
  const before = revealed;
  if (!before) return;
  const result = await api("/api/card/" + keyPath(before.key));
  if (revealed === before && (result.lookup !== before.lookup ||
      result.exampleTranslation !== before.exampleTranslation || result.translationPending !== before.translationPending)) {
    revealed = result;
    renderDetails(result);
  }
}

function answerExpanded() { return answerPanel.getAttribute("aria-hidden") === "false"; }

function reviewTiming(token, easing) {
  if (reducedMotion.matches || !swipeContent.animate) return;
  return {options: motionOptions(token, easing), start: document.timeline.currentTime};
}

function playReviewMotion(element, frames, timing, fill = "both") {
  const animation = element.animate(frames, {...timing.options, fill});
  if (timing.start != null) animation.startTime = timing.start;
  return animation;
}

function currentMotionFrame(element) {
  const {transform, opacity} = getComputedStyle(element);
  return {transform, opacity};
}

function wantsVoteBar() {
  return view === "review" && studying && !!state?.current && !practice &&
    (windowsReview || answerExpanded());
}

function settleVoteBar(visible) {
  voteBarVisible = visible;
  voteDock.hidden = !visible;
  voteActions.inert = !visible;
  voteBarAnimation?.cancel();
  voteBarAnimation = undefined;
}

function moveVoteBar(from, to, timing) {
  voteBarAnimation?.cancel();
  voteDock.hidden = false;
  voteBarAnimation = playReviewMotion(voteActions, [from, to], timing);
  return voteBarAnimation;
}

function syncVoteBar(timing) {
  // The flip owns both animation phases while the next card is being prepared.
  if (mobile && swiping) return;
  const visible = wantsVoteBar();
  if (visible === voteBarVisible) return;
  if (!mobile || !timing) {
    settleVoteBar(visible);
    return;
  }
  voteBarVisible = visible;
  voteActions.inert = !visible;
  const from = voteBarAnimation ? currentMotionFrame(voteActions) : visible ? barLowered : barRaised;
  const motion = moveVoteBar(from, visible ? barRaised : barLowered, timing);
  motion.finished.then(() => {
    if (voteBarAnimation === motion) settleVoteBar(visible);
  }).catch(() => {});
}

function setAnswerExpanded(expanded, animate = true) {
  const changed = answerExpanded() !== expanded;
  const timing = changed && animate ? reviewTiming(expanded ? "--motion-answer" : "--motion") : undefined;
  const visible = {opacity: 1, transform: "translateY(0)"};
  const concealed = {opacity: 0, transform: "translateY(-5px)"};
  const from = timing && answerAnimation ? currentMotionFrame(answerPanel) : expanded ? concealed : visible;

  answerAnimation?.cancel();
  answerAnimation = undefined;
  answerPanel.setAttribute("aria-hidden", String(!expanded));
  answerPanel.inert = !expanded;
  el("reveal").setAttribute("aria-expanded", String(expanded));
  el("reveal-label").textContent = expanded ? "收起释义" : "查看中文释义";
  if (timing) {
    answerPanel.hidden = false;
    const motion = playReviewMotion(answerPanel, [from, expanded ? visible : concealed], timing);
    answerAnimation = motion;
    motion.finished.then(() => {
      if (answerAnimation !== motion) return;
      answerPanel.hidden = !expanded;
      motion.cancel();
      answerAnimation = undefined;
    }).catch(() => {});
  } else answerPanel.hidden = !expanded;
  syncVoteBar(timing);
}

async function toggleReveal({animate = true, duringSlide = false} = {}) {
  const current = practice || state?.current;
  const trigger = el("reveal");
  if (!current || (!studying && !practice) || busy || (swiping && !duringSlide) ||
      view !== "review" || trigger.disabled) return;
  if (answerExpanded()) {
    if (!practice && state?.waitingForNext) return;
    if (answerPanel.contains(document.activeElement)) trigger.focus({preventScroll: true});
    setAnswerExpanded(false, animate);
    renderReview();
    return;
  }
  const intent = viewRequest;
  const request = ++revealRequest;
  trigger.disabled = true;
  try {
    if (!revealed) {
      const card = await api("/api/card/" + keyPath(current.key));
      if (view !== "review" || intent !== viewRequest || request !== revealRequest ||
          !sameCard(practice || state?.current, current) || (!studying && !practice) ||
          (swiping && !duringSlide)) return;
      savedNote = card.personalNote;
      el("personal-note").value = savedNote;
      noteClock = Math.max(noteClock, card.noteClock || 0);
      revealed = card;
      renderDetails(card);
    }
    setAnswerExpanded(true, animate);
    fitNote();
    renderReview();
  } catch (error) {
    if (intent === viewRequest && request === revealRequest) showError(error);
  } finally {
    if (request === revealRequest) trigger.disabled = false;
  }
}

function noteDirty() { return !!revealed && el("personal-note").value !== savedNote; }

function fitNote() {
  if ((!mobile && !windowsReview) || CSS.supports("field-sizing", "content") || view !== "review" || !answerExpanded()) return;
  const note = el("personal-note");
  note.style.height = "auto";
  note.style.height = (note.scrollHeight + note.offsetHeight - note.clientHeight) + "px";
}

async function flushNote(background = false) {
  clearTimeout(noteTimer);
  if (!noteDirty()) return;
  const key = revealed.key;
  const value = el("personal-note").value;
  const clock = noteClock;
  let card;
  try {
    card = await api("/api/note/" + keyPath(key), {note: value, clock}, {keepalive: background});
  } catch (error) {
    if (revealed?.key === key && clock === noteClock) {
      el("note-status").textContent = "备注保存失败，请修改或离开编辑框后重试。";
      el("note-status").classList.add("note-error");
    }
    throw error;
  }
  if (revealed?.key === key && clock === noteClock) {
    savedNote = card.personalNote;
    if (savedNote !== value) { el("personal-note").value = savedNote; fitNote(); }
    el("note-status").textContent = "";
    el("note-status").classList.remove("note-error");
    }
}

async function nextCard(beforeSwap) {
  if (!studying || practice || busy || !state?.waitingForNext) return;
  busy = true;
  updateReviewControls();
  try {
    await flushNote();
    const result = await api("/api/review/next", {order: state.current.order});
    if (beforeSwap) await beforeSwap;
    showError();
    applyState(result, true);
    pageScroller().scrollTo({top: 0});
  } catch (error) {
    if (error.status === 409) await refreshState().catch(() => {});
    showError(error);
  } finally {
    busy = false;
    updateReviewControls();
    if (windowsReview && state?.current && !state.waitingForNext) el("reveal").focus({preventScroll: true});
  }
}

async function resumeWaiting(options) {
  if (view !== "review" || practice || !state?.waitingForNext) return;
  studying = true;
  renderReview();
  if (!answerExpanded()) await toggleReveal(options);
}

function noteInput(event) {
  noteClock = Math.max(noteClock + 1, Date.now() * 1000);
  el("note-status").textContent = noteDirty() ? "正在保存…" : "";
  el("note-status").classList.remove("note-error");
  fitNote();
  clearTimeout(noteTimer);
  if (noteDirty() && !event?.isComposing) noteTimer = setTimeout(() => flushNote().catch(showError), 600);
}

async function vote(remembered, {beforeSwap, passToday = false} = {}) {
  if (!studying || practice || !state?.current || busy || (swiping && !beforeSwap) || state.waitingForNext ||
      (passToday ? state.current.recall : !answerExpanded())) return;
  busy = true;
  updateReviewControls();
  showError();
  let completed = false;
  try {
    await flushNote();
    const {current, date} = state;
    const result = await api("/api/vote", {
      key: current.key, reviews: current.reviews, order: current.order, date, remembered, stay: !remembered, passToday,
    });
    if (beforeSwap) await beforeSwap;
    applyState(result, remembered);
    if (remembered) pageScroller().scrollTo({top: 0});
    completed = result.taskCompleted && result.date === date &&
      result.taskDone === result.taskTotal && !result.current;
  } catch (error) {
    if (error.status === 409) await refreshState().catch(() => {});
    showError(error);
  } finally {
    busy = false;
    updateReviewControls();
    if (windowsReview && state?.current && studying) (state.waitingForNext ? el("forgot") : el("reveal")).focus({preventScroll: true});
  }
  return completed;
}

async function undo(beforeSwap) {
  if (view !== "review" || !state?.canUndo || busy || (swiping && !beforeSwap) || practice) return;
  busy = true;
  let revealAfterUndo = false;
  try {
    await flushNote();
    const wasWaiting = state.waitingForNext;
    const result = await api("/api/undo", {});
    if (beforeSwap) await beforeSwap;
    if (beforeSwap && result.current && studying) revealAfterUndo = !result.waitingForNext;
    applyState(result, !wasWaiting);
    if (beforeSwap) pageScroller().scrollTo({top: 0});
  } catch (error) { showError(error); }
  finally { busy = false; updateReviewControls(); }
  const revealOptions = {animate: false, duringSlide: true};
  if (state?.waitingForNext) await resumeWaiting(revealOptions);
  else if (revealAfterUndo && !answerExpanded()) await toggleReveal(revealOptions);
}

function canAdvanceReview() {
  return studying && state?.current && answerExpanded();
}

function sameReview({card, date, waiting}) {
  return date === state?.date && sameCard(card, practice || state.current) &&
    waiting === Boolean(state.waitingForNext);
}

function resetReviewSlide() {
  swipeContent.style.transform = "";
  swipeContent.style.opacity = "";
  swipeCue.style.opacity = "";
}

async function slideReview({offset = 0, reversing = false, passToday = false} = {}) {
  if (view !== "review" || !state || practice || busy || swiping ||
      (reversing ? !state.canUndo : passToday
        ? !studying || !state.current || state.current.recall || state.waitingForNext
        : !canAdvanceReview())) return;
  const {current: card, date, waitingForNext} = state;
  const waiting = Boolean(waitingForNext);
  const direction = reversing ? 1 : -1;
  const exitTiming = reviewTiming("--motion-card-out", "--motion-card-ease");
  const distance = exitTiming && !flashcard.hidden ? swipeContent.clientWidth : 0;
  const barWasVisible = mobile && !voteDock.hidden;
  const emptyPanel = el("review-empty");
  let outgoing, outgoingBar, completed;
  swiping = true;
  updateReviewControls();
  try {
    if (distance) {
      const exit = direction * Math.max(distance, Math.abs(offset) + 24);
      outgoing = playReviewMotion(swipeContent, [
        {transform: `translateX(${offset}px)`, opacity: 1},
        {transform: `translateX(${exit}px)`, opacity: 0},
      ], exitTiming);
      if (barWasVisible) {
        voteActions.inert = true;
        const from = voteBarAnimation ? currentMotionFrame(voteActions) : barRaised;
        // Keep part of the downward travel for the incoming card's phase.
        outgoingBar = moveVoteBar(from, barLeaving, exitTiming);
      }
    }
    const beforeSwap = Promise.allSettled([outgoing?.finished, outgoingBar?.finished]);
    // Run the local request and card exit together. Keep the outgoing frames
    // in place until both finish, even when the request takes longer.
    if (reversing) await undo(beforeSwap);
    else if (waiting) await nextCard(beforeSwap);
    else completed = await vote(true, {beforeSwap, passToday});

    const changed = !sameReview({card, date, waiting});
    const barVisible = wantsVoteBar();
    const incomingTarget = !flashcard.hidden ? swipeContent : changed && !emptyPanel.hidden ? emptyPanel : null;
    const entryTiming = incomingTarget && reviewTiming("--motion-card-in", "--motion-card-ease");
    if (entryTiming) {
      const start = !changed ? currentMotionFrame(incomingTarget) : {
        transform: incomingTarget === emptyPanel ? "translateY(6px)" : `translateX(${-direction * 24}px)`,
        opacity: .55,
      };
      outgoing?.cancel();
      resetReviewSlide();
      const incoming = playReviewMotion(incomingTarget, [start, {transform: "none", opacity: 1}], entryTiming, "none");
      let incomingBar;
      if (mobile && (barWasVisible || barVisible)) {
        const from = voteDock.hidden ? barLowered : currentMotionFrame(voteActions);
        incomingBar = moveVoteBar(from, barVisible ? barRaised : barLowered, entryTiming);
      }
      await Promise.allSettled([incoming.finished, incomingBar?.finished]);
    }
  } finally {
    outgoing?.cancel();
    resetReviewSlide();
    swiping = false;
    settleVoteBar(wantsVoteBar());
    updateReviewControls();
  }
  if (completed && view === "review") window.WordReviewCelebrate(emptyPanel);
}

async function stopReview() {
  if (busy || swiping) return;
  try {
    await flushNote();
    const showLibrary = !!practice;
    if (!showLibrary && studying) applyState(await api("/api/review/stop", {}));
    studying = false;
    practice = undefined;
    clearAnswer();
    renderReview();
    if (showLibrary) await changeView("library");
  } catch (error) { showError(error); }
}

function positionNavigationIndicator() {
  const index = views.indexOf(view);
  if (mobile) {
    navigation.style.setProperty("--nav-index", index);
  } else {
    const active = navigationButtons[index];
    const {offsetWidth, offsetHeight, offsetLeft, offsetTop} = active;
    navigationIndicator.style.width = offsetWidth + "px";
    navigationIndicator.style.height = offsetHeight + "px";
    navigationIndicator.style.transform = `translate(${offsetLeft}px, ${offsetTop}px)`;
  }
  navigation.classList.add("has-indicator");
}

function pageScroller() { return mobile || windowsReview ? shell : window; }

async function displayView(next, {intent, top = 0, prepare, focus} = {}) {
  const previous = view;
  if (intent !== viewRequest) return;
  const update = () => {
    if (intent !== viewRequest) return;
    if (next !== "library") closeWordPreview();
    if (previous !== next && shell.contains(document.activeElement)) document.activeElement.blur();
    view = next;
    document.body.dataset.view = next;
    if (next !== "review") settleVoteBar(false);
    for (const id of views) el(id + "-view").hidden = id !== next;
    prepare?.();
    for (const item of navigationButtons) {
      if (item.dataset.view === next) item.setAttribute("aria-current", "page");
      else item.removeAttribute("aria-current");
    }
    positionNavigationIndicator();
    pageScroller().scrollTo({top});
    focus?.focus({preventScroll: true});
    if (next === "review") fitNote();
  };
  if (previous === next) {
    update();
    return;
  }
  pageTransition?.skipTransition();
  fallbackPageAnimation?.cancel();
  if (listAnimation) {
    listAnimation.cancel();
    listAnimation = undefined;
  }
  if (reducedMotion.matches) {
    update();
    return;
  }
  const direction = Math.sign(views.indexOf(next) - views.indexOf(previous));
  if (!document.startViewTransition) {
    update();
    const offset = mobile ? `translateX(${direction * 12}px)` : `translateY(${direction * 5}px)`;
    fallbackPageAnimation = el(next + "-view").animate?.([
      {opacity: .7, transform: offset}, {opacity: 1, transform: "none"},
    ], motionOptions("--motion-page"));
    return;
  }

  // Only the scroller is captured. Navigation and action buttons stay live.
  const root = document.documentElement;
  shell.classList.add("page-capture");
  root.style.setProperty("--page-shift", mobile ? `${direction * 16}px` : "0px");
  const transition = document.startViewTransition(update);
  pageTransition = transition;
  const cleanup = () => {
    if (pageTransition !== transition) return;
    pageTransition = undefined;
    shell.classList.remove("page-capture");
    root.style.removeProperty("--page-shift");
  };
  transition.ready.catch(() => {}); // A newer tap can supersede the transition before capture.
  transition.finished.then(cleanup, cleanup);
  await transition.updateCallbackDone;
}

function isViewDestination(next, intent) {
  return intent === viewRequest && next === (pendingNavigation?.next ?? view);
}

function refreshViewContent(next) {
  if (next === "library") return refreshLibrary();
  if (next === "stats") return refreshStats();
  if (next === "add" && imported.length) return updateImportStatus();
}

async function changeView(next, temporary) {
  if (swiping || busy) return;
  const staying = next === view && !temporary && !(next === "review" && practice);
  if (!temporary && (pendingNavigation ? next === pendingNavigation.next && !pendingNavigation.temporary : staying)) return;
  const intent = ++viewRequest;
  pendingNavigation = {next, temporary};
  try {
    if (staying) return;
    if (next === "settings") return await openSettings(intent);
    if (view === "settings" && next === settingsFrom && !temporary) return await closeSettings(intent);
    if (view === "settings") {
      commitFocusedSetting();
      await flushSettings();
    }
    await flushNote();
    if (intent !== viewRequest) return;
    controls.close(false);
    const previous = view === "settings" ? settingsFrom : view;
    const transition = displayView(next, {intent, prepare() {
      const wasPractice = !!practice;
      practice = temporary;
      if (wasPractice || practice) clearAnswer();
      showError();
      if (next === "review") renderReview();
    }});
    await Promise.all([transition, refreshViewContent(next)]);
    if (intent !== viewRequest) return;
    if (next === "review") {
      if (previous !== next && studying && !practice) await startReview(intent);
      else await refreshState(intent);
      if (intent === viewRequest) await resumeWaiting();
    }
  } catch (error) {
    if (intent === viewRequest) showError(error);
  } finally {
    if (intent === viewRequest) pendingNavigation = undefined;
  }
}

async function startReview(intent) {
  if (busy) return;
  const manual = intent === undefined;
  const old = state;
  if (manual) {
    intent = viewRequest;
    busy = true;
    el("start-review").disabled = true;
  }
  try {
    const result = await api("/api/review/start", {});
    if (intent !== viewRequest || view !== "review" || (!manual && (state !== old || busy || swiping))) return;
    studying = true;
    practice = undefined;
    applyState(result, manual);
    if (windowsReview && result.current) el("reveal").focus({preventScroll: true});
  } catch (error) {
    if (intent === viewRequest) showError(error);
  } finally {
    if (manual) {
      busy = false;
      el("start-review").disabled = false;
      updateReviewControls();
    }
    if (intent === viewRequest && state?.waitingForNext && !answerExpanded()) await toggleReveal();
  }
}

function englishVoice() {
  if (!window.speechSynthesis) return;
  const voices = speechSynthesis.getVoices().filter(v => v.localService && /^en\b/i.test(v.lang));
  return voices.find(v => v.lang === "en-US") || voices[0];
}
function updateVoice() {
  const native = window.WordReviewFiles;
  el("speak").hidden = native ? !native.canSpeak() : !englishVoice();
}
function speak() {
  if (window.WordReviewFiles) {
    window.WordReviewFiles.speak(el("word").textContent);
    return;
  }
  const voice = englishVoice();
  if (!voice) return;
  const utterance = new SpeechSynthesisUtterance(el("word").textContent);
  utterance.voice = voice;
  utterance.lang = voice.lang;
  speechSynthesis.cancel();
  speechSynthesis.speak(utterance);
}

function libraryQuery() { return {query: el("search").value, filter: el("filter").value, sort: el("sort").value}; }
function selection() { return {...libraryQuery(), all: selectedAll, keys: [...selected]}; }

function showSelection() {
  const count = selectedAll ? listTotal : selected.size;
  el("selection-count").textContent = count ? `已选 ${count} 个` : "";
  el("clear-selection").hidden = count === 0;
  el("batch-action").disabled = count === 0;
  el("select-all").disabled = !listTotal;
  const trash = el("filter").value === "trash";
  for (const option of el("batch-action").options) {
    option.hidden = option.value === "restore" ? !trash : ["reset", "remove", "retry"].includes(option.value) && trash;
  }
  controls.syncSelect(el("batch-action"));
}

function cancelPreviewStart() {
  clearTimeout(previewStart?.timer);
  previewStart = undefined;
}

function hideWordPreview() {
  clearTimeout(previewDismiss);
  preview = undefined;
  if (wordPreview.open) wordPreview.close();
}

function closeWordPreview() {
  cancelPreviewStart();
  hideWordPreview();
}

function delayPreviewClose() {
  const leaving = preview;
  if (!leaving?.point) return;
  clearTimeout(previewDismiss);
  previewDismiss = setTimeout(() => {
    if (preview === leaving) hideWordPreview();
  }, 260);
}

function placeWordPreview({x, y}) {
  const viewport = window.visualViewport;
  const left = viewport?.offsetLeft || 0;
  const top = viewport?.offsetTop || 0;
  const right = left + (viewport?.width || window.innerWidth);
  const bottom = top + (viewport?.height || window.innerHeight);
  const gap = 10;
  const below = bottom - y - 2 * gap;
  const above = y - top - 2 * gap;
  wordPreview.style.setProperty("--preview-width", right - left - 2 * gap + "px");
  wordPreview.style.setProperty("--preview-height", Math.max(below, above) + "px");
  const {width, height} = wordPreview.getBoundingClientRect();
  const showBelow = height <= below || below >= above;
  wordPreview.style.left = Math.max(left + gap, Math.min(x + gap, right - width - gap)) + "px";
  wordPreview.style.top = (showBelow ? y + gap : y - gap - height) + "px";
}

function renderWordPreview(card, progress) {
  const {fields, sound, sources} = cardDetailSections(card, true);
  const timing = document.createElement("p");
  timing.className = "preview-progress";
  timing.textContent = progress;
  const body = el("word-preview-content");
  body.replaceChildren(timing, ...fields, group("我的备注", card.personalNote, "暂无备注。"));
  for (const [label, value] of [["词性", card.pos], ["音标", sound.join(" · ")],
    ["词根和词源", card.etymology], ["搭配", card.collocations], ["记忆提示", card.note]]) {
    if (value) body.append(group(label, value));
  }
  if (sources.childElementCount) body.append(sources);
  el("word-preview-title").textContent = card.word;
}

async function openWordPreview(row, entry, progress, point) {
  if (view !== "library" || !row.isConnected || controls.isOpen() ||
      document.querySelector("dialog[open]:not(#word-preview)")) return;
  closeWordPreview();
  const pending = {row, point, progress};
  preview = pending;
  try {
    const card = await api("/api/card/" + keyPath(entry.key));
    if (preview !== pending || !row.isConnected || view !== "library" ||
        controls.isOpen() || document.querySelector("dialog[open]:not(#word-preview)")) return;
    pending.card = card;
    renderWordPreview(card, progress);
    wordPreview.classList.toggle("floating-preview", !!point);
    if (point) {
      const focus = document.activeElement;
      wordPreview.show();
      placeWordPreview(point);
      if (focus === document.body) document.activeElement.blur();
      else focus?.focus({preventScroll: true});
    } else wordPreview.showModal();
    el("word-preview-content").scrollTop = 0;
  } catch (error) {
    if (preview === pending) {
      hideWordPreview();
      showError(error);
    }
  }
}

function addWordPreview(target, row, entry, progress) {
  const arm = (event, hover) => {
    cancelPreviewStart();
    const start = {row, target, id: event.pointerId, hover, x: event.clientX, y: event.clientY};
    start.timer = setTimeout(() => {
      if (previewStart === start && (!hover || target.matches(":hover")) && !window.getSelection()?.toString()) {
        openWordPreview(row, entry, progress, hover ? {x: start.x, y: start.y} : undefined);
      }
      if (previewStart === start) cancelPreviewStart();
    }, hover ? 650 : 475);
    previewStart = start;
  };
  target.addEventListener("pointerenter", event => {
    if (mobile || event.pointerType !== "mouse" || event.buttons) return;
    if (preview?.row === row) clearTimeout(previewDismiss);
    else arm(event, true);
  });
  target.addEventListener("pointerdown", event => {
    cancelPreviewStart();
    if (mobile && event.pointerType === "touch" && event.isPrimary) arm(event, false);
  });
  target.addEventListener("pointermove", event => {
    const start = previewStart;
    if (start?.row !== row || event.pointerId !== start.id) return;
    if (start.hover) {
      start.x = event.clientX;
      start.y = event.clientY;
    } else if (Math.hypot(event.clientX - start.x, event.clientY - start.y) > 10) {
      cancelPreviewStart();
    }
  });
  for (const type of ["pointerup", "pointercancel", "pointerleave"]) target.addEventListener(type, event => {
    if (previewStart?.row === row && previewStart.id === event.pointerId) cancelPreviewStart();
    if (type === "pointerleave" && preview?.row === row && !wordPreview.contains(event.relatedTarget)) delayPreviewClose();
  });
}

function addWordActions(row, entry, progress) {
  const edit = () => openEditor(entry.key).catch(showError);
  const act = name => () => batchAction(name, {keys: [entry.key]}, 1);
  const restore = act("restore");
  const items = [
    {label: "预览词条", icon: "review", pick: () => openWordPreview(row, entry, progress)},
    {label: "学习历史", icon: "history", pick: () => openHistory(entry).catch(showError)},
  ];
  if (entry.deleted) items.push({label: "恢复", icon: "backup", pick: restore});
  else {
    items.push({label: "编辑", icon: "edit", pick: edit});
    if (entry.mastered) items.push(
      {label: "提前复习", icon: "review", pick: () => changeView("review", entry)},
      {label: "重新学习", icon: "history", pick: act("reset")});
    items.push({label: "移入回收站", icon: "trash", danger: true, pick: act("remove")});
  }
  const actions = document.createElement("div");
  actions.className = "row-actions";
  actions.append(entry.deleted ? button("恢复", restore) : button("编辑", edit));
  const more = document.createElement("button");
  more.type = "button";
  more.className = "icon-button";
  more.setAttribute("aria-label", entry.word + " 的更多操作");
  more.setAttribute("aria-haspopup", "menu");
  more.setAttribute("aria-controls", "popup-list");
  more.setAttribute("aria-expanded", "false");
  more.append(controls.icon("more"));
  const show = point => {
    closeWordPreview();
    controls.context(more, entry.word, items, point);
  };
  more.onclick = () => show();
  more.onkeydown = event => {
    if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
      event.preventDefault();
      show();
    }
  };
  const acceptsMenu = target => !target.closest("input, button, a, textarea, [contenteditable], .selection-target");
  row.addEventListener("contextmenu", event => {
    if (!acceptsMenu(event.target)) return;
    event.preventDefault();
    if (mobile && event.pointerType !== "mouse") return;
    show(event.clientX || event.clientY ? {x: event.clientX, y: event.clientY} : undefined);
  });
  actions.append(more);
  return actions;
}

async function refreshLibrary(animate = false) {
  const requestID = ++listRequest;
  const intent = viewRequest;
  const data = await api("/api/cards?" + new URLSearchParams({...libraryQuery(), page: listPage}));
  if (requestID !== listRequest || !isViewDestination("library", intent)) return;
  if (listPage && !data.rows.length) {
    listPage = Math.max(0, Math.ceil(data.total / 24) - 1);
    return refreshLibrary(animate);
  }
  listTotal = data.total;
  showSelection();
  el("lookup-bar").hidden = !data.pending;
  el("lookup-status").textContent = !data.online ? `有 ${data.pending} 个词待补充，联网查词已关闭。`
    : data.paused ? `在线词典暂时无法连接，${data.pending} 个词等待重试。` : `正在补充 ${data.pending} 个词。`;
  el("retry-all").hidden = !data.online || !data.paused;
  const signature = JSON.stringify([data.rows, data.total, data.goal, listPage, selectedAll, [...selected]]);
  if (signature === listSignature) return;
  listSignature = signature;
  closeWordPreview();
  const rows = document.createDocumentFragment();
  if (!data.rows.length) {
    const paragraph = document.createElement("p");
    paragraph.textContent = el("search").value || el("filter").value ? "没有符合条件的单词。" : "还没有添加单词。";
    rows.append(paragraph);
  }
  for (const entry of data.rows) {
    const row = document.createElement("article");
    row.className = "word-row";
    const check = document.createElement("input");
    check.type = "checkbox";
    check.checked = selectedAll || selected.has(entry.key);
    check.disabled = selectedAll;
    check.setAttribute("aria-label", "选择 " + entry.word);
    check.onchange = () => {
      if (check.checked) selected.add(entry.key); else selected.delete(entry.key);
      showSelection();
      listSignature = undefined;
    };
    const checkTarget = document.createElement("label");
    checkTarget.className = "selection-target";
    checkTarget.append(check);
    const main = document.createElement("div");
    main.className = "word-main";
    const title = document.createElement("h3");
    const spelling = document.createElement("span");
    spelling.lang = "en";
    spelling.textContent = entry.word;
    const marker = document.createElement("span");
    marker.className = "word-status" + (entry.deleted ? " trash" : entry.mastered ? " mastered" : "");
    marker.textContent = entry.deleted ? "回收站" : entry.mastered ? (entry.legacyMastered ? "已掌握 · 旧规则" : "已掌握") : entry.lastTry ? "学习中" : "待复习";
    title.append(spelling, marker);
    const meaning = document.createElement("p");
    meaning.textContent = entry.meaning || (entry.lookup === "pending" ? "等待词典补充" : "暂无中文释义");
    const timing = document.createElement("small");
    const progress = entry.legacyMastered ? `旧规则连续 ${entry.legacyStreak || 0} 次` :
      masteryProgress(entry.streak, data.goal, entry.mastered);
    const legacy = entry.legacyStreak && !entry.legacyMastered ? ` · 旧规则记录 ${entry.legacyStreak} 次` : "";
    timing.textContent = `${progress}${legacy} · 最近复习 ${entry.lastTry || "—"}`;
    main.append(title, meaning, timing);
    addWordPreview(main, row, entry, timing.textContent);
    row.append(checkTarget, main, addWordActions(row, entry, timing.textContent));
    rows.append(row);
  }
  const list = el("word-list");
  list.replaceChildren(rows);
  el("results").textContent = data.total ? `共 ${data.total} 个 · 第 ${listPage + 1} 页` : "共 0 个";
  el("prev-page").disabled = listPage === 0;
  el("next-page").disabled = (listPage + 1) * 24 >= data.total;
  if (animate === true && !pageTransition && !reducedMotion.matches && list.animate) {
    listAnimation?.cancel();
    listAnimation = list.animate([{opacity: .7}, {opacity: 1}], motionOptions("--motion-panel"));
  }
}

function clearSelection() { selected.clear(); selectedAll = false; }
function runSearch() {
  clearSelection();
  listPage = 0;
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => refreshLibrary(true).catch(showError), 130);
}
function usePage(delta) {
  listPage += delta;
  refreshLibrary(true).catch(showError);
}

function ask(title, text, label, perform, danger = false) {
  confirmation = perform;
  el("action-title").textContent = title;
  el("action-text").textContent = text;
  el("action-confirm").textContent = label;
  el("action-confirm").classList.toggle("danger", danger);
  message("action-status");
  el("action-dialog").showModal();
}

async function confirmAction(event) {
  event.preventDefault();
  const confirm = el("action-confirm");
  confirm.disabled = true;
  try {
    await confirmation();
    el("action-dialog").close();
  } catch (error) { message("action-status", error.message); }
  finally { confirm.disabled = false; }
}

function batchAction(action, items, count) {
  const actions = {
    remove: ["移入回收站", "词条、备注和学习历史会一并移入回收站，可以恢复。"],
    reset: ["重新学习", "当天连对次数和累计完成天数会清零，备注和历史保留。"],
    restore: ["恢复词条", "卡片会恢复到词库，保留移入回收站前的学习进度。"],
    retry: ["联网补缺", "词典只填写空白字段。"],
  };
  if (action === "export") return exportCSV(items).catch(showError);
  if (!actions[action]) return;
  const [label, explain] = actions[action];
  ask(`${label} · ${count} 个单词`, explain, label, async () => {
    await api("/api/batch/" + action, items);
    clearSelection();
    await refreshLibrary();
    await refreshState();
  }, action === "remove" || action === "reset");
}

async function exportCSV(items) {
  if (windowsReview) {
    await api("/api/export/windows", items);
    return;
  }
  const {filename} = await api("/api/export/filename");
  if (window.WordReviewFiles) {
    window.WordReviewFiles.pickFile("csv", JSON.stringify({filename, selection: items}));
    return;
  }
  const response = await request("/api/export", items);
  if (!response.ok) throw new Error("无法导出 CSV，请重试。");
  const url = URL.createObjectURL(await response.blob());
  const item = document.createElement("a");
  item.href = url;
  item.download = filename;
  item.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

async function openHistory(entry) {
  const history = await api("/api/history/" + keyPath(entry.key));
  const answers = history.filter(row => !row.undone && !row.practice && (row.kind === "yes" || row.kind === "no"));
  const wrong = answers.filter(row => row.kind === "no").length;
  el("history-title").textContent = entry.word + " · 学习历史";
  el("history-answer-count").textContent = `答错 ${wrong} / ${answers.length} 次`;
  const items = {
    add: "添加单词", reimport: "因重复录入重新学习", reset: "手动重新学习",
    yes: "记住了", no: "没记住", remove: "移入回收站", restore: "恢复到词库",
  };
  el("history-list").replaceChildren(...history.map(row => {
    const item = document.createElement("li");
    const rule = row.firstTryMastered ? "（首次记住即掌握）" : row.legacy ? "（旧规则）" : row.recall ? "（易错词回顾）" : row.dayPassed ? (row.credit ? "（完成今日记忆）" : "（当天已计入）") : "";
    const suffix = rule + (row.undone ? "（已撤销）" : "");
    item.textContent = [row.day, items[row.kind] + suffix].join(" · ");
    return item;
  }));
  el("history-dialog").showModal();
}

async function openEditor(key) {
  closeWordPreview();
  editing = await api("/api/card/" + keyPath(key));
  el("edit-title").textContent = "编辑 " + editing.word;
  for (const field of editFields) el("edit-" + field).value = editing[field] || "";
  el("retry-one").hidden = !!(editing.meaning && editing.pos && editing.examples && editing.exampleTranslation && editing.etymology);
  el("retry-one").disabled = !state?.settings.online;
  message("edit-status");
  el("edit-dialog").showModal();
}

function editedFields(commitWord) {
  const updates = {};
  if (!editing) return updates;
  for (const field of editFields) {
    if (field === "word" && !commitWord) continue;
    const input = el("edit-" + field);
    if (input.value !== (editing[field] || "")) updates[field] = input.value;
  }
  return updates;
}

async function flushEdit(commitWord = false, background = false) {
  clearTimeout(editTimer);
  while (editJob) await editJob;
  if (!editing || !el("edit-dialog").open) return;
  const updates = editedFields(commitWord);
  if (!Object.keys(updates).length) return;
  if (updates.word !== undefined && !el("edit-word").reportValidity()) {
    throw new Error("请输入有效的英文拼写。");
  }
  const key = editing.key;
  const submittedTranslation = el("edit-exampleTranslation").value;
  const job = (async () => {
    const card = await api("/api/card/" + keyPath(key), updates, {keepalive: background && JSON.stringify(updates).length < 16000});
    editing = card;
    const translation = el("edit-exampleTranslation");
    if (translation.value === submittedTranslation && card.exampleTranslation !== submittedTranslation) {
      translation.value = card.exampleTranslation || "";
    }
    if (updates.word !== undefined && el("edit-word").value === updates.word) el("edit-word").value = card.word;
    el("edit-title").textContent = "编辑 " + card.word;
    if (revealed?.key === key) {
      revealed = card;
      noteClock = Math.max(noteClock, card.noteClock || 0);
      savedNote = card.personalNote;
      el("personal-note").value = savedNote;
      fitNote();
      renderDetails(card);
    }
    if (practice?.key === key) practice = card;
    message("edit-status");
  })();
  editJob = job;
  try { await job; }
  finally { if (editJob === job) editJob = undefined; }
}

function editError(error) { message("edit-status", error.message); }

async function closeEditor() {
  try {
    await flushEdit(true);
    el("edit-dialog").close();
    editing = undefined;
    if (view === "library") await refreshLibrary();
    await refreshState();
  } catch (error) { editError(error); }
}

async function retryOne() {
  try {
    await api("/api/batch/retry", {keys: [editing.key]});
    message("edit-status");
    el("retry-one").disabled = true;
  } catch (error) { message("edit-status", error.message); }
}

async function submitWords(event) {
  event.preventDefault();
  const input = el("add-input");
  const payload = {text: input.value, format: fileFormat};
  el("add-submit").disabled = true;
  try {
    const plan = await api("/api/import-preview", payload);
    const numbers = `解析 ${plan.total} 项 · 新增 ${plan.new} · 重置 ${plan.reset} · 批内重复 ${plan.duplicates} · 无效 ${plan.invalid.length}`;
    if (!plan.new && !plan.reset) {
      message("import-status", numbers + (plan.invalid.length ? "\n请检查：" + plan.invalid.slice(0, 8).join("、") : ""));
      return;
    }
    const parts = [numbers];
    if (plan.reset) parts.push("将重新学习：" + plan.resetWords.join("、") + (plan.reset > plan.resetWords.length ? " 等" : ""),
      "复习进度清零，资料、备注和历史保留。");
    if (plan.invalid.length) parts.push("将跳过无效项：" + plan.invalid.slice(0, 8).join("、"));
    ask("确认批量导入", parts.join("\n"), "确认导入", async () => {
      const result = await api("/api/import", {...payload, token: plan.token});
      imported = result.keys;
      if (input.value === payload.text) {
        input.value = "";
        fileFormat = "words";
      }
      message("import-status");
      await updateImportStatus();
      await refreshState();
    });
  } catch (error) { message("import-status", error.message); }
  finally { el("add-submit").disabled = false; }
}

async function updateImportStatus() {
  if (!imported.length) return;
  const keys = imported;
  const data = await api("/api/import-status", {keys});
  if (keys !== imported) return;
  importPending = data.pending > 0;
  if (!data.pending && !data.partial) {
    message("import-lookup");
    return;
  }
  let label = [data.partial && `资料缺项 ${data.partial}`, data.pending && `等待补全 ${data.pending}`].filter(Boolean).join(" · ");
  if (data.paused && importPending) label += "。暂时连不上在线词典，稍后会重试。";
  if (!data.online) label += "。联网补充已关闭。";
  message("import-lookup", label);
}

async function readWordFile(event) {
  const file = event.target.files[0];
  event.target.value = "";
  if (!file) return;
  if (file.size > 4 << 20) {
    message("import-status", "词表文件过大，请拆成多批。");
    return;
  }
  try {
    const value = new TextDecoder("utf-8", {fatal: true}).decode(await file.arrayBuffer());
    const extension = file.name.toLowerCase().split(".").pop();
    fileFormat = ["csv", "json"].includes(extension) ? extension : "words";
    el("add-input").value = value;
    message("import-status");
  } catch { message("import-status", "请将词表另存为 UTF-8 编码，再重新读取。"); }
}

async function refreshStats() {
  const intent = viewRequest;
  const data = await api("/api/stats");
  if (!isViewDestination("stats", intent)) return;
  const today = data.days[data.days.length - 1];
  const stats = [
    ["历史累计录入不同词", data.imported], ["当前词库", data.current], ["已掌握", data.mastered],
    ["未掌握", data.unmastered], ["回收站", data.trashed], ["今天复习的不同词", today.distinct], ["今日已完成的词", today.passed],
    ["今天记住操作", today.yes], ["今天没记住操作", today.no], ["累计复习操作", data.totalReviews],
    ["累计没记住操作", data.totalForgot], ["累计重新录入", data.totalReimport],
  ];
  const counts = el("stats-counts");
  for (const [index, [label, value]] of stats.entries()) {
    let amount = counts.children[index]?.lastElementChild;
    if (!amount) {
      const item = document.createElement("div");
      const title = document.createElement("span");
      amount = document.createElement("strong");
      title.textContent = label;
      item.append(title, amount);
      counts.append(item);
    }
    if (amount.textContent !== String(value)) amount.textContent = value;
  }
  window.WordReviewChart.update(data.days);
}

function customLimit() {
  const custom = el("daily-limit").value === "custom";
  el("custom-limit").hidden = !custom;
  el("custom-limit").required = custom;
  if (!custom) el("custom-limit").removeAttribute("aria-invalid");
}

function renderStudyRollover() {
  el("study-rollover-display").textContent = el("study-rollover").value || "选择时间";
}

function renderExamDate() {
  const date = el("exam-date");
  const [, month, day] = date.value.split("-").map(Number);
  const fixed = el("exam-mode").value === "fixed";
  const label = !date.value ? "选择日期" : fixed ? date.value : `${month} 月 ${day} 日`;
  el("exam-date-display").textContent = label;
  date.title = label;
}

function commitExamDate() {
  const field = el("exam-date");
  renderExamDate();
  if (!field.value || !field.validity.valid) {
    field.setAttribute("aria-invalid", "true");
    message("study-settings-status", "请输入有效的考试日期。此前的日期已保留。");
    return;
  }
  field.removeAttribute("aria-invalid");
  const date = el("exam-mode").value === "fixed" ? field.value : field.value.slice(5);
  message("study-settings-status");
  settingsDraft.examDate = date === "12-20" ? undefined : date;
}

async function openSettings(intent) {
  await flushNote();
  if (!state) await refreshState(intent);
  if (intent !== viewRequest) return;
  const prefs = state.settings;
  settingsDraft = {...prefs};
  settingsSaved = {...prefs};
  el("daily-limit").value = [0, 10, 20, 40, 100, 200].includes(prefs.limit) ? String(prefs.limit) : "custom";
  el("custom-limit").value = prefs.limit || 20;
  el("goal").value = prefs.goal;
  el("daily-goal").value = prefs.dailyGoal;
  el("first-try-mastery").checked = !!prefs.firstTryMastery;
  el("study-rollover").value = prefs.rollover || "05:00";
  renderStudyRollover();
  el("online").checked = prefs.online;
  el("show-ipa").checked = prefs.showIPA;
  const fixed = prefs.examDate?.length === 10;
  el("exam-mode").value = fixed ? "fixed" : "annual";
  el("exam-date").value = fixed ? prefs.examDate : state.exam.date;
  renderExamDate();
  customLimit();
  controls.syncSelect(el("daily-limit"));
  controls.syncSelect(el("exam-mode"));
  message("settings-status");
  message("study-settings-status");
  for (const input of el("settings-form").querySelectorAll("[aria-invalid]")) input.removeAttribute("aria-invalid");
  controls.close(false);
  settingsFrom = view;
  settingsOffset = (mobile || windowsReview) ? shell.scrollTop : window.scrollY;
  await Promise.all([
    displayView("settings", {intent, focus: el("settings-title")}),
    refreshRecovery(intent),
  ]);
}

async function closeSettings(intent) {
  commitFocusedSetting();
  await flushSettings();
  if (intent !== viewRequest) return;
  controls.close(false);
  // Keep the current card and scroll position when returning from settings.
  const next = settingsFrom;
  const focus = next === "review" && !el("flashcard").hidden ? el("reveal")
    : navigationButtons.find(button => button.dataset.view === next);
  await Promise.all([
    displayView(next, {intent, top: settingsOffset, focus}),
    refreshViewContent(next),
  ]);
  if (intent === viewRequest && next === "review") await resumeWaiting();
}

function commitSetting(input) {
  if (view !== "settings" || !settingsDraft) return;
  if (input.id === "study-rollover") {
    renderStudyRollover();
    if (!input.value || !input.validity.valid) {
      input.setAttribute("aria-invalid", "true");
      message("study-settings-status", "请输入有效的学习日结束时间。");
      return;
    }
    input.removeAttribute("aria-invalid");
    message("study-settings-status");
    settingsDraft.rollover = input.value === "05:00" ? undefined : input.value;
    return;
  }
  if (input.id === "exam-mode" || input.id === "exam-date") {
    commitExamDate();
    return;
  }
  if (input.id === "daily-limit") {
    customLimit();
    if (input.value === "custom") {
      el("custom-limit").focus();
      el("custom-limit").select();
    } else settingsDraft.limit = Number(input.value);
    return;
  }
  const key = {
    "custom-limit": "limit", "daily-goal": "dailyGoal", goal: "goal",
    "first-try-mastery": "firstTryMastery", "show-ipa": "showIPA", online: "online",
  }[input.id];
  if (!key || (input.id === "custom-limit" && input.hidden)) return;
  if (!input.validity.valid || (input.type === "number" && input.value === "")) {
    input.setAttribute("aria-invalid", "true");
    message("study-settings-status", "请输入范围内的整数。当前设置已保留。");
    return;
  }
  input.removeAttribute("aria-invalid");
  settingsDraft[key] = input.type === "checkbox" ? input.checked : Number(input.value);
}

function commitFocusedSetting() {
  const field = document.activeElement;
  if (view === "settings" && ["number", "date", "time"].includes(field?.type)) commitSetting(field);
}

function hasSettingChanges() {
  return settingsDraft && Object.keys(settingsDraft).some(key => settingsDraft[key] !== settingsSaved[key]);
}

async function flushSettings(background = false) {
  while (settingsJob) await settingsJob;
  if (!hasSettingChanges()) return;
  const snapshot = {...settingsDraft};
  const job = (async () => {
    const next = await api("/api/settings", snapshot, {keepalive: background});
    settingsSaved = snapshot;
    applyState(next);
    message("study-settings-status");
  })();
  settingsJob = job;
  try { await job; }
  catch (error) { message("study-settings-status", error.message); throw error; }
  finally { if (settingsJob === job) settingsJob = undefined; }
}

async function showRestored() {
  studying = false;
  practice = undefined;
  clearAnswer();
  clearSelection();
  listPage = 0;
  listSignature = undefined;
  imported = [];
  importPending = false;
  settingsDraft = settingsSaved = undefined;
  settingsOffset = 0;
  el("action-dialog").close();
  await refreshState();
  await changeView("review");
}

function showTransferPreview(data) {
  const created = new Intl.DateTimeFormat("zh-CN", {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
  }).format(new Date(data.createdAt));
  const overview = `文件生成时间（本机时间） ${created}
导出端学习日 ${data.sourceDay} · 本机学习日 ${data.deviceDay}
词条 ${data.words} 条 · 已掌握 ${data.mastered} 个 · 有备注 ${data.notes} 条 · 学习记录 ${data.events} 条`;
  if (data.blocked) {
    message("settings-status", overview + `
` + data.blocked);
    return;
  }
  const plan = data.continuation
    ? `继续当天任务，已完成 ${data.passed} 个词，还有 ${data.pending} 个词待记忆。`
    : "文件来自较早的学习日。复习会按本机当前日期安排。";
  ask("覆盖当前学习数据？", `${overview}
${plan}
确认后，程序会先备份本机数据，再完整替换词库、进度、设置和历史。`,
    "确认覆盖", async () => {
      await api("/api/transfer/commit", {token: data.token});
      await showRestored();
    }, true);
}

async function refreshRecovery(intent) {
  const data = await api("/api/transfer/recovery");
  if (isViewDestination("settings", intent)) el("transfer-recovery").hidden = !data.available;
}

async function pickTransfer(kind) {
  const opening = kind === "import";
  message("settings-status");
  if (window.WordReviewFiles) {
    window.WordReviewFiles.pickFile(kind, "");
    return;
  }
  const trigger = el("transfer-" + kind);
  trigger.disabled = true;
  try {
    const path = opening ? "open" : kind === "recovery" ? "recovery" : "save";
    const data = await api("/api/transfer/windows/" + path, {});
    if (opening && !data.cancelled) showTransferPreview(data);
  } catch (error) { message("settings-status", error.message); }
  finally { trigger.disabled = false; }
}

if (mobile) {
  el("transfer-guide").textContent = "在「导出数据」中分享或保存 .lexi 文件，再到另一台设备导入。";
  window.wordReviewFlushNote = flushOnHide;
  window.wordReviewResume = () => refreshState().catch(showError);
  window.wordReviewSpeechChanged = updateVoice;
  window.wordReviewBack = () => {
    if (controls.close()) return true;
    const dialogs = all("dialog[open]");
    const last = dialogs[dialogs.length - 1];
    if (last) {
      if (last.id === "edit-dialog") closeEditor();
      else if (last.id !== "action-dialog" || !el("action-confirm").disabled) last.close();
      return true;
    }
    if (view === "settings") { changeView(settingsFrom); return true; }
    if (practice || studying && view === "review") {
      stopReview();
      return true;
    }
    if (view !== "review") {
      changeView("review");
      return true;
    }
    return false;
  };
  window.wordReviewFileResult = (kind, data, error) => {
    const target = kind === "csv" ? "error" : "settings-status";
    if (error) {
      message(target, error);
      return;
    }
    if (!data) return;
    try {
      const result = JSON.parse(data);
      if (result.cancelled) return;
      if (kind === "import") {
        changeView("settings").then(() => {
          if (view === "settings") showTransferPreview(result);
        });
      }
    } catch (e) { message(target, e.message); }
  };
}

async function restoreFile(event) {
  const file = event.target.files[0];
  event.target.value = "";
  if (!file) return;
  try {
    if (file.size > 124 << 20) throw new Error("备份文件过大。");
    const backup = JSON.parse(await file.text());
    ask("覆盖当前词库？", "恢复会替换现有词条、备注、学习历史和设置。程序会先在本地自动保存一份恢复前备份。", "确认覆盖", async () => {
      await api("/api/restore", backup);
      await showRestored();
    }, true);
  } catch (error) { message("settings-status", error instanceof SyntaxError ? "这个文件不是有效的 JSON 备份。" : error.message); }
}

async function refreshStorage() {
  const data = await api("/api/storage");
  el("storage-path").textContent = mobile ? "应用私有存储" : data.database;
  message("storage-notice", data.notice);
}

function reviewKeyAllowed(event) {
  const target = event.target;
  return !event.defaultPrevented && !event.isComposing && event.keyCode !== 229 &&
    !event.ctrlKey && !event.altKey && !event.metaKey && view === "review" &&
    studying && !practice && !busy && !swiping && state?.current &&
    !controls.isOpen() && !document.querySelector("dialog[open]") && target instanceof Element &&
    !target.isContentEditable &&
    (!target.closest("input,textarea,select,button,a[href],summary,[contenteditable]") ||
     target === el("reveal") || target.closest("#vote-actions"));
}

function keyboard(event) {
  if (event.key === "Escape" && (preview || previewStart) && !controls.isOpen()) {
    event.preventDefault();
    closeWordPreview();
    return;
  }
  if (view === "settings" && event.key === "Escape" && !controls.isOpen() && !document.querySelector("dialog[open]")) {
    event.preventDefault();
    changeView(settingsFrom);
    return;
  }
  if (windowsReview && event.key === "Enter" && reviewKeyAllowed(event)) {
    event.preventDefault();
    if (!event.repeat && !event.shiftKey && !el("reveal").disabled && !answerExpanded()) toggleReveal();
    return;
  }
  if (view !== "review" || event.repeat || event.altKey || event.isComposing || event.keyCode === 229 ||
      event.defaultPrevented || !(event.target instanceof Element) || event.target.isContentEditable ||
      event.target.closest("textarea,input,select,[contenteditable]") || controls.isOpen() || document.querySelector("dialog[open]")) return;
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
    event.preventDefault(); slideReview({reversing: true}); return;
  }
  if (event.ctrlKey || event.metaKey) return;
  if (event.code === "Space" && !event.target.closest("button,a,.meaning-panel,.memory-panel")) {
    event.preventDefault(); toggleReveal();
  }
  const voteKeys = windowsReview ? ["1", "2"] : ["1", "2", "ArrowLeft", "ArrowRight"];
  if (voteKeys.includes(event.key) && !voteDock.hidden && answerExpanded()) {
    event.preventDefault();
    const remembered = event.key === "2" || event.key === "ArrowRight";
    if (remembered) {
      if (!state.waitingForNext) slideReview();
    } else if (state.waitingForNext) slideReview();
    else vote(false);
  }
}

if (windowsReview || mobile) document.body.append(voteDock);
if (windowsReview && !CSS.supports("field-sizing", "content")) {
  let noteWidth;
  new ResizeObserver(([entry]) => {
    if (entry.contentRect.width === noteWidth) return;
    noteWidth = entry.contentRect.width;
    fitNote();
  }).observe(el("personal-note").parentElement);
}

for (const form of all("form[data-validation]")) {
  const status = el(form.dataset.validation);
  form.addEventListener("invalid", event => {
    event.preventDefault();
    const input = event.target;
    input.setAttribute("aria-invalid", "true");
    if (input !== form.querySelector(":invalid")) return;
    const name = input.getAttribute("aria-label") || input.labels?.[0]?.textContent.trim() || "该字段";
    const detail = input.validity.valueMissing ? "不能为空。"
      : input.type === "number" ? `应为 ${input.min} 到 ${input.max} 之间的整数。` : "的内容格式无效。";
    status.setAttribute("data-invalid", "");
    message(status.id, name + detail);
    input.focus();
  }, true);
  form.addEventListener("input", event => {
    if (event.target.hasAttribute("aria-invalid") && event.target.validity.valid) {
      event.target.removeAttribute("aria-invalid");
    }
    if (status.hasAttribute("data-invalid") && !form.querySelector("[aria-invalid]")) {
      status.removeAttribute("data-invalid");
      message(status.id);
    }
  });
}

if (mobile) {
  const textField = "textarea, input[type=text], input[type=search], input[type=number], [contenteditable]";
  const viewport = window.visualViewport;
  let nativeKeyboard;
  let fullHeight = viewport?.height || window.innerHeight;
  let editingFrame, visibleHeight;
  const updateEditing = () => {
    const editing = document.activeElement?.matches(textField);
    const height = viewport?.height || window.innerHeight;
    if (!editing && !nativeKeyboard) fullHeight = height;
    else fullHeight = Math.max(fullHeight, height);
    const shown = nativeKeyboard ?? (editing && fullHeight - height > 95);
    document.body.classList.toggle("editing-input", Boolean(shown));
    if (visibleHeight !== height) {
      visibleHeight = height;
      document.documentElement.style.setProperty("--visible-height", height + "px");
    }
  };
  const scheduleEditing = () => {
    if (editingFrame) return;
    editingFrame = requestAnimationFrame(() => { editingFrame = undefined; updateEditing(); });
  };
  window.wordReviewKeyboardChanged = shown => { nativeKeyboard = shown; scheduleEditing(); };
  document.addEventListener("focusin", scheduleEditing);
  document.addEventListener("focusout", scheduleEditing);
  viewport?.addEventListener("resize", scheduleEditing);
  window.addEventListener("resize", () => { scheduleEditing(); fitNote(); });
  updateEditing();
}

if (mobile) {
  let drag;
  let frame;
  let suppressClickUntil = 0;
  const editingTarget = "input,textarea,select,[contenteditable]";
  const canSwipe = () => view === "review" && state && !busy && !swiping &&
    !document.activeElement?.matches(editingTarget) &&
    !document.querySelector("dialog[open]") && !controls.isOpen() && !window.getSelection()?.toString();
  const canRevealBySwipe = () => cardFront.hasAttribute("data-swipe-reveal") && !el("reveal").disabled;
  const stillOnCard = () => drag.intent === viewRequest && sameReview(drag);
  const offsetFor = (action, dx, dy) => action === "reveal" ? Math.max(0, dy)
    : action === "undo" ? Math.max(0, dx) : Math.min(0, dx);

  function cancelDrag() {
    if (!drag) return;
    cancelAnimationFrame(frame);
    const x = drag.action === "reveal" ? 0 : drag.offset;
    drag = undefined;
    resetReviewSlide();
    if (x && !flashcard.hidden) {
      const timing = reviewTiming("--motion-card-in", "--motion-card-ease");
      if (timing) playReviewMotion(swipeContent, [
        {transform: `translateX(${x}px)`}, {transform: "none"},
      ], timing, "none");
    }
  }

  document.addEventListener("click", event => {
    if (event.detail && performance.now() < suppressClickUntil) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
  document.addEventListener("pointerdown", event => {
    if (event.pointerType !== "touch") return;
    if (!event.isPrimary) { cancelDrag(); return; }
    suppressClickUntil = 0;
    if (drag || !canSwipe() || !shell.contains(event.target) || event.target.closest(editingTarget)) return;
    const reveal = canRevealBySwipe() && cardFront.contains(event.target) &&
      !event.target.closest("button:not(#reveal), a");
    const forward = !practice && swipeContent.contains(event.target) && event.clientX >= 24 &&
      innerWidth - event.clientX >= 24 && canAdvanceReview();
    const undo = !practice && state.canUndo;
    if (!forward && !undo && !reveal) return;
    drag = {id: event.pointerId, x: event.clientX, y: event.clientY, offset: 0,
      forward, undo, reveal, card: practice || state.current, date: state.date,
      waiting: Boolean(state.waitingForNext), intent: viewRequest};
  });
  document.addEventListener("pointermove", event => {
    if (drag?.id !== event.pointerId) return;
    if (!canSwipe() || !stillOnCard()) { cancelDrag(); return; }
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    const x = Math.abs(dx);
    const y = Math.abs(dy);
    if (!drag.action) {
      if (drag.reveal && dy >= 12 && dy >= x * 1.35) {
        drag.action = "reveal";
        drag.threshold = 48;
        el("swipe-label").textContent = "查看中文释义";
        el("swipe-icon").setAttribute("href", "/icons.svg#down");
      } else if (x >= 12 && x >= y * 1.4) {
        const reversing = dx > 0;
        if (reversing ? !drag.undo : !drag.forward) { cancelDrag(); return; }
        drag.action = reversing ? "undo" : "forward";
        drag.threshold = reversing ? Math.max(52, Math.min(78, innerWidth * .18)) : Math.min(88, swipeContent.clientWidth * .21) * .75;
        el("swipe-label").textContent = reversing ? "撤销上一次" : drag.waiting ? "下一单词" : "记住了";
        el("swipe-icon").setAttribute("href", "/icons.svg#" + (reversing ? "arrow-left" : drag.waiting ? "right" : "check"));
      } else {
        if (y > 10 && y >= x && (!drag.reveal || dy < 0)) cancelDrag();
        return;
      }
      document.body.setPointerCapture(event.pointerId);
    }
    suppressClickUntil = performance.now() + 350;
    drag.offset = offsetFor(drag.action, dx, dy);
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      if (drag.action !== "reveal") swipeContent.style.transform = `translate3d(${drag.offset}px,0,0)`;
      swipeCue.style.opacity = Math.min(1, Math.abs(drag.offset) / drag.threshold);
    });
  });
  document.addEventListener("pointercancel", event => { if (drag?.id === event.pointerId) cancelDrag(); });
  document.body.addEventListener("lostpointercapture", event => {
    if (event.target === document.body && drag?.id === event.pointerId) cancelDrag();
  });
  document.addEventListener("contextmenu", cancelDrag);
  document.addEventListener("selectionchange", () => { if (window.getSelection()?.toString()) cancelDrag(); });
  document.addEventListener("pointerup", async event => {
    if (drag?.id !== event.pointerId) return;
    const {action, threshold} = drag;
    if (action) suppressClickUntil = performance.now() + 350;
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    const offset = offsetFor(action, dx, dy);
    if (!action || Math.abs(offset) < threshold || !canSwipe() || !stillOnCard()) { cancelDrag(); return; }
    if (action === "reveal") {
      cancelDrag();
      if (canRevealBySwipe() && dy >= Math.abs(dx) * 1.35) await toggleReveal();
      return;
    }
    const reversing = action === "undo";
    if (reversing ? !state.canUndo : !canAdvanceReview()) { cancelDrag(); return; }
    drag = undefined;
    cancelAnimationFrame(frame);
    await slideReview({offset, reversing});
  });
}

positionNavigationIndicator();
if (!mobile) {
  const navigationResize = new ResizeObserver(positionNavigationIndicator);
  navigationResize.observe(navigation);
  for (const item of navigationButtons) navigationResize.observe(item);
}
for (const item of navigationButtons) item.addEventListener("click", () => changeView(item.dataset.view));
wordPreview.addEventListener("pointerenter", () => clearTimeout(previewDismiss));
wordPreview.addEventListener("pointerleave", event => {
  if (!preview?.row.contains(event.relatedTarget)) delayPreviewClose();
});
let previewBackdropDown = false;
wordPreview.addEventListener("pointerdown", event => {
  previewBackdropDown = event.target === wordPreview && !preview?.point;
});
wordPreview.addEventListener("click", event => {
  if (previewBackdropDown && event.target === wordPreview) closeWordPreview();
  previewBackdropDown = false;
});
wordPreview.addEventListener("close", () => {
  if (!wordPreview.open) {
    preview = undefined;
    previewBackdropDown = false;
    clearTimeout(previewDismiss);
  }
});
document.addEventListener("pointerdown", event => {
  if (preview?.point && !wordPreview.contains(event.target)) hideWordPreview();
});
document.addEventListener("scroll", event => {
  if (view !== "library" || wordPreview.contains(event.target)) return;
  if (!previewStart?.hover || !previewStart.target.matches(":hover")) cancelPreviewStart();
  if (preview?.point) hideWordPreview();
}, {capture: true, passive: true});
window.addEventListener("resize", () => {
  if (preview?.point) hideWordPreview();
});
for (const item of all("[data-close]")) item.addEventListener("click", () => {
  const dialog = item.closest("dialog");
  if (dialog.id === "edit-dialog") closeEditor();
  else dialog.close();
});
let hideGuide = document.documentElement.dataset.hideGuide === "true";
function openGuide() {
  el("guide-controls-title").textContent = mobile ? "Android 手势" : "Windows 快捷键";
  el("guide-windows").hidden = mobile;
  el("guide-android").hidden = !mobile;
  el("guide-swipe-area").hidden = !mobile;
  el("guide-preview-tip").textContent = mobile ? "长按词条可预览详情。" : "把鼠标停在词条上可预览详情。";
  el("guide-export-tip").textContent = mobile ? "在 Android 选择「分享数据」或「保存到手机」。" : "在 Windows 选择文件的保存位置。";
  el("hide-guide").checked = hideGuide;
  message("guide-error");
  el("guide-dialog").showModal();
  el("guide-dialog").querySelector(".dialog-body").scrollTop = 0;
}
el("open-guide").onclick = openGuide;
el("guide-form").onsubmit = async event => {
  event.preventDefault();
  const hidden = el("hide-guide").checked;
  const confirm = el("guide-confirm");
  confirm.disabled = true;
  try {
    if (hidden !== hideGuide) await api("/api/guide", {hidden});
    hideGuide = hidden;
    el("guide-dialog").close();
  } catch (error) { message("guide-error", error.message); }
  finally { confirm.disabled = false; }
};
el("open-licenses").onclick = async event => {
  event.preventDefault();
  try {
    const dialog = el("license-dialog");
    el("license-text").textContent = "正在加载…";
    dialog.showModal();
    const response = await request("/licenses");
    if (!response.ok) throw new Error("无法读取第三方许可，请重试。");
    el("license-text").textContent = await response.text();
  } catch (error) { el("license-text").textContent = error.message; }
};
el("settings-form").addEventListener("change", event => {
  commitSetting(event.target);
  flushSettings().catch(() => {});
});
el("settings-form").onsubmit = event => event.preventDefault();
el("settings-form").addEventListener("keydown", event => {
  const field = event.target;
  if ((field.type !== "number" && !(windowsReview && field.id === "study-rollover")) ||
      event.key !== "Enter" || event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  commitSetting(field);
  field.blur();
  flushSettings().catch(() => {});
});
function showNativePicker(event) {
  const field = event.currentTarget;
  if (!field.showPicker) return;
  try {
    field.showPicker();
    event.preventDefault();
  } catch {
    // The native input can still open its picker on older WebViews.
  }
}
const studyRollover = el("study-rollover");
studyRollover.addEventListener("input", renderStudyRollover);
if (mobile) studyRollover.addEventListener("click", showNativePicker);
const examDateInput = el("exam-date");
examDateInput.addEventListener("click", showNativePicker);
examDateInput.addEventListener("input", renderExamDate);
examDateInput.addEventListener("keydown", event => {
  if (["Enter", " "].includes(event.key) && !event.isComposing && event.keyCode !== 229) showNativePicker(event);
});
const exportData = el("transfer-export");
if (mobile && window.WordReviewFiles) {
  exportData.setAttribute("aria-haspopup", "menu");
  exportData.setAttribute("aria-expanded", "false");
  exportData.setAttribute("aria-controls", "popup-list");
  exportData.onclick = () => controls.context(exportData, "导出数据", [
    {label: "分享数据", icon: "upload", pick: () => pickTransfer("share")},
    {label: "保存到手机", icon: "download", pick: () => pickTransfer("export")},
  ]);
} else {
  exportData.onclick = () => pickTransfer("export");
}
el("transfer-import").onclick = () => pickTransfer("import");
el("transfer-recovery").onclick = () => pickTransfer("recovery");
el("download-backup").onclick = event => {
  if (window.WordReviewFiles) {
    event.preventDefault();
    window.WordReviewFiles.pickFile("backup", "");
  } else if (windowsReview) {
    event.preventDefault();
    api("/api/backup/windows", {}).catch(error => message("settings-status", error.message));
  }
};
el("pick-restore").onclick = () => el("restore-file").click();
el("restore-file").onchange = restoreFile;
el("start-review").onclick = () => startReview();
el("undo").onclick = () => slideReview({reversing: true});
el("reveal").onclick = () => toggleReveal();
el("forgot").onclick = () => state?.waitingForNext ? slideReview() : vote(false);
el("pass").onclick = () => slideReview();
el("pass-today").onclick = () => slideReview({passToday: true});
el("personal-note").oninput = noteInput;
el("personal-note").oncompositionend = noteInput;
el("personal-note").onblur = () => flushNote().catch(showError);
el("speak").onclick = speak;
if (window.speechSynthesis) speechSynthesis.onvoiceschanged = updateVoice;
el("open-add").onclick = () => changeView("add");
el("search").oninput = runSearch;
el("filter").onchange = runSearch;
el("sort").onchange = runSearch;
el("prev-page").onclick = () => usePage(-1);
el("next-page").onclick = () => usePage(1);
el("select-all").onclick = () => { selected.clear(); selectedAll = true; refreshLibrary().catch(showError); };
el("clear-selection").onclick = () => { clearSelection(); refreshLibrary().catch(showError); };
el("batch-action").onchange = (event) => {
  batchAction(event.target.value, selection(), selectedAll ? listTotal : selected.size);
  event.target.value = "";
  controls.syncSelect(event.target);
};
el("export-list").onclick = () => exportCSV({...libraryQuery(), all: true}).catch(showError);
el("retry-all").onclick = () => api("/api/batch/retry", {keys: []}).then(refreshLibrary).catch(showError);
el("edit-form").onsubmit = event => { event.preventDefault(); closeEditor(); };
el("edit-dialog").addEventListener("cancel", event => { event.preventDefault(); closeEditor(); });
el("edit-form").addEventListener("input", event => {
  clearTimeout(editTimer);
  if (!event.isComposing && event.target.id !== "edit-word") editTimer = setTimeout(() => flushEdit().catch(editError), 600);
});
el("edit-form").addEventListener("compositionend", () => {
  clearTimeout(editTimer);
  editTimer = setTimeout(() => flushEdit().catch(editError), 600);
});
el("edit-form").addEventListener("focusout", event => {
  if (event.target.id.startsWith("edit-")) flushEdit(event.target.id === "edit-word").catch(editError);
});
el("retry-one").onclick = retryOne;
el("action-form").onsubmit = confirmAction;
el("add-form").onsubmit = submitWords;
el("pick-txt").onclick = () => el("txt-file").click();
el("txt-file").onchange = readWordFile;
el("add-input").oninput = () => { if (!el("add-input").value.trim()) fileFormat = "words"; };
document.addEventListener("keydown", keyboard);
function flushOnHide() {
  commitFocusedSetting();
  flushSettings(true).catch(() => {});
  flushNote(true).catch(showError);
  flushEdit(false, true).catch(editError);
}
if (window.wordReviewFinishClose) {
  window.wordReviewPrepareClose = async () => {
    let saved = false;
    try {
      commitFocusedSetting();
      await flushSettings();
      await flushNote();
      if (editing) await flushEdit(true);
      saved = true;
    } catch (error) {
      if (editing) editError(error);
      else showError(error);
    }
    await window.wordReviewFinishClose(saved);
  };
  window.wordReviewWindowReady();
}
document.addEventListener("visibilitychange", () => {
  if (document.hidden) flushOnHide();
});
window.addEventListener("pagehide", event => {
  flushOnHide();
  const ticket = Number(document.documentElement.dataset.windowTicket);
  if (ticket && !event.persisted) request("/api/window/closed", {ticket}, {keepalive: true}).catch(() => {});
});
if (windowsReview) window.addEventListener("beforeunload", event => {
  commitFocusedSetting();
  if (noteDirty() || editJob || hasSettingChanges() || Object.keys(editedFields(true)).length) {
    flushOnHide();
    if (editing) flushEdit(true).catch(editError);
    event.preventDefault();
    event.returnValue = "";
  }
});
let tick = 0;
setInterval(async () => {
  if (busy || swiping || pendingNavigation) return;
  tick++;
  try {
    if (view === "library" && !controls.isOpen() && !document.querySelector("dialog[open]")) await refreshLibrary();
    if (view === "add" && importPending) await updateImportStatus();
    if (view === "review" && revealed && answerExpanded() && state.settings.online &&
        (revealed.lookup === "pending" || revealed.translationPending)) {
      await refreshRevealedDetails();
    }
    if (wordPreview.open && preview?.card && state?.settings.online &&
        (preview.card.translationPending || preview.card.lookup === "pending")) {
      const active = preview;
      const result = await api("/api/card/" + keyPath(active.card.key));
      if (preview === active && (result.lookup !== active.card.lookup ||
          result.exampleTranslation !== active.card.exampleTranslation || result.translationPending !== active.card.translationPending)) {
        active.card = result;
        renderWordPreview(result, active.progress);
        if (active.point) placeWordPreview(active.point);
      }
    }
    if (tick % 5 === 0) {
      if (view === "stats") await refreshStats();
      else await refreshState();
    }
  } catch (error) { showError(error); }
}, 4000);
if (document.documentElement.dataset.showGuide === "true") openGuide();
refreshState().then(resumeWaiting).catch(error => { el("review-empty").removeAttribute("aria-busy"); showError(error); });
refreshStorage().catch(showError);
