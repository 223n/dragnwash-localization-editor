// 起動してから行が並ぶまで（目録・ロケールの選択・読み直し）を見る。
//
// 画面は文言を1つも持たず、/api/bootstrap の目録で組み上がる（app.js 冒頭の約束）。
// 行は /api/lines で読み、読み直しとロケールの切り替えでも同じ道を通る。
// ここが崩れると、翻訳者は「どの言語の画面で、どのファイルの、どの版を見ているか」を
// 取り違える。
//
// いちばん重いのは、切り替えと読み直しが訳を捨てる操作だということである。
// 未保存の訳があれば必ず尋ね、断られたら何も変えない。読み込みに失敗したときも
// 何も変えない（app.js の load と confirmDiscard）。
import { readFileSync } from "node:fs";
import { rm } from "node:fs/promises";
import { join } from "node:path";

import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { root } from "../support/paths.mjs";
import { SAMPLE, SAMPLE_LINES, keyFor, sampleRepo } from "../support/repo.mjs";
import {
  dataRows,
  editor,
  openApp,
  rowByLine,
  saveState,
  translationCell,
  typeTranslation,
  waitForSaved,
} from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";
const heRel = "Translations/he/strings.csv";
const typed = "さようなら。";

// wrongKey はファイルに無いキー。保存の要求をこれに差し替えると、待ち受けは
// 「この行はずれています」で書かずに断る（internal/web の keyMatches）。行ごとの
// 失敗を、待ち受けの本物の応答で作るために使う。
const wrongKey = keyFor("この原文は見本のどこにも無い");

// meta は目録ファイルの lang と dir。catalog.mjs は messages しか返さないので、ここで読む。
function meta(lang) {
  const path = join(root, "internal", "web", "ui", "i18n", `${lang}.json`);
  const json = JSON.parse(readFileSync(path, "utf8"));
  return { lang: json.lang, dir: json.dir };
}

// fileLabel は #file-path に出る1行。
function fileLabel(lang, path) {
  return `${msg(lang, "ui.file")}: ${path}`;
}

// isPath は URL のパスだけで当てる述語。問い合わせ文字列の有無に左右されない。
function isPath(pathname) {
  return (url) => url.pathname === pathname;
}

// watchRequests は、ある経路への要求を数える。
function watchRequests(page, pathname) {
  const seen = [];
  page.on("request", (req) => {
    if (new URL(req.url()).pathname === pathname) {
      seen.push(req);
    }
  });
  return seen;
}

// watchDialogs はダイアログを控え、accept が真なら受け、偽なら断る。
function watchDialogs(page, accept) {
  const seen = [];
  page.on("dialog", async (dialog) => {
    seen.push({ type: dialog.type(), message: dialog.message() });
    if (accept) {
      await dialog.accept();
    } else {
      await dialog.dismiss();
    }
  });
  return seen;
}

// chipBox は条件のチップのチェックボックス。label は画面に出る名前。
function chipBox(page, label) {
  return page.locator("#filters label.chip").filter({ hasText: label }).locator('input[type="checkbox"]');
}

// failRow は行 n に value を打ち、待ち受けに行ごとに断らせて「保存できない行」にする。
//
// 要求のキーをファイルに無いものへ差し替えて送る。待ち受けは 422 と行ごとの理由を
// 返し、ファイルへは1バイトも書かない。画面は値を控えたまま自動保存の対象から外すので、
// 時計が動いても状態が変わらない（送り直しの時計を回さない）。
async function failRow(page, n, value) {
  await page.route(isPath("/api/rows"), async (route) => {
    const body = JSON.parse(route.request().postData() ?? "{}");
    body.edits = (body.edits ?? []).map((edit) => ({ ...edit, key: wrongKey }));
    await route.continue({ postData: JSON.stringify(body) });
  });
  await typeTranslation(page, n, value);
  // Escape は閉じるだけで、閉じるときに保存へ回る。
  await editor(page).press("Escape");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
  await expect(rowByLine(page, n)).toHaveClass(/(^|\s)save-failed(\s|$)/);
  await expect(translationCell(page, n)).toHaveText(value);
}

test.describe("画面の言語", () => {
  // 目録から入る文言の代表。どれも index.html では空で、app.js の applyCatalog が埋める。
  const texts = [
    ["#app-title", "app.title"],
    ["#locale-label", "ui.locale"],
    ["#reload-label", "ui.reload"],
    ["#col-line", "ui.col_line"],
    ["#col-status", "ui.col_status"],
    ["#col-speaker", "ui.col_speaker"],
    ["#col-source", "ui.col_source"],
    ["#col-translation", "ui.edit_label"],
    ["#edit-notice-text", "ui.edit_notice"],
    ["#finder-title", "ui.finder_title"],
    ["#filter-label", "ui.filter"],
    ["#filter-clear-label", "ui.filter_clear"],
    ["#keys-title", "ui.keys_title"],
    ["#keys", "ui.keys_help"],
    ["#finder-note-title", "ui.finder_note_title"],
    ["#finder-note", "ui.finder_note"],
    ["#export-title", "ui.export_title"],
    ["#panel-more", "ui.panel_more"],
    ["#counts-title", "ui.counts"],
    ["#stats-title", "ui.stats"],
    ["#conflict-title", "ui.conflict_title"],
    ["#orphans-title", "ui.orphans_title"],
    ["#save-state", "ui.save_clean"],
  ];
  // 名前を属性で持つもの（アイコンだけのボタンと、検索の欄）。
  const attrs = [
    ["#menu", "aria-label", "ui.sidebar"],
    ["#sidebar-close", "aria-label", "ui.sidebar_close"],
    ["#search", "aria-label", "ui.search"],
    ["#search", "placeholder", "ui.search_placeholder"],
  ];

  // expectCatalog は画面が lang の目録で組まれていることを確かめる。
  async function expectCatalog(page, lang) {
    const { lang: htmlLang, dir } = meta(lang);
    await expect(page.locator("html")).toHaveAttribute("lang", htmlLang);
    await expect(page.locator("html")).toHaveAttribute("dir", dir);
    for (const [selector, key] of texts) {
      await expect(page.locator(selector), selector).toHaveText(msg(lang, key));
    }
    for (const [selector, name, key] of attrs) {
      await expect(page.locator(selector), `${selector} の ${name}`).toHaveAttribute(name, msg(lang, key));
    }
    await expect(page.locator("#rows")).toHaveText(msg(lang, "ui.rows", { count: 3 }));
    // 断り書きは待ち受けが組む（/api/lines）。画面と同じ言語で来なければ、1画面に2言語が混ざる。
    await expect(page.locator("#notes li").first()).toHaveText(
      msg(lang, "note.via_publish", { path: "Translations/ja/strings.csv" }),
    );
  }

  for (const lang of ["ja", "en"]) {
    test.describe(`--ui-lang ${lang} で起動したとき`, () => {
      // ブラウザーは設定の既定（ja-JP）のまま。--ui-lang はそれより強い。
      test.use({ dwloc: { uiLang: lang } });

      // 画面の中に言語の切り替えは無い（README）。--ui-lang が効かないと、翻訳者は
      // 読めない言語の画面から抜け出す手段を持たない。html の lang は読み上げと字形の
      // 選び方を決めるので、文言と一緒に変わらなければならない。
      test("文言と html の lang・dir がその言語の目録になる", async ({ app }) => {
        await expectCatalog(app, lang);
      });
    });
  }

  test.describe("--ui-lang en でブラウザーが日本語を求めるとき", () => {
    test.use({ dwloc: { uiLang: "en" }, locale: "ja-JP" });

    // 英語の画面に日本語の文言が1つでも残ると、それは目録を通らずに書かれた文字列である
    // （「文言を1つも持たない」の破れ）。行の中身（ja の訳）は日本語でよいので一覧は外し、
    // 目録と待ち受けの文だけで組まれる場所を見る。閉じた畳みの中身も textContent で見る。
    test("行の中身のほかには日本語を1字も出さない", async ({ app }) => {
      await expect(app.locator("#stats li")).not.toHaveCount(0);
      const texts = await app.evaluate(() =>
        [".top", "#sidebar", ".thead", "#edit-notice"].map((sel) => [sel, document.querySelector(sel).textContent]),
      );
      for (const [selector, text] of texts) {
        expect(text, selector).not.toMatch(/[\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Han}、。「」『』（）：]/u);
      }
    });
  });

  // --ui-lang を省いたときは Accept-Language で決まり、当たらなければ英語（internal/web の
  // forRequest）。英語へ落とすのは、目録の無い言語の翻訳者にとって日本語より読める
  // 見込みが高いからで、日本語へ落とすと README の「ja が当たらないと英語」とも食い違う。
  for (const [browser, lang] of [
    ["en-US", "en"],
    ["ja-JP", "ja"],
    ["de-DE", "en"],
  ]) {
    test.describe(`--ui-lang を省いてブラウザーが ${browser} のとき`, () => {
      test.use({ dwloc: { uiLang: "" }, locale: browser });

      test(`${lang} の目録で組む`, async ({ app }) => {
        await expectCatalog(app, lang);
      });
    });
  }
});

// 題名はブラウザーの履歴に残る（app.js 冒頭「題名に行の中身を入れない」）。行の中身が
// 入ると、原文（再配布しない英語の台本）が履歴という別の場所へ漏れる。ロケールを
// 切り替えても題名が変わらないことも見る。変わるなら、そこに何かを差し込む道がある。
test("題名は目録の app.title だけで、ロケールを切り替えても変わらない", async ({ app }) => {
  const title = msg("ja", "app.title");
  await expect(app).toHaveTitle(title);
  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
  await expect(app).toHaveTitle(title);
});

// トークンは最初の1回だけ URL に載る。303 で / へ移して Cookie に替えるので、アドレス欄と
// 履歴にはトークンの無い URL しか残らない。残ると、履歴を見た人がこの待ち受けに書き込める。
test("トークン付きの URL を開くと 303 で / へ移り、URL にトークンが残らない", async ({ page, server }) => {
  const statuses = [];
  page.on("response", (res) => {
    if (res.request().isNavigationRequest()) {
      statuses.push([new URL(res.url()).search, res.status()]);
    }
  });
  await openApp(page, server);
  expect(page.url()).toBe(`${server.origin}/`);
  expect(page.url()).not.toContain(server.token);
  expect(statuses).toEqual([
    [`?t=${server.token}`, 303],
    ["", 200],
  ]);
});

// 翻訳者は画面を F5 で開き直す。トークンはもう URL に無いので、Cookie だけで入れなければ
// ならない。入れないと、訳している途中の頁を開き直した瞬間に締め出される。
test("トークンの無い / のまま開き直しても、同じ画面が組み上がる", async ({ app, server }) => {
  await app.reload();
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  expect(app.url()).toBe(`${server.origin}/`);
});

// ロケール名はデータ側の語彙なので訳さずにそのまま出す（fillLocales）。並びと中身は
// 待ち受けが返した一覧のままで、画面では足さない。指定して起動したときは「選んで
// ください」を入れない。入れると、行が並んでいるのに未選択へ戻せる選択肢が1つ増える。
test("ロケールの選択肢は待ち受けが返した一覧で、起動時に指定したロケールを選んでおく", async ({ app }) => {
  const options = app.locator("#locale option");
  await expect(options).toHaveText(["he", "ja"]);
  await expect(app.locator("#locale")).toHaveValue("ja");
  await expect(app.locator('#locale option[value=""]')).toHaveCount(0);
});

test.describe("ロケールを省いて起動したとき", () => {
  test.use({ dwloc: { locale: "" } });

  // 省くのは既定の経路（ダブルクリックで開いたときもこれ）。選ぶ前に「ファイル」の畳みを
  // 出すと、名前の入っていない畳みが焦点の順に残る（render の注記）。選ぶ前にどれかの
  // ロケールを読みにいくと、翻訳者が決めていないロケールの一覧を直させることになる。
  test("選ぶまで行もファイルの畳みも出さず、/api/lines も叩かない", async ({ page, server }) => {
    const lines = watchRequests(page, "/api/lines");
    await openApp(page, server);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.select_locale"));
    await expect(page.locator("#locale option")).toHaveText([msg("ja", "ui.select_locale"), "he", "ja"]);
    await expect(page.locator("#locale")).toHaveValue("");
    await expect(dataRows(page)).toHaveCount(0);
    await expect(page.locator("#rows")).toBeEmpty();
    await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", true);
    expect(lines).toHaveLength(0);

    await page.locator("#locale").selectOption("ja");
    await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", false);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(page.locator("#message")).toBeEmpty();
    expect(lines).toHaveLength(1);
  });

  // 選んだあとも空の選択肢が残っていたころは、空へ戻すと一覧だけが消え、「表示中 N 行」・
  // ファイルの名前・条件のチップ・抱えている訳は前のロケールのまま残った。「表示中 N 行」は、
  // いま画面に出ている行数を言う唯一の場所である（doc.go「出ている行数は件数と混ぜない」、
  // index.html の #shown）。選んだあとは、起動時に指定したときと同じ並びにして（上の
  // 「ロケールの選択肢は待ち受けが返した一覧で…」）、戻す道そのものを作らない（app.js の load）。
  test("選んだあとは空の選択肢を外し、前のロケールの表示を残したまま空へ戻す道を作らない", async ({ app }) => {
    await app.locator("#locale").selectOption("ja");
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 3 }));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator('#locale option[value=""]')).toHaveCount(0);

    // 外したあとも、ほかのロケールへは今までどおり切り替えられる。
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
  });

  // 空の選択肢を外すのは読めたときだけにする。最初の選択で読めなかったときに外すと、
  // 欄は空（load の catch が state.locale へ戻す）なのに、それを表す選択肢が無くなる。
  test("最初に選んだロケールを読めなければ、選択を空へ戻し、空の選択肢も残す", async ({ app, server }) => {
    await rm(server.rootPath(heRel));
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(app.locator("#locale")).toHaveValue("");
    await expect(app.locator("#locale option")).toHaveText([msg("ja", "ui.select_locale"), "he", "ja"]);
    await expect(dataRows(app)).toHaveCount(0);

    await app.locator("#locale").selectOption("ja");
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
  });
});

// 畳みの summary は焦点を受ける。文言が入る前に出すと、名前の無い開閉要素が焦点の順に
// 並ぶ（index.html の注記）。「ファイル」の畳みだけは summary の主役がファイルの名前なので、
// 目録ではなく /api/lines を受けてから出す（applyCatalog と render の注記）。
test("畳みは目録が届いたら出し、ファイルの畳みだけは行を読んでから出す", async ({ page, server }) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await page.route(isPath("/api/lines"), async (route) => {
    await gate;
    await route.continue();
  });
  await page.goto(server.url);

  // 目録は届いて、行はまだ届いていない。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(page.locator("#message")).toHaveText(msg("ja", "ui.loading"));
  for (const id of ["#keys-fold", "#finder-fold", "#export-fold"]) {
    await expect(page.locator(id), id).toHaveJSProperty("hidden", false);
  }
  await expect(page.locator("#keys-title")).toHaveText(msg("ja", "ui.keys_title"));
  await expect(page.locator("#finder-note-title")).toHaveText(msg("ja", "ui.finder_note_title"));
  await expect(page.locator("#export-title")).toHaveText(msg("ja", "ui.export_title"));
  await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", true);
  await expect(page.locator("#file-path")).toBeEmpty();
  await expect(page.locator("#rows")).toBeEmpty();

  release();
  await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", false);
  await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
  await expect(page.locator("#message")).toBeEmpty();
  await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
});

// 目録が取れないと、画面は1文字も訳せない。それでも「黙って失敗しない」ので、失敗は
// 帯に出す。畳みは名前が入らないので出さない。行も読みにいかない（どのロケールかも
// 分からない）。
test("目録を取れないときも黙らず、名前の無い畳みを出さず、行を読みにいかない", async ({ page, server }) => {
  await page.route(isPath("/api/bootstrap"), (route) =>
    route.fulfill({ status: 500, contentType: "text/plain", body: "boom" }),
  );
  const lines = watchRequests(page, "/api/lines");
  await page.goto(server.url);
  await expect(page.locator("#message")).not.toBeEmpty();
  for (const id of ["#keys-fold", "#finder-fold", "#export-fold", "#panel-fold"]) {
    await expect(page.locator(id), id).toHaveJSProperty("hidden", true);
  }
  await expect(page.locator("#locale option")).toHaveCount(0);
  await expect(dataRows(page)).toHaveCount(0);
  expect(lines).toHaveLength(0);
});

test.describe("ゲームのフォルダー", () => {
  // --game が無いのに探し先の欄を出すと、どこも探していないのに「探している」と読まれる。
  test("--game が無ければ探し先の欄を隠して空にする", async ({ app }) => {
    await expect(app.locator("#game-path")).toHaveJSProperty("hidden", true);
    await expect(app.locator("#game-path")).toBeEmpty();
  });

  test.describe("--game を付けたとき", () => {
    test.use({ repo: sampleRepo({ game: true }) });

    // 探し先は起動時に決まり、画面の操作では変わらない（showGamePath の注記）。ロケールを
    // 替えて消えると、he では探したのに無かったのか、探していないのかが分からなくなる。
    test("ロケールを切り替えても探し先を出し続け、ファイルの欄だけが替わる", async ({ app, server }) => {
      const game = `${msg("ja", "ui.game_folder")}: ${server.game.replaceAll("\\", "/")}`;
      await expect(app.locator("#game-path")).toHaveText(game);
      await app.locator("#locale").selectOption("he");
      await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
      await expect(app.locator("#game-path")).toHaveText(game);
      await expect(app.locator("#game-path")).toHaveJSProperty("hidden", false);
    });
  });
});

// 画面で直した訳がどこへ入るのかは、断り書きにしか出ない。作業コピーを並べているときに
// 「publish を回すとコミットする側へ入る」と言わないと、翻訳者は画面の保存でコミットする
// 側が変わったと思う。公開ファイルそのものを並べているときに言うと、それは嘘になる。
test("作業コピーを並べているか公開ファイルを並べているかを、断り書きで言い分ける", async ({ app }) => {
  const notes = app.locator("#notes li");
  const viaPublish = msg("ja", "note.via_publish", { path: "Translations/ja/strings.csv" });
  await expect(notes.filter({ hasText: viaPublish })).toHaveCount(1);
  await expect(notes.filter({ hasText: msg("ja", "note.working_read", { path: workingRel }) })).toHaveCount(1);
  await expect(notes.filter({ hasText: msg("ja", "note.no_source") })).toHaveCount(0);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
  await expect(notes.filter({ hasText: msg("ja", "note.no_source") })).toHaveCount(1);
  await expect(
    notes.filter({ hasText: msg("ja", "note.working_none", { path: "Translations/_discovered/he.working.csv" }) }),
  ).toHaveCount(1);
  // he は入力と出力が同じファイル。publish を回せと言う理由が無い。
  await expect(notes.filter({ hasText: viaPublish })).toHaveCount(0);
  await expect(notes.filter({ hasText: msg("ja", "note.via_publish", { path: heRel }) })).toHaveCount(0);
});

// 数えたものは待ち受けが数えた値で、画面は並べるだけ（「判断を1つも持たない」）。ロケールを
// 替えたら描き直す。前のロケールの数が残ると、別のファイルの行数を読むことになる。
test("数えたものを『名前: 数』で並べ、ロケールを替えると描き直す", async ({ app }) => {
  const stats = app.locator("#stats li");
  const line = (key, value) => `${msg("ja", key)}: ${value}`;
  await expect(stats.filter({ hasText: line("stats.file_lines", 8) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.data_lines", 3) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.working_rows", 3) })).toHaveCount(1);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
  await expect(stats.filter({ hasText: line("stats.file_lines", 5) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.data_lines", 1) })).toHaveCount(1);
  // he に作業コピーは無い。作業コピーの行数を出すと、無いファイルを数えたことになる。
  await expect(stats.filter({ hasText: msg("ja", "stats.working_rows") })).toHaveCount(0);
});

test.describe("ヘッダーを受理できない作業コピー", () => {
  test.use({ repo: sampleRepo({ workingCopy: "a,b,c\n1,2,3\n" }) });

  // ヘッダーが読めないファイルの最終フィールドは訳とは限らない。書き換えさせると、訳でない
  // 列を壊す。理由はファイル全体の断り書きと行の両方に出す。出さないと、押しても開かない
  // 欄が黙って並ぶ。生の行はそのまま出す（直せなくても読めるようにする）。
  test("読み取り専用の理由を出し、行を編集させず、ファイルを1バイトも変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    const why = msg("ja", "reason.edit_bad_header", { line: 1, text: '"a,b,c"' });

    await expect(app.locator("#notes li").filter({ hasText: msg("ja", "ui.file_readonly", { reason: why }) })).toHaveCount(1);
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));

    const row = rowByLine(app, 2);
    await expect(row).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(row.locator(".cell.raw")).toHaveText("1,2,3");
    const note = row.locator(".cell.row-note");
    await expect(note).toBeVisible();
    await expect(note).toHaveText(msg("ja", "ui.not_editable", { reason: why }));
    await expect(note.locator('use[href="#i-lock"]')).toHaveCount(1);
    // 焦点を受ける欄が1つも無い（Tab でも入れない）。
    await expect(app.locator("#list [data-line]")).toHaveCount(0);
    await expect(app.locator("#list [tabindex]")).toHaveCount(0);

    await row.locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});

test.describe("読み直し", () => {
  // 読み直しは、よそ（ゲームの Export、別の窓の publish）が書き換えたファイルを見るための
  // 手段である。未保存が無いのに尋ねると、毎回「消えます」と脅されて押しにくくなる。
  test("未保存が無ければ尋ねずに、よそが書き換えたファイルの中身を並べ直す", async ({ app, server }) => {
    const dialogs = watchDialogs(app, false);
    const changed = "やあ";
    const text = await server.readRootText(workingRel);
    await server.writeRoot(workingRel, text.replace(`,${SAMPLE.hello.ja}\n`, `,${changed}\n`));

    const read = app.waitForResponse((res) => new URL(res.url()).pathname === "/api/lines");
    await app.locator("#reload").click();
    expect((await read).status()).toBe(200);
    await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText(changed);
    await expect(app.locator("#message")).toBeEmpty();
    expect(dialogs).toHaveLength(0);
  });

  // 読み直しは同じロケールを見続ける操作なので、翻訳者が決めた条件と検索語は残す
  // （boot の注記「読み直し（el.reload）では外さない」）。外すと、絞った一覧で作業して
  // いた人が、読み直すたびに1721行の先頭へ戻される。
  test("条件と検索語を外さずに当て直す", async ({ app }) => {
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    const read = app.waitForResponse((res) => new URL(res.url()).pathname === "/api/lines");
    await app.locator("#reload").click();
    await read;
    await expect(app.locator("#search")).toHaveValue(SAMPLE.goodbye.speaker);
    await expect(untranslated).toBeChecked();
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeVisible();
    await expect(rowByLine(app, SAMPLE_LINES.hello)).toBeHidden();
  });

  // 読み込みの失敗は、翻訳者にとって「読み直せばよい」一時の失敗である。そのとき一覧を
  // 消すと、直していた行も見ていた位置も失う。ファイルが戻れば、同じボタンで戻れること。
  test("失敗しても一覧を残して理由を出し、ファイルが戻れば読み直せる", async ({ app, server }) => {
    const bytes = await server.readRoot(workingRel);
    await rm(server.rootPath(workingRel));
    await app.locator("#reload").click();
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));

    await server.writeRoot(workingRel, bytes);
    await app.locator("#reload").click();
    await expect(app.locator("#message")).toBeEmpty();
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
  });
});

test.describe("ロケールの切り替え", () => {
  // 前のロケールで決めた条件と検索語を持ち越すと、ヘッダーは「行: N」と言うのに一覧が
  // 空になる（boot の注記）。he の行には "Ryan" が当たらないので、外れていなければ 0 行になる。
  test("読めたら、前のロケールの条件と検索語を外す", async ({ app }) => {
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#search")).toHaveValue("");
    await expect(app.locator('#filters input[type="checkbox"]:checked')).toHaveCount(0);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, 4).locator(".cell.translation")).toHaveText(SAMPLE.hello.he);
  });

  // 失敗したときに条件だけ外すと、チップと一覧が前のロケールのまま、欄だけが新しい
  // ロケールを指す三者バラバラの画面になった（load の注記。実際に起きた）。失敗したら
  // 何も変わっていないのが正しい。
  test("読めなければ、選択も一覧も条件も検索語も前のロケールのまま残す", async ({ app, server }) => {
    const dialogs = watchDialogs(app, false);
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await rm(server.rootPath(heRel));
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(app.locator("#search")).toHaveValue(SAMPLE.goodbye.speaker);
    await expect(untranslated).toBeChecked();
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeVisible();
    expect(dialogs).toHaveLength(0);
  });
});

test.describe("保存できていない訳があるとき", () => {
  // 切り替えと読み直しは、保存できていない訳を捨てる（load が state.pending / failed を
  // 空にする）。だから必ず尋ね、断られたら何1つ変えない（confirmDiscard）。欄だけ
  // 新しいロケールを指したり、要求が1つでも出たりすると、断ったのに訳が消える道になる。
  test("ロケールを切り替える前に尋ね、断れば何も変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await app.locator("#locale").selectOption("he");
    await expect.poll(() => dialogs.length).toBe(1);
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
    expect(lines).toHaveLength(0);
    // 待ち受けは行ごとに断ったので、ファイルは起動したときのまま。
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 読み直しも同じロケールの行を描き直すので、抱えている訳を同じように捨てる。尋ね方が
  // 切り替えと違えば、片方の道だけ黙って捨てることになる。
  test("読み直す前に尋ね、断れば何も変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
    expect(lines).toHaveLength(0);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 受けたときに捨てるのは、人が選んだ結果である（黙って破棄したのではない）。捨てたあとは
  // ファイルの中身を出し、保存の欄も「保存済み」に戻る。ファイルは1バイトも変わらない。
  test("尋ねて受けたときだけ、抱えていた訳を捨ててファイルの中身を並べ直す", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, true);

    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(SAMPLE.goodbye.ja);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).not.toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 受けても読めなかったら、何も変わっていないのが正しい（load の注記）。捨てるのは読めた
  // ときだけで、読めないまま訳を消すと、ファイルにも画面にも訳が無くなる。
  test("尋ねて受けても読み直しに失敗したら、抱えていた訳を捨てない", async ({ app, server }) => {
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, true);
    await app.route(isPath("/api/lines"), (route) => route.fulfill({ status: 500, body: "boom" }));

    await app.locator("#reload").click();
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    expect(dialogs).toHaveLength(1);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
  });

  // 欄を開いたまま読み直しを押すと、欄から離れた時点で保存が走る（blur）。その返事を
  // 待たずに尋ねるので、断ったあとに返ってきた保存はそのまま画面へ載らなければならない。
  // 載らないと、訳はファイルに入ったのに画面は「未保存」のまま、という食い違いになる。
  test("送りかけの訳があるときに読み直しを断っても、その訳はファイルに入る", async ({ app, server }) => {
    const before = await server.readRootText(workingRel);
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    await app.route(isPath("/api/rows"), async (route) => {
      await gate;
      await route.continue();
    });
    const dialogs = watchDialogs(app, false);

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    release();

    await waitForSaved(app);
    const after = await server.readRootText(workingRel);
    expect(after).toBe(before.replace(`,${SAMPLE.goodbye.source},\n`, `,${SAMPLE.goodbye.source},${typed}\n`));
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
  });
});

test.describe("保存を送り直しているとき", () => {
  // 送り直しは途中で諦めない（app.js の retryDelays、doc.go「訳を失わない」）。読み直しに
  // 失敗したら何も変わっていないのが正しい（load の注記）。以前は load が送り直しの時計と
  // saveError を読む前に片付け、読めなかったときに戻さなかった。訳は「未保存 1 件」と出たまま
  // どこへも送られず、原因（ゲームがファイルを開いている）が消えてもファイルに入らなかった。
  test("読み直しを受けたが読めなかったとき、送り直しを続けて訳をファイルへ入れる", async ({ page, server }) => {
    await page.clock.install();
    await openApp(page, server);

    // 待ち受けが書けない状態（Windows の共有違反で返る 503 と同じ形）。
    const rows = isPath("/api/rows");
    await page.route(rows, (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
      }),
    );
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    const dialogs = watchDialogs(page, true);
    await page.route(isPath("/api/lines"), (route) => route.fulfill({ status: 500, body: "boom" }), { times: 1 });
    await page.locator("#reload").click();
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    expect(dialogs).toHaveLength(1);
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);
    // 読めなかったので何も変わっていない。保存できていないことも出たまま。
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 原因が消えた。送り直しの間隔の最大（30 秒）を超えるぶん時計を進める。
    await page.unroute(rows);
    await page.clock.runFor(60_000);
    await expect
      .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\n`), { timeout: 5_000 })
      .toBe(true);
    await waitForSaved(page);
  });
});
