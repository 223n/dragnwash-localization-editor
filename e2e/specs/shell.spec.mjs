// 画面の枠組み（左の列と引き出し、スラッシュと Escape の近道、貼り付く帯の高さ、
// beforeunload の引き止め、外へ出さない守り）を見る。
//
// 枠組みは行の中身を1つも持たないが、訳を失わない約束の半分はここで守られている。
//
//   - スラッシュは「入力欄の外」でだけ効く。訳の入力欄や検索の欄で横取りすると、
//     その字が訳にも検索語にも打てなくなる。変換中と修飾キー付きも横取りしない
//     （app.js の document の keydown、internal/web の doc.go「スラッシュ」）。
//   - 未保存のまま頁を閉じようとしたら beforeunload で止める（app.js 冒頭の約束）。
//     止めなければ、打った訳はブラウザーの中だけで消える。
//   - 貼り付く帯の高さを --top-height へ渡し、焦点の入る要素を帯の下へ潜らせない
//     （watchTopHeight、doc.go「余白は帯の高さから作る」）。
//   - 取りにいく先は自分自身だけで、トークンは Cookie（HttpOnly）へ移して URL にも
//     履歴にも残さない。どの応答にも CSP と Cache-Control: no-store を付ける
//     （internal/web の middleware.go と token.go）。
//
// 保存が入ったかは、画面の表示ではなくディスクのバイトで見る。触っていない行が
// 1バイトも変わらないことも、書き込みを伴う試験では一緒に見る。
//
// 自動保存の時計に左右される試験は page.clock で時計を止めてから開く。止めないと、
// 遅い機械では「欄から離れたので待たずに送った」のか「1.5 秒の時計が切れた」のかを
// 取り違える。page.clock.install は頁を開く前に要るので、その試験は app ではなく
// page と server から開く。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { saveCoverage } from "../support/coverage.mjs";
import { SAMPLE, SAMPLE_LINES, keyFor, workingCopy, sampleRepo } from "../support/repo.mjs";
import {
  dataRows,
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
const s = SAMPLE;
const L = SAMPLE_LINES;
const typed = "さようなら。";

// 画面の幅。900px 以下が引き出し、901px からが左の列（app.js の narrow と app.css の @media）。
const WIDE = { width: 1280, height: 720 };
const NARROW = { width: 375, height: 812 };

// wrongKey はファイルに無いキー。保存の要求をこれに差し替えると、待ち受けは
// 「この行はずれています」で書かずに断る（行ごとの失敗を本物の応答で作る）。
const wrongKey = keyFor("この原文は見本のどこにも無い");

// ---- 見本 ----

// 既定の見本（support/repo.mjs の sampleWorkingCopy）と同じ形を、goodbye の訳と
// 後ろに足す行を変えられるように組み直す。よそが書き換えたファイルもここで組む。
const HEAD = ["", "# ===== Level 1: Ryan (Sunny) =====", "# --- intro: Ryan_1_intro ---"];

function jaCopy({ goodbye = s.goodbye.ja, extra = [] } = {}) {
  return workingCopy([
    ...HEAD,
    { ...s.hello, translation: s.hello.ja },
    { ...s.goodbye, translation: goodbye },
    { ...s.wonderful, translation: s.wonderful.ja },
    ...extra,
    "",
  ]);
}

// fillers は一覧をスクロールさせるための行。再生順には無いが、列はそろっているので
// 編集できる行として並ぶ。
function fillers(count) {
  return Array.from({ length: count }, (_, i) => ({
    source: `Filler line ${String(i).padStart(3, "0")}.`,
    speaker: "Ryan",
    order: String(10 + i),
    translation: `埋め草${i}`,
  }));
}

// ---- ディスクの読み方 ----

// splitLines は改行を残したまま物理行に分ける。
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

// waitForDiskChange はファイルが before から変わるまで待つ。
async function waitForDiskChange(server, before) {
  await expect
    .poll(async () => (await server.readRoot(workingRel)).equals(before), { timeout: 10_000 })
    .toBe(false);
}

// expectOnlyTranslation は、物理行 line の訳（最終フィールド、もとは空）だけが value に
// 変わり、ほかの行が1バイトも変わっていないことを確かめる。
async function expectOnlyTranslation(server, before, line, value) {
  const b = splitLines(before);
  const a = splitLines(await server.readRoot(workingRel));
  expect(a, "物理行の数が変わった").toHaveLength(b.length);
  for (let i = 0; i < b.length; i++) {
    if (i === line - 1) {
      continue;
    }
    expect(a[i].equals(b[i]), `${i + 1}行目が変わった`).toBe(true);
  }
  const edited = b[line - 1].toString("utf8").replace(/,(\r?\n)$/, `,${value}$1`);
  expect(a[line - 1].toString("utf8")).toBe(edited);
}

// ---- 画面の操作 ----

// isPath は URL のパスだけで当てる述語（page.route に渡す）。
function isPath(pathname) {
  return (url) => url.pathname === pathname;
}

const rowsApi = isPath("/api/rows");
const linesApi = isPath("/api/lines");

// openPaused は偽の時計を入れてから画面を開き、開き終えたところで時計を止める。
// 止めたあとは page.clock.runFor で進めたぶんしか時間が流れない。
async function openPaused(page, server) {
  await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
  await openApp(page, server);
  await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
}

// nextSave は次の保存の応答を待つ。操作の前に呼んでおく。
function nextSave(page) {
  return page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/api/rows" && res.request().method() === "POST",
  );
}

// pressOutside は焦点を body へ落としてから key を押す（「入力欄の外」で押す）。
async function pressOutside(page, key) {
  await page.evaluate(() => {
    const active = document.activeElement;
    if (active && active !== document.body) {
      active.blur();
    }
  });
  await page.keyboard.press(key);
}

// dispatchKey は要素へ keydown を投げ、画面が既定の動作を止めた（横取りした）かを返す。
//
// 変換中の打鍵（isComposing、keyCode 229）は Playwright の keyboard では作れないので、
// 事象を直に投げる。古い形の keyCode は初期化の辞書で入らない実装があるので、
// 入らなければ読み口を差し替える（edit.spec.mjs と同じやり方）。
function dispatchKey(locator, init) {
  return locator.evaluate((node, options) => {
    const ev = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...options });
    if (options.keyCode !== undefined && ev.keyCode !== options.keyCode) {
      Object.defineProperty(ev, "keyCode", { get: () => options.keyCode });
    }
    node.dispatchEvent(ev);
    return ev.defaultPrevented;
  }, init);
}

// cls は class 属性に name が含まれることを当てる正規表現。
function cls(name) {
  return new RegExp(`(^|\\s)${name}(\\s|$)`);
}

const body = (page) => page.locator("body");
const menu = (page) => page.locator("#menu");
const sidebar = (page) => page.locator("#sidebar");
const backdrop = (page) => page.locator("#backdrop");
const search = (page) => page.locator("#search");

// expectDrawerOpen / expectDrawerClosed は狭い画面の引き出しの状態を確かめる。
// 開閉は body のクラスと幕と aria-expanded の3つがそろって初めて正しい。
async function expectDrawerOpen(page) {
  await expect(body(page)).toHaveClass(cls("sidebar-open"));
  await expect(backdrop(page)).toBeVisible();
  await expect(menu(page)).toHaveAttribute("aria-expanded", "true");
  await expect(sidebar(page)).toBeInViewport();
}

async function expectDrawerClosed(page) {
  await expect(body(page)).not.toHaveClass(cls("sidebar-open"));
  await expect(backdrop(page)).toBeHidden();
  await expect(menu(page)).toHaveAttribute("aria-expanded", "false");
  await expect(sidebar(page)).not.toBeInViewport();
}

// topHeight は --top-height の値（px）。まだ入っていなければ NaN。
function topHeight(page) {
  return page.evaluate(() =>
    parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--top-height")),
  );
}

// bandHeight は貼り付く帯（.top）のいまの高さ（px）。
function bandHeight(page) {
  return page.evaluate(() => document.querySelector(".top").getBoundingClientRect().height);
}

// expectTopHeightFollowsBand は --top-height がいまの帯の高さと一致するまで待ち、その高さを返す。
async function expectTopHeightFollowsBand(page) {
  await expect
    .poll(async () => Math.abs((await topHeight(page)) - (await bandHeight(page))))
    .toBeLessThan(0.5);
  return bandHeight(page);
}

// reachable は selector の要素の真ん中を押したとき、その要素（か中身）に当たるか。
// Playwright の click は押す前に要素を画面へ送るので、帯の下に潜っていても押せてしまう。
function reachable(page, selector) {
  return page.evaluate((sel) => {
    const target = document.querySelector(sel);
    const box = target.getBoundingClientRect();
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
    return hit !== null && (hit === target || target.contains(hit));
  }, selector);
}

// failLines は次の /api/lines を 500 にし、読み直しを押して失敗の理由を帯に出す。
// 帯（.top）の中の #message が埋まるので、帯が伸びる。
async function failReload(page) {
  await page.route(linesApi, (route) => route.fulfill({ status: 500, body: "boom" }));
  await page.locator("#reload").click();
  await expect(page.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
}

// tryClose は beforeunload を走らせて頁を閉じにいき、何が起きたかを返す。
//
// 引き止められたら dialog を断って頁を残す（{ type: "beforeunload" }）。そのまま
// 閉じたら { type: "closed" }。Chromium は利用者の操作（クリックや打鍵）を1度も
// 受けていない頁では beforeunload の確認を出さないので、呼ぶ前に必ず頁を操作しておく。
function tryClose(page) {
  const outcome = new Promise((resolve) => {
    page.once("dialog", async (dialog) => {
      const type = dialog.type();
      await dialog.dismiss();
      resolve({ type });
    });
    page.once("close", () => resolve({ type: "closed" }));
  });
  return page.close({ runBeforeUnload: true }).then(() => outcome);
}

// ---- スラッシュの近道 ----

test.describe("広い画面のスラッシュ", () => {
  test.use({ viewport: WIDE });

  // 行の途中から絞り込みへ戻るための近道である（README「行の途中から触りたいときは / を
  // 押してください」）。押したスラッシュが検索の欄に字として入ると、前に打った語の
  // 後ろに「/」が付いて何にも当たらなくなる。前の語は選んだ状態にして、打てば
  // 置き換わるようにする。
  test("入力欄の外でスラッシュを押すと検索の欄へ移り、字は入らず、前の検索語を選ぶ", async ({ app }) => {
    await search(app).fill("Hello");
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await pressOutside(app, "/");
    await expect(search(app)).toBeFocused();
    await expect(search(app)).toHaveValue("Hello");
    const selection = await search(app).evaluate((el) => [el.selectionStart, el.selectionEnd]);
    expect(selection).toEqual([0, "Hello".length]);
    // 広い画面では引き出しを使わない。
    await expect(body(app)).not.toHaveClass(cls("sidebar-open"));
    await expect(backdrop(app)).toBeHidden();
  });

  // 列を畳んでいるときに検索の欄へ焦点だけ移すと、見えない欄に字が入る。畳んでいれば
  // 先に列を出す（app.js の keydown）。#menu で畳んだ直後（焦点は #menu のボタン）に
  // 押す、がいちばんありそうな流れなので、そのまま押す。
  test("列を畳んでいても、スラッシュで列を出してから検索の欄へ移る", async ({ app }) => {
    await menu(app).click();
    await expect(sidebar(app)).toBeHidden();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "false");

    await app.keyboard.press("/");
    await expect(body(app)).not.toHaveClass(cls("sidebar-collapsed"));
    await expect(sidebar(app)).toBeVisible();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "true");
    await expect(search(app)).toBeFocused();
    await expect(search(app)).toHaveValue("");
  });

  // 検索の欄とロケールの選択は字を打つ（選ぶ）ところである。ここでスラッシュを横取りすると、
  // 「/」を含む語を検索できない。ロケールの選択で横取りすると、選択の最中に焦点が
  // 引き剥がされる（isTyping が INPUT・TEXTAREA・SELECT を数える）。
  test("検索の欄ではスラッシュが検索語に入る", async ({ app }) => {
    await search(app).fill("a");
    await app.keyboard.press("/");
    await expect(search(app)).toBeFocused();
    await expect(search(app)).toHaveValue("a/");
  });

  test("ロケールの選択に焦点があるときは、スラッシュで焦点を引き剥がさない", async ({ app }) => {
    await app.locator("#locale").focus();
    await app.keyboard.press("/");
    await expect(app.locator("#locale")).toBeFocused();
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(search(app)).not.toBeFocused();
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  });

  // 絞り込みのチェックボックスは字を打つところではない。数えていたころは、条件を選んだ
  // 直後にスラッシュを押しても黙って何も起きなかった（doc.go「スラッシュ」）。
  // 押したスラッシュで条件の選択が変わってもいけない。
  test("絞り込みのチェックボックスに焦点があるときは、スラッシュで検索の欄へ移る", async ({ app }) => {
    const chip = app.locator("#filters input[type=checkbox]").first();
    await chip.check();
    await expect(chip).toBeFocused();

    await app.keyboard.press("/");
    await expect(search(app)).toBeFocused();
    await expect(search(app)).toHaveValue("");
    await expect(chip).toBeChecked();
  });
});

test.describe("狭い画面のスラッシュ", () => {
  test.use({ viewport: NARROW });

  // 狭い画面では検索の欄は引き出しの中にあり、閉じているあいだは画面の外にある。
  // 焦点だけ移すと、見えない欄に字が入り、一覧だけが黙って絞られる。先に引き出しを開く
  // （README の操作の表「狭い画面では、先に左の列（引き出し）を開きます」）。
  test("スラッシュで引き出しを開いてから検索の欄へ移る", async ({ app }) => {
    await expectDrawerClosed(app);

    await pressOutside(app, "/");
    await expectDrawerOpen(app);
    await expect(search(app)).toBeFocused();
    await expect(search(app)).toHaveValue("");
    await expect.poll(() => reachable(app, "#search")).toBe(true);
  });

  // いちばん重い約束。訳の入力欄でスラッシュが効くと、訳に「/」が打てなくなるうえ、
  // 引き出しが一覧の上に被さって、打っている行が見えなくなる。打った「/」はそのまま
  // 訳としてファイルへ入らなければならない。
  test("訳の入力欄ではスラッシュが訳に入り、そのままファイルへ保存される", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    const value = "A/B";
    await openEditor(app, L.goodbye);
    await editor(app).pressSequentially(value);

    await expect(editor(app)).toBeFocused();
    await expect(editor(app)).toHaveValue(value);
    await expect(search(app)).not.toBeFocused();
    await expectDrawerClosed(app);

    await editor(app).press("Enter");
    await waitForDiskChange(server, before);
    await waitForSaved(app);
    await expectOnlyTranslation(server, before, L.goodbye, value);
  });

  // Ctrl+/ などはブラウザーや支援技術の側に意味がある。横取りすると、その操作が効かなく
  // なる（app.js の keydown「修飾キーが付いているものも…触らない」）。
  test("修飾キーの付いたスラッシュは横取りしない", async ({ app }) => {
    for (const modifier of ["Control", "Alt", "Meta"]) {
      await pressOutside(app, `${modifier}+/`);
      await expect(search(app), `${modifier}+/`).not.toBeFocused();
      await expectDrawerClosed(app);
    }
  });

  // ja / ko / zh-Hans / zh-Hant のために変換中のキーは1つも横取りしない（app.js 冒頭）。
  // 検索の欄で変換を Escape で取り消したときに引き出しが閉じると、打ちかけの読みごと
  // 焦点が #menu へ飛ぶ。変換中の印は isComposing と keyCode 229 の2つを見る。
  test("変換中のスラッシュと Escape は横取りせず、引き出しも開け閉めしない", async ({ app }) => {
    for (const marker of [{ isComposing: true }, { keyCode: 229 }]) {
      const label = JSON.stringify(marker);
      expect(await dispatchKey(body(app), { key: "/", ...marker }), label).toBe(false);
      await expect(search(app), label).not.toBeFocused();
      await expect(body(app), label).not.toHaveClass(cls("sidebar-open"));
    }

    // 印の無い同じ投げ方なら横取りする。投げた事象が画面へ届いていることの確かめ。
    expect(await dispatchKey(body(app), { key: "/" })).toBe(true);
    await expectDrawerOpen(app);
    await expect(search(app)).toBeFocused();

    for (const marker of [{ isComposing: true }, { keyCode: 229 }]) {
      const label = JSON.stringify(marker);
      expect(await dispatchKey(search(app), { key: "Escape", ...marker }), label).toBe(false);
      await expect(body(app), label).toHaveClass(cls("sidebar-open"));
      await expect(search(app), label).toBeFocused();
    }

    expect(await dispatchKey(search(app), { key: "Escape" })).toBe(true);
    await expectDrawerClosed(app);
  });
});

// ---- 左の列と引き出し ----

test.describe("狭い画面の引き出し", () => {
  test.use({ viewport: NARROW });

  // 引き出しは一覧の上に被さる。キーボードだけで閉じられないと、一覧へ戻れない。
  // 閉じたら焦点は開いたボタンへ戻す。落とすと body へ飛び、どこにいるか分からなくなる
  // （app.js の #sidebar-close の注記）。閉じるのは引き出しだけで、検索語は外さない。
  test("Escape で引き出しを閉じ、焦点を #menu へ戻し、検索語は残す", async ({ app }) => {
    await pressOutside(app, "/");
    await expectDrawerOpen(app);
    await search(app).fill("Hello");
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await app.keyboard.press("Escape");
    await expectDrawerClosed(app);
    await expect(menu(app)).toBeFocused();
    await expect(search(app)).toHaveValue("Hello");
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
  });

  // #menu は aria-controls で列を指し、開閉のたびに aria-expanded を書く（index.html）。
  // 読み上げで操作している人には、この属性だけが開いているかどうかの手がかりになる。
  // 幕（#backdrop）は引き出しのあいだだけ出し、押せば閉じる。
  test("#menu で引き出しを開き、幕を押すと閉じる", async ({ app }) => {
    await expectDrawerClosed(app);
    await menu(app).click();
    await expectDrawerOpen(app);

    // 幕のうち、引き出しに隠れていない右側を押す。
    await backdrop(app).click({ position: { x: NARROW.width - 10, y: NARROW.height / 2 } });
    await expectDrawerClosed(app);
  });

  test("閉じるボタンで引き出しを閉じ、焦点を #menu へ戻す", async ({ app }) => {
    await menu(app).click();
    await expectDrawerOpen(app);
    await app.locator("#sidebar-close").click();
    await expectDrawerClosed(app);
    await expect(menu(app)).toBeFocused();
  });

  // 狭い画面で開いた引き出しを広い画面へ持ち越すと、幕が残って一覧を押せない
  // （幕は広い画面では描かないが、body の sidebar-open と aria-expanded が食い違う）。
  // 900px をまたいだら閉じる（app.js の narrow の change）。狭い画面へ戻っても勝手に開かない。
  test("900px をまたいで幅が変わると、開いていた引き出しを閉じる", async ({ app }) => {
    await menu(app).click();
    await expectDrawerOpen(app);

    await app.setViewportSize(WIDE);
    await expect(body(app)).not.toHaveClass(cls("sidebar-open"));
    await expect(backdrop(app)).toBeHidden();
    // 広い画面の列は畳んでいないので、出ている。
    await expect(sidebar(app)).toBeVisible();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "true");

    await app.setViewportSize({ width: 800, height: 600 });
    await expectDrawerClosed(app);
  });

  // 入力欄から #menu へ移ると、入力欄は閉じる。そのとき打った訳を送らずに引き出しの
  // 裏へ置き去りにすると、翻訳者は引き出しを閉じるまで保存されていないことに気づけない。
  // 自動保存の時計は止めてあるので、ファイルに入るのは欄から離れたときの即時の保存である。
  test("入力欄を開いたまま #menu で引き出しを開いても、打った訳は待たずに保存される", async ({ page, server }) => {
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    await typeTranslation(page, L.goodbye, typed);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

    await menu(page).click();
    await expectDrawerOpen(page);
    await waitForDiskChange(server, before);
    await waitForSaved(page);
    await expectOnlyTranslation(server, before, L.goodbye, typed);
  });
});

test.describe("広い画面の左の列", () => {
  test.use({ viewport: WIDE });

  // 広い画面では #menu は列を畳んで一覧を広げる。引き出しと違って幕は出さない。
  // aria-expanded は列が出ているかどうかに合わせる。
  test("#menu で列を畳んだり出したりし、aria-expanded を合わせる", async ({ app }) => {
    await expect(sidebar(app)).toBeVisible();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "true");

    await menu(app).click();
    await expect(body(app)).toHaveClass(cls("sidebar-collapsed"));
    await expect(sidebar(app)).toBeHidden();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "false");
    await expect(backdrop(app)).toBeHidden();

    await menu(app).click();
    await expect(body(app)).not.toHaveClass(cls("sidebar-collapsed"));
    await expect(sidebar(app)).toBeVisible();
    await expect(menu(app)).toHaveAttribute("aria-expanded", "true");
  });
});

// 画面（app.js の matchMedia）と見た目（app.css の @media）が同じ幅で切り替わることを見る。
// 食い違うと、その幅では #menu が見た目に効かないクラスを付け外しするだけになり、
// 押しても何も起きない。
for (const c of [
  { width: 900, drawer: true },
  { width: 901, drawer: false },
]) {
  test(`幅 ${c.width}px では #menu が${c.drawer ? "引き出しを開く" : "列を畳む"}`, async ({ page, server }) => {
    await page.setViewportSize({ width: c.width, height: 700 });
    await openApp(page, server);
    await menu(page).click();
    if (c.drawer) {
      await expectDrawerOpen(page);
      await expect(page.locator("#sidebar-close")).toBeVisible();
      return;
    }
    await expect(body(page)).toHaveClass(cls("sidebar-collapsed"));
    await expect(body(page)).not.toHaveClass(cls("sidebar-open"));
    await expect(sidebar(page)).toBeHidden();
    await expect(backdrop(page)).toBeHidden();
  });
}

// ---- 貼り付く帯の高さ ----

test.describe("貼り付く帯の高さ", () => {
  test.use({ viewport: WIDE });

  // 焦点の入る要素の余白は --top-height から作る（app.css）。帯は失敗の理由・競合・
  // 行き先の無い訳で伸びるので、伸びたあとも縮んだあとも実際の高さに付いていかないと、
  // 焦点の入った行や検索の欄が帯の下へ潜る（doc.go「余白は帯の高さから作る」）。
  test("--top-height は帯の高さに付いていき、失敗の理由が出れば伸び、消えれば縮む", async ({ app }) => {
    const start = await expectTopHeightFollowsBand(app);
    expect(start).toBeGreaterThan(0);

    await failReload(app);
    const grown = await expectTopHeightFollowsBand(app);
    expect(grown).toBeGreaterThan(start);

    await app.unroute(linesApi);
    await app.locator("#reload").click();
    await expect(app.locator("#message")).toHaveText("");
    const back = await expectTopHeightFollowsBand(app);
    expect(Math.abs(back - start)).toBeLessThan(0.5);
  });

  // 幅が狭まると帯の中身が折り返して帯が高くなる（doc.go の実測で 320px は 121.4px）。
  // 窓の幅を変えたときにも付いていかないと、狭めた直後から行が帯の下へ潜る。
  test("窓の幅が変わって帯の中身が折り返すと、--top-height も付いていく", async ({ app }) => {
    const wide = await expectTopHeightFollowsBand(app);

    await app.setViewportSize({ width: 320, height: 600 });
    const narrow = await expectTopHeightFollowsBand(app);
    expect(narrow).toBeGreaterThan(wide);

    await app.setViewportSize(WIDE);
    const back = await expectTopHeightFollowsBand(app);
    expect(Math.abs(back - wide)).toBeLessThan(0.5);
  });

  // 測れない環境（ResizeObserver が無い）では変数を入れず、CSS の var() の既定値に任せる、
  // と app.js（watchTopHeight の注記）と doc.go の両方が書いている。以前は、目録が
  // 届く前に1度だけ測った帯の高さが入ったまま残った。目録が入ると帯は高くなるので、
  // 狭い画面では余白が帯より低くなり、焦点の入った行が帯の下へ潜りえた。
  test("ResizeObserver が無い環境では --top-height を入れず、CSS の既定値に任せる", async ({ page, server }) => {
    await page.setViewportSize(NARROW);
    await page.addInitScript(() => {
      delete window.ResizeObserver;
    });
    await openApp(page, server);
    expect(await page.evaluate(() => typeof window.ResizeObserver)).toBe("undefined");
    const inline = await page.evaluate(() => document.documentElement.style.getPropertyValue("--top-height"));
    expect(inline).toBe("");
  });
});

test.describe("長い一覧の途中から", () => {
  test.use({ repo: sampleRepo({ workingCopy: jaCopy({ extra: fillers(150) }) }) });

  // スラッシュは「行の途中から絞り込みへ戻る」ための近道である。帯が伸びているときに
  // 検索の欄が帯の下へ潜ると、焦点は入っているのに打っている欄が見えない
  // （doc.go の実測では、固定の余白のころ elementFromPoint が #conflict を返した）。
  // 広い画面（列）と狭い画面（引き出し）の両方で、一覧の最後から押して確かめる。
  for (const size of [WIDE, { width: 800, height: 600 }, { width: 320, height: 600 }]) {
    test(`${size.width}x${size.height} で帯が伸びていても、スラッシュで移った検索の欄は帯の下に潜らない`, async ({
      page,
      server,
    }) => {
      await page.setViewportSize(size);
      await openApp(page, server);
      await failReload(page);
      await expectTopHeightFollowsBand(page);
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
      await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(1000);

      await pressOutside(page, "/");
      await expect(search(page)).toBeFocused();
      await expect.poll(() => reachable(page, "#search")).toBe(true);
      if (size.width > 900) {
        // 広い画面の列は帯の下に貼り付く。検索の欄の上端は帯の下端より下にある。
        const top = await search(page).evaluate((el) => el.getBoundingClientRect().top);
        expect(top).toBeGreaterThanOrEqual((await topHeight(page)) - 0.5);
      }
    });
  }
});

// ---- beforeunload ----

// 抱えている訳が無いのに引き止めると、翻訳者は毎回「本当に閉じるか」を尋ねられ、
// 本当に訳が残っているときの引き止めを読まずに閉じるようになる。保存が済んだら
// 黙って閉じさせる。頁を操作して（打って）から閉じにいくので、引き止めが要るなら
// Chromium は確認を出せる状態にある。
test("保存が済んで抱えている訳が無ければ、頁を閉じるときに引き止めない", async ({ app, server }, testInfo) => {
  const before = await server.readRoot(workingRel);
  await typeTranslation(app, L.goodbye, typed);
  await editor(app).press("Escape");
  await waitForDiskChange(server, before);
  await waitForSaved(app);

  // 受け手そのものが閉じるのを止めない（preventDefault しない）ことを、頁を閉じずに
  // 事象を投げて見る。閉じた頁からはカバレッジを取れないので、受け手の「抱えていない」
  // 側はここでしか記録に残らない。
  const prevented = await app.evaluate(() => {
    const ev = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(ev);
    return ev.defaultPrevented;
  });
  expect(prevented).toBe(false);

  // 閉じた頁からはカバレッジを取れないので、先に書き出しておく。
  await saveCoverage(app, testInfo, "before-close");
  expect(await tryClose(app)).toEqual({ type: "closed" });
  expect(app.isClosed()).toBe(true);
});

// 打ってから自動保存の時計が切れるまでの 1.5 秒は、訳がブラウザーの中にしか無い。
// そこで閉じられると訳は消える（app.js 冒頭「未保存のまま頁を閉じようとしたら
// beforeunload で止める」）。引き止めを断った頁は、そのまま訳をファイルへ入れる。
test("まだ送っていない訳があると、頁を閉じる前に引き止め、残った頁はその訳をファイルへ入れる", async ({
  page,
  server,
}) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  await typeTranslation(page, L.goodbye, typed);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  expect(await tryClose(page)).toEqual({ type: "beforeunload" });
  expect(page.isClosed()).toBe(false);
  await expect(translationCell(page, L.goodbye)).toHaveText(typed);

  await page.clock.runFor(2_000);
  await waitForDiskChange(server, before);
  await waitForSaved(page);
  await expectOnlyTranslation(server, before, L.goodbye, typed);
});

// 保存の要求が届かない（503、本文は JSON でない）あいだ、訳は未保存のまま抱えられ、
// 送り直しを待っている。「保存しようとした」ことで引き止めを外すと、送り直しが
// 成功する前に閉じられて訳が消える。
test("保存が届かず送り直しを待っているあいだも、頁を閉じる前に引き止める", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  await page.route(rowsApi, (route) =>
    route.fulfill({ status: 503, contentType: "text/plain; charset=utf-8", body: "busy" }),
  );
  const saving = nextSave(page);
  await typeTranslation(page, L.goodbye, typed);
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(503);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
  await expect(page.locator("#message")).toHaveText(msg("ja", "ui.save_failed_detail"));

  expect(await tryClose(page)).toEqual({ type: "beforeunload" });
  expect(page.isClosed()).toBe(false);

  // 待ち受けが戻れば、送り直しがその訳をファイルへ入れる（最初の送り直しは 500ms 後）。
  await page.unroute(rowsApi);
  await page.clock.runFor(1_000);
  await waitForDiskChange(server, before);
  await waitForSaved(page);
  await expectOnlyTranslation(server, before, L.goodbye, typed);
});

// 行ごとに断られた訳（state.failed）は自動保存の対象から外れるので、放っておいても
// 送られない。画面にしか無い訳なので、閉じる前に引き止める。
test("行ごとに断られて保存できなかった訳があると、頁を閉じる前に引き止める", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  await app.route(rowsApi, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    payload.edits = (payload.edits ?? []).map((edit) => ({ ...edit, key: wrongKey }));
    await route.continue({ postData: JSON.stringify(payload) });
  });
  await typeTranslation(app, L.goodbye, typed);
  await editor(app).press("Escape");
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
  await expect(rowByLine(app, L.goodbye)).toHaveClass(cls("save-failed"));

  expect(await tryClose(app)).toEqual({ type: "beforeunload" });
  expect(app.isClosed()).toBe(false);
  await expect(translationCell(app, L.goodbye)).toHaveText(typed);
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 競合のあいだ、自分の訳は state.mine に抱えられ、どちらを載せるか選ぶまでファイルに
// 入らない（未保存の数には入らない）。ここで閉じさせると、選ばせる約束ごと訳が消える。
test("競合で選ばせているあいだは、頁を閉じる前に引き止め、両方の訳を出し続ける", async ({ page, server }) => {
  await openPaused(page, server);
  expect(await server.readRootText(workingRel)).toBe(jaCopy());
  await typeTranslation(page, L.goodbye, typed);
  const external = jaCopy({ goodbye: "またね。" });
  await server.writeRoot(workingRel, external);
  const saving = nextSave(page);
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(409);
  await expect(page.locator("#conflict")).toBeVisible();
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_conflict"));

  expect(await tryClose(page)).toEqual({ type: "beforeunload" });
  expect(page.isClosed()).toBe(false);
  await expect(page.locator("#conflict")).toBeVisible();
  await expect(rowByLine(page, L.goodbye).locator(".row-note .note-value")).toHaveText(["またね。", typed]);
  expect(await server.readRootText(workingRel)).toBe(external);
});

// ---- 外へ出さない守り ----

// トークンは起動のたびに作る鍵で、これがあればこの待ち受けへ書き込める。HttpOnly なら
// 頁の JavaScript から読めず、SameSite=Strict なら他の生成元からの要求に載らない。
// 名前にポートを入れるのは、もう1つ dwloc edit を動かしたときに上書きされないため
// （internal/web の token.go の sessionCookie と cookieNameFor）。
test("Cookie は頁の JavaScript から読めず、SameSite=Strict のセッション Cookie で、名前にポートを持つ", async ({
  app,
  server,
}) => {
  expect(await app.evaluate(() => document.cookie)).toBe("");
  const cookies = await app.context().cookies(server.origin);
  expect(cookies).toHaveLength(1);
  expect(cookies[0]).toMatchObject({
    name: `dwloc_session_${server.port}`,
    path: "/",
    httpOnly: true,
    secure: false,
    sameSite: "Strict",
    expires: -1,
  });
});

// トークン付きの URL は最初の1回だけで、303 で素の / へ送り返す。履歴に残ると、
// 履歴を読める人がこの待ち受けに書き込める。アドレス欄だけでなく、戻る・進むの
// 履歴の項目そのもの（CDP の Page.getNavigationHistory）で見る。開き直しても増えない。
test("トークンはアドレス欄にも、戻る・進むの履歴にも残らない", async ({ app, server }) => {
  const cdp = await app.context().newCDPSession(app);
  const historyUrls = async () => (await cdp.send("Page.getNavigationHistory")).entries.map((e) => e.url);

  let urls = await historyUrls();
  expect(urls).toContain(`${server.origin}/`);
  for (const url of urls) {
    expect(url).not.toContain(server.token);
    expect(url).not.toContain("?t=");
  }

  await app.reload();
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
  urls = await historyUrls();
  for (const url of urls) {
    expect(url).not.toContain(server.token);
  }
  expect(await app.evaluate(() => location.href)).toBe(`${server.origin}/`);
  await cdp.detach();
});

// walkThrough は画面のひととおりの操作をする。外へ出さない守りの試験は、この操作の
// あいだに出た要求・応答・違反を見る。
async function walkThrough(page, server) {
  const shown = page.locator("#shown");
  const rows = page.locator("#rows");
  await openApp(page, server);

  // 検索と絞り込み。
  await search(page).fill("Hello");
  await expect(shown).toHaveText(msg("ja", "ui.shown", { count: 1 }));
  await search(page).fill("");
  await expect(shown).toHaveText(msg("ja", "ui.shown", { count: 3 }));
  const chip = page.locator("#filters input[type=checkbox]").first();
  await chip.check();
  await chip.uncheck();

  // 書き換えて保存する。
  const before = await server.readRoot(workingRel);
  await typeTranslation(page, L.goodbye, typed);
  await editor(page).press("Enter");
  await waitForDiskChange(server, before);
  await waitForSaved(page);

  // ロケールを切り替えて戻し、読み直す。
  await page.locator("#locale").selectOption("he");
  await expect(rows).toHaveText(msg("ja", "ui.rows", { count: 1 }));
  await page.locator("#locale").selectOption("ja");
  await expect(rows).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  await page.locator("#reload").click();
  await expect(translationCell(page, L.goodbye)).toHaveText(typed);
  await expect(dataRows(page)).toHaveCount(3);

  // 狭い画面で引き出しを開けて閉じ、広い画面へ戻す。
  await page.setViewportSize(NARROW);
  await pressOutside(page, "/");
  await expect(search(page)).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu(page)).toBeFocused();
  await page.setViewportSize(WIDE);
  await expect(sidebar(page)).toBeVisible();
}

test.describe("外へ出さない守り", () => {
  test.use({ viewport: WIDE });

  // 取りにいく先は自分自身だけ（app.js 冒頭）。原文を再配布しないことが設計の中心なので、
  // 画面から外への通信が1本でもあれば、それが台本の漏れる道になる。Referer も付けない
  // （index.html の meta と Referrer-Policy）。保存の要求は Content-Type: application/json
  // と Origin を必ず持つ。待ち受けはこの2つが無い書き込みを通さない（postJSON の注記）。
  test("画面が出す要求は同じ生成元だけで、Referer を付けず、保存は JSON で Origin を添える", async ({
    page,
    server,
  }) => {
    const requests = [];
    const sockets = [];
    page.on("request", (req) => requests.push(req));
    page.on("websocket", (ws) => sockets.push(ws.url()));
    await walkThrough(page, server);

    expect(requests.length).toBeGreaterThan(0);
    for (const req of requests) {
      expect(new URL(req.url()).origin, req.url()).toBe(server.origin);
    }
    expect(sockets).toEqual([]);

    const headers = await Promise.all(requests.map(async (req) => [req, await req.allHeaders()]));
    for (const [req, h] of headers) {
      expect(h.referer, `${req.method()} ${new URL(req.url()).pathname}`).toBeUndefined();
    }
    const posts = headers.filter(([req]) => req.method() === "POST");
    expect(posts.length).toBeGreaterThan(0);
    for (const [req, h] of posts) {
      expect(new URL(req.url()).pathname).toBe("/api/rows");
      expect(h["content-type"]).toMatch(/^application\/json(;|$)/);
      expect(h.origin).toBe(server.origin);
    }
  });

  // 守りのヘッダーは「どの応答にも付ける」（middleware.go の securityHeaders）。付け忘れる
  // 経路が1つでもあると、その応答だけディスクキャッシュに原文が残ったり、外の頁から
  // 読めたりする。303 の送り返しと、資産と、API の読み書きのすべてで見る。資産は
  // nosniff なので、正しい Content-Type でなければブラウザーが読み込まない。
  test("どの応答にも同じ CSP と no-store と nosniff が付き、CORS のヘッダーは付かない", async ({ page, server }) => {
    const responses = [];
    page.on("response", (res) => responses.push(res));
    await walkThrough(page, server);

    const seen = await Promise.all(
      responses.map(async (res) => ({
        path: new URL(res.url()).pathname,
        status: res.status(),
        method: res.request().method(),
        headers: await res.allHeaders(),
      })),
    );
    const paths = new Set(seen.map((r) => `${r.method} ${r.path} ${r.status}`));
    for (const want of ["GET / 303", "GET / 200", "GET /app.js 200", "GET /app.css 200", "GET /api/bootstrap 200", "GET /api/lines 200", "POST /api/rows 200"]) {
      expect(paths, want).toContain(want);
    }

    const policies = new Set(seen.map((r) => r.headers["content-security-policy"]));
    expect(policies.size, "応答によって CSP が違う").toBe(1);
    const policy = [...policies][0];
    expect(policy).toBeTruthy();
    const directives = Object.fromEntries(
      policy
        .split(";")
        .map((d) => d.trim())
        .filter(Boolean)
        .map((d) => {
          const [name, ...values] = d.split(/\s+/);
          return [name, values.join(" ")];
        }),
    );
    expect(directives).toMatchObject({
      "default-src": "'none'",
      "script-src": "'self'",
      "style-src": "'self'",
      "connect-src": "'self'",
      "form-action": "'none'",
      "base-uri": "'none'",
      "frame-ancestors": "'none'",
    });

    for (const r of seen) {
      const label = `${r.method} ${r.path} ${r.status}`;
      expect(r.headers["cache-control"], label).toBe("no-store");
      expect(r.headers["x-content-type-options"], label).toBe("nosniff");
      expect(r.headers["referrer-policy"], label).toBe("no-referrer");
      expect(r.headers["access-control-allow-origin"], label).toBeUndefined();
      if (r.path === "/app.js") {
        expect(r.headers["content-type"], label).toMatch(/^text\/javascript/);
      }
      if (r.path === "/app.css") {
        expect(r.headers["content-type"], label).toMatch(/^text\/css/);
      }
    }
  });

  // CSP は外への通信を塞ぐ守りだが、画面自身が CSP に引っかかる書き方（行内の style や
  // script、外の資産）をしていると、その部分は黙って効かなくなる。ひととおり操作して、
  // 違反が1件も出ないことを見る。
  test("ひととおり操作しても、画面は CSP に1度も引っかからない", async ({ page, server }) => {
    await page.addInitScript(() => {
      window.__cspViolations = [];
      document.addEventListener("securitypolicyviolation", (e) => {
        window.__cspViolations.push(`${e.violatedDirective} ${e.blockedURI}`);
      });
    });
    const consoleErrors = [];
    page.on("console", (m) => {
      if (/Content Security Policy/i.test(m.text())) {
        consoleErrors.push(m.text());
      }
    });
    await walkThrough(page, server);

    expect(await page.evaluate(() => window.__cspViolations)).toEqual([]);
    expect(consoleErrors).toEqual([]);
  });
});
