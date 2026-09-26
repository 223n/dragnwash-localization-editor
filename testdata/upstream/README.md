# 上流との突き合わせの正解

上流（翻訳リポジトリ）の`tools/hash-strings.ps1`と、dwlocの読み手と`publish`を突き合わせるための入力と正解です。
全体を解釈する読み手へ移す作業（PR0〜PR4）の土台にします。
英文と訳はどれも架空の文です。
ゲームの台本は含みません。

## 中身

| ファイル | 中身 |
| ---- | ---- |
| `cases.json` | 入力の表です。複数行の値、閉じない引用符、飲み込み、単独のCR、BOM、`,`だけの行、`#`で始まるヘッダーなどを並べます |
| `expected.json` | 上流に`cases.json`を読ませた結果です。`scripts/upstream-fixtures.ps1`が書きます。手では直しません |

`expected.json`には、1つの入力ごとに次の2つが入ります。

- `read`は、上流の`Read-Csv`と`Remove-NonRecords`だけを取り出して読ませた結果です。レコードの値と、元の物理行の範囲（`line`と`end`）を持ちます
- `run`は、一時ディレクトリに`tools/`・`data/`・`Translations/`を置き、上流のスクリプトを通しで走らせた結果です。書き出した`strings.csv`のバイト列（`output`）と、集計の1行の項目ごとの値（`summary`）を持ちます

物理行は`\r\n`・`\n`・`\r`で分けて数えます。
BOMは数えません。
エディターの行番号と同じです。
上流の関数は行番号を返さないので、入力の先頭から何行までを読ませるかを変えて、外から求めています。
求め方は`scripts/upstream-fixtures.ps1`の冒頭に書いてあります。

`expected.json`には、上流のコミット、`tools/hash-strings.ps1`のblobの名前、pwshの版、`cases.json`のSHA-256も記録します。
`cases.json`を変えたのに`expected.json`を作り直していなければ、Goの試験が落ちます。

## 使う試験

| 試験 | 比べるもの |
| ---- | ---- |
| `internal/csvfile/upstream_fixture_test.go` | 主の読み手（`ReadPowerShell`）、守り専用の`ReadPowerShellWhole`、行単位の読み手（`ReadPowerShellTable`）を`read`と比べます |
| `cmd/dwloc/publish_upstream_test.go` | `dwloc publish --no-game`の結果を`run`と比べます |

上流と違ってよい入力は、試験の表に理由と一緒に載せてあります。
表は、どう違うかまで固定します。
表に無い違いが出たとき、表の入力が上流と一致するようになったとき、違い方が変わったときは、どれも試験が落ちます。
表の分類は次のとおりです。

- 意図して違える：上流の不具合や、dwlocが止める形です。一覧は[移植仕様](../../docs/port-spec.md)の「上流と意図して違える点」にあります
- 守り専用の読み方：PR1まで`publish`の守りが使っていた`ReadPowerShellWhole`だけの違いです。主の読み手は上流とそろっています。PR2で守りが主の読み手へ移り、この読み手はどこからも使われていません
- 行単位の読み方：1物理行を1レコードとして読むことから来る違いです。PR2で`publish`・`diff`・`order`が主の読み手へ移り、行単位の読み手はどこからも使われていません
- 未決：上流と違えてよいかを、まだ決めていないものです。いまの振る舞いを固定するだけで、正しいとは見なしません。一覧は[移植仕様](../../docs/port-spec.md)の「上流と違うが未決の点」にあります

PR0とPR1では、`publish`の試験に「PR2で変わる」という分類もありました。
PR2で`publish`が全体を解釈して読むようになり、その行は上流と一致するもの（表から外しました）か、意図して違えるものへ移しました。
この分類は無くしました。

`publish`の試験は、「意図して違える」と「未決」の分類が、移植仕様の2つの表の「試験の入力」の列とそろっているかも確かめます。
分類を変えるときは、移植仕様の表も一緒に直してください。

## 正解を作り直す

`cases.json`を変えたときと、上流の`tools/hash-strings.ps1`が変わったときに作り直します。
上流のリポジトリは読むだけです。
`git show`で取り出したファイルを使います。

```bash
UP=<上流のリポジトリ>
COMMIT=$(git -C "$UP" rev-parse main)
git -C "$UP" show "$COMMIT:tools/hash-strings.ps1" > /tmp/hash-strings.ps1
pwsh -NoProfile -File scripts/upstream-fixtures.ps1 -Script /tmp/hash-strings.ps1 -Commit "$COMMIT" -Out testdata/upstream/expected.json
```

保存してある`expected.json`は、上流のdockerのhashの経路と同じpwsh 7.4.6（Linux）で作りました。
上流の`docker/Dockerfile`と同じ土台に、同じやり方でPowerShellを入れたイメージを使います。
イメージは次の`Dockerfile`で作れます。

```dockerfile
FROM mcr.microsoft.com/dotnet/sdk:8.0-noble
RUN dotnet tool install --global PowerShell --version 7.4.6 \
    && ln -s /root/.dotnet/tools/pwsh /usr/local/bin/pwsh
ENV PATH="/root/.dotnet/tools:${PATH}"
```

```bash
docker build -t dwloc-pwsh746:local <Dockerfileのあるフォルダー>
docker run --rm -v "$PWD:/src:ro" -v /tmp:/out dwloc-pwsh746:local \
  pwsh -NoProfile -File /src/scripts/upstream-fixtures.ps1 \
  -Script /out/hash-strings.ps1 -Commit "$COMMIT" -Out /out/expected.json -WorkDir /out
cp /tmp/expected.json testdata/upstream/expected.json
```

土台のイメージと`PowerShell`のパッケージのダウンロードが要ります。
2026-09-24に作ったときは、土台が圧縮で約326MB（11層）、パッケージが約48MBでした。

手元のpwsh 7.6.6（Windows、`ja-JP`）で作った結果とも比べました。
違ったのは、pwshの版の記録と、例外の文面の言語だけでした。
例外の文面は比べる対象にしていません。
訳への改行の入力（PR4）で、画面が訳に改行を入れて書いた形（`ml-edit-working-crlf`と`ml-edit-published-lf`）を足し、同じ2つの版で作り直しました。
この2つの入力が画面の保存の書く形そのものであることは、`internal/edit`の`TestEditWritesTheUpstreamCases`が確かめます。

入力を足したら、正解を作り直してからGoの試験を走らせます。
新しい入力が上流と違えば、試験が違い方を出して落ちます。
理由を確かめてから、試験の表に足してください。

## 上流を走らせて突き合わせる

ふだんのGoの試験は、保存した`expected.json`とだけ比べます。
CIには、上流のリポジトリとpwshが無いためです。
環境変数`DWLOC_UPSTREAM_REPO`に上流のリポジトリを渡すと、手元のpwshで正解を作り直し、保存した`expected.json`と同じになるかも確かめます。

```bash
DWLOC_UPSTREAM_REPO=<上流のリポジトリ> go test ./internal/csvfile -run UpstreamFixture
```

| 環境変数 | 意味 |
| ---- | ---- |
| `DWLOC_UPSTREAM_REPO` | 上流のリポジトリです。これが無ければ突き合わせを飛ばします |
| `DWLOC_UPSTREAM_REF` | 取り出すコミットです。省くと`expected.json`に記録したコミットを使います |
| `DWLOC_PWSH` | pwshの実行ファイルです。省くと`PATH`の`pwsh`を使います |

`DWLOC_UPSTREAM_REF`に上流の新しいコミットを渡すと、上流の変更で結果の変わった入力を見つけられます。
