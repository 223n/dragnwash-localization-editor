// 一覧の描き方を見る。行の中身は字のまま出し、見出し・バッジ・件数・数えたものは
// 待ち受けが渡したものをそのまま描く。
//
// app.js の冒頭が約束していることのうち、描くところに関わるのは次の3つである。
//
//   - 行の中身を innerHTML に渡さない。訳には <i> のような字が実際に入っている
//     （ゲームの書式）。要素にしてしまうと、翻訳者はその字を読めず、直した訳から
//     タグが落ちる。
//   - 判断を1つも持たない。バッジも件数も待ち受けが渡したものをそのまま描く。
//   - 文言を1つも持たない。理由もカテゴリ名も目録から来る。
//
// 画面はファイルの写しでもある（internal/web の doc.go「画面に新しい判断を置かない」）。
// 見出しはファイルのコメント行そのまま、並びは物理行の順で、直せない行も
// 読めるまま出す。ここが崩れると、翻訳者は自分がどの行を直しているのかを取り違える。
//
// 見本はわざと、タグや文字参照を訳・話者・原文・見出し・直せない行に入れ、
// 改行も LF と CRLF を混ぜてある。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { field, publishedFile, sampleRepo, scriptOrder, workingCopy } from "../support/repo.mjs";
import {
  editor,
  headings,
  openApp,
  openEditor,
  rowByLine,
  translationCell,
  typeTranslation,
  waitForSaved,
} from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// 台詞。タグ・文字参照・イベント属性を、訳・話者・原文のそれぞれに入れてある。
// onerror が動けば window.__pwned が立つ（CSP でも止まるはずだが、ここで見たいのは
// その手前、要素そのものが作られないことである）。
const L = {
  hello: { source: "Hello <b>there</b>?", speaker: "<i>Ryan</i>", order: "1", translation: "<i>もしもし</i>？" },
  amp: { source: "Tom &amp; Jerry", speaker: "Ryan", order: "2", translation: "トム &amp; ジェリー" },
  goodbye: { source: "Goodbye.", speaker: "Ryan", order: "3", translation: "" },
  img: {
    source: "Picture <img src=x>",
    speaker: "Kobold",
    order: "4",
    translation: "<img src=x onerror=window.__pwned='translation'>",
  },
  // 公開ファイルにあって再生順に無い行。section が UI でなければ「台本から消えた行」、
  // UI なら「由来を判定できない行」になる（Go の newTestRoot の keyVanished / keyUI と同じ作り）。
  vanished: { key: "aaaaaaaaaaaaaaaa", source: "", speaker: "Ryan", order: "5", translation: "きえた" },
  ui: { key: "ffffffffffffffff", source: "", section: "UI", node: "", speaker: "UI", order: "", translation: "設定" },
};

// 見出し。ファイルのコメント行そのもの。
const HEADS = {
  section: "# ===== Level 1: <b>Ryan</b> & Co =====",
  node: "# --- intro: <i>Ryan_1_intro</i> ---",
  memo: "# memo <script>window.__pwned='heading'</script> &amp; later",
  ui: "# ===== UI =====",
};

// 列数がヘッダー（7列）と合わない行。3列と1列。
// 2つ目は空白で始まるので見出しではない（internal/edit は生の先頭1文字だけを見る）。
const BROKEN = "deadbeefdeadbeef,<script>window.__pwned='raw'</script>,<img src=x onerror=window.__pwned='raw-img'>";
const INDENTED = " # indented memo";

// 作業コピーの並び。名前 → 中身。items[i] は物理行 i + 2（1行目はヘッダー）。
const ITEMS = [
  ["blank1", ""],
  ["section", HEADS.section],
  ["node", HEADS.node],
  ["memo", HEADS.memo],
  ["hello", L.hello],
  ["amp", L.amp],
  // 空白だけの行は空行相当（internal/edit の isRecord）。一覧に出ない。
  ["spaces", "   "],
  ["broken", BROKEN],
  ["indented", INDENTED],
  ["goodbye", L.goodbye],
  ["img", L.img],
  ["vanished", L.vanished],
  ["blank2", ""],
  ["uiHead", HEADS.ui],
  ["ui", L.ui],
  ["blank3", ""],
];

// LINE は名前 → 物理行番号。
const LINE = Object.fromEntries(ITEMS.map(([name], i) => [name, i + 2]));

// 添字 0 がヘッダー。奇数の添字（偶数の物理行）を CRLF にする。
const mixedEol = (i) => (i % 2 === 1 ? "\r\n" : "\n");

const WORKING = workingCopy(
  ITEMS.map(([, item]) => item),
  { eol: mixedEol },
);

// データ行の数（見出し・空行・ヘッダーを除く）。直せない行も数に入る。
const DATA_ROWS = ["hello", "amp", "broken", "indented", "goodbye", "img", "vanished", "ui"];

// renderRepo はこのファイルの既定の見本。ja は作業コピーあり、he は公開ファイルだけ。
function renderRepo() {
  return {
    root: {
      "data/script_order.csv": scriptOrder([L.hello, L.amp, L.goodbye, L.img]),
      // publish は訳が空の行を書かないので、goodbye は公開ファイルに無い。
      "Translations/ja/strings.csv": publishedFile([
        "",
        "# --- intro: Ryan_1_intro ---",
        L.hello,
        L.amp,
        L.img,
        L.vanished,
        "",
        HEADS.ui,
        L.ui,
        "",
      ]),
      "Translations/he/strings.csv": publishedFile(["", "# --- intro ---", { ...L.hello, translation: "שלום" }, ""]),
      [workingRel]: WORKING,
    },
    game: null,
  };
}

test.use({ repo: renderRepo() });

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

// expectOnlyLineChanged は、before と after が n 行目（1始まり）の最終フィールド以外
// 1バイトも違わないことを確かめる。n 行目の最終フィールドは value になっている。
function expectOnlyLineChanged(before, after, n, value) {
  const beforeLines = splitLines(before);
  const afterLines = splitLines(after);
  expect(afterLines).toHaveLength(beforeLines.length);
  for (let i = 0; i < beforeLines.length; i++) {
    if (i === n - 1) {
      continue;
    }
    expect(afterLines[i].equals(beforeLines[i]), `${i + 1}行目が変わった`).toBe(true);
  }
  // 最終フィールドの始まりは、最後のカンマの次（見本の訳にはカンマも引用符も無い）。
  const line = beforeLines[n - 1].toString("utf8");
  // 改行（LF か CRLF）は元の行のものが残る。
  const m = /^(.*,)[^,\r\n]*(\r?\n)$/s.exec(line);
  expect(m, `${n}行目の形が想定と違う: ${JSON.stringify(line)}`).not.toBeNull();
  // JSON にして比べるのは、CR の有無を失敗の表示で読めるようにするため。
  expect(JSON.stringify(afterLines[n - 1].toString("utf8"))).toBe(JSON.stringify(`${m[1]}${field(value)}${m[2]}`));
}

// textOf は要素の textContent をそのまま返す。toHaveText は空白を詰めて比べるので、
// バイト単位で同じかを見たいところではこちらを使う。
function textOf(locator) {
  return locator.evaluate((e) => e.textContent);
}

// elementChildren は子要素の数。字だけが入っていれば 0 になる。
function elementChildren(locator) {
  return locator.evaluate((e) => e.childElementCount);
}

// openCapturing は画面を開き、そのとき届いた /api/lines の応答を返す。
// 画面が「待ち受けが渡したものをそのまま描く」ことを、応答と見比べて確かめるために使う。
async function openCapturing(page, server) {
  const response = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/api/lines" && res.request().method() === "GET",
  );
  await openApp(page, server);
  return (await response).json();
}

// 行の中身を innerHTML に渡すと、訳の <i> は斜体になり、<img onerror> は動く。
// 翻訳者はゲームの書式を字として読めなくなり、直した訳からタグが落ちる。
// 訳・話者・原文・見出し・直せない行の生のテキストの、5か所すべてで字のまま出ることを見る。
test("訳・話者・原文・見出し・直せない行に入ったタグや文字参照は、要素にせず字のまま出す", async ({ app }) => {
  const hello = rowByLine(app, LINE.hello);
  expect(await textOf(hello.locator(".cell.speaker"))).toBe(L.hello.speaker);
  expect(await textOf(hello.locator(".cell.source"))).toBe(L.hello.source);
  expect(await textOf(hello.locator(".cell.translation"))).toBe(L.hello.translation);

  // 文字参照は解かない。&amp; は &amp; のまま出る（& に化けると、ファイルと画面が食い違う）。
  const amp = rowByLine(app, LINE.amp);
  expect(await textOf(amp.locator(".cell.source"))).toBe(L.amp.source);
  expect(await textOf(amp.locator(".cell.translation"))).toBe(L.amp.translation);

  expect(await textOf(rowByLine(app, LINE.img).locator(".cell.translation"))).toBe(L.img.translation);
  expect(await textOf(rowByLine(app, LINE.broken).locator(".cell.raw"))).toBe(BROKEN);
  expect(await textOf(headings(app).nth(2))).toBe(HEADS.memo);

  // どの欄にも子要素は無い。字だけが入っている。
  for (const cell of [
    hello.locator(".cell.speaker"),
    hello.locator(".cell.source"),
    hello.locator(".cell.translation"),
    rowByLine(app, LINE.img).locator(".cell.translation"),
    rowByLine(app, LINE.broken).locator(".cell.raw"),
    headings(app).nth(2).locator("span"),
  ]) {
    expect(await elementChildren(cell)).toBe(0);
  }
  // 一覧のどこにも、中身から作られた要素は無い。
  await expect(app.locator("#list").locator("i, b, img, script")).toHaveCount(0);
  expect(await app.evaluate(() => window.__pwned)).toBeUndefined();
});

// 見出しは internal/order から組み直さず、ファイルのコメント行を写す（doc.go）。
// 組み直すと、ファイルに無い見出しが出たり、翻訳者が書いたメモが消えたりする。
// 深さは待ち受けが付けた印（# ===== が節、# --- が節点、それ以外が other）で分ける。
// CRLF の行でも、見出しの字に CR が混ざらないことも見る。
//
// 節と節点は読み上げの見出しにもする（role="heading"。節は aria-level 2、節点は 3）。
// 見出しで移る操作で、節から節へ飛べるようにするためである。どちらの印も無いコメント行
// （翻訳者のメモなど）は文書の構造ではないので、見出しにしない。
test("見出しはファイルのコメント行を書き換えずに写し、印で深さを分け、節と節点だけを読み上げの見出しにする", async ({
  app,
}) => {
  await expect(headings(app)).toHaveCount(4);
  const expected = [
    [HEADS.section, "heading section", "2"],
    [HEADS.node, "heading node", "3"],
    [HEADS.memo, "heading other", null],
    [HEADS.ui, "heading section", "2"],
  ];
  for (const [i, [text, className, level]] of expected.entries()) {
    const head = headings(app).nth(i);
    await expect(head).toHaveClass(className);
    expect(await textOf(head)).toBe(text);
    if (level === null) {
      await expect(head).not.toHaveAttribute("role");
      await expect(head).not.toHaveAttribute("aria-level");
      continue;
    }
    await expect(head).toHaveRole("heading");
    await expect(head).toHaveAttribute("aria-level", level);
  }
});

// 画面はファイルの写しである。空行とヘッダー行は番号だけの行になるので出さないが、
// データ行は、直せない行も含めて1行も落とさずファイルの順に並べる。
// 空白で始まる # の行は見出しではなくデータ行として出る（internal/edit と同じ判定）。
// 行数は待ち受けが数えたもので、直せない行も入る。
test("空行とヘッダー行は出さず、データ行は直せない行も含めてファイルの順に並べる", async ({ app }) => {
  const shape = await app
    .locator("#list > *")
    .evaluateAll((nodes) =>
      nodes.map((n) => (n.classList.contains("heading") ? n.textContent : `#${n.querySelector(".cell.num").textContent}`)),
    );
  expect(shape).toEqual([
    HEADS.section,
    HEADS.node,
    HEADS.memo,
    `#${LINE.hello}`,
    `#${LINE.amp}`,
    `#${LINE.broken}`,
    `#${LINE.indented}`,
    `#${LINE.goodbye}`,
    `#${LINE.img}`,
    `#${LINE.vanished}`,
    HEADS.ui,
    `#${LINE.ui}`,
  ]);
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: DATA_ROWS.length }));
  await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: DATA_ROWS.length }));
  // ヘッダー行の列名は一覧に出ない。
  expect(await textOf(app.locator("#list"))).not.toContain("source_en");
});

// 列数がヘッダーと合わない行は、最終フィールドが訳とは限らない。書き換えさせると
// 余った列を巻き込んで壊すので、編集させない（internal/edit の refresh）。
// ただし中身を理由で置き換えると、直せない行を読むことすらできなくなる。
// 生の行を読めるまま出し、理由は鍵のアイコンを付けて別に添える。
test("列数がヘッダーと合わない行は、生の行を読めるまま出し、編集させずに理由を鍵のアイコンで添える", async ({
  app,
}) => {
  const row = rowByLine(app, LINE.broken);
  await expect(row).toHaveClass("row not-editable");
  const raw = row.locator(".cell.raw");
  expect(await textOf(raw)).toBe(BROKEN);
  // 生の行は左から右に固定する。列がずれているので、訳の向きで読ませない。
  await expect(raw).toHaveAttribute("dir", "ltr");
  // 焦点を受けない。Tab の行き先にもクリックの的にもならない。
  await expect(row.locator(".cell.translation[data-id]")).toHaveCount(0);
  await expect(row.locator("[tabindex]")).toHaveCount(0);
  await expect(row.locator(".cell.translation")).toHaveCount(0);

  const note = row.locator(".row-note");
  await expect(note).toBeVisible();
  await expect(note).toHaveText(
    msg("ja", "ui.not_editable", { reason: msg("ja", "reason.edit_field_count", { header: 7, row: 3 }) }),
  );
  await expect(note.locator("svg use")).toHaveAttribute("href", "#i-lock");

  // 1列しかない行も同じ。理由の数だけが変わる。
  const indented = rowByLine(app, LINE.indented);
  await expect(indented).toHaveClass("row not-editable");
  expect(await textOf(indented.locator(".cell.raw"))).toBe(INDENTED);
  await expect(indented.locator(".row-note")).toHaveText(
    msg("ja", "ui.not_editable", { reason: msg("ja", "reason.edit_field_count", { header: 7, row: 1 }) }),
  );

  // 押しても入力欄は開かない。
  await raw.click();
  await expect(editor(app)).toHaveCount(0);
});

// バッジは internal/diff の判定をそのまま写す。画面が数え直したり名前を付け直したり
// すると、diff が避けている誤検出を作り直すことになる（app.js 冒頭、doc.go）。
// 重さは色だけでなく名前でも伝え、理由は添え書き（title）に入れる。
test("バッジは待ち受けが付けた名前・重さ・理由をそのまま写す", async ({ page, server }) => {
  const data = await openCapturing(page, server);

  // 応答の1行ごとに、同じバッジが同じ順で並ぶ。足しも減らしもしない。
  // 理由の無いバッジには添え書きを付けない（空の title を置かない）。
  const drawn = await page.locator("#list .row").evaluateAll((rows) =>
    rows.map((r) => ({
      n: Number(r.querySelector(".cell.num").textContent),
      badges: [...r.querySelectorAll(".badge")].map((b) => ({
        className: b.className,
        text: b.textContent,
        title: b.getAttribute("title"),
      })),
    })),
  );
  const want = data.lines
    .filter((l) => l.kind === "data")
    .map((l) => ({
      n: l.n,
      badges: (l.badges ?? []).map((b) => ({
        className: `badge ${b.status}`,
        text: b.label,
        title: b.note ? b.note : null,
      })),
    }));
  expect(drawn).toEqual(want);

  // 名前と理由は目録から来ている（鍵がそのまま出ていない）。3つの重さを1つずつ見る。
  const expected = [
    [LINE.goodbye, "todo", "category.untranslated", "reason.note_untranslated"],
    [LINE.vanished, "review", "category.vanished", "reason.note_vanished"],
    [LINE.ui, "info", "category.unknown_origin", "reason.note_unknown_origin"],
  ];
  for (const [n, status, label, note] of expected) {
    const badge = rowByLine(page, n).locator(".badge");
    await expect(badge).toHaveCount(1);
    await expect(badge).toHaveClass(`badge ${status}`);
    await expect(badge).toHaveText(msg("ja", label));
    await expect(badge).toHaveAttribute("title", msg("ja", note));
  }
  // 理由にタグが入っていても（原文とタグが違う行）、添え書きの字として入るだけで要素にはならない。
  await expect(rowByLine(page, LINE.hello).locator(".badge")).toHaveAttribute("title", /<i>/);
  await expect(page.locator("#list .badges").locator("i, b")).toHaveCount(0);
});

// 判定できていないカテゴリを「0 件」と書くと「もう何も残っていない」と読まれ、
// 翻訳者は訳を捨てる（internal/diff の doc.go、app.js の countText）。数の代わりに理由を出す。
// 件数（diff が見つけた数）とこの一覧の行数が食い違うカテゴリでは、両方を書く。
// どちらを書くかは待ち受けが決めた印（judged / rowsDiffer）に従い、画面は比べない。
test("件数の欄は、判定していないカテゴリに数を書かず理由を出し、行数が違えば両方を書く", async ({
  page,
  server,
}) => {
  const ja = await openCapturing(page, server);
  const items = page.locator("#counts > li");

  // 待ち受けが返したカテゴリを、返した順にすべて並べる（画面で定義し直さない）。
  await expect(items).toHaveCount(ja.counts.length);
  const statuses = ja.counts.map((c) => c.status);
  // 並びは 要作業 → 要確認 → 参考。
  const rank = { todo: 0, review: 1, info: 2 };
  expect(statuses.map((s) => rank[s])).toEqual([...statuses.map((s) => rank[s])].sort((a, b) => a - b));

  const line = (c) => `${c.statusLabel} / ${c.label} `;
  const byId = (data, id) => {
    const c = data.counts.find((x) => x.category === id);
    expect(c, `${id} が件数に無い`).toBeTruthy();
    return c;
  };

  // 判定できて、数と行数が同じもの。
  const untranslated = byId(ja, "untranslated");
  expect(untranslated).toMatchObject({ judged: true, count: 1, rows: 1, rowsDiffer: false });
  const untranslatedItem = items.nth(ja.counts.indexOf(untranslated));
  await expect(untranslatedItem).toHaveClass("todo");
  await expect(untranslatedItem).toHaveText(
    `${msg("ja", "status.todo")} / ${msg("ja", "category.untranslated")} ${msg("ja", "ui.count_value", { count: 1 })}`,
  );

  // 判定できないもの。再生順に norm 列が無いので「引き継ぎ元の候補」は判定しない。
  const carryFrom = byId(ja, "carry_from");
  expect(carryFrom.judged).toBe(false);
  const carryFromItem = items.nth(ja.counts.indexOf(carryFrom));
  await expect(carryFromItem).toHaveClass("review held");
  await expect(carryFromItem).toHaveText(
    `${msg("ja", "status.review")} / ${msg("ja", "category.carry_from")} ${msg("ja", "ui.not_judged", {
      reason: msg("ja", "reason.judge_order_no_norms"),
    })}`,
  );
  // 判定していないカテゴリのどれにも「0 件」は出ない。
  for (const [i, c] of ja.counts.entries()) {
    if (!c.judged) {
      await expect(items.nth(i)).toHaveClass(`${c.status} held`);
      await expect(items.nth(i)).not.toContainText(msg("ja", "ui.count_value", { count: 0 }));
      await expect(items.nth(i)).toHaveText(`${line(c)}${msg("ja", "ui.not_judged", { reason: c.reason })}`);
    }
  }

  // he は公開ファイルだけなので、未翻訳は判定できず、ほかのロケールにある行は
  // この一覧に出せない（件数が立つのに 0 行）。
  const heResponse = page.waitForResponse((res) => {
    const url = new URL(res.url());
    return url.pathname === "/api/lines" && url.searchParams.get("locale") === "he";
  });
  await page.locator("#locale").selectOption("he");
  const he = await (await heResponse).json();
  await expect(page.locator("#file-path")).toHaveText(`${msg("ja", "ui.file")}: Translations/he/strings.csv`);
  await expect(items).toHaveCount(he.counts.length);

  const heUntranslated = byId(he, "untranslated");
  expect(heUntranslated.judged).toBe(false);
  await expect(items.nth(he.counts.indexOf(heUntranslated))).toHaveText(
    `${msg("ja", "status.todo")} / ${msg("ja", "category.untranslated")} ${msg("ja", "ui.not_judged", {
      reason: msg("ja", "reason.judge_working_missing"),
    })}`,
  );

  const gap = byId(he, "locale_gap");
  expect(gap).toMatchObject({ judged: true, rows: 0, rowsDiffer: true });
  expect(gap.count).toBeGreaterThan(0);
  const gapItem = items.nth(he.counts.indexOf(gap));
  await expect(gapItem).toHaveClass("todo");
  await expect(gapItem).toHaveText(
    `${msg("ja", "status.todo")} / ${msg("ja", "category.locale_gap")} ${msg("ja", "ui.count_and_rows", {
      count: gap.count,
      rows: 0,
    })}`,
  );
});

// 「数えたもの」も待ち受けが数えた値である。画面は名前と数を「名前: 数」の形に
// 並べるだけで、複数形の規則も持ち込まない（model.go の statView）。
test("数えたものは、待ち受けが返した名前と数を「名前: 数」で並べる", async ({ page, server }) => {
  const data = await openCapturing(page, server);
  const items = page.locator("#stats > li");
  await expect(items).toHaveText(data.stats.map((s) => `${s.label}: ${s.value}`));

  // 先頭の2つはファイルから分かる数。物理行は改行の数（末尾に改行がある）、
  // データ行は直せない行も含めた数。
  const physical = WORKING.split("\n").length - 1;
  await expect(items.nth(0)).toHaveText(`${msg("ja", "stats.file_lines")}: ${physical}`);
  await expect(items.nth(1)).toHaveText(`${msg("ja", "stats.data_lines")}: ${DATA_ROWS.length}`);
});

// 保存したあとの描き直しでも、訳は字として入る（applyResults の setShownText）。
// ここで要素にすると、打った瞬間は字だったのに保存した途端に斜体や画像に化ける。
// ファイルには打った字がそのまま入り、ほかの行（タグ入りの行、直せない行、
// 見出し、改行の種類）は1バイトも変わらない。
test("タグ入りの訳を打って保存しても、字のまま描き直し、ファイルにはその行の訳だけが入る", async ({
  app,
  server,
}) => {
  const typed = "<b>さようなら</b> & <img src=x onerror=window.__pwned='typed'>";
  const before = await server.readRoot(workingRel);

  await typeTranslation(app, LINE.goodbye, typed);
  await editor(app).press("Enter");
  // 次の編集できる行（img）が開き、閉じた行は待たずに送られる。
  await expect(rowByLine(app, LINE.img).locator("textarea.editor")).toHaveCount(1);

  await expect
    .poll(async () => (await server.readRoot(workingRel)).equals(before), { timeout: 10_000 })
    .toBe(false);
  await waitForSaved(app);
  expectOnlyLineChanged(before, await server.readRoot(workingRel), LINE.goodbye, typed);

  const cell = translationCell(app, LINE.goodbye);
  await expect(cell).toBeVisible();
  expect(await textOf(cell)).toBe(typed);
  expect(await elementChildren(cell)).toBe(0);
  await expect(app.locator("#list").locator("i, b, img, script")).toHaveCount(0);
  expect(await app.evaluate(() => window.__pwned)).toBeUndefined();
});

// 入力欄に入る値は、ファイルの訳そのものでなければならない（shownValue と entry.saved）。
// 描いた字からタグを落とした値で開くと、翻訳者が1字書き足しただけでタグが消えた訳が保存される。
test("タグ入りの訳に書き足すと、タグを残したまま保存する", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);

  await openEditor(app, LINE.hello);
  await expect(editor(app)).toHaveValue(L.hello.translation);
  await editor(app).press("End");
  await editor(app).pressSequentially("!");
  await editor(app).press("Enter");

  await expect
    .poll(async () => (await server.readRoot(workingRel)).equals(before), { timeout: 10_000 })
    .toBe(false);
  await waitForSaved(app);
  expectOnlyLineChanged(before, await server.readRoot(workingRel), LINE.hello, `${L.hello.translation}!`);
  expect(await textOf(translationCell(app, LINE.hello))).toBe(`${L.hello.translation}!`);
});

test.describe("英語の画面では", () => {
  test.use({ dwloc: { uiLang: "en" } });

  // 画面は文言を1つも持たない（app.js 冒頭）。行に添える理由、バッジの名前と理由、
  // 件数、数えたものまで目録を通る。1か所でも日本語が残ると、英語の画面で
  // その行だけ読めなくなる。ここは行の中身（日本語の訳）を含まない場所だけを見る。
  test("バッジ・件数・数えたもの・直せない行の理由も英語で出る", async ({ app }) => {
    await expect(rowByLine(app, LINE.broken).locator(".row-note")).toHaveText(
      msg("en", "ui.not_editable", { reason: msg("en", "reason.edit_field_count", { header: 7, row: 3 }) }),
    );
    const vanished = rowByLine(app, LINE.vanished).locator(".badge");
    await expect(vanished).toHaveText(msg("en", "category.vanished"));
    await expect(vanished).toHaveAttribute("title", msg("en", "reason.note_vanished"));

    await expect(app.locator("#counts > li", { hasText: msg("en", "category.carry_from") })).toHaveText(
      `${msg("en", "status.review")} / ${msg("en", "category.carry_from")} ${msg("en", "ui.not_judged", {
        reason: msg("en", "reason.judge_order_no_norms"),
      })}`,
    );
    await expect(app.locator("#stats > li").nth(1)).toHaveText(`${msg("en", "stats.data_lines")}: ${DATA_ROWS.length}`);

    // ひらがな・カタカナ（U+3040〜U+30FF）と漢字（U+3400〜U+9FFF）が1字も無い。
    const hasJapanese = (text) =>
      [...text].some((ch) => {
        const c = ch.codePointAt(0);
        return (c >= 0x3040 && c <= 0x30ff) || (c >= 0x3400 && c <= 0x9fff);
      });
    const texts = await app
      .locator("#list .badge, #list .row-note, #counts > li, #stats > li")
      .evaluateAll((nodes) => nodes.flatMap((n) => [n.textContent, n.getAttribute("title") ?? ""]));
    expect(texts.length).toBeGreaterThan(0);
    for (const text of texts) {
      expect(hasJapanese(text), `日本語が残っている: ${text}`).toBe(false);
    }
  });
});

test.describe("右から左のロケールでは", () => {
  // he の作業コピー。原文の欄が埋まるように作業コピーを置く。
  const hebrew = { source: "Hello?", speaker: "Ryan", order: "1", translation: "שלום, עולם" };
  // 先頭がラテン文字の訳。向きを中身から決めるので、こちらは左から右になる。
  const latinFirst = { source: "OK then.", speaker: "Kobold", order: "2", translation: "OK שלום" };
  const items = ["", "# --- intro: Ryan_1_intro ---", hebrew, latinFirst, ""];
  const HE = { hebrew: 4, latinFirst: 5 };

  test.use({
    repo: {
      root: {
        "data/script_order.csv": scriptOrder([hebrew, latinFirst]),
        "Translations/he/strings.csv": publishedFile(items),
        "Translations/_discovered/he.working.csv": workingCopy(items),
      },
      game: null,
    },
    dwloc: { locale: "he" },
  });

  // 訳の向きを頁（ltr）に合わせると、he の訳は句読点や数字の位置が崩れて読めない。
  // 逆に rtl に決め打ちすると、ラテン文字で始まる訳が崩れる。だから向きは中身から
  // 決めさせ（dir=auto）、lang にはロケール名を入れる。原文は常に英語なので、
  // 訳に引きずられないよう左から右に固定する（app.js の rowNode）。
  // 入力欄を開いても同じ扱いになることも見る。開いた途端に向きが変わると、打つ位置を見失う。
  test("訳の欄は中身から向きを決め、原文は左から右に固定する", async ({ app }) => {
    const direction = (locator) => locator.evaluate((e) => getComputedStyle(e).direction);

    const cell = translationCell(app, HE.hebrew);
    await expect(cell).toHaveAttribute("dir", "auto");
    await expect(cell).toHaveAttribute("lang", "he");
    expect(await direction(cell)).toBe("rtl");

    const latin = translationCell(app, HE.latinFirst);
    await expect(latin).toHaveAttribute("dir", "auto");
    expect(await direction(latin)).toBe("ltr");

    for (const n of [HE.hebrew, HE.latinFirst]) {
      const source = rowByLine(app, n).locator(".cell.source");
      await expect(source).toHaveAttribute("dir", "ltr");
      expect(await direction(source)).toBe("ltr");
    }

    await openEditor(app, HE.hebrew);
    await expect(editor(app)).toHaveAttribute("dir", "auto");
    await expect(editor(app)).toHaveAttribute("lang", "he");
    expect(await direction(editor(app))).toBe("rtl");
    await expect(editor(app)).toHaveValue(hebrew.translation);
  });
});

test.describe("空白を含む訳", () => {
  // 先頭の空白と、途中で続く空白。前後に空白がある値は引用して書く（field）。
  const spaced = "  もし  もし？";
  const repo = sampleRepo();
  repo.root[workingRel] = workingCopy([
    "",
    "# --- intro: Ryan_1_intro ---",
    { source: "Hello?", speaker: "Ryan", order: "1", translation: spaced },
    { source: "Goodbye.", speaker: "Ryan", order: "2", translation: "" },
    { source: "Wonderful!", speaker: "Kobold", order: "3", translation: "すばらしい！" },
    "",
  ]);
  test.use({ repo });

  // 訳の空白はゲームにそのまま出る。前後の空白は internal/edit がわざわざ引用して守って
  // いる値で（escapeTranslation）、翻訳者が画面で気づけないと直しようがない。
  // app.js の入力欄の注記は「出ている側の欄は white-space: pre-wrap で折り返している」と
  // 書いており、入力欄（pre-wrap）と同じ見え方にそろえたと言っている。
  // 描かれた字（innerText）が、ファイルの訳と同じであることを見る。
  test("訳の先頭の空白と続いた空白を詰めずに出す", async ({ app }) => {
    const cell = translationCell(app, 4);
    // ファイルの値は届いている（ここまでは通る）。
    expect(await textOf(cell)).toBe(spaced);
    // 描かれた字も同じでなければならない。
    expect(await cell.evaluate((e) => e.innerText)).toBe(spaced);
  });
});

test.describe("長い一覧の読み上げの木", () => {
  // 200 行と、20 行ごとの節点の見出し 10 個。画面（800px の高さ）に収まらない長さにする。
  const probes = [];
  const items = [];
  for (let i = 1; i <= 200; i++) {
    const n = String(i).padStart(3, "0");
    if (i % 20 === 1) {
      items.push(`# --- block ${n} ---`);
    }
    const probe = { source: `Probe line ${n}.`, section: "UI", node: "", order: "", speaker: "UI", translation: `訳${i}` };
    probes.push(probe);
    items.push(probe);
  }
  const repo = sampleRepo();
  repo.root[workingRel] = workingCopy(items);
  test.use({ repo });

  // axNodes は、Chromium の読み上げの木（アクセシビリティツリー）の節点のうち、無視されて
  // いないものを返す。Playwright の toHaveAccessibleName や ariaSnapshot は DOM から
  // 計算するので、読み上げの木の欠けを見ない。CDP で木そのものを読む。
  async function axNodes(page) {
    const cdp = await page.context().newCDPSession(page);
    try {
      await cdp.send("Accessibility.enable");
      const { nodes } = await cdp.send("Accessibility.getFullAXTree");
      return nodes.filter((n) => !n.ignored);
    } finally {
      await cdp.detach();
    }
  }

  // nodeHeadingNames は、読み上げの木にある節点の見出し（aria-level 3）の名前を並びのまま返す。
  async function nodeHeadingNames(page) {
    const level = (n) => n.properties?.find((p) => p.name === "level")?.value?.value;
    return (await axNodes(page))
      .filter((n) => n.role?.value === "heading" && level(n) === 3)
      .map((n) => (n.name?.value ?? "").trim());
  }

  // missingRowTexts は、見本の 200 行の原文と訳のうち、読み上げの木の字の節点に無いものを返す。
  async function missingRowTexts(page) {
    const texts = new Set(
      (await axNodes(page)).filter((n) => n.role?.value === "StaticText").map((n) => n.name?.value ?? ""),
    );
    return probes.flatMap((p) => [p.source, p.translation]).filter((text) => !texts.has(text));
  }

  // afterFrames は、2つの描画を待つ。送った先を描き終えてから読み上げの木を読む。
  function afterFrames(page) {
    return page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  }

  // 読み上げソフトの見出しの一覧と見出しへ移る操作（節と節点を見出しにした改善の ui-10）は、
  // 読み上げの木の見出しの名前を使う。見出しにも描かない指定（content-visibility: auto）を
  // 付けていたころは、画面の外の見出しの名前が空になり、スクロールすると今度は上の見出しの
  // 名前が空になった（PR3 の検証の指摘）。
  test("画面の外の節点の見出しも、読み上げの木で名前を持つ", async ({ app }) => {
    await expect(app.locator("#list .row")).toHaveCount(200);
    const want = Array.from({ length: 10 }, (_, i) => `# --- block ${String(i * 20 + 1).padStart(3, "0")} ---`);
    // 最後の見出しは、はじめは画面の外にある。
    const top = await headings(app)
      .last()
      .evaluate((e) => e.getBoundingClientRect().top);
    expect(top).toBeGreaterThan(await app.evaluate(() => window.innerHeight));
    await expect.poll(() => nodeHeadingNames(app)).toEqual(want);

    await headings(app).last().evaluate((e) => e.scrollIntoView());
    await afterFrames(app);
    await expect.poll(() => nodeHeadingNames(app)).toEqual(want);
  });

  // 読み上げソフトで行を読み進めるときは、画面の外の行も読めなければならない。行にも描かない
  // 指定を付けていたころは、画面の外の行の原文と訳が、描くまで読み上げの木に入らず、読み
  // 進めると飛ばされるおそれがあった（決まったことの 23 で指定を取り下げた）。
  test("画面の外の行の原文と訳も、読み上げの木に入る", async ({ app }) => {
    const rows = app.locator("#list .row");
    await expect(rows).toHaveCount(200);
    // 最後の行は、はじめは画面の外にある。
    const top = await rows.last().evaluate((e) => e.getBoundingClientRect().top);
    expect(top).toBeGreaterThan(await app.evaluate(() => window.innerHeight));
    await expect.poll(() => missingRowTexts(app)).toEqual([]);

    // 最後まで送ると、今度は最初の行が画面の外になる。
    await rows.last().evaluate((e) => e.scrollIntoView());
    await afterFrames(app);
    expect(await rows.first().evaluate((e) => e.getBoundingClientRect().bottom)).toBeLessThan(0);
    await expect.poll(() => missingRowTexts(app)).toEqual([]);
  });
});
