// 引用符で囲んだ値に改行がある（複数の物理行にまたがる）レコードの見せ方と書き方を見る。
//
// publish と diff は、上流 main と同じくファイル全体を1つの文字列として解釈して読む。
// 画面の編集モデル（internal/edit）も同じ区切りの関数でレコードに分け、1レコードを
// 1行として並べる（internal/web の doc.go「読み違える形と読めないファイル」）。
//
//   行をまたぐレコード   1行として並べ、行番号の欄に最初の物理行と「〜最後の物理行」を
//                        出す。値の中の改行と空行はそのまま描き、値の中の '#' の行は
//                        見出しにしない。訳が1行に収まるかぎり書ける。保存は最終
//                        フィールドだけを差し替える。
//   行は ID で指す       ID はセグメントの通し番号。行をまたぐレコードの後ろでは、ID と
//                        物理行の番号がずれる。保存の要求も、入力欄の行き先も ID で決める。
//   書かせない形         飲み込みの疑い、ゲームの読み方との食い違い、閉じない引用符は、
//                        理由を付けて読み取り専用にする。値がどれも空のレコード（,,,,,,）は
//                        空行と同じく並べない。
//
// 保存がほかのバイトを変えないことは、ファイル全体のバイトで比べる（support/records.mjs）。
// 見本の英文と訳はどれも架空の文である。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, keyFor, record, sampleRepo, workingCopy } from "../support/repo.mjs";
import { expectSameBytes, replaceRecord } from "../support/records.mjs";
import {
  dataRows,
  editor,
  headings,
  openEditor,
  rowById,
  rowByLine,
  saveState,
  translationCell,
  typeTranslation,
  waitForSaved,
} from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// MULTI は、空行を挟んで行をまたぐ原文。ゲーム側の作業コピーの実物にある形（値の中に
// LF が2つ）をまねてある。HASHED は、値の2行目が '#' で始まる原文。どちらも再生順に無い
// UI の行として置く。
const MULTI = "para1\n\npara2";
const HASHED = "First line.\n# not a heading";

const ui = (source, translation = "") => ({
  key: keyFor(source),
  source,
  section: "UI",
  node: "",
  order: "",
  speaker: "UI",
  translation,
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

// textOf は要素の字をそのまま返す（innerText と違い、描き方に左右されない）。
function textOf(locator) {
  return locator.evaluate((e) => e.textContent);
}

// drawnText は描いた字を返す（innerText）。white-space で詰めた改行や空行は、ここで消える。
function drawnText(locator) {
  return locator.evaluate((e) => e.innerText);
}

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

test.describe("行をまたぐレコード", () => {
  // 行番号と ID（区切りは CRLF、値の中は LF）:
  //   1 ヘッダー / 2 空行 / 3・4 見出し / 5 hello（ID 5）
  //   6〜8 MULTI（ID 6。7行目は値の中の空行） / 9〜10 HASHED（ID 7。10行目は値の中の '#' の行）
  //   11 wonderful（ID 8） / 12 空行（ID 9）
  const hello = { ...SAMPLE.hello, translation: SAMPLE.hello.ja };
  const multi = ui(MULTI);
  const hashed = ui(HASHED);
  const wonderful = { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja };
  const ID = { hello: 5, multi: 6, hashed: 7, wonderful: 8 };
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(
        ["", "# ===== Level 1: Ryan (Sunny) =====", "# --- intro: Ryan_1_intro ---", hello, multi, hashed, wonderful, ""],
        { eol: "\r\n" },
      ),
    }),
  });

  test("1レコードを1行に並べ、行番号に範囲を添え、原文の改行・空行・'#' の行をそのまま描く", async ({ app }) => {
    await expect(dataRows(app)).toHaveCount(4);
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 4 }));
    // 並びと行番号（最初の物理行）。行の ID と物理行の番号は、MULTI の後ろからずれる。
    await expect(dataRows(app).locator(".num-start")).toHaveText(["5", "6", "9", "11"]);
    expect(await dataRows(app).evaluateAll((rows) => rows.map((r) => Number(r.dataset.id)))).toEqual([5, 6, 7, 8]);
    // 行をまたぐレコードだけ、下に最後の物理行を添える。
    await expect(dataRows(app).locator(".num-end")).toHaveCount(2);
    await expect(rowById(app, ID.multi).locator(".num-end")).toHaveText(msg("ja", "ui.line_end", { line: 8 }));
    await expect(rowById(app, ID.hashed).locator(".num-end")).toHaveText(msg("ja", "ui.line_end", { line: 10 }));
    // 途中の物理行を1行として並べていない。
    await expect(rowByLine(app, 6)).toHaveCount(1);
    await expect(rowByLine(app, 7)).toHaveCount(0);
    await expect(rowByLine(app, 10)).toHaveCount(0);

    // 原文の改行と空行は、描いた字（innerText）にも残る。
    const source = rowById(app, ID.multi).locator(".cell.source");
    expect(await drawnText(source)).toBe(MULTI);
    expect(await drawnText(rowById(app, ID.hashed).locator(".cell.source"))).toBe(HASHED);
    // 値の中の '#' の行は見出しにしない。見出しはファイルのコメント行の2つだけ。
    await expect(headings(app)).toHaveCount(2);
    await expect(headings(app).filter({ hasText: "not a heading" })).toHaveCount(0);
    // 原文が行をまたいでも、訳が1行に収まるレコードは書ける。
    for (const id of Object.values(ID)) {
      await expect(translationCell(app, id)).toHaveCount(1);
    }
    // 数えたもの。ファイルの物理行と、データ行（レコード）は別に数える。
    const stats = app.locator("#stats li");
    await expect(stats.filter({ hasText: `${msg("ja", "stats.file_lines")}: 12` })).toHaveCount(1);
    await expect(stats.filter({ hasText: `${msg("ja", "stats.data_lines")}: 4` })).toHaveCount(1);
  });

  test("Enter で ID の順に進み、書いたレコードの最終フィールドだけが変わり、ほかは1バイトも変わらない", async ({
    app,
    server,
  }) => {
    const saves = trackSaves(app);
    const before = await server.readRoot(workingRel);
    const typed = { multi: "段落の訳", hashed: "見出しではない訳", wonderful: "すばらしい。" };

    await openEditor(app, ID.multi);
    await editor(app).fill(typed.multi);
    // 次の行は行番号の 7 ではなく、次のレコード（ID 7、9行目から）。
    await editor(app).press("Enter");
    await expect(rowById(app, ID.hashed).locator("textarea.editor")).toBeFocused();
    await editor(app).fill(typed.hashed);
    await editor(app).press("Enter");
    await expect(rowById(app, ID.wonderful).locator("textarea.editor")).toBeFocused();
    await editor(app).fill(typed.wonderful);
    await editor(app).press("Escape");
    await expect.poll(() => saves.flatMap((body) => body.edits).length).toBe(3);
    await waitForSaved(app);

    // 保存の要求は ID で行を指す（行番号の 6・9・11 ではない）。
    const edits = saves.flatMap((body) => body.edits);
    expect(edits.map((edit) => edit.id).sort()).toEqual([ID.multi, ID.hashed, ID.wonderful]);
    for (const edit of edits) {
      expect(edit).not.toHaveProperty("line");
    }

    let expected = replaceRecord(before, recordOf(multi, ""), recordOf(multi, typed.multi));
    expected = replaceRecord(expected, recordOf(hashed, ""), recordOf(hashed, typed.hashed));
    expected = replaceRecord(expected, recordOf(wonderful, SAMPLE.wonderful.ja), recordOf(wonderful, typed.wonderful));
    expectSameBytes(await server.readRoot(workingRel), expected, "作業コピー");

    // 訳を空に戻すと、元のバイト列に戻る。
    for (const id of [ID.multi, ID.hashed]) {
      await typeTranslation(app, id, "");
      await editor(app).press("Escape");
      await waitForSaved(app);
    }
    await typeTranslation(app, ID.wonderful, SAMPLE.wonderful.ja);
    await editor(app).press("Escape");
    await waitForSaved(app);
    expectSameBytes(await server.readRoot(workingRel), before, "作業コピー");
  });
});

test.describe("書くと読み違える形のレコード", () => {
  // 行番号:
  //   1 ヘッダー / 2 hello
  //   3〜5 SWALLOW（訳の開き引用符が5行目で閉じ、4行目のレコードを値に飲み込む）
  //   6 値がどれも空のレコード / 7 DISAGREE（訳の途中の '"'。ゲームの読み方では引用になる）
  //   8 wonderful
  const swallow =
    `${recordOf(ui("One."), "")}"いち\r\n` + `${recordOf(ui("Two."), "")}\r\n` + `${recordOf(ui("Three."), "")}さん"`;
  const disagree = `${recordOf(ui("Four."), "")}よ"ん"`;
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(
        [
          { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
          swallow,
          ",,,,,,",
          disagree,
          { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
        ],
        { eol: "\r\n" },
      ),
    }),
  });

  test("飲み込みの疑いとゲームの読み方との食い違いは理由を付けて編集させず、空のレコードは並べない", async ({
    app,
    server,
  }) => {
    const before = await server.readRoot(workingRel);
    const notEditable = (reason) => msg("ja", "ui.not_editable", { reason });

    // 飲み込みの疑い。飲み込まれて見える物理行（4行目）を名指しする。レコードは1行で、生の字を出す。
    const swallowed = rowByLine(app, 3);
    await expect(swallowed).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(swallowed.locator(".num-end")).toHaveText(msg("ja", "ui.line_end", { line: 5 }));
    await expect(swallowed.locator(".row-note")).toHaveText(notEditable(msg("ja", "reason.edit_swallow", { line: 4 })));
    expect(await textOf(swallowed.locator(".cell.raw"))).toBe(swallow.replaceAll("\r\n", "\n"));
    await expect(rowByLine(app, 4)).toHaveCount(0);

    // 値がどれも空のレコードは、空行と同じく並べない（打った訳が publish で黙って落ちる）。
    await expect(rowByLine(app, 6)).toHaveCount(0);

    // ゲームの読み方との食い違い。どの列かを言う。
    const split = rowByLine(app, 7);
    await expect(split).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(split.locator(".row-note")).toHaveText(
      notEditable(msg("ja", "reason.edit_game_disagrees", { column: "translation" })),
    );
    expect(await textOf(split.locator(".cell.raw"))).toBe(disagree);

    // ほかのレコードはいままでどおり書ける。書けない行には訳の欄が無い。
    await expect(dataRows(app)).toHaveCount(4);
    await expect(app.locator("#list .cell.translation[data-id]")).toHaveCount(2);
    await expect(translationCell(app, 2)).toHaveCount(1);
    await expect(rowByLine(app, 8).locator(".cell.translation[data-id]")).toHaveCount(1);
    await split.locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
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
    // 開いた行から後ろは、1物理行ずつ生の行のまま並べる（行番号に範囲は付かない）。
    const raws = { 6: opened, 7: "# --- 見出しに見える行 ---", 8: wonderful };
    for (const [n, text] of Object.entries(raws)) {
      const row = rowByLine(app, Number(n));
      await expect(row).toHaveClass(/(^|\s)not-editable(\s|$)/);
      await expect(row.locator(".num-end")).toHaveCount(0);
      expect(await textOf(row.locator(".cell.raw"))).toBe(text);
      await expect(row.locator(".row-note")).toHaveText(msg("ja", "ui.not_editable", { reason: why }));
    }
    // 引用符が開く前の行も、ファイル全体が書けないので編集させない。
    await expect(rowByLine(app, 5)).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(app.locator("#list .cell.translation[data-id]")).toHaveCount(0);

    await rowByLine(app, 8).locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});
