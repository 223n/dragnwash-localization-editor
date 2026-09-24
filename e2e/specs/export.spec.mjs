// 書き出し（上の帯の「別に保存」）が、押した時点の訳をそのままの形で渡すことを見る。
//
// 書き出しは、待ち受けがファイルを読んで返したものを、ブラウザーへ「これを保存して」と
// 渡すだけの道である（app.js の exportCsv、internal/web の export.go）。保存先を決めるのは
// ブラウザーで、待ち受けは1バイトも書かない。画面が守ることは3つある。
//
//   先に送りきる   未保存の訳を先に送ってから取りにいく。送り切れなければ書き出さず、
//                  押し直してもらう（ui.export_wait）。送っていない訳は、待ち受けが
//                  読むファイルに入っていないので、書き出したものにも入らない。
//                  送っても片付かない訳（競合・保存できない行・行き先の無い訳）が
//                  残っていても書き出さず、何を片付ければよいかを言う。
//   そのまま渡す   working はいま書き込んでいるファイルのバイトそのまま、published は
//                  dwloc publish が書くのと同じバイト。どちらもリポジトリは書き換えない。
//   黙らない       守り（失われる訳、巻き戻り）に当たったら理由を帯の中（#export-state）に出す。
//                  逆に、人が保存ダイアログを閉じたときは「書き出しました」と言わない。
//
// 中身は画面の表示ではなく、ブラウザーへ渡ったバイトで見る。headless の Chromium では
// 本物の保存ダイアログを出せないので、showSaveFilePicker は addInitScript で差し替える
// （スタブ）か消す（<a download> の道）。どちらも goto より前に仕込むので、app ではなく
// page + server + openApp を使う。
import { execFile } from "node:child_process";
import { cp, mkdtemp, readdir, readFile, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, relative, sep } from "node:path";
import { promisify } from "node:util";

import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { binaryPath } from "../support/dwloc.mjs";
import {
  BOM,
  SAMPLE,
  SAMPLE_LINES,
  field,
  keyFor,
  publishedFile,
  sampleRepo,
  sampleWorkingCopy,
  scriptOrder,
  workingCopy,
} from "../support/repo.mjs";
import { editor, openApp, saveState, typeTranslation, waitForSaved } from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";
const publishedRel = "Translations/ja/strings.csv";

// 見本はわざと BOM 付きで、行ごとに LF と CRLF を混ぜる（autosave.spec.mjs と同じ）。
// 「編集中のファイルのまま」がバイトを1つも変えずに渡すことを、この2つで見分ける。
// 添字 0 がヘッダーなので、偶数の物理行（6行目の goodbye など）が CRLF になる。
const mixedEol = (i) => (i % 2 === 1 ? "\r\n" : "\n");

test.use({
  repo: sampleRepo({ workingCopy: sampleWorkingCopy({ bom: true, eol: mixedEol }) }),
});

// #export-state の「うまくいかなかった」の印と、「時間で消える」の印（app.js の setExportState）。
const BAD = /(^|\s)error(\s|$)/;
const TIMED = /(^|\s)timed(\s|$)/;

// wrongKey はファイルに無いキー。保存の要求をこれに差し替えると、待ち受けは
// 「この行はずれています」で書かずに断る（行ごとの失敗を本物の応答で作る）。
const wrongKey = keyFor("この原文は見本のどこにも無い");

// ---- 保存ダイアログの差し替え ----

// stubPicker は showSaveFilePicker を差し替える。goto より前に呼ぶこと。
//
// mode は呼ばれた時点の window.__picker.mode で決まるので、途中で setPickerMode で変えられる。
//   save   保存する。渡った中身（バイト列）と、閉じた回数を控える
//   abort  人がダイアログを閉じた（AbortError）
//   deny   ダイアログを出せなかった（人の操作から続いていないと見なされた、など）
//   broken 選んだ先へ書けなかった（createWritable が断る。書き込み先の権限が無い、など）
async function stubPicker(page, mode = "save") {
  await page.addInitScript((initial) => {
    const log = { mode: initial, calls: [], written: [], closed: 0 };
    window.__picker = log;
    window.showSaveFilePicker = function (options) {
      log.calls.push(JSON.parse(JSON.stringify(options ?? null)));
      if (log.mode === "abort") {
        return Promise.reject(new DOMException("The user aborted a request.", "AbortError"));
      }
      if (log.mode === "deny") {
        return Promise.reject(
          new DOMException("Must be handling a user gesture to show a file picker.", "SecurityError"),
        );
      }
      return Promise.resolve({
        createWritable() {
          if (log.mode === "broken") {
            return Promise.reject(new DOMException("The request is not allowed by the user agent.", "NotAllowedError"));
          }
          return Promise.resolve({
            write(blob) {
              return blob.arrayBuffer().then((buf) => {
                log.written.push(Array.from(new Uint8Array(buf)));
              });
            },
            close() {
              log.closed += 1;
              return Promise.resolve();
            },
          });
        },
      });
    };
  }, mode);
}

// setPickerMode はスタブの振る舞いを途中で変える。
async function setPickerMode(page, mode) {
  await page.evaluate((next) => {
    window.__picker.mode = next;
  }, mode);
}

// pickerLog はスタブが控えたものを返す。written は Buffer の並びにする。
async function pickerLog(page) {
  const log = await page.evaluate(() => window.__picker);
  return { ...log, written: log.written.map((bytes) => Buffer.from(bytes)) };
}

// removePicker は showSaveFilePicker を消す。対応していないブラウザーの道（<a download>）になる。
async function removePicker(page) {
  await page.addInitScript(() => {
    delete window.showSaveFilePicker;
    if (window.showSaveFilePicker) {
      Object.defineProperty(window, "showSaveFilePicker", { value: undefined, configurable: true });
    }
  });
}

// ---- 画面の操作 ----

// openExport は帯の「別に保存」を押して、2つの選び方のメニュー（popover）を開く。
// ボタンは目録が入ると出る（app.js の applyCatalog）。
async function openExport(page) {
  await exportOpen(page).click();
  await expect(page.locator("#export-published")).toBeVisible();
  await expect(page.locator("#export-working")).toBeVisible();
}

function exportOpen(page) {
  return page.locator("#export-open");
}

function exportMenuIsOpen(page) {
  return page.locator("#export-menu").evaluate((menu) => menu.matches(":popover-open"));
}

// choose は書き出しの形を選ぶ。選ぶとメニューは閉じる（app.js の closeExportMenu）ので、
// 閉じていれば開き直してから押す。
async function choose(page, form) {
  if (!(await exportMenuIsOpen(page))) {
    await openExport(page);
  }
  await (form === "working" ? workingButton(page) : publishedButton(page)).click();
}

function exportState(page) {
  return page.locator("#export-state");
}

function publishedButton(page) {
  return page.locator("#export-published");
}

function workingButton(page) {
  return page.locator("#export-working");
}

// waitExportSettled は書き出しの1回が終わるまで待つ。ボタンは押した瞬間に disabled に
// なり（app.js の updateExportButtons）、終わると戻る。押したあとで呼ぶこと。
async function waitExportSettled(page) {
  await expect(publishedButton(page)).toBeEnabled();
  await expect(workingButton(page)).toBeEnabled();
}

// watchApi は、頁から出た要求のうち /api/ で始まるものを、方法・パス・問い合わせの形で控える。
// 呼んだあとに出た要求だけが入る。
function watchApi(page) {
  const seen = [];
  page.on("request", (req) => {
    const url = new URL(req.url());
    if (url.pathname.startsWith("/api/")) {
      seen.push({ method: req.method(), path: url.pathname, search: url.search, url });
    }
  });
  return seen;
}

// exportsOf は控えた要求のうち、書き出しを取りにいったものだけを返す。
function exportsOf(seen) {
  return seen.filter((r) => r.path === "/api/export");
}

// gate は route の手を止めておくための栓。release を呼ぶまで開かない。
function gate() {
  let release;
  const promise = new Promise((resolve) => {
    release = resolve;
  });
  return { promise, release };
}

// openPaused は時計を差し替えてから画面を開き、開き終えたところで時計を止める
// （save-failure.spec.mjs と同じ）。止めておけば、保存に失敗したあとの送り直しの時計が
// 試験の手より先に走らない。送るのは、書き出しが先に送る flush だけになる。
async function openPaused(page, server) {
  await page.clock.install();
  await openApp(page, server);
  const now = await page.evaluate(() => Date.now());
  await page.clock.pauseAt(now + 1_000);
}

// ---- ファイルの比べ方 ----

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

// withTranslation は物理行 n の最終フィールドを value に差し替えた中身を返す。
// 見本の訳にはカンマも引用符も無いので、最後のカンマから行末までを差し替えれば足りる。
function withTranslation(buf, n, value) {
  const lines = splitLines(buf);
  lines[n - 1] = Buffer.from(
    lines[n - 1].toString("utf8").replace(/,[^,\r\n]*(\r?\n)?$/, (whole, eol) => `,${field(value)}${eol ?? ""}`),
    "utf8",
  );
  return Buffer.concat(lines);
}

// snapshot は dir の下のファイルを全部読んで「相対パス（/ 区切り）→ バイト列」にする。
async function snapshot(dir) {
  const files = new Map();
  for (const entry of await readdir(dir, { recursive: true, withFileTypes: true })) {
    if (!entry.isFile()) {
      continue;
    }
    const full = join(entry.parentPath, entry.name);
    files.set(relative(dir, full).split(sep).join("/"), await readFile(full));
  }
  return files;
}

// expectSameTree は2つの snapshot が、ファイルの顔ぶれも中身も1バイトも違わないことを確かめる。
function expectSameTree(before, after) {
  expect([...after.keys()].sort()).toEqual([...before.keys()].sort());
  for (const [rel, body] of before) {
    expect(after.get(rel).equals(body), `${rel} が変わった`).toBe(true);
  }
}

// publishWithCli は、server のリポジトリを写した先で dwloc publish を走らせ、
// 書かれた ja の公開ファイルのバイト列を返す。
//
// 写すのは、待ち受けが開いているリポジトリを publish に書き換えさせないためである。
// 作業ディレクトリも一時ディレクトリにする（dwloc は logs/ をカレントに作る）。
async function publishWithCli(server) {
  const dir = await realpath(await mkdtemp(join(tmpdir(), "dwloc-e2e-publish-")));
  try {
    const root = join(dir, "repo");
    await cp(server.root, root, { recursive: true });
    await promisify(execFile)(binaryPath(), ["publish", "--root", root, "--no-game", "--locale", "ja"], {
      cwd: dir,
      windowsHide: true,
    });
    return await readFile(join(root, publishedRel));
  } finally {
    await rm(dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
}

// ---- 試験 ----

test.describe("保存先を尋ねられるブラウザー（showSaveFilePicker あり）", () => {
  // 「編集中のファイルのまま」は、いま書き込んでいるファイルを写す道である。組み立て直すと、
  // BOM や行ごとの改行が揃えられ、翻訳者が「そのままの形で欲しい」と選んだものと別物になる。
  // 名前も待ち受けの付けた名前（作業コピーの名前）を保存ダイアログに渡す。
  test("編集中のファイルのまま保存すると、作業コピーのバイトをそのまま渡す", async ({ page, server }) => {
    await stubPicker(page);
    const seen = watchApi(page);
    await openApp(page, server);
    const before = await snapshot(server.root);
    const working = before.get(workingRel);
    // 見本が BOM と CRLF を持っていること（持っていなければ、この試験は何も見分けない）。
    expect(working.subarray(0, 3).equals(Buffer.from(BOM, "utf8"))).toBe(true);
    expect(working.includes("\r\n")).toBe(true);

    await openExport(page);
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    await expect(exportState(page)).not.toHaveClass(BAD);

    const log = await pickerLog(page);
    expect(log.calls).toEqual([{ suggestedName: "ja.working.csv", types: [{ accept: { "text/csv": [".csv"] } }] }]);
    expect(log.written).toHaveLength(1);
    expect(log.written[0].equals(working), "渡した中身が作業コピーのバイトと違う").toBe(true);
    expect(log.closed).toBe(1);

    // 待ち受けへ渡すのはロケールと形の2つだけ。保存先のパスはどこにも載らない。
    expect(exportsOf(seen).map((r) => [...r.url.searchParams])).toEqual([
      [
        ["locale", "ja"],
        ["form", "working"],
      ],
    ]);
    // 書き出しは待ち受けに1バイトも書かせない。
    expectSameTree(before, await snapshot(server.root));
  });

  // 「公開ファイルの形」は dwloc publish と同じものでなければならない（README「画面で訳を
  // 書き換える」、export.go）。違うと、翻訳者が保存したものをリポジトリへ置いたときに、
  // publish を回した結果と食い違う差分がコミットに混ざる。直したばかりの訳も入っていること、
  // そのうえでリポジトリの公開ファイルは書き換えていないことを見る。
  test("公開ファイルの形で保存すると、dwloc publish が書くのと同じバイトを渡し、リポジトリは書き換えない", async ({
    page,
    server,
  }) => {
    const typed = "さようなら。";
    await stubPicker(page);
    await openApp(page, server);
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect
      .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\r\n`), { timeout: 10_000 })
      .toBe(true);
    await waitForSaved(page);

    const expected = await publishWithCli(server);
    const before = await snapshot(server.root);
    // 打った訳は、まだコミット済みの公開ファイルには無い。publish の出力にだけある。
    expect(before.get(publishedRel).includes(typed)).toBe(false);
    expect(expected.includes(typed)).toBe(true);

    await openExport(page);
    await choose(page, "published");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));

    const log = await pickerLog(page);
    expect(log.calls.map((c) => c.suggestedName)).toEqual(["strings.csv"]);
    expect(log.written).toHaveLength(1);
    expect(log.written[0].equals(expected), "dwloc publish の出力と違う").toBe(true);
    expectSameTree(before, await snapshot(server.root));
  });

  // 人が保存ダイアログを閉じたのに「書き出しました」と言えば、どこにも無いファイルを
  // 探しにいかせることになる（app.js の saveCsv）。前の書き出しの「書き出しました」が
  // 残ったままになるのも同じなので、先に1度書き出してから閉じる。閉じたときに
  // いつもの落とし先へ勝手に落とすこともしない。
  test("保存ダイアログを閉じたときは、書き出したと言わず、ほかの場所にも落とさない", async ({ page, server }) => {
    await stubPicker(page);
    const downloads = [];
    page.on("download", (d) => downloads.push(d));
    await openApp(page, server);
    await openExport(page);

    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));

    await setPickerMode(page, "abort");
    await choose(page, "working");
    await expect.poll(async () => (await pickerLog(page)).calls.length).toBe(2);
    await waitExportSettled(page);
    expect(await exportState(page).textContent()).toBe("");
    await expect(exportState(page)).not.toHaveClass(BAD);
    expect((await pickerLog(page)).written).toHaveLength(1);
    expect(downloads).toHaveLength(0);
  });

  // ダイアログを出せなかったとき（押してから中身が届くまでに、人の操作から続いていると
  // 見なされる時間が切れた、など）は、何も保存されないより、いつもの落とし先へ入れる
  // ほうがよい（app.js の saveCsv）。落としたものの名前と中身も同じであること。
  test("保存ダイアログを出せなかったときは、いつもの落とし先へ同じ中身を落とす", async ({ page, server }) => {
    await stubPicker(page, "deny");
    await openApp(page, server);
    await openExport(page);

    const downloaded = page.waitForEvent("download");
    await choose(page, "working");
    const download = await downloaded;
    expect(download.suggestedFilename()).toBe("ja.working.csv");
    const body = await readFile(await download.path());
    expect(body.equals(await server.readRoot(workingRel)), "落とした中身が作業コピーと違う").toBe(true);
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    expect((await pickerLog(page)).calls).toHaveLength(1);
  });
});

test.describe("保存先を尋ねられないブラウザー（showSaveFilePicker なし）", () => {
  // 対応していないブラウザーでは、いつものダウンロード先へ入る（README）。ここでも
  // 名前は待ち受けの付けた名前で、中身はファイルのバイトそのままであること。
  test("<a download> でいつもの落とし先へ落とし、名前と中身はファイルのまま", async ({ page, server }) => {
    await removePicker(page);
    await openApp(page, server);
    expect(await page.evaluate(() => typeof window.showSaveFilePicker)).toBe("undefined");
    await openExport(page);

    const downloaded = page.waitForEvent("download");
    await choose(page, "working");
    const download = await downloaded;
    expect(download.suggestedFilename()).toBe("ja.working.csv");
    const body = await readFile(await download.path());
    expect(body.equals(await server.readRoot(workingRel)), "落とした中身が作業コピーと違う").toBe(true);
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    // 落とすために足したリンクは片付ける。残すと、押すたびに頁にリンクが溜まる。
    await expect(page.locator("a[download]")).toHaveCount(0);
  });

  // 落とすために作った中身（blob の URL）は、すぐ剥がすと保存が始まる前に消える実装が
  // ある。かといって持ち続けると、押すたびに書き出しの中身が頁の中に溜まる。
  // 落とした直後はまだ読め、時間が経てば手放していること（app.js の downloadCsv）。
  // 待つ長さそのものは約束ではないので、十分に長く進めて確かめる。
  //
  // 頁の CSP（connect-src 'self'）が blob: への fetch を止めるので、読めるかどうかでは
  // 見られない。手放したかは URL.revokeObjectURL の呼び出しを控えて見る。
  test("落とした中身はすぐには手放さず、時間が経てば手放す", async ({ page, server }) => {
    await removePicker(page);
    await page.addInitScript(() => {
      const revoked = [];
      window.__revoked = revoked;
      const original = URL.revokeObjectURL.bind(URL);
      URL.revokeObjectURL = (url) => {
        revoked.push(url);
        original(url);
      };
    });
    await page.clock.install();
    await openApp(page, server);
    await openExport(page);

    const downloaded = page.waitForEvent("download");
    await choose(page, "working");
    const download = await downloaded;
    const url = download.url();
    expect(url.startsWith("blob:"), `blob の URL で落としていない: ${url}`).toBe(true);
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    expect(await page.evaluate(() => window.__revoked)).toEqual([]);

    await page.clock.runFor(10 * 60_000);
    expect(await page.evaluate(() => window.__revoked)).toEqual([url]);
  });
});

test.describe("未保存の訳があるとき", () => {
  // いちばん大事な約束。待ち受けはファイルを読んで書き出すので、送っていない訳は
  // 書き出したものに入らない。押した時点の画面と書き出したものが違うのは、この道具が
  // いちばんやってはいけないこと（app.js の exportCsv）。だから押したら先に送りきる。
  // 保存が1度落ちて未保存のまま抱えている訳で、送る → 取りにいく の順を見る。
  test("押すと、未保存の訳を先に送りきってから書き出す", async ({ page, server }) => {
    const typed = "さようなら。";
    await stubPicker(page);
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    let down = true;
    await page.route("**/api/rows", (route) => (down ? route.abort("connectionrefused") : route.continue()));

    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    // 届かなかった。訳は未保存のまま画面が抱えていて、ファイルにはまだ無い。
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    down = false;
    const seen = watchApi(page);
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    expect(seen.map((r) => `${r.method} ${r.path}`)).toEqual(["POST /api/rows", "GET /api/export"]);

    // 送った訳がファイルに入り、触っていない行は1バイトも変わっていない。
    const after = await server.readRoot(workingRel);
    const want = withTranslation(before, SAMPLE_LINES.goodbye, typed);
    expect(after.equals(want), "作業コピーが期待と違う").toBe(true);
    // 書き出したものは、送ったあとのファイルそのもの。
    const log = await pickerLog(page);
    expect(log.written).toHaveLength(1);
    expect(log.written[0].equals(after), "書き出したものに送った訳が入っていない").toBe(true);
    await waitForSaved(page);
  });

  // 訳を打った直後にボタンを押すと、欄から離れた時点で保存が走り出す（blur の flush）。
  // その返りを待たずに取りにいくと、送っている最中の訳が入らない古い中身を渡すことになる。
  // 待って勝手に書き出す作りにもしない（押したのに何も起きない時間ができる）。
  // 押し直してもらうよう伝え、押し直せば送った訳ごと書き出せる。
  test("送っている最中に押したときは、取りにいかずに押し直すよう伝える", async ({ page, server }) => {
    const typed = "さようなら。";
    await stubPicker(page);
    await openApp(page, server);
    const seen = watchApi(page);
    const hold = gate();
    await page.route("**/api/rows", async (route) => {
      await hold.promise;
      await route.continue().catch(() => {});
    });

    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    // ボタンを押すと焦点が移り、欄から離れたところで保存が走り出す（まだ返らない）。
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_wait"));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect(exportsOf(seen)).toHaveLength(0);
    expect((await pickerLog(page)).calls).toHaveLength(0);

    hold.release();
    await waitForSaved(page);
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    const log = await pickerLog(page);
    expect(log.written).toHaveLength(1);
    const disk = await server.readRoot(workingRel);
    expect(disk.includes(`,${typed}\r\n`)).toBe(true);
    expect(log.written[0].equals(disk), "押し直したのに送った訳が入っていない").toBe(true);
  });

  // 送れなかった（待ち受けに届かない）ときに書き出すと、打った訳の入らない古い中身を
  // 「書き出しました」と渡すことになる。書き出さずに伝え、訳は未保存のまま抱え続ける。
  test("送れなかったときは書き出さず、訳は未保存のまま抱える", async ({ page, server }) => {
    const typed = "さようなら。";
    await stubPicker(page);
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    await page.route("**/api/rows", (route) => route.abort("connectionrefused"));

    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    const seen = watchApi(page);
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_wait"));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    // 書き出しの前にもう1度送ろうとはした。取りにはいっていない。
    expect(seen.map((r) => `${r.method} ${r.path}`)).toEqual(["POST /api/rows"]);
    expect((await pickerLog(page)).calls).toHaveLength(0);

    // 訳は画面に残り、保存できていないことも出たまま。ファイルは1バイトも変わらない。
    await expect(page.locator(`#list .cell.translation[data-line="${SAMPLE_LINES.goodbye}"]`)).toHaveText(typed);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 書き出しの前に送った訳が 409（手前でよそがファイルを変えた）で返ったとき、打った訳は
  // ファイルに入っていない。画面は競合を出して「どちらを載せるか」を人に選ばせる
  // （app.js 冒頭「黙って上書きも、黙って破棄もしない」）。その選ぶ前に、よその訳のまま
  // の中身を「書き出しました」と渡せば、書き出したものの中では黙って破棄したのと同じになる。
  test("書き出す前の保存が競合したときは、選ぶ前のファイルを書き出さない", async ({ page, server }) => {
    const typed = "さようなら。";
    const theirs = "またね。";
    await stubPicker(page);
    const downloads = [];
    page.on("download", (d) => downloads.push(d));
    await openPaused(page, server);
    let down = true;
    await page.route("**/api/rows", (route) => (down ? route.abort("connectionrefused") : route.continue()));

    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 未保存を抱えているあいだに、よそ（ゲームや別の画面）が同じ行を書き換える。
    await server.writeRoot(workingRel, withTranslation(await server.readRoot(workingRel), SAMPLE_LINES.goodbye, theirs));
    down = false;

    await choose(page, "working");
    await expect(page.locator("#conflict")).toBeVisible();
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls, "競合を選ぶ前に中身を渡した").toHaveLength(0);
    expect(downloads).toHaveLength(0);
    // 「送っています」とは言わない。競合のあいだは何も送っていない。選んでから押し直してもらう。
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_conflict"));
    await expect(exportState(page)).toHaveClass(BAD);

    // 競合が出たままもう一度押しても、書き出さない。
    await choose(page, "working");
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls, "競合が出たまま押し直したら中身を渡した").toHaveLength(0);
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_conflict"));
  });

  // 行ごとに断られた訳（保存できない行）はファイルに入っていない。画面は値ごと抱え、
  // 自動保存の対象からだけ外している（app.js の state.failed）。そのまま取りにいくと、
  // 画面に出ているその訳の入らない中身を「書き出しました」と渡す（hasUnsaved が未保存に
  // 数えるのと同じ考えで止める）。送り直しでは片付かないので「送っています」とも言わない。
  // 片付けば、同じボタンで書き出せる。
  test("保存できない行が残っているときは書き出さずに理由を出し、片付けば書き出す", async ({ page, server }) => {
    const typed = "さようなら。";
    await stubPicker(page);
    await openPaused(page, server);
    const seen = watchApi(page);
    // 要求のキーをファイルに無いものへ差し替え、本物の待ち受けに行ごとに断らせる。
    await page.route("**/api/rows", async (route) => {
      const body = JSON.parse(route.request().postData() ?? "{}");
      body.edits = (body.edits ?? []).map((edit) => ({ ...edit, key: wrongKey }));
      await route.continue({ postData: JSON.stringify(body) });
    });

    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
    const before = await server.readRoot(workingRel);

    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_row_failed"));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect(exportsOf(seen)).toHaveLength(0);
    expect((await pickerLog(page)).calls).toHaveLength(0);
    // 訳は画面に残り、ファイルは1バイトも変わっていない。
    await expect(page.locator(`#list .cell.translation[data-line="${SAMPLE_LINES.goodbye}"]`)).toHaveText(typed);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // 元の訳（空）へ戻すと、保存できない行ではなくなる。そうすれば書き出せる。
    await page.unroute("**/api/rows");
    await typeTranslation(page, SAMPLE_LINES.goodbye, SAMPLE.goodbye.ja);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    const log = await pickerLog(page);
    expect(log.written).toHaveLength(1);
    expect(log.written[0].equals(await server.readRoot(workingRel))).toBe(true);
  });

  // 行き先の無い訳（409 の読み直しで、載せる行がファイルから無くなった訳）もファイルに
  // 入っていない。画面のその欄が訳の残っている最後の場所で、書き出したものには無い。
  // 保存できない行と同じく、書き出さずに何をすればよいかを言う。
  test("行き先の無い訳が残っているときは書き出さずに理由を出す", async ({ page, server }) => {
    await stubPicker(page);
    // 時計を止める。止めないと、打ってから「よその書き換え」を挟むまでに自動保存が走り、
    // 409 にならずに保存される。
    await openPaused(page, server);
    const seen = watchApi(page);

    await typeTranslation(page, SAMPLE_LINES.goodbye, "さようなら。");
    // 送る前に、よそが goodbye の行ごと消した。
    const external = workingCopy([
      "",
      "# ===== Level 1: Ryan (Sunny) =====",
      "# --- intro: Ryan_1_intro ---",
      { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
      { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
      "",
    ]);
    await server.writeRoot(workingRel, external);
    const saving = page.waitForResponse(
      (res) => new URL(res.url()).pathname === "/api/rows" && res.request().method() === "POST",
    );
    await editor(page).press("Escape");
    expect((await saving).status()).toBe(409);
    await expect(page.locator("#orphans")).toBeVisible();
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));

    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_orphans"));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect(exportsOf(seen)).toHaveLength(0);
    expect((await pickerLog(page)).calls).toHaveLength(0);
    // 行き先の無い訳は出たまま。
    await expect(page.locator("#orphans-list li")).toHaveCount(1);
  });
});

test("書き出しているあいだは、2つのボタンを押せない", async ({ page, server }) => {
  // 二重に押すと、同じ中身の保存ダイアログが2つ出たり、片方の結果の文が
  // もう片方の結果で上書きされたりする。終わるまで押させない（app.js の state.exporting）。
  await stubPicker(page);
  await openApp(page, server);
  const hold = gate();
  const exports = [];
  await page.route(
    (url) => url.pathname === "/api/export",
    async (route) => {
      exports.push(route.request().url());
      await hold.promise;
      await route.continue().catch(() => {});
    },
  );
  await openExport(page);

  await choose(page, "published");
  await expect(publishedButton(page)).toBeDisabled();
  await expect(workingButton(page)).toBeDisabled();
  await expect.poll(() => exports.length).toBe(1);

  hold.release();
  await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
  await waitExportSettled(page);
  expect(exports).toHaveLength(1);
  expect((await pickerLog(page)).calls).toHaveLength(1);
});

test.describe("コミット済みの訳が作業コピーに無いとき", () => {
  // 作業コピーに wonderful が無い。publish の形にするとコミット済みの「すばらしい！」が
  // 新しい出力に残らない。止める場所が画面に無いと、publish が止める中身を翻訳者が
  // 自分の手でリポジトリへ写せてしまう（export.go の exportPublished）。
  test.use({
    repo: sampleRepo({
      workingCopy: workingCopy([
        "",
        "# ===== Level 1: Ryan (Sunny) =====",
        "# --- intro: Ryan_1_intro ---",
        { ...SAMPLE.hello, translation: SAMPLE.hello.ja },
        { ...SAMPLE.goodbye, translation: SAMPLE.goodbye.ja },
        "",
      ]),
    }),
  });

  test("公開ファイルの形は書き出さずに理由を出し、編集中のファイルのままなら書き出せる", async ({
    page,
    server,
  }) => {
    await stubPicker(page);
    const downloads = [];
    page.on("download", (d) => downloads.push(d));
    await openApp(page, server);
    await openExport(page);

    await choose(page, "published");
    await expect(exportState(page)).toHaveText(msg("ja", "error.export_would_lose", { count: 1 }));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls).toHaveLength(0);
    expect(downloads).toHaveLength(0);

    // 止めるのは publish の形のときだけ。いま編集しているファイルはそのまま出せ、
    // 前の理由は次の書き出しで消える（残すと、成功したのに失敗の文が出たままになる）。
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    await expect(exportState(page)).not.toHaveClass(BAD);
    const log = await pickerLog(page);
    expect(log.calls.map((c) => c.suggestedName)).toEqual(["ja.working.csv"]);
    expect(log.written[0].equals(await server.readRoot(workingRel))).toBe(true);
  });
});

// 読むと訳や原文を取り違える形。publish は書く前に形を見て止め（internal/publish の
// shape.go）、画面の書き出しも同じ確かめを同じ順で通る（export.go）。画面には、確かめた
// うえで通す指定（dwloc publish --accept-multiline）を置かない。どのファイルの何行目かは
// パスを含むので出さず、件数と直し方の案内（dwloc publish）だけを出す。
const shapeCases = (() => {
  const s = SAMPLE;
  const head = ["", "# ===== Level 1: Ryan (Sunny) =====", "# --- intro: Ryan_1_intro ---"];
  const row = (item, translation) =>
    [keyFor(item.source), "L01 Ryan", "Ryan_1_intro", item.order, item.speaker, item.source, translation]
      .map(field)
      .join(",");
  // 閉じ忘れた引用符が、キーの形で始まる次の行（goodbye）を訳に飲み込む。
  const swallow = workingCopy([
    ...head,
    row(s.hello, "").replace(/,$/, ',"もしもし'),
    row(s.goodbye, "さようなら") + '"',
    { ...s.wonderful, translation: s.wonderful.ja },
    "",
  ]);
  // 訳の中の単独の CR。上流の道具はこの行をあとで落とす。
  const loneCR = workingCopy([
    ...head,
    { ...s.hello, translation: "もし\rもし" },
    { ...s.goodbye, translation: "" },
    { ...s.wonderful, translation: s.wonderful.ja },
    "",
  ]);
  // 再生順の見出し（'# --- … ---'）に使う値の改行。書き出すと見出しの2行目がデータの行になる。
  const order = sampleRepo();
  order.root["data/script_order.csv"] = scriptOrder([{ ...s.hello, node: "Ryan_1\nintro" }, s.goodbye, s.wonderful]);
  return [
    { name: "作業コピーで引用符が別の行で閉じ、次の行を飲み込むとき", repo: sampleRepo({ workingCopy: swallow }) },
    { name: "作業コピーの訳に単独の CR があるとき", repo: sampleRepo({ workingCopy: loneCR }) },
    { name: "再生順の見出しに使う値に改行があるとき", repo: order },
  ];
})();

for (const { name, repo } of shapeCases) {
  test.describe(name, () => {
    test.use({ repo });

    test("公開ファイルの形は書き出さずに件数と案内を出し、リポジトリを書き換えない", async ({ page, server }) => {
      await stubPicker(page);
      const downloads = [];
      page.on("download", (d) => downloads.push(d));
      await openApp(page, server);
      const before = await snapshot(server.root);
      await openExport(page);

      await choose(page, "published");
      const text = msg("ja", "error.export_unsafe_shape", { count: 1 });
      await expect(exportState(page)).toHaveText(text);
      await expect(exportState(page)).toHaveClass(BAD);
      // 画面に通す指定は無い。正しい複数行の値なら dwloc publish の指定で書く、と案内する。
      expect(text).toContain("dwloc publish --accept-multiline");
      await waitExportSettled(page);
      expect((await pickerLog(page)).calls).toHaveLength(0);
      expect(downloads).toHaveLength(0);
      expectSameTree(before, await snapshot(server.root));

      // 止めるのは publish の形だけで、いま編集しているファイルはそのまま出せる。
      await choose(page, "working");
      await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
      const log = await pickerLog(page);
      expect(log.written[0].equals(await server.readRoot(workingRel))).toBe(true);
    });
  });
}

test.describe("ゲームに入っている翻訳がコミット済みより古いとき", () => {
  // ゲーム側の公開ファイル（作業コピーの土台）の hello が古い訳のまま。その上に建つ
  // 作業コピーを publish の形にすると、訳は消えないまま古い版へ巻き戻る。失われる訳の
  // 守りでは捕まらないので、土台の食い違いとして先に止める（export.go、publish と同じ順）。
  const repo = sampleRepo({ game: true });
  repo.game["Translations/ja/strings.csv"] = publishedFile([
    "",
    "# --- intro: Ryan_1_intro ---",
    { ...SAMPLE.hello, translation: "もしもし" },
    { ...SAMPLE.wonderful, translation: SAMPLE.wonderful.ja },
    "",
  ]);
  test.use({ repo });

  test("公開ファイルの形は巻き戻るので書き出さず、理由を出す", async ({ page, server }) => {
    await stubPicker(page);
    await openApp(page, server);
    const before = await snapshot(server.root);
    await openExport(page);

    await choose(page, "published");
    await expect(exportState(page)).toHaveText(msg("ja", "error.export_would_roll_back", { count: 1 }));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls).toHaveLength(0);
    expectSameTree(before, await snapshot(server.root));
  });
});

test.describe("書き出しを取りにいけなかったとき", () => {
  // 待ち受けの理由の本文が空でも、黙らずに書き出せなかったと言う（app.js の fetchCsv）。
  // 本文だけを当てにすると、ボタンの下が空のまま、何も起きなかったように見える。
  test("理由の本文が空でも、書き出せなかったと目録の文で伝える", async ({ page, server }) => {
    await stubPicker(page);
    await openApp(page, server);
    await page.route(
      (url) => url.pathname === "/api/export",
      (route) => route.fulfill({ status: 500, contentType: "text/plain; charset=utf-8", body: "" }),
    );
    await openExport(page);

    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_failed"));
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls).toHaveLength(0);
  });

  // 画面は文言を1つも持たず、すべて目録から出す（app.js 冒頭）。待ち受けに届かなかった
  // ときに、ブラウザーの英語の誤り（"Failed to fetch"）をそのまま出すと、ja の画面に
  // 目録に無い英文が出る。目録の「書き出せませんでした。」で伝えるのが筋である。
  test("待ち受けに届かなかったときも、目録の文で書き出せなかったと伝える", async ({ page, server }) => {
    await stubPicker(page);
    await openApp(page, server);
    await page.route(
      (url) => url.pathname === "/api/export",
      (route) => route.abort("connectionrefused"),
    );
    await openExport(page);

    await choose(page, "working");
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls).toHaveLength(0);
    expect(await exportState(page).textContent()).toBe(msg("ja", "ui.export_failed"));
  });

  // 中身は届いたが、保存ダイアログで選んだ先へ書けなかった。ここで出るのもブラウザーの
  // 誤り（DOMException の英文）なので、上と同じく目録の文で伝える。「書き出しました」とは言わない。
  test("選んだ先へ書けなかったときも、目録の文で書き出せなかったと伝える", async ({ page, server }) => {
    await stubPicker(page, "broken");
    await openApp(page, server);
    await openExport(page);

    await choose(page, "working");
    await expect(exportState(page)).toHaveClass(BAD);
    await waitExportSettled(page);
    const log = await pickerLog(page);
    expect(log.calls).toHaveLength(1);
    expect(log.written).toHaveLength(0);
    expect(await exportState(page).textContent()).toBe(msg("ja", "ui.export_failed"));
  });
});

// 応答の文字列をそのままファイル名にしない（app.js の nameFromDisposition）。待ち受けが
// 付ける名前は [A-Za-z0-9._-] だけなので、当てるのもその形に限り、外れたら strings.csv に
// する。パスの区切りや引用符の混ざった名前が保存ダイアログへ渡る道を作らない。
//
// 字の種類が通っても、先頭がドットの名前（".."、".csv"）と ".." を含む名前は落とす。
// 待ち受けの exportName と同じ規則である。字の種類だけで見ていたころは ".." がそのまま
// 保存ダイアログへ渡った。
test("応答の名前が決めた形でなければ、strings.csv の名前で保存させる", async ({ page, server }) => {
  const cases = [
    // 通す字だけの名前はそのまま（数字・ハイフン・ドット・下線）。
    { header: 'attachment; filename="pt-BR_v2.working.csv"', want: "pt-BR_v2.working.csv" },
    { header: null, want: "strings.csv" },
    { header: 'attachment; filename="../evil.csv"', want: "strings.csv" },
    { header: 'attachment; filename="..\\evil.csv"', want: "strings.csv" },
    { header: 'attachment; filename="a b.csv"', want: "strings.csv" },
    { header: 'attachment; filename="日本語.csv"', want: "strings.csv" },
    { header: 'attachment; filename=".."', want: "strings.csv" },
    { header: 'attachment; filename="."', want: "strings.csv" },
    { header: 'attachment; filename=".csv"', want: "strings.csv" },
    { header: 'attachment; filename="a..b.csv"', want: "strings.csv" },
  ];
  await stubPicker(page);
  await openApp(page, server);
  let current = null;
  await page.route(
    (url) => url.pathname === "/api/export",
    (route) => {
      const headers = { "Content-Type": "text/csv; charset=utf-8" };
      if (current.header !== null) {
        headers["Content-Disposition"] = current.header;
      }
      return route.fulfill({ status: 200, headers, body: "key,section,node,order,speaker,translation\n" });
    },
  );
  await openExport(page);

  for (const [i, c] of cases.entries()) {
    current = c;
    await choose(page, "working");
    await expect.poll(async () => (await pickerLog(page)).calls.length).toBe(i + 1);
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    await waitExportSettled(page);
    expect((await pickerLog(page)).calls[i].suggestedName, `Content-Disposition: ${c.header}`).toBe(c.want);
  }
});

test.describe("ロケールを選ぶ前", () => {
  test.use({ dwloc: { locale: "" } });

  // ロケールを決めていないうちは、書き出すものが無い。取りにいけば、ロケールの無い要求が
  // 待ち受けに断られるだけになる。選んだあとは、いま開いているロケールのファイルを出す。
  // he には作業コピーが無いので、出るのは公開ファイル自身（README の表）。
  test("押しても取りにいかず、選んだあとはそのロケールのファイルを書き出す", async ({ page, server }) => {
    await stubPicker(page);
    const seen = watchApi(page);
    await openApp(page, server);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.select_locale"));
    await openExport(page);

    await choose(page, "working");
    await waitExportSettled(page);
    expect(await exportState(page).textContent()).toBe("");

    await page.locator("#locale").selectOption("he");
    await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));

    // 取りにいったのは選んだあとの1回だけ。
    expect(exportsOf(seen).map((r) => r.search)).toEqual(["?locale=he&form=working"]);
    const log = await pickerLog(page);
    expect(log.calls.map((c) => c.suggestedName)).toEqual(["strings.csv"]);
    expect(log.written[0].equals(await server.readRoot("Translations/he/strings.csv"))).toBe(true);
  });
});

// 書き出しの入口は帯のボタン1つで、2つの選び方はそれが開くメニュー（popover）にある。
// メニューは最前面の層に開くので、開いたままだと一覧の行を覆う。選んだら閉じ、焦点は
// 開いた帯のボタンへ戻す（app.js の closeExportMenu）。結果はメニューの外、帯の中の
// #export-state に出す。閉じたメニューの中身は支援技術の木から外れるので、中に出すと
// 見えも告知されもしない。
//
// うまくいった知らせは、帯の下の残り時間が尽きたら消す。貼り付く帯に居座ると、その
// ぶん一覧が狭くなるためである。失敗の理由は消さない。読み終わる前に消えると、何が
// 起きたかを知るすべが無くなる（app.js の setExportState と animationend）。
test.describe("帯のメニューと結果の知らせ", () => {
  test("選ぶとメニューを閉じて焦点を帯のボタンへ戻し、うまくいった知らせは時間で消える", async ({
    page,
    server,
  }) => {
    await stubPicker(page);
    await openApp(page, server);
    await openExport(page);

    await workingButton(page).click();
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_done"));
    expect(await exportMenuIsOpen(page)).toBe(false);
    await expect(exportOpen(page)).toBeFocused();
    await expect(exportState(page)).toHaveClass(TIMED);
    await expect(exportState(page)).not.toHaveClass(BAD);

    // 残り時間の帯（CSS のアニメーション）が尽きたら消える。
    await expect(exportState(page)).toBeEmpty({ timeout: 15_000 });
    await expect(exportState(page)).not.toHaveClass(TIMED);
    // 消えたのは知らせだけで、書き出した中身はそのまま渡っている。
    expect((await pickerLog(page)).written).toHaveLength(1);
  });

  test("失敗の理由は時間で消さない", async ({ page, server }) => {
    await stubPicker(page);
    await openApp(page, server);
    await page.route(
      (url) => url.pathname === "/api/export",
      (route) => route.fulfill({ status: 500, contentType: "text/plain; charset=utf-8", body: "" }),
    );

    await choose(page, "working");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_failed"));
    await expect(exportState(page)).toHaveClass(BAD);
    await expect(exportState(page)).not.toHaveClass(TIMED);

    // 残り時間の帯は付かないが、何かのアニメーションの終わりが届いても消さない。
    await exportState(page).dispatchEvent("animationend");
    await expect(exportState(page)).toHaveText(msg("ja", "ui.export_failed"));
    await expect(exportState(page)).toHaveClass(BAD);
  });
});
