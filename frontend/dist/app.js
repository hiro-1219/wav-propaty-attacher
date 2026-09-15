"use strict";

const state = {
  job: null,
  busy: false,
  root: "",
  showAllEditions: false,
  audioIndex: -1,
  coverPreloads: new Map(),
};

const $ = (id) => document.getElementById(id);
const elements = {};

function backend() {
  return window.go && window.go.main && window.go.main.App;
}

function settings() {
  return {
    root: state.root,
    albumTitle: elements.albumTitle.value.trim(),
    artist: elements.artist.value.trim(),
    year: elements.year.value.trim(),
    apiKey: elements.apiKey.value.trim(),
    fpcalcPath: elements.fpcalcPath.value.trim(),
    useFingerprint: elements.useFingerprint.checked,
  };
}

function setStatus(message, type = "ready") {
  elements.statusText.textContent = message;
  elements.statusText.title = message;
  elements.statusText.classList.toggle("error", type === "error");
  elements.statusIndicator.className = `status-indicator ${type}`;
}

function setBusy(busy, label = "処理中…") {
  state.busy = busy;
  document.querySelectorAll("[data-action]").forEach((element) => {
    element.disabled = busy;
  });
  elements.cancelButton.disabled = !busy;
  if (busy) setStatus(label, "busy");
  updateExportState();
}

function updateExportState() {
  elements.exportButton.disabled = state.busy || !state.job || state.job.selected < 0;
}

async function runTask(label, task, successMessage) {
  if (state.busy) return;
  setBusy(true, label);
  try {
    const result = await task();
    if (result && typeof result === "object" && "tracks" in result) {
      applyJob(result);
    }
    setBusy(false);
    setStatus(typeof successMessage === "function" ? successMessage(result) : successMessage, "ready");
    return result;
  } catch (error) {
    setBusy(false);
    setStatus(errorMessage(error), "error");
    return null;
  }
}

function errorMessage(error) {
  if (!error) return "不明なエラーが発生しました。";
  if (typeof error === "string") return error;
  return error.message || String(error);
}

function applyJob(job) {
  state.job = job;
  state.root = job.root || state.root;
  elements.albumPath.textContent = state.root || "未選択";
  elements.albumPath.title = state.root || "未選択";
  elements.trackCount.textContent = String((job.tracks || []).length);
  elements.candidateCount.textContent = String((job.candidates || []).length);
  if (!(job.candidates || []).length) {
    elements.albumTitle.value = job.suggestedAlbum || "";
    elements.artist.value = job.suggestedArtist || "";
    elements.year.value = job.suggestedYear || "";
    state.showAllEditions = false;
  }
  preloadCandidateCovers(job.candidates || []);
  renderCandidates();
  renderMappings();
  renderCover();
  updateExportState();
}

function preloadCandidateCovers(candidates) {
  candidates.forEach((candidate) => {
    const source = candidate.coverThumbnail;
    if (!source || state.coverPreloads.has(source)) return;
    const image = new Image();
    image.referrerPolicy = "no-referrer";
    image.decoding = "async";
    image.src = source;
    state.coverPreloads.set(source, image);
  });
}

function renderCandidates() {
  const list = elements.candidateList;
  list.replaceChildren();
  const candidates = state.job ? state.job.candidates || [] : [];
  list.classList.toggle("empty-state", candidates.length === 0);
  if (!candidates.length) {
    const message = document.createElement("p");
    message.textContent = state.job && state.job.tracks.length ? "「アルバムを検索」を押してください" : "検索すると候補がここに表示されます";
    list.append(message);
    return;
  }
  if (state.job.warnings && state.job.warnings.length) {
    const warning = document.createElement("div");
    warning.className = "search-warning";
    warning.textContent = state.job.warnings.join(" / ");
    warning.title = warning.textContent;
    list.append(warning);
  }
  const groupCounts = new Map();
  let hiddenEditions = 0;
  candidates.forEach((candidate) => {
    const group = candidate.releaseGroupId || candidate.id;
    const count = (groupCounts.get(group) || 0) + 1;
    groupCounts.set(group, count);
    if (!state.showAllEditions && count > 2) {
      hiddenEditions++;
      return;
    }
    const card = document.createElement("button");
    card.type = "button";
    card.className = "candidate-card" + (candidate.index === state.job.selected ? " selected" : "");
    card.dataset.action = "candidate";
    card.disabled = state.busy;

    const visual = document.createElement("div");
    visual.className = "candidate-cover";
    const coverPlaceholder = document.createElement("span");
    coverPlaceholder.textContent = "♪";
    const cover = document.createElement("img");
    cover.alt = `${candidate.title || "アルバム"}のジャケット`;
    cover.loading = "eager";
    cover.decoding = "async";
    cover.referrerPolicy = "no-referrer";
    cover.hidden = true;
    const coverSource = candidate.index === state.job.selected && state.job.coverDataUrl
      ? state.job.coverDataUrl : candidate.coverThumbnail;
    if (coverSource) {
      cover.addEventListener("load", () => { cover.hidden = false; coverPlaceholder.hidden = true; });
      cover.addEventListener("error", () => { cover.hidden = true; coverPlaceholder.hidden = false; });
      cover.src = coverSource;
    }
    visual.append(cover, coverPlaceholder);

    const content = document.createElement("div");
    content.className = "candidate-content";

    const title = document.createElement("div");
    title.className = "candidate-title";
    title.textContent = candidate.title || "タイトル不明";
    const artist = document.createElement("div");
    artist.className = "candidate-artist";
    artist.textContent = candidate.albumArtist || "アーティスト不明";
    const meta = document.createElement("div");
    meta.className = "candidate-meta";
    const detail = document.createElement("span");
    detail.textContent = `${candidate.date || "日付不明"} · ${candidate.tracks.length}曲`;
    const score = document.createElement("span");
    score.className = "score";
    score.textContent = `${candidate.scorePercent}% · ${candidate.confidence}`;
    meta.append(detail, score);
    const evidence = document.createElement("div");
    evidence.className = "evidence-row";
    (candidate.evidence || []).forEach((label) => {
      const badge = document.createElement("span");
      badge.textContent = label === "指紋一致" && candidate.fingerprintHits ? `${label} ${candidate.fingerprintHits}` : label;
      evidence.append(badge);
    });
    content.append(title, artist, meta, evidence);
    card.append(visual, content);
    card.addEventListener("click", () => selectCandidate(candidate.index));
    list.append(card);
  });
  if (hiddenEditions > 0) {
    const more = document.createElement("button");
    more.type = "button";
    more.className = "candidate-card more-editions";
    more.dataset.action = "more-editions";
    more.textContent = `盤違いをさらに${hiddenEditions}件表示`;
    more.addEventListener("click", () => { state.showAllEditions = true; renderCandidates(); });
    list.append(more);
  }
}

function renderMappings() {
  const list = elements.mappingList;
  list.replaceChildren();
  if (!state.job || state.job.selected < 0 || !state.job.assignments.length) {
    list.classList.add("empty-state");
    const message = document.createElement("p");
    message.textContent = "アルバム候補を選択してください";
    list.append(message);
    return;
  }
  list.classList.remove("empty-state");
  const release = state.job.candidates[state.job.selected];
  state.job.assignments.forEach((assignment) => {
    const local = state.job.tracks[assignment.localIndex];
    const row = document.createElement("div");
    row.className = "mapping-row";
    const info = document.createElement("button");
    info.type = "button";
    info.className = "local-track track-picker" + (assignment.localIndex === state.audioIndex ? " selected" : "");
    info.title = "クリックしてプレビュー再生する曲を選択";
    const name = document.createElement("strong");
    name.textContent = assignment.localName;
    name.title = assignment.localName;
    const hint = document.createElement("small");
    hint.textContent = `Disc ${local.discHint} / Track ${local.trackHint} · ${local.duration}`;
    info.append(name, hint);
    info.addEventListener("click", () => selectAudioTrack(assignment.localIndex));

    const select = document.createElement("select");
    select.dataset.action = "assignment";
    select.disabled = state.busy;
    addOption(select, "-1", "未割当");
    addOption(select, "exclude", "出力から除外");
    release.tracks.forEach((track) => {
      addOption(select, String(track.index), `D${track.disc}-${String(track.position).padStart(2, "0")}  ${track.title} — ${track.artist}`);
    });
    select.value = assignment.excluded ? "exclude" : String(assignment.releaseIndex);
    select.addEventListener("change", async () => {
      const value = select.value;
      const excluded = value === "exclude";
      const releaseIndex = excluded ? -1 : Number(value);
      try {
        await backend().SetAssignment(assignment.localIndex, releaseIndex, excluded);
        assignment.releaseIndex = releaseIndex;
        assignment.excluded = excluded;
        setStatus(`${assignment.localName} の割当を変更しました。`, "ready");
      } catch (error) {
        setStatus(errorMessage(error), "error");
        renderMappings();
      }
    });
    row.append(info, select);
    list.append(row);
  });
}

function playbackTime(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return "0:00";
  const rounded = Math.floor(seconds);
  return `${Math.floor(rounded / 60)}:${String(rounded % 60).padStart(2, "0")}`;
}

function updatePlayerTime() {
  const duration = elements.audioPlayer.duration;
  const current = elements.audioPlayer.currentTime;
  elements.currentTime.textContent = playbackTime(current);
  elements.totalTime.textContent = Number.isFinite(duration) ? playbackTime(duration) : "0:00";
  elements.seekBar.value = Number.isFinite(duration) && duration > 0 ? String(Math.round(current / duration * 1000)) : "0";
}

function resetPlayer() {
  if (!elements.audioPlayer) return;
  elements.audioPlayer.pause();
  elements.audioPlayer.removeAttribute("src");
  elements.audioPlayer.load();
  state.audioIndex = -1;
  elements.playerTitle.textContent = "曲を選択してください";
  elements.playerSubtitle.textContent = "曲名をクリックすると再生準備します";
  elements.playButton.textContent = "▶";
  elements.playButton.disabled = true;
  elements.stopButton.disabled = true;
  elements.seekBar.disabled = true;
  elements.seekBar.value = "0";
  elements.currentTime.textContent = "0:00";
  elements.totalTime.textContent = "0:00";
}

async function selectAudioTrack(index) {
  try {
    const source = await backend().GetAudioSource(index);
    elements.audioPlayer.pause();
    state.audioIndex = index;
    elements.audioPlayer.src = source.url;
    elements.audioPlayer.load();
    elements.playerTitle.textContent = source.name;
    elements.playerTitle.title = source.name;
    elements.playerSubtitle.textContent = `ローカルWAV · ${source.duration}`;
    elements.playButton.textContent = "▶";
    elements.playButton.disabled = false;
    elements.stopButton.disabled = false;
    elements.seekBar.disabled = false;
    elements.currentTime.textContent = "0:00";
    elements.totalTime.textContent = source.duration;
    renderMappings();
    setStatus(`${source.name} を再生対象に選択しました。`, "ready");
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

async function togglePlayback() {
  if (state.audioIndex < 0) return;
  if (elements.audioPlayer.paused) {
    try {
      await elements.audioPlayer.play();
      elements.playButton.textContent = "❚❚";
      elements.playerSubtitle.textContent = "再生中";
    } catch (error) {
      setStatus(`再生できませんでした: ${errorMessage(error)}`, "error");
    }
  } else {
    elements.audioPlayer.pause();
    elements.playButton.textContent = "▶";
    elements.playerSubtitle.textContent = "一時停止中";
  }
}

function stopPlayback() {
  elements.audioPlayer.pause();
  elements.audioPlayer.currentTime = 0;
  elements.playButton.textContent = "▶";
  elements.playerSubtitle.textContent = "停止中";
  updatePlayerTime();
}

function addOption(select, value, text) {
  const option = document.createElement("option");
  option.value = value;
  option.textContent = text;
  select.append(option);
}

function renderCover() {
  const hasCover = state.job && state.job.coverDataUrl;
  elements.coverImage.hidden = !hasCover;
  elements.coverPlaceholder.hidden = Boolean(hasCover);
  if (hasCover) {
    elements.coverImage.src = state.job.coverDataUrl;
    elements.coverInfo.textContent = `${state.job.coverMime} · ${state.job.coverKiB} KiB · AIFFへ埋め込み`;
  } else {
    elements.coverImage.removeAttribute("src");
    elements.coverInfo.textContent = "未取得 · ローカル画像を選択できます";
  }
}

async function chooseFolder() {
  try {
    const path = await backend().ChooseFolder();
    if (path) {
      resetPlayer();
      state.root = path;
      elements.albumPath.textContent = path;
      elements.albumPath.title = path;
      setStatus("フォルダを選択しました。「WAVを読み込む」を押してください。", "ready");
    }
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

async function scan() {
  if (!state.root) {
    setStatus("アルバムフォルダを指定してください。", "error");
    return;
  }
  resetPlayer();
  state.coverPreloads.clear();
  await runTask("WAVファイルを読み込んでいます…", () => backend().Scan(settings()), (job) => `${job.tracks.length}曲を読み込みました。アルバムを検索してください。`);
}

async function search() {
  if (!state.job || !state.job.tracks.length) {
    setStatus("先にWAVファイルを読み込んでください。", "error");
    return;
  }
  if (elements.useFingerprint.checked && !elements.apiKey.value.trim()) {
    setStatus("音響指紋検索にはAcoustIDアプリケーションキーが必要です。", "error");
    return;
  }
  state.showAllEditions = false;
  await runTask("アルバム候補を検索しています…", () => backend().Search(settings()), (job) => job.selected < 0
    ? `${job.candidates.length}件の候補があります。一致度を確認して選択してください。`
    : `${job.candidates.length}件の候補から高信頼のアルバムを選択しました。`);
}

async function lookupRelease() {
  const value = elements.mbid.value.trim();
  if (!state.job || !state.job.tracks.length) {
    setStatus("先にWAVファイルを読み込んでください。", "error");
    return;
  }
  if (!value) {
    setStatus("MusicBrainzのRelease URLまたはMBIDを入力してください。", "error");
    return;
  }
  await runTask("指定されたMusicBrainz Releaseを取得しています…", () => backend().LookupRelease(value), "指定Releaseを読み込みました。");
}

async function selectCandidate(index) {
  await runTask("候補の曲情報とジャケットを取得しています…", () => backend().SelectCandidate(index), "アルバム候補と曲割当を更新しました。");
}

async function chooseCover() {
  if (!state.job) {
    setStatus("先にWAVを読み込み、アルバムを検索してください。", "error");
    return;
  }
  try {
    const path = await backend().ChooseCover();
    if (path) await loadCover(path);
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

async function loadCover(path) {
  await runTask("ジャケット画像を読み込んでいます…", () => backend().LoadCover(path), "ローカル画像をジャケットに設定しました。");
}

async function exportAIFF() {
  if (!state.job || state.job.selected < 0) {
    setStatus("アルバム候補を選択してください。", "error");
    return;
  }
  await runTask("AIFFを生成し、音声ハッシュを検証しています…", () => backend().Export(), (path) => `AIFFの生成が完了しました: ${path}`);
}

async function handleDrop(x, y, paths) {
  if (state.busy || !paths || !paths.length) return;
  const target = document.elementFromPoint(x, y);
  const isCover = target && target.closest("#cover-drop");
  try {
    if (isCover) {
      if (!state.job) throw new Error("先にアルバムを検索してください。");
      const path = await backend().ResolveCoverDrop(paths[0]);
      await loadCover(path);
    } else {
      const root = await backend().ResolveAlbumDrop(paths[0]);
      state.root = root;
      elements.albumPath.textContent = root;
      elements.albumPath.title = root;
      await scan();
    }
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

async function initialise() {
  Object.assign(elements, {
    albumDrop: $("album-drop"), albumPath: $("album-path"), albumTitle: $("album-title"),
    artist: $("artist"), year: $("year"), mbid: $("mbid"), mbidButton: $("mbid-button"),
    apiKey: $("api-key"), fpcalcPath: $("fpcalc-path"), useFingerprint: $("use-fingerprint"),
    scanButton: $("scan-button"), searchButton: $("search-button"), trackCount: $("track-count"),
    candidateCount: $("candidate-count"), candidateList: $("candidate-list"), mappingList: $("mapping-list"),
    coverDrop: $("cover-drop"), coverImage: $("cover-image"), coverPlaceholder: $("cover-placeholder"),
    coverInfo: $("cover-info"), exportButton: $("export-button"), cancelButton: $("cancel-button"),
    statusText: $("status-text"), statusIndicator: $("status-indicator"),
    audioPlayer: $("audio-player"), playButton: $("play-button"), stopButton: $("stop-button"),
    playerTitle: $("player-title"), playerSubtitle: $("player-subtitle"), seekBar: $("seek-bar"),
    currentTime: $("current-time"), totalTime: $("total-time"),
  });

  elements.albumDrop.addEventListener("click", chooseFolder);
  elements.scanButton.addEventListener("click", scan);
  elements.searchButton.addEventListener("click", search);
  elements.mbidButton.addEventListener("click", lookupRelease);
  elements.coverDrop.addEventListener("click", chooseCover);
  elements.exportButton.addEventListener("click", exportAIFF);
  elements.cancelButton.addEventListener("click", async () => {
    await backend().Cancel();
    setStatus("キャンセルしています…", "busy");
  });
  elements.playButton.addEventListener("click", togglePlayback);
  elements.stopButton.addEventListener("click", stopPlayback);
  elements.seekBar.addEventListener("input", () => {
    const duration = elements.audioPlayer.duration;
    if (Number.isFinite(duration) && duration > 0) {
      elements.audioPlayer.currentTime = Number(elements.seekBar.value) / 1000 * duration;
    }
  });
  elements.audioPlayer.addEventListener("timeupdate", updatePlayerTime);
  elements.audioPlayer.addEventListener("loadedmetadata", updatePlayerTime);
  elements.audioPlayer.addEventListener("pause", () => {
    if (!elements.audioPlayer.ended) elements.playButton.textContent = "▶";
  });
  elements.audioPlayer.addEventListener("ended", () => {
    elements.playButton.textContent = "▶";
    elements.playerSubtitle.textContent = "再生終了";
    updatePlayerTime();
  });
  elements.audioPlayer.addEventListener("error", () => {
    if (elements.audioPlayer.getAttribute("src")) setStatus("WAVを再生できませんでした。ファイルを確認してください。", "error");
  });

  const defaults = await backend().GetDefaults();
  state.root = defaults.musicPath || "";
  elements.albumPath.textContent = state.root || "未選択";
  elements.albumPath.title = state.root || "未選択";
  elements.fpcalcPath.value = defaults.fpcalcPath || "";

  if (window.runtime) {
    window.runtime.EventsOnMultiple("search:progress", (message) => {
      if (state.busy) setStatus(String(message), "busy");
    }, -1);
    window.runtime.OnFileDrop(handleDrop, true);
  }
}

function waitForBridge(attempt = 0) {
  if (backend()) {
    initialise().catch((error) => setStatus(errorMessage(error), "error"));
    return;
  }
  if (attempt > 200) return;
  window.setTimeout(() => waitForBridge(attempt + 1), 25);
}

window.addEventListener("DOMContentLoaded", () => {
  Object.assign(elements, { statusText: $("status-text"), statusIndicator: $("status-indicator") });
  waitForBridge();
});
