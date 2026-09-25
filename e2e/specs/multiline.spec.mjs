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
//                        物理行の番号がずれる。保存の要求も、入力欄の行き先も ID で決め、
//                        読み上げの名前にだけ行番号を使う。
//   書かせない形         飲み込みの疑い、ゲームの読み方との食い違い、publish がキーを
//                        決められない行（原文が空で、key 列が16桁のキーでも台詞ID でも
//                        ない）、閉じない引用符は、理由を付けて読み取り専用にする。値が
//                        どれも空のレコード（,,,,,,）は空行と同じく並べない。
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

// conflictWith は、次からの保存に、いまのファイルの行一覧を change で書き換えた 409 を返す。
//
// よそが書き換えた場面を、待ち受けの応答だけで作る。訳に改行が入ったレコードを待ち受けは
// 読み取り専用で返す（改行の入力を足すまで）ので、複数行の訳を持つ競合は、いまはファイルを
// 書き換えても作れない。画面がその形を描けることを、応答を差し替えて先に確かめておく。
async function conflictWith(page, server, change) {
  await page.route("**/api/rows", async (route) => {
    const res = await page.request.get(`${server.origin}/api/lines?locale=ja`);
    const current = await res.json();
    current.lines.forEach(change);
    await route.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({ conflict: true, message: msg("ja", "error.conflict"), current }),
    });
  });
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

    // 原文の改行と空行は、描いた字（innerText）にも残る。原文は英語として読ませる。
    const source = rowById(app, ID.multi).locator(".cell.source");
    expect(await drawnText(source)).toBe(MULTI);
    await expect(source).toHaveAttribute("lang", "en");
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

  test("入力欄の名前は最初の物理行を言い、説明に原文と行の1言（保存できない理由）を結ぶ", async ({ app }) => {
    const ed = editor(app);
    await openEditor(app, ID.wonderful);
    await expect(ed).toHaveAccessibleName(msg("ja", "ui.edit_label_line", { line: 11 }));
    await expect(ed).toHaveAttribute("aria-describedby", `source-${ID.wonderful} note-${ID.wonderful}`);
    await expect(ed).toHaveAccessibleDescription(SAMPLE.wonderful.source);
    // 行をまたぐ原文も、説明に全体が入る。
    await openEditor(app, ID.multi);
    await expect(ed).toHaveAccessibleName(msg("ja", "ui.edit_label_line", { line: 6 }));
    await expect(ed).toHaveAccessibleDescription(/para1\s+para2/);

    // 保存できなかった行は、その理由も説明に入る。待ち受けにキーが食い違う要求を送り、
    // その行だけ断らせる（error.row_moved）。
    await app.route("**/api/rows", async (route) => {
      const body = route.request().postDataJSON();
      for (const edit of body.edits) {
        edit.key = "0000000000000000";
      }
      await route.continue({ postData: JSON.stringify(body) });
    });
    await editor(app).fill("段落の訳");
    await editor(app).press("Escape");
    const why = msg("ja", "ui.row_error", { reason: msg("ja", "error.row_moved") });
    await expect(rowById(app, ID.multi).locator(".row-note")).toHaveText(why);
    await openEditor(app, ID.multi);
    await expect(ed).toHaveValue("段落の訳");
    await expect(ed).toHaveAccessibleDescription(new RegExp(`para2\\s+${escapeRegExp(why)}$`));
  });

  test("競合でファイルの訳が複数行でも、ファイルの訳とあなたの訳を段に分けて並べる", async ({ app, server }) => {
    const external = "一行目\n\n三行目";
    await conflictWith(app, server, (line) => {
      if (line.id === ID.wonderful) {
        line.translation = external;
      }
    });
    await typeTranslation(app, ID.wonderful, "すごい！");
    await editor(app).press("Escape");

    const row = rowById(app, ID.wonderful);
    await expect(row).toHaveClass(/(^|\s)conflicted(\s|$)/);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_conflict"));
    const pairs = row.locator(".row-note .note-pair");
    await expect(pairs).toHaveCount(2);
    await expect(pairs.locator(".note-label")).toHaveText([
      `${msg("ja", "ui.conflict_file")}:`,
      `${msg("ja", "ui.conflict_mine")}:`,
    ]);
    // ファイルの訳の改行と空行は、描いた字にも残る。
    expect(await drawnText(pairs.nth(0).locator(".note-value"))).toBe(external);
    await expect(pairs.nth(1).locator(".note-value")).toHaveText("すごい！");
    await expect(row.locator(".row-note .note-locked")).toHaveText(msg("ja", "ui.conflict_locked"));

    // 段が分かれている。あなたの訳の段は、ファイルの訳の段の下から始まる。値の2行目
    // 以降もラベルの下へ回り込まず、値の列にそろう。
    const first = await pairs.nth(0).boundingBox();
    const second = await pairs.nth(1).boundingBox();
    expect(second.y).toBeGreaterThanOrEqual(first.y + first.height - 1);
    const label = await pairs.nth(0).locator(".note-label").boundingBox();
    const value = await pairs.nth(0).locator(".note-value").boundingBox();
    expect(value.x).toBeGreaterThanOrEqual(label.x + label.width - 1);
    expect(value.height).toBeGreaterThan(label.height * 2);
  });

  test("載せる先の無い訳は、目印と訳の段を分けて、行き先の無い訳に並べる", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    // よそが hello のキーを書き換えた。打った訳を載せる先が無くなる。
    await conflictWith(app, server, (line) => {
      if (line.id === ID.hello) {
        line.key = "0000000000000000";
      }
    });
    const typed = "もしもーし、聞こえますか。こちらは港の洗車場です。";
    await typeTranslation(app, ID.hello, typed);
    await editor(app).press("Escape");

    const item = app.locator("#orphans-list li");
    await expect(item).toHaveText([`${keyFor(SAMPLE.hello.source)}: ${typed}`]);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));
    const label = await item.locator(".note-label").boundingBox();
    const value = await item.locator(".note-value").boundingBox();
    expect(value.y).toBeGreaterThanOrEqual(label.y + label.height - 1);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});

test.describe("書くと読み違える形のレコード", () => {
  // 行番号:
  //   1 ヘッダー / 2 hello
  //   3〜5 SWALLOW（訳の開き引用符が5行目で閉じ、4行目のレコードを値に飲み込む）
  //   6 値がどれも空のレコード / 7 DISAGREE（訳の途中の '"'。ゲームの読み方では引用になる）
  //   8 KEYLESS（key 列も原文も空で、訳だけがある）
  //   9 NON_KEY（key 列が16桁のキーでも台詞ID でもなく、原文が空） / 10 wonderful
  const swallow =
    `${recordOf(ui("One."), "")}"いち\r\n` + `${recordOf(ui("Two."), "")}\r\n` + `${recordOf(ui("Three."), "")}さん"`;
  const disagree = `${recordOf(ui("Four."), "")}よ"ん"`;
  const keyless = ",UI,,,UI,,訳だけ";
  const nonKey = "hello,UI,,,UI,,訳だけ";
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy(
        [
          { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
          swallow,
          ",,,,,,",
          disagree,
          keyless,
          nonKey,
          { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
        ],
        { eol: "\r\n" },
      ),
    }),
  });

  test("飲み込みの疑い・ゲームの読み方との食い違い・publish がキーを決められない行は理由を付けて編集させず、空のレコードは並べない", async ({
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

    // publish がキーを決められない行（決まったことの 24）。key 列も原文も空の行と、key 列が
    // 16桁のキーでも台詞ID でもなく原文が空の行。publish が捨てるので、書いた訳は公開されない。
    // 値が空でない列があるので並べるが、生の字と理由を出して編集させない。
    for (const [line, raw] of [
      [8, keyless],
      [9, nonKey],
    ]) {
      const orphan = rowByLine(app, line);
      await expect(orphan).toHaveClass(/(^|\s)not-editable(\s|$)/);
      await expect(orphan.locator(".row-note")).toHaveText(notEditable(msg("ja", "reason.edit_no_key_or_source")));
      expect(await textOf(orphan.locator(".cell.raw"))).toBe(raw);
      await orphan.locator(".cell.raw").click();
      await expect(editor(app)).toHaveCount(0);
    }

    // ほかのレコードはいままでどおり書ける。書けない行には訳の欄が無い。
    await expect(dataRows(app)).toHaveCount(6);
    await expect(app.locator("#list .cell.translation[data-id]")).toHaveCount(2);
    await expect(translationCell(app, 2)).toHaveCount(1);
    await expect(rowByLine(app, 10).locator(".cell.translation[data-id]")).toHaveCount(1);
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

// escapeRegExp は文字列を正規表現の字としてそのまま当てる形にする。
function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
