// 画面が開いて、ファイルの行がそのまま並ぶことを見る。ハーネスの動作確認も兼ねる。
//
// 画面はファイルの写しである（internal/web の doc.go「画面に新しい判断を置かない」）。
// 並びも見出しも、ファイルにあるとおりに出なければならない。ここが崩れると、
// 翻訳者は自分がどのファイルのどの行を直しているのかを取り違える。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, SAMPLE_LINES, sampleRepo } from "../support/repo.mjs";
import { dataRows, headings, rowByLine, saveState } from "../support/ui.mjs";

test("起動すると、作業コピーの行がファイルの順に並ぶ", async ({ app }) => {
  // 行数は待ち受けが数えたもの。画面は数えない（linesResponse.Rows）。
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 3 }));

  // 見出しはファイルのコメント行を書き換えずに写す。組み直すと、ファイルに無い
  // 見出しが出たり、翻訳者が書いたメモが消えたりする。
  await expect(headings(app)).toHaveText([
    "# ===== Level 1: Ryan (Sunny) =====",
    "# --- intro: Ryan_1_intro ---",
  ]);

  // 並びは物理行の順。番号は空行とヘッダー行のぶんも数えたファイルの行番号で、
  // 保存の要求はこの番号で行を指す。
  await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);

  // 作業コピーを読めたので、原文の欄が埋まる。
  const hello = rowByLine(app, SAMPLE_LINES.hello);
  await expect(hello.locator(".cell.speaker")).toHaveText(SAMPLE.hello.speaker);
  await expect(hello.locator(".cell.source")).toHaveText(SAMPLE.hello.source);
  await expect(hello.locator(".cell.translation")).toHaveText(SAMPLE.hello.ja);

  // どのファイルを読み書きしているかは、畳んだままでも読める場所に出る。
  // リポジトリの作業コピーを直しているのか、公開ファイルを直しているのかは、ここにしか出ない。
  await expect(app.locator("#file-path")).toHaveText(
    `${msg("ja", "ui.file")}: Translations/_discovered/ja.working.csv`,
  );
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
});

test.describe("ロケールを省いて起動したとき", () => {
  test.use({ dwloc: { locale: "" } });

  test("選ぶまで行を出さず、選ぶとそのロケールの行が並ぶ", async ({ app }) => {
    // --locale を省くのは既定の経路（ダブルクリックで開いたときもこれ）。
    // 選ぶ前に行を出すと、どのロケールかを決めていない一覧を直させることになる。
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.select_locale"));
    await expect(dataRows(app)).toHaveCount(0);

    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
    // he には作業コピーが無いので、並べているのは公開ファイルそのもの。
    await expect(app.locator("#file-path")).toHaveText(`${msg("ja", "ui.file")}: Translations/he/strings.csv`);
    await expect(rowByLine(app, 4).locator(".cell.translation")).toHaveText(SAMPLE.hello.he);
  });
});

test.describe("ゲームのフォルダーを使うとき", () => {
  test.use({ repo: sampleRepo({ game: true }) });

  test("ゲーム側の作業コピーを並べ、探し先を画面に出す", async ({ app, server }) => {
    // ゲーム側の作業コピーはリポジトリの外にあるので、パスは絶対パスのまま出る。
    const working = server.gamePath("Translations/_discovered/ja.working.csv").replaceAll("\\", "/");
    await expect(app.locator("#file-path")).toHaveText(`${msg("ja", "ui.file")}: ${working}`);
    await expect(app.locator("#game-path")).toHaveText(
      `${msg("ja", "ui.game_folder")}: ${server.game.replaceAll("\\", "/")}`,
    );
    await expect(rowByLine(app, SAMPLE_LINES.hello).locator(".cell.source")).toHaveText(SAMPLE.hello.source);
  });
});
