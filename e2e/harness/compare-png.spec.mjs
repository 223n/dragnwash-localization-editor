// 画面の例の撮り直し（samples/screenshots.mjs）が、揺れだけの画像を書かないための比べ方
// （samples/compare-png.mjs）を見る。
//
// 画面の試験ではないが、node で動く台本の試験を置ける場所がここしかないので、試験の
// 仕組みの試験と一緒に置く。dwloc の画面は開かず、カバレッジの生データも書かない。
// ブラウザーを使うのは、コミット済みの画像の読み取りを Chromium と比べる試験だけで、
// 空の頁で画像を読ませる。
//
// 見本の画像は、揺れの形（線が1画素下がって白い行が消える、文字の列が1画素上がる、
// 色が1だけ違う）を小さく写して作る。撮り直しで実際に見た形である。
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { deflateSync } from "node:zlib";

import { expect, test } from "@playwright/test";

import { root } from "../support/paths.mjs";

const { comparePictures, decodePng, judgeRetake, MAX_SHIFTED_RATIO } = await import(
  new URL("../../samples/compare-png.mjs", import.meta.url).href
);

const BG = [248, 249, 250, 255];
const LINE = [222, 226, 230, 255];
const WHITE = [255, 255, 255, 255];
const HEAD = [232, 238, 247, 255];
const INK = [60, 64, 67, 255];

// picture は行ごとの色の一覧から画像を作る。rows[y] は1行の色か、画素ごとの色の配列。
function picture(width, rows) {
  const data = new Uint8Array(width * rows.length * 4);
  rows.forEach((row, y) => {
    for (let x = 0; x < width; x++) {
      const color = Array.isArray(row[0]) ? row[x] : row;
      data.set(color, (y * width + x) * 4);
    }
  });
  return { width, height: rows.length, data };
}

// withPixels は image を写し、指定した画素だけ色を変えたものを返す。
function withPixels(image, changes) {
  const data = new Uint8Array(image.data);
  for (const [x, y, color] of changes) {
    data.set(color, (y * image.width + x) * 4);
  }
  return { ...image, data };
}

// scene は、rows の縞（上から順の色）を 120×120 の背景に置いた画像を作る。縞の幅は
// 10画素。揺れとみなす上限（MAX_SHIFTED_RATIO）は画像全体に対する割合なので、実物と
// 同じく、変わる所が画像のごく一部になるようにする。
const SCENE = 120;
const BAND = 10;
function scene(rows) {
  const changes = [];
  rows.forEach((color, i) => {
    for (let x = 0; x < BAND; x++) {
      changes.push([x, 40 + i, color]);
    }
  });
  return withPixels(picture(SCENE, Array(SCENE).fill(BG)), changes);
}

// 表の見出しのあたり。P は線（2画素）と見出しのあいだに白い1画素の行があり、
// Q は線が1画素下がって白い行が消えている。撮り直しで両方が出る。
const headerP = scene([BG, BG, LINE, LINE, WHITE, HEAD, HEAD, HEAD]);
const headerQ = scene([BG, BG, BG, LINE, LINE, HEAD, HEAD, HEAD]);

// 左の列の文字。縦に濃淡のある列を、1画素上へずらしたもの。
const glyph = [WHITE, WHITE, [234, 235, 236, 255], [220, 222, 223, 255], [200, 202, 205, 255], INK, WHITE, WHITE];
const textBand = scene(glyph);
const textBandUp = scene([...glyph.slice(1), WHITE]);

// crc32 は PNG の塊の CRC を求める。読む側は CRC を確かめないが、正しい PNG を作っておく。
function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let k = 0; k < 8; k++) {
      crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1;
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const head = Buffer.alloc(8);
  head.writeUInt32BE(data.length, 0);
  head.write(type, 4, "latin1");
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])), 0);
  return Buffer.concat([head, data, crc]);
}

// encodePng は画像を PNG にする。colorType は 0（グレー）、2（RGB）、4（グレーと透明度）、
// 6（RGBA）。filters[y] で行ごとのフィルター（0〜4）を選ぶ。読む側の5つのフィルターを
// 全部通すためである。
function encodePng(image, { colorType = 6, filters = [], bitDepth = 8, interlace = 0 } = {}) {
  const channels = { 0: 1, 2: 3, 4: 2, 6: 4 }[colorType];
  const { width, height } = image;
  const stride = width * channels;
  const rows = [];
  for (let y = 0; y < height; y++) {
    const row = new Uint8Array(stride);
    for (let x = 0; x < width; x++) {
      const [r, g, b, a] = image.data.subarray((y * width + x) * 4, (y * width + x) * 4 + 4);
      const values = { 0: [r], 2: [r, g, b], 4: [r, a], 6: [r, g, b, a] }[colorType];
      row.set(values, x * channels);
    }
    rows.push(row);
  }
  const raw = [];
  rows.forEach((row, y) => {
    const filter = filters[y % filters.length] ?? 0;
    const prev = y > 0 ? rows[y - 1] : new Uint8Array(stride);
    const line = new Uint8Array(stride + 1);
    line[0] = filter;
    for (let x = 0; x < stride; x++) {
      const a = x >= channels ? row[x - channels] : 0;
      const b = prev[x];
      const c = x >= channels ? prev[x - channels] : 0;
      let predictor = 0;
      if (filter === 1) predictor = a;
      if (filter === 2) predictor = b;
      if (filter === 3) predictor = (a + b) >> 1;
      if (filter === 4) {
        const p = a + b - c;
        const [pa, pb, pc] = [Math.abs(p - a), Math.abs(p - b), Math.abs(p - c)];
        predictor = pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
      }
      line[x + 1] = (row[x] - predictor) & 0xff;
    }
    raw.push(line);
  });
  return pngFromScanlines({ width, height, colorType, bitDepth, interlace }, raw);
}

// pngFromScanlines は、フィルターを掛け終えた行（先頭の1バイトがフィルターの種類）を
// そのまま PNG の塊に包む。フィルターの計算を試験の側で持たずに、読む側を確かめるのに使う。
function pngFromScanlines({ width, height, colorType, bitDepth = 8, interlace = 0 }, scanlines) {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = bitDepth;
  ihdr[9] = colorType;
  ihdr[12] = interlace;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(Buffer.concat(scanlines.map((line) => Uint8Array.from(line))))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

// decodeInChromium は PNG を Chromium に読ませ、RGBA の並びを返す。
//
// 色の変換はさせない（colorSpaceConversion: "none"）。PNG に色の情報の塊があっても、
// decodePng と同じく値をそのまま読む。画面の例は不透明なので、canvas の乗算済みの
// 透明度で値が丸まることも無い。
async function decodeInChromium(page, bytes) {
  const { width, height, base64 } = await page.evaluate(async (input) => {
    const png = Uint8Array.from(atob(input), (c) => c.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([png], { type: "image/png" }), {
      colorSpaceConversion: "none",
      premultiplyAlpha: "none",
    });
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
    const context = canvas.getContext("2d", { willReadFrequently: true });
    context.drawImage(bitmap, 0, 0);
    const data = context.getImageData(0, 0, bitmap.width, bitmap.height).data;
    // btoa は文字列しか受けないので、少しずつ文字にする。一度に広げると引数が多すぎる。
    let text = "";
    for (let i = 0; i < data.length; i += 0x8000) {
      text += String.fromCharCode(...data.subarray(i, i + 0x8000));
    }
    return { width: bitmap.width, height: bitmap.height, base64: btoa(text) };
  }, bytes.toString("base64"));
  return { width, height, data: Buffer.from(base64, "base64") };
}

// differentBytes は2つのバイト列で値の違う位置の数を返す。4MB の画素を toEqual で
// 比べると、落ちたときの差分の表示が大きすぎて読めないので、数だけを見る。
function differentBytes(a, b) {
  let count = Math.abs(a.length - b.length);
  for (let i = 0; i < Math.min(a.length, b.length); i++) {
    if (a[i] !== b[i]) {
      count++;
    }
  }
  return count;
}

test.describe("PNG を読む", () => {
  // 実物の画像は、撮り直しの比べる相手そのものである。読み取りが化けると、化けた画素
  // どうしを比べて揺れかどうかを決めてしまう。下の往復の試験は試験の側の符号器と
  // 組なので、両方が同じ誤りを持つと気付けない。そこで、別の読み手である Chromium と
  // 画素まで同じに読めることを確かめる。決まったハッシュと比べる形にしないのは、画面を
  // 変えて撮り直すたびに、この試験まで直すことになるからである。
  const imagesDir = join(root, "docs", "images");
  const images = readdirSync(imagesDir).filter((name) => name.endsWith(".png"));

  test("画面の例の画像がある", () => {
    // 画像が無いと下の試験が1つも作られず、何も確かめないまま通ってしまう。
    expect(images.length).toBeGreaterThan(0);
  });

  for (const name of images) {
    test(`コミット済みの画面の例（${name}）を、Chromium と同じ画素に読む`, async ({ page }) => {
      const bytes = readFileSync(join(imagesDir, name));
      const ours = decodePng(bytes);
      const theirs = await decodeInChromium(page, bytes);
      expect([ours.width, ours.height]).toEqual([theirs.width, theirs.height]);
      expect(ours.data.length).toBe(ours.width * ours.height * 4);
      expect(differentBytes(ours.data, theirs.data), "Chromium の読み取りと値の違うバイトの数").toBe(0);
      expect(comparePictures(ours, ours)).toMatchObject({ jitterOnly: true, changed: 0, shifted: 0 });
    });
  }

  // Paeth の予測（PNG の仕様の 9.4）は、p = a + b - c に最も近いものを、左 a、上 b、
  // 左上 c から選ぶ。同点なら a、b、c の順に取る。同点の扱いを誤っても、同点の起きない
  // 見本では往復の試験が通る。誤った読み手で実物の画面の例を読むと、バイトの数%から
  // 十数%が化けた。そこで、フィルターを掛け終えた行を手で書き、試験の側の符号器を
  // 通さずに読ませる。
  //
  // 画像は 2×2 のグレー。右下の画素（値は 40）の予測だけが同点になる。2行目の左の
  // 画素は、左と左上が無い（0 とみなす）ので予測は上の c になり、a - c を書く。
  for (const [name, [c, b, a], predictor] of [
    // p = 10 + 25 - 20 = 15。pa = 5、pb = 10、pc = 5 で、a と c が同点なので a を取る。
    ["左と左上", [20, 25, 10], 10],
    // p = 25 + 10 - 20 = 15。pa = 10、pb = 5、pc = 5 で、b と c が同点なので b を取る。
    ["上と左上", [20, 10, 25], 10],
  ]) {
    test(`Paeth で${name}が同点なら、仕様の順に選ぶ`, () => {
      const png = pngFromScanlines({ width: 2, height: 2, colorType: 0 }, [
        [0, c, b],
        [4, (a - c) & 0xff, (40 - predictor) & 0xff],
      ]);
      const gray = (v) => [v, v, v, 255];
      expect(decodePng(png).data).toEqual(picture(2, [[gray(c), gray(b)], [gray(a), gray(40)]]).data);
    });
  }

  test("5つのフィルターと4つの色の種類を読み戻せる", () => {
    // 色が行と列で変わる画像にして、どのフィルターも前の画素と上の画素を使うようにする。
    const width = 7;
    const rows = Array.from({ length: 10 }, (_, y) =>
      Array.from({ length: width }, (_, x) => [(x * 37 + y * 11) % 256, (x * 5 + 200) % 256, (y * 29) % 256, 255]),
    );
    const image = picture(width, rows);
    const filters = [0, 1, 2, 3, 4];

    expect(decodePng(encodePng(image, { colorType: 6, filters })).data).toEqual(image.data);
    expect(decodePng(encodePng(image, { colorType: 2, filters })).data).toEqual(image.data);

    // グレーは赤のチャンネルの値を3つに広げ、透明度が無ければ 255 にする。
    const gray = picture(width, rows.map((row) => row.map(([r]) => [r, r, r, 255])));
    expect(decodePng(encodePng(image, { colorType: 0, filters })).data).toEqual(gray.data);
    const translucent = withPixels(image, [[0, 0, [9, 9, 9, 128]]]);
    const grayAlpha = withPixels(gray, [[0, 0, [9, 9, 9, 128]]]);
    expect(decodePng(encodePng(translucent, { colorType: 4, filters })).data).toEqual(grayAlpha.data);
  });

  // palette は色の種類をパレット（3）に書き換えた PNG。IHDR の色の種類は、署名（8）と
  // 塊の長さと種類（8）のあとの9バイト目にある。
  const palette = Buffer.from(encodePng(headerP));
  palette[8 + 8 + 9] = 3;

  for (const [name, bytes, message] of [
    ["PNG でないもの", Buffer.from("not a png"), /PNG ではありません/],
    // IHDR の塊（33バイト目まで）のあと、IDAT の塊の途中で切る。
    ["途中で切れたもの", encodePng(headerP).subarray(0, 45), /途中で切れています/],
    ["16ビットのもの", encodePng(headerP, { bitDepth: 16 }), /読めない形/],
    ["飛び越しのもの", encodePng(headerP, { interlace: 1 }), /読めない形/],
    ["パレットのもの", palette, /読めない形/],
  ]) {
    test(`${name}は例外にする`, () => {
      expect(() => decodePng(bytes)).toThrow(message);
    });
  }
});

test.describe("揺れの範囲", () => {
  test("見出しの線が1画素下がり、白い行が消えた画像は、どちら向きでも揺れとみなす", () => {
    // 片向きの確かめだけだと、Q から P へ戻る向き（白い行が現れる）を違う画素と数えてしまう。
    for (const [before, after] of [
      [headerP, headerQ],
      [headerQ, headerP],
    ]) {
      const result = comparePictures(before, after);
      expect(result).toMatchObject({ jitterOnly: true, changed: 0 });
      expect(result.shifted).toBeGreaterThan(0);
    }
  });

  test("文字の列が1画素上へずれた画像は、揺れとみなす", () => {
    expect(comparePictures(textBand, textBandUp)).toMatchObject({ jitterOnly: true, changed: 0 });
    expect(comparePictures(textBandUp, textBand)).toMatchObject({ jitterOnly: true, changed: 0 });
  });

  test("各チャンネルが1だけ違う画像は、揺れとみなす", () => {
    const off = picture(4, [[247, 250, 249, 255], [249, 248, 251, 254]]);
    const base = picture(4, [BG, BG]);
    expect(comparePictures(base, off)).toMatchObject({ jitterOnly: true, changed: 0, shifted: 0 });
  });

  test("高さ1画素の線が1画素下がっただけなら、揺れとみなす", () => {
    const before = scene([BG, BG, LINE, BG, BG, BG]);
    const after = scene([BG, BG, BG, LINE, BG, BG]);
    expect(comparePictures(before, after)).toMatchObject({ jitterOnly: true, changed: 0, shifted: 2 * BAND });
  });
});

test.describe("本物の変化", () => {
  test("1つの画素が2だけ違えば、違う画素と数える", () => {
    const base = picture(4, [BG, BG, BG]);
    const result = comparePictures(base, withPixels(base, [[1, 1, [250, 249, 250, 255]]]));
    expect(result).toMatchObject({ jitterOnly: false, changed: 1 });
  });

  test("2画素のずれは、揺れとみなさない", () => {
    const twoDown = scene([BG, BG, BG, BG, LINE, LINE, HEAD, HEAD]);
    const result = comparePictures(headerP, twoDown);
    expect(result.jitterOnly).toBe(false);
    expect(result.changed).toBeGreaterThan(0);
  });

  // 平らな背景では、足した画素も消えた画素も、上下の背景の画素と片向きには合う。
  // 近くに両向きのずれが無いので、揺れとは数えない。
  for (const [name, before, after] of [
    ["高さ1画素の横線が増えた", [BG, BG, BG, BG, BG], [BG, BG, LINE, BG, BG]],
    ["高さ1画素の横線が消えた", [BG, BG, LINE, BG, BG], [BG, BG, BG, BG, BG]],
    ["高さ2画素の横線が増えた", [BG, BG, BG, BG, BG, BG], [BG, BG, LINE, LINE, BG, BG]],
  ]) {
    test(`${name}だけでも、違う画素と数える`, () => {
      const result = comparePictures(scene(before), scene(after));
      expect(result.jitterOnly).toBe(false);
      expect(result.changed).toBeGreaterThanOrEqual(BAND);
    });
  }

  test("平らな背景に足した小さな四角は、違う画素と数える", () => {
    const base = picture(8, Array(8).fill(BG));
    const square = [];
    for (let y = 3; y < 6; y++) {
      for (let x = 3; x < 6; x++) {
        square.push([x, y, INK]);
      }
    }
    expect(comparePictures(base, withPixels(base, square))).toMatchObject({ jitterOnly: false, changed: 9 });
  });

  test("大きさが違えば、比べずに違うとみなす", () => {
    const result = comparePictures(headerP, picture(SCENE + 1, Array(SCENE).fill(BG)));
    expect(result).toMatchObject({ jitterOnly: false, changed: null, shifted: null });
    expect(result.reason).toContain("大きさが違います");
  });

  test("1画素のずれでも、上限を超える数なら揺れとみなさない", () => {
    // 行ごとに色の違う縞を丸ごと1画素下げる。一覧全体が下がるような本物の変化の形。
    const stripes = Array.from({ length: 40 }, (_, y) => (y % 2 ? LINE : [100 + y, 120, 140, 255]));
    const before = picture(50, stripes);
    const after = picture(50, [stripes[0], ...stripes.slice(0, -1)]);
    const result = comparePictures(before, after);
    expect(result.changed).toBe(0);
    expect(result.shifted).toBeGreaterThan(Math.floor(50 * 40 * MAX_SHIFTED_RATIO));
    expect(result.jitterOnly).toBe(false);
    expect(result.reason).toContain("上限");
  });
});

test.describe("撮り直した画像を書くか", () => {
  const before = encodePng(headerP);

  test("まだ画像が無ければ書く", () => {
    expect(judgeRetake(null, before)).toMatchObject({ write: true });
  });

  test("バイトまで同じなら書かない", () => {
    expect(judgeRetake(before, Buffer.from(before))).toMatchObject({ write: false, reason: "バイトまで同じです" });
  });

  test("揺れだけなら、バイトが違っても書かない", () => {
    // フィルターを変えると、同じ画素でもバイト列は変わる。
    expect(judgeRetake(before, encodePng(headerP, { filters: [4] }))).toMatchObject({ write: false });
    expect(judgeRetake(before, encodePng(headerQ))).toMatchObject({ write: false });
  });

  test("中身が変われば書く", () => {
    const changed = withPixels(headerP, [[5, 0, INK]]);
    const verdict = judgeRetake(before, encodePng(changed));
    expect(verdict.write).toBe(true);
    expect(verdict.reason).toContain("違う画素");
  });

  test("既にある画像を読めなければ、比べずに書く", () => {
    const verdict = judgeRetake(Buffer.from("broken"), before);
    expect(verdict.write).toBe(true);
    expect(verdict.reason).toContain("画素で比べられません");
  });
});
