// 画面を操作する小さな道具。セレクターを試験ごとに書き散らさないためにある。
//
// 画面の骨組みは internal/web/ui/index.html、行の組み立ては app.js の rowNode にある。
// 行は div の並びで、1行が1レコード（引用符で囲んだ値に改行があるレコードも1行）。
// 行（.row）は ID（待ち受けの lineView の id、セグメントの通し番号）を data-id に持ち、
// 訳の欄（.cell.translation）も、編集できる行にだけ同じ data-id を持つ。
// 行番号の欄（.cell.num）には、最初の物理行（.num-start）と、行をまたぐレコードだけ
// 最後の物理行（.num-end、「〜M」）が入る。
// 入力欄は頁に1つだけで、焦点が入った行へ差し込まれる（textarea.editor）。
//
// 行を引く道具は、ID で引くもの（rowById、translationCell など）と、行番号で引くもの
// （rowByLine）がある。どのレコードも1物理行に収まる見本では、ID は物理行の番号と
// 同じになる（ヘッダー・空行・コメント行もセグメントとして1つずつ番号を取るため）。
// 行をまたぐレコードがある見本では、その後ろで ID と行番号がずれる。
import { expect } from "@playwright/test";

// openApp はトークン付きの URL を開き、画面が組み上がるまで待つ。
//
// 待つのは2つ。目録が届いて保存の状態が描かれたこと、ロケールを指定して起動した
// ときは行が描かれたこと（#rows は render のあとでしか埋まらない）。
export async function openApp(page, server) {
  await page.goto(server.url);
  await expect(page.locator("#save-state")).not.toBeEmpty();
  if (server.options.locale) {
    await expect(page.locator("#rows")).not.toBeEmpty();
  }
}

// dataRows は一覧のデータ行（見出しを除く）。
export function dataRows(page) {
  return page.locator("#list .row");
}

// headings は一覧の見出し。
export function headings(page) {
  return page.locator("#list .heading");
}

// rowById は ID が id の行。編集できない行も引ける。
export function rowById(page, id) {
  return page.locator(`#list .row[data-id="${id}"]`);
}

// rowByLine は最初の物理行が n の行。編集できない行も引ける（行番号の欄で引く）。
// 行をまたぐレコードは、その最初の物理行で引ける。途中の物理行では引けない。
export function rowByLine(page, n) {
  return page.locator("#list .row").filter({
    has: page.locator(".cell.num > .num-start", { hasText: new RegExp(`^${n}$`) }),
  });
}

// translationCell は ID が id の行の訳の欄（編集できる行だけ）。
export function translationCell(page, id) {
  return page.locator(`#list .cell.translation[data-id="${id}"]`);
}

// editor は差し込まれている入力欄。
export function editor(page) {
  return page.locator("#list textarea.editor");
}

// saveState は上の帯の保存の状態。
export function saveState(page) {
  return page.locator("#save-state");
}

// openEditor は ID が id の行の訳の欄を押して入力欄を開く。
export async function openEditor(page, id) {
  await translationCell(page, id).click();
  await expect(editor(page)).toBeFocused();
}

// typeTranslation は ID が id の行の入力欄を開き、訳を value に置き換える。
//
// fill は入力欄の中身を選んでから差し替え、input を1回だけ投げる。1字ずつ打つ
// （pressSequentially）と打鍵ごとに自動保存の時計が引き直される。どちらを使うかは
// 見たい振る舞いで決めること。閉じない（blur しない）ので、保存は自動保存の
// 時計か、呼び出し側の操作（Enter、Tab、ほかの場所を押す）で走る。
export async function typeTranslation(page, id, value) {
  await openEditor(page, id);
  await editor(page).fill(value);
}

// waitForSaved は保存の状態が「保存済み」になるまで待つ。
//
// 入力の直後は「未保存」、送っているあいだは「保存しています」になるので、
// 入力のあとで呼べば、その保存が返るまで待つことになる。
export async function waitForSaved(page) {
  await expect(saveState(page)).toHaveClass(/(^|\s)clean(\s|$)/);
}
