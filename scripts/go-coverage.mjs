// Go のカバレッジを集計し、package.json の閾値を下回っていたら失敗させる。
//
// 使い方:
//   node scripts/go-coverage.mjs <カバープロファイル>   集計して閾値と比べる（CI）
//   node scripts/go-coverage.mjs --run                  テストを走らせてから同じことをする（手元）
//
// 閾値は package.json の coverageThresholds.go.statements にある。
// 値は CI（Linux、元リポジトリ無し）での実測から、少し余裕を引いたものにしてある。
// 手元では元リポジトリを読むテストも走るので、CI より少し高く出る。
//
// 集計は go tool cover -func と同じ数え方をする（文の数で重みを付け、1回でも
// 通った塊を「通った」とする）。go tool cover を呼ばずに自分で読むのは、
// パッケージごとの表を GitHub の要約に出したいからである。-func は関数ごとの
// 表しか出さない。
import { spawnSync } from "node:child_process";
import { appendFileSync, mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const modulePrefix = "github.com/223n/dragnwash-localization-editor/";
const defaultProfile = join(root, "coverage", "go", "cover.out");

// runTests は手元用。CI と同じ対象を、同じく -count=1 で走らせる。
function runTests(profile) {
  mkdirSync(dirname(profile), { recursive: true });
  const result = spawnSync(
    "go",
    ["test", "./cmd/...", "./internal/...", "-count=1", `-coverprofile=${profile}`],
    { cwd: root, stdio: "inherit" },
  );
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    // テストが落ちたときは集計しない。落ちたテストのぶん数字が下がり、
    // 「カバレッジ不足」と誤った理由で止まって見えるからである。
    process.exit(result.status ?? 1);
  }
}

// readProfile はカバープロファイルを読み、パッケージごとの文の数を返す。
//
// 行の形は「ファイル:開始行.列,終了行.列 文の数 回数」。同じ塊が複数回出る
// ことがある（プロファイルを連結したとき）ので、塊ごとにまとめてから数える。
function readProfile(profile) {
  const blocks = new Map();
  const lines = readFileSync(profile, "utf8").split(/\r?\n/);
  for (const line of lines.slice(1)) {
    if (line === "") {
      continue;
    }
    const match = /^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$/.exec(line);
    if (!match) {
      throw new Error(`カバープロファイルの行を読めません: ${line}`);
    }
    const [, file, range, stmts, count] = match;
    const key = `${file}:${range}`;
    const hit = Number(count) > 0;
    const seen = blocks.get(key);
    if (seen) {
      seen.hit ||= hit;
      continue;
    }
    const pkg = dirname(file).replaceAll("\\", "/").replace(modulePrefix, "");
    blocks.set(key, { pkg, stmts: Number(stmts), hit });
  }

  const packages = new Map();
  for (const { pkg, stmts, hit } of blocks.values()) {
    const sum = packages.get(pkg) ?? { total: 0, covered: 0 };
    sum.total += stmts;
    if (hit) {
      sum.covered += stmts;
    }
    packages.set(pkg, sum);
  }
  return packages;
}

// percent は go tool cover と同じく小数1桁に丸める。比べるのも丸めた値にする。
// 画面に 88.0% と出ているのに 88.0% の閾値で落ちる、ということが起きないように。
function percent({ total, covered }) {
  return total === 0 ? 100 : Math.round((covered / total) * 1000) / 10;
}

function readThreshold() {
  const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  const value = pkg.coverageThresholds?.go?.statements;
  if (typeof value !== "number") {
    throw new Error("package.json に coverageThresholds.go.statements がありません");
  }
  return value;
}

function main(args) {
  let profile = args[0];
  if (profile === "--run") {
    profile = defaultProfile;
    runTests(profile);
  }
  if (!profile) {
    console.error("使い方: node scripts/go-coverage.mjs <カバープロファイル> | --run");
    process.exit(2);
  }

  const packages = readProfile(profile);
  const names = [...packages.keys()].sort();
  const all = { total: 0, covered: 0 };
  for (const sum of packages.values()) {
    all.total += sum.total;
    all.covered += sum.covered;
  }
  const total = percent(all);
  const threshold = readThreshold();
  const ok = total >= threshold;

  const width = Math.max(...names.map((name) => name.length));
  for (const name of names) {
    console.log(`${name.padEnd(width)}  ${percent(packages.get(name)).toFixed(1).padStart(5)}%`);
  }
  // 「合計」は全角2文字で半角4文字ぶんの幅を取るので、その差だけ詰める。
  console.log(`${"合計".padEnd(width - 2)}  ${total.toFixed(1).padStart(5)}%（閾値 ${threshold.toFixed(1)}%）`);

  const summary = process.env.GITHUB_STEP_SUMMARY;
  if (summary) {
    const rows = names.map((name) => `| \`${name}\` | ${percent(packages.get(name)).toFixed(1)}% |`);
    appendFileSync(summary, [
      "### Go のカバレッジ",
      "",
      "| パッケージ | 文 |",
      "| ---- | ---: |",
      ...rows,
      `| **合計** | **${total.toFixed(1)}%**（閾値 ${threshold.toFixed(1)}%） |`,
      "",
      "",
    ].join("\n"));
  }

  if (!ok) {
    console.error(`Go のカバレッジが閾値を下回りました: ${total.toFixed(1)}% < ${threshold.toFixed(1)}%`);
    process.exit(1);
  }
}

main(process.argv.slice(2));
