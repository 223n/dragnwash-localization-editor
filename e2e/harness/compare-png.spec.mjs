// 画面の例の撮り直し（samples/screenshots.mjs）が、揺れだけの画像を書かないための比べ方
// （samples/compare-png.mjs）を見る。
//
// 画面の試験ではないが、node で動く台本の試験を置ける場所がここしかないので、試験の
// 仕組みの試験と一緒に置く。画面は開かず、生データも書かない。
//
// 見本の画像は、揺れの形（線が1画素下がって白い行が消える、文字の列が1画素上がる、
// 色が1だけ違う）を小さく写して作る。撮り直しで実際に見た形である。
import { readFileSync } from "node:fs";
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
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = bitDepth;
  ihdr[9] = colorType;
  ihdr[12] = interlace;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(Buffer.concat(raw))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

test.describe("PNG を読む", () => {
  test("コミット済みの画面の例を読める", () => {
    const image = decodePng(readFileSync(join(root, "docs", "images", "edit-overview-ja.png")));
    expect([image.width, image.height]).toEqual([1280, 800]);
    expect(image.data.length).toBe(1280 * 800 * 4);
    expect(comparePictures(image, image)).toMatchObject({ jitterOnly: true, changed: 0, shifted: 0 });
  });

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
