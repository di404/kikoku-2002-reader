// Reader front end: draws the compiled steps the Go side hands over.
// Geometry comes in the original 800x600 space and is scaled here, so
// pictures are enlarged and text is drawn at the window's real resolution.
"use strict";

const W = 800, H = 600;
const $ = (id) => document.getElementById(id);
const api = () => window.go.main.App;

const state = {
  index: 0, total: 0, scenes: [], movies: [], opening: "", settings: null, fonts: [],
  view: null, k: 1, hidden: false, busy: false, queued: null,
  lastLayers: "", movie: false, menu: false,
};

// ---------- layout ----------

function layout() {
  const vw = window.innerWidth, vh = window.innerHeight;
  const k = Math.min(vw / W, vh / H);
  state.k = k;
  const w = W * k, h = H * k;
  const left = (vw - w) / 2, top = (vh - h) / 2;
  for (const el of [$("stage"), $("movie"), $("flash")]) {
    Object.assign(el.style, { left: left + "px", top: top + "px", width: w + "px", height: h + "px" });
  }
  Object.assign($("box").style, { left: left + "px", width: w + "px", bottom: top + "px" });
  applyFontSize();
  if (state.view) drawLayers(state.view.step.layers, 0);
}

function applyFontSize() {
  const scale = state.settings ? state.settings.fontScale : 1;
  $("box").style.fontSize = (H * state.k * 0.036 * scale) + "px";
}

// ---------- layers ----------

function layerEl(l, k) {
  let el;
  if (l.kind === "image") {
    el = document.createElement("img");
    el.src = "/img/" + encodeURIComponent(l.src);
    el.decoding = "async";
    el.draggable = false;
  } else {
    el = document.createElement("div");
    el.style.background = l.color;
  }
  el.className = "layer";
  Object.assign(el.style, {
    left: l.x * k + "px", top: l.y * k + "px",
    width: l.w * k + "px", height: l.h * k + "px",
  });
  if (l.opacity < 1) el.style.opacity = l.opacity;
  return el;
}

// prepareLayers builds the next picture off-screen and resolves, once its
// images are decoded, to a commit function that swaps it in with a fade
// of the given length. Callers commit together with the text so picture
// and text change in the same frame.
function prepareLayers(layers, fade, force) {
  const key = JSON.stringify(layers || []);
  if (key === state.lastLayers && !force) return Promise.resolve(() => {});
  state.lastLayers = key;
  const scene = document.createElement("div");
  scene.className = "scene";
  for (const l of layers || []) scene.appendChild(layerEl(l, state.k));
  const imgs = [...scene.querySelectorAll("img")];
  const decoded = Promise.all(imgs.map((i) => i.decode().catch(() => {})));
  return Promise.race([decoded, sleep(1500)]).then(() => () => {
    const stage = $("stage");
    const old = [...stage.querySelectorAll(".scene")];
    if (!fade || old.length === 0) {
      old.forEach((o) => o.remove());
      stage.appendChild(scene);
      return;
    }
    scene.style.opacity = 0;
    scene.style.transition = `opacity ${fade}ms ease`;
    stage.appendChild(scene);
    scene.getBoundingClientRect(); // start the fade from 0
    scene.style.opacity = 1;
    // Drop the old picture only once the new one fully covers it.
    let done = false;
    const drop = () => { if (!done) { done = true; old.forEach((o) => o.remove()); } };
    scene.addEventListener("transitionend", drop, { once: true });
    setTimeout(drop, fade + 300);
  });
}

function drawLayers(layers, fade) {
  prepareLayers(layers, fade, true).then((commit) => commit());
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const preloaded = new Map();
function preload(list) {
  for (const src of list || []) {
    if (preloaded.has(src)) continue;
    const img = new Image();
    img.src = "/img/" + encodeURIComponent(src);
    img.decode().catch(() => {}); // decode now so the switch does not wait
    preloaded.set(src, img);
    if (preloaded.size > 40) preloaded.delete(preloaded.keys().next().value);
  }
}

// ---------- text ----------

function drawText(st) {
  const text = $("text");
  text.innerHTML = st.html;
  // Quoted speech hangs its opening bracket like the original box.
  text.classList.toggle("speech", /^[「『（]/.test(text.textContent));
  text.classList.remove("in");
  void text.offsetWidth;
  text.classList.add("in");
}

function flash() {
  const f = $("flash");
  f.classList.remove("on");
  void f.offsetWidth;
  f.classList.add("on");
}

// ---------- stepping ----------

async function show(i, jumped) {
  if (i < 0 || i >= state.total) return;
  if (state.busy) { state.queued = [i, jumped]; return; }
  state.busy = true;
  try {
    const v = await api().Show(i, !!jumped);
    state.view = v;
    state.index = i;
    const st = v.step;
    const fade = jumped ? 0 : Math.min(st.fade || 0, 1200);
    const commit = await prepareLayers(st.layers, fade, false);
    await new Promise((r) => requestAnimationFrame(() => {
      if (st.flash && !jumped) flash();
      commit(); drawText(st); r();
    }));
    preload(v.next);
  } catch (e) {
    hint("出错：" + e);
  } finally {
    state.busy = false;
    if (state.queued) {
      const [qi, qj] = state.queued;
      state.queued = null;
      show(qi, qj);
    }
  }
}

function next() {
  if (state.menu) return;
  if (state.movie) { stopMovie(); return; }
  if (state.hidden) { toggleHidden(); return; }
  if (state.index + 1 >= state.total) { hint("全文完"); return; }
  show(state.index + 1, false);
}

function prev() {
  if (state.menu || state.movie) return;
  if (state.hidden) toggleHidden();
  show(Math.max(0, state.index - 1), true);
}

function toggleHidden() {
  state.hidden = !state.hidden;
  $("box").classList.toggle("hidden", state.hidden);
}

let hintTimer;
function hint(msg) {
  const h = $("hint");
  h.textContent = msg;
  h.classList.add("on");
  clearTimeout(hintTimer);
  hintTimer = setTimeout(() => h.classList.remove("on"), 2200);
}

// ---------- movies ----------

let afterMovie = null;

// playMovie shows a cutscene over the page; a click skips it. then runs
// when it ends, to resume the page's sound.
function playMovie(file, then) {
  const m = $("movie");
  state.movie = true;
  afterMovie = then || null;
  api().Silence();
  hint("过场准备中… 点击跳过");
  m.src = "/movie/" + encodeURIComponent(file) + ".mp4";
  m.classList.add("on");
  m.play().catch(() => {});
  m.onended = stopMovie;
  m.onerror = () => { hint("过场无法播放（需要 ffmpeg）"); stopMovie(); };
}

function stopMovie() {
  const m = $("movie");
  if (!state.movie) return;
  state.movie = false;
  m.pause();
  m.classList.remove("on");
  m.removeAttribute("src");
  m.load();
  const then = afterMovie;
  afterMovie = null;
  if (then) then();
}

// ---------- settings ----------

function applySettings() {
  const s = state.settings;
  const root = document.documentElement.style;
  root.setProperty("--font", `"${(s.fontFamily || "Songti SC").replace(/"/g, "")}"`);
  root.setProperty("--line", s.lineHeight);
  root.setProperty("--box", s.boxOpacity);
  $("preview").style.fontSize = 20 * s.fontScale + "px";
  applyFontSize();
  loadUserFont(s.fontFile);
}

let userFont = null, userFontFile = "";
function loadUserFont(file) {
  $("fontFileName").textContent = file ? file.split("/").pop() : "";
  if (file === userFontFile) return;
  userFontFile = file;
  if (userFont) { document.fonts.delete(userFont); userFont = null; }
  if (!file) return;
  const f = new FontFace("UserFont", `url(/font/user?${encodeURIComponent(file)})`);
  f.load().then(() => { document.fonts.add(f); userFont = f; })
    .catch(() => hint("字体文件载入失败"));
}

const sliders = ["fontScale", "lineHeight", "boxOpacity", "music", "effect"];

function fillMenu() {
  const s = state.settings;
  const sel = $("fontFamily");
  sel.innerHTML = "";
  let known = false;
  for (const f of state.fonts) {
    const o = new Option(f.label, f.family);
    if (f.family === s.fontFamily) { o.selected = true; known = true; }
    sel.add(o);
  }
  $("fontCustom").value = known ? "" : s.fontFamily;
  for (const id of sliders) $(id).value = s[id];

  const cur = currentScene();
  const ol = $("scenes");
  ol.innerHTML = "";
  state.scenes.forEach((sc, n) => {
    const li = document.createElement("li");
    li.textContent = sc.title;
    if (n === cur) li.className = "cur";
    li.onclick = () => { closeMenu(); show(sc.first, true); };
    ol.appendChild(li);
  });
  const ul = $("movies");
  ul.innerHTML = "";
  for (const mv of state.movies) {
    const li = document.createElement("li");
    li.textContent = mv.title;
    li.onclick = () => { closeMenu(); playMovie(mv.file, () => show(state.index, true)); };
    ul.appendChild(li);
  }
  const sc = state.scenes[cur];
  $("where").textContent = sc ? `${sc.title} · ${state.index + 1} / ${state.total}` : "";
}

function currentScene() {
  return state.view ? state.view.step.scene : -1;
}

function readMenu() {
  const s = state.settings;
  s.fontFamily = $("fontCustom").value.trim() || $("fontFamily").value;
  for (const id of sliders) s[id] = parseFloat($(id).value);
  applySettings();
  clearTimeout(readMenu.t);
  readMenu.t = setTimeout(() => api().SaveSettings(s).then((r) => { state.settings = r; }), 250);
}

function openMenu() { if (state.movie) stopMovie(); state.menu = true; fillMenu(); $("menu").classList.remove("hidden"); }
function closeMenu() { state.menu = false; $("menu").classList.add("hidden"); }

// ---------- input ----------

function wire() {
  $("screen").addEventListener("click", (e) => { if (e.button === 0) next(); });
  $("screen").addEventListener("contextmenu", (e) => { e.preventDefault(); if (!state.menu) toggleHidden(); });
  let wheelAt = 0;
  window.addEventListener("wheel", (e) => {
    if (state.menu) return;
    const now = Date.now();
    if (now - wheelAt < 180) return;
    wheelAt = now;
    if (e.deltaY < 0) prev(); else if (e.deltaY > 0) next();
  }, { passive: true });

  let skip = null;
  const stopSkip = () => { if (skip) { clearInterval(skip); skip = null; } };
  window.addEventListener("keydown", (e) => {
    if (e.target.tagName === "INPUT" && e.key !== "Escape") return;
    switch (e.key) {
      case " ": case "Enter": case "ArrowRight": case "ArrowDown": case "PageDown":
        e.preventDefault(); next(); break;
      case "ArrowLeft": case "ArrowUp": case "PageUp": case "Backspace":
        e.preventDefault(); prev(); break;
      case "h": case "H":
        if (!state.menu) toggleHidden(); break;
      case "f": case "F":
        api().ToggleFullscreen(); break;
      case "Escape":
        state.menu ? closeMenu() : openMenu(); break;
      case "Control":
        if (!skip && !state.menu) skip = setInterval(() => { if (!state.busy && !state.movie) next(); }, 70);
        break;
    }
  });
  window.addEventListener("keyup", (e) => { if (e.key === "Control") stopSkip(); });
  window.addEventListener("blur", stopSkip);

  $("close").onclick = closeMenu;
  $("menu").addEventListener("click", (e) => { if (e.target.id === "menu") closeMenu(); });
  for (const id of ["fontFamily", "fontCustom", ...sliders]) $(id).addEventListener("input", readMenu);
  $("fontFamily").addEventListener("change", () => { $("fontCustom").value = ""; readMenu(); });
  $("fontFile").onclick = async () => {
    const p = await api().ChooseFontFile();
    if (p) { state.settings.fontFile = p; applySettings(); }
  };
  $("fontFileName").ondblclick = () => { state.settings.fontFile = ""; readMenu(); };
  $("fullscreen").onclick = () => api().ToggleFullscreen();
  $("quit").onclick = () => api().Quit();
  $("gamedir").onclick = pickGameDir;
  $("errorPick").onclick = pickGameDir;
  window.addEventListener("resize", layout);
}

async function pickGameDir() {
  const r = await api().ChooseGameDir();
  closeMenu();
  start(r);
}

function start(r) {
  state.settings = r.settings;
  state.fonts = r.fonts || [];
  applySettings();
  if (r.error) {
    $("errorText").textContent = "无法载入游戏数据：\n" + r.error +
      "\n\n请选择《鬼哭街》2002 原版的目录（包含 script.chs、IMAGE.PK2、SOUND.PK2 的文件夹）。";
    $("error").classList.remove("hidden");
    return;
  }
  $("error").classList.add("hidden");
  state.scenes = r.scenes;
  state.total = r.total;
  state.movies = r.movies || [];
  state.opening = r.opening || "";
  layout();
  $("box").classList.remove("hidden");
  // "#123" in the URL starts at that step (used by the browser preview)
  const at = parseInt(location.hash.slice(1), 10);
  if (at >= 0) { show(Math.min(at, r.total - 1), true); return; }
  const last = Math.min(r.settings.last || 0, r.total - 1);
  if (last > 0) {
    show(last, true);
    hint("从上次读到的地方继续 · Esc 打开章节");
  } else if (state.opening) {
    playMovie(state.opening, () => show(0, false));
  } else {
    show(0, false);
  }
}

window.addEventListener("DOMContentLoaded", async () => {
  wire();
  layout();
  start(await api().Init());
});
