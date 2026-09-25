// 訳の欄を入力欄に差し替えて書き換え、保存へ回すまでを見る。
//
// app.js の冒頭にあるとおり、編集でいちばん大事なのは訳を失わないことである。
// ここでは、入力欄の開き方と閉じ方（押す、Tab、Enter、Escape）、自動保存の時計、
// 変換中（IME）の扱い、貼り付け、保存の要求と応答の描き方を、1つの試験で1つの
// 約束として確かめる。autosave.spec.mjs が見ている「止まると保存する」「Enter で
// 次の行へ行く」の基本の道は繰り返さない。
//
// 保存が入ったかは、画面の表示ではなくディスクのバイトで見る。触っていない行が
// 1バイトも変わらないことも、書き換えを伴う試験では一緒に見る。
//
// 時間の流れに頼る試験（自動保存の時計、変換の確定直後の猶予）は、page.clock で
// 時計を止めてから動かす。止めておけば、自動保存の時計が勝手に切れないので、
// 「待たずに保存した」ことを遅い機械でも取り違えずに確かめられる。page.clock.install は
// 頁を開く前に要るので、その試験は app ではなく page と server から開く。
import { createHash } from "node:crypto";

import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, SAMPLE_LINES, keyFor, record, sampleRepo, workingCopy } from "../support/repo.mjs";
import {
  editor,
  openApp,
  openEditor,
  rowByLine,
  saveState,
  translationCell,
  typeTranslation,
  waitForSaved,
} from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// 見本の見出し。sampleWorkingCopy と同じものを、行を組み替えた見本でも使う。
const SECTION = "# ===== Level 1: Ryan (Sunny) =====";
const NODE = "# --- intro: Ryan_1_intro ---";

// hello / goodbye / wonderful を作業コピーの1行にするときの形（訳は ja）。
const ROW = {
  hello: { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
  goodbye: { ...SAMPLE.goodbye, translation: SAMPLE.goodbye.ja },
  wonderful: { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
};

// ---- 見本とディスクの読み方 ----

// dataText は作業コピーの1行（改行を除く）を組む。repo.mjs の workingCopy と同じ並び。
function dataText(item, translation) {
  return record(
    item.key ?? keyFor(item.source ?? ""),
    item.section ?? "L01 Ryan",
    item.node ?? "Ryan_1_intro",
    item.order ?? "",
    item.speaker ?? "",
    item.source ?? "",
    translation,
  );
}

// splitLines は改行を残したまま物理行に分ける（BOM は1行目に含まれる）。
function splitLines(buf) {
  const out = [];
  let start = 0;
  for (let i = 0; i < buf.length; i++) {
    if (buf[i] === 0x0a) {
      out.push(buf.subarray(start, i + 1));
      start = i + 1;
    }
  }
  if (start < buf.length) {
    out.push(buf.subarray(start));
  }
  return out;
}

// expectOnlyLines は、changed に挙げた行（物理行番号 → 改行まで含めた期待の中身）
// だけが変わり、ほかの行が1バイトも変わっていないことを確かめる。
function expectOnlyLines(before, after, changed) {
  const b = splitLines(before);
  const a = splitLines(after);
  expect(a, "物理行の数が変わった").toHaveLength(b.length);
  for (let i = 0; i < b.length; i++) {
    const n = i + 1;
    if (Object.prototype.hasOwnProperty.call(changed, n)) {
      expect(a[i].toString("utf8"), `${n}行目`).toBe(changed[n]);
      continue;
    }
    expect(a[i].equals(b[i]), `${n}行目が変わった`).toBe(true);
  }
}

// lineOnDisk は作業コピーの n 行目（改行まで含む）を読む。
async function lineOnDisk(server, n) {
  const lines = splitLines(await server.readRoot(workingRel));
  return lines[n - 1] ? lines[n - 1].toString("utf8") : undefined;
}

// ---- 画面の操作 ----

// trackSaves は保存の要求（POST /api/rows）の本文を集める。
function trackSaves(page) {
  const bodies = [];
  page.on("request", (req) => {
    if (req.method() === "POST" && new URL(req.url()).pathname === "/api/rows") {
      bodies.push(req.postDataJSON());
    }
  });
  return bodies;
}

// openPaused は偽の時計を入れてから画面を開き、開き終えたところで時計を止める。
//
// 止めたあとは page.clock.runFor で進めたぶんしか時間が流れない。自動保存の時計
// （1.5 秒）も、変換の確定からの猶予（performance.now）も、進めない限り動かない。
async function openPaused(page, server) {
  await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
  await openApp(page, server);
  await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
}

// expectEditorIn は入力欄が物理行 n に差し込まれ、焦点が載っていることを確かめる。
async function expectEditorIn(page, n) {
  await expect(rowByLine(page, n).locator("textarea.editor")).toHaveCount(1);
  await expect(editor(page)).toBeFocused();
}

// compose は入力欄へ変換（IME）の事象を投げる。
//
// type は compositionstart / compositionend / input。value を渡すと、投げる前に
// 入力欄の値をそれにする（変換中の読みや確定した字が欄に入った状態を作る）。
// 実機の IME を CDP の Input.imeSetComposition で真似る道もあるが、確かめた範囲の
// Chromium では compositionend が出ず、変換中の Enter で改行まで入った。実機の並びと
// 違うので、画面が約束している事象の並びを直に投げる。
async function compose(page, type, value) {
  await editor(page).evaluate(
    (ed, [kind, text]) => {
      if (text !== null) {
        ed.value = text;
      }
      if (kind === "input") {
        ed.dispatchEvent(
          new InputEvent("input", { bubbles: true, isComposing: true, inputType: "insertCompositionText", data: text }),
        );
        return;
      }
      ed.dispatchEvent(new CompositionEvent(kind, { bubbles: true, data: text ?? "" }));
    },
    [type, value ?? null],
  );
}

// keydown は入力欄へ keydown を投げ、画面が既定の動作を止めた（横取りした）かを返す。
async function keydown(page, init) {
  return editor(page).evaluate((ed, options) => {
    const ev = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...options });
    if (options.keyCode !== undefined && ev.keyCode !== options.keyCode) {
      // 古い形の keyCode は初期化の辞書で入らない実装があるので、読み口を差し替える。
      Object.defineProperty(ev, "keyCode", { get: () => options.keyCode });
    }
    ed.dispatchEvent(ev);
    return ev.defaultPrevented;
  }, init);
}

// ---- 入力欄そのもの ----

// 入力欄は頁に1つだけ作って触っている行へ差し込む約束で、1721行ぶんを常設しない。
// 綴り検査を切るのは、ブラウザーの綴り検査が入力の中身を外部のサービスへ送りうる
// 経路だからである（原文と訳を外へ出さない約束）。自動修正と翻訳も、訳を勝手に
// 書き換えさせないために切ってある。どれか1つでも抜けると、打った訳が外へ出るか、
// 打っていない字に化ける。
test("入力欄は頁に1つだけで、綴り検査・自動修正・翻訳を切ってある", async ({ app }) => {
  await openEditor(app, SAMPLE_LINES.hello);
  const ed = editor(app);
  await expect(ed).toHaveAttribute("spellcheck", "false");
  await expect(ed).toHaveAttribute("autocorrect", "off");
  await expect(ed).toHaveAttribute("autocapitalize", "off");
  await expect(ed).toHaveAttribute("autocomplete", "off");
  await expect(ed).toHaveAttribute("translate", "no");
  await expect(ed).toHaveClass(/(^|\s)notranslate(\s|$)/);
  expect(await ed.evaluate((node) => node.spellcheck)).toBe(false);
  // 名前は目録から。向きは中身から決めさせ、lang はロケール名そのまま（ヘブライ語の確認用）。
  await expect(ed).toHaveAttribute("aria-label", msg("ja", "ui.edit_label_line", { line: SAMPLE_LINES.hello }));
  await expect(ed).toHaveAttribute("lang", "ja");
  await expect(ed).toHaveAttribute("dir", "auto");

  // 別の行を開いても、入力欄は増えずに移るだけ。元の行は字の欄に戻る。
  await openEditor(app, SAMPLE_LINES.wonderful);
  await expect(app.locator("textarea")).toHaveCount(1);
  await expectEditorIn(app, SAMPLE_LINES.wonderful);
  await expect(translationCell(app, SAMPLE_LINES.hello)).toBeVisible();
  await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText(SAMPLE.hello.ja);
});

// 強制色モード（Windows のハイコントラスト）では、ブラウザーが box-shadow を消し、色を
// 系統の色へ置き換える。入力欄の焦点の印は枠の色と box-shadow で付けていて、outline は
// none にしていたので、この状態では印がほぼ消えた（どこに打っているか分からない）。
// 透明の輪郭を敷いておくと、強制色モードでは見える色の輪郭として描かれる。ふだんの
// 配色では透明なので見た目は変わらない。
test("強制色モードでも、入力欄に焦点の輪郭が残る", async ({ app }) => {
  const outline = () =>
    editor(app).evaluate((node) => {
      const s = getComputedStyle(node);
      return { style: s.outlineStyle, width: parseFloat(s.outlineWidth) };
    });

  await openEditor(app, SAMPLE_LINES.hello);
  expect(await outline()).toMatchObject({ style: "solid" });
  expect((await outline()).width).toBeGreaterThanOrEqual(2);
  // ふだんの配色では透明で、印は枠と box-shadow のまま。
  expect(await editor(app).evaluate((node) => getComputedStyle(node).outlineColor)).toBe("rgba(0, 0, 0, 0)");

  await app.emulateMedia({ forcedColors: "active" });
  await expect(editor(app)).toBeFocused();
  expect(await outline()).toMatchObject({ style: "solid" });
  expect((await outline()).width).toBeGreaterThanOrEqual(2);
  // 強制色モードでは、透明の輪郭が見える色に置き換わる。
  expect(await editor(app).evaluate((node) => getComputedStyle(node).outlineColor)).not.toBe("rgba(0, 0, 0, 0)");
});

// 長い訳を横へ送らずに全部見ながら直せる、と README が約束している。textarea は
// rows で決まった高さのままなので、画面が伸ばさないと送りの棒が出て、打っている
// 場所の前後しか見えない。窓の幅が変わったときも測り直さないと同じことになる。
test("長い訳を打つと入力欄が折り返して伸び、窓の幅が変わっても測り直す", async ({ app }) => {
  await openEditor(app, SAMPLE_LINES.goodbye);
  const ed = editor(app);
  const size = () =>
    ed.evaluate((node) => ({
      height: node.getBoundingClientRect().height,
      scroll: node.scrollHeight,
      client: node.clientHeight,
    }));
  const short = await size();

  await ed.fill("折り返して全部見える訳。".repeat(30));
  await expect.poll(async () => (await size()).height).toBeGreaterThan(short.height * 2);
  // 伸ばした高さで中身が収まっている（送りの棒が要らない）。
  let now = await size();
  expect(now.scroll).toBeLessThanOrEqual(now.client);

  // 幅を狭めると折り返しが増える。resize で測り直して、また収まる。
  // 900px 以下では左の列が引き出しになって訳の欄がかえって広がるので、広い画面の
  // 並びのまま狭める。
  const tall = now.height;
  await app.setViewportSize({ width: 960, height: 720 });
  await expect.poll(async () => (await size()).height).toBeGreaterThan(tall);
  now = await size();
  expect(now.scroll).toBeLessThanOrEqual(now.client);

  // 短くすれば縮む。前の高さが下限として残らない。
  await ed.fill("短い");
  await expect.poll(async () => (await size()).height).toBeLessThan(tall);
  now = await size();
  expect(now.scroll).toBeLessThanOrEqual(now.client);

  // 入力欄を閉じたあとに幅が変わっても、測る先が無いだけで何も起きない
  // （頁の中の例外は後始末で拾う）。
  await ed.press("Escape");
  await expect(ed).toHaveCount(0);
  await app.setViewportSize({ width: 1280, height: 720 });
  await openEditor(app, SAMPLE_LINES.goodbye);
  now = await size();
  expect(now.scroll).toBeLessThanOrEqual(now.client);
});

// ---- 開き方と閉じ方 ----

// マウスで行から行へ移る道。押した時点で入力欄を差し替え、離れた行は自動保存の
// 時計を待たずに送る。送らずに移ると、その訳は時計が切れるまでブラウザーの中に
// しか無く、その間に頁を閉じれば失われる。時計は止めてあるので、ここで入った
// 保存は押したことによるものである。
test("別の行を押すと入力欄がその行へ移り、離れた行を待たずに保存する", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "さようなら。";
  await typeTranslation(page, SAMPLE_LINES.goodbye, typed);

  await translationCell(page, SAMPLE_LINES.wonderful).click();
  await expectEditorIn(page, SAMPLE_LINES.wonderful);
  // 開いた入力欄には、その行のいまの訳が入っている。
  await expect(editor(page)).toHaveValue(SAMPLE.wonderful.ja);
  // 離れた行は字の欄に戻り、打った訳を出している。
  await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);

  const expected = `${dataText(ROW.goodbye, typed)}\n`;
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(expected);
  await waitForSaved(page);
  expectOnlyLines(before, await server.readRoot(workingRel), { [SAMPLE_LINES.goodbye]: expected });
});

// キーボードだけで打っていく道（README の「Tab: 次の行へ移ります」）。Tab で移った
// 先の訳欄が焦点を受けたら入力欄に差し替わらないと、キーボードの人は打ち始められない。
// 離れた行は blur で待たずに保存する。
test("Tab で次の行へ移ると入力欄が開き、離れた行を待たずに保存する", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "もしもし。";
  await typeTranslation(page, SAMPLE_LINES.hello, typed);

  await editor(page).press("Tab");
  await expectEditorIn(page, SAMPLE_LINES.goodbye);
  await expect(editor(page)).toHaveValue(SAMPLE.goodbye.ja);

  const expected = `${dataText(ROW.hello, typed)}\n`;
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.hello)).toBe(expected);
  await waitForSaved(page);
  expectOnlyLines(before, await server.readRoot(workingRel), { [SAMPLE_LINES.hello]: expected });
});

// 入力欄の中を押すのは、字の途中へキャレットを動かすためである。そこで閉じたり
// 開き直したりすると、打ちかけの位置を失い、打った訳が保存へ回る前に欄が作り直される。
test("入力欄の中を押しても閉じず、打った字もそのまま残る", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  await typeTranslation(page, SAMPLE_LINES.goodbye, "さようなら");

  await editor(page).click({ position: { x: 4, y: 4 } });
  await expectEditorIn(page, SAMPLE_LINES.goodbye);
  await expect(editor(page)).toHaveValue("さようなら");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  // 押しただけでは送らない（時計は止めてあるので、送られたなら押したせいである）。
  expect(saves).toHaveLength(0);
});

// README の「Escape: 入力欄を閉じます。打った訳は残ります」。閉じるときに入力を
// 消せば、黙って破棄したことになる。閉じたら待たずに保存へ回す。
test("Escape は入力欄を閉じるだけで、打った訳は残して保存する", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "さようなら。";
  await typeTranslation(page, SAMPLE_LINES.goodbye, typed);

  await editor(page).press("Escape");
  await expect(editor(page)).toHaveCount(0);
  await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);

  const expected = `${dataText(ROW.goodbye, typed)}\n`;
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(expected);
  await waitForSaved(page);
  expectOnlyLines(before, await server.readRoot(workingRel), { [SAMPLE_LINES.goodbye]: expected });

  // 開き直すと、閉じる前に打った訳が入っている。
  await openEditor(page, SAMPLE_LINES.goodbye);
  await expect(editor(page)).toHaveValue(typed);
});

// 閉じた行の訳は、応答が返るまでは未保存の控えにしか無い。そのあいだに開き直した
// 入力欄へ古い保存値を入れると、1字打った瞬間に、送っている最中の訳が上書きされる
// （shownValue が未保存の控えを先に見る理由）。
test("保存の応答を待っているあいだに開き直しても、入力欄には打った訳が入る", async ({ page, server }) => {
  await openPaused(page, server);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await page.route("**/api/rows", async (route) => {
    await gate;
    await route.continue();
  });

  const typed = "さようなら。";
  await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
  await editor(page).press("Escape");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_saving"));

  await openEditor(page, SAMPLE_LINES.goodbye);
  await expect(editor(page)).toHaveValue(typed);
  await expect(rowByLine(page, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)unsaved(\s|$)/);

  release();
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(`${dataText(ROW.goodbye, typed)}\n`);
  await waitForSaved(page);
  await expect(editor(page)).toHaveValue(typed);
});

test.describe("編集できない行を挟んだとき", () => {
  // 6行目は列がヘッダーより1つ多い（8列）。internal/edit はこの行を編集させない。
  const broken = record(keyFor(SAMPLE.goodbye.source), "L01 Ryan", "Ryan_1_intro", "2", "Ryan", "Goodbye.", "", "余り");
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(["", SECTION, NODE, ROW.hello, broken, ROW.wonderful, ""]),
    }),
  });

  // Enter は「次の、出ている、編集できる行」へ進む。編集できない行へ進むと、
  // openEditor が開かずに戻り、閉じたばかりなので焦点が body へ落ちる。
  // キーボードだけで打っている人は、そこで自分がどこにいるか分からなくなる。
  test("Enter は編集できない行を飛ばして、その次の編集できる行を開く", async ({ app }) => {
    await expect(rowByLine(app, 6)).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await openEditor(app, 5);
    await editor(app).press("Enter");
    await expectEditorIn(app, 7);
    await expect(editor(app)).toHaveValue(SAMPLE.wonderful.ja);
    await expect(rowByLine(app, 6).locator("textarea.editor")).toHaveCount(0);
  });
});

// 絞り込んだ一覧を上から順に打っていけるように、Enter は隠れている行を飛ばす。
// 飛ばさないと、条件に当たらない行の入力欄が画面の外で開き、打った字の行き先が
// 見えなくなる。「し」は hello（もしもし？）と wonderful（すばらしい！）の訳にだけ
// 入っていて、goodbye（訳が空）のどの欄にも無い。
test("Enter は検索で隠れている行を飛ばして、次に出ている行を開く", async ({ app }) => {
  await app.locator("#search").fill("し");
  await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeHidden();
  await expect(rowByLine(app, SAMPLE_LINES.hello)).toBeVisible();

  await openEditor(app, SAMPLE_LINES.hello);
  await editor(app).press("Enter");
  await expectEditorIn(app, SAMPLE_LINES.wonderful);
  await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeHidden();
});

test.describe("最後の行が改行で終わらない作業コピー", () => {
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(["", SECTION, NODE, ROW.hello, ROW.goodbye, ROW.wonderful], { trailing: false }),
    }),
  });

  // README の「出ている最後の行では、閉じずにその行に留まります」。閉じると焦点が
  // body へ落ち、終わりまで来た合図も出ない。留まったうえで、保存は待たずに走らせる。
  // 最後の行はファイルの末尾でもあり、改行で終わっていない。保存で改行を足すと、
  // 翻訳者が触っていない差分がコミットに混ざる。
  test("出ている最後の行で Enter を押すと、閉じずに留まり、待たずに保存する", async ({ page, server }) => {
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const typed = "すばらしい。";
    await typeTranslation(page, SAMPLE_LINES.wonderful, typed);

    await editor(page).press("Enter");
    await expectEditorIn(page, SAMPLE_LINES.wonderful);
    // 改行は入らない。
    await expect(editor(page)).toHaveValue(typed);

    const expected = dataText(ROW.wonderful, typed);
    await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.wonderful)).toBe(expected);
    await waitForSaved(page);
    const after = await server.readRoot(workingRel);
    expectOnlyLines(before, after, { [SAMPLE_LINES.wonderful]: expected });
    expect(after.at(-1), "末尾に改行が足された").not.toBe(0x0a);
    // 入力欄はまだ開いていて、続けて打てる。
    await expect(editor(page)).toBeFocused();
  });
});

// ---- 未保存の控えと自動保存の時計 ----

// 保存済みの値に戻したのに「未保存」のままだと、送る必要の無い要求が飛び、
// 翻訳者は閉じてよいのかを読み違える。戻した行は未保存の控えから外し、何も送らない。
test("保存済みの値に戻すと未保存から外れ、何も送らない", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  const before = await server.readRoot(workingRel);
  const row = rowByLine(page, SAMPLE_LINES.hello);

  await typeTranslation(page, SAMPLE_LINES.hello, "もしもし。");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  await expect(row).toHaveClass(/(^|\s)unsaved(\s|$)/);

  await editor(page).fill(SAMPLE.hello.ja);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(row).not.toHaveClass(/(^|\s)unsaved(\s|$)/);

  // 閉じても、時計を十分に進めても、送るものが無い。
  await editor(page).press("Escape");
  await page.clock.runFor(5_000);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
  expect(saves).toHaveLength(0);
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 自動保存は「入力が止まってから」1.5 秒（autosaveDelay）。打つたびに時計を引き直さないと、
// 打っている最中の訳が途中で送られ、ゲームがホットリロードで打ちかけの訳を読み込む。
// 止まったあとは1回だけ送る。
test("打つたびに自動保存の時計を引き直し、止まってから 1.5 秒で1回だけ送る", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  await openEditor(page, SAMPLE_LINES.goodbye);

  await editor(page).pressSequentially("B");
  await page.clock.runFor(1_000);
  await editor(page).pressSequentially("y");
  // 最初の字から 2 秒経ったが、最後の字からは 1 秒。まだ送らない。
  await page.clock.runFor(1_000);
  await editor(page).pressSequentially("e");
  await page.clock.runFor(1_499);
  expect(saves).toHaveLength(0);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await page.clock.runFor(1);
  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].edits).toEqual([{ id: SAMPLE_LINES.goodbye, key: keyFor(SAMPLE.goodbye.source), translation: "Bye" }]);
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(`${dataText(ROW.goodbye, "Bye")}\n`);
  await waitForSaved(page);
  // 送ったあとに時計を進めても、同じ訳をもう一度は送らない。
  await page.clock.runFor(5_000);
  expect(saves).toHaveLength(1);
  // 入力欄は開いたまま（自動保存は閉じない）。
  await expectEditorIn(page, SAMPLE_LINES.goodbye);
});

// 保存の状態（#save-state）は読み上げの見張り（aria-live="polite"）である。打鍵のたびに
// 中身を作り直すと、文が同じ「未保存 1 件」でも、読み上げによっては変わったものとして
// 読み直され、打つ手の横で同じ文が繰り返される。文と種類が同じあいだは触らない。
test("打っているあいだ、保存の状態の文が変わらなければ、見張りの中身を作り直さない", async ({ page, server }) => {
  await openPaused(page, server);
  await openEditor(page, SAMPLE_LINES.goodbye);
  await editor(page).pressSequentially("B");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await saveState(page).evaluate((node) => {
    window.__saveStateMutations = 0;
    new MutationObserver((records) => {
      window.__saveStateMutations += records.length;
    }).observe(node, { childList: true, subtree: true, characterData: true, attributes: true });
  });
  await editor(page).pressSequentially("ye!");
  await expect(editor(page)).toHaveValue("Bye!");
  expect(await page.evaluate(() => window.__saveStateMutations)).toBe(0);

  // 文が変わるときは書き換える（送り始めると「保存しています」になる）。
  await page.clock.runFor(1_500);
  await waitForSaved(page);
  expect(await page.evaluate(() => window.__saveStateMutations)).toBeGreaterThan(0);
});

// 送った訳を未保存の控えから先に消すと、応答が来なかったときにその訳がどこにも
// 残らない。応答で「保存できた」と分かった行も、送ったあとに打ち直していれば
// 控えに残し、続けて送る。ここが崩れると、保存の往復の間に打った字が黙って消える。
test("送っているあいだに打ち直した訳は捨てず、応答のあとで続けて送る", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let held = false;
  await page.route("**/api/rows", async (route) => {
    if (!held) {
      held = true;
      await gate;
    }
    await route.continue();
  });

  const n = SAMPLE_LINES.wonderful;
  const first = "すばらしい。";
  const second = "すばらしい。本当に。";
  await typeTranslation(page, n, first);
  // 出ている最後の行なので、Enter は閉じずに送るだけ。
  await editor(page).press("Enter");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_saving"));
  await expect.poll(() => saves.length).toBe(1);

  // 送っている最中に打ち直す。
  await editor(page).fill(second);
  release();

  // 1回目の応答が返っても、打ち直した訳は未保存として残り、欄も書き換えられない。
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, first)}\n`);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  await expect(editor(page)).toHaveValue(second);

  // 自動保存の時計で続けて送る。
  await page.clock.runFor(1_500);
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, second)}\n`);
  await waitForSaved(page);
  expect(saves.map((body) => body.edits.map((e) => e.translation))).toEqual([[first], [second]]);
});

// 上と同じ往復の間に、打ち直すのではなく「元の訳」へ戻したとき。戻した値は、その
// 時点の保存値（まだ送る前の古い値）と同じである。以前は画面がそれを見て未保存の
// 控えから外していた。ところが送った値はそのままファイルに入るので、応答のあとは
// ファイルが送った値、画面が戻した値になり、それでも保存の欄は「保存済み」と言って
// 二度と送らなかった。翻訳者が最後に選んだ訳が、黙ってファイルに入らなかった
// （app.js の onInput と state.inflight）。
test("送っているあいだに元の訳へ戻しても、戻した訳をファイルへ送る", async ({ page, server }) => {
  await openPaused(page, server);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let held = false;
  await page.route("**/api/rows", async (route) => {
    if (!held) {
      held = true;
      await gate;
    }
    await route.continue();
  });

  const n = SAMPLE_LINES.wonderful;
  const sent = "すばらしい。";
  await typeTranslation(page, n, sent);
  await editor(page).press("Enter");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_saving"));

  // 送っている最中に、読み込んだときの訳へ戻す。
  await editor(page).fill(SAMPLE.wonderful.ja);
  release();
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, sent)}\n`);
  // 応答を画面が受け取り終えるまで待つ（送っている最中の表示から外れる）。
  await expect(saveState(page)).not.toHaveClass(/(^|\s)saving(\s|$)/);

  // 時計を進め、閉じて、送る機会を全部与える。
  await page.clock.runFor(1_500);
  await editor(page).press("Escape");
  await page.clock.runFor(1_500);
  await expect(translationCell(page, n)).toHaveText(SAMPLE.wonderful.ja);
  // 画面に出ている訳がファイルに入る。
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, SAMPLE.wonderful.ja)}\n`);
  await waitForSaved(page);
});

// ---- 変換中（IME） ----

// 変換中に時計が切れると、打ちかけの読み（「あこ」など）がそのまま保存され、
// 作業コピーへ書けばゲームが約2秒でそれを読み込む。変換を始めたら時計を止め、
// 確定してから引き直す。ja / ko / zh-Hans / zh-Hant のための約束である。
test("変換中は自動保存の時計を止め、打ちかけの読みを送らない", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  await openEditor(page, SAMPLE_LINES.goodbye);

  // 変換の前の1打鍵で時計が動き出す。
  await editor(page).pressSequentially("a");
  await compose(page, "compositionstart");
  await compose(page, "input", "aあこ");
  // 変換の前の打鍵から十分に経っても、変換中は送らない。
  await page.clock.runFor(10_000);
  expect(saves).toHaveLength(0);
  // 送っていれば「保存しています」を経て「保存済み」になる。未保存のまま抱えている。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await compose(page, "compositionend", "a朝");
  await page.clock.runFor(1_499);
  expect(saves).toHaveLength(0);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  await page.clock.runFor(1);
  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].edits.map((e) => e.translation)).toEqual(["a朝"]);
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(`${dataText(ROW.goodbye, "a朝")}\n`);
  await waitForSaved(page);
});

// 変換中のキーは1つも横取りしない（app.js 冒頭の約束）。Enter を横取りすると
// 変換を確定できなくなり、Escape を横取りすると変換を取り消すつもりで入力欄が閉じる。
// 見張りは3つ（compositionstart からの状態、e.isComposing、keyCode 229）で、
// どれか1つが立てば素通しにする。
test("変換中の Enter と Escape は横取りせず、行も移らず閉じもしない", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  await openEditor(page, SAMPLE_LINES.goodbye);

  await test.step("compositionstart のあと", async () => {
    await compose(page, "compositionstart");
    expect(await keydown(page, { key: "Enter", code: "Enter" })).toBe(false);
    expect(await keydown(page, { key: "Escape", code: "Escape" })).toBe(false);
    await expectEditorIn(page, SAMPLE_LINES.goodbye);
    await compose(page, "compositionend", "朝");
  });
  await test.step("isComposing が立っている", async () => {
    expect(await keydown(page, { key: "Enter", code: "Enter", isComposing: true })).toBe(false);
  });
  await test.step("keyCode が 229", async () => {
    expect(await keydown(page, { key: "Enter", code: "Enter", keyCode: 229 })).toBe(false);
  });

  await expectEditorIn(page, SAMPLE_LINES.goodbye);
  await expect(editor(page)).toHaveValue("朝");
  expect(saves).toHaveLength(0);
});

// compositionend が確定の keydown より先に届く並びだと、3つの見張りを全部すり抜けて
// 素の Enter に見える。確定から 100ms（composedGrace）以内の Enter は行送りに使わず、
// 改行も入れない。確定だけで行が飛ぶほうが重い事故だからである。猶予が過ぎた
// Enter はふつうに次の行へ進む。
test("確定の直後の Enter は行送りに使わず、少し置いた Enter で次の行へ進む", async ({ page, server }) => {
  await openPaused(page, server);
  await openEditor(page, SAMPLE_LINES.hello);
  await compose(page, "compositionstart");
  await compose(page, "input", "あさ");
  await compose(page, "compositionend", "朝");

  // 時計は止まっているので、確定からの経過は 0ms のまま。
  await editor(page).press("Enter");
  await expectEditorIn(page, SAMPLE_LINES.hello);
  await expect(editor(page)).toHaveValue("朝");

  await page.clock.runFor(100);
  await editor(page).press("Enter");
  await expectEditorIn(page, SAMPLE_LINES.goodbye);
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.hello)).toBe(`${dataText(ROW.hello, "朝")}\n`);
  await waitForSaved(page);
});

// 猶予は経過時間（performance.now）で測る。壁時計（Date.now）で測ると、確定と Enter の
// あいだに OS の時計が後ろへ動いたとき（NTP の段差、休止からの復帰）、差が負のまま
// 猶予を超えるまで Enter の行送りが効かなくなる。時計を1時間戻せば1時間効かない。
// page.clock.setSystemTime は壁時計だけを動かし、経過時間の時計は動かさない。
test("確定のあとで OS の時計が戻っても、少し置いた Enter で次の行へ進む", async ({ page, server }) => {
  await openPaused(page, server);
  await openEditor(page, SAMPLE_LINES.hello);
  await compose(page, "compositionstart");
  await compose(page, "input", "あさ");
  await compose(page, "compositionend", "朝");

  // 壁時計だけを1時間戻す。
  await page.clock.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  expect(await page.evaluate(() => Date.now())).toBe(new Date("2026-01-01T00:00:00Z").getTime());

  await page.clock.runFor(100);
  await editor(page).press("Enter");
  await expectEditorIn(page, SAMPLE_LINES.goodbye);
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.hello)).toBe(`${dataText(ROW.hello, "朝")}\n`);
  await waitForSaved(page);
});

// 保存の往復の途中で変換を始めることがある。応答のあとに「送っている間に足された
// ぶん」を送る時計が引き直されるが、そのとき変換中なら、欄にあるのは打ちかけの読みで
// ある。送る直前にも変換中かを見て、確定してから送る。
test("保存の応答が変換中に返っても、打ちかけの読みは送らず、確定してから送る", async ({ page, server }) => {
  await openPaused(page, server);
  const saves = trackSaves(page);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let held = false;
  await page.route("**/api/rows", async (route) => {
    if (!held) {
      held = true;
      await gate;
    }
    await route.continue();
  });

  const n = SAMPLE_LINES.wonderful;
  await typeTranslation(page, n, "a");
  // 出ている最後の行なので、Enter は閉じずに送るだけ。
  await editor(page).press("Enter");
  await expect.poll(() => saves.length).toBe(1);

  await compose(page, "compositionstart");
  await compose(page, "input", "aあさ");
  release();
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, "a")}\n`);
  // 応答を受け取ると、欄にある読みが未保存として残り、送り直しの時計が引き直される。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  // その時計が切れても、変換中なので送らない。
  await page.clock.runFor(10_000);
  expect(saves).toHaveLength(1);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await compose(page, "compositionend", "a朝");
  await page.clock.runFor(1_500);
  await expect.poll(() => lineOnDisk(server, n)).toBe(`${dataText(ROW.wonderful, "a朝")}\n`);
  await waitForSaved(page);
  expect(saves.map((body) => body.edits[0].translation)).toEqual(["a", "a朝"]);
});

// ---- 入れられない字 ----

// internal/edit は CR / LF / NUL を含む値を拒む（公開ファイルの読み手が1物理行=1レコードで
// 読むため）。拒まれる値を送ると、その行は「保存できない行」になる。入ってくるのは
// ほとんど貼り付けなので、改行は空白に置き換えてその行に収め、NUL は落とす。
test("貼り付けた改行は空白に置き換わり、NUL は落ちて、1行の訳として保存される", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  await openEditor(app, SAMPLE_LINES.goodbye);
  const prevented = await editor(app).evaluate((ed) => {
    const data = new DataTransfer();
    data.setData("text/plain", "一行目\r\n二行目\n\n三行目\u0000終わり");
    const ev = new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: data });
    ed.dispatchEvent(ev);
    return ev.defaultPrevented;
  });
  // ブラウザー任せにしない（改行の扱いが実装ごとに違う）。
  expect(prevented).toBe(true);
  const expected = "一行目 二行目 三行目終わり";
  await expect(editor(app)).toHaveValue(expected);
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await editor(app).press("Escape");
  const line = `${dataText(ROW.goodbye, expected)}\n`;
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(line);
  await waitForSaved(app);
  // 保存できない行にはなっていない。
  await expect(rowByLine(app, SAMPLE_LINES.goodbye)).not.toHaveClass(/(^|\s)save-failed(\s|$)/);
  expectOnlyLines(before, await server.readRoot(workingRel), { [SAMPLE_LINES.goodbye]: line });
});

// 改行は貼り付け以外からも入りうる（ドラッグで落とした字、入力補助など）。どの道で
// 入っても送る値は1行にする。打っている位置も跳ねさせない。
test("貼り付け以外で入った改行も空白に置き換え、打つ位置を保つ", async ({ app }) => {
  await openEditor(app, SAMPLE_LINES.goodbye);
  const ed = editor(app);
  await ed.fill("前後");
  // 字の間にキャレットを置き、そこへ改行を差し込む（貼り付けの事象は出ない）。
  await ed.evaluate((node) => node.setSelectionRange(1, 1));
  await app.keyboard.insertText("\n");
  await expect(ed).toHaveValue("前 後");
  // 値を入れ替えると、キャレットは既定では末尾へ飛ぶ。差し込んだ直後の位置に留める。
  expect(await ed.evaluate((node) => [node.selectionStart, node.selectionEnd])).toEqual([2, 2]);
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  await ed.press("Escape");
  await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText("前 後");
  await waitForSaved(app);
});

// 前後の空白は、引用しないと読み手（ゲーム・publish・validate）ごとに別の値になる。
// internal/edit は前後に空白がある訳を引用して書き、書いたとおりに読み戻せるように
// している。画面から見ると、打ったとおりに保存され、値が変わったという断りも出ない。
test("前後に空白のある訳は、打ったとおりの値で保存される", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  const typed = "  さようなら。 ";
  await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
  await editor(app).press("Escape");

  const line = `${dataText(ROW.goodbye, typed)}\n`;
  expect(line, "見本の組み方が引用していない").toContain(`"${typed}"`);
  await expect.poll(() => lineOnDisk(server, SAMPLE_LINES.goodbye)).toBe(line);
  await waitForSaved(app);
  expectOnlyLines(before, await server.readRoot(workingRel), { [SAMPLE_LINES.goodbye]: line });

  // 画面の値も削られていない。値を変えたという断りも出ない。
  expect(await translationCell(app, SAMPLE_LINES.goodbye).textContent()).toBe(typed);
  await expect(rowByLine(app, SAMPLE_LINES.goodbye).locator(".row-note")).toBeHidden();
});

// ---- 保存の要求と応答 ----

// 行番号だけで送ると、手前でよそが行を足したり消したりしていたときに訳が別の
// キーの行へ入る。画面は常にキーを載せ、読んだときの版（ファイル全体の SHA-256）も
// 載せる。保存のあとは、次の要求に保存後の版を載せる。古い版のまま送ると、
// 自分の保存に対して 409 が出る。
test("保存の要求は行番号にキーと読んだときの版を添えて送る", async ({ app, server }) => {
  const saves = trackSaves(app);
  const sha = (buf) => createHash("sha256").update(buf).digest("hex");
  const v1 = sha(await server.readRoot(workingRel));

  const request = app.waitForRequest((req) => req.method() === "POST" && new URL(req.url()).pathname === "/api/rows");
  await typeTranslation(app, SAMPLE_LINES.goodbye, "さようなら。");
  await editor(app).press("Escape");
  const sent = await request;
  expect(sent.headers()["content-type"]).toBe("application/json");
  expect(new URL(sent.url()).origin).toBe(server.origin);
  expect(sent.postDataJSON()).toEqual({
    locale: "ja",
    baseVersion: v1,
    edits: [{ id: SAMPLE_LINES.goodbye, key: keyFor(SAMPLE.goodbye.source), translation: "さようなら。" }],
  });
  await waitForSaved(app);

  const v2 = sha(await server.readRoot(workingRel));
  await typeTranslation(app, SAMPLE_LINES.hello, "もしもし。");
  await editor(app).press("Escape");
  await expect.poll(() => saves.length).toBe(2);
  await waitForSaved(app);
  expect(saves[1].baseVersion).toBe(v2);
  expect(saves[1].edits).toEqual([
    { id: SAMPLE_LINES.hello, key: keyFor(SAMPLE.hello.source), translation: "もしもし。" },
  ]);
});

// 待ち受けは保存後の行を読み直した値（translation）を返し、入力と違えば断り（warning）を
// 付ける（internal/web の handleRows）。画面はその値を出して、画面とファイルを同じにし、
// 断りを行に添える。いまの internal/edit は前後の空白も引用して書くので、画面から
// 値が変わる道は見つかっていない。そのため応答だけを差し替えて、画面の描き方を見る。
test("保存の応答が値を変えたと言ったら、その値を行に出して断りを添える", async ({ app }) => {
  const shown = "ファイルにある値";
  const warning = msg("ja", "warn.value_normalized");
  await app.route("**/api/rows", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    for (const r of body.results) {
      if (r.id === SAMPLE_LINES.goodbye) {
        r.translation = shown;
        r.warning = warning;
      }
    }
    await route.fulfill({ response, json: body });
  });

  await typeTranslation(app, SAMPLE_LINES.goodbye, "さようなら。");
  await editor(app).press("Escape");
  await waitForSaved(app);
  await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(shown);
  const note = rowByLine(app, SAMPLE_LINES.goodbye).locator(".row-note");
  await expect(note).toBeVisible();
  await expect(note).toHaveText(warning);

  // 開き直すと、入力欄にもファイルの値が入る。書き換えれば断りは消える。
  await openEditor(app, SAMPLE_LINES.goodbye);
  await expect(editor(app)).toHaveValue(shown);
  await editor(app).fill("さようなら！");
  await expect(note).toBeHidden();
});

// 画面は件数を数えない。保存の応答に入っている、待ち受けが数え直した件数・チップの
// 行数・断り書きをそのまま写す。写さないと、訳を入れた行のバッジだけが消えて、
// チップと件数の欄は入れる前の数のまま残る。数え直しが局所的であること
// （note.counts_local）も、保存したあとで初めて断る。
test("保存すると、件数とチップの数と断り書きを待ち受けが数え直したものに置き換える", async ({ app }) => {
  const label = msg("ja", "category.untranslated");
  const chip = app.locator("#filters label.chip").filter({ has: app.getByText(label, { exact: true }) });
  const count = app.locator("#counts li").filter({ hasText: ` / ${label} ` });
  const countLine = (n) => `${msg("ja", "status.todo")} / ${label} ${msg("ja", "ui.count_value", { count: n })}`;
  const localNote = msg("ja", "note.counts_local");

  await expect(chip.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 1 }));
  await expect(chip).toHaveAttribute("title", msg("ja", "ui.count_value", { count: 1 }));
  await expect(count).toHaveText(countLine(1));
  await expect(app.locator("#notes li").filter({ hasText: localNote })).toHaveCount(0);
  // 条件を組み直したかを見分けるために、いまのチップの要素へ印を付けておく。
  await chip.evaluate((node) => {
    node.dataset.probe = "before-save";
  });

  await typeTranslation(app, SAMPLE_LINES.goodbye, "さようなら。");
  await editor(app).press("Escape");
  await waitForSaved(app);

  await expect(chip.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 0 }));
  await expect(chip).toHaveAttribute("title", msg("ja", "ui.count_value", { count: 0 }));
  await expect(count).toHaveText(countLine(0));
  await expect(app.locator("#notes li").filter({ hasText: localNote })).toHaveCount(1);
  // 条件は組み直さない（触っている最中の選択が跳ねないように）。数を書き換えたのは
  // 前と同じチップの要素である。
  await expect(chip).toHaveAttribute("data-probe", "before-save");
});

test.describe("同じキーの行が2つある作業コピー", () => {
  // 6行目と7行目が同じ原文（＝同じキー）で、どちらも訳が空。Mod が書き出す CSV なので、
  // 同じキーが2行あることを止める仕掛けはどこにも無い。
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(["", SECTION, NODE, ROW.hello, ROW.goodbye, ROW.goodbye, ROW.wonderful, ""]),
    }),
  });

  // 待ち受けはバッジをキー単位で付け直すので、訳が入ったキーの「未翻訳」はそのキーの
  // どの行からも落ちる。応答に入っている行だけを描き直すと、もう一方の行が古い
  // バッジのまま残り、チップの数（待ち受けが数え直したもの）とも食い違う。
  // 保存するのは書き換えた1行だけで、もう一方の行は1バイトも変わらない。
  test("片方の行を保存すると、同じキーのもう一方の行のバッジも描き直す", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    const label = msg("ja", "category.untranslated");
    const badge = (n) => rowByLine(app, n).locator(".badge", { hasText: label });
    await expect(badge(6)).toHaveCount(1);
    await expect(badge(7)).toHaveCount(1);

    const typed = "さようなら。";
    await typeTranslation(app, 6, typed);
    await editor(app).press("Escape");
    await waitForSaved(app);

    await expect(badge(6)).toHaveCount(0);
    await expect(badge(7)).toHaveCount(0);
    const chip = app.locator("#filters label.chip").filter({ has: app.getByText(label, { exact: true }) });
    await expect(chip.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 0 }));
    // 7行目の訳は空のまま（画面もファイルも）。
    await expect(translationCell(app, 7)).toHaveText("");
    expectOnlyLines(before, await server.readRoot(workingRel), { 6: `${dataText(ROW.goodbye, typed)}\n` });
  });
});

test.describe("キーの欄が空の行がある作業コピー", () => {
  // 6行目はキーの欄が空。列の数はヘッダーと合うので、internal/edit は編集させる。
  const keyless = { ...ROW.goodbye, key: "" };
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(["", SECTION, NODE, ROW.hello, keyless, ROW.wonderful, ""]),
    }),
  });

  // キーを持たない行は、要求のキーを空にして送る。待ち受けは空のキーを照合しない
  // （internal/web の rowEdit.Key）。画面が行番号から別の行のキーを拾って載せたり、
  // キーが無いことで保存を諦めたりすると、この行は二度と訳せない。
  test("キーの無い行も、キーを空にして送り、その行だけを書き換える", async ({ app, server }) => {
    const saves = trackSaves(app);
    const before = await server.readRoot(workingRel);
    const typed = "さようなら。";
    await typeTranslation(app, 6, typed);
    await editor(app).press("Escape");

    const line = `${dataText(keyless, typed)}\n`;
    await expect.poll(() => lineOnDisk(server, 6)).toBe(line);
    await waitForSaved(app);
    expect(saves).toHaveLength(1);
    expect(saves[0].edits).toEqual([{ id: 6, key: "", translation: typed }]);
    await expect(rowByLine(app, 6)).not.toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, 6)).toHaveText(typed);
    expectOnlyLines(before, await server.readRoot(workingRel), { 6: line });
  });

  // 待ち受けに届かないときは、まだファイルに入っていない訳を行番号とキーを添えて並べる
  // （app.js の renderUnsent）。キーの無い行では行番号だけにし、空のキーを並べない。
  test("待ち受けに届かないとき、キーの無い行の訳は行番号だけを添えて並べる", async ({ app }) => {
    await app.route("**/api/rows", (route) => route.abort("connectionrefused"));
    await typeTranslation(app, 6, "さようなら。");
    await editor(app).press("Escape");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.unreachable"));
    await expect(app.locator("#unsent-list > li")).toHaveText([
      `${msg("ja", "ui.unsent_line", { line: 6 })}: さようなら。`,
    ]);
  });
});
