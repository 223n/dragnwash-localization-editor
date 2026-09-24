// 全体を解釈して読むと取り違える形の行を、画面が理由を付けて読み取り専用にすることを見る。
//
// publish と diff は、上流 main と同じくファイル全体を1つの文字列として解釈して読む
// （引用符で囲んだ値は物理行をまたいでも1つの値になる）。画面の編集モデル
// （internal/edit）も、行の種類を同じ区切りの関数で決める。保存の単位は物理行のまま
// なので、次の2つは書かせない（internal/web の doc.go「読み違える形と読めないファイル」）。
//
//   行をまたぐレコード   どの物理行も編集させない。1行目にキーと原文の全体を出し、
//                        バッジもそこへ付ける。続きの行は生の行のまま出し、値の中の
//                        '#' の行を見出しにしない。
//   閉じない引用符       ファイル全体を読み取り専用にし、引用符が開いた行から後ろを
//                        生の行のまま並べる。起動は止めず、どのファイルの何行目かを
//                        断り書きに出す。
//
// 見本の英文と訳はどれも架空の文である。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, keyFor, record, sampleRepo, workingCopy } from "../support/repo.mjs";
import { editor, headings, rowByLine, saveState, translationCell } from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// MULTI は、空行を挟んで行をまたぐ原文。ゲーム側の作業コピーの実物にある形（値の中に
// LF が2つ）をまねてある。再生順に無い UI の行として置く。
const MULTI = "para1\n\npara2";

// textOf は要素の字をそのまま返す（innerText と違い、改行を詰めない）。
function textOf(locator) {
  return locator.evaluate((e) => e.textContent);
}

test.describe("行をまたぐレコード", () => {
  // 行番号:
  //   1 ヘッダー / 2 空行 / 3・4 見出し / 5〜6 hello（訳が行をまたぎ、2行目が '#' で始まる）
  //   7〜9 MULTI（原文が行をまたぎ、訳が空。8行目は空行） / 10 wonderful / 11 空行
  const helloFirst = record(
    keyFor(SAMPLE.hello.source), "L01 Ryan", "Ryan_1_intro", "1", "Ryan", SAMPLE.hello.source, "",
  ) + '"もしもし';
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy([
        "",
        "# ===== Level 1: Ryan (Sunny) =====",
        "# --- intro: Ryan_1_intro ---",
        helloFirst,
        '# 二行目"',
        { key: keyFor(MULTI), source: MULTI, section: "UI", node: "", order: "", speaker: "UI", translation: "" },
        { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
        "",
      ]),
    }),
  });

  test("どの物理行も編集させず、1行目にキーと原文の全体とバッジを出し、続きの行は生のまま出す", async ({
    app,
    server,
  }) => {
    const before = await server.readRoot(workingRel);
    const helloWhy = msg("ja", "ui.not_editable", {
      reason: msg("ja", "reason.edit_multiline", { line: 5, end: 6 }),
    });
    const multiWhy = msg("ja", "ui.not_editable", {
      reason: msg("ja", "reason.edit_multiline", { line: 7, end: 9 }),
    });

    // 訳が行をまたぐレコード。1行目は生の行を出し、訳の欄にしない。
    const first = rowByLine(app, 5);
    await expect(first).toHaveClass(/(^|\s)not-editable(\s|$)/);
    expect(await textOf(first.locator(".cell.raw"))).toBe(helloFirst);
    expect(await textOf(first.locator(".cell.source"))).toBe(SAMPLE.hello.source);
    await expect(first.locator(".row-note")).toHaveText(helloWhy);
    // 続きの行は値の中の行で、見出しではない。
    const rest = rowByLine(app, 6);
    await expect(rest).toHaveClass(/(^|\s)not-editable(\s|$)/);
    expect(await textOf(rest.locator(".cell.raw"))).toBe('# 二行目"');
    await expect(rest.locator(".row-note")).toHaveText(helloWhy);
    await expect(headings(app).filter({ hasText: "二行目" })).toHaveCount(0);

    // 原文が行をまたぐレコード。原文の欄は原文の全体（空行を含む）で、未翻訳のバッジが付く。
    const multi = rowByLine(app, 7);
    await expect(multi).toHaveClass(/(^|\s)not-editable(\s|$)/);
    expect(await textOf(multi.locator(".cell.source"))).toBe(MULTI);
    await expect(multi.locator(".row-note")).toHaveText(multiWhy);
    await expect(multi.locator(".badge")).toHaveText(msg("ja", "category.untranslated"));
    // 空白だけの続きの行は、ほかの空行と同じく並べない。
    await expect(rowByLine(app, 8)).toHaveCount(0);
    expect(await textOf(rowByLine(app, 9).locator(".cell.raw"))).toBe('para2",');
    await expect(rowByLine(app, 9).locator(".row-note")).toHaveText(multiWhy);

    // 1物理行のレコードは、いままでどおり編集できる。行をまたぐレコードには欄が無い。
    await expect(translationCell(app, 10)).toHaveCount(1);
    for (const n of [5, 6, 7, 9]) {
      await expect(translationCell(app, n)).toHaveCount(0);
    }
    await first.locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});

test.describe("閉じない引用符のある作業コピー", () => {
  // 行番号:
  //   1 ヘッダー / 2 空行 / 3・4 見出し / 5 hello / 6 goodbye（引用符が開いて閉じない）
  //   7 見出しに見える行（値の中） / 8 wonderful / 9 空行
  const opened = record(
    keyFor(SAMPLE.goodbye.source), "L01 Ryan", "Ryan_1_intro", "2", "Ryan", SAMPLE.goodbye.source, "",
  ) + '"さよ';
  const wonderful = record(
    keyFor(SAMPLE.wonderful.source), "L01 Ryan", "Ryan_1_intro", "3", "Kobold", SAMPLE.wonderful.source,
    SAMPLE.wonderful.ja,
  );
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy([
        "",
        "# ===== Level 1: Ryan (Sunny) =====",
        "# --- intro: Ryan_1_intro ---",
        { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
        opened,
        "# --- 見出しに見える行 ---",
        wonderful,
        "",
      ]),
    }),
  });

  test("起動して理由を出し、ファイル全体を編集させず、開いた行から後ろを生の行のまま並べる", async ({
    app,
    server,
  }) => {
    const before = await server.readRoot(workingRel);
    const why = msg("ja", "reason.edit_unclosed_quote", { line: 6 });
    const notes = app.locator("#notes li");
    await expect(notes.filter({ hasText: msg("ja", "ui.file_readonly", { reason: why }) })).toHaveCount(1);
    // 起動時の diff も、この作業コピーに依るカテゴリを判定しなかったことを、行とともに言う。
    await expect(notes.filter({ hasText: msg("ja", "note.working_unclosed", { path: workingRel, line: 6 }) })).toHaveCount(1);

    // 引用符が開く前の見出しは見出しのまま。開いたあとの '#' の行は値の中の行で、見出しにしない。
    await expect(headings(app)).toHaveCount(2);
    await expect(headings(app).filter({ hasText: "見出しに見える行" })).toHaveCount(0);
    const raws = { 6: opened, 7: "# --- 見出しに見える行 ---", 8: wonderful };
    for (const [n, text] of Object.entries(raws)) {
      const row = rowByLine(app, Number(n));
      await expect(row).toHaveClass(/(^|\s)not-editable(\s|$)/);
      expect(await textOf(row.locator(".cell.raw"))).toBe(text);
      await expect(row.locator(".row-note")).toHaveText(msg("ja", "ui.not_editable", { reason: why }));
    }
    // 引用符が開く前の行も、ファイル全体が書けないので編集させない。
    await expect(rowByLine(app, 5)).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(app.locator("#list [data-line]")).toHaveCount(0);

    await rowByLine(app, 8).locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});
