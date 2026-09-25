// 訳への改行の入力を見る（複数行の移行の PR4。決まったことの 1）。
//
//   Shift+Enter        どの行でも、入力欄に改行（LF）を入れる。Enter はいままでどおり
//                      次の行へ進む。変換中は横取りせず、変換を確定した直後（composedGrace）の
//                      Shift+Enter でも改行を入れない。
//   保存               改行の入った訳は、引用符で囲んだ複数行の値として書く。値の中の改行は
//                      LF で、レコードの終端（この見本では CRLF）は元のまま。変わるのは
//                      そのレコードの最終フィールドだけ。
//   行番号             改行のぶんだけ物理行が増え、後ろの行の行番号がずれる。行は ID で
//                      引くので、下の行（キーのある行も無い行も）を続けて保存しても正しい
//                      レコードに入る。画面は保存の応答（numbers）で行番号の欄と読み上げの
//                      名前を直す。
//   競合と行き先の無い訳  複数行の値が入った本物の流れ（PR3 の申し送り。PR3 では応答を
//                      差し替えて見ていた）。
//
// 保存がほかのバイトを変えないことは、ファイル全体のバイトで比べる（support/records.mjs）。
// 見本の英文と訳はどれも架空の文である。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, keyFor, record, sampleRepo, workingCopy } from "../support/repo.mjs";
import { expectSameBytes, replaceRecord } from "../support/records.mjs";
import { editor, openApp, openEditor, rowById, saveState, translationCell, waitForSaved } from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// 見本（区切りは CRLF、値の中は LF）。行番号と ID:
//   1 ヘッダー（ID 1）
//   2 hello（ID 2）
//   3 キーの欄が空の行（ID 3。原文があるので編集できる）
//   4〜5 訳が2行のレコード（ID 4）
//   6 wonderful（ID 5）
const hello = { ...SAMPLE.hello, translation: SAMPLE.hello.ja };
const keyless = { key: "", source: "A line with no key.", section: "UI", node: "", order: "", speaker: "UI", translation: "" };
const twoLines = {
  key: keyFor("One."),
  source: "One.",
  section: "UI",
  node: "",
  order: "",
  speaker: "UI",
  translation: "いち\nに",
};
const wonderful = { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja };
const ID = { hello: 2, keyless: 3, twoLines: 4, wonderful: 5 };

test.use({
  repo: sampleRepo({ workingCopy: workingCopy([hello, keyless, twoLines, wonderful], { eol: "\r\n" }) }),
});

// recordOf は作業コピーのレコードの字（終端を除く）。repo.mjs の workingCopy と同じ組み方。
function recordOf(item, translation) {
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

// drawnText は描いた字を返す（innerText）。white-space で詰めた改行や空行は、ここで消える。
function drawnText(locator) {
  return locator.evaluate((e) => e.innerText);
}

// numbers は、データ行の ID ごとの行番号の欄（最初の物理行と「〜最後の物理行」）。
function numbers(page) {
  return page.locator("#list .row").evaluateAll((rows) =>
    rows.map((r) => [
      Number(r.dataset.id),
      r.querySelector(".num-start").textContent,
      r.querySelector(".num-end") ? r.querySelector(".num-end").textContent : "",
    ]),
  );
}

// lineEnd は行番号の欄の「〜M」の字（目録の ui.line_end）。
function lineEnd(n) {
  return msg("ja", "ui.line_end", { line: n });
}

// fileLines は「数えたもの」のファイルの物理行の欄の字。
function fileLines(page) {
  return page.locator("#stats li").filter({ hasText: `${msg("ja", "stats.file_lines")}: ` });
}

// openPaused は偽の時計を入れてから画面を開き、開き終えたところで時計を止める
// （edit.spec.mjs と同じ）。
async function openPaused(page, server) {
  await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
  await openApp(page, server);
  await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
}

// compose は入力欄へ変換（IME）の事象を投げる（edit.spec.mjs と同じ）。
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
      Object.defineProperty(ev, "keyCode", { get: () => options.keyCode });
    }
    ed.dispatchEvent(ev);
    return ev.defaultPrevented;
  }, init);
}

test("Shift+Enter で入れた改行は、引用した複数行の値として保存され、下の行は行番号がずれても正しいレコードに入る", async ({
  app,
  server,
}) => {
  const before = await server.readRoot(workingRel);
  await expect(fileLines(app)).toHaveText(`${msg("ja", "stats.file_lines")}: 6`);
  expect(await numbers(app)).toEqual([
    [ID.hello, "2", ""],
    [ID.keyless, "3", ""],
    [ID.twoLines, "4", lineEnd(5)],
    [ID.wonderful, "6", ""],
  ]);

  // hello に2行の訳を打ち、Enter で次の行（キーの無い行）へ進む。
  await openEditor(app, ID.hello);
  await editor(app).fill("もしもし。");
  await editor(app).press("Shift+Enter");
  await editor(app).pressSequentially("聞こえますか。");
  await expect(editor(app)).toHaveValue("もしもし。\n聞こえますか。");
  await editor(app).press("Enter");
  await expect(rowById(app, ID.keyless).locator("textarea.editor")).toBeFocused();
  await waitForSaved(app);

  // hello は2物理行になり、後ろの行の行番号が1つずつ下がる。開いている行の読み上げの
  // 名前も、いまの行番号で言う。
  expect(await numbers(app)).toEqual([
    [ID.hello, "2", lineEnd(3)],
    [ID.keyless, "4", ""],
    [ID.twoLines, "5", lineEnd(6)],
    [ID.wonderful, "7", ""],
  ]);
  await expect(editor(app)).toHaveAccessibleName(msg("ja", "ui.edit_label_line", { line: 4 }));
  await expect(fileLines(app)).toHaveText(`${msg("ja", "stats.file_lines")}: 7`);
  expect(await drawnText(translationCell(app, ID.hello))).toBe("もしもし。\n聞こえますか。");

  // キーの無い行、訳が2行の行（行末に1行足す）、wonderful を続けて保存する。
  await editor(app).fill("キーの無い行の訳。");
  await editor(app).press("Enter");
  await expect(rowById(app, ID.twoLines).locator("textarea.editor")).toBeFocused();
  await expect(editor(app)).toHaveValue("いち\nに");
  await editor(app).press("Control+End");
  await editor(app).press("Shift+Enter");
  await editor(app).pressSequentially("さん");
  await editor(app).press("Enter");
  await expect(rowById(app, ID.wonderful).locator("textarea.editor")).toBeFocused();
  await editor(app).fill("すごい！");
  await editor(app).press("Escape");
  await waitForSaved(app);

  let expected = replaceRecord(before, recordOf(hello, hello.translation), recordOf(hello, "もしもし。\n聞こえますか。"));
  expected = replaceRecord(expected, recordOf(keyless, ""), recordOf(keyless, "キーの無い行の訳。"));
  expected = replaceRecord(expected, recordOf(twoLines, twoLines.translation), recordOf(twoLines, "いち\nに\nさん"));
  expected = replaceRecord(expected, recordOf(wonderful, wonderful.translation), recordOf(wonderful, "すごい！"));
  expectSameBytes(await server.readRoot(workingRel), expected, "作業コピー");
  // 値の中の改行は LF、レコードの終端は CRLF のまま。
  expect((await server.readRoot(workingRel)).toString("utf8")).toContain(`,"もしもし。\n聞こえますか。"\r\n`);

  expect(await numbers(app)).toEqual([
    [ID.hello, "2", lineEnd(3)],
    [ID.keyless, "4", ""],
    [ID.twoLines, "5", lineEnd(7)],
    [ID.wonderful, "8", ""],
  ]);
  await expect(fileLines(app)).toHaveText(`${msg("ja", "stats.file_lines")}: 8`);

  // 読み直しても、同じ ID が同じ行番号の範囲で並び、訳は改行ごと入っている。
  await app.reload();
  await expect(app.locator("#rows")).not.toBeEmpty();
  expect(await numbers(app)).toEqual([
    [ID.hello, "2", lineEnd(3)],
    [ID.keyless, "4", ""],
    [ID.twoLines, "5", lineEnd(7)],
    [ID.wonderful, "8", ""],
  ]);
  expect(await drawnText(translationCell(app, ID.twoLines))).toBe("いち\nに\nさん");
});

// 下の行を、上の行の改行を書いた保存の応答が返る前に打ち、行番号がずれたあとのファイルへ
// 送る。画面が持っている行番号は古いが、行は ID で指すので、正しいレコードに入る。
test("改行を足した保存の応答を待つあいだに打った下の行も、正しいレコードに入る", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let held = false;
  const saves = [];
  await page.route("**/api/rows", async (route) => {
    saves.push(route.request().postDataJSON());
    if (!held) {
      held = true;
      await gate;
    }
    await route.continue();
  });

  await openEditor(page, ID.hello);
  await editor(page).fill("もしもし。");
  await editor(page).press("Shift+Enter");
  await editor(page).pressSequentially("聞こえますか。");
  await editor(page).press("Enter");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_saving"));
  // 送っているあいだに、キーの無い行と wonderful を打つ。
  await editor(page).fill("キーの無い行の訳。");
  await translationCell(page, ID.wonderful).click();
  await editor(page).fill("すごい！");
  await editor(page).press("Escape");
  release();
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 2 }));
  await page.clock.runFor(1_500);
  await waitForSaved(page);

  expect(saves.map((body) => body.edits.map((e) => e.id))).toEqual([[ID.hello], [ID.keyless, ID.wonderful]]);
  let expected = replaceRecord(before, recordOf(hello, hello.translation), recordOf(hello, "もしもし。\n聞こえますか。"));
  expected = replaceRecord(expected, recordOf(keyless, ""), recordOf(keyless, "キーの無い行の訳。"));
  expected = replaceRecord(expected, recordOf(wonderful, wonderful.translation), recordOf(wonderful, "すごい！"));
  expectSameBytes(await server.readRoot(workingRel), expected, "作業コピー");
  expect(await numbers(page)).toEqual([
    [ID.hello, "2", lineEnd(3)],
    [ID.keyless, "4", ""],
    [ID.twoLines, "5", lineEnd(6)],
    [ID.wonderful, "7", ""],
  ]);
});

// 行番号の欄を直すのは、保存の応答の numbers にある行だけで、描いていない行の ID が
// 入っていても飛ばす（待ち受けは描いた行の ID しか返さないが、応答を差し替えて見る）。
test("保存の応答の行番号に描いていない行が入っていても、描いた行だけを直す", async ({ app }) => {
  await app.route("**/api/rows", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.numbers = [{ id: 999, n: 1 }, ...(body.numbers ?? [])];
    await route.fulfill({ response, json: body });
  });
  await openEditor(app, ID.hello);
  await editor(app).fill("もしもし。\n聞こえますか。");
  await editor(app).press("Escape");
  await waitForSaved(app);
  expect(await numbers(app)).toEqual([
    [ID.hello, "2", lineEnd(3)],
    [ID.keyless, "4", ""],
    [ID.twoLines, "5", lineEnd(6)],
    [ID.wonderful, "7", ""],
  ]);
});

// 改行はキャレットの位置に入り、選んでいる字は改行に置き換わる。
test("Shift+Enter はキャレットの位置に改行を入れ、選んでいる字は改行に置き換える", async ({ app }) => {
  await openEditor(app, ID.hello);
  const ed = editor(app);
  await ed.fill("前後");
  await ed.evaluate((node) => node.setSelectionRange(1, 1));
  await ed.press("Shift+Enter");
  await expect(ed).toHaveValue("前\n後");
  expect(await ed.evaluate((node) => [node.selectionStart, node.selectionEnd])).toEqual([2, 2]);

  await ed.fill("前ABC後");
  await ed.evaluate((node) => node.setSelectionRange(1, 4));
  await ed.press("Shift+Enter");
  await expect(ed).toHaveValue("前\n後");
  // 入力欄は改行のぶん伸び、送りの棒は出ない。
  const size = await ed.evaluate((node) => ({ scroll: node.scrollHeight, client: node.clientHeight }));
  expect(size.scroll).toBeLessThanOrEqual(size.client);
});

// 変換中のキーは1つも横取りしない。Shift+Enter で確定する IME もあるので、変換中の
// Shift+Enter に改行を入れると、確定のつもりで改行が入る。確定の直後（composedGrace）の
// Shift+Enter も改行には使わない（compositionend が確定の keydown より先に届く並び）。
// 猶予が過ぎた Shift+Enter は改行を入れる。
test("変換中と確定の直後の Shift+Enter は改行を入れず、少し置いた Shift+Enter で入れる", async ({ page, server }) => {
  await openPaused(page, server);
  await openEditor(page, ID.hello);
  await editor(page).fill("");

  await compose(page, "compositionstart");
  await compose(page, "input", "あさ");
  expect(await keydown(page, { key: "Enter", code: "Enter", shiftKey: true })).toBe(false);
  expect(await keydown(page, { key: "Enter", code: "Enter", shiftKey: true, isComposing: true })).toBe(false);
  expect(await keydown(page, { key: "Enter", code: "Enter", shiftKey: true, keyCode: 229 })).toBe(false);
  await compose(page, "compositionend", "朝");
  await expect(editor(page)).toHaveValue("朝");

  // 時計は止まっているので、確定からの経過は 0ms のまま。既定の動作（改行）も止める。
  expect(await keydown(page, { key: "Enter", code: "Enter", shiftKey: true })).toBe(true);
  await editor(page).press("Shift+Enter");
  await expect(editor(page)).toHaveValue("朝");
  await expect(rowById(page, ID.hello).locator("textarea.editor")).toBeFocused();

  await page.clock.runFor(100);
  await editor(page).press("Shift+Enter");
  await expect(editor(page)).toHaveValue("朝\n");
  await expect(rowById(page, ID.hello).locator("textarea.editor")).toBeFocused();
});

// 書こうとした訳の行が、その行だけで読むとレコードに見えると、publish は引用符の閉じ誤りの
// 疑いとして止める。待ち受けは書かずに、訳の何行目かを添えて断り、画面はその行を保存
// できない行にする（訳は抱えたまま）。
test("訳の2行目がレコードに見える形なら書かず、訳の何行目かを添えて保存できない行にする", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  const typed = "もしもし。\n0123456789abcdef,UI,,,UI,x,y";
  await openEditor(app, ID.hello);
  await editor(app).fill(typed);
  await editor(app).press("Escape");

  const why = msg("ja", "error.invalid_value", {
    line: 2,
    reason: msg("ja", "reason.edit_line_looks_like_record", { line: 2 }),
  });
  const row = rowById(app, ID.hello);
  await expect(row.locator(".row-note")).toHaveText(msg("ja", "ui.row_error", { reason: why }));
  await expect(row).toHaveClass(/(^|\s)save-failed(\s|$)/);
  expect(await drawnText(translationCell(app, ID.hello))).toBe(typed);
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 競合で、ファイルの訳もあなたの訳も複数行の値になる本物の流れ（PR3 の申し送り）。よそが
// wonderful に複数行の訳を書き、こちらは同じ行に複数行の訳を打っていた。どちらも改行と
// 空行を保ったまま段に分けて並べ、「自分の訳を上に載せる」を選ぶと、こちらの訳を複数行の
// 値として書く。
test("競合でファイルの訳もあなたの訳も複数行のとき、改行を保って並べ、載せ直すと複数行で書く", async ({
  page,
  server,
}) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const theirs = "よその訳。\n\nよその2段落目。";
  const mine = "すごい！\n本当に。";
  const external = replaceRecord(before, recordOf(wonderful, wonderful.translation), recordOf(wonderful, theirs));

  await openEditor(page, ID.wonderful);
  await editor(page).fill("すごい！");
  await editor(page).press("Shift+Enter");
  await editor(page).pressSequentially("本当に。");
  await server.writeRoot(workingRel, external);
  const saving = page.waitForResponse((res) => new URL(res.url()).pathname === "/api/rows");
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(409);

  const row = rowById(page, ID.wonderful);
  await expect(row).toHaveClass(/(^|\s)conflicted(\s|$)/);
  // Escape で留まっていた行は競合して開けないので、焦点は引き止めの枠へ移す（body へ落とさない）。
  await expect(page.locator("#conflict")).toBeFocused();
  const values = row.locator(".row-note .note-pair .note-value");
  await expect(values).toHaveCount(2);
  expect(await drawnText(values.nth(0))).toBe(theirs);
  expect(await drawnText(values.nth(1))).toBe(mine);
  // よその訳で wonderful は3物理行になった（読み直した行一覧の行番号）。
  await expect(row.locator(".num-start")).toHaveText("6");
  await expect(row.locator(".num-end")).toHaveText(lineEnd(8));

  await page.locator("#conflict-keep").click();
  await waitForSaved(page);
  expectSameBytes(
    await server.readRoot(workingRel),
    replaceRecord(external, recordOf(wonderful, theirs), recordOf(wonderful, mine)),
    "作業コピー",
  );
  await expect(row.locator(".num-end")).toHaveText(lineEnd(7));
  expect(await drawnText(translationCell(page, ID.wonderful))).toBe(mine);
});

// 載せる先の無い訳に複数行の値が入る本物の流れ（PR3 の申し送り）。こちらが複数行の訳を
// 打っているあいだに、よそが hello の key 列を書き換えた。打った訳は捨てずに、目印と
// 訳の段を分けて、改行を保ったまま並べる。ファイルには1バイトも書かない。
test("載せる先の無い訳が複数行でも、改行を保って行き先の無い訳に並べる", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "もしもし。\n\n聞こえますか。";
  const moved = { ...hello, key: "0000000000000000" };
  const external = replaceRecord(before, recordOf(hello, hello.translation), recordOf(moved, hello.translation));

  await openEditor(page, ID.hello);
  await editor(page).fill("もしもし。");
  await editor(page).press("Shift+Enter");
  await editor(page).press("Shift+Enter");
  await editor(page).pressSequentially("聞こえますか。");
  await server.writeRoot(workingRel, external);
  const saving = page.waitForResponse((res) => new URL(res.url()).pathname === "/api/rows");
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(409);

  const item = page.locator("#orphans-list li");
  await expect(item).toHaveCount(1);
  await expect(item.locator(".note-label")).toHaveText(`${keyFor(SAMPLE.hello.source)}:`);
  expect(await drawnText(item.locator(".note-value"))).toBe(typed);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));
  expect((await server.readRoot(workingRel)).equals(Buffer.from(external, "utf8"))).toBe(true);
});
