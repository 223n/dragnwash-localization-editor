// 1行を書き換えると自動保存でファイルに入り、触っていない行は1バイトも変わらないことを見る。
//
// 保存は「触った行の最終フィールドだけを差し替える」であって再生成ではない
// （internal/web の handleRows、internal/edit の SetTranslation）。再生成すると並びと
// 見出しが作り直され、BOM や行ごとの改行の種類も揃えられてしまう。作業コピーは
// ゲーム内のModが書いたファイルで、ホットリロードでゲームが読み直すので、
// 触っていない行が1バイトでも動くと、翻訳者が見ていない差分がコミットに混ざる。
//
// そのため見本はわざと BOM 付きで、行ごとに LF と CRLF を混ぜてある。書き換える
// 行は CRLF で終わる行にして、その行の改行も残ることを見る。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, SAMPLE_LINES, sampleRepo, sampleWorkingCopy } from "../support/repo.mjs";
import { editor, rowByLine, saveState, typeTranslation, waitForSaved } from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// 添字 0 がヘッダー。奇数の添字（偶数の物理行）を CRLF にするので、6行目の goodbye は CRLF。
const mixedEol = (i) => (i % 2 === 1 ? "\r\n" : "\n");

test.use({
  repo: sampleRepo({ workingCopy: sampleWorkingCopy({ bom: true, eol: mixedEol }) }),
});

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

test("入力が止まると自動で保存し、書き換えた行の訳だけが変わる", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  const typed = "さようなら。";
  const row = rowByLine(app, SAMPLE_LINES.goodbye);

  // 訳が空の行には「未翻訳」が付いている。保存したあとで外れることを見るために、先に確かめる。
  await expect(row.locator(".badge", { hasText: msg("ja", "category.untranslated") })).toHaveCount(1);

  await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
  // 欄から離れていない。保存は自動保存の時計（autosaveDelayMs）に任せる。
  // 送るまでは「未保存 1 件」と出る。黙って保存済みに見せない。
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  await expect(editor(app)).toBeFocused();

  // ファイルに入るまで待つ。画面の表示ではなくディスクのバイトで見る。
  await expect
    .poll(async () => (await server.readRoot(workingRel)).equals(before), { timeout: 10_000 })
    .toBe(false);
  await waitForSaved(app);

  const after = await server.readRoot(workingRel);
  const beforeLines = splitLines(before);
  const afterLines = splitLines(after);
  expect(afterLines).toHaveLength(beforeLines.length);
  const index = SAMPLE_LINES.goodbye - 1;
  for (let i = 0; i < beforeLines.length; i++) {
    if (i === index) {
      continue;
    }
    // BOM も（1行目に含まれる）、ほかの行の LF / CRLF も、そのまま残る。
    expect(afterLines[i].equals(beforeLines[i]), `${i + 1}行目が変わった`).toBe(true);
  }
  // 書き換えた行は、最終フィールドだけが入れ替わり、CRLF は残る。
  const edited = beforeLines[index].toString("utf8").replace(/,\r\n$/, `,${typed}\r\n`);
  expect(afterLines[index].toString("utf8")).toBe(edited);

  // 保存の応答で、訳が入った行から「未翻訳」が外れる（待ち受けの局所更新をそのまま描く）。
  await expect(row.locator(".badge", { hasText: msg("ja", "category.untranslated") })).toHaveCount(0);
});

test("Enter で確定すると次の行が開き、待たずに保存する", async ({ app, server }) => {
  const typed = "さようなら。";
  await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
  await editor(app).press("Enter");

  // 上から順に打っていけるように、次の編集できる行（wonderful）の入力欄が開く。
  await expect(editor(app)).toBeFocused();
  await expect(editor(app)).toHaveValue(SAMPLE.wonderful.ja);
  await expect(rowByLine(app, SAMPLE_LINES.wonderful).locator("textarea.editor")).toHaveCount(1);

  // 閉じた行は自動保存の時計を待たずに送る。1.5 秒より十分短い間に入ることを見る。
  await expect
    .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\r\n`), { timeout: 1_000 })
    .toBe(true);
  await waitForSaved(app);
});
