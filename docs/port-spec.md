# 移植仕様

`dragnwash-localization`の既存実装から抽出した、Go移植のための仕様です。
抽出したあと、別のエージェントが元コードを読み直して反証した結果を反映しています。

- 規則: 156件
- 食い違いの指摘: 42件
- 未決の点: 54件

## 公開CSV生成

### データ構造

#### ScriptOrderEntry (data/script_order.csv)

```text
section string / phase string / node string / order string (数字文字列だが出力時は文字列のまま) / line_id string ("line:xxxxxxxx") / key string (16桁hex) / speaker string / condition string
```

ヘッダーは `section,phase,node,order,line_id,key,speaker,condition`。実データ1839行。ファイル出現順が「ゲームの再生順」そのもので、ソートは一切しない。key は重複しうる（実データで119種の重複値、最大29回出現）。line_id は実データでは全行非空かつ重複なし。Cutscene/Reaction/Unused セクションは phase が空。condition は `$conrad_jerked_off_3 $conrad_used_mount_3` のようなスペース区切り文字列

#### LevelFlowEntry (data/level_flow.csv)

```text
level string→int / dragon string / weather string / set_flags string / end_flags string （他に flow_asset, intro, progress_dialogs, idle_dialogs, nag_dialogs, phone, outro, jerkoff_dialog, cum_dialog, mount_start, mount_finish, spawn_flag, player_spawn があるが本スクリプトは未使用）
```

実ファイルはUTF-8 BOM付き。level は 0 始まり。set_flags/end_flags は複数値を " | " で連結した文字列（例 `MedkitCompleted | level_5_complete`）

#### InputRow（入力CSVの1行）

```text
key string? / source_en string? / translation string? / speaker string?
```

列は全て任意。存在判定は `$r.PSObject.Properties['key']` 等。ConvertFrom-Csv の性質上、ヘッダーに列があれば全行にそのプロパティが存在する（値が足りない行は $null → `[string]$null` = ""）。ヘッダーに無い列はプロパティ自体が存在しない

#### OutputRow（公開strings.csvの1行）

```text
key,section,node,order,speaker,translation の6列固定
```

ヘッダー行は `key,section,node,order,speaker,translation`

#### Target

```text
Input string / Output string
```

入力パスと出力パスの組。既定モードでは別パスになりうるが、作業コピーが無ければ Input == Output

#### 内部集約構造

```text
rows map[key]{Speaker,Translation} / inputOrder []string（rows への登録順） / lineRows 挿入順保持マップ[lineId]translation / speakers map[key][]string / levels map[section]headline / done set[string] / counters converted,kept,dropped,lineKept int
```

rows/speakers/levels は PowerShell の既定 Hashtable（大文字小文字を区別しない）。lineRows は `[ordered]@{}`＝OrderedDictionary で、PowerShell 既定では大文字小文字を区別しない（検証済み）。done は `HashSet[string]` で大文字小文字を区別する（検証済み）が、投入されるキーは全て小文字化済みなので実害はない

### 規則

#### R1. リポジトリルートはスクリプトの置かれたディレクトリの親ディレクトリ。

`$root = Split-Path -Parent $PSScriptRoot`。tools/hash-strings.ps1 なので $PSScriptRoot=<repo>/tools、$root=<repo>。以降 data/script_order.csv, data/level_flow.csv, Translations/ は全て $root 起点。

**Goでの注意**: Goでは実行ファイル位置ではなくソース位置に相当する概念が無い。ルートは引数か環境変数で受けるか、カレントディレクトリ起点にする設計判断が必要。

#### R2. キーは「元文字列のUTF-8バイト列に対するSHA-256の先頭8バイトを小文字16進で連結した16文字」。

`function Get-Key`: `$digest = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($text))` のあと `($digest[0..7] | ForEach-Object { $_.ToString('x2') }) -join ''`。先頭8バイト＝16 hex 文字。

**Goでの注意**: `sum := sha256.Sum256([]byte(text)); key := hex.EncodeToString(sum[:8])`。hex.EncodeToString は小文字なので 'x2' と一致。BOM等を勝手に付けないこと。

#### R3. CSVエスケープは「値に カンマ / ダブルクォート / CR / LF のいずれかを含むときだけ」全体をダブルクォートで囲み、内部の " を "" に二重化する。それ以外は素通し。

`function Escape-Csv`: `if ($null -eq $v) { return '' }` / `if ($v -match '[,"\r\n]') { return '"' + ($v -replace '"', '""') + '"' }` / `return $v`。文字クラスは カンマ・二重引用符・CR・LF の4種のみ。

**Goでの注意**: 先頭/末尾の空白、タブ、セミコロン、'#' はエスケープ対象外である点に注意。encoding/csv の Writer は仕様が異なる（先頭空白や \r を独自判断で引用する等）ので使わず、この条件を手書きで実装すること。実データでは `<gradient="yellow - orange">` のようなHTML風タグを含む訳文が引用符付きで出力されている。

#### R4. CSV読み込みは「行頭が '#' の行を除去してからCSVパース」。残り行数が2未満なら空配列を返す。

`function Read-Csv`: `[System.IO.File]::ReadAllLines($file, UTF8) | Where-Object { -not $_.StartsWith('#') }`、`if ($lines.Count -lt 2) { return @() }`、`($lines | ConvertFrom-Csv)`。除去後の最初の行がヘッダーになる。

**Goでの注意**: (a) .NET の ReadAllLines(path, Encoding) は detectEncodingFromByteOrderMarks:true なので UTF-8 BOM を自動で取り除く（level_flow.csv は実際にBOM付きで、検証したところ先頭文字は 'f'=102 だった）。Go では手動で \xEF\xBB\xBF を剥がすこと。(b) 空行は ConvertFrom-Csv が読み飛ばす（検証済み：4データ行中1行が空なら3レコード）。(c) '#' 判定は行の生の先頭文字に対する前方一致で、トリムしない。(d) この方式なので、引用フィールド内で改行して次行が '#' で始まる場合は壊れる。

**上流の変更への追従（ヘッダーの選び方）**: 上流の hash-strings.ps1 は 55d2e09（2026-09-16）で、ヘッダーより上の空行とコメント行を落としてからヘッダーを選ぶようになり、c8fda90（同日）でそれを Remove-NonRecords に置き換えた。Remove-NonRecords は、引用の外にある物理行のうち `$body.Trim().Length -eq 0`（空行と空白だけの行）と `$body.StartsWith('#')` を落とす。003ed1e の ConvertFrom-Csv は完全な空行しか読み飛ばさないので、ファイルの先頭に空白だけの行があると、それが列0個のヘッダーになり、全行が malformed として捨てられていた（公開ファイルがヘッダーだけになり、終了コードは0）。移植（csvfile.ReadPowerShellTable）は、ヘッダーを選ぶ前に空行と空白だけの行を飛ばすように合わせた（上流の報告 #8）。空白の判定は .NET の Trim と同じ集合（Go の strings.TrimSpace）で、全角空白だけの行も飛ばす。"," や `""` の行は上流でも Trim で空にならないので、これまでどおりヘッダーになる。1物理行を1レコードとして読む点（f816618 で上流は全文の解釈へ移った）は変えていない。その代わり、行単位では読み違える形のファイルは publish が書く前に止める（「上流の変更への守り（dwloc の独自）」）。

#### R5. 再生順データは data/script_order.csv、存在しなければ空配列。

`$orderFile = Join-Path $root 'data/script_order.csv'` / `if (Test-Path $orderFile) { $order = @(Read-Csv $orderFile) }`。初期値 `$order = @()`。

**Goでの注意**: $order.Count が 0 のときの分岐が R24/R25 で効いてくるので、空でも処理は続行する。

#### R6. data/level_flow.csv から「セクション名→見出し文言」の対応表を作る。

`foreach ($l in Read-Csv $flowFile)` の中で以下を順に実行する。`$idx = [int]$l.level`。`$sec = ('L{0:00} {1}' -f ($idx + 1), $l.dragon)` ＝ level+1 を2桁ゼロ埋め＋半角スペース＋dragon（例 level=0,dragon=Ryan → `L01 Ryan`）。`$h = 'Level {0}: {1}' -f ($idx + 1), $l.dragon` ＝ こちらはゼロ埋めしない（例 `Level 1: Ryan`）。続いて `if ($l.weather) { $h += " ($($l.weather))" }` ＝ 半角スペース＋丸括弧。`if ($l.set_flags) { $h += ' | sets ' + ($l.set_flags -replace ' \| ', ', ') }`。`if ($l.end_flags) { $h += ' | ends ' + ($l.end_flags -replace ' \| ', ', ') }`。最後に `$levels[$sec] = $h`。実出力例：`Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete`、`Level 9: Alexander (Sunny)`、`Level 10: Ryan (Sunny) | ends PicnicCompleted`。

**Goでの注意**: `-replace ' \| ', ', '` は正規表現置換だが、パターンは実質リテラル " | "（前後に半角スペース1つずつ、パイプはエスケープ済み）。Go では strings.ReplaceAll(s, " | ", ", ") で等価。空判定は PowerShell の真偽値評価で、空文字列のみ偽（文字列 "0" は真）。$sec が重複した場合は後勝ちで上書きされる（実データでは L01〜L15 が全て一意）。

#### R7. Section-Title はセクション名を見出し文言に変換する。Cutscene / Reaction / Unused の3つだけ固定文言、それ以外は levels 表を引き、無ければセクション名をそのまま返す。

`function Section-Title([string]$s) { switch ($s) { 'Cutscene' { 'Cutscenes (started by game code)' } 'Reaction' { 'Dragon reactions (started by game code)' } 'Unused' { 'Unused nodes (not reachable in the current game)' } default { if ($levels.ContainsKey($s)) { $levels[$s] } else { $s } } } }`。

**Goでの注意**: PowerShell の switch は既定で大文字小文字を区別しない。また $levels は既定 Hashtable なので ContainsKey も大文字小文字を区別しない。厳密移植なら両方とも case-insensitive 比較にすること（意図的かは不明＝openQuestions参照）。実データのセクション値は `Cutscene`,`Reaction`,`Unused`,`L01 Ryan`〜`L15 Alexander` の18種。

#### R8. 処理対象の決め方。-Path 指定時はそのパスを入出力兼用にする。無指定時は Translations 直下の各ロケールディレクトリを走査し、作業コピーがあればそれを入力に、無ければ公開 strings.csv 自身を入力にする。出力は常に <locale>/strings.csv。

`if ($Path) { foreach ($p in $Path) { $targets += @{ Input = $p; Output = $p } } }`。else 側は `Get-ChildItem -Path (Join-Path $root 'Translations') -Directory | Where-Object { $_.Name -notlike '_*' } | ForEach-Object { $out = Join-Path $_.FullName 'strings.csv'; $work = Join-Path $root ('Translations/_discovered/' + $_.Name + '.working.csv'); $in = if (Test-Path $work) { $work } else { $out }; if (Test-Path $in) { $targets += @{ Input = $in; Output = $out } } }`。

**Goでの注意**: (a) ディレクトリのみ対象（Translations/ignore.txt のようなファイルは除外される）。(b) 名前が `_` で始まるディレクトリを除外（`-notlike '_*'`、ワイルドカード、大文字小文字非依存）＝ `_discovered` を対象にしない。(c) 入力が存在しないターゲットは作らない。(d) -Path 指定時は存在チェックが無いので、存在しないパスを渡すと Read-Csv で例外（`$ErrorActionPreference = 'Stop'`）。(e) 作業コピーが無い場合 Input==Output となり、同じファイルを「CSV入力として」と「ヘッダー直下コメント引き継ぎ用として」の2回読む。書き込みは全て読み終えた後なので問題は起きない。

#### R9. speakers 表：script_order 全体を走査し、同一キーに対して「空でない speaker を、初出順に重複なく」集めたリストを作る。

`foreach ($e in $order) { $k = ([string]$e.key).Trim().ToLowerInvariant(); if (-not $e.speaker) { continue }; if (-not $speakers.ContainsKey($k)) { $speakers[$k] = New-Object System.Collections.Generic.List[string] }; if (-not $speakers[$k].Contains([string]$e.speaker)) { $speakers[$k].Add([string]$e.speaker) } }`。

**Goでの注意**: キーは Trim → ToLowerInvariant。speaker 側は Trim もキャストも小文字化もしない生の値。List<string>.Contains は序数的な大文字小文字を区別する比較。並び順は script_order のファイル出現順。この表が後で `-join '/'` されて `Ryan/Alexander` や `Phone/Ryan/Alexander/Conrad/Kobold` になる（実出力に存在）。

#### R10. 台詞ID（line row）の判定は正規表現 `^line:[A-Za-z0-9_.\-]{1,59}$` を大文字小文字を区別して適用する。

`$lineIdPattern = '^line:[A-Za-z0-9_.\-]{1,59}$'`、判定は `if ($rawKey -cmatch $lineIdPattern)`。-cmatch なので `LINE:abc` や `Line:abc` はここに入らない。

**Goでの注意**: Go では `regexp.MustCompile("^line:[A-Za-z0-9_.\\-]{1,59}$")` で MatchString。プレフィックスは小文字 `line:` 固定。コロン以降は英数字・アンダースコア・ピリオド・ハイフンのみ、1〜59文字。ここに入らなかった `LINE:abc` 等は R13以降に落ち、source_en も無く16桁hexでもないので R17 で dropped になる。

#### R11. 入力各行の key は、key 列がヘッダーに存在するときのみ読み、前後の空白をトリムする。存在しなければ空文字列。

`$rawKey = if ($r.PSObject.Properties['key']) { ([string]$r.key).Trim() } else { '' }`。

**Goでの注意**: key だけ Trim され、source_en / translation / speaker はトリムされない（R14, R19）。PSObject.Properties[...] の有無は実質「入力CSVのヘッダーにその列名があるか」と同値（検証済み：列数が足りない行でもプロパティは存在し値が $null になる）。

#### R12. key が台詞IDパターンに一致する行は line row として別扱いにする。訳が空なら捨てる。同じ台詞IDが複数あれば先勝ち。どちらの場合も以降のハッシュ処理には一切進まず、converted/kept/dropped のどのカウンタにも入らない。

`if ($rawKey -cmatch $lineIdPattern) { $tr = if ($r.PSObject.Properties['translation']) { [string]$r.translation } else { '' }; if ($tr -ne '' -and -not $lineRows.Contains($rawKey)) { $lineRows[$rawKey] = $tr }; continue }`。

**Goでの注意**: (a) 空判定は `-ne ''` のみでトリムしないので、半角スペース1つの訳は「非空」として採用される。(b) 先勝ち条件 `-not $lineRows.Contains($rawKey)` の Contains は大文字小文字を区別しない（[ordered]@{} の既定、検証済み）。ただし R10 が -cmatch で `line:` を小文字に固定しているため、差が出るのはコロン以降の英字の大小のみ。(c) lineRows は挿入順を保持する必要がある（R26 の出力順に効く）。(d) speaker 列は line row では読まれない（出力時は script_order 側の speaker を使う＝R24）。

#### R13. 台詞IDでない行は key を小文字化してから扱う。

`$key = $rawKey.ToLowerInvariant()`。

**Goでの注意**: ToLowerInvariant（カルチャ非依存）。Go は strings.ToLower でよいが、トルコ語ロケール等の影響を受けない点で等価。

#### R14. source_en 列の値（トリムしない生の文字列）を取得する。列が無ければ空文字列。

`$src = if ($r.PSObject.Properties['source_en']) { [string]$r.source_en } else { '' }`。

**Goでの注意**: ここをトリムしてはいけない。ハッシュは「厳密にこの文字列」に対して計算される（.SYNOPSIS の "SHA-256 over the UTF-8 bytes of the exact source string"）。

#### R15. source_en が非空の行：ハッシュを計算し、key 列が非空でハッシュと一致しないなら dropped として捨てる。一致するか key が空なら、計算したハッシュを key として採用し converted を加算する。

`if ($src -ne '') { $hashed = Get-Key $src; if ($key -ne '' -and $key -ne $hashed) { $dropped++; continue }; $key = $hashed; $converted++ }`。

**Goでの注意**: (a) 不一致時は「この行を丸ごと捨てる」であって、ハッシュで上書きするのではない。source_en が改変されたのに古いキーが残っている行を検出するための仕掛け。(b) `-ne` は大文字小文字を区別しないが、$key は R13 で小文字化済み、$hashed も小文字なので実質単純比較。(c) source_en の空判定は `-ne ''` のみでトリムしない。

#### R16. source_en が空で、key が16桁の小文字16進に完全一致する行：そのまま通し kept を加算する。

`} elseif ($key -match '^[0-9a-f]{16}$') { $kept++ }`。

**Goでの注意**: `-match` は大文字小文字非依存だが、$key は既に小文字化済みなので事実上「小文字16進16桁」。PowerShell の `$` は末尾改行の直前にもマッチするが、key は Trim 済みなので影響なし。Go では `^[0-9a-f]{16}$` を小文字化後の文字列に適用すれば等価。

#### R17. source_en も空で key も16桁16進でない行は dropped として捨てる。

`} else { $dropped++; continue }`。

**Goでの注意**: key が空文字列の行、`line:`以外の任意文字列、16進でも桁数違いの行が全部ここに来る。

#### R18. 既に同じキーが rows に登録済みなら、その行は何のカウンタも動かさずに読み飛ばす（先勝ち）。この重複チェックは R15/R16 のカウンタ加算より後、訳の空判定より前に行われる。

`if ($rows.ContainsKey($key)) { continue }`（110〜134行目の中で、$converted++/$kept++ の後、`if ($tr -eq '')` の前）。

**Goでの注意**: 順序が意味を持つ。converted/kept は「形式的に妥当だった行の数」であり、重複や未翻訳で実際には出力されない行も数に含まれる。ログ数値を再現するならこの順序を守ること。

#### R19. speaker と translation を、列がある場合のみトリムせずに取得する。

`$who = if ($r.PSObject.Properties['speaker']) { [string]$r.speaker } else { '' }` / `$tr = if ($r.PSObject.Properties['translation']) { [string]$r.translation } else { '' }`。

**Goでの注意**: どちらもトリムしない。

#### R20. 訳が空文字列の行は出力しない（rows にも inputOrder にも登録しない）。カウンタも動かさない。

`if ($tr -eq '') { continue }   # nothing to publish for an untranslated line`。

**Goでの注意**: 重要な副作用：R18 の重複チェックより後にあるため、同じキーで「訳が空の行が先、訳のある行が後」に並んでいる場合、空の行はスロットを占有せず、後の訳のある行がちゃんと採用される。逆に「訳のある行が先」なら後の行は R18 で落ちる。空判定はトリムしないので、空白のみの訳は採用される。

#### R21. 採用した行は rows[key] に {Speaker, Translation} として保存し、そのキーを inputOrder に追加する。

`$rows[$key] = @{ Speaker = $who; Translation = $tr }` / `$inputOrder.Add($key)`。

**Goでの注意**: inputOrder は入力ファイルの出現順を保持する。R18 のおかげで同じキーが2回入ることはない。

#### R22. 出力の1行目は固定ヘッダー `key,section,node,order,speaker,translation`。

`[void]$out.AppendLine('key,section,node,order,speaker,translation')`。

**Goでの注意**: 入力のヘッダーが何であれ出力は常にこの6列。

#### R23. 既存の公開ファイルがある場合、そのヘッダー直下のコメント行（言語名・暫定訳の注意書き・クレジット）をそのまま引き継ぐ。1行目を飛ばして順に見ていき、「'#' で始まらない行」「'# =====' で始まる行」「'# ---' で始まる行」のいずれかに当たった時点で打ち切る。

`if (Test-Path $t.Output) { foreach ($line in ([System.IO.File]::ReadAllLines($t.Output, UTF8) | Select-Object -Skip 1)) { if (-not $line.StartsWith('#') -or $line.StartsWith('# =====') -or $line.StartsWith('# ---')) { break }; [void]$out.AppendLine($line) } }`。

**Goでの注意**: (a) 打ち切り条件は OR なので、空行でも即 break する。(b) 実例：Translations/de/strings.csv は2〜5行目が `# Language: Deutsch (de)` 以下の4行、6行目が空行なので4行が引き継がれ、6行目で break。Translations/ja/strings.csv は2行目が空行なので何も引き継がれない。(c) 引き継いだ行の後に空行は付けない。空行は R24 の最初のセクション見出しが `AppendLine('')` で出すので結果的に1行空く。(d) ファイルが無ければ何もしない＝コメントは消える。(e) 判定は行の生の先頭一致（トリムしない）。

#### R24. 本体は script_order をファイル出現順に走査し、「そのキーの訳が入力にあり未出力」か「その line_id の訳が lineRows にある」場合だけ行を出す。セクションが変わったら空行＋'# ===== タイトル =====' を、ノードが変わったら '# --- タイトル ---' を、行を書く直前に挿入する。ハッシュ行と台詞ID行の両方が該当する場合はハッシュ行を先に書く。

`foreach ($e in $order) { $k = ([string]$e.key).Trim().ToLowerInvariant(); $lid = [string]$e.line_id; $hashRow = $rows.ContainsKey($k) -and -not $done.Contains($k); $lineRow = $lid -ne '' -and $lineRows.Contains($lid); if (-not $hashRow -and -not $lineRow) { continue }`。セクション見出し：`if ($e.section -ne $lastSection) { [void]$out.AppendLine(''); [void]$out.AppendLine('# ===== ' + (Section-Title $e.section) + ' ====='); $lastSection = $e.section; $lastNode = $null }`。ノード見出し：`if ($e.node -ne $lastNode) { $title = $(if ($e.phase) { $e.phase + ': ' } else { '' }) + $e.node + $(if ($e.condition) { ' | if ' + $e.condition } else { '' }); [void]$out.AppendLine('# --- ' + $title + ' ---'); $lastNode = $e.node }`。ハッシュ行：`if ($hashRow) { $who = if ($speakers.ContainsKey($k)) { $speakers[$k] -join '/' } elseif ($rows[$k].Speaker) { $rows[$k].Speaker } else { $e.speaker }; [void]$out.AppendLine($k + ',' + (Escape-Csv $e.section) + ',' + (Escape-Csv $e.node) + ',' + $e.order + ',' + (Escape-Csv $who) + ',' + (Escape-Csv $rows[$k].Translation)); [void]$done.Add($k) }`。台詞ID行：`if ($lineRow) { [void]$out.AppendLine($lid + ',' + (Escape-Csv $e.section) + ',' + (Escape-Csv $e.node) + ',' + $e.order + ',' + (Escape-Csv $e.speaker) + ',' + (Escape-Csv $lineRows[$lid])); $lineRows.Remove($lid); $lineKept++ }`。

**Goでの注意**: (a) 見出しの挿入は「行が1つ以上書かれることが確定した後」に行う（`continue` が先）。該当行の無いセクション/ノードの見出しは出ない。(b) `$done` により、script_order 内で同じキーが複数回出てもハッシュ行は最初の位置に1回だけ出る。(c) 見出しの変化判定は Section-Title 変換前の生の section / node を比較する。PowerShell の `-ne` は大文字小文字非依存。初期値は `$lastSection = $null; $lastNode = $null` で、セクションが変わると lastNode は null にリセットされるため、同名ノードが別セクションに現れれば見出しは再度出る。(d) ノードタイトルの組み立て：phase が非空なら `phase + ': '` を前置、node、condition が非空なら `' | if ' + condition` を後置。実例 `# --- intro: Ryan_1_intro ---`、`# --- Alexander_SexScene ---`（Cutscene は phase が空）、`# --- cum: Conrad_Outro_3 | if $conrad_jerked_off_3 $conrad_used_mount_3 ---`。(e) speaker の決定順は厳密に：1) script_order 由来の全話者を '/' 連結（speakers に該当キーがあれば無条件にこれ）、2) 無ければ入力行の speaker（PowerShell 真偽値評価＝空文字列のみ偽）、3) それも空なら当該 script_order 行の speaker。speakers は「そのキーを持つ script_order 行のどれか1つでも speaker が非空なら」存在するので、2)・3) に落ちるのは全行 speaker が空のときだけ。(f) `$e.order` と `$lid` はエスケープを通さず素のまま連結する（section/node/speaker/translation のみ Escape-Csv）。(g) 台詞ID行の speaker は「入力の speaker」ではなく `$e.speaker`（script_order のその行の speaker）。(h) `$lineRows.Remove($lid)` により、残ったものが R26 の対象になる。(i) 出力例：`84f325bca745e504,L01 Ryan,Ryan_1_intro,9,Ryan/Alexander,素晴らしい！` の直後に `line:6046bedf,L01 Ryan,Ryan_1_intro,9,Ryan,よかった！`（同じ order 番号で2行）。

#### R25. script_order に無かったキー（UI テキスト等）は最後にまとめて出す。script_order が1件以上読めている場合のみ空行＋'# ===== UI and other text (not part of the dialogue script) =====' 見出しを付け、section を 'UI'、speaker は入力の speaker が非空ならそれ、空なら 'UI' とする。node と order は空。

`$left = @($inputOrder | Where-Object { -not $done.Contains($_) })` / `if ($left.Count -gt 0) { if ($order.Count -gt 0) { [void]$out.AppendLine(''); [void]$out.AppendLine('# ===== UI and other text (not part of the dialogue script) =====') }; foreach ($k in $left) { $who = if ($rows[$k].Speaker) { $rows[$k].Speaker } else { 'UI' }; $sec = if ($order.Count -gt 0) { 'UI' } else { '' }; [void]$out.AppendLine($k + ',' + $sec + ',,,' + (Escape-Csv $who) + ',' + (Escape-Csv $rows[$k].Translation)) } }`。

**Goでの注意**: (a) 出力順は inputOrder の順＝入力ファイルの出現順（ソートしない）。(b) 見出しと section='UI' の両方が `$order.Count -gt 0` に依存する。script_order が読めなかった場合は見出しも 'UI' も付かず section は空文字列になる。(c) 行の形は `key,UI,,,speaker,translation`（node と order は空フィールド）。(d) `$sec` は Escape-Csv を通さない（リテラルなので実害なし）。(e) 実データでは ja/strings.csv に110行。

#### R26. script_order に対応が見つからなかった台詞ID行は、空行＋'# ===== Per-line translations not found in the script order =====' 見出しの下に、section/node/order/speaker を全て空にして出す。この見出しには script_order の有無による条件が付かない。

`if ($lineRows.Count -gt 0) { [void]$out.AppendLine(''); [void]$out.AppendLine('# ===== Per-line translations not found in the script order ====='); foreach ($lid in @($lineRows.Keys)) { [void]$out.AppendLine($lid + ',,,,,' + (Escape-Csv $lineRows[$lid])); $lineKept++ } }`。

**Goでの注意**: (a) 行の形は `line:xxxx,,,,,訳`（カンマ5つ＝4列が空）。$lid はエスケープしない。(b) 出力順は lineRows の挿入順＝入力ファイルでの出現順。(c) ここでも $lineKept を加算するので、ログの "per-line" 数は「script_order 上に置けた数」＋「置けなかった数」の合計になる。(d) R25 の UI ブロックより後に来る。(e) 実データの ja/strings.csv にはこのセクションは存在しない（全 line: 行が script_order に収まっている）。

#### R27. 出力は BOM なし UTF-8 で、出力パスへ丸ごと上書き保存する。改行は StringBuilder.AppendLine 由来で、Windows 実行時は CRLF。ファイル末尾は改行で終わる。

`[System.IO.File]::WriteAllText($t.Output, $out.ToString(), (New-Object System.Text.UTF8Encoding $false))`。組み立ては全て `$out.AppendLine(...)`。

**Goでの注意**: (a) `UTF8Encoding $false` ＝ BOM を出力しない（実ファイルもBOM無しを確認）。(b) AppendLine は Environment.NewLine を使うので Windows では "\r\n"。ただしリポジトリの blob は LF（`git config core.autocrlf` が `input` で、コミット時に正規化されている）。Go で「作業ツリー上のファイルとバイト一致」を狙うなら CRLF、「git の中身と一致」を狙うなら LF。どちらを正とするかは要確認（openQuestions参照）。(c) 最後の行も AppendLine なので必ず末尾改行が付く。(d) 上流は f816618 で StringWriter（NewLine は LF）に変え、どの環境でも LF で書くようになった。(b) の「要確認」は LF で確定した（「未決の点」の1つ目）。

#### R28. ターゲットごとに1行の集計ログを出す。

`Write-Host ("{0} <- {1}: {2} converted, {3} already hashed, {4} per-line, {5} malformed dropped, {6} in play order, {7} other" -f $t.Output, $t.Input, $converted, $kept, $lineKept, $dropped, $done.Count, $left.Count)`。プレースホルダの順番と変数の順番が一致していない点に注意：{4}=$lineKept が "per-line"、{5}=$dropped が "malformed dropped"。

**Goでの注意**: {6} の $done.Count は script_order 上に出力できたハッシュ行の数、{7} の $left.Count は UI 行の数。

#### R29. エラーは即座に中断する。

`$ErrorActionPreference = 'Stop'`（34行目）。

**Goでの注意**: 入力ファイルが読めない、-Path が存在しない、level_flow の level が数値でない等で例外になり、以降のターゲットも処理されない。Go では error を返して打ち切る設計に対応。

### 境界条件

- UTF-8 BOM：data/level_flow.csv は実際にBOM付き。.NET の File.ReadAllLines(path, Encoding) は detectEncodingFromByteOrderMarks:true で動くため BOM が自動的に剥がされ、1列目のヘッダー名が正しく 'flow_asset' になる（PowerShellで実測し先頭文字コード102='f' を確認）。Go の encoding/csv は BOM を剥がさないので、明示的に \xEF\xBB\xBF を除去しないと先頭列名が "﻿level..." 相当になり、level_flow の読み取りが全滅する。
- 空行：ConvertFrom-Csv は入力中の空行をレコードとして返さない（実測で確認）。Read-Csv の '#' 除去は空行を残すので、空行は CSV パーサ側で落ちる。Go でも空行はスキップすること。
- 行頭 '#' によるコメント除去は行単位の前方一致（トリムなし）で行われるため、引用フィールド内の改行で次の行が '#' から始まるデータは破壊される。逆に Escape-Csv は '#' をエスケープ対象にしていないので、'#' で始まる訳文を出力すると次回読み込み時にコメントとして消える（実データには該当なしを確認）。
- 列が足りない行：ConvertFrom-Csv は不足フィールドを $null にするが PSObject のプロパティ自体は存在する（実測で確認）。つまり `$r.PSObject.Properties['source_en']` の真偽は「入力CSVのヘッダーにその列があるか」と同値で、行ごとの列数には依存しない。`[string]$null` は空文字列。
- トリムの非対称性：key だけ Trim される（R11, R9, R24）。source_en / translation / speaker はトリムされない。空白1文字だけの translation は「非空」と判定され出力される。
- key が空文字列で source_en もある行：`$key -ne '' -and $key -ne $hashed` の前半が偽になるのでハッシュ不一致チェックをすり抜け、計算したハッシュが採用される（converted 扱い）。
- 同一キーの重複行：先に rows に入った方が勝つ（R18）。ただし訳が空の行は rows に入らないので、空→非空の順でも後の非空行が採用される（R20 が R18 より後にあるため）。
- 同一台詞IDの重複行：訳が非空の最初の1件が勝つ（R12）。実データの script_order 内では line_id の重複は0件。
- script_order 内のキー重複：実データで119種の値が重複し、最大は ab5df625bc76dbd4 の29回。この場合 speakers 表には登場する全話者が初出順で集まり（例 `Phone/Ryan/Alexander/Conrad/Kobold`）、実際の出力行は $done により最初の出現位置にだけ1行出る。
- 大文字小文字の扱いが混在する：rows / speakers / levels は PowerShell 既定 Hashtable で case-insensitive、lineRows の [ordered]@{} も case-insensitive（実測）、一方 done の HashSet[string] は case-sensitive（実測）、List<string>.Contains（speaker 重複判定）も case-sensitive、Section-Title の switch は case-insensitive、`-ne` / `-match` / `-notlike` も case-insensitive、台詞ID判定の `-cmatch` だけ case-sensitive。
- 台詞IDらしいが大文字を含む key（例 `LINE:abc`）：R10 の -cmatch に外れ、source_en も無く16桁hexでもないので dropped になる。
- script_order が読めない（$order.Count == 0）場合：セクション/ノード見出しは一切出ず、全ての採用行が R25 の leftovers に落ちる。さらに UI 見出しも出ず、section も 'UI' でなく空文字列になる。ただし R26 の台詞ID用見出しだけは条件なしで出る。
- 出力ファイルが存在しない場合：ヘッダー直下コメントの引き継ぎは行われず、言語名・暫定訳の注意書き・クレジットが失われる。
- 入力＝出力になるケース（作業コピーが無い既定モード）：同じファイルを CSV として1回、コメント引き継ぎ用に生の行として1回、計2回読む。書き込みは全て読み終えた後なので破壊はしない。
- コメント引き継ぎの break 条件は OR 結合：空行でも、`# =====` でも、`# ---` でも打ち切る。ja/strings.csv は2行目が空行なので0行、de/strings.csv は2〜5行目が '#' コメントなので4行引き継がれる（実ファイルで確認）。
- level_flow の set_flags/end_flags が単一値のときは " | " を含まないので置換は何も起こらない（例 `sets level_1`）。複数値のときだけ `MedkitCompleted, level_5_complete` のようにカンマ区切りになる。
- level 番号のゼロ埋め有無が2箇所で異なる：セクションキー側は `{0:00}` で2桁ゼロ埋め（L01）、見出し文言側は `{0}` でゼロ埋めなし（Level 1）。
- $e.order と $lid はエスケープされず素のまま連結される。現行データは数値と `line:xxxxxxxx` なので問題ないが、カンマや引用符を含む値が来ると出力CSVが壊れる。
- ハッシュ行と台詞ID行が同じ script_order 行に両方該当した場合、同じ order 番号を持つ2行が連続して出力される（ja/strings.csv 13〜14行目が実例）。
- 改行コード：スクリプトが書き出すのは Environment.NewLine（Windows では CRLF）だが、リポジトリに入っている strings.csv は LF（core.autocrlf=input による正規化）。バイト一致検証をする際はこの差を考慮する必要がある。上流は f816618 から LF で書くので、上流 main と比べるときはこの差が無い。

### 敵対検証で見つかった食い違い

#### [high] ノード見出し `# --- ... ---` の再出力条件。仕様R24は「ノードが変わったら」とだけ書き、goNote(c)で「同名ノードが別セクションに現れれば見出しは再度出る」と述べているため、「ノードごとに1回」（seen集合）と読める。実際は直前に出力した行のノードとの比較（`$e.node -ne $lastNode`）なので、同一セクション内でノードが飛び飛びに再登場すると見出しがもう一度出る。

- 根拠: 158-162行 `if ($e.node -ne $lastNode) { ... $lastNode = $e.node }`。$lastNode は `continue`（153行）を通過して実際に行が出る行でのみ更新される。data/script_order.csv を走査すると同一セクション内のノード再訪が2件（`L08 Conrad / Conrad_finished_jerkoff_3`、`L13 Ryan / Ryan_5_intro`）存在し、実出力でも ja/strings.csv の440行目と486行目に `# --- cum: Conrad_finished_jerkoff_3 ---` が2回現れる。13ロケール全てで nodeHeaders=183・重複2件を実測。
- 直し方: R24に明記する：「ノード見出しは『直前に出力した行のノード』と異なるときに出す。過去に出したことがあるかは見ない」。さらに『$lastNode を更新するのは実際に出力された行だけ』も書く（全行スキップされたノードを挟んでも見出しは再出力されない）。Goでは `lastNode` 変数比較で実装し、seen集合を使わないこと。これを誤ると全ロケールで2行ずれてバイト一致しない。

#### [high] 入力CSVの列名照合が大文字小文字を区別しない点が仕様に一切書かれていない。R11/R14/R19は `$r.PSObject.Properties['key']` を「入力CSVのヘッダーにその列名があるか」と説明するだけで、照合方法に言及がない。

- 根拠: 111,113,118,129,130行の `$r.PSObject.Properties['key']` および `$r.key` はいずれも PSMemberInfoCollection の大文字小文字非依存ルックアップ。実測：ヘッダー `Key,Source_EN,Translation,Speaker` のCSVに対し `$r.PSObject.Properties['key']` が True を返し `$r.key` が値を返した。script_order / level_flow 側の `$e.key`,`$e.section`,`$e.node`,`$e.order`,`$e.line_id`,`$e.speaker`,`$e.phase`,`$e.condition`,`$l.level`,`$l.dragon` も同様。
- 直し方: 「列名の照合は大文字小文字を区別しない」をR11/R14/R19（およびR6/R9/R24のscript_order参照）に追記する。Goではヘッダー名を正規化（ToLower）してから索引すること。exact match で実装すると、in-game のワーキングコピーのヘッダーが `Key,Source_EN,...` だった場合に全行が key='' / source_en='' 扱いで dropped になり、出力ファイルがヘッダー＋コメントだけで上書きされる（サイレントな全損）。ワーキングコピーの列構成が openQuestions で未確認なだけに影響が大きい。

#### [medium] 引用フィールド内の改行が「壊れる条件付き」として書かれている。R4 goNote(d)は「引用フィールド内で改行して次行が '#' で始まる場合は壊れる」とするが、実際は '#' に関係なく常に壊れる（複数行フィールドは一切サポートされない）。

- 根拠: 53行で `ReadAllLines` により行配列にしてから 55行 `$lines | ConvertFrom-Csv` に渡すため、配列要素1つ＝1レコードとして扱われる。実測：`@('a,b','1,"x','y"','2,3') | ConvertFrom-Csv` は3レコードを返し、値は `[1][x]` / `[y"][]` / `[2][3]` になる。さらに実際にスクリプトへ複数行訳を含む入力を食わせたところ、訳 `one\ntwo` は `one` に切り詰められ、継続行 `two",Ann` は独立レコードとして dropped に計上された（3 converted, 2 malformed dropped）。一方 Escape-Csv（47行）は値に CR/LF を含むと引用付きで出力するので、往復で必ず壊れる経路が存在する。
- 直し方: R4を「CSVは行単位でパースする。引用フィールド内の改行はサポートされず、物理行ごとに別レコードになる」と書き換える。Goでは encoding/csv に全文を流す実装（複数行フィールドを正しく解釈する）を使ってはならない。1行ずつ切り出し、1行を単独のCSVレコードとしてパースすること。そうしないと同じ入力から異なるレコード集合・異なるSHAキーが生成される。

#### [medium] 出力改行コードが未決（openQuestions行き）のままで、しかも根拠の記述が事実と異なる。R27 goNote(b)は「作業ツリー上のファイルとバイト一致を狙うなら CRLF」と書くが、作業ツリーのファイルは実際には LF。

- 根拠: 実測：Translations 配下13ロケールの strings.csv はいずれも CRLF を含まない（全て LF）。.gitattributes には `*.sh text eol=lf` しかなく strings.csv の指定はない。core.autocrlf は local/global とも `input` なのでチェックアウト時に CRLF へ変換されない。つまり「作業ツリー＝blob＝LF」であり、188行の WriteAllText + AppendLine(Environment.NewLine) が Windows で吐く CRLF は、実行直後の一時的な状態にすぎない（git add で LF に戻る）。
- 直し方: R27を「リポジトリ上も作業ツリー上も LF。Windows版PowerShellは実行時に一時的に CRLF を書くが、それはリポジトリの正の状態ではない」と訂正し、Go実装は `\n` 固定と決め打つ。実際、スクラッチパッドで本スクリプトを実行し LF 正規化して比較したところ13ロケール全てでコミット済み内容と完全一致した（差分は改行のみ）。

#### [low] `$lastSection` / `$lastNode` の初期値 `$null` と空文字列 `''` が区別される点が書かれていない。R24 goNote(c)は「初期値は $lastSection = $null」と書くだけ。

- 根拠: PowerShell では `'' -ne $null` が True（実測）。したがって先頭要素の section（またはセクション変更直後の node）が空文字列でも見出しは出力される。Goで `lastSection := ""` と初期化すると、この場合に見出しが出なくなる。
- 直し方: R24に「未設定を表すセンチネルは空文字列と区別する必要がある」と明記し、Goでは `*string` か `hasLast bool` を併用する。現行 data/script_order.csv には空の section/node は存在しない（実測で0件）ため現時点の出力には影響しないが、条件の強弱が変わる。

#### [low] 空行スキップの条件が狭い。R4 goNote(b)と edgeCases は「空行は ConvertFrom-Csv が読み飛ばす」とだけ書くが、実際は空白のみの行も読み飛ばされる。

- 根拠: 実測：`@('a,b','1,2','   ','3,4') | ConvertFrom-Csv` は2レコードしか返さず、`'   '` の行はレコード化されない。
- 直し方: R4を「空行および空白のみの行はレコードにならない」に修正する。Goで `len(line)==0` だけを条件にすると、空白行が1レコード（全列 null）になり key='' / source_en='' で `dropped++` されるため、R28のログ数値が実装間でずれる。

#### [low] 入力行のフィールド数がヘッダーより多い場合と、ヘッダー名が重複する場合の挙動が未記載。

- 根拠: 実測：`@('a,b','1,2,3,4') | ConvertFrom-Csv` は余剰フィールドを黙って捨て a=1,b=2 のみを返す（エラーなし）。一方 `@('a,a','1,2') | ConvertFrom-Csv` は「メンバー "a" は既に存在します」で例外となり、34行 `$ErrorActionPreference='Stop'` により処理全体が停止する。
- 直し方: R4に両方を追記。Goでは `csv.Reader.FieldsPerRecord = -1` 相当にして余剰フィールドを切り捨て（エラーにしない）、ヘッダー重複（大文字小文字非依存で重複扱い）は致命エラーで打ち切る。encoding/csv の既定はフィールド数不一致でエラーになるため、そのままでは挙動が変わる。

#### [low] R28の前置き「プレースホルダの順番と変数の順番が一致していない点に注意」が誤り。実際は位置対応は正しく、ラベル語の並びが直感と違うだけ。

- 根拠: 189行の引数順は converted, kept, lineKept, dropped, done.Count, left.Count。{4}は5番目＝$lineKept で文言は "per-line"、{5}は6番目＝$dropped で "malformed dropped"。位置と値は正しく対応している。仕様が併記している対応表（{4}=$lineKept, {5}=$dropped）自体は正しい。
- 直し方: 前置きの一文を削除し「フォーマット文字列のラベル順が converted, already hashed, per-line, malformed dropped であり、per-line と malformed の並びが集計処理の順序と違うだけ」と書き換える。実装者が「順番が狂っている」と読んで $dropped と $lineKept を入れ替える事故を防ぐ。

### 抽出が取りこぼしていた規則

- 入力CSVの列名照合が大文字小文字非依存であること（$r.PSObject.Properties['key'] / $r.key / $e.section / $l.level すべて）。divergences に詳細。
- $lastNode を更新するのは実際に出力された行だけで、全行スキップされたノードは $lastNode を書き換えないこと（そのためノード見出しの再出力は『翻訳が入っている行の並び』に依存する）。
- ヘッダー名が重複する入力は ConvertFrom-Csv が例外を投げ、$ErrorActionPreference='Stop' により以降のターゲットも処理されずに全体が停止すること。
- 入力行のフィールド数がヘッダーより多い場合、余剰フィールドはエラーにならず黙って捨てられること。
- 空白のみの行も（空行と同様に）レコード化されないこと。
- 引用フィールド内の改行は常にサポートされず、物理行ごとに独立レコードになること（'#' で始まるかどうかに関係なく）。
- Escape-Csv の `[string]$v` パラメータバインドにより $null は '' に変換されるため、46行の `if ($null -eq $v) { return '' }` は到達不能であること（挙動差はないが、Goで nil 分岐を作る必要はない）。
- ハッシュ行に出力される key は script_order 側の `$k`（Trim+ToLowerInvariant 済み）であり、入力行の key ではないこと（$rows が大文字小文字非依存なので、入力側の綴りが違っても script_order 側の綴りで出る）。現行データでは常に同一。
- $order / $levels / $speakers はターゲットループの外で1回だけ構築され、$rows / $inputOrder / $lineRows / $done / 各カウンタはターゲットごとに初期化されること（106〜109行）。
- $Path が空配列の場合、PowerShell の真偽値評価で偽になり else 分岐（Translations 走査）に落ちること（85行 `if ($Path)`）。
- Write-Host は情報ストリームへの出力であって標準出力ではないこと（PS5.1）。Go で stdout に出すとリダイレクト挙動が変わる。
- -Path 指定時は必ず Input == Output になるため、変換対象そのものをコメント引き継ぎ用にも読むこと（140行）。R8(e) は既定モードについてのみ述べているが、-Path 指定時は常にこの形になる。

### 上流の変更への守り（dwloc の独自）

上流の hash-strings.ps1 は f816618（2026-09-16）で Read-Csv をファイル全体の解釈へ移し、c8fda90（同日）で Remove-NonRecords を足した。この移植は、全体を解釈する読み手へ移す作業の PR2 で、publish の読み手を同じ読み方（csvfile.ReadPowerShell）へ移した。集め方、失われる訳の確かめ（CheckLoss）、ゲーム側の土台の確かめ（CheckBase）のすべてが同じ読み手で読む。引用符で囲んだ値は物理行をまたいで1つの値になり、値の中の改行は読んだとおりに書く（LF も CRLF も直さない。上流とバイト一致させるため）。

全体を解釈する読み方は、引用符の閉じ誤りと正当な複数行の値を見分けない。上流は閉じ誤りのある作業コピーから、英語の原文やほかの行のキーを訳として公開ファイルへ書く（上流の報告 #11）。失われる訳の確かめはこれを捕まえられない。いまの公開ファイルの訳は消えず、英文が足されるだけだからである。そこで publish は、書き出す前にファイルの形そのものを見て止める（internal/publish の shape.go、CheckTargetShape と CheckShape）。画面の書き出し（公開の形）も同じ確かめを同じ順で通る。

対象は、入力（作業コピー）、いまの公開ファイル（書き出し先）、ゲーム側の公開ファイルの3つで、どれも同じ確かめを通す。ゲーム側の公開ファイルは、土台の確かめが読むとき（書き出し先とゲーム側の両方があるとき）だけ確かめる。入力と書き出し先が同じファイル（作業コピーの無いロケールと --path）なら1回だけ確かめる。どれかに当たれば、どのロケールも書かず、どのファイルの何行目か・何が起きるか・どう直すかを出して終了コード1で止まる（失われる訳の確かめと同じ。読めなくて確かめられなかったときだけが2）。ゲーム側の公開ファイルで当たったときは、コミット済みの公開ファイルを写し直す直し方を先に出す。確かめる順は、形 → 組み立て → ゲーム側の土台の食い違い → 失われる訳。形を組み立てより前に見るのは、閉じない引用符のように組み立てが読み方の誤りで止まる形でも、終了コード2（変換できません）ではなく、どのファイルの何行目をどう直すかを出すためである（決まったことの 3）。ヘッダーの列名の重複は形の確かめでは誤りにせず、組み立て（入力）と失われる訳の確かめ（いまの公開ファイル）が誤りにする（終了コード2。データ行が0件でも誤りにする。決まったことのそのほか 2 (i)）。

- (a) ヘッダーに key 列も source_en 列も無い、translation 列が無い、または最初の列名が '#' で始まる。どの行もキーか訳を引けず、すべて捨てられる。'#' で始まるヘッダーは、読んだ最初の値そのものが '#' で始まるもの（`"#key"` や、先頭の半角空白を読み手が落とす ` #key`）で、上流はそのヘッダーを飛ばして次の行をヘッダーにし、訳を1行も公開しない（決まったことの 12 と 15。csvfile.PowerShellHeader.CommentLike）。source_en,translation だけの作業コピー（上流の .DESCRIPTION が認める形）は通す。データ行が無いファイルでもヘッダーは確かめる（ヘッダーの無い、データ行1行だけのファイルを見分けるため）。ヘッダーの中で開いた引用符が閉じないときは、列名にファイルの終わりまでが入っているので (e) だけを出す。
- (d) 空でない行があるのに1行も読めない。読み手が行の区切りにしない改行の文字（U+2028、U+0085 など）があり、Python の str.splitlines と同じ広さで行を分けるとレコードになる行が2行以上あるのに、読み手がデータのレコードを1つも読めないときに止める。そうした文字の無いファイルは見ない。行をまたぐヘッダーだけでデータの無いファイルを、物理行で数えて誤って止めないためである。CR だけの改行のファイルは止めない。読み手は単独の CR でも行を分けるので正しく読める（上流 main はここで訳をすべて消す。検証の指摘「単独の CR」）。
- (e) 開いた引用符がファイルの終わりまで閉じない（上流の報告 #11）。読み手は型付きの誤り（csvfile.UnclosedQuoteError）を返す。上流の全文の読み方は、そこから後ろ（英語の原文を含む）を1つの値に飲み込み、公開ファイルへ漏らす。
- (f) 飲み込み（引用符が別の行で閉じ、後ろの行を値に飲み込んだと見られる形）。見分けは csvfile.FindSwallows（下の「全体を解釈する読み手（csvfile）」）で、行をまたぐレコードの続きの物理行が、単独で読むとレコードに見える（キーの形で始まる、区切りの数がヘッダーと同じ）か、行をまたいだ引用が閉じたすぐ後ろに文字が続くと止める。飲み込まれたと疑う物理行ごとに1件を出す。入力・いまの公開ファイル・入力と書き出し先が同じファイルのどの経路でも止める（決まったことの 4）。同じファイルの中の訳を同じファイルのいまの訳と比べても、食い違いは見えないためである。直し方は、引用符の閉じ位置を確かめ、値を閉じる " が抜けていれば足し、値の中の " は "" と書くこと。
- (g) 単独の CR。値の中の単独の CR（後ろに LF の続かない CR）と、引用符で囲まない値が引用の外の単独の CR で切れた形の2つ。前者は訳の入ったレコードだけを止める。上流の道具はそのレコードをあとで落とすので、LF に直すよう案内する（決まったことのそのほか 1）。訳の空のレコードはどちらの道具でも公開されないので止めない（原文は翻訳者には直せない）。後者は csvfile.FindCRCuts で見つけ、訳の有無によらず止める（決まったことの 11 と 14）。ゲームは引用の外の CR を捨ててつなげて読むが、publish は切れた前半だけを書き、後半の訳は黙って落ちるためである。

飲み込みの確かめは、正しい複数行の値でも当たることがある（原文の2行目がカンマを多く含む ml-continuation-looks-like-row、source_en,translation の2列の作業コピーで原文が行をまたぐ ml-source-translated-2col）。そのため、続きの行がレコードに見える形だけは、確かめたうえで通す指定（dwloc publish --accept-multiline <ロケール>。--path で走らせたときは --path に渡したファイル）で通せる。通した行は標準エラーに出す。当たらない指定（無いロケール、--path に無いファイル）は終了コード2で止める。閉じ引用符の後ろに文字が続く形は、どの書き手（上流の Escape-Csv、ゲームの WorkingCopy.cs が使う CsvReader.Escape）も作らないので通さない。閉じない引用符と単独の CR も、直せる形なので通さない。画面の書き出しにはこの指定が無く、止まったときの文面で dwloc publish の指定を案内する。

行をまたぐ値の訳は、止めずにそのまま書く（決まったことの 1。設計案の「段階1のあいだ新しい複数行の訳を止める」は採らない）。行単位で読んでいたあいだは、訳が1行目で切り詰められるので、いまの公開ファイルに行をまたぐレコードがあれば止め（旧 (b)）、入力の行をまたぐレコードに訳が入っているか、行単位と全体で集めた訳が食い違えば止めていた（旧 (c)）。読み手が全体を解釈するようになったので、どちらも外した。

表計算ソフトなどで作業コピーを保存し直すと、原文（source_en）の中の改行が LF から CRLF に変わることがある。キーは Mod が LF の原文から計算したものなので合わなくなり、publish はその行を捨てる（R15。上流と同じ）。止めはしないが、新しい訳が黙って公開されないので、訳の入った行のうち原文の CRLF を LF にするとキーが一致するものを、入力の行の範囲とキーで知らせる（決まったことのそのほか 7。internal/publish の SourceLineEndHints）。原文は出さない。知らせるのは失われる訳の確かめより前で、その訳がいまの公開ファイルにあると、失われる訳の確かめが止める理由がここで分かる。

「行をまたぐ」と「閉じない」は、行ごとの引用符の偶奇ではなく、ConvertFrom-Csv の全文の読み方で決める（規則は区切りの関数 csvfile.SplitSegments が持つ。下の「全体を解釈する読み手（csvfile）」）。pwsh 7.6.6 で実測した規則は、引用の中の LF・CRLF・単独の CR は値に残る、フィールドの途中の '"' と閉じ引用符の後ろの '"' は引用を開かない、先頭の空白の後ろの '"' は引用を開く、閉じない引用符はファイルの終わりまでを値にする、の4つ。引用符なしのフィールドの途中の '"'（5" screen のような値）はただの文字で、引用を開かない（上流の報告 #4b の入力 p-bare-quote）。行の区切りは上流の Remove-NonRecords の正規表現ではなく、\r\n / \n / \r にした。

コメント行と空行の落とし方も上流と違える。上流の Remove-NonRecords は、物理行が引用の外かどうかを行ごとの '"' の数の偶奇で決める。dwloc の全文の読み方（区切りの関数 csvfile.SplitSegments）は、フィールドの先頭で開いた引用だけで決める（ConvertFrom-Csv と同じ規則）。そのため、引用符なしのフィールドの途中に裸の '"' がある行（5" screen のような値）の後ろでは、上流は引用の中にいると見なし、次に '"' の数が奇数の行が来るまで、コメント行と空行を落とさずに残す。残ったコメント行は ConvertFrom-Csv がレコードとして読むので、列の多いコメント行は公開ファイルに不正な行として出ることがある（検証が上流 main で再現した。入力「K1,UI,,,UI,5" screen」「# note,b,c,d,e,COMMENT-TAIL」「K2,UI,,,UI,b」で、上流は `fa515818c52bd108,,,,e,COMMENT-TAIL` を公開した）。dwloc はこれらの行を引用の外として落とす。上流の不具合として扱い、写さない。p-bare-quote で出力が上流と同じになるのは、その入力のコメント行が訳の列まで届かない（列が1つしか無い）ためで、この違いが無いことを示すものではない。

引用符で囲まない値の中の単独の CR（手で書いた `a\rb` など）は、全文の読み方でも行の区切りとして値を切る。ゲームは引用の外の CR を捨てて ab と読む（R8）。書き手（Escape-Csv）は CR を含む値を必ず引用するので、道具が書いたファイルには現れない。以前はどの確かめにも当たらず、切れた訳をそのまま公開していた（残る隙間）。PR2 から (g) で止める（下の「上流と意図して違える点」の表。見つける関数は csvfile.FindCRCuts）。

### 全体を解釈する読み手（csvfile）

全体を解釈する読み手へ移す作業（PR0〜PR4）の PR1 で、internal/csvfile に読み方の土台を足した。PR2 で publish（集め方・失われる訳の確かめ・土台の確かめ・形の確かめ）がこの読み手へ移った。diff・order・edit・画面は PR2 と PR3 で移す。ここに書くのは、その土台の約束である。

区切りの関数（csvfile.SplitSegments）は、ファイル全体を1つの文字列として、コメント行・空行・レコードのセグメントに分ける。読み方の規則はこの関数だけが持ち、主の読み手、守り専用の読み手（csvfile.ReadPowerShellWhole）、形の検出が、みなこの結果に載る。保存（internal/edit）も PR3 でここに載せ、レコードの最終フィールドの開始位置から本体の終わりまでを差し替える。書く側と読む側で区切りを別々に持つと、最終フィールドの位置がずれて、訳ではない値を書き換えるためである。

- 物理行は \r\n / \n / \r で分け、1始まりで数える。BOM は数えない（エディターの行番号と同じ）。
- レコードの境目（引用の外）にある、TrimSpace で空になる行は空行、'#' で始まる行はコメントになる。全角空白や NO-BREAK SPACE だけの行も空行である（上流の Remove-NonRecords の Trim と同じ集合）。
- 引用はフィールドの先頭（先頭の空白の後ろを含む）で開いたものだけを数える。閉じない引用符は、ファイルの終わりまでを1つの値にし、引用符が開いた行を覚える。
- 最初のレコードがヘッダーになる。"," や '""' の行でもヘッダーになる。ヘッダーより後ろの、読むとフィールドが0個か空の1個になるレコード（"," や '""' の行）は、空のレコードとして種類を分け、データとしては落とす。2列の作業コピーでキーの空いた行の訳を消すと、その行は空のレコードに変わる。保存でどう扱うかは PR3 で決める。
- 各セグメントは、通し番号の ID（1始まり）、物理行の範囲、本体のバイト範囲（ファイルの先頭から。BOM を含めた位置）、元の終端（CRLF / LF / CR / 無し）を持つ。ヘッダーとレコードは、ConvertFrom-Csv の規則で読んだ値と、フィールドの開始位置（ファイルの先頭から。末尾の空フィールドも1つと数える。csvfile.FieldOffsets と同じ契約）も持つ。セグメントは BOM の直後からファイルの終わりまで、隙間も重なりも無く並ぶ。
- 行の同定には ID を使い、物理行の番号は表示と報告の照合にだけ使う。ほかのレコードの値に改行が増えると行番号はずれるが、ID は変わらない。

主の読み手（csvfile.ReadPowerShell）は、区切りの上に列名の対応と誤りを載せる。

- 閉じない引用符は型付きの誤り（csvfile.UnclosedQuoteError。引用符が開いた行を持つ）にし、値を返さない。上流はファイルの終わりまでを飲み込んで書く（上流の報告 #11）。値を返すと、確かめ忘れた呼び出し側から同じ漏れが起きる。
- 列名の重複は、データが0件でも csvfile.DuplicateColumnError にする（上流 main に合わせる）。両方に当たれば閉じない引用符を返す。
- 壊れたファイルを画面に並べるための入口（csvfile.ReadPowerShellMarked）は、誤りにせず印を付けて返す。閉じない引用符のあるファイルでは、引用符が開いたレコードより後ろを物理行のまま引ける。
- 最初の列名が '#' で始まるヘッダー（`"#key"` など）は、上流と違って飛ばさずにヘッダーとして返す。呼び出し側は PowerShellHeader.CommentLike で見分け、publish は形の確かめ (a) で止める。CommentLike は上流の飛ばし方に合わせ、読んだ最初の値そのものが '#' で始まるか（`"#key"`、先頭の半角空白を読み手が落とす ` #key`）を見る。引用の中の空白（`" #key"`）や、読み手が落とさない NO-BREAK SPACE の後ろに '#' があるヘッダーは、上流ではその名前の列になり source_en から訳を書く。訳を失う形ではないので、dwloc も止めずに上流と同じに書く（hash-header-quoted-space、hash-header-nbsp）。

形の検出は、読み手が見分けない引用符の閉じ誤りなどを、形を見て見つける。止めるかどうかは呼び出し側が決め、publish（PR2 から。形の確かめ (f)(g)）と画面（PR3 から）は同じ関数を呼ぶ。物理行を単独で読むときは1物理行の読み方（csvfile.ParsePowerShellRecord と csvfile.FieldOffsets の中身）を使う。

- 飲み込み（csvfile.FindSwallows）: 行をまたぐレコードの続きの物理行を単独で読み、最初の値がキーの形（前後の空白を除き、台詞ID か、小文字にして16桁の16進）か、区切りの数（引用の外のカンマの数+1）がヘッダーの列数と同じなら、レコードに見えるとして返す。空行・空白だけの行・'#' の行は見ない。閉じない引用符のレコードも見ない。飲み込まれた行が自分の値を引用符で開く形（`one,"いち` の次に `"Alpha line` が来るなど）では、その開き引用符が前の行の閉じ忘れを閉じ、後ろの文字は閉じ引用符の後ろの文字として訳に足される。その行を単独で読むと引用が開いたまま終わるので、上の2つに当たらない。そこで、行をまたいだ引用が閉じたすぐ後ろに文字が続く物理行も返す（理由は text-after-quote）。閉じ引用符の後ろに文字を書く書き手は無い（上流の Escape-Csv も、ゲームの WorkingCopy.cs が使う CsvReader.Escape も値全体を引用し、後ろは区切りか改行）ので、値が改行で終わる形を含め、正当な複数行の値では当たらない。こちらは全体を解釈したときの閉じ方で見るので、'#' の行でも当たる。同じ行の中で開いて閉じた引用の後ろの文字は見ない。上流との突き合わせの入力の表では、飲み込みの8件がすべて当たり（自分の値を引用符で開く3件は text-after-quote で当たる）、実物と同じ形の複数行の原文や複数行の訳は当たらない。正当でも当たるものが2件ある（原文の2行目がカンマを多く含む ml-continuation-looks-like-row と、2列の作業コピーで原文が行をまたぐ ml-source-translated-2col）。これらは確かめたうえで通す指定（dwloc publish --accept-multiline）で書く。
- 引用符で囲まない値の中の単独の CR（csvfile.FindCRCuts）: 見分け方をファイルで分ける（決まったことの 14）。行の区切り（引用の外の終端）がすべて単独の CR のファイルには当てない。CR だけで改行したファイルは読むと決めてあり、そこでは単独の CR を行の区切りと見分ける手がかりが無いためである。それ以外のファイル（LF や CRLF の改行が1つでもあるもの）では、単独の CR で終わるレコードのうち、次のセグメント（区切りの関数が分けたもの）がレコードに見えないものを返す。次のセグメントが空行（空白だけの行を含む）・'#' の行・空のレコード（"," や '""'）でも返す。値が CR の直後の '#' で切れる形（`い\r# ち`）を見逃さないためである。次のセグメントがレコードなら、最初の値がキーの形か、区切りの数がヘッダーの列数と同じならレコードに見えるとし、行末の CR で改行しただけと見て返さない（飲み込みと同じ見方。lone-cr-line-end）。区切りはレコード全体で数える。次の物理行だけを単独で読むと、キー列の空いた行の原文が行をまたぐとき、引用が開いたまま行が終わって区切りが足りず、正当な行末の CR で当たるためである。入力の表の改行をすべて単独の CR に直した写しでは、どの入力にも当たらない。ファイルで分ける前は、U+00AD のあとの '#' の行（soft-hyphen-comment。読み手はデータのレコードにする）と、ヘッダーより列の足りないレコード（ml-header）の前の CR で、切れた値ではないのに当たっていた。
- 値の中の単独の CR（csvfile.LoneCRValues）と、見出しに使う値の中の改行（csvfile.LineBreakValues）。
- ゲームの読み方との食い違い（csvfile.CSharpDisagreements）: レコードを key 列（空なら source_en）の値で組にし（同じ値が複数あれば出現順）、ゲームの読み方（csvfile.ReadCSharpRows）で読んだ値と列ごとに比べる。フィールドの途中の '"'、引用符で囲まない値の前後の空白、引用の外の単独の CR で割れる。

実データでは次を確かめた（件数だけを見た）。

- 上流 main の16ロケールの公開ファイルと、再生順の2ファイル: 主の読み手の件数と値が、行単位の読み手と同じだった（ja 1738、de 1704）。形の検出はどれも当たらなかった（internal/csvfile の TestRealDataWholeReaderAgrees。DRAGNWASH_SOURCE_REPO で元リポジトリを渡す）。
- ゲーム側の ja 作業コピー: レコードは1769件で、行単位の読み手では1770行になる。行をまたぐレコードは1件（原文が空行を挟む複数行で、訳は空）。飲み込み、単独の CR、ゲームの読み方との食い違いは0件で、全レコードのフィールドの開始位置が、本体に csvfile.FieldOffsets を掛けた位置と一致した（TestRealWorkingCopy。DWLOC_WORKING_COPY でファイルを渡したときだけ走る）。ゲーム側の ja の公開ファイルも、1738件で同じ結果だった。

### 上流と意図して違える点

publish の読み方を全体を解釈する読み手へ移す作業（PR0〜PR4）の土台として、上流 main（dc55c9e）の tools/hash-strings.ps1 と dwloc を、合成した入力の表で突き合わせる試験を置いた。入力は testdata/upstream/cases.json（65件、英文と訳はどれも架空の文）、上流の正解は testdata/upstream/expected.json で、scripts/upstream-fixtures.ps1 が上流の Read-Csv と Remove-NonRecords を AST で取り出して読ませた結果（レコードの値と物理行の範囲）と、上流のスクリプトを通しで走らせた結果（出力のバイトと集計の1行）を持つ。作り方は testdata/upstream/README.md にある。

保存した正解は pwsh 7.4.6（上流の docker の hash 経路と同じ。mcr.microsoft.com/dotnet/sdk:8.0-noble に dotnet tool で入れたもの、Linux、カルチャは不変）で作った。手元の pwsh 7.6.6（Windows、ja-JP）で作り直した結果とは、pwsh の版の記録と例外の文面の言語のほかは同じだった（2026-09-24）。culture に依存する StartsWith('#') も、両方で U+00AD のあとの '#' をコメントと見なした。

違ってよいのは、次の表と、あとの「上流と違うが未決の点」の表の行だけである。試験（internal/csvfile/upstream_fixture_test.go と cmd/dwloc/publish_upstream_test.go）は、違う入力と違い方を表として固定し、それ以外の違いが出れば落ちる。

| 項目 | 上流 main | dwloc | 試験の入力 |
| ---- | ---- | ---- | ---- |
| 単独の CR（引用の中、行末） | Remove-NonRecords が単独の CR を行末と見なさず、その手前を捨てる。その行の訳が消える | 単独の CR も行の区切りにして読む。引用の中の単独の CR は値に残し、publish は訳の入ったレコードなら形の確かめ (g) で止めて LF に直すよう案内する（決まったことのそのほか 1） | lone-cr-in-quoted-translation、lone-cr-line-end |
| 引用符で囲まない値の中の単独の CR（上の「残る隙間」） | その行を失う | 単独の CR を行の区切りにする規則のまま読み、値は CR の手前で切れる（ゲームは引用の外の CR を捨てて、切れる前の値を読む）。そのまま書くと切れた訳を公開するので、publish は形の確かめ (g) で止め、CR を取り除くか値を引用符で囲むよう案内する（決まったことの 11。csvfile.FindCRCuts）。見分け方はファイルで分け、行の区切りがすべて単独の CR のファイルには当てず（CR だけの改行のファイルを読むため）、それ以外のファイルでは、単独の CR で終わるレコードの次のセグメントがレコードに見えなければ、空行・'#' の行・空のレコードでも止める（決まったことの 14）。行末の単独の CR で次の行がレコードになる形（lone-cr-line-end）は、いまどおり読む | lone-cr-unquoted-value |
| CR だけの改行のファイル | 全行を捨て、ヘッダーとコメントだけを書く | 読む | cr-only-published、cr-only-working |
| ',' だけの行 | 空の値のレコードにし、malformed dropped に数える | 空行相当として黙って落とす。違うのは集計の数だけ | comma-only-row |
| '#' で始まるヘッダー（引用符で囲んだもの、空白のあとのもの） | ConvertFrom-Csv がそのレコードを飛ばし、次のレコード（データ）をヘッダーにする。何も書かない | 飛ばさずヘッダーとして読み、publish は形の確かめ (a) で止める（読んだ最初の列名が '#' で始まるか。csvfile.PowerShellHeader.CommentLike。決まったことの 12 と 15）。(a) を「key 列が無い」に広げると、正当な source_en,translation の2列の作業コピーまで止まるので、'#' を見る判定を別に置く | hash-header-quoted、hash-header-leading-space |
| 裸の引用符の後ろのコメント行と見出し | '"' の偶奇で引用の中と見なし、レコードにする。列が多ければ公開ファイルに書く | 引用の外として落とす | bare-quote-then-comment、bare-quote-then-heading、quote-after-closing-quote |
| 行頭の '#' の判定 | StartsWith('#') はカルチャに依存する照合で、U+00AD のように照合上無視される文字を飛ばす | 序数で比べる。U+00AD で始まる行はデータとして読み、malformed dropped に数える | soft-hyphen-comment |
| 閉じない引用符（上流の報告 #11） | ファイルの終わりまでを値に飲み込み、英語の原文ごと書く | 読み手が型付きの誤りを返し、publish は形の確かめ (e) で、組み立てより前に終了コード1と直し方の案内にして止める（決まったことの 3）。ヘッダーで開いたときは (a) を重ねて出さない | unclosed-to-eof、unclosed-last-line、unclosed-header、unclosed-published-middle |
| 飲み込み（引用符が別の行で閉じる） | 後ろの行（英語の原文やキー）を訳に取り込んで書く | 飲み込みの確かめ (f) で止める（csvfile.FindSwallows）。入力・いまの公開ファイル・入力と書き出し先が同じファイルのどの経路でも止める | swallow-3col、swallow-7col-hash-close、swallow-2col-hash-close、swallow-7col-empty-key-english、swallow-6col-published、swallow-2col-own-quote、swallow-7col-empty-key-own-quote、swallow-6col-published-own-quote |
| 正しい複数行の値でも飲み込みの確かめが当たる形 | 正しい値として書く | 続きの行が単独で読むとレコードに見えるので (f) で止める（原文の2行目がカンマを多く含み7列、2列の作業コピーで原文が行をまたぐ）。確かめたうえで通す指定（dwloc publish --accept-multiline）を付けると、上流と同じバイトを書く | ml-continuation-looks-like-row、ml-source-translated-2col |
| ヘッダーに key 列も source_en 列も無いか、translation 列が無い | すべての行を捨て、ヘッダーとコメントだけを書く | 形の確かめ (a) で止める | ml-header、hash-header-unquoted |
| 集計の1行の kept from the published file | いまの公開ファイルから引き継いだ行の数を、9項目目として出す | この項目を持たない。publish の試験は、この項目を除いた6項目を比べる | （すべての入力） |

読み手の試験（internal/csvfile）は、主の読み手（csvfile.ReadPowerShell）、publish の守り専用の csvfile.ReadPowerShellWhole、行単位の読み手の3つを突き合わせる。主の読み手の違いは、上の表の読み手に関わる行（単独の CR、CR だけの改行、',' だけの行、'#' で始まるヘッダー、裸の引用符、行頭の '#'、閉じない引用符）だけである。閉じない引用符では、値を返さずに型付きの誤り（UnclosedQuoteError）を返す。

PR0 では、次の違いを「PR1 で直す」として表に載せていた。PR1 で主の読み手が入り、主の読み手では上流と一致するようになった。守り専用の読み手の表では「守り専用の読み方」として残す（PR2 で publish の守りが主の読み手へ移り、この読み手は使われなくなった）。

- csvfile.ReadPowerShellWhole は列名の重複を確かめない（行単位の読み手が先に確かめる）。上流は、データ行が0件でも ConvertFrom-Csv の例外で止まる。主の読み手は、データが0件でも DuplicateColumnError を返す（dup-columns-with-data、dup-columns-no-data、dup-columns-blank-after）。

行単位の読み手（csvfile.ReadPowerShellTable）の違い（行をまたぐ値が最初の行で切れる、閉じない引用符が行の終わりで閉じる、全角空白や NO-BREAK SPACE だけの行がレコードになる、データ行の無いファイルで列名の重複を確かめない）は、「行単位の読み方」として同じ表に載せてある。

publish の試験（cmd/dwloc）では、PR0 と PR1 のあいだ、publish が全体を解釈する読み手へ移る PR2 で変わる箇所を「PR2 で変わる」として表に載せていた。PR2 で publish が全体を解釈して読むようになり、その行はすべて次のどちらかへ移し、分類そのものを無くした。

- 上流と同じバイトと集計になり、表から外したもの: 行をまたぐ訳・原文・台詞ID行の訳（ml-translation-lf、ml-translation-crlf、ml-translation-blank-lines、ml-translation-only-newline、ml-source-translated、ml-published-crlf、ml-published-translation、ml-hash-line-in-translation、ml-hash-line-in-published、ml-line-id-translation）、訳の空の行をまたぐレコード（ml-source-real-shape、ml-source-mixed-lines）、表計算ソフトで保存し直した形（ml-source-crlf-key-mismatch）、データ行の無いファイルの列名の重複（dup-columns-no-data）、全角空白や NO-BREAK SPACE だけの行（ideographic-space-line、nbsp-line）。
- 止めることを意図して、上の表へ移したもの: 飲み込み（swallow- の8件）、正しい複数行の値でも飲み込みの確かめが当たる形（ml-continuation-looks-like-row、ml-source-translated-2col）、単独の CR（lone-cr-in-quoted-translation、lone-cr-unquoted-value）、'#' で始まるヘッダー（hash-header-quoted、hash-header-leading-space）。

引用の中の空白か NO-BREAK SPACE の後ろに '#' があるヘッダー（hash-header-quoted-space、hash-header-nbsp）は、上流も飛ばさずにその名前の列にして訳を書くので、dwloc も止めずに同じバイトを書き、表に無い。

集計の1行は項目ごとに比べる（converted、already hashed、per-line、malformed dropped、in play order、other）。kept from the published file は dwloc の集計の1行に無いので比べない（上の表の集計の1行の行）。上流 main の集計の1行は、R28 に書いた 003ed1e の8項目に、この項目を足した9項目である。項目を比べないことは決めてあるが、この項目が数える処理（いまの公開ファイルにだけある行の引き継ぎ、上流の #10）に追従するかは、次の「上流と違うが未決の点」に置く。

### 上流と違うが未決の点

次の点は上流と違うが、違えてよいかをまだ決めていない。試験（cmd/dwloc/publish_upstream_test.go）は「未決」として、いまの振る舞いと違い方を固定するだけで、正しいとはしない。「上流と意図して違える点」と分けるのは、仕様の上で決まったように読まれないためである。決まったら「上流と意図して違える点」へ移すか、直して試験の表から外す。

2つの表の最後の列（試験の入力）は、publish の試験（TestPublishDiffKindsMatchPortSpec）が読む。publish の試験で「意図して違える」に置いた入力は「上流と意図して違える点」の表に、「未決」に置いた入力はこの表にだけ載っていなければ落ちる。分類を取り違えても違い方の比べ方は変わらず、ほかの試験では気付けないためである。

| 項目 | 上流 main | いまの dwloc | 決めること | 試験の入力 |
| ---- | ---- | ---- | ---- | ---- |
| key 列の英文（上流の #9） | ハッシュにして公開する | その行を捨てる。いまの公開ファイルにある訳が失われるとして止める | 追従するか。追従するなら、16進らしい打ち間違いのキーをどう扱うか。全体を解釈する読み手へ移す作業（PR0〜PR4）の範囲外で、この作業では変えない | english-in-key-column |
| いまの公開ファイルにだけある行（上流の #10） | 引き継ぐ。集計の1行に kept from the published file として数える | 引き継がない。訳が失われるとして止める | 追従するか（上流どおり引き継ぐ、作業コピーに無い行だけ引き継いで訳を空にした行では止める、いまのまま、など）。PR0〜PR4 の範囲外で、この作業では変えない | published-row-missing-from-working |

PR0 では「引用符で囲まない値の中の単独の CR」もこの表に置いていた。PR1 の時点で PR2 で止めると決まったので、「上流と意図して違える点」の表へ移した。PR2 で止めるようにした。

### 未決の点

- 出力の改行コードを CRLF と LF のどちらに固定すべきか。スクリプトは Environment.NewLine 依存（Windows で CRLF）だが、リポジトリの blob は LF（core.autocrlf=input）。Go は環境非依存なので、どちらかに決め打つ必要がある。仕様としてどちらが「正」なのかはコードからは判断できない。
  - **確定（LF）**: 上流の hash-strings.ps1 は f816618（2026-09-16）で、出力を StringBuilder.AppendLine から StringWriter（`` $out.NewLine = "`n" ``）に変え、Windows でも LF で書くようになった。コメントは「the file is LF on every platform, matching the repository (see .gitattributes)」としている。移植の LF 固定（csvfile.LineTerminator）と一致するので、この点は未決ではなくなった。上流の調査（上流の報告 #12）で、上流 main の16ロケールを上流の pwsh 版と dwloc publish で一括変換し、どちらもコミット済みとバイト単位で一致した（冪等）。以前の食い違い（003ed1e 版は Windows で CRLF を書く）は、上流の側で解消した。
- Translations/_discovered/<locale>.working.csv の正確な列構成。ディレクトリが現リポジトリに存在せず実例を確認できなかった。ドキュメンテーションコメントには "a working copy from the in-game menu, or plain source_en,translation rows" とあるので、少なくとも source_en と translation、おそらく key と speaker も持ちうる、と読めるが未検証。
- PowerShell 由来の大文字小文字の扱い（Section-Title の switch、$levels.ContainsKey、$rows.ContainsKey、lineRows.Contains、$e.section -ne $lastSection、-notlike '_*'）が意図的な仕様なのか、単に PowerShell の既定を使った結果なのか。Go に移す際に case-sensitive にすると挙動が変わりうるが、現行データでは差が出ない。厳密移植なら case-insensitive、意図なら case-sensitive のどちらを選ぶか要判断。
- speakers の重複判定 List<string>.Contains が case-sensitive である一方、他のマップが case-insensitive である点が意図的かどうか。`Ryan` と `ryan` が両方 script_order にあれば `Ryan/ryan` と連結されるが、現行データでは発生しない。
- `$_.StartsWith('#')` は .NET の既定でカルチャ依存比較（StringComparison.CurrentCulture）。'#' のような ASCII 記号では実害は考えにくいが、厳密には Ordinal と異なる。Go の strings.HasPrefix（Ordinal相当）で置き換えてよいと判断したが、これは推測。
- `[int]$l.level` が非数値だった場合の挙動（例外で全体停止）が意図的な仕様か、あるいはその行をスキップすべきかは不明。$ErrorActionPreference='Stop' から「停止」が既定挙動だと読んだが、明示的な設計意図はコードに書かれていない。
- Get-ChildItem のディレクトリ列挙順（実質は名前順）が出力内容に影響しないことは確認したが、ログ出力の順序としてこの順を再現する必要があるかは不明。
- Escape-Csv が '#' や先頭/末尾の空白をエスケープしないことが意図的な割り切りなのか、単なる見落としなのかは判断できない。訳文が '#' で始まると往復で失われる潜在的な不具合がある（現行データには該当なし）。
- `# =====` / `# ---` という打ち切りマーカーの前方一致は、末尾の ` =====` や ` ---` を確認していない。`# ----- 何か` のような別形式のコメントも `# ---` に一致して打ち切られるが、これが意図かは不明。

## 形式検証

この節の R1〜R24 と境界条件は、003ed1e の`tools/check-translations.py`から抽出したものである。その後の上流の変更（f816618、c8fda90、cc01bfc、912f519）で読み方が変わり、検査が増えた。dwloc は上流の dev に合わせてある。変わった点はこの節の最後の「上流 dev への追従」にまとめた。食い違う箇所は、そちらが正しい。

### データ構造

#### KeptLine

```text
OrigLineNo int（1始まりの物理行番号）, Text string（改行文字を含む生の行。newline="" のため \r\n は保持される）
```

Python では `kept: list[tuple[int, str]]`。ここから `origin []int` を切り出して報告用行番号の復元に使う。

#### Header / AcceptedHeader

```text
[]string。受理される3種のみ: (1) [key section node order speaker translation] (2) [key speaker translation] (3) [key translation]
```

比較は完全一致。正規化・トリム・大文字小文字無視は一切なし。`col map[string]int`（列名→添字）と `width int`（= len(header)）を派生させる。

#### Row

```text
[]string。Key = row[0]、Translation = row[len-1]。6列ヘッダー時のみ Section = row[col["section"]]（添字1）, Node = row[col["node"]]（添字2）。order（添字3）と speaker（添字4）は未検証。
```

列名ではなく位置アクセス。len(row) != width の行は以降の検証をすべてスキップ。

#### Problem

```text
string 1本のみ（構造体ではない）。`{display(path)}[:{lineno}]: {message}` 形式に整形済みの文字列を list に積む。
```

重大度や種別の区別は持たない。件数がそのまま終了コード判定に使われる。

#### SeenKeys

```text
map[string]int（key → 最初に出現した報告用行番号）
```

`seen.setdefault(key, n)` なので最初の行番号のみ保持。フィールド数不一致の行は登録されない。

#### Patterns（定数）

```text
KEY = ^[0-9a-f]{16}$ / LINE_ID = ^line:[A-Za-z0-9_.\-]{1,59}$ / IDENT = ^(?:[A-Za-z0-9_]*|L\d\d [A-Za-z]+|UI)$
```

いずれも `re.match` で使用。`^` があるため実質 fullmatch だが、Python の `$` は末尾の単一 \n を許すため Go と挙動が異なる（R18/R21 参照）。

#### Paths（定数）

```text
ROOT = <script>/../.. , TRANSLATIONS = ROOT/Translations, 検査対象 = TRANSLATIONS/<locale>/strings.csv, 禁止 = TRANSLATIONS/_discovered/* と TRANSLATIONS/<locale>/strings.local.csv
```

ロケールディレクトリの条件は「ディレクトリであり、名前が '_' で始まらない」。実在例: de, eo, es, fr, he, ja, ko, pl, pt-BR, ru, tok, zh-Hans, zh-Hant。Translations/ignore.txt は通常ファイルなので無視される。

### 規則

#### R1. ROOT はスクリプト自身の実体パスの2階層上、TRANSLATIONS は ROOT/"Translations"。

`ROOT = Path(__file__).resolve().parent.parent`、`TRANSLATIONS = ROOT / "Translations"`。スクリプトは tools/ 配下にあるのでリポジトリルートを指す。`resolve()` なのでシンボリックリンクも解決される。

**Goでの注意**: Goでは実行ファイル位置ではなく、ソース相対の概念がない。`os.Executable()`+`filepath.EvalSymlinks` か、CLIフラグ/カレントディレクトリ起点でリポジトリルートを決める必要がある。挙動差になるので方式を明示すること。

#### R2. display(path) はリポジトリ相対のPOSIX表記を返し、ROOT配下でなければ絶対パス文字列をそのまま返す。

```
try:
    return path.resolve().relative_to(ROOT).as_posix()
except ValueError:
    return str(path)
```
`as_posix()` なので Windows でも `Translations/ja/strings.csv` と区切りは `/`。全メッセージの先頭 `{name}` はこの関数の戻り値。

**Goでの注意**: Goでは `filepath.Rel(root, p)` の結果を `filepath.ToSlash()` に通す。`filepath.Rel` は `..` を含む結果も成功で返すので、`strings.HasPrefix(rel, "..")` を ValueError 相当として扱う必要がある。

#### R3. main はまず Translations/_discovered を走査し、直下の「ファイル」で git 追跡されているものを問題として報告する。サブディレクトリは再帰しない。

```
discovered = TRANSLATIONS / "_discovered"
if discovered.is_dir():
    for f in sorted(discovered.iterdir()):
        if f.is_file() and is_tracked(f):
            problems.append(f"{display(f)}: must not be committed (contains source text)")
```
`_discovered` が存在しない、またはディレクトリでない場合はこのブロック全体をスキップ（エラーにしない）。`iterdir()` は直下のみで再帰なし。`f.is_file()` なのでサブディレクトリは無視される。

**Goでの注意**: `os.ReadDir` は名前の昇順（バイト順）でソート済み。Python の `sorted(Path...)` は Windows では大文字小文字を無視した比較（PurePath は `_str_normcase` で比較）、POSIX では大文字小文字を区別する。現状のリポジトリに `_discovered` は存在しない（未作成）ため、実運用での並び順の差は表面化しにくい。

#### R4. 次に Translations 直下を sorted 順に走査し、ディレクトリでないもの、および名前が "_" で始まるものをスキップする。

```
for locale_dir in sorted(TRANSLATIONS.iterdir()):
    if not locale_dir.is_dir() or locale_dir.name.startswith("_"):
        continue
```
これにより `Translations/ignore.txt`（実在する通常ファイル）と `Translations/_discovered` がロケール走査から除外される。`TRANSLATIONS` 自体が存在しない場合 `iterdir()` は FileNotFoundError を送出し、例外は捕捉されていない（トレースバックで異常終了）。

**Goでの注意**: `os.ReadDir` は名前昇順を保証。`d.IsDir()` はシンボリックリンクを辿らないので、Python の `Path.is_dir()`（辿る）と差が出る。ディレクトリへのシンボリックリンクを想定するなら `os.Stat` を使う。

#### R5. 各ロケールディレクトリで strings.local.csv が存在しかつ git 追跡下なら問題として報告する。

```
local = locale_dir / "strings.local.csv"
if local.exists() and is_tracked(local):
    problems.append(f"{display(local)}: must not be committed (contains source text)")
```
メッセージ文言は _discovered のものと完全に同一（`: must not be committed (contains source text)`）。存在しないだけなら問題なし（ローカル作業用コピーは許容）。

**Goでの注意**: `os.Stat` の `err == nil` を `exists()` 相当にする。

#### R6. strings.csv が存在すれば check_file の結果を problems に連結し、存在しなければロケールディレクトリ自体に対する「no strings.csv」を1件報告する。

```
strings = locale_dir / "strings.csv"
if strings.exists():
    problems.extend(check_file(strings))
else:
    problems.append(f"{display(locale_dir)}: no strings.csv")
```
後者のメッセージの `{name}` はファイルではなくディレクトリの表示パス（例 `Translations/ja: no strings.csv`）。

**Goでの注意**: —

#### R7. is_tracked は `git ls-files --error-unmatch <絶対パス>` を ROOT をカレントとして実行し、終了コード0のときのみ true。OSError 時のみ path.exists() にフォールバックする。

```
import subprocess
try:
    out = subprocess.run(
        ["git", "ls-files", "--error-unmatch", str(path)],
        cwd=ROOT, capture_output=True, text=True,
    )
    return out.returncode == 0
except OSError:
    return path.exists()
```
重要な点: (a) `check=True` を付けていないので非ゼロ終了で例外にならない。(b) 標準出力・標準エラーは捨てられ、一切表示されない。(c) `str(path)` は絶対パス（ROOT が resolve 済みのため）。(d) git 実行ファイルが無い等の OSError（FileNotFoundError は OSError のサブクラス）でのみ `path.exists()` にフォールバックする＝「gitが無い環境では存在するだけでコミット済み扱い」になる。(e) git がありリポジトリでない場合は非ゼロ終了なので false（追跡されていない扱い）。

**Goでの注意**: `exec.Command("git", "ls-files", "--error-unmatch", abs).Dir = root` とし、`cmd.Run()` の戻りが `*exec.ExitError` なら false、`exec.ErrNotFound`/`*fs.PathError` 系なら `os.Stat` フォールバック、という切り分けが必要。`exec.LookPath` の失敗を OSError 相当に対応させる。

#### R8. check_file はファイルを encoding="utf-8-sig"、newline="" で開く。

`with io.open(path, encoding="utf-8-sig", newline="") as f:`
- `utf-8-sig`: 先頭にUTF-8 BOM (EF BB BF) があれば読み捨てる。BOMが無くても正常に読める。BOM除去は「ファイル先頭のみ」。不正なUTF-8バイト列は UnicodeDecodeError（未捕捉、異常終了）。
- `newline=""`: 改行変換を行わない。行末の `\r\n` は `\r\n` のまま各行文字列に残る。ただし行分割自体は `\n` / `\r` / `\r\n` すべてを終端として認識する。これは csv モジュールが要求する開き方で、引用フィールド内に埋め込まれた改行を破壊しないため。

**Goでの注意**: Goでは `bytes.TrimPrefix(data, []byte{0xEF,0xBB,0xBF})` でBOMを除去。行分割は `\r\n` を保持したまま行う必要がある（`bufio.Scanner` の ScanLines は `\r` を落とすので使えない）。独自に `\n` 区切りで分割しつつ `\r` を保持し、単独 `\r` 改行も終端扱いにするのが忠実。実ファイル（ja/he）はいずれもLFのみなので、CRLF対応は保険。

#### R9. CSVパーサに渡す前に「strip して空になる行」と「'#' で始まる行」を物理行単位で除去する。

```
kept = [(i, line) for i, line in enumerate(f, start=1) if line.strip() and not line.startswith("#")]
```
- `i` は1始まりの物理行番号。
- `line.strip()` が偽 → 除去（空行、空白のみの行、`\r\n` のみの行）。
- `line.startswith("#")` が真 → 除去。先頭に空白があると `#` でも除去されない（` # x` は残る）。
- 判定順は「空でない」かつ「#で始まらない」の両方。
- この除去は物理行単位なので、引用フィールド内の改行をまたぐ行にも無差別に適用される（R-EdgeCase参照）。

**Goでの注意**: 実装は素直に写せる。`strings.TrimSpace` は Unicode 空白を落とすが、Python の `str.strip()` も引数なしで Unicode 空白を落とすのでほぼ一致する（完全一致ではない。差異は openQuestions 参照）。

**上流 dev での変更**: f816618 と c8fda90 で、この物理行の除去は無くなった。空白だけの行は1フィールドのレコードとして残り、コメントは引用符の偶奇で決める。下の「上流 dev への追従」を参照。

#### R10. origin は kept の物理行番号だけを取り出した0始まり配列で、報告用行番号の復元に使う。

`origin = [i for i, _ in kept]`、`reader = csv.reader(line for _, line in kept)`。csv.reader には物理行番号を持たない行テキストだけを渡すので、行番号の対応付けは origin が担う。

**Goでの注意**: Goでは `[]int` と `[]string` の2本、または構造体スライスで持つ。

#### R11. kept が空（＝実質的な内容行が1行も無い）なら「empty file」1件だけを返す。

```
try:
    header = next(reader)
except StopIteration:
    return [f"{name}: empty file"]
```
メッセージに行番号は付かない（`{name}: empty file`）。ファイルサイズ0だけでなく、全行がコメント・空行の場合もここに来る。

**Goでの注意**: —

**上流 dev での変更**: f816618 以降、ここに来るのは「コメントと完全な空行だけ」のファイルである。空白だけの行が1本でもあれば、それがヘッダーとして比べられて R13 になる。その前に、CSV として読めなければ「could not be parsed as CSV」の1件を返す。

#### R12. ヘッダーは3種類のみを受理する。リストの完全一致（順序・大文字小文字・空白すべて厳密）。

```
accepted = (
    ["key", "section", "node", "order", "speaker", "translation"],
    ["key", "speaker", "translation"],
    ["key", "translation"],
)
if header not in accepted:
```
`header` は csv.reader が返す文字列リスト。比較は Python のリスト等価比較なので、要素数・順序・各文字列が完全一致する必要がある。前後空白のトリムや大文字小文字の正規化は一切行わない。BOMは R8 の utf-8-sig で既に除去済みなので `﻿key` にはならない。docstring が言う「source_en 列があると英語原文混入の印」は専用チェックではなく、この完全一致判定によって弾かれる（`key,source_en,translation` は accepted に無い）。

**Goでの注意**: Goでは `[]string` の比較に `slices.Equal` を使う。3候補を順に比較。

#### R13. ヘッダー不一致なら専用メッセージを1件だけ積んで即 return し、行の検証は一切行わない。

```
problems.append(
    f"{name}: header is {header!r}; the published file must be "
    f"'key,section,node,order,speaker,translation' (run tools/hash-strings.ps1 before committing)"
)
return problems
```
`{header!r}` は Python のリスト repr。例: `['key', 'source_en', 'translation']`（要素はシングルクォート、区切りは `, `、全体を `[]`）。メッセージに行番号は付かない。

**Goでの注意**: Go で repr を再現するには `"['" + strings.Join(header, "', '") + "']"` 相当を自作する。ただし要素に `'` やバックスラッシュ、非ASCII制御文字が含まれると Python の repr はエスケープ規則が変わる（openQuestions 参照）。空リストの場合 Python は `[]`。

#### R14. ヘッダー受理後、列名→添字の辞書 col と列数 width を作り、重複検出用の seen 辞書を初期化する。

```
col = {column: i for i, column in enumerate(header)}
width = len(header)
seen = {}
```
`col` は section/node の存在判定と添字取得に使う。`seen` は key→最初に出現した報告用行番号。

**Goでの注意**: `map[string]int` と `map[string]int`。Go の map は反復順序が不定だが、ここでは反復しないので問題ない。

#### R15. 各行の報告用行番号 n は「その行を読む直前までに reader が消費した kept 行数」を origin の添字にして求める。引用フィールドが複数行にまたがる場合は行の先頭行を指す。

```
consumed = reader.line_num   # ヘッダー読み込み直後
for row in reader:
    n = origin[consumed]
    consumed = reader.line_num
```
`reader.line_num` は「基になるイテレータから引いた行数（1始まりの累計）」。origin は0始まりなので、直前の line_num をそのまま添字にすると「次の行の先頭」を指す。実験で確認済み: kept=[(1,header),(4,'abc,"multi'),(5,'line"'),(6,'def,x')] のとき、`['abc','multi\nline']` は n=4、`['def','x']` は n=6 と報告される。ヘッダー自体が複数行にまたがっていても（line_num=2 等）正しく追随する。行を1つ返すには最低1行消費するため、`origin[consumed]` が範囲外になることは通常発生しない。

**Goでの注意**: Go の `encoding/csv` は `Reader.FieldPos(0)` で行番号を取れるが、それは「パーサに渡した入力内の行番号」なので同じ復元処理が必要。より簡単なのは kept 構築時に各論理行の先頭物理行番号を自前で追跡する実装。忠実移植するなら「直前までの消費行数を保持し、それを origin の添字にする」ロジックをそのまま写すのが安全。

**上流 dev での変更**: f816618 以降は origin を持たず、ファイル全体を csv.reader に通して「直前のレコードの `reader.line_num` + 1」をそのレコードの行番号にする。報告される番号は、いまも各レコードの先頭の物理行である。

#### R16. フィールド数が width と異なる行は「expected N fields, got M」を報告し、その行の他の検証はすべてスキップする（continue）。

```
if len(row) != width:
    problems.append(f"{name}:{n}: expected {width} fields, got {len(row)}")
    continue
```
この continue により、フィールド数不一致の行では key 正規表現・重複・空翻訳・section/node の各チェックが行われず、seen にも登録されない。

**Goでの注意**: Go の `encoding/csv` は既定で最初のレコードの列数に固定し、不一致を `ErrFieldCount` エラーにしてしまう。必ず `reader.FieldsPerRecord = -1` を設定して可変長を許容し、列数チェックは自前で行うこと。

#### R17. key は先頭フィールド、translation は最終フィールド。列名ではなく位置で取る。

`key, translation = row[0], row[-1]`
3種のヘッダーいずれでも translation は最終列なのでこれで一致する。R16 の continue を通過しているので len(row) == width が保証されている。

**Goでの注意**: `row[0]` と `row[len(row)-1]`。

#### R18. key は KEY（16桁の小文字16進）または LINE_ID（line: + 1〜59文字の [A-Za-z0-9_.-]）のいずれかにマッチしなければならない。不一致なら key の値は出力しない。

```
KEY = re.compile(r"^[0-9a-f]{16}$")
LINE_ID = re.compile(r"^line:[A-Za-z0-9_.\-]{1,59}$")
...
if not KEY.match(key) and not LINE_ID.match(key):
    problems.append(f"{name}:{n}: key is not 16 lowercase hex digits or a line ID")
```
確認済みの挙動: 大文字16進 `0123456789ABCDEF` は不可。`line:` のみ（本体0文字）は不可。本体59文字は可、60文字は不可（key 全体の最大長は 5+59=64）。実ファイルでは ja に `line:6046bedf` 形式が41件存在。コメントにある通り、公開リポジトリでキーを晒さないため意図的に key 値をメッセージに含めない。

**Goでの注意**: 重大な差異: Python の `$` は「文字列末尾、または末尾の単一 `\n` の直前」にマッチする。実測で `KEY.match("0123456789abcdef\n")` は True（`\n\n` は False）。Go の `$` は `(?m)` なしなら文字列末尾のみ（`\z` 相当）。忠実に再現するなら Go 側は `^[0-9a-f]{16}\n?$` 相当にするか、そもそも「末尾 `\n` 1個を許容するか」を仕様決定として明示する。引用フィールドで末尾改行を含む値でのみ表面化する。`re.match` は先頭アンカーだが `^` も書かれているので実質 fullmatch 相当（上記 `$` の例外を除く）。

#### R19. 既出の key なら「duplicate key (see line N)」を報告する。N は最初に出現した行番号。記録は setdefault なので最初の行番号のみ保持される。

```
if key in seen:
    problems.append(f"{name}:{n}: duplicate key (see line {seen[key]})")
seen.setdefault(key, n)
```
3回以上出現した場合、2回目も3回目も参照先は常に「1回目の行番号」。重複していても key 正規表現チェック・空翻訳チェックは独立に実行される（continue しない）。key が正規表現不一致でも seen には登録されるので、不正な key の重複も検出される。

**Goでの注意**: `if first, ok := seen[key]; ok { ... }` のあと `if !ok { seen[key] = n }`。Python の setdefault は既存値を上書きしない点に注意。

#### R20. translation を strip して空なら「empty translation」を報告する。

```
if not translation.strip():
    problems.append(f"{name}:{n}: empty translation")
```
空文字列だけでなく、空白・タブ・改行のみの翻訳も空とみなす。

**Goでの注意**: `strings.TrimSpace(v) == ""`。Python の `str.strip()` は Unicode 空白全般を落とす。Go の `TrimSpace` も `unicode.IsSpace` ベースだが、対象集合が完全一致しない（openQuestions 参照）。

#### R21. ヘッダーに section / node 列がある場合のみ、その値を IDENT 正規表現で検証する。チェック順は section → node。

```
IDENT = re.compile(r"^(?:[A-Za-z0-9_]*|L\d\d [A-Za-z]+|UI)$")
for column in ("section", "node"):
    if column in col and not IDENT.match(row[col[column]]):
        problems.append(f"{name}:{n}: {column} does not look like an identifier")
```
IDENT の3択:
1. `[A-Za-z0-9_]*` — 空文字列も可（実ファイルに `UI,,,UI,...` のように node/order が空の行が実在）。
2. `L\d\d [A-Za-z]+` — 'L' + 数字ちょうど2桁 + 半角スペース1個 + ASCII英字1文字以上。実ファイルの `L01 Ryan`, `L15 Alexander` がこれ。
3. `UI` — 実質1番目に含まれるので冗長。
実測で確認: 空文字=可、`L01 Ryan`=可、`L01  Ryan`（スペース2個）=不可、`L01 Ryan Extra`=不可、`pt-BR`（ハイフン）=不可。
この検証は6列ヘッダーのときだけ発動する（3列・2列ヘッダーには section/node が無いので col に入らずスキップ）。order 列と speaker 列はどのヘッダーでも一切検証されない。

**Goでの注意**: 重大な差異: Python の `\d` は str パターンでは Unicode の十進数字すべてにマッチする。実測で `IDENT.match("L٠١ Ryan")`（アラビア数字）は True。Go の `\d` は ASCII `[0-9]` のみ。忠実にするなら Go 側で `[\p{Nd}]{2}` を使う。加えて `$` の末尾 `\n` 許容（R18の goNote）もここに適用され、実測で `IDENT.match("UI\n")` は True。

#### R22. 問題は収集順（_discovered → ロケール昇順、各ロケール内は strings.local.csv → strings.csv の行順）に、1件1行で標準出力へ出す。

```
for p in problems:
    print(p)
```
check_file 内でも append 順は「フィールド数 → key正規表現 → 重複 → 空翻訳 → section → node」なので、同一行で複数問題があればこの順で並ぶ。

**Goでの注意**: —

#### R23. 問題があれば空行＋「N problem(s).」を出して終了コード1、なければ「translations OK」を出して終了コード0。

```
if problems:
    print(f"\n{len(problems)} problem(s).")
    return 1
print("translations OK")
return 0
```
`f"\n..."` なので、最後の問題行の後に空行が1行入り、その次に `12 problem(s).` のような行が来る（末尾はピリオド、`(s)` は常に付く＝1件でも `1 problem(s).`）。成功時の文字列は厳密に `translations OK`。`sys.exit(main())` で終了コードに反映。現行リポジトリで実行すると `translations OK` / exit 0 を確認済み。

**Goでの注意**: Go では `fmt.Println()` で空行、`fmt.Printf("%d problem(s).\n", n)`、`os.Exit(1)`。

#### R24. 行番号付きメッセージの体裁は一律 `{表示パス}:{行番号}: {本文}`、行番号なしは `{表示パス}: {本文}`。

全メッセージ一覧（本文のみ）:
- `empty file`（行番号なし）
- `header is {repr}; the published file must be 'key,section,node,order,speaker,translation' (run tools/hash-strings.ps1 before committing)`（行番号なし）
- `expected {width} fields, got {len(row)}`
- `key is not 16 lowercase hex digits or a line ID`
- `duplicate key (see line {最初の行番号})`
- `empty translation`
- `section does not look like an identifier`
- `node does not look like an identifier`
- `must not be committed (contains source text)`（行番号なし、_discovered 配下と strings.local.csv 共通）
- `no strings.csv`（行番号なし、ディレクトリパスに対して）

**Goでの注意**: CIログの差分を避けたいなら文言をバイト単位で一致させること。

### 境界条件

- BOM: utf-8-sig はファイル先頭の EF BB BF のみ除去する。行途中や2個目のBOMは普通の文字として残り、ヘッダー完全一致に失敗して R13 のヘッダーエラーになる。
- 改行コード: newline="" により \r\n は行文字列に保持される。行分割は \n / \r / \r\n すべてを終端として認識するため、旧Mac形式の単独 \r もそれぞれ別の物理行として数えられる。実ファイル ja/he は LF のみ。
- 空白のみの行: `line.strip()` が偽になるので空行と同様に除去される。物理行番号の欠番になるだけで問題としては報告されない。（f816618 以降は1フィールドのレコードとして残り、「expected N fields, got 1」かヘッダー不正になる）
- コメント判定は先頭1文字のみ: `line.startswith("#")`。先頭に空白がある ` # foo` はコメントとみなされず、CSV行として解釈されてフィールド数不一致などになる。
- 引用フィールドが複数行にまたがるケースで、その途中の行が空行または '#' 始まりだと、R9 の前処理がその物理行を丸ごと落としてしまい、CSVの構造が壊れる。実測でこの前処理はCSV構文を見ずに物理行単位で動く。現行の ja/he には複数行引用フィールドは存在しない（引用は8件すべて1行内、`""` エスケープ付き）が、翻訳者が改行入り翻訳を書くと発生しうる。（c8fda90 以降は、引用の途中の空行と '#' 行は値の一部として残る）
- 複数行にまたがる引用フィールドを含む行の報告行番号は「その行の先頭物理行」を指す。実測で kept=[(1,header),(4,'abc,"multi'),(5,'line"'),(6,'def,x')] のとき n=4 と n=6 になる。
- ヘッダー自体が複数行にまたがっていても `consumed = reader.line_num` の初期化で正しく追随する。
- 全行がコメントまたは空行のファイルは「empty file」（サイズ0と同じ扱い）。（f816618 以降、空白だけの行はここに含まれない）
- 空の section / node は IDENT の `[A-Za-z0-9_]*` にマッチするので合法。実ファイル he の末尾に `e2b9759e2326caba,UI,,,UI,...` が存在し、node と order が空。
- order 列と speaker 列は一切検証されない。speaker に英語原文が入っていても検出されない。
- docstring の「contain no English source text (a source_en column is the tell)」に対応する専用コードは存在しない。source_en 列は R12 のヘッダー完全一致判定に落ちることで間接的に弾かれるだけで、他の列に英文が入っていても検出されない。
- フィールド数不一致の行は continue するため、その行の重複キーも空翻訳も section/node も検出されず、seen にも登録されない（後続の同一キー行が「重複」として報告されなくなる）。
- 重複キーが3回以上出た場合、2回目以降のすべてのメッセージが「1回目の行番号」を指す（setdefault のため）。
- git が PATH に無い（OSError）場合、is_tracked は `path.exists()` にフォールバックするため、_discovered 配下や strings.local.csv が「存在するだけ」で誤って「コミットされている」と報告される。
- git はあるがリポジトリ外で実行した場合、`git ls-files` は非ゼロ終了なので false（追跡されていない扱い）になり、検出漏れになる。stderr は捨てられるので原因が画面に出ない。
- Translations ディレクトリが存在しない場合、`TRANSLATIONS.iterdir()` が FileNotFoundError を送出して未捕捉のままトレースバック終了する（終了コードは1だが出力形式が異なる）。
- Translations/_discovered のサブディレクトリは再帰されない。1階層下に置かれた英語原文ファイルは検出されない。
- 不正なUTF-8バイト列を含むファイルは UnicodeDecodeError で未捕捉のまま異常終了する。
- 1件だけの場合も `1 problem(s).` と出る（単複の出し分けなし）。
- key が正規表現に不一致でも、その行の重複チェック・空翻訳チェック・section/node チェックは継続して実行される。
- LINE_ID の本体は最大59文字（key 全体で最大64文字）。`line:` のみ（本体0文字）は不正。
- IDENT の `L\d\d [A-Za-z]+` は区切りが半角スペースちょうど1個。実測で `L01  Ryan`（2個）は不可、`L01 Ryan Extra`（語が2つ）も不可。

### 敵対検証で見つかった食い違い

#### [high] 非引用フィールド中の裸の " が Go では「そのファイルの検証を丸ごと打ち切る」致命エラーになる。仕様はこれを rules ではなく openQuestions #11 の「要判断」に落としているため、素直にGoへ移植すると LazyQuotes=false のまま実装され、該当行以降の全問題（重複キー、空翻訳、フィールド数不一致）が一切報告されなくなる。

- 根拠: 元コードは csv の既定 dialect（strict=False）で読むため、`0123456789abcdef,he said "hi"` を `['0123456789abcdef', 'he said "hi"']` として黙って受理し、for ループを継続する（line 48, 68）。Go 1.27 で同じ入力を `encoding/csv` に流すと `parse error on line 2, column 26: bare " in non-quoted-field` を返して ReadAll が中断することを実測で確認した。翻訳者が `he said "hi"` と書くのは十分ありうる入力で、元実装では「問題なし」、Go移植では「以降無検証」と結果が正反対になる。
- 直し方: openQuestions から rules に昇格させ、「Go では csv.Reader.LazyQuotes = true を必須とする」と断定的に書く。実測で LazyQuotes=true なら実ファイル13ロケール全部で Python と結果がバイト一致し、裸の " のケースも Python と一致した。ただし `"x"y"` のような閉じ引用符直後の文字は Python が `xy"`、Go(Lazy) が `x"y` と依然食い違うので、この残差も仕様に明記すること。加えて「パースエラー時にファイル全体を捨てるか1件の problem として報告するか」を決めておく（元コードにパースエラー経路は存在しない）。

#### [medium] 単独 `\r` 改行が Go 側で行終端として失われる。R8 の goNote は「単独 \r 改行も終端扱いにするのが忠実」と書く一方、R15/R16 の goNote は Go の `encoding/csv` を使う前提で書かれており、両者は両立しない。仕様どおりに「\r を保持して行分割 → kept を連結して encoding/csv に渡す」と実装すると、\r 終端は保持された結果ただのデータ文字に戻る。

- 根拠: 元コードは `io.open(..., newline="")`（line 43）なので、ファイルオブジェクトの行分割が `\n` / `\r` / `\r\n` すべてを終端として認識する。実測: `key,translation\r0123456789abcdef,x\r0123456789abcde0,y\r` を Python は3レコードに分けるが、同じ内容を Go の encoding/csv に渡すと `["key","translation\r0123456789abcdef","x\r0123456789abcde0","y"]` の1レコードになる（Go の readLine は `\n` 基準で、単独 `\r` は終端にしない）。レコード数が変われば origin/報告行番号の対応が全面的に崩れ、R15 の復元ロジックも意味を失う。
- 直し方: R8 を「物理行の定義」として rules に独立させ、Go 実装方針をどちらか一方に確定する。(a) 論理レコード分割を自前で行い encoding/csv を使わない、または (b) kept 行の単独 `\r` 終端を `\n` に置換してからパーサに渡し、origin は置換前の分割結果から取る、のいずれかを明記する。「kept をそのまま連結して encoding/csv に渡す」は誤りである旨も書き添える。

#### [medium] `str.strip()` と `strings.TrimSpace` の空白集合差が、単なる判定の揺れではなく「報告行番号そのもののズレ」に波及する点が書かれていない。仕様は openQuestions で「R9（行の空判定）と R20（空翻訳判定）で理論上差が出る」としか述べていない。

- 根拠: R9 の `line.strip()`（line 46）は kept に入る物理行を決め、その kept から origin（line 47）が作られ、すべての `{name}:{n}:` 行番号がそこから復元される（line 71）。実測で Python は `'\x1c'.isspace()` → True、つまり `\x1c` だけの行は strip で空になり除去されるが、Go の `unicode.IsSpace` は U+001C〜U+001F を空白に含めないため同じ行が kept に残る。結果、その行が余計な1レコードとして `expected 6 fields, got 1` を生むうえ、以降の全行の origin 添字が1つずれて別の行番号が印字される。U+0085 と U+00A0 は両者一致することも実測済みなので、実際に差が出るのは U+001C〜U+001F に限られる。
- 直し方: openQuestions ではなく R9 の rule 本文に「空判定は Python の str.isspace() 集合に従う。Go では TrimSpace ではなく U+001C〜U+001F を追加した独自の判定関数を使う」と書き、「この判定が origin と全報告行番号を決める」という影響範囲も明記する。R20 側は verdict のみの影響なので別扱いでよい。

#### [low] Go の `encoding/csv` が引用フィールド内の `\r\n` を `\n` に正規化する件（openQuestions #12）は、R18/R21 の `$` 末尾改行許容と組み合わさると verdict が反転する。仕様は「出力に差が出うる」としか書いていない。

- 根拠: 実測で Python は `['abc', 'multi\r\nline']` のように `\r\n` をそのまま保持するのに対し、Go は `['abc','multi\nline']` を返す。一方 Python の `$` は末尾の単一 `\n` の直前にはマッチするが `\r\n` にはマッチしない（実測: `KEY.match("0123456789abcdef\n")`=True、`KEY.match("0123456789abcdef\r\n")`=False、`IDENT.match("UI\n")`=True、`IDENT.match("UI\r\n")`=False）。したがって CRLF ファイルで section 値が複数行引用になった場合、元実装は `section does not look like an identifier` を報告し、`\n?$` 相当で忠実移植した Go は正規化後に一致して何も報告しない。
- 直し方: R18/R21 の `$` 仕様を決める際に「CRLF 正規化をするかどうか」とセットで決定し、どちらか一方だけ忠実にすると verdict が反転すると明記する。素直なのは「Go 側では `\r\n` を保持し、かつ `$` を `\z` 相当（末尾改行を許容しない）に厳格化する」で、この場合 CRLF/LF どちらでも不一致となり元実装より厳しくなる旨を明示する。

#### [low] R3 の goNote がシンボリックリンクの扱いに触れていない。R4 では `d.IsDir()` の非追従を注意しているのに、R3（`f.is_file()`）と R5（`local.exists()`）には同じ注意がない。

- 根拠: line 112 の `f.is_file()` と line 118 の `local.exists()` は Python では常にリンクを辿る。`os.ReadDir` の DirEntry で `d.Type().IsRegular()` を使うと、ファイルへのシンボリックリンクは false になって検査対象から外れる。`_discovered` 配下の英語原文ファイルがリンクで置かれていた場合、元実装は検出し Go 移植は見逃す（この検査の目的が「原文の混入防止」であることを踏まえると見逃しの方向に倒れるのは望ましくない）。
- 直し方: R3 と R5 の goNote に「Python の is_file()/exists() はリンクを辿るので、Go では DirEntry ではなく os.Stat（Lstat ではない）の結果で判定する」と追記する。

#### [low] R2 の goNote が `resolve()` を落としている。ルール本文は `path.resolve().relative_to(ROOT)` と正しく書いているのに、Go の指針は `filepath.Rel(root, p)` だけで `EvalSymlinks` に触れていない。加えて Windows での大文字小文字の扱いも逆になる。

- 根拠: line 35 は必ず resolve してから relative_to する。Go の `filepath.Rel` はリンクを解決しないため、ROOT 外を指すリンク経由のパスに対して Python は ValueError → 絶対パス文字列を返すのに、Go はきれいな相対パスを返す。さらに Python の `relative_to` は Windows では PurePath の `_str_normcase` 比較（大文字小文字無視）である一方、`filepath.Rel` は素の文字列比較で大文字小文字を区別する。
- 直し方: R2 の goNote に「`filepath.EvalSymlinks` を通してから `filepath.Rel` する」「Windows では大文字小文字無視の比較になる点が異なるので、ROOT を同じ手順で正規化しておく」を追記する。

#### [low] 標準出力のエンコーディングが仕様に一切書かれていない。Python は Windows でリダイレクト時にロケールエンコーディングで書き出すため、非ASCII を含むメッセージで未捕捉の UnicodeEncodeError を起こす。Go は常に UTF-8 を書く。

- 根拠: line 60 の `{header!r}` はヘッダー列名をそのまま repr するため非ASCII が混入しうる（BOM混入や誤ったファイルの検証時）。また display() が返すロケールディレクトリ名も非ASCII になりうる。本セッションの実測で、このマシン上で Python の出力をパイプに流したところ `UnicodeEncodeError: 'cp932' codec can't encode character 'א'` が発生することを確認した。元実装は「問題を報告して exit 1」ではなくトレースバック終了になる。
- 直し方: 出力エンコーディングを仕様に1項目として追加し、「Go は常に UTF-8。元実装は Windows のロケール依存で非ASCII時にクラッシュしうるが、そこは意図的に厳格化して UTF-8 固定とする」と決めを書く。CI ログのバイト一致を要件にするなら、この点だけは一致させられないことも明示する。

#### [low] Python の csv モジュール固有のハード制限（フィールド長上限、NUL バイト）が仕様に無い。

- 根拠: `csv.field_size_limit()` の既定は 131072 文字で、これを超えるフィールドは `_csv.Error: field larger than field limit` を送出する（未捕捉）。また行に NUL が含まれると `_csv.Error: line contains NUL` になる。いずれも line 48/68 の経路で発生し、元実装はトレースバック終了する。Go の `encoding/csv` にはどちらの制限も無く、正常にパースして検証を続ける。
- 直し方: edgeCases に2項目追加し、「Go 側では制限を再現しない（＝元実装より寛容になる）」ことを意図的な決定として記録する。CI での見え方（クラッシュ vs 正常終了）が変わる点も添える。
- その後: 上流の f816618 が csv.Error を捕まえて1件の問題にするようになったため、dwloc もフィールド長の上限を再現した（下の「上流 dev への追従」）。NUL は Python 3.11 以降エラーにならず、上流の CI（3.12）でも dwloc でもただの文字として読む。

### 抽出が取りこぼしていた規則

- 重複キー判定は正規化なしの完全一致（大文字小文字を区別し、前後空白もトリムしない）。KEY は小文字16進固定なので影響は薄いが、LINE_ID は `[A-Za-z0-9_.-]` で大文字を許すため `line:Abc` と `line:abc` は別キーとして扱われる。Go の map[string]int は同じ序数比較なので一致するが、仕様として明示されていない。
- `seen` は check_file のローカル変数（line 66）なので重複検出はファイル単位。ロケール間で同じキーが出ても重複としては報告されない。「per-file スコープ」が明示されていない。
- key / section / node は一切トリムされずにそのまま正規表現へ渡される。トリムされるのは translation の空判定（line 84）だけで、しかもトリム結果は捨てられ値は書き換わらない。
- ヘッダー行そのものは data row としての検証を一切受けない。next(reader) で消費された後、フィールド数チェックも重複チェックも空翻訳チェックも走らず、seen にも登録されない（line 50 と line 68 のループが別経路）。
- スクリプトはコマンドライン引数を一切受け取らない。sys.argv を参照する箇所が無く、line 135 は `sys.exit(main())` のみ。R1 の goNote が提案する「CLIフラグで ROOT を決める」は元実装に無い機能の追加であり、その旨の注記が必要。
- 出力はすべて標準出力（line 126, 128, 130）。stderr には何も書かれない。git のstdout/stderrも capture_output で捨てられる（line 100）。Go移植で問題をstderrに出すとCIログの拾い方が変わる。
- Go の csv.Reader.Comment は 0 のままにすること。`r.Comment = '#'` で代用すると R9 と等価にならない（Go はレコード開始位置でしか判定せず、空白のみの行も落とさない。行カウントの扱いも異なる）。仕様に禁止事項として書くべき。
- kept 行を連結して Go のパーサに渡す場合、区切り文字を足さずに `strings.Join(kept, "")` とすること。各行文字列は改行文字を含んだまま保持されている（newline="" のため）ので、`"\n"` で join すると origin の対応が1行ずつずれる。
- `strings.exists()`（line 121）が真でも `io.open`（line 43）が IsADirectoryError / PermissionError を送出しうる。これらは未捕捉でトレースバック終了する。Translations不在や不正UTF-8と同種の扱いを決める必要がある。
- edgeCases の「現行の ja/he には…引用は8件すべて1行内」は ja のみの数字。実測で he は引用符を含む行が407件ある（複数行引用が0件という結論自体は ja/he とも正しい）。引用処理の経路を通る実データ量が実際にはかなり多いため、LazyQuotes の判断材料としてこの数字は訂正しておくべき。

### 未決の点

- `$` の末尾改行許容を Go でどう扱うか。Python の `$` は文字列末尾または末尾の単一 `\n` の直前にマッチし、実測で `KEY.match("0123456789abcdef\n")` と `IDENT.match("UI\n")` はいずれも True。これが意図した仕様なのか Python の副作用なのか、コードからは判断できない。Go で忠実移植（`\n?$` 相当）するか厳格化するかは判断が必要。
- IDENT の `\d` が Unicode 数字にマッチする件。実測で `IDENT.match("L٠١ Ryan")`（アラビア・インド数字）は True。ASCII 限定が意図だと推測されるが根拠はコードに無い。Go では `\d` が ASCII のみなので、意図的に合わせる（`[\p{Nd}]{2}`）か厳格化するかの決定が必要。
- `str.strip()` と Go の `strings.TrimSpace` の空白集合の差。Python の `str.strip()` は `str.isspace()` が真の文字を落とし、U+001C〜U+001F なども含む。Go の `TrimSpace` は `unicode.IsSpace` ベース。R9（行の空判定）と R20（空翻訳判定）で理論上差が出るが、実データで該当するかは未確認。
- `{header!r}` の Python repr を Go でどこまで再現すべきか。通常の列名なら `['key', 'translation']` で済むが、列名に `'` やバックスラッシュ、非ASCII文字、制御文字が含まれると Python の repr はクォート記号の選択やエスケープ規則を変える。CI 出力の完全一致が要件かどうかが不明。
- `sorted(Path)` の順序。Windows では PurePath 比較が大文字小文字を無視し、POSIX では区別する。Go の `os.ReadDir` はバイト順。実在ロケール名はすべて小文字＋ハイフン（pt-BR, zh-Hans は大文字混在）なので順序差が出うる。ただし出力順のみの差で合否には影響しない。この差を許容するかは要判断。
- `is_tracked` の OSError フォールバックが `path.exists()` である理由。コメントは「maintainer keeps an untracked copy locally on purpose」とあるが、git が使えない環境でフォールバックが真逆の結論（存在＝コミット済み扱い）を出すのが意図的か、単なる保守的なフェイルセーフかは不明。Go で同じ挙動にすべきかは要確認。
- `git ls-files` に渡すパスが絶対パスであること（`str(path)`）。Windows では `C:\...` 形式になるが git は受け付ける。Go で `filepath.Rel` した相対パスに変えると挙動が変わりうる（変わらない可能性が高いが未検証）。
- ROOT の決定方法。Python はスクリプト位置から導くが、Go バイナリでは同じ方法が使えない。CLI 引数、カレントディレクトリからの `.git` 探索、ビルド時埋め込みのいずれを採るかは移植側の設計判断であり、元コードからは決まらない。
- Translations ディレクトリ不在時や不正UTF-8時の未捕捉例外を、Go で「パニック相当」にするか「問題1件として報告して exit 1」にするか。元コードは前者だが、CI での見え方は大きく異なる。
- 引用フィールド内の空行・'#' 始まり行が前処理で落ちる件が既知のバグなのか許容された制約なのかが不明。現行データでは表面化していないが、Go 側で先にCSVをパースしてからコメント除去する設計に変えると、この（意図せざる）挙動が消える。（決着: 上流が f816618 と c8fda90 で不具合として直した。dwloc も合わせた）
- Go の `encoding/csv` は既定で `LazyQuotes=false` のため、非引用フィールド内の裸の `"` でエラーになる。Python の csv はこれを黙って受け入れる。互換性のため `LazyQuotes=true` にすべきかは要判断（true にすると別の解析差が生じる）。
- Go の `encoding/csv` は引用フィールド内の `\r\n` を `\n` に正規化する。Python の csv（newline=""）は `\r\n` のまま保持する。翻訳文に CRLF を含む値があった場合に R20 の空判定や出力に差が出うるが、現行データでは未確認。

### 上流 dev への追従

この節だけは抽出結果ではない。003ed1e のあとに上流の`tools/check-translations.py`へ入った変更を、上流の dev（upstream/dev 9249232。origin/dev 63816a0 と`tools/`は同じ）で読み、Python 3.12.3（上流の CI と同じ Ubuntu 24.04）と 3.14.6 で実測して反映した。上の R1〜R24 や境界条件と食い違う場合は、こちらが正しい。

基準は main ではなく dev に置いた。上流はこれまで dev をまとめて main へマージしてきた（56420df、#44）ので、dev の版は次のリリースでも main に入る見込みであり、その版に先に合わせておくためである。上流の現行の文書に、そう約束する記載があるわけではない。翻訳者の Pull Request は、これまでどおりフォークから main へ出す（上流 130d57e、2026-09-23。dev はメンテナーの作業をためる場所）。そのため Pull Request の CI は main の版で走る。main と dev の`tools/check-translations.py`の差は 912f519（credits.txt の状態語）だけで、ほかの変更は main にも入っている。

したがって dwloc は、版の差の分（credits.txt の状態語）だけ main の CI より厳しい。ほかに、下の「写さなかった上流の不具合」にあたる入力（コメント行の引用符など）でも、dwloc だけが問題を報告する。main の CI が通す credits.txt を、dwloc は問題として報告することがある。いまの影響は小さい。upstream/main 56420df には credits.txt が1件も無く（upstream/dev は16件）、上流 dev の CONTRIBUTING は credits.txt をメンテナーが更新するものとしている。

判定が割れる入力は、`internal/validate/upstream_test.go`に表として固定した。環境変数`DWLOC_UPSTREAM_CHECKER`に上流のスクリプトを渡すと、上流を実際に走らせて突き合わせる。

#### 読み方の変更（f816618、c8fda90）

| 項目 | 003ed1e まで | いま（上流 dev と dwloc） |
| ---- | ---- | ---- |
| 空白だけの行 | 物理行ごと落とす（R9） | 1フィールドのレコードとして残る。ヘッダーより上にあればヘッダー不正（R13）、下にあれば「expected N fields, got 1」 |
| 完全な空行 | 物理行ごと落とす（R9） | 0フィールドのレコードとして捨てる。行番号は消費する |
| コメント | 行頭が '#' の物理行を落とす（R9） | 引用符の偶奇を行をまたいで持ち回り、引用の外で行頭が '#' の行をコメントとする。複数行の引用値の途中にある '#' 行と空行は、値の一部として残る |
| 偶奇の数え方 | なし | 引用符で囲まないフィールド中の裸の '"' も数える。`5" 画面`のあとでは偶奇が反転したままになり、続く '#' 行はデータとして読まれる。コメント行の引用符は数えない |
| CSV として読めない | トレースバックで異常終了 | `{表示パス}:{行番号}: could not be parsed as CSV ({例外の文面})`の1件だけを返し、そのファイルのほかの検査はしない |
| 行番号 | origin 配列（R10、R15） | 直前のレコードの`reader.line_num`+1。報告はいまも各レコードの先頭の物理行 |

CSV として読めないのは、既定の dialect では「フィールドが`csv.field_size_limit()`の既定値 131072 文字を超えたとき」だけ。文面は`field larger than field limit (131072)`になる。数えるのはバイトではなく文字で、引用値の中の改行も1文字に数える。行番号は、超えた文字のある物理行（`reader.line_num`）で、レコードの先頭行とは限らない。コメント行も csv.reader に通すので、長すぎるコメント行でもこの1件になる。NUL は Python 3.11 以降エラーにならない。

#### 写さなかった上流の不具合

- コメント行の引用符が複数行の引用を開く。上流の`comment_lines`は「コメントの引用符はレコードを開かない」前提で偶奇を数えるが、csv.reader はコメント行もレコードとして読む。`# メモ,"開いたまま`のような行があると、閉じるまでの後ろの行がコメントのレコードに飲み込まれて捨てられ、空の訳や重複を見逃す。dwloc はゲーム（CsvReader.cs）と同じく、レコードの境目に来たコメント行をパーサーに渡さずに落とす。引用値の途中（パーサーがまだ前のレコードを読んでいる最中）に来た行は、偶奇の上でコメントでも値として渡す。上流もそこは値として読むので、この場合は一致する
- `comment_lines`の行の数え方。上流は`text.splitlines()`で行番号を振るので、U+2028、U+2029、U+0085、`\v`、`\f`、`\x1c`〜`\x1e`でも行が割れ、csv.reader の行番号（`\r`と`\n`だけで割る）とずれる。コメント行がデータとして検査されたり（誤報）、データ行が検査から漏れたり（見逃し）する。dwloc は`\r\n` / `\n` / `\r`だけで割る
- 00e927e の GitHub Actions の注釈（`::error file=...`）。dwloc は上流の CI で動かないので写さない。標準出力と終了コードは変わらない。なお、環境変数`GITHUB_ACTIONS=true`を立てて上流を走らせると、注釈のパスの先頭に`Translations/`が二重に付く（事前の調査で確認。上流の CI の中で注釈が出ているかは確かめていない）

#### credits.txt（912f519、dev のみ）

`Translations/<ロケール>/credits.txt`があれば、`strings.csv`の次に検査する。`strings.csv`が無いロケールでも見る。

- 文字コードは utf-8-sig、行は Python のテキストモードと同じく`\r\n` / `\n` / `\r`で分ける
- 各行の前後の空白を`str.strip()`と同じく落とし、空の行と '#' で始まる行を飛ばす。前に空白のある ` # x` もコメントになる（strings.csv と違う）
- 残らなければ`{表示パス}: empty; the first line is the status (supervised, proofread, converted, provisional, fun)`
- 最初に残った行を小文字にして、`supervised`、`proofread`、`converted`、`provisional`、`fun`のどれでもなければ`{表示パス}:{行番号}: "{値}" is not a status; use one of supervised, proofread, converted, provisional, fun`。値は repr ではなく、そのまま二重引用符で囲む
- 問題は多くても1件。2行目より後（確かめた人の名前）は見ない
- 小文字にするのは ASCII だけでよい。Python の`lower()`で ASCII になる非 ASCII の文字はケルビン記号（U+212A → k）だけで、状態語に k は無い。Go の`strings.ToLower`は 'İ'（U+0130）を 'i' にして`provİsional`を通してしまうが、Python は2文字の`i̇`にするので通さない

#### textures/（cc01bfc）

`Translations/<ロケール>/textures/`がフォルダーなら、credits.txt の次に検査する。同じ名前のファイルは見ない。

1. 中身を名前順に1つずつ見る。フォルダーなら`{表示パス}: no folders inside textures/`。拡張子（`PurePath.suffix`）を小文字にして`.png`なら絵として覚える。`fallback.txt`ならその場で中身を見る。`credits.csv`でもなければ`{表示パス}: only .png files, credits.csv and fallback.txt belong in textures/`。`.png`という名前だけのファイルは拡張子が空なので、絵ではなくこちらになる
2. 覚えた絵を名前順に見る。拡張子が小文字の`.png`でなければ`use a lowercase .png extension`。8MB（8388608 バイト）を超えれば`{KB} KB, more than 8 MB`（KB はバイト数を 1024 で割って切り捨て）。先頭24バイトが PNG のシグネチャと IHDR でなければ`not a PNG file`で、その絵の残りは見ない。幅か高さが 4096 を超えれば`{幅}x{高さ}, larger than 4096x4096`。大きさの問題と「PNG でない」は同じ絵で両方出ることがある
3. `credits.csv`が無ければ、絵が1枚でもあるときだけ`{textures の表示パス}: credits.csv is missing (file,author,note - one row per picture)`を出して終える
4. `credits.csv`を csv.reader でそのまま読む（コメントは扱わない）。最初のレコードの各フィールドの前後の空白を落として`file,author,note`でなければ`{表示パス}:1: the header must be file,author,note`を出して終える。空のファイルや1行目が空行でもこれになる
5. 2つめ以降のレコードを見る。空か、全フィールドをつないで空白だけなら飛ばす。3列でなければ`expected 3 columns (file,author,note), found {列数}`。3列なら各値の前後の空白を落とし、同じ file が2回目なら`{file} is listed twice`、その名前の絵が無ければ`{file} is not in textures/`、author が空なら`who made {file}? (author is empty)`、note が空なら`say what was done for {file} (note is empty), for example: drawn from scratch, or game texture repainted`
6. 最後に、credits.csv に行の無い絵を名前順に`{表示パス}: no row in credits.csv`

credits.csv の報告の行番号は物理行ではなく「何番目のレコードか」になる。上流が`enumerate(rows[1:], start=2)`で数えるためで、引用値が複数行にまたがるレコードのあとでは物理行とずれる。CI と同じ番号を出すことを優先して写した。

`fallback.txt`は utf-8-sig で読み、`str.splitlines()`で行に分ける（U+2028 や`\v`でも割れる。strings.csv と違い数え方が1通りなので、ずれは生まれない）。各行の前後の空白を落とし、空の行と '#' で始まる行を飛ばす。自分のロケール名なら`{表示パス}:{行番号}: {名前} is this language itself`。'_' で始まる、'/' か '\\' を含む、`Translations/{名前}/strings.csv`が通常のファイルでない、のどれかなら`{表示パス}:{行番号}: {名前} is not a language in Translations/`。

#### 残る差

- 不正な UTF-8。上流は UnicodeDecodeError で異常終了し、dwloc は検査を続ける（従来どおり）
- 読めないファイルと、CSV として読めない`textures/credits.csv`。上流は例外を捕まえずに異常終了（終了コード1、トレースバック）し、dwloc は「検証できません」で終了コード2になる。どちらも失敗として扱われる
- Windows の Python の`sorted()`は大文字小文字を無視して並べる。dwloc はバイト順で、上流の CI（Linux）と同じ。大文字で始まる名前が混ざると、手元の Windows で走らせた上流とは出る順だけが変わる
- `PurePath.suffix`の切り出し方が Python 3.14 で変わった。3.14 は先頭に続く '.' を飛ばしてから最後の '.' を探すので、`..png`と`...png`は空（3.12 は`.png`）、`..PNG`も空（3.12 は`.PNG`）になる。そのため`..png`のような名前では判定が割れる。`..png`という絵に credits.csv の行を付けた木は、上流の CI（3.12）と dwloc では問題なしになる。手元の 3.14 で上流を走らせると`..png: only .png files, credits.csv and fallback.txt belong in textures/`と`credits.csv:2: ..png is not in textures/`の2件になる。dwloc は CI の 3.12 に合わせた。`.a.png`はどちらも`.png`になる。`a.`は 3.14 だけ`"."`になるが、`.png`ではないので判定は変わらない

## CSVとキー生成

### データ構造

#### Record（1レコード）

```text
[]string — 引用解除済みのフィールド値の並び。長さは行ごとに可変（列数チェックなし）。
```

元コードでは List<string>。Parse の戻り値は List<List<string>>（= [][]string）。

#### Row（ReadRows が返す1行）

```text
map[string]string 相当 — キーはヘッダー行のセル値、値はその列の値（欠損列は空文字）。
```

元コードは Dictionary<string,string>(StringComparer.OrdinalIgnoreCase)。Go には大小無視マップが無いので、キーを正規化して保持する型（例: type Row struct { m map[string]string } に Get(name string) string を生やす）にするのが安全。ヘッダーに無い列は保持されない（R13）。

#### ParserState（Parse のローカル状態）

```text
records [][]string / fields []string / field strings.Builder(またはbytes.Buffer) / inQuotes bool / i int / len int
```

元コードの変数名は records, fields, field, inQuotes, i, len。移植時はこの6つをそのまま持てば1対1で追える。

#### TranslationKey（値オブジェクト）

```text
16桁の小文字16進文字列（SHA-256 先頭8バイト）。定数 Length = 16（16進桁数であってバイト数ではない）。
```

Length は「16進桁数」。バイト数は Length/2 = 8。Go で const KeyLength = 16 とする場合、桁数である旨をコメントに残さないと 16 バイトと誤読される。

#### LineId（Yarn の行ID）

```text
"line:" 接頭辞 + [0-9a-zA-Z_-.] の並び。全長6〜64。
```

定数 LinePrefix = "line:"。キー列にはハッシュキーと行IDのどちらも入りうり、呼び出し側（TranslationStore.cs / WorkingCopy.cs）が LooksLikeLineId → LooksLikeKey の順に判定している。

### 規則

#### R1. ファイル全体を一度に文字列へ読み込んでからパースする（ストリーミングではない）。

ReadRows(string path) は `using (var fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))` / `using (var reader = new StreamReader(fs, Encoding.UTF8, true))` で `text = reader.ReadToEnd();` し、ファイルを閉じたあとに `List<List<string>> records = Parse(text);` を呼ぶ。行の yield はその後。CsvReader 内に try/catch は一切なく、I/O 例外はそのまま呼び出し元へ伝播する。FileShare.ReadWrite|Delete は「翻訳者が Excel で開いたままでも読める」ための共有指定（コメント: "an exclusive open would throw"）。

**Goでの注意**: os.ReadFile で全読みし、[]byte をそのままパースすれば等価。Windows の Go の os.Open は既定で共有読み書きなので FileShare 相当の追加指定は通常不要（要検証）。C# は iterator なので最初の MoveNext まで実行されない遅延評価だが、ファイル読み込み自体は最初の1件を返す前に完了しているので、Go で []Row を一括返却しても観測可能な差は出ない。

#### R2. 先頭の BOM は除去され、本文には現れない。不正バイトは例外にせず U+FFFD に置換する。

`new StreamReader(fs, Encoding.UTF8, true)` の第3引数 true は detectEncodingFromByteOrderMarks。UTF-8 BOM(EF BB BF) のほか UTF-16 LE/BE・UTF-32 の BOM も検出してそのエンコーディングに切り替わる。BOM がなければ Encoding.UTF8 として解釈され、Encoding.UTF8 プロパティは置換フォールバックなので不正バイト列は U+FFFD になり例外は出ない。

**Goでの注意**: Go の encoding/csv は BOM を除去しないので、除去しないと1列目のヘッダー名が "﻿key" になりマッチしなくなる。`bytes.TrimPrefix(b, []byte{0xEF,0xBB,0xBF})` を明示的に行うこと。UTF-16/32 BOM 検出まで再現するかは要判断（R-openQuestions 参照）。不正 UTF-8 を U+FFFD にしたいなら strings.ToValidUTF8(s, "�") 相当が必要。

#### R3. Parse は1文字ずつ進む状態機械で、判定順序は (1) inQuotes 中の処理 (2) 行頭 '#' コメント判定 (3) switch('"' / ',' / '\r' / '\n' / default) の固定順である。

状態は `bool inQuotes`、蓄積は `var fields = new List<string>();` と `var field = new StringBuilder();`。ループは `while (i < len) { char c = text[i]; if (inQuotes) { ... continue; } if (c == '#' && fields.Count == 0 && field.Length == 0) { ... continue; } switch (c) { ... } }`。つまり inQuotes が真なら '#' 判定も switch も到達しない。区切り文字は ',' 固定で設定項目はなく、空白のトリムも一切行わない。

**Goでの注意**: この評価順を変えると挙動が変わる（特に '#' 判定が switch より前にある点）。Go でも同じ順序の手書きループにする。文字単位で走査するが、判定対象は ASCII のみなので []byte でバイト単位に走査しても等価（マルチバイト文字は default 節でそのまま蓄積されるため）。

#### R4. 引用符の内側では、'"' 以外のすべての文字（'#'、','、'\r'、'\n' を含む）はただのデータとしてフィールドに追加される。

`if (inQuotes) { if (c == '"') { if (i + 1 < len && text[i + 1] == '"') { field.Append('"'); i += 2; continue; } inQuotes = false; i++; continue; } field.Append(c); i++; continue; }`。つまり引用中の `""` はリテラルの '"' 1個、単独の '"' は引用の終了（この '"' 自体はフィールドに入らない）。引用中の '\r' は捨てられずそのまま入るので、引用内の CRLF 改行は "\r\n" の2文字としてフィールド値に残る。

**Goでの注意**: Go の encoding/csv は readLine 段階で行末 \r\n を \n に正規化するため、引用内の改行が "\n" になってしまい C# と一致しない。手書きパーサなら原文どおり \r を保持すること。

#### R5. '#' がコメント開始とみなされるのは「引用の外」かつ「そのレコードでまだ1つもフィールドを確定していない(fields.Count == 0)」かつ「現在のフィールドバッファが空(field.Length == 0)」のときだけ。それ以外の '#' はただの文字。

`if (c == '#' && fields.Count == 0 && field.Length == 0) { while (i < len && text[i] != '\n') i++; i++; continue; }`。スキップ範囲は次の '\n' まで（その '\n' も i++ で消費する。直前の '\r' もスキップ範囲に含まれる）。'\n' が見つからずEOFに達した場合は i が len になり、さらに i++ で len+1 となってループが終わる。この条件から派生する重要な帰結: (a) `a,#x` の '#' は fields.Count==1 なので通常文字、(b) ` #x`（先頭が空白）は field.Length==1 なので通常文字、(c) 直前に '\r' だけがあった場合は '\r' が field に入らないためコメント判定は成立する、(d) `""#x` のように空の引用フィールドの直後だと field.Length==0 かつ fields.Count==0 のままなのでコメントとして行ごと捨てられる。

**Goでの注意**: Go の csv.Reader.Comment = '#' は「レコード先頭行の最初のルーンが '#' か」で判定するため、上記 (d) を再現できない（Go はレコードとして読んでしまう）。(a)(b)(c) は概ね一致する。厳密一致が要るなら手書きで条件式そのものを移植する。

#### R6. 引用の外に現れた '"' はフィールドの途中であっても引用開始として扱われ、その '"' 自体は出力に含まれない。

switch の `case '"': inQuotes = true; i++; break;`。開始位置の制約が一切ないので、`ab"cd"ef` は `abcdef` に、`"abc"def` は `abcdef` になる。不正な引用でエラーになることはなく、常に何らかの結果を返す。

**Goでの注意**: Go の encoding/csv は LazyQuotes=false だと ErrBareQuote/ErrQuote になり、LazyQuotes=true だと `ab"cd"ef` → `ab"cd"ef`、`"abc"def` → `abc"def` になる。どちらの設定でも C# と一致しないので、この挙動は手書きでしか再現できない。

#### R7. 引用の外の ',' はフィールド区切り。現在のバッファを（空でも）確定して次のフィールドへ進む。

`case ',': fields.Add(field.ToString()); field.Clear(); i++; break;`。空フィールドも必ず1要素として追加される。

#### R8. 引用の外の '\r' は位置を問わず完全に無視される（フィールドにも入らず、状態も変えない）。

`case '\r': i++; break;`。行末の CRLF だけでなくフィールド途中の '\r' も消えるので、`a\rb` は `ab` になる。また '\r' は field.Length を増やさないため、コメント判定・空行判定の「空」条件を壊さない。

**Goでの注意**: Go の csv は行末の \r\n のみ正規化し、行中の孤立 \r はデータとして残すので不一致。手書きで「引用外の \r は常に破棄」を実装すること。CR のみ改行(旧Mac)のファイルは '\n' が1つも無いため、C# 実装では全体が1レコードに潰れる（R11 の末尾フラッシュで1件だけ出る）ことに注意。

#### R9. 引用の外の '\n' はレコード終端。ただし fields.Count == 0 かつ field.Length == 0 のとき（＝空行）はレコードを作らずに読み飛ばす。

`case '\n': if (fields.Count == 0 && field.Length == 0) { i++; break; } fields.Add(field.ToString()); field.Clear(); records.Add(fields); fields = new List<string>(); i++; break;`。レコード確定時は必ず最後のフィールドを追加してから records に積む。帰結: (a) ',' だけの行は fields.Count==1 なので ["",""] という2列レコードになる、(b) '\r' だけの行は空行扱い、(c) `""` だけの行は引用が開いて閉じただけで field.Length==0 のままなので空行として捨てられる（1列の空レコードにはならない）。

**Goでの注意**: Go の csv も空行をスキップするが、(c) の `""` だけの行は1フィールドのレコードとして返すので不一致。(a) は一致。

#### R10. 上記以外の文字はそのままフィールドに追加される。

`default: field.Append(c); i++; break;`。

#### R11. ループ終了後、`field.Length > 0 || fields.Count > 0` のときだけ最後のフィールドを確定して末尾レコードを1件追加する。

`if (field.Length > 0 || fields.Count > 0) { fields.Add(field.ToString()); records.Add(fields); }`。帰結: (a) 末尾が改行で終わるファイルは余分な空レコードを作らない、(b) 改行なしで終わるファイルの最終行はレコードになる、(c) `a,` で終端した場合 fields.Count==1 なので ["a",""] が出る、(d) 引用が閉じないままEOFに達しても例外にならず、そこまでの内容が最終フィールドになる、(e) 末尾がコメント行（改行なし）の場合は fields も field も空なのでレコードは増えない。

#### R12. パース結果の先頭レコードがヘッダー。レコードが0件なら1行も返さず、ヘッダーのみ（1件）でも1行も返さない。

`if (records.Count == 0) { yield break; }` / `List<string> header = records[0];` / `for (int i = 1; i < records.Count; i++)`。コメント行と空行はレコードにならないので、ファイル冒頭にコメントや空行があっても、その次の実データ行がヘッダーになる。ヘッダー名は一切トリム・正規化されない（引用解除だけは行われる）。

#### R13. 各データ行はヘッダー列数ぶんだけ辞書化する。足りない列は空文字、ヘッダーより多い列は捨てる。辞書のキー比較は大文字小文字を無視する。

`var dict = new Dictionary<string, string>(System.StringComparer.OrdinalIgnoreCase);` / `for (int c = 0; c < header.Count; c++) { dict[header[c]] = c < row.Count ? row[c] : string.Empty; }`。ループ上限が header.Count なので row の余剰列は参照されず失われる。インデクサ代入なので、ヘッダーに大文字小文字違いも含めて同名の列が複数あっても例外にならず「後の列が勝つ」。値の側は大小変換されない。

**Goでの注意**: Go には大小無視マップが無いので、キーを正規化した map[string]string + アクセサ関数にする。StringComparer.OrdinalIgnoreCase は .NET の序数＋インバリアント大文字化であり、Go の strings.EqualFold（Unicode simple folding）とは一部の文字（例: U+212A KELVIN SIGN と 'k'）で結果が食い違う。ヘッダー名は実運用では ASCII なので、ASCII 限定の大文字化で正規化するのが安全（この差異は openQuestions に記載）。

#### R14. Escape は値に ',' '"' '\n' '\r' のいずれかが含まれるときだけ全体を '"' で囲み、内部の '"' を '""' に倍加する。null は空文字。

`if (value == null) return string.Empty;` / `bool needsQuotes = value.IndexOfAny(new[] { ',', '"', '\n', '\r' }) >= 0;` / `if (!needsQuotes) return value;` / `return "\"" + value.Replace("\"", "\"\"") + "\"";`。'#' は引用対象に入っていないので、'#' で始まる値を1列目に書くと読み戻し時に R5 のコメント行として丸ごと消える（ラウンドトリップが壊れる既知の非対称）。前後の空白も引用されない。空文字は引用されず空のまま。

**Goでの注意**: Go の csv.Writer の fieldNeedsQuotes は、上記に加えて「先頭ルーンが空白なら引用」「フィールドが `\.` なら引用」という規則があるため出力が一致しない。Escape も手書きで移植すること。

#### R15. キーは「文字列の UTF-8 バイト列の SHA-256 の先頭8バイトを小文字16進で連結した16桁」。入力に対する加工（トリム・正規化・タグ除去）は一切行わない。null は空文字を返す。

TranslationKey.cs: `public const int Length = 16;` / `if (source == null) return string.Empty;` / `byte[] digest = _sha.ComputeHash(Encoding.UTF8.GetBytes(source));` / `var sb = new StringBuilder(Length); for (int i = 0; i < Length / 2; i++) { sb.Append(digest[i].ToString("x2")); }`。Length/2 == 8 バイト = 64bit。"x2" は小文字ゼロ埋め2桁。コメントにも "exactly as TMP received it (no trimming, tags included)" と明記。SHA256 インスタンスは `[ThreadStatic] private static SHA256 _sha;` で遅延生成されるが、出力には影響しない実装詳細。

**Goでの注意**: `sum := sha256.Sum256([]byte(s)); key := hex.EncodeToString(sum[:8])` で一致する。Encoding.UTF8.GetBytes は BOM を付けない。ただし C# の文字列は UTF-16 なので、ペアにならないサロゲートは U+FFFD(EF BF BD) としてエンコードされる。Go の []byte(string) は不正バイトをそのまま渡すため、不正データ経路では差が出る可能性がある。

#### R16. LooksLikeLineId は「null でない」「長さが 5(="line:".Length) より大きい」「長さが 64 以下」「Ordinal(大小区別あり)で "line:" で始まる」かつ「接頭辞より後ろの全文字が [0-9a-zA-Z_-.] のいずれか」のときだけ true。

`public const string LinePrefix = "line:";` / `if (value == null || value.Length <= LinePrefix.Length || value.Length > 64 || !value.StartsWith(LinePrefix, StringComparison.Ordinal)) return false;` / `for (int i = LinePrefix.Length; i < value.Length; i++) { char c = value[i]; bool ok = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '-' || c == '.'; if (!ok) return false; }`。境界: 長さ6以上64以下（両端含む）、"line:" ちょうど(長さ5)は false、"LINE:xxx" は Ordinal 比較なので false、接頭辞より後ろに ':' は許されない。

**Goでの注意**: strings.HasPrefix + バイト単位のループでそのまま移植可能。長さは C# の UTF-16 コード単位長だが、許可文字が ASCII のみなので、非 ASCII を含む値は文字判定で先に false になる。ただし「長さ > 64」の判定は文字種判定より前に走るため、非 ASCII を含む長大な文字列では len(バイト) と Length(UTF-16単位) の差が結論に影響しうる（いずれも false になるので実害なしと判断）。

#### R17. LooksLikeKey は「null でない」「長さがちょうど 16」「全文字が [0-9a-f]」のときだけ true。大文字 A-F は false。

`if (value == null || value.Length != Length) return false;` / `foreach (char c in value) { bool hex = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'); if (!hex) return false; }`。呼び出し側の TranslationStore.cs:251 は `TranslationKey.LooksLikeKey(key.ToLowerInvariant())` のように明示的に小文字化してから診断に使っており、LooksLikeKey 自体は小文字しか受け付けないことが前提になっている。

**Goでの注意**: len(value) == 16 のバイト長判定で等価（非 ASCII が混ざると UTF-16 長とバイト長がずれるが、その場合いずれも false になる）。

#### R18. Go の encoding/csv では同じ挙動にならない。手書きの状態機械として移植すること。

主な非互換: (1) 引用がフィールド途中で開始・終了できる（R6）— LazyQuotes の true/false どちらとも結果が違う。(2) 引用内の CRLF を \r\n のまま保持する（R4）— Go は readLine で \n に正規化する。(3) 引用外の孤立 \r を常に破棄する（R8）— Go はデータとして残す。(4) `""` だけの行を空行として捨てる（R9-c）— Go は1フィールドのレコードにする。(5) `""#...` をコメントとして捨てる（R5-d）— Go はコメント扱いしない。(6) 列数不一致でエラーにしない — Go は既定で ErrFieldCount（FieldsPerRecord = -1 が必要）。(7) いかなる不正入力でもエラーを返さない — Go は ErrQuote/ErrBareQuote を返す。(8) BOM を除去しない（R2）。(9) Escape 側も Go の Writer とは引用条件が違う（R14）。

**Goでの注意**: encoding/csv を使うなら Comment='#'、FieldsPerRecord=-1、LazyQuotes=true が最も近い設定だが、上記 (1)(2)(3)(4)(5) は設定では埋まらない。仕様どおりのバイト互換が要るなら Parse を移植する一択。

### 境界条件

- 空ファイル、または内容がコメント行と空行だけのファイル: records.Count == 0 で `yield break`、行は0件。
- ヘッダー行しかないファイル: ループが `i = 1` から始まるので行は0件（エラーにはならない）。
- BOM 付きファイル: StreamReader が除去するので1列目のヘッダー名に ﻿ は付かない。Go では明示除去が必要。
- `""` だけの行: 引用が開いて閉じるだけで field.Length == 0 のままなので、'\n' 到達時に空行と判定されレコードにならない（1列の空レコードにはならない）。
- `""#comment` のように空の引用フィールド直後に '#' が来る行: fields.Count==0 && field.Length==0 が成立するためコメントとして行末まで捨てられる。
- ',' だけの行: ',' で fields.Count が1になるため空行判定に該当せず、["", ""] という2列レコードになる。
- '\r' だけの行、および '\r' が連続する行: '\r' は無視されるので空行扱い。
- CR のみを改行に使うファイル(旧Mac): '\n' が1つも無いため引用外の '\r' が全部捨てられ、ファイル全体が1レコード（＝ヘッダーのみ）に潰れて行が0件になる。
- フィールド途中の '\r'（例: `a\rb`）: 引用外なので消えて `ab` になる。引用内の '\r' は残る。
- 引用内の改行: CRLF は "\r\n" の2文字としてそのまま値に残る（Go の encoding/csv は "\n" に潰す）。
- 引用が閉じないままEOF: 例外にならず、残りの内容が最終フィールドとして確定し、非空なら末尾レコードとして出る。
- 末尾が改行のファイル: 余分な空レコードは作られない。末尾が改行なしのファイル: 最終行はレコードになる。
- 末尾が `a,` で終わるファイル: fields.Count > 0 のため ["a", ""] が出る。
- 末尾がコメント行（改行なし）: fields も field も空なので末尾レコードは作られない。
- 行の列数がヘッダーより少ない: 足りない列は string.Empty。多い: 余剰列は読まれずに捨てられる（ヘッダー数でループするため）。
- ヘッダーに同名の列が複数（大文字小文字違いを含む）: インデクサ代入なので例外にならず、右側の列の値が残る。
- 引用符がフィールド途中に現れる（例: `ab"cd"ef`）: エラーにならず `abcdef` になる。引用は途中で開始・終了できる。
- '#' が2列目以降や、空白のあとに現れる場合: コメントにならずただの文字として値に入る。
- Escape は '#' も前後の空白も引用しないため、'#' 始まりの値を先頭列に書くと読み戻し時にコメント行として消える（ラウンドトリップの非対称）。
- Escape(null) は空文字、Hash(null) も空文字を返す（例外にしない）。
- 不正な UTF-8 バイト列: StreamReader の既定フォールバックで U+FFFD に置換され、例外にならない（Go は素通りするので差が出る）。
- キー値が大文字16進（例: "ABCDEF0123456789"）の場合 LooksLikeKey は false。呼び出し側は必要に応じて ToLowerInvariant してから渡している。
- "line:" ちょうど（長さ5）は LooksLikeLineId が false。65文字以上も false。"LINE:xxx" も Ordinal 比較のため false。
- ファイルが存在しない・ロックされている等の I/O 例外は CsvReader 内で捕捉されず呼び出し元へ伝播する。

### 敵対検証で見つかった食い違い

#### [medium] R15「null は空文字を返す」だけを移植すると、Go では Hash("") が "e3b0c44298fc1c14" という「本物に見える16桁キー」を返してしまう。C# 側の null ガードは空文字をガードしていない点が仕様に書かれていない。

- 根拠: TranslationKey.cs:24-27 は `if (source == null) return string.Empty;` のみで、空文字は素通りして `Encoding.UTF8.GetBytes("")` = 0バイトの SHA-256 になる。その先頭8バイトは e3b0c442 98fc1c14 で、LooksLikeKey("e3b0c44298fc1c14") は16桁・全部 [0-9a-f] なので true を返す（TranslationKey.cs:70-85）。C# 側では列が無いときに TryGetValue が false を返して変数が null になり、`key?.Trim()`（TranslationStore.cs:265-267）で null が伝播し、最終的に Hash(null)=="" として「キー無し」に落ちる。Go には null が無いので、この経路が丸ごと「空文字 → 実在するキー」に化ける。
- 直し方: 規則を2本に分ける。(1)『Hash("") は空文字ではなく e3b0c44298fc1c14 を返す。C# の Hash(null)=="" とは別物』を明記する。(2) Go 側は「値が無い」を空文字で表現せず、`Lookup(col) (string, bool)` か `*string` で欠損を持ち回り、欠損時は Hash を呼ばずに空キー扱いする、と規定する。

#### [medium] R1 の goNote「Windows の Go の os.Open は既定で共有読み書きなので FileShare 相当の追加指定は通常不要」は、FileShare.Delete を取りこぼす。os.ReadFile では C# が開けるファイルで共有違反になる／逆に他プロセスの削除・リネームを阻害する可能性がある。

- 根拠: CsvReader.cs:17 は `FileShare.ReadWrite | FileShare.Delete` を明示指定しており、14-15行のコメントが『翻訳者が Excel やエディタで開いたまま動かす。排他オープンでは throw する』とその理由を書いている。これはこの関数の存在理由そのもの。Go の syscall.Open は sharemode に FILE_SHARE_READ|FILE_SHARE_WRITE を渡すのが従来の実装で、FILE_SHARE_DELETE は含まれない（バージョンにより差があり、この環境からは確定できない）。CreateFile の共有判定は既存ハンドルの許可アクセスと自分の share mode の双方向チェックなので、DELETE を許可しないと Excel の保存（一時ファイル＋置換）と衝突しうる。実際、TranslationStore.cs の catch は『Typically a sharing violation from an editor holding the file exclusively』と書いており、共有違反は運用上起きる前提になっている。
- 直し方: goNote の『通常不要』を撤回し、『golang.org/x/sys/windows.CreateFile に FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE を明示して開き、そのハンドルから読む』を規則に格上げする。os.ReadFile で済ませるなら、Excel で開いたままの strings.csv を読めることを実測してから採用する、と条件付きにする。

#### [medium] R13 と dataModel の Row 設計（`Get(name string) string`）が「ヘッダーに無い列」と「値が空文字の列」を区別できない。C# はこの2つを区別している。

- 根拠: CsvReader.cs:33-36 は header.Count ぶんだけ dict を埋めるので、返る Dictionary のエントリ数は常に header.Count。したがって『列が無い』はヘッダーに無い場合だけで、その場合 `row.TryGetValue("key", out string key)` は false を返して key は null になる。呼び出し側はこの差を実際に見ており、ScriptOrder.cs:283 は `row.TryGetValue(col, out string v) ? v ?? "" : ""` と明示的に null を潰す一方、TranslationStore.cs:265-267 の ResolveKey は `key?.Trim()` / `key?.ToLowerInvariant()` で null のまま流している。Get が常に "" を返す設計だと、この非対称が消える。
- 直し方: dataModel の Row に `Lookup(name string) (string, bool)`（comma-ok）を必須の第一アクセサとして置き、`Get` は「欠損を空文字に潰す便宜版」と位置づける。あわせて『返る Row のエントリ数は常に header.Count（行が短ければ空文字で埋まる）ので、欠損はヘッダー由来のみ』を R13 に明記する。

#### [low] R16 と dataModel の文字クラス表記 `[0-9a-zA-Z_-.]` が、正規表現として読むと `_`(0x5F) から `.`(0x2E) への逆順レンジになり、そのまま Go の regexp に渡すとコンパイルエラーになる（並べ替えると別の集合になる）。

- 根拠: TranslationKey.cs:58 の実体は `c == '_' || c == '-' || c == '.'` という3文字の OR であってレンジではない。rule 本文・goNote・dataModel の3箇所が同じ表記を使っているため、機械的に写した実装が壊れる。
- 直し方: 表記を `[0-9A-Za-z._-]`（ハイフンは末尾）に直すか、レンジ誤読を避けるため『英数字と `_` `-` `.` の3文字のみ』と日本語で書く。実装はバイト単位の OR 比較にして regexp を使わない。

#### [low] R2 の『不正 UTF-8 は U+FFFD に置換』を `strings.ToValidUTF8(s, "�")` で再現しても、置換される U+FFFD の個数が .NET と一致しない。ハッシュキーが変わるため、壊れたファイルでの挙動は一致しない。

- 根拠: CsvReader.cs:18 の `Encoding.UTF8` は置換フォールバックなので例外にならない点は正しい。ただし .NET Core 以降は不正列を「最大部分列（maximal subpart）」単位で1個ずつ U+FFFD にするのに対し、Go の strings.ToValidUTF8 は連続する不正バイト列をまとめて1個の置換文字にする。さらに本体は Unity（Mono/.NET Framework 系）で動くため、.NET Core の規則とも一致する保証がない。置換文字数が変われば TranslationKey.Hash(TranslationKey.cs:34) の入力バイト列が変わり、キーが変わる。
- 直し方: 『不正 UTF-8 は契約外。バイト等価は保証しない』と明記して線を引くか、等価を要求するなら .NET 側の実際の出力を実測してからテストケース付きで規定する。少なくとも goNote の『strings.ToValidUTF8 相当が必要』という等価主張は外す。

#### [low] R13 goNote の大小無視比較の例示のうち 'ſ'(U+017F) は逆で、.NET の OrdinalIgnoreCase と Go の EqualFold は一致する側。差が出るのは U+212A(KELVIN SIGN) の方だけ。

- 根拠: CsvReader.cs:32 の StringComparer.OrdinalIgnoreCase は序数＋インバリアント大文字化なので、ToUpperInvariant('ſ') = 'S' となり ſ と S は等しいと判定される。Go の EqualFold も ſ/s/S を同一視するので、ここは一致する。一方 ToUpperInvariant(U+212A) は U+212A のままなので .NET では 'K' と別物、Go の EqualFold では同一と、ここだけ食い違う。結論（ASCII 限定正規化を推奨）は変わらないが、根拠が誤っていると移植者が判断を誤る。
- 直し方: 例示を U+212A のみに絞り、『.NET は等しくないと判定、Go の EqualFold は等しいと判定』と向きを明示する。実ヘッダーは ASCII なので ASCII 限定大文字化で正規化する、という結論はそのままでよい。

### 抽出が取りこぼしていた規則

- Hash("") の戻り値が仕様に無い。null ガードしかないので空文字は素通りし、SHA-256("") の先頭8バイト = "e3b0c44298fc1c14" が返る。これは LooksLikeKey が true を返す有効形式のキーなので、欠損列を空文字にする移植では黙って誤ったキーが生まれる（TranslationKey.cs:24-27, 34-39, 70-85）。
- ReadRows が返す Dictionary のエントリ数は常に header.Count であり、行が短くても空文字で必ず埋まる（CsvReader.cs:33-36）。したがって『その列が無い』はヘッダーに無い場合のみで、C# ではそれが TryGetValue==false / null として観測できる。R13 はこの不変条件を書いていない。
- 重複ヘッダー時、値は後勝ちだがキーの綴りは初出が残る。`dict[header[c]] = ...`（CsvReader.cs:35）はインデクサ代入なので、既存キーがあると値だけ更新して元のキー文字列を保持する。ヘッダーの綴りを保存して書き戻す移植をすると差が出る。
- BOM 未除去の影響がヘッダー名汚染だけではない。BOM を残したまま R3 のループに入ると field.Length != 0 になるため、先頭行が `#` コメントでも R5 の条件（fields.Count==0 && field.Length==0）が成立せず、コメント行がそのままヘッダーレコードになる。先頭が空行の場合も R9 の空行判定が壊れる。R2 の goNote は1列目のヘッダー名の話しかしていない。
- R5 のコメントスキップは引用を一切考慮しない。`while (i < len && text[i] != '\n') i++;`（CsvReader.cs:77）なので、`#"a\nb"` のような行では引用内の '\n' でスキップが終わり、残りの `b"` が次のレコードの先頭として解釈される。
- ヘッダー名が空文字でも許される。末尾カンマ付きのヘッダー行では dict[""] が成立し、以降その列は空文字キーで引ける（CsvReader.cs:35）。Go の map でも同じだが、ヘッダー名を検証する実装にすると挙動が変わる。
- digest[i].ToString("x2")（TranslationKey.cs:38）がカルチャ依存に見える点。16進書式指定子は NumberFormatInfo を参照しないためカルチャ不変で、hex.EncodeToString と一致する。仕様に根拠が書かれていないので、移植時に ToString("x2", CultureInfo.InvariantCulture) との差を疑わせないよう一文入れておくとよい。

### 未決の点

- StringComparer.OrdinalIgnoreCase と Go の大小無視比較の差（例: 'k' と U+212A KELVIN SIGN、'ſ' と 's'）をどこまで再現するか。実ファイルのヘッダー名は ASCII だと推測しているが、ヘッダー名の実際の値セットはこの2ファイルからは確認できていない（ASCII 限定の正規化を推奨）。
- StreamReader の BOM 検出が UTF-16/UTF-32 まで及ぶ点を Go 側で再現する必要があるか。実運用のファイルは UTF-8（BOM有無不明）と推測しているが未確認。
- 不正 UTF-8 の U+FFFD 置換挙動は .NET の既定フォールバック仕様からの推論であり、このコードベースで実際に不正バイトを含むファイルが来るかは未確認。
- ヘッダーより多い列を黙って捨てる挙動（R13）が意図的な仕様か、単なる実装都合かは判断できなかった。Go でも同じく捨てるのが安全と考えるが、要確認。
- `""` だけの行を空行として捨てる挙動（R9-c）と `""#...` をコメント扱いする挙動（R5-d）が意図的かは不明。条件式の副作用である可能性が高い。移植時に「条件式どおり」にするか「意図どおり」にするかの判断が要る。
- Escape が '#' を引用しない件（R14）は既知のラウンドトリップ破れだが、既存の出力ファイルとの互換のため現状維持すべきか、Go 側で直すべきかは判断材料が足りない。直すと既存ファイルとバイト非互換になる。
- 出力側の改行コード。Escape はフィールド単位の処理のみで、行終端は呼び出し側（TranslationStore.cs / WorkingCopy.cs などの writer.WriteLine）が決めており、.NET の WriteLine は既定で Environment.NewLine（Windows では \r\n）。Go で \n 固定にするか \r\n にするかはこの2ファイルからは決められない。
- SHA-256 の対象文字列がゲーム側から来る時点でどんな正規化を受けているか（TMP が渡す文字列そのもの、とコメントにあるだけ）。Go 側の入力が同じバイト列になる保証は、この2ファイルの範囲では確認できない。
- tools/hash-strings.ps1 が同じキーを計算するとコメントにあるが、そのスクリプトは今回未読なので、PowerShell 側の実装と一致しているかは未検証。

## スクリプト順

### データ構造

#### Entry（script_order.csv の1行）

```text
Section string（例 "L01 Ryan" / "Cutscene" / "Reaction" / "Unused"）、Phase string（intro, phone, progress, idle, nag, picnic, jerkoff, cum, mount_start, mount_finish, outro, または空）、Node string（Yarn ノード名）、Order int（ノード内 1 始まりの連番、解析不能なら 0）、LineId string（"line:xxxxxxxx"）、Key string（英文 SHA-256 先頭16桁 hex、小文字・trim 済み）、Speaker string（"Ryan" "Kobold" "Phone" など、空もあり）、Condition string（親ノードがジャンプ前に読んだ変数名をスペース区切りで連結。例 "$conrad_jerked_off_3 $conrad_used_mount_3"、空もあり）
```

文字列フィールドは Get() 経由なので null にならず必ず "" 以上。Key 以外は正規化されない（大小・前後空白はファイルのまま）。

#### LevelMeta（level_flow.csv の1行）

```text
Index int（level 列、0 始まり）、Dragon string、Weather string（Sunny/Rainy/Night、空もあり）、SetFlags string（" | "→", " 置換済み）、EndFlags string（同）、派生 Section string = "L{Index+1:00} {Dragon}"、派生 Header string = "Level {Index+1}: {Dragon}" + 任意で " ({Weather})" " | sets {SetFlags}" " | ends {EndFlags}"
```

level_flow.csv には他に flow_asset, intro, progress_dialogs, idle_dialogs, nag_dialogs, phone, outro, jerkoff_dialog, cum_dialog, mount_start, mount_finish, spawn_flag, player_spawn 列があるが Load は読み込まない（Generate 側が FlowDumper.LevelInfo として使う）。Section と Header は都度計算のプロパティで、Go では メソッドにするのが素直。

#### Data（Load の戻り値）

```text
Entries []Entry（ファイル順）、Levels map[string]LevelMeta（キー＝LevelMeta.Section、Ordinal 比較、後勝ち）、Source string（実際に読んだ script_order.csv のパス）、speakers map[string][]string（遅延構築、キー＝Entry.Key、値＝出現順の重複無し話者リスト）
```

speakers は SpeakersFor / IsShared の初回呼び出しで一度だけ構築される（`if (_speakers == null) BuildSpeakers();`）。Entries を後から変更しても再構築されない。

#### WriteOrdered の入出力

```text
入力: w（出力先）、data *Data、keysPresent 順序付きコレクション（呼び出し側は重複排除済み List<string>）、emit func(key string, e Entry)、wantLine func(e Entry) bool（任意）、emitLine func(e Entry)（任意）。出力: leftovers []string
```

戻り値は無く、出力は w への見出し書き込みとコールバック呼び出しで行われる。Go では out パラメータの代わりに戻り値 []string にする。

#### CSV 行（CsvReader.ReadRows の1要素）

```text
map[string]string、キーは1行目のヘッダ名、比較は大小文字無視（OrdinalIgnoreCase）
```

列不足は "" 補完、余剰列は破棄、ヘッダ名重複は後勝ち。

### 規則

#### R1. Load(pluginDirectory) は生成済みファイルを優先し、どちらも無ければ null を返す。

`string shipped = Path.Combine(pluginDirectory, "data", "script_order.csv");` / `string generated = Path.Combine(pluginDirectory, "Translations", "_discovered", "script_order.csv");` / `string path = File.Exists(generated) ? generated : (File.Exists(shipped) ? shipped : null); if (path == null) return null;`。採用したパスは `data.Source` に格納される（`var data = new Data { Source = path };`）。

**Goでの注意**: Go では os.Stat の存在確認で同順に判定。戻り値は (*Data, error) ではなく「見つからない＝nil」を表現できる形にする（元コードは見つからない場合も例外時も同じ null）。

#### R2. 読み込み結果は「パス + 最終更新時刻(UTC Ticks)」をキーにした静的キャッシュで再利用される。

`string stamp = path + "|" + File.GetLastWriteTimeUtc(path).Ticks; if (_cached != null && _cachedFrom == stamp) return _cached;` … 成功時のみ末尾で `_cached = data; _cachedFrom = stamp;`。`_cached`/`_cachedFrom` は static フィールドでプロセス全体で共有される。キャッシュキーには script_order.csv しか含まれず、level_flow.csv の更新は無視される。

**Goでの注意**: Ticks は 100ns 単位の .NET 時刻。Go では ModTime().UnixNano() など任意の単調な表現でよいが、「script_order.csv の mtime のみ」で判定する挙動は維持する。static 共有をパッケージ変数で再現するなら sync.Mutex が要る（元コードはロック無し）。

#### R3. script_order.csv の各行は key 列が空（null または長さ0）なら丸ごとスキップされ、Entry は作られない。

`row.TryGetValue("key", out string key); if (string.IsNullOrEmpty(key)) continue;`。判定は Trim 前の生値に対して行われるため、空白のみの key（例 " "）はこの判定を通過する。

**Goでの注意**: IsNullOrEmpty は空白文字を含まない。Go でも `if raw == "" { continue }` とし、TrimSpace 後で判定しないこと。

#### R4. Entry.Key だけが Trim + 小文字化で正規化される。他の列は一切正規化されない。

`Key = key.Trim().ToLowerInvariant()`。同じ new Entry 初期化子の中で `Section = Get(row, "section"), Phase = Get(row, "phase"), Node = Get(row, "node"), LineId = Get(row, "line_id"), Speaker = Get(row, "speaker"), Condition = Get(row, "condition")` はいずれも Get() の素通しで、trim も小文字化もされない。

**Goでの注意**: `strings.ToLower(strings.TrimSpace(key))`。C# の Trim() は char.IsWhiteSpace 基準（U+00A0, U+0085 等も含む）で Go の strings.TrimSpace（unicode.IsSpace）とほぼ一致するが完全同一ではない。ToLowerInvariant は実運用上は16桁hexなので ASCII 範囲のみ。

#### R5. order 列は int.TryParse で解析し、失敗したら 0 を入れて行は捨てない。

`row.TryGetValue("order", out string o); int.TryParse(o, out int order);` 戻り値を見ていないため、解析失敗時は out 既定値 0 がそのまま `Order = order` に入る。

**Goでの注意**: strconv.Atoi のエラーは無視して 0 を使う。ただし int.TryParse（既定 NumberStyles.Integer）は前後の空白と先頭符号を許容し、Atoi は空白を許容しない。空白許容を合わせるなら TrimSpace してから Atoi する。

#### R6. 列の取得は Get() 経由で、列が無い／値が null なら空文字を返す。

`private static string Get(Dictionary<string, string> row, string col) => row.TryGetValue(col, out string v) ? v ?? "" : "";`。したがって Entry の文字列フィールドは決して null にならない。

**Goでの注意**: Go の map 参照は欠損時に "" を返すので自然に一致する。

#### R7. level_flow.csv は「採用した script_order.csv と同じディレクトリ」だけを探し、無ければ Levels は空のままで処理を続ける。

`string flow = Path.Combine(Path.GetDirectoryName(path), "level_flow.csv"); if (File.Exists(flow)) { ... }`。data/ を採用したら data/level_flow.csv、_discovered/ を採用したら _discovered/level_flow.csv。

**Goでの注意**: filepath.Dir(path) を使う。存在しない場合はエラーにせず空の Levels で返す。

#### R8. level_flow.csv の行は level 列が整数として解析できなければスキップされる。

`if (!int.TryParse(Get(row, "level"), out int index)) continue;`。空欄や非数値の level 行は LevelMeta を作らない。

**Goでの注意**: ヘッダ行は CsvReader がヘッダとして消費済みなのでここには来ない。

#### R9. set_flags / end_flags は「 | 」（半角スペース+パイプ+半角スペース）を「, 」に置換して保持する。weather と dragon は無加工。

`SetFlags = Get(row, "set_flags").Replace(" | ", ", "), EndFlags = Get(row, "end_flags").Replace(" | ", ", ")`。生成側 FlowDumper.ExportLevelFlow が `string.Join(" | ", lv.SetFlags)` で書き出す形式に対応する。実データ例: `MedkitCompleted | level_5_complete` → `MedkitCompleted, level_5_complete`。

**Goでの注意**: strings.ReplaceAll(s, " | ", ", ")。パイプ単体や空白数が違うものは置換されない（完全一致の3文字列）。

#### R10. LevelMeta の辞書キーは Index と Dragon から導出した Section 文字列で、Ordinal 比較。同じ Section が複数行あれば後の行が上書きする。

`public string Section => $"L{Index + 1:00} {Dragon}";` と `data.Levels[meta.Section] = meta;`（辞書は `new Dictionary<string, LevelMeta>(StringComparer.Ordinal)`）。level=0,dragon=Ryan なら "L01 Ryan"。実データでは level 0..14 → "L01 Ryan"…"L15 Alexander"。

**Goでの注意**: 書式 "00" は最小2桁ゼロ埋め（3桁以上はそのまま、負値は "-01" のように符号付き）。Go では `fmt.Sprintf("L%02d %s", index+1, dragon)`（負値の挙動は %02d だと "L-1" となり .NET と異なる点に注意）。map への代入は後勝ちで一致。

#### R11. LevelMeta.Header は「Level {Index+1}: {Dragon}」に、Weather / SetFlags / EndFlags が非空の場合だけ後置パーツを連結する（ゼロ埋めしない）。

`public string Header => $"Level {Index + 1}: {Dragon}" + (string.IsNullOrEmpty(Weather) ? "" : $" ({Weather})") + (string.IsNullOrEmpty(SetFlags) ? "" : $" | sets {SetFlags}") + (string.IsNullOrEmpty(EndFlags) ? "" : $" | ends {EndFlags}");` 例: `Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete`。Section と違い数字はゼロ埋めされない。

**Goでの注意**: 連結順は weather → sets → ends の固定順。

#### R12. 話者表は初回アクセス時に遅延構築され、Entries の並び順で、Speaker が空でないものだけを、Key ごとに重複排除して追加する。

`private void BuildSpeakers() { _speakers = new Dictionary<string, List<string>>(StringComparer.Ordinal); foreach (Entry e in Entries) { if (string.IsNullOrEmpty(e.Speaker)) continue; if (!_speakers.TryGetValue(e.Key, out List<string> list)) { _speakers[e.Key] = list = new List<string>(); } if (!list.Contains(e.Speaker)) list.Add(e.Speaker); } }`。呼び出し側は `if (_speakers == null) BuildSpeakers();`（SpeakersFor と IsShared の両方の先頭）。Speaker が全件空の Key は辞書に登場しない。

**Goでの注意**: list.Contains は string の既定比較＝完全一致（序数）。Go では []string + 線形探索、または map で存在確認しつつ slice で順序保持。構築は sync.Once 相当で1回だけ。

#### R13. SpeakersFor(key) は話者リストを "/" で連結して返し、未登録キーは空文字を返す。

`return _speakers.TryGetValue(key, out List<string> list) ? string.Join("/", list) : string.Empty;`。順序は R12 の出現順（例: "Ryan/Alexander"）。引数は正規化済み（小文字・trim 済み）の key であることが前提。

**Goでの注意**: strings.Join(list, "/")。

#### R14. IsShared(key) は「その key の異なる話者が2人以上」のときだけ true。

`return _speakers.TryGetValue(key, out List<string> list) && list.Count > 1;`。同一話者が何度喋っても R12 の重複排除で 1 のまま false。登録されていない key も false。

**Goでの注意**: len(list) > 1。

#### R15. WriteOrdered は data.Entries をファイル順にそのまま1回走査するだけで、Order 列や Section による並べ替えは一切行わない。

`foreach (Entry e in data.Entries)` のみ。Entry.Order は WriteOrdered 内で比較にも整列にも使われず、呼び出し側が行出力に使うだけ。

**Goでの注意**: 入力 CSV の行順が出力順の唯一の根拠。読み込み時も並べ替えないこと。

#### R16. ハッシュ行（emit 呼び出し）の条件は「keysPresent に Key が含まれ、かつその Key をまだ出していない」。

`bool hashRow = keysPresent.Contains(e.Key) && !done.Contains(e.Key);`（`var done = new HashSet<string>(StringComparer.Ordinal);`）。つまり同一 Key の最初の該当出現のみ 1 回 emit される。emit 実行後に `done.Add(e.Key);`。

**Goでの注意**: keysPresent は ICollection<string>（実際の呼び出しでは List<string>）なので線形探索だが意味は集合所属。Go では map[string]struct{} で可。ただし leftovers の順序保持のため元のスライス順も保持が必要（R22）。

#### R17. 行ID行（emitLine 呼び出し）の条件は「emitLine が非 null、かつ LineId が非空、かつ wantLine(e) が true」。

`bool lineRow = emitLine != null && !string.IsNullOrEmpty(e.LineId) && wantLine(e);` 短絡評価のため emitLine==null なら wantLine は呼ばれない。4引数オーバーロードは `WriteOrdered(w, data, keysPresent, emit, null, null, out leftovers);` と両方 null を渡すので lineRow は常に false。行ID行は同じ Key で何度でも（出現ごとに）出る。

**Goでの注意**: emitLine != nil かつ wantLine == nil の組み合わせは元コードでは NullReferenceException になる（現行呼び出し元は両方 nil か両方非 nil）。Go でも同様に未定義として良いが、防御するなら明記する。

#### R18. hashRow も lineRow も false の Entry は完全に無視され、見出しも出力されない。

`if (!hashRow && !lineRow) continue;` が見出し判定より前にある。結果として「行が1つも出ないノード／セクション」の見出しは印字されない。

**Goでの注意**: 見出し出力を先にやってしまう実装にしないこと。出力対象が確定してから見出しを出す。

#### R19. セクション見出しは「直前に出力した Entry の Section と異なるとき」だけ、空行1行 + "# ===== {header} =====" を出力し、同時に lastNode を null にリセットする。

`if (e.Section != lastSection) { string header = data.Levels.TryGetValue(e.Section, out LevelMeta meta) ? meta.Header : SectionTitle(e.Section); w.WriteLine(); w.WriteLine("# ===== " + header + " ====="); lastSection = e.Section; lastNode = null; }`。初期値 `string lastSection = null` なので最初の出力対象で必ず見出しが出る（Section が空文字でも出る）。header は Levels に一致があれば LevelMeta.Header、無ければ SectionTitle(section)：`case "Cutscene": return "Cutscenes (started by game code)"; case "Reaction": return "Dragon reactions (started by game code)"; case "Unused": return "Unused nodes (not reachable in the current game)"; default: return section;`。

**Goでの注意**: 辞書引きは Ordinal 完全一致（大小・空白差があれば SectionTitle 側にフォールバック）。空行は見出しの直前に必ず1行入る（ファイル先頭でも）。

#### R20. ノード見出しは「直前に出力した Entry の Node と異なるとき」だけ "# --- {title} ---" を出力する。空行は入れない。

`if (e.Node != lastNode) { string title = (string.IsNullOrEmpty(e.Phase) ? "" : e.Phase + ": ") + e.Node + (string.IsNullOrEmpty(e.Condition) ? "" : " | if " + e.Condition); w.WriteLine("# --- " + title + " ---"); lastNode = e.Node; }` 例: `# --- intro: Ryan_1_intro ---`、`# --- cum: Conrad_Outro_3 | if $conrad_jerked_off_3 $conrad_used_mount_3 ---`。Phase が空なら "node ---" だけ（Cutscene/Reaction/Unused は phase 空）。

**Goでの注意**: 判定対象は Node のみ。Phase や Condition だけが変わっても見出しは出し直されない（R19 のセクション変化でのみ lastNode がリセットされる）。

#### R21. 同一 Entry で両方該当する場合、必ず emit（ハッシュ行）が先、emitLine（行ID行）が後。

`if (hashRow) { emit(e.Key, e); done.Add(e.Key); } if (lineRow) { emitLine(e); }`。コメントにも "right after the hash row when both fall on the same occurrence" とある。

**Goでの注意**: emit のコールバック引数は (key, entry) の2値、emitLine は (entry) の1値。

#### R22. leftovers は「keysPresent のうち一度も emit されなかったキー」を keysPresent の並び順のまま返す。

`leftovers = keysPresent.Where(k => !done.Contains(k)).ToList();`。LINQ Where は入力順を保つ。done には emit 済みキーだけが入る（行ID行しか出なかった Entry の Key は done に入らないので、その Key が keysPresent にあれば leftovers に残る…ただし keysPresent にあれば通常は hashRow 側で先に出る）。keysPresent に重複があれば leftovers にも重複が出る。

**Goでの注意**: 呼び出し側は leftovers を "# ===== UI and other text (not part of the dialogue script) =====" 見出しの下に追記する（TranslationStore.cs / WorkingCopy.cs）。順序保持が必須なので map ではなくスライス走査で実装する。

#### R23. 読み込み中に例外が起きたらログを出して null を返し、キャッシュは更新しない（部分結果は破棄）。

`catch (Exception ex) { Plugin.Log($"[order] Could not read {path}: {ex.Message}"); return null; }`。この catch は script_order.csv の走査と level_flow.csv の走査の両方を囲っているため、level_flow.csv が壊れていると script_order 側の成果も丸ごと捨てられる。

**Goでの注意**: Go では err を返すか nil を返すか選ぶが、「level_flow の失敗が全体の失敗になる」点は挙動として引き継ぐか、意図的に変えるなら明示する。

#### R24. CSV 解析は CsvReader.ReadRows：1行目をヘッダとし、以降の行を「ヘッダ名→値」の大小文字無視辞書にする。列不足は空文字で補い、ヘッダより多い列は捨てる。

`List<string> header = records[0]; for (int i = 1; ...) { var dict = new Dictionary<string, string>(System.StringComparer.OrdinalIgnoreCase); for (int c = 0; c < header.Count; c++) { dict[header[c]] = c < row.Count ? row[c] : string.Empty; } yield return dict; }`。records が空ならレコード0件。ヘッダ名が重複すると後勝ち。

**Goでの注意**: 列名照合は OrdinalIgnoreCase。Go では strings.ToLower したキーで map を作るなどして大小文字無視を再現する。encoding/csv は列数不一致でエラーにするので FieldsPerRecord = -1 が必要（あるいは自前パーサ）。

#### R25. CSV 解析の特殊規則：レコード先頭の '#' はコメント行として行末まで読み飛ばす、空行はレコードにしない、'\r' は引用符外なら常に破棄、引用符内は "" でエスケープされた " とカンマ・改行を許容する。

`if (c == '#' && fields.Count == 0 && field.Length == 0) { while (i < len && text[i] != '\n') i++; i++; continue; }`（フィールド途中の # は通常文字）／`case '\n': if (fields.Count == 0 && field.Length == 0) { i++; break; } ...`（空行スキップ）／`case '\r': i++; break;`（CR は無条件に捨てる＝引用符外ならフィールド途中の CR も消える）／引用符内は `if (c == '"') { if (i + 1 < len && text[i + 1] == '"') { field.Append('"'); i += 2; continue; } inQuotes = false; ... }`。末尾は `if (field.Length > 0 || fields.Count > 0) { fields.Add(...); records.Add(fields); }` で未終端レコードを確定する。

**Goでの注意**: encoding/csv には '#' コメントの Comment 設定があるが「レコード先頭のみ」「空行スキップ」「引用符外 CR 破棄」を含めて完全一致させるには自前パーサが安全。公開ファイル（翻訳パック）には実際に '#' 見出し行と空行が含まれるため必須の挙動。

#### R26. ファイルは共有読み取りで開き、BOM 付き UTF-8（および UTF-16 BOM）は StreamReader が自動判別して除去する。

`using (var fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete)) using (var reader = new StreamReader(fs, Encoding.UTF8, true)) { text = reader.ReadToEnd(); }`。第3引数 true が detectEncodingFromByteOrderMarks。実データでは data/level_flow.csv が EF BB BF 始まり（BOM 有り）、data/script_order.csv は BOM 無し。BOM が除去されるので先頭ヘッダ名は "flow_asset" として一致する。

**Goでの注意**: Go は BOM を除去しないので、読み込み直後に \xEF\xBB\xBF を手動で剥がすこと。剥がさないとヘッダ名が "﻿flow_asset" になり level 列以前に flow_asset 列が引けなくなる（今回のコードは flow_asset を使わないが、同じ事故が script_order.csv の section 列で起きうる）。

#### R27. 生成側 Generate が書く script_order.csv の書式：ヘッダ "section,phase,node,order,line_id,key,speaker,condition"、BOM 無し UTF-8、section/node/line_id/speaker/condition のみ CSV エスケープ、phase・order・key は無加工で連結。

`using (var w = new StreamWriter(path, false, new UTF8Encoding(false))) { w.WriteLine("section,phase,node,order,line_id,key,speaker,condition"); foreach (Entry e in entries) { w.WriteLine(string.Join(",", CsvReader.Escape(e.Section), e.Phase, CsvReader.Escape(e.Node), e.Order.ToString(), CsvReader.Escape(e.LineId), e.Key, CsvReader.Escape(e.Speaker), CsvReader.Escape(e.Condition))); } }`。Escape は `value.IndexOfAny(new[] { ',', '"', '\n', '\r' }) >= 0` のときだけ二重引用符で囲み、内部の " を "" にする。null は空文字。

**Goでの注意**: Go 側で書き出すなら encoding/csv は全フィールドを同じ規則でクォートするので概ね一致するが、\r 単独を含む値の扱いなど差異が出る。読み取り専用の移植なら不要。

#### R28. データの意味：1行＝1台詞の1出現。order はノード内 1 始まりの連番、key は英文 SHA-256 先頭16桁 hex、line_id は Yarn の "line:xxxxxxxx"。

Generate の Emit 内 `if (!textById.TryGetValue(lineId, out string text) || text.Length == 0) return; if (!emittedLines.Add(lineId)) return; string key = TranslationStore.KeyFor(text); order++;` のとおり、英文が取得できない行と既出 line_id は出力されず、order は出力された行だけを数える。key は TranslationKey.Hash＝`SHA256(UTF8 bytes)` の先頭8バイトを "x2" で連結した小文字16桁（`public const int Length = 16;`）。同じ英文＝同じ key なので、同じ key が別ノード・別話者で複数行に現れる。

**Goでの注意**: key の再計算が必要なら sha256.Sum256([]byte(s)) の先頭8バイトを hex.EncodeToString。line_id 判定は TranslationKey.LooksLikeLineId（"line:" 始まり、以降は [0-9A-Za-z_.-]、全長64以下）。

### 境界条件

- level_flow.csv は BOM 付き UTF-8（先頭 EF BB BF を実ファイルで確認）。.NET は StreamReader の BOM 自動判別で除去するが Go は除去しないため、手動で剥がさないと最初の列名（flow_asset）が壊れる。script_order.csv は BOM 無し。
- key 列が空文字／列自体が無い行は Entry を作らずスキップ（R3）。ただし空白のみの key はスキップされず、Trim 後に Key == "" の Entry が登録される。この Entry は keysPresent に "" が無い限り出力されないが、BuildSpeakers では "" キーの話者リストを作る。
- order 列が空欄や非数値なら Order = 0。行は捨てられない。
- level_flow.csv の level 列が空欄・非数値の行は無視され、そのレベルの Header は使われず SectionTitle フォールバック（＝section 文字列そのまま）になる。
- level_flow.csv の weather / set_flags / end_flags は実データで空欄が多く、空欄なら Header の該当パーツごと出力されない（" ()" のような空括弧は出ない）。
- idle_dialogs / nag_dialogs など複数値の列は " | " 区切り（例 "Conrad_idle_3_1 | Conrad_idle_3_2"）。Load が置換するのは set_flags / end_flags の2列のみで、他は読まない。
- script_order.csv の Section 値と level_flow.csv 由来の Section キーは Ordinal 完全一致でしか結び付かない。手編集で大小や空白がずれると黙って SectionTitle フォールバックになり、レベル見出しが失われる。
- CSV 本体にコメント行（レコード先頭の '#'）と空行が混在しうる。公開される翻訳ファイルは WriteOrdered が出す "# ===== ... =====" と "# --- ... ---" を含むため、読み側で必ずスキップが要る。data/script_order.csv 自体には現時点でコメント行も空行も引用符も含まれない（grep で 0 件を確認）。
- 引用符外の '\r' は無条件に破棄されるため、CRLF/LF 混在でも同じ結果になる。一方フィールド途中に単独 CR があると黙って消える。
- ファイル末尾に改行が無くても最後のレコードは確定される（`if (field.Length > 0 || fields.Count > 0)`）。逆に末尾が改行で終わっていれば余分な空レコードは出ない。
- 同じ Key が複数の Entry に現れるのが通常（同じ英文を複数ノード・複数話者が喋る）。emit は最初の該当出現だけ、emitLine は条件に合う全出現。
- WriteOrdered は出力対象が1件も無ければ見出しを一切書かない。逆に行ID行だけが出る場合でもセクション／ノード見出しは出る。
- セクションが変わると lastNode が null にリセットされるため、別セクションで同名ノードが続いても見出しは出し直される。逆に同一セクション内では Node 名が同じ限り Phase や Condition が変わっても見出しは出し直されない。
- keysPresent に重複があると leftovers にも重複が残る（現行の呼び出し元 TranslationStore.cs / WorkingCopy.cs はいずれも事前に重複排除済み）。
- Load のキャッシュは script_order.csv の mtime だけを見るため、level_flow.csv だけ差し替えた場合に古い Levels が返り続ける。
- level_flow.csv の解析中に例外が出ると script_order.csv の解析結果も含めて null が返る（catch が両方を囲っている）。
- Speaker が全出現で空の Key は _speakers に登録されず、SpeakersFor は ""、IsShared は false を返す。
- Section が "Cutscene" / "Reaction" / "Unused" のときだけ SectionTitle が固定の説明文に差し替える。それ以外の未知セクションは文字列そのまま。

### 敵対検証で見つかった食い違い

#### [medium] R20（ノード見出し）で lastNode の初期値・リセット値が null であること（空文字 "" と区別される）が書かれていない。R19 では lastSection について「初期値 null なので Section が空文字でも見出しが出る」と明記しているのに、R20 だけ抜けている。

- 根拠: ScriptOrder.cs:301 `string lastSection = null, lastNode = null;`、313 `lastNode = null;`、315 `if (e.Node != lastNode)`。Entry.Node は Get() 経由（283行）なので決して null にならず必ず "" 以上。したがって Node が空文字の Entry が出力対象になると、C# では `"" != null` が真になり `# ---  ---`（Phase/Condition も空なら空タイトル）が必ず1行出る。Go で `lastNode := ""` と書くと同じ状況で見出しが出ず、出力行数が変わる。セクション切替直後（313行でリセット）にも同じ分岐を通る。
- 直し方: R20 に「lastNode は null 相当の未設定センチネルで初期化され、セクション変化時も未設定に戻る。Go では `var lastNode *string` か `nodeSet bool` を併用し、空文字 Node と未設定を区別する」と明記する。R19 の lastSection も同様に「空文字ではなく未設定」であることを goNote に残す。

#### [medium] R5 の goNote「strconv.Atoi のエラーは無視して 0 を使う」がそのままでは実装できない。Atoi は範囲外のとき err と一緒に非ゼロ値（MaxInt64 等）を返すため、`order, _ := strconv.Atoi(s)` と書くと C# の 0 と食い違う。

- 根拠: ScriptOrder.cs:251 `int.TryParse(o, out int order);` は戻り値を見ていないが、int.TryParse は失敗時に out 変数へ必ず 0 を書き込む（オーバーフロー時も 0）。一方 Go の strconv.Atoi("99999999999999") は (9223372036854775807, ErrRange) を返す。エラーを「無視」すると 0 ではなく巨大値が Order に入る。同じ理由で int32 範囲外（"3000000000"）も C#=0 / Go=3000000000 と割れる。
- 直し方: goNote を「err != nil なら明示的に order = 0 を代入する。かつ int32 範囲外も失敗扱いにするため strconv.ParseInt(s, 10, 32) を使い、err があれば 0」に書き換える。R8（level 列）は元コードが戻り値を見て continue しているので err != nil → continue でよい、と対比して書く。

#### [medium] R25 が引用符の状態遷移を「引用符内は "" エスケープとカンマ・改行を許容」としか書いておらず、(a) フィールド途中の裸の `"` が引用モードを開始する、(b) 閉じ引用符の後の文字は同じフィールドに続けて追記される、(c) 引用符が閉じないまま EOF に達してもエラーにならず残り全部が1フィールドになる、という3点が抜けている。

- 根拠: CsvReader.cs Parse の switch は `case '"': inQuotes = true; i++; break;` で、field.Length や fields.Count を一切見ない（'#' 判定だけが「レコード先頭」条件を持つ）。また引用符内の `"` は `inQuotes = false; i++; continue;` で抜けるだけで、その後の文字は default 分岐で同じ field に Append される。while ループ終了後は `if (field.Length > 0 || fields.Count > 0)` で無条件に確定するため、未終端引用符でも例外は出ない。結果 `ab"cd"ef` は1フィールド `abcdef` になる。Go の encoding/csv は LazyQuotes=false なら ErrBareQuote / ErrQuote でレコードごと失敗、LazyQuotes=true なら `ab"cd"ef` を（引用符を残したまま）別の結果にする。どちらも一致しない。
- 直し方: R25 に上記 (a)(b)(c) を規則として追記し、goNote を「encoding/csv は LazyQuotes の有無どちらでも一致しないので必ず自前パーサ。トグル式の inQuotes を使い、malformed でもエラーを返さず最善努力でパースする」と断定形にする。CsvReader は翻訳者が手編集する Translations/*/strings.csv にも使われるので到達可能な経路である旨も残す。

#### [low] 出力の改行コードが仕様に書かれていない。WriteOrdered が使う TextWriter.WriteLine() は Environment.NewLine（Windows の .NET Framework/デスクトップなら CRLF、Mono/Linux なら LF）であり、Go で "\n" 決め打ちにすると実行環境によってバイト列が変わる。

- 根拠: ScriptOrder.cs:310-311,319 はいずれも `w.WriteLine()` / `w.WriteLine("# ===== " + header + " =====")`。呼び出し元も `new StreamWriter(path, false, new UTF8Encoding(false))` を渡すだけで NewLine を設定していない（TranslationStore.cs:449、WorkingCopy.cs:146）。コミット済みの Translations/ja/strings.csv は実際には LF（0x0a のみ、xxd で確認）で、.gitattributes は `*.sh text eol=lf` しか指定していないため正規化もされていない＝LF 環境で生成されたもの。読み側は CR を捨てる（CsvReader の `case '\r'`）ので往復は壊れないが、生成物の diff は一致しない。
- 直し方: R19/R20 に「出力改行は元コードでは Environment.NewLine（プラットフォーム依存）。既存の公開ファイルは LF なので Go 側は "\n" 固定にする」と明記し、バイト一致を検証する場合の前提として残す。

#### [low] R23 の「読み込み中に例外が起きたら null」の範囲が広すぎる。パス解決と mtime 取得（File.Exists / File.GetLastWriteTimeUtc）は try の外にあり、そこで例外が出た場合は null ではなく呼び出し元へ送出される。

- 根拠: ScriptOrder.cs:235-241 の shipped/generated 組み立て、File.Exists、`File.GetLastWriteTimeUtc(path).Ticks`、キャッシュ判定はすべて 244行の `try {` より前。catch（273-277）が覆うのは 246行以降の CsvReader.ReadRows 走査だけ。パス長超過・アクセス拒否・Exists と Stat の間でのファイル削除などはここで throw する。
- 直し方: R23 を「try が覆うのは CSV 走査のみ。パス存在確認と mtime 取得の失敗は捕捉されず呼び出し元へ伝播する」と限定し、Go 側で stat エラーも nil に潰すなら意図的な変更として明記する（呼び出し元 WorkingCopy.cs:203 は外側 try-catch があるが TranslationStore.cs:433 には無い点も添える）。

### 抽出が取りこぼしていた規則

- WriteOrdered 自身は行ID行（emitLine）の重複排除を一切しない。一度きりになるのは呼び出し元が emitLine の中でマップを破壊的に消すため（TranslationStore.cs:475 `lineRows.Remove(e.LineId)`、WorkingCopy.cs:180 `lineTranslations.Remove(e.LineId)`）。Go で emitLine を純粋関数にすると、同じ LineId が Entries に2回現れた場合に出力が増える。
- 同一セクション内で同じノード見出しが複数回出るのは正常動作であり、実データで発生している。Generate の Walk が RunNode/DetourToNode で子ノードへ降りる際、親ノードの行の途中に子ノードの行が挟まるため（ScriptOrder.cs:163-168）。確認済み: data/script_order.csv の Conrad_finished_jerkoff_3 は order 1-10（408-417行）と order 11-15（456-460行）に分断され、Translations/ja/strings.csv では `# --- cum: Conrad_finished_jerkoff_3 ---` が440行と486行の2回、`# --- intro: Ryan_5_intro ---` が1128行と1148行の2回出ている。R20 を「一度出したノードは二度と出さない（seen 集合）」と読み違えると見出しが欠落する。
- Entry.Order はノード内で連番（1始まり・欠番なし）だが、出力されるハッシュ行の order 列には欠番が生じる。同一ノード内に同じ key が再出現すると done により2回目以降が抑止されるため。確認済み: script_order.csv の order 1 と 3 はどちらも key=10069c101bcc95c1、order 2 と 4 はどちらも 82d37703a9140210 で、ja/strings.csv には 1,2,5,6,7,8,10 しか出ていない。「order が飛んでいる＝バグ」ではないことを仕様に残すべき。
- CSV のヘッダ名は trim されない（CsvReader.cs の `dict[header[c]] = ...`）。ヘッダに余分な空白や BOM が残ると、その列は row.TryGetValue で引けなくなる。key 列でこれが起きると ScriptOrder.cs:248-249 で全行が無条件スキップされ、Entries が空の Data が返る（null ではない）ので、呼び出し元は「順序データはあるが1行も出ない」状態になる。R26 は BOM の話だけで、この一般則が書かれていない。
- Load が返す Data はキャッシュされた同一インスタンスであり、SpeakersFor / IsShared が遅延構築で _speakers を書き込む（ScriptOrder.cs:77,85）。つまり同じプロセス内の2回目以降の Load は「話者表が構築済みの Data」を返す。R2 と dataModel の注記が別々に書かれていて、この組み合わせ（共有可変インスタンス＋ロック無し static）が一箇所にまとまっていない。
- Generate 側の `visited` HashSet（ScriptOrder.cs:131,140）により各 Yarn ノードは全体で1回しか歩かれない。複数レベルから到達可能なノードは最初のレベルのセクションにしか現れない。openQuestions で Generate を対象外と推測しているが、script_order.csv の内容を理解するうえでは R28 に添えるべき前提。
- CsvReader.ReadRows は yield return のイテレータなので、FileStream のオープン自体が最初の MoveNext（＝foreach 内）で起きる。結果としてファイルオープン失敗も ScriptOrder.cs:273 の catch に入り null になる。R23 の「読み込み中」がここまで含むことが明示されていない。

### 未決の点

- Go 側の移植対象に Generate() を含むのかが不明。Generate は Unity / Yarn / Resources.FindObjectsOfTypeAll に依存するためゲーム外では動かず、Go では「CSV を読む Load と WriteOrdered だけ」が現実的だと推測した（R27・R28 は生成物の書式の説明として記載）。
- ToLowerInvariant を Go の strings.ToLower に置き換えた場合の非 ASCII 差異は未検証。実データの key は16桁 hex のみなので実害は無いと推測したが、手編集で非 ASCII が混ざる可能性は確認していない。
- int.TryParse が前後空白を許容するのに対し strconv.Atoi は許容しない差を、そのまま無視してよいか（order 列に空白付きの値が入る運用があるか）は未確認。
- LevelMeta.Section の書式 "L{Index+1:00}" が負の Index でどうなるかは実データに存在しないため未検証。.NET の書式指定では "-01" 相当になると推測しているが実行確認はしていない。
- emitLine が非 null で wantLine が null の場合は NullReferenceException になるが、これが意図的な前提条件なのか、Go では防御すべきなのかは判断できない。現行の呼び出し元2箇所は常に両方まとめて渡している。
- 静的キャッシュ（_cached / _cachedFrom）はロック無しで、Go で並行アクセスする設計にするなら排他が必要。元の設計がシングルスレッド前提かどうかはコードからは断定できない。
- level_flow.csv のキャッシュ非考慮（R2 末尾）が意図的な割り切りかバグかは判断できない。
- data.Entries の並びが常に Generate の出力順であるという保証は無く、公開ファイルが手編集される運用があるかどうかは確認していない。WriteOrdered はファイル順を唯一の根拠にしているため、手編集で順序が崩れた場合の期待動作は不明。
- CsvReader が UTF-16 BOM も自動判別する点について、実運用でそうしたファイルが来るのかは未確認。Go 移植で UTF-8（BOM 有無）だけ対応すれば足りるかは要判断。

## 作業コピー生成

### データ構造

#### WorkingCopyRow

```text
Key string（hexキー or "line:xxxx" or 検証されていない任意文字列）/ Section string / Node string / Order string（数値化前。空文字あり）/ Speaker string（"Ryan/Alexander" のような / 連結、"UI"、または空）/ SourceEn string（ゲームが未ロードなら空）/ Translation string
```

ヘッダ `key,section,node,order,speaker,source_en,translation` の7列。Order を int にするなら空文字と非数値を許容すること（ScriptOrder.Load も `int.TryParse` で失敗時0）。Key の種別は LooksLikeLineId → LooksLikeKey → それ以外（不正）の3分岐で判定する。

#### PublishedRow

```text
Key string / Section string / Node string / Order string / Speaker string / Translation string
```

公開 strings.csv のヘッダは `key,section,node,order,speaker,translation`（6列、source_en 無し）。tools/check-translations.py が source_en 列の有無で作業コピー混入を検出する。作業コピーは公開形式の上位互換ではなく、列が1つ多い別形式。

#### ScriptOrderEntry

```text
Section string / Phase string / Node string / Order int / LineId string / Key string（Trim + ToLowerInvariant 済み）/ Speaker string / Condition string
```

data/script_order.csv（または _discovered/script_order.csv）のヘッダは `section,phase,node,order,line_id,key,speaker,condition`。英文は含まないのでコミット可能。Go でも読める。key 列が空の行は読み捨てる。

#### LevelMeta

```text
Index int（level 列）/ Dragon string / Weather string / SetFlags string / EndFlags string
```

level_flow.csv 由来。Section は `$"L{Index+1:00} {Dragon}"`、Header は `$"Level {Index+1}: {Dragon}"` に weather を ` (…)`、set_flags を ` | sets …`、end_flags を ` | ends …` で付加したもの。読み込み時 set_flags/end_flags の `" | "` は `", "` に置換される。

#### SpeakerIndex

```text
map[key][]string（script_order の Entries を順に走査し、Speaker 非空のものだけを重複排除して追加）
```

SpeakersFor(key) = strings.Join(list, "/")、無ければ空文字。IsShared(key) = len(list) > 1。遅延構築（_speakers == null のとき BuildSpeakers）。

#### ExportStats

```text
Written int / Resolved int / Unresolved int / Untranslated int / LineRows int / Fresh bool / HasScriptOrder bool
```

R20/R21 の集計とメッセージ生成に必要な全状態。Written は hash 行のみ、LineRows は台詞ID行のみ。

### 規則

#### R1. 作業コピーのパスは <pluginDirectory>/Translations/_discovered/<locale>.working.csv で固定。

`FileNameFor(locale) => locale + ".working.csv"`、`PathFor` は `Path.Combine(pluginDirectory, "Translations", "_discovered", FileNameFor(locale))`。書き込み前に `Directory.CreateDirectory(Path.GetDirectoryName(path))`。読み込み元の公開ファイルは `Path.Combine(pluginDirectory, "Translations", locale, "strings.csv")`。

**Goでの注意**: filepath.Join を使う。locale はそのまま連結されるだけで正規化・検証されない（`Path.Combine` の挙動: locale が絶対パスなら前方を捨てる。Go の filepath.Join は挙動が異なるので注意）。

#### R2. 公開ファイル <locale>/strings.csv が存在しなければ fresh 扱いとなり、既存訳の読み込みを一切行わない。

`bool fresh = !File.Exists(published);` と `foreach (var row in fresh ? new List<Dictionary<string,string>>() : CsvReader.ReadRows(published))`。fresh のとき translations / fileOrder / lineTranslations はすべて空のまま。結果として全行 translation 空、`untranslated == written`、line 行は `order.IsShared(e.Key)` が真の箇所だけに出る。

**Goでの注意**: fresh はファイル存在のみで判定。中身が空・ヘッダのみでも fresh ではない（その場合は読み込みが走り、行が0件になるだけ）。

#### R3. 既存 strings.csv の各行で、key 列を Trim した値が LooksLikeLineId なら台詞ID行として lineTranslations に入れ、その行の処理を打ち切る。

`key = key?.Trim(); if (TranslationKey.LooksLikeLineId(key)) { if (!string.IsNullOrEmpty(tr)) lineTranslations[key] = tr; continue; }`。translation が空なら記録しない（＝捨てる）。key は小文字化されず、Trim 後の原形のまま辞書キーになる。同一 line ID が複数回現れた場合は後勝ち（`lineTranslations[key] = tr` の代入）。

**Goでの注意**: LooksLikeLineId の定義: value != null かつ `value.Length > 5`（"line:" の長さ超）かつ `value.Length <= 64` かつ Ordinal で "line:" 開始、かつ 6文字目以降が全て `[0-9a-zA-Z_.-]`。大文字を許す点に注意。辞書は StringComparer.Ordinal（大文字小文字を区別）。

#### R4. 台詞ID行でない場合、key は小文字化され、key が空で source_en が非空なら source_en のハッシュから key を導出する。それでも空なら行を捨てる。

`key = key?.ToLowerInvariant(); if (string.IsNullOrEmpty(key) && !string.IsNullOrEmpty(src)) { key = TranslationStore.KeyFor(src); } if (string.IsNullOrEmpty(key)) { continue; }`。key と source_en の両方がある場合、key が優先され source_en は完全に無視される。

**Goでの注意**: `ToLowerInvariant` は不変カルチャ。Go の strings.ToLower は Unicode 全体を小文字化するため、トルコ語ロケール差はないが、key 列に非ASCIIが入ったときの結果は厳密には一致しない可能性がある（実データはhex想定）。

#### R5. この読み込みループは key の形式検証も key と source_en の整合検証も行わない。TranslationStore.ResolveKey や hash-strings.ps1 とは挙動が異なる。

WorkingCopy.Export の読み込みループには `TranslationKey.LooksLikeKey` の呼び出しが無く、`KeyFor(src) != key` の不一致検出も無い。対して `TranslationStore.ResolveKey` は `if (hasKey && !TranslationKey.LooksLikeKey(key))` で分岐し、`if (hasKey && hashed != key) { malformed = true; return null; }` で落とす。hash-strings.ps1 も `if ($key -ne '' -and $key -ne $hashed) { $dropped++; continue }`。

**Goでの注意**: つまり作業コピーには「16桁hexでもline:でもない任意文字列」が key として残り得る。Goのリーダは作業コピー中の key を信頼できないものとして扱い、必要なら別途 LooksLikeKey 相当の検証をかけること。

#### R6. translations 辞書は「最初の出現で順序を確定し、値は最後の出現が勝つ」。

`if (!translations.ContainsKey(key)) { fileOrder.Add(key); } translations[key] = tr ?? string.Empty;`。fileOrder への追加は初回のみ、値の代入は毎回。translation 列が欠落している行は空文字として記録される（`tr ?? string.Empty`）。

**Goでの注意**: 重複キーの扱いが TranslationStore.HashFileInPlace（`if (rows.ContainsKey(key)) continue;` ＝先勝ち）と逆。移植時に取り違えやすい。

#### R7. 英文原文の主たる供給源は、ロードされた Yarn プロジェクトの台詞列挙。ゲーム内でしか取得できない。

`foreach (KeyValuePair<string,string> line in DialogueDumper.EnumerateOrderedLines())` で `line.Key` が英文、`line.Value` が話者。`string key = TranslationStore.KeyFor(line.Key); if (!sources.ContainsKey(key)) { sources[key]=line.Key; speakers[key]=line.Value; scriptOrder.Add(key); }`。初回出現のみ登録。EnumerateOrderedLines は各 YarnProject のノード名を Ordinal 昇順ソートし、RunLine / AddOption 命令の LineID 順に英文を返し、最後に未出力の LocalizedString を話者空で吐く。

**Goでの注意**: Go 側では再現不可能。Go ツールは英文を作業コピーの source_en 列から読むしかない。

#### R8. シーン中の全 TMP_Text からも原文を収集する。ゲーム内でしか取得できない。

`foreach (TMP_Text component in Resources.FindObjectsOfTypeAll<TMP_Text>())`。`if (!TmpTextHook.TryGetTrackedSource(component, out text)) { text = component.text; }`、例外は `catch { continue; }` で当該コンポーネントを飛ばす。`if (string.IsNullOrEmpty(text) || IgnoreRules.IsIgnored(text)) continue;`。新規キーなら `sources[key]=text; scriptOrder.Add(key);`。ここでは speakers に登録しない（＝後段で "UI" にフォールバックする対象）。

**Goでの注意**: IgnoreRules は数値・解像度・Hz/FPS・ビルド刻印・時刻の正規表現＋Translations/ignore.txt の追加パターン＋Exact集合。Trim した文字列に対して判定する。Go 側では再現不要（生成はゲーム側の仕事）。

#### R9. _discovered/strings.csv（発見ダンプ）が存在すれば、その source_en 列からも原文を補う。ここだけはファイルだけで再現できる。

`string discovered = Path.Combine(pluginDirectory, "Translations", "_discovered", "strings.csv"); if (File.Exists(discovered)) { foreach (var row in CsvReader.ReadRows(discovered)) { if (row.TryGetValue("source_en", out string text) && !string.IsNullOrEmpty(text)) { string key = TranslationStore.KeyFor(text); if (!sources.ContainsKey(key)) { sources[key]=text; scriptOrder.Add(key); } } } }`。speakers には登録しない。

**Goでの注意**: この3系統（R7→R8→R9）の登録順がそのまま scriptOrder の順序であり、script_order.csv が無い場合の出力順になる。

#### R10. 出力対象キー集合 all は「scriptOrder（ゲームが持っている分）を先、fileOrder（既存ファイルにしかない分）を後」で重複除去した並び。

`var all = new List<string>(); var seen = new HashSet<string>(StringComparer.Ordinal); foreach (string key in scriptOrder) if (seen.Add(key)) all.Add(key); foreach (string key in fileOrder) if (seen.Add(key)) all.Add(key);`。ゲームが知らないキーも捨てずに残す（クラスコメント: "Rows whose key matches nothing the game has loaded are kept with an empty source_en rather than dropped."）。

**Goでの注意**: all の各キーはちょうど1回だけ hash 行として出力される（WriteOrdered の done と leftovers が補集合関係）。したがって written == all.Count。

#### R11. 並び順データは ScriptOrder.Load が _discovered/script_order.csv を優先、無ければ data/script_order.csv を読む。どちらも無ければ null。

`string path = File.Exists(generated) ? generated : (File.Exists(shipped) ? shipped : null); if (path == null) return null;`（generated = Translations/_discovered/script_order.csv、shipped = data/script_order.csv）。読み込み時 `Key = key.Trim().ToLowerInvariant()`、`int.TryParse(o, out int order)`（失敗時は0）。key 列が空の行はスキップ。同ディレクトリの level_flow.csv があれば LevelMeta も読む。読み込み中の例外は null 返却。パス＋最終更新時刻でキャッシュ。

**Goでの注意**: script_order.csv のヘッダは `section,phase,node,order,line_id,key,speaker,condition`、level_flow.csv は少なくとも `level,dragon,weather,set_flags,end_flags` を持つ。両方リポジトリにコミットされるのでGoでも読める。

#### R12. 出力の1行目は必ず固定ヘッダ `key,section,node,order,speaker,source_en,translation`（7列）。

`writer.WriteLine("key,section,node,order,speaker,source_en,translation");`。クラス冒頭コメントには `key,speaker,source_en,translation` と書かれているが、これは古い記述であり実装と一致しない。公開ファイル側のヘッダは `key,section,node,order,speaker,translation`（6列、source_en 無し）で、tools/check-translations.py はこの source_en の有無で作業コピーと公開ファイルを見分ける。

**Goでの注意**: Go 側は列位置ではなくヘッダ名（大文字小文字無視）で解決すべき。CsvReader の行辞書は StringComparer.OrdinalIgnoreCase。

#### R13. hash 行（Emit）の出力形式: key は無エスケープ、order も無エスケープ、section/node/speaker/source_en/translation は CsvReader.Escape を通す。

`writer.WriteLine(key + "," + CsvReader.Escape(section) + "," + CsvReader.Escape(node) + "," + ord + "," + CsvReader.Escape(who ?? string.Empty) + "," + CsvReader.Escape(src ?? string.Empty) + "," + CsvReader.Escape(tr ?? string.Empty));`。src が null（ゲームが未ロード）なら source_en は空文字。

**Goでの注意**: key を無エスケープで書く点が R5 と組み合わさると危険（後述 edgeCases）。Go で書き戻す場合は素直に全列エスケープした方が安全だが、そうするとバイト一致しなくなる。

#### R14. hash 行の speaker は「script_order の全話者 → Yarn列挙の話者 → その出現の話者 → "UI"」の優先順で決まる。

`string who = order?.SpeakersFor(key); if (string.IsNullOrEmpty(who)) speakers.TryGetValue(key, out who); if (string.IsNullOrEmpty(who)) who = fallbackSpeaker; if (string.IsNullOrEmpty(who) && src != null) who = "UI";`。`SpeakersFor` は script_order の Entries を走査し、speaker 非空のものを出現順に重複排除して `string.Join("/", list)`（例 "Ryan/Alexander"）。fallbackSpeaker は順序付き行では `e.Speaker`、leftovers 行では空文字。

**Goでの注意**: src == null（原文未取得）の行は speaker が空のままになる。"UI" が入るのは原文が取れているときだけ。

#### R15. script_order がある場合、ScriptOrder.WriteOrdered が Entries 順に走査し、出力対象がある箇所でだけセクション見出し・ノード見出しを印字する。

`bool hashRow = keysPresent.Contains(e.Key) && !done.Contains(e.Key); bool lineRow = emitLine != null && !string.IsNullOrEmpty(e.LineId) && wantLine(e); if (!hashRow && !lineRow) continue;`。セクション変化時: 空行 + `"# ===== " + header + " ====="`（header は level_flow から引けるなら `LevelMeta.Header`＝`$"Level {Index+1}: {Dragon}"` + weather/sets/ends の付加、引けなければ SectionTitle: Cutscene→"Cutscenes (started by game code)"、Reaction→"Dragon reactions (started by game code)"、Unused→"Unused nodes (not reachable in the current game)"、既定はセクション名そのまま）。セクション変化で lastNode は null にリセット。ノード変化時: `"# --- " + (phase 非空なら phase + ": ") + node + (condition 非空なら " | if " + condition) + " ---"`。hash 行→emit→done.Add、その後 line 行→emitLine（同一 Entry で両方出るときは hash 行が先）。最後に `leftovers = keysPresent.Where(k => !done.Contains(k)).ToList()`（all の順序を保つ）。

**Goでの注意**: 見出しは `#` コメント行なので CsvReader は読み飛ばす。section/node/order は列としても入っているので、Go 側は見出しを解析しなくても構造を復元できる。

#### R16. 台詞ID行を出す条件は「その英文が複数キャラに共有されている」または「既存ファイルにその line ID の訳がある」。

`e => order.IsShared(e.Key) || lineTranslations.ContainsKey(e.LineId)`。`IsShared(key)` は script_order から作った key→話者リストの `list.Count > 1`。共有行には訳が空の line 行が各出現位置に並び、翻訳者が必要な箇所だけ埋める運用（CONTRIBUTING.md「Lines said by more than one character」）。

**Goでの注意**: hash 行が無くても（そのキーが all に無くても）lineRow だけで見出しが立ち、line 行が単独で出ることがある。

#### R17. 台詞ID行の出力形式: key 列が line ID、speaker はその出現の単一話者、source_en は共有元キーの英文、translation は既存ファイルの該当訳。出力後 lineTranslations から削除する。

`sources.TryGetValue(e.Key, out string src); lineTranslations.TryGetValue(e.LineId, out string tr); lineTranslations.Remove(e.LineId); writer.WriteLine(e.LineId + "," + CsvReader.Escape(e.Section) + "," + CsvReader.Escape(e.Node) + "," + e.Order + "," + CsvReader.Escape(e.Speaker) + "," + CsvReader.Escape(src ?? string.Empty) + "," + CsvReader.Escape(tr ?? string.Empty)); lineRows++;`。speaker は hash 行のような "A/B" 連結ではなく `e.Speaker` 単体である点が hash 行と異なる。

**Goでの注意**: Remove があるため、orphan ブロックに残るのは script_order のどの Entry の line_id とも一致しなかったものだけ。

#### R18. script_order が知らないキーは最後にまとめ、直前に UI 見出しを置く。section 列は "UI"、node/order は空。

`if (leftovers.Count > 0) { writer.WriteLine(); writer.WriteLine("# ===== UI and other text (not part of the dialogue script) ====="); }`（この見出しは order != null のときだけ出る）。続けて `foreach (string key in leftovers) { Emit(key, order == null ? "" : "UI", "", "", ""); }`。

**Goでの注意**: order == null の場合は見出しも一切出ず、section 列も空文字。leftovers は all そのもの（`leftovers = all;`）。

#### R19. script_order に一致しなかった台詞ID訳は、専用見出しの下に7列・他列すべて空で出力する。

`if (lineTranslations.Count > 0) { writer.WriteLine(); writer.WriteLine("# ===== Per-line translations not found in the script order ====="); foreach (KeyValuePair<string,string> kv in lineTranslations) { writer.WriteLine(kv.Key + ",,,,,," + CsvReader.Escape(kv.Value)); lineRows++; } }`。`kv.Key` の後にカンマ6個＝7フィールド（key, section='', node='', order='', speaker='', source_en='', translation=値）。コメント通り「ゲームが更新された、または順序データが無い」場合に作業を失わないための救済。

**Goでの注意**: 列挙順は Dictionary の列挙順。R17 の Remove が挟まるため .NET の挿入順保証は崩れ得る。Go の map は順序不定なので、バイト一致を狙うならこの順序は再現できない（openQuestions 参照）。

#### R20. 集計は written/resolved/unresolved/untranslated が Emit（hash 行）呼び出し単位、lineRows が台詞ID行単位。

Emit 内: `if (src != null) resolved++; else unresolved++;`（sources に key があるか＝ゲームが英文を持っているか）、`if (string.IsNullOrEmpty(tr)) untranslated++;`（translations に無い、または空文字）、末尾で `written++;`。line 行は written を増やさず lineRows のみ増やす（R17 と R19 の両方で）。したがって恒等式は `resolved + unresolved == written`、`untranslated <= written`、`written == all.Count`。ファイルの実データ行数は `written + lineRows`（見出しコメント行・空行は含まない）。

**Goでの注意**: unresolved の意味は「作業コピーに source_en を書けなかった行」。resolved/untranslated は独立で、両方立つ（英文あり・未訳）のが通常の作業対象行。

#### R21. 戻り値メッセージは固定の文面で、fresh と order==null の2つの追記が前から順に連結される。

`string ordered = order == null ? " No script order data found (Export game flow with a level loaded), so rows are in discovery order." : ""; if (fresh) ordered = $" {locale}/strings.csv did not exist, so this is a fresh start with every line the game has loaded." + ordered;` 本文は `$"[working] Wrote {written} row(s) to _discovered/{FileNameFor(locale)}: {resolved} with English, {unresolved} whose text the game has not loaded, {untranslated} still untranslated, plus {lineRows} per-line row(s) for English said by more than one character.{ordered} Edit this file; hot reload applies it. Hash it before committing."`。fresh と order==null が同時なら fresh 文が先、order 文が後。

**Goでの注意**: 各追記は先頭に半角スペースを持ち、本文の `.` の直後に連結される。

#### R22. Export 全体は try/catch で包まれ、例外時はファイルを書いた途中であっても例外メッセージだけを返す。

`catch (Exception ex) { return $"[working] Failed: {ex.Message}"; }`。StreamWriter は using なので途中まで書かれたファイルは残る（append: false なので既存は切り詰め済み）。

**Goでの注意**: 失敗時に旧作業コピーが失われ得る。Go 側で生成を扱うなら一時ファイル＋rename が望ましいが、これは現行仕様ではない。

#### R23. 出力エンコーディングは BOM 無し UTF-8、改行は実行環境の Environment.NewLine（Windows では CRLF）。

`using (var writer = new StreamWriter(path, append: false, new UTF8Encoding(false)))`。`UTF8Encoding(false)` は BOM を出さない。StreamWriter.WriteLine は既定で `Environment.NewLine`。

**Goでの注意**: Go で書き戻すなら CRLF を選ぶか、読み側が両対応であることに依存するか決める必要がある（読み側 CsvReader は裸の `\r` を無視するので LF/CRLF 両対応）。

#### R24. このファイルを読むパーサは CsvReader.Parse と同じ規則に従う必要がある（作業コピー自身も strings.csv も同じリーダで読まれる）。

(a) 全文を `new StreamReader(fs, Encoding.UTF8, true)` で読む＝BOM は自動検出・除去。(b) レコード先頭（`c == '#' && fields.Count == 0 && field.Length == 0`、かつ引用符外）の `#` はコメントで、次の `\n` まで読み飛ばす。(c) 引用符外の `\r` は位置を問わず単に捨てられる。(d) 引用符外の `\n` で、fields が空かつ field が空なら空行としてレコードにしない。(e) 引用符外の `"` はどの位置でも引用モードを開始し、引用中の `""` はリテラル `"`、単独 `"` で引用終了（以降も同じフィールドに追記される）。(f) 末尾は `if (field.Length > 0 || fields.Count > 0)` でフラッシュ。(g) records[0] がヘッダ。各行は `Dictionary<string,string>(StringComparer.OrdinalIgnoreCase)` に header 名で詰められ、行の列数が足りなければ空文字、ヘッダより多い列は捨てられる。ヘッダ名重複時は後の列が勝つ。またファイルは `FileShare.ReadWrite | FileShare.Delete` で共有読み（Excel で開いたままでも読める）。

**Goでの注意**: encoding/csv は (b)(c)(e) の緩い引用処理と互換でない。手書きパーサか、LazyQuotes + 前処理が必要。特に (e)「フィールド途中からの引用開始」と (c)「裸 \r の無条件除去」は encoding/csv では再現できない。

#### R25. エスケープ規則は「`,` `"` `\n` `\r` のいずれかを含むときだけ全体を二重引用符で囲み、内部の `"` を `""` に置換」。null は空文字。

`bool needsQuotes = value.IndexOfAny(new[] { ',', '"', '\n', '\r' }) >= 0; if (!needsQuotes) return value; return "\"" + value.Replace("\"", "\"\"") + "\"";`。前後の空白やタブでは引用しない。

**Goでの注意**: Go の encoding/csv.Writer は前後空白などでも引用する条件が異なるため、バイト一致を狙うならこの関数を自前で移植すること。

#### R26. key（ハッシュ）は SHA-256(UTF-8 bytes of source) の先頭8バイトを小文字hexにした16文字。

`TranslationStore.KeyFor(source) => HashCache.GetOrAdd(source, TranslationKey.Hash)`、`TranslationKey.Hash`: `byte[] digest = _sha.ComputeHash(Encoding.UTF8.GetBytes(source)); for (int i = 0; i < Length / 2; i++) sb.Append(digest[i].ToString("x2"));`（`Length = 16`）。source が null なら空文字を返す。トリムもタグ除去もしない（クラスコメント: "exactly as TMP received it (no trimming, tags included)"）。

**Goでの注意**: Go: `h := sha256.Sum256([]byte(s)); hex.EncodeToString(h[:8])`。正規化を挟まないこと。

#### R27. key の形式判定は2種類あり、用途が異なる。

`LooksLikeLineId(v)`: v != null && `v.Length > "line:".Length` && `v.Length <= 64` && Ordinal で "line:" 開始 && 6文字目以降が全て `[0-9a-zA-Z_.-]`。`LooksLikeKey(v)`: `v.Length != Length`（16）なら false、全文字が `[0-9a-f]`（小文字hexのみ、大文字は false）。WorkingCopy.Export は LooksLikeLineId のみを使い LooksLikeKey は使わない（R5）。

**Goでの注意**: tools/hash-strings.ps1 の対応正規表現は `'^line:[A-Za-z0-9_.\-]{1,59}$'` で、最大長 64 と一致する。

#### R28. script_order が無い場合（order == null）、見出しは一切出ず、全行が all の順序（＝発見順）で section/node/order 空のまま出力される。

`if (order == null) { leftovers = all; } else { ... }`、`Emit(key, order == null ? "" : "UI", "", "", "")`。speaker は `order?.SpeakersFor(key)` が null になるため `speakers[key]`（Yarn 由来）→ 空 → src != null なら "UI"。台詞ID行は WriteOrdered を通らないため R16 の共有行展開が起きず、既存ファイルから拾った lineTranslations が全て R19 の orphan ブロックに落ちる。

**Goでの注意**: この分岐は Go ツールが「section 列が空の作業コピー」を受け取り得ることを意味する。section/node/order を必須前提にしないこと。

#### R29. Go ツールが作業コピーを『読む』ために最小限必要なのは、R24 のパーサ・R12 のヘッダ名解決・R3/R4 の key 解決・R26 のハッシュだけ。R7/R8 のゲーム内収集は不要。

読み取りに必要な最小手順: (1) R24 のパーサで全レコードを取る（コメント `#` 行・空行は捨てられる。見出しの意味が要るなら別途生テキストから拾う）。(2) ヘッダ名を大文字小文字無視で引く。列は key / section / node / order / speaker / source_en / translation。(3) key を Trim。LooksLikeLineId なら台詞ID行（section/node/order/speaker/source_en は参考情報、訳は translation のみが意味を持つ）。(4) そうでなければ ToLowerInvariant。空かつ source_en 非空ならハッシュ導出。空なら無視。(5) 検証をかけるなら TranslationStore.ResolveKey 相当（LooksLikeKey、key と KeyFor(source_en) の一致）を自前で追加する。生成側（R7/R8）はゲーム内専用なので Go では実装しない。

**Goでの注意**: 公開ファイルを作り直す処理まで書くなら tools/hash-strings.ps1 が既にファイルだけで完結した参照実装になっており、そちらの規則（空 translation は publish しない、重複キーは先勝ち、line 行は script_order の位置に差し込む）を移植対象とすべき。WorkingCopy.Export とは重複キーの勝ち負けが逆なので注意。

### 境界条件

- key 列が16桁hexでも line: でもない任意文字列（翻訳者が公開ファイルの末尾に `English,訳` を足したケース）の場合、WorkingCopy.Export はそれを検証せず ToLowerInvariant しただけで key として採用し、R13 で無エスケープのまま書き出す。その文字列にカンマ・二重引用符・改行が含まれると、生成された作業コピーの CSV 構造が壊れる。TranslationStore.ResolveKey はこのケースを `source = rawKey; return KeyFor(rawKey);` として救済するが、WorkingCopy は救済しない。
- key と source_en が両方あり、かつ `KeyFor(source_en) != key` の場合、WorkingCopy.Export は key を優先して source_en を捨てる（不一致を検出しない）。一方 TranslationStore.ResolveKey と hash-strings.ps1 はその行を malformed として落とす。ゲーム更新で英文が変わった直後に挙動が分かれる。
- 既存 strings.csv に同じ key が複数回現れると、出力順は最初の出現位置、translation は最後の出現の値（R6）。HashFileInPlace の先勝ちと逆。
- translation 列が存在しないファイル（ヘッダに translation が無い）を読むと、`row.TryGetValue("translation", out tr)` が false で tr は null、`tr ?? string.Empty` で空文字になり、全行が untranslated としてカウントされる。
- 台詞ID行で translation が空のものは lineTranslations に入らない（R3）。そのため次回の生成では「共有行だから」という理由（IsShared）でしか再出力されない。共有でない line 行に空訳を書いても消える。
- 同一 line ID が既存ファイルに複数回現れると後勝ち（`lineTranslations[key] = tr`）。key の比較は Ordinal なので `line:ABC` と `line:abc` は別物として扱われる。
- R19 の orphan ブロックの出力順は Dictionary の列挙順であり、R17 の `lineTranslations.Remove` が挟まるため .NET の挿入順が保たれる保証はない。Go の map は順序不定なので、この順序を再現しようとすると必ずずれる。
- order == null（script_order.csv が無い）と fresh（strings.csv が無い）は独立に起こり得る。両方同時なら、見出し無し・全列 section/node/order 空・全訳空・line 行ゼロ（lineTranslations も空）の、単純な発見順一覧になる。
- BOM: 読み側は `new StreamReader(fs, Encoding.UTF8, true)` で BOM を自動除去するが、書き側は `new UTF8Encoding(false)` で BOM を出さない。Excel で保存し直して BOM が付いても読み込みは通る。
- 改行コード: 書き出しは Environment.NewLine（Windows なら CRLF）。読み込みは引用符外の `\r` を無条件に捨てるため CRLF/LF 双方を受け付ける。ただし引用符の内側の `\r` は保持されるので、CRLF 環境で書かれた複数行フィールドを読むと `\r` が値に残る。
- 引用フィールドの途中で `"` が閉じた後も同じフィールドへの追記が続く（R24 (e)）。`a"b"c` は `abc` になる。RFC4180 厳密実装や Go の encoding/csv とは異なる。
- `#` はレコード先頭でのみコメント。引用符の内側にある `#` はコメントにならない。ただし tools/hash-strings.ps1 の Read-Csv は行単位で `StartsWith('#')` を弾くため、引用内の複数行フィールドが `#` で始まる行を含むと PowerShell 側だけが壊れる（実装間の非対称）。
- ヘッダより列数が多い行は余分な列が捨てられ、少ない行は不足分が空文字になる。作業コピーの source_en に生のカンマが入っていてエスケープが壊れた場合、静かに列がずれる。
- TMP_Text 走査中の例外は `catch { continue; }` で握り潰され、そのコンポーネントの原文は収集されない。結果として作業コピーの source_en が空（unresolved）になる行が増えるが、エラーは報告されない。
- `Resources.FindObjectsOfTypeAll<TMP_Text>()` の結果はロード済みシーンに依存するため、同じ locale でも起動状況によって unresolved の数と行数が変わる。生成は非決定的。
- Export は途中で例外が出ても `append: false` で既に切り詰めた出力ファイルを残す（R22）。翻訳者の作業コピーが部分的に失われ得る。
- locale 名は検証されない。`Path.Combine` に渡されるだけなので、パス区切りを含む locale は想定外の場所を指す。

### 敵対検証で見つかった食い違い

#### [medium] 「英文が取れているか」の判定が C# では `src != null`（辞書に存在するか）なのに、仕様は R13/R14/R20 でそれを `src が null` としか書いていない。Go には nil string が無いため素直に `src != ""` と移植され、しかも sources には実際に空文字の英文が入り得るため、resolved/unresolved の集計と speaker の "UI" フォールバックが元実装とずれる。

- 根拠: WorkingCopy.cs:151-153 `sources.TryGetValue(key, out string src); if (src != null) resolved++; else unresolved++;`、同 159 `if (string.IsNullOrEmpty(who) && src != null) who = "UI";`。sources に空文字が入る経路は DialogueDumper.cs:289-295 の未参照 LocalizedString 掃き出しループで、283 行目にある `text.Length > 0` ガードがこちらには無く `kv.Value` をそのまま yield している。そのため WorkingCopy.cs:84-90 で `sources[KeyFor("")] = ""` が成立し得る。このとき元実装は resolved++ かつ speaker="UI" だが、source_en 列は空で出力される。仕様 R14 goNote の「"UI" が入るのは原文が取れているときだけ」はこのケースで成り立たない。
- 直し方: R13/R14/R20 の条件を「`src, ok := sources[key]` の ok で判定する。値が空文字でも存在すれば resolved 扱い・"UI" フォールバック対象。source_en 列が空であることと unresolved であることは同値ではない」と書き換える。

#### [medium] R19（orphan 台詞ID行）の出力順を「Dictionary の列挙順」「Remove が挟まるため挿入順が保たれる保証はない」「Go の map は順序不定なので必ずずれる」としているが、実際には決定的で Go でも再現できる。この誤った前提の上に openQuestions の1番目（順序を決め直す必要がある＝現行実装との差異を許容する）が立っている。

- 根拠: lineTranslations への挿入は WorkingCopy.cs:51-76 の読み込みループで全て完了し、Remove は WorkingCopy.cs:179（WriteOrdered 実行中）でしか呼ばれない。削除後の Add が一切無いため、.NET/Mono の Dictionary（entries 配列を索引順に列挙し、削除は hashCode を -1 にして穴を残すだけで詰め直さない）では生存エントリの列挙順＝挿入順になる。すなわち出力順は「公開 strings.csv 中で各 line ID が最初に現れた順」（重複行は WorkingCopy.cs:59 で値だけ更新され位置は動かない）で一意に決まる。
- 直し方: 「orphan ブロックの順序＝公開 strings.csv 中の line 行の初出順。Go では挿入順スライス＋map で完全に再現できる」と訂正し、openQuestions 1 の『決定的な順序に定め直す必要がある』という前提を撤回する。ただし Dictionary の列挙順は公式には未規定である旨は注記しておく。

#### [low] R26 の「TranslationStore.KeyFor は source が null なら空文字を返す」が誤り。Go には null が無いので、この規則は自然に `if s == "" { return "" }` に落ちるが、元実装の KeyFor("") は空文字ではなく sha256("") 先頭8バイト（e3b0c44298fc1c14）を返す。

- 根拠: TranslationStore.cs:69-72 `public static string KeyFor(string source) { return HashCache.GetOrAdd(source, TranslationKey.Hash); }`。HashCache は ConcurrentDictionary で、GetOrAdd は key が null なら ArgumentNullException を投げるため、TranslationKey.cs:24-27 の `if (source == null) return string.Empty;` には KeyFor 経由では到達しない。空文字は正当なキーなのでハッシュが計算される。なお R26 の goNote 自体（`hex.EncodeToString(h[:8])`、null 分岐なし）は正しく、rule 文と goNote が矛盾している。
- 直し方: 「KeyFor は常にハッシュを計算する（空文字も例外ではない）。null 相当の入力は呼び出し側が弾く（WorkingCopy.cs:63 の `!string.IsNullOrEmpty(src)` ガード）」に訂正する。

#### [low] 同一 line ID が既存ファイルに複数回現れたときの扱いを R3 と edgeCases が単に「後勝ち」としているが、正しくは「最後の非空が勝つ」。hash 行の「後勝ち（空でも上書き）」と非対称で、edgeCases だけ読むと取り違える。

- 根拠: WorkingCopy.cs:59 `if (!string.IsNullOrEmpty(tr)) lineTranslations[key] = tr;` は空訳のとき代入自体を行わないので、`line:x,訳` の後に `line:x,`（空）があっても訳は残る。一方 WorkingCopy.cs:75 `translations[key] = tr ?? string.Empty;` は無条件代入なので、hash 行は後続の空訳で上書きされて消える。edgeCases 3番目（hash 行）と6番目（line 行）が同じ「後勝ち」という語で書かれており区別が付かない。
- 直し方: 「line 行＝空は無視し非空のみ後勝ち／hash 行＝空を含めて後勝ち」と明示的に書き分ける。

#### [low] R7 の DialogueDumper.EnumerateOrderedLines の順序記述が3点不正確（Go では再現しない前提なので実害は小さいが、scriptOrder の登録順＝R9 goNote が主張する『order==null 時の出力順』の根拠になっている）。

- 根拠: DialogueDumper.cs:252-296。(1) 「RunLine / AddOption 命令の LineID 順」とあるが、実際は LineID でのソートは無く命令列の出現順（268-287行）。(2) 「最後に未出力の LocalizedString を吐く」とあるが、実際は YarnProject ごとに、そのプロジェクトのノード走査直後に吐く（289-295行）ので、複数プロジェクトがあると project1ノード群→project1未参照→project2ノード群→… の順になる。(3) `emitted` は 258 行でプロジェクトごとに作り直されるため、プロジェクトをまたいだ lineId 重複排除は効かない（実際の重複排除は WorkingCopy.cs:85 の `sources.ContainsKey`）。
- 直し方: 「プロジェクトごとに（ノード名 Ordinal 昇順 → 各ノードの命令出現順）→ そのプロジェクトの未参照 LocalizedString（話者は空、英文が空文字でも出る）」と書き直す。

### 抽出が取りこぼしていた規則

- CsvReader.ReadRows はヘッダ列名を Trim しない（CsvReader.cs:35 `dict[header[c]] = ...` に生の列名をそのまま使う）。`key, section, node` のように空白入りヘッダのファイルでは " section" という列名になり、section 列が引けなくなる。Go 側で列名を Trim/正規化すると挙動が変わる。
- CsvReader.cs:18 の `new StreamReader(fs, Encoding.UTF8, true)` は UTF-8 BOM だけでなく UTF-16/UTF-32 の BOM も検出してその符号化で読む。また Encoding.UTF8 の既定フォールバックは置換なので、不正な UTF-8 バイトは例外にならず U+FFFD になる。Go 側で utf8 検証してエラーにすると挙動が変わる。
- ScriptOrder.Load の level_flow.csv 読み込み（ScriptOrder.cs:263-270）で、`level` 列が int にパースできない行はスキップされ、同じ Section（`L{Index+1:00} {Dragon}`）が重複した場合は後勝ち（`data.Levels[meta.Section] = meta`）。
- ScriptOrder.cs:251 の `int.TryParse(o, out int order)` は NumberStyles.Integer + CurrentCulture なので、前後の空白と先頭符号を許容する（Go の strconv.Atoi は空白を拒否する）。失敗時 0 は仕様に記載済みだが許容範囲の差は未記載。
- key の Trim は C# の `string.Trim()`（char.IsWhiteSpace 基準で NBSP U+00A0 や全角空白 U+3000 も除去）。Go の strings.TrimSpace とは対象文字集合が完全一致しない。
- ScriptOrder.WriteOrdered は最初のセクション見出しの前にも必ず空行を1行出力する（ScriptOrder.cs:310 の `w.WriteLine()` は初回も実行される）。結果として作業コピーは「CSVヘッダ行 → 空行 → `# ===== ... =====`」で始まる。
- `LINE:xxxx` のように prefix が大文字の行は LooksLikeLineId（Ordinal 比較）に該当せず、WorkingCopy.cs:62 で `line:xxxx` に小文字化されたうえで hash 行として無検証で書き出される。その出力を次回読み込むと今度は台詞ID行として解釈されるため、1往復で行の意味が変わる。
- R13 で key が無エスケープなことの帰結として、カンマ・引用符・改行だけでなく「`#` で始まる key」も壊れる。次回読み込み時に CsvReader.cs:75 のコメント判定でレコードごと捨てられ、訳が静かに消える。同様に `"` で始まる key は引用フィールド開始として解釈される。
- order != null でも data.Entries が空（script_order.csv がヘッダのみ、または key 列が全行空）の場合、WriteOrdered は何も出力せず leftovers == all になるため、UI 見出しが出て全行の section 列が "UI" になる（order == null 時の『見出し無し・section 空』とは別の出力になる）。
- ScriptOrder.Load はプロセス静的キャッシュ（path + 最終更新時刻）を持ち、キャッシュヒット時は同一 Data インスタンスを返す（_speakers も構築済みのまま再利用される）。例外時は null を返すが _cached は更新されないので、以前のキャッシュが残る。

### 未決の点

- R19 の orphan 台詞ID行の出力順が仕様として意味を持つのか、単なる実装都合なのかが判断できなかった。Go 側で生成やバイト比較を行うなら、line ID の昇順など決定的な順序に定める必要があるが、それは現行実装との差異になる。方針決定が必要。
- クラス冒頭コメントの `key,speaker,source_en,translation` は実際の7列ヘッダと食い違っている。コメントが古いだけと推測したが（README.md / CONTRIBUTING.md はいずれも7列版を記載しているため）、意図的な別形式の名残である可能性は排除できていない。
- R5 の「key 形式を検証しない」がバグなのか意図的な緩さなのか判断材料が無い。TranslationStore.ResolveKey が明示的に救済ロジックを持っているのに WorkingCopy が持たないのは非対称で、Go 移植時にどちらへ寄せるべきかは設計判断が要る。
- Go ツールの用途が「作業コピーを読んで編集支援する」までなのか「書き戻す／公開ファイルを生成する」まで含むのかが指示から確定できなかった。書き戻すなら R13 の無エスケープ key や R23 の改行コードをどう扱うか（バイト一致を狙うか、より安全な出力にするか）を先に決める必要がある。
- 作業コピーの `#` 見出し行（セクション／ノード）を Go ツールが保持すべきかどうかが不明。CsvReader は見出しを捨てるため、読んで書き戻す実装だと見出しが消える。section/node/order 列から再構築できるが、level_flow.csv が無いとセクション見出しの文言（LevelMeta.Header）を復元できない。
- WriteOrdered の `keysPresent.Contains(e.Key)` は List<string> に対する線形探索で、script_order が約1839行・キーが数千件という規模では O(n*m) になる。現行で実用上問題になっていないのか（ゲーム内の一括処理なので許容されている）は未検証。Go では素直に集合にすべきだが、挙動は変わらないと推測している。
- `fresh` 判定がファイル存在のみであることが意図通りか未確認。空ファイルやヘッダのみの strings.csv は fresh 扱いされず、メッセージに fresh 文が出ない。
- TmpTextHook.TryGetTrackedSource / IgnoreRules の詳細は生成側の話として概要のみ確認した。Go ツールが原文の再収集を行わない前提なら不要だが、もし「作業コピーの source_en を検証する」要件があるなら、どの文字列が意図的に除外されているかを別途仕様化する必要がある。

## 実データの形式

### データ構造

#### ScriptOrderRow

```text
Section string / Phase string / Node string / Order string（数値だが出力に文字列のまま使うので string 保持）/ LineID string（`line:` 接頭辞込み、空にはならない）/ Key string（16桁小文字16進）/ Speaker string / Condition string
```

data/script_order.csv の1行。1839件。全列非nullだが Phase と Condition は空文字になりうる。ファイル順そのものが「ゲームが再生する順」で、出力順の唯一の権威。Order を int にパースし直して出力すると元の文字列と差が出る恐れがあるため string のまま持つ。

#### LevelFlowRow

```text
FlowAsset string（全行 "LevelFlow"）/ Level string→int / Dragon string / Intro string / ProgressDialogs string / IdleDialogs string / NagDialogs string / Phone string / Outro string / JerkoffDialog string / CumDialog string / MountStart string / MountFinish string / SpawnFlag string / SetFlags string / EndFlags string / Weather string / PlayerSpawn string
```

data/level_flow.csv の1行。15件（Level 0..14）。BOM付きファイル。複数値セルは ` | ` 区切り。見出し生成に使うのは Level・Dragon・Weather・SetFlags・EndFlags の5列だけで、残り13列は strings.csv の生成には一切関与しない（ノード名とセクションの対応付けは script_order.csv 側が持つ）。

#### PublishedRow

```text
Key string（16桁hex または `line:` 形式）/ Section string / Node string / Order string / Speaker string / Translation string
```

Translations/<locale>/strings.csv のデータ行。6列固定。UIセクションでは Node と Order が空文字。line: 行の Speaker は単独キャラ、ハッシュ行の Speaker は `/` 連結されうる。埋め込み改行を含むフィールドは実データに0件。

#### InputRow

```text
Key string（任意、空可）/ SourceEn string（任意列。published ファイルには存在してはならない）/ Speaker string（任意）/ Translation string / ほかの列は無視
```

翻訳者の作業コピー（Translations/_discovered/<locale>.working.csv）または published ファイル自身を入力として読むときの行。PowerShell 側は `$r.PSObject.Properties['...']` で列の有無を見ているので、Go では map[string]string 方式（csv.Reader + ヘッダ名→添字）にして「列が無い＝空文字」として扱う。

#### BuildState

```text
Rows map[string]{Speaker,Translation}（ハッシュキー→採用行）/ InputOrder []string（ハッシュキーの入力初出順、UIブロックの並びに使う）/ LineRows *OrderedMap[string]string（line ID→翻訳、挿入順保持）/ Speakers map[string][]string（キー→話者の初出順リスト）/ Done map[string]bool（出力済みハッシュキー）/ LastSection, LastNode string
```

生成器の内部状態。InputOrder と LineRows と Speakers の3つは挿入順が出力に直結するので、Go の map 単体では実装できない（slice + 存在確認 map の組、または順序保持マップが必須）。

### 規則

#### R1. 5ファイルすべて改行はLFのみ。CRは1バイトも存在しない。全ファイル末尾に改行が1個あり、末尾空行（LFLF）はない。

`grep -c $'\r'` が全ファイル0。`tail -c 48 | od -c` で全ファイル末尾が `\n` 単独。`git ls-files --eol` は5ファイルとも `i/lf w/lf attr/`（.gitattributes には `*.sh text eol=lf` しかなく、csvは対象外）。なお生成元 tools/hash-strings.ps1 は `StringBuilder.AppendLine()`（= Environment.NewLine）を使っており、Windowsで実行すればCRLFになるはずだが、コミットされている実体はLF。

**Goでの注意**: Goでは明示的に "\n" を書く。`csv.Writer` の `UseCRLF` はデフォルトfalseなのでそのままでよいが、自前で組み立てる場合も Environment.NewLine 相当を使わないこと。

#### R2. BOMは data/level_flow.csv にのみ存在する（EF BB BF）。script_order.csv と3つの strings.csv にはBOMがない。

`head -c 64 | od -c`: level_flow.csv は `357 273 277 f l o w _ a s s e t`（= EF BB BF + "flow_asset"）。script_order.csv は `s e c t i o n`、strings.csv は `k e y ,` から始まる。level_flow.csv だけゲーム内エクスポート（F1 -> Tools -> Export game flow、hash-strings.ps1 の .DESCRIPTION に記載）由来なのでBOM付き。出力側は `WriteAllText(..., New-Object System.Text.UTF8Encoding $false)`（189行目）でBOMなし。

**Goでの注意**: Goの `encoding/csv` はBOMを剥がさない。level_flow.csv をそのまま読むと最初のヘッダ名が "﻿flow_asset" になり、列名引きが必ず失敗する。読み込み前に `bytes.TrimPrefix(b, []byte{0xEF,0xBB,0xBF})` すること。出力時はBOMを付けないこと。

#### R2b. BOM除去は「全入力ファイル」に適用する。元実装はUTF8指定の File.ReadAllLines を使っており、.NETはBOMがあれば自動で剥がす。

tools/hash-strings.ps1 の Read-Csv（53行目）は `[System.IO.File]::ReadAllLines($file, [System.Text.Encoding]::UTF8)`。この overload は detectEncodingFromByteOrderMarks が有効で、先頭BOMは読み取り結果に含まれない。tools/check-translations.py（43行目）も `encoding="utf-8-sig"`。つまり「入力にBOMがあってもなくても同じ結果」が仕様。

**Goでの注意**: 入力読み込み関数を1つに集約し、その中でBOM除去する。ファイルごとに分岐しない。

#### R3. data/script_order.csv は 1840物理行 = ヘッダ1 + データ1839。コメント行・空行・引用符は1つもない。全行が8フィールド固定。

ヘッダ: `section,phase,node,order,line_id,key,speaker,condition`。`grep -c '^$'`=0、`grep -c '^#'`=0、`grep -c '"'`=0、`awk -F, '{print NF}' | sort | uniq -c` は `1839 8` のみ。

**Goでの注意**: 引用符なしの単純CSVだが、将来入る可能性があるのでRFC4180パーサで読むこと。`FieldsPerRecord=8` を明示すると崩れを検出できる。

#### R4. data/level_flow.csv は 16物理行 = ヘッダ1 + データ15（level=0..14）。コメント行・空行・引用符なし。全行18フィールド。

ヘッダ: `flow_asset,level,dragon,intro,progress_dialogs,idle_dialogs,nag_dialogs,phone,outro,jerkoff_dialog,cum_dialog,mount_start,mount_finish,spawn_flag,set_flags,end_flags,weather,player_spawn`。flow_asset は全行 `LevelFlow` 固定。dragon は Ryan/Conrad/Alexander の3値が0始まりで循環。weather は Sunny(12)/Rainy(2: level 4,11)/Night(2: level 5,13)。複数値を持つセルは ` | `（半角スペース+パイプ+半角スペース）区切り。実例: level 4 の end_flags = `MedkitCompleted | level_5_complete`、level 7 の idle_dialogs = `Conrad_idle_3_1 | Conrad_idle_3_2`。最終行 level 14 の player_spawn は空（末尾カンマ）。

**Goでの注意**: `level` は文字列なので `strconv.Atoi` が必要（元コードは `[int]$l.level`、65行目）。

#### R5. 翻訳キーは「ソース英文のUTF-8バイト列に対するSHA-256の先頭8バイトを小文字16進で並べた16文字」。

tools/hash-strings.ps1 の Get-Key（37-43行目）: `$digest = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($text))` のあと `$digest[0..7] | ForEach-Object { $_.ToString('x2') } -join ''`。実ファイルでも `^[0-9a-f]{16}$` に一致しないキーはゼロ件。

**Goでの注意**: `h := sha256.Sum256([]byte(text)); key := hex.EncodeToString(h[:8])`。`hex.EncodeToString` は小文字なので追加のToLowerは不要。

#### R6. キーにはもう1種類、Yarnの行ID `line:xxxxxxxx` がある。受理パターンは `^line:[A-Za-z0-9_.\-]{1,59}$` で、判定は大文字小文字を区別する。

tools/hash-strings.ps1 104行目 `$lineIdPattern = '^line:[A-Za-z0-9_.\-]{1,59}$'`、112行目で `$rawKey -cmatch $lineIdPattern`（-cmatch = case-sensitive）。tools/check-translations.py 26行目にも同一の LINE_ID 正規表現。実データ上は全て `line:` + 8桁小文字16進だが、規則としては上記の広い文字集合を許す。

**Goでの注意**: `regexp.MustCompile("^line:[A-Za-z0-9_.\\-]{1,59}$")`。大小文字を区別するので `(?i)` を付けないこと。`Line:abcd` は line行として扱わない。

#### R7. line: 行は「同じ英文を複数キャラが話す」ケースで、その1行だけを個別に訳すための上書き行。script_order.csv の line_id と1対1で対応する。

hash-strings.ps1 .DESCRIPTION 12-15行目「A row keyed by a Yarn line ID (line:6046bedf) translates that one line only, for English said by more than one character.」。実例: script_order.csv の `L01 Ryan,intro,Ryan_1_intro,9,line:6046bedf,84f325bca745e504,Ryan,` に対し、ja/strings.csv 13-14行目は
  `84f325bca745e504,L01 Ryan,Ryan_1_intro,9,Ryan/Alexander,素晴らしい！`
  `line:6046bedf,L01 Ryan,Ryan_1_intro,9,Ryan,よかった！`
の2行。ハッシュ行が先、line行が後。件数は ja=41件、he=1件（line:95aa0bf4）、tok=0件。

**Goでの注意**: line_id は script_order.csv 内で一意（1839件すべて重複なし）。map[lineID]row で引ける。

#### R8. script_order.csv の line_id は全件一意（1839件、重複0）。key は重複あり（distinct 1602件、うち119キーが2回以上出現）。

最多は `ab5df625bc76dbd4` が29回、次に `1aa7b1c1d5fb1fd6` が11回、`f51bead488e14b65` が10回。この重複こそが「同じ英文を複数キャラが話す」構造そのもの。

**Goでの注意**: key を主キーにする map は「最初の出現」を保持する実装にする（R12参照）。

#### R9. script_order.csv の order は (section, node) ごとに必ず 1..N の連番で、欠番も重複もない。

`awk -F, '{k=$1"|"$3; cnt[k]++; if($4>max[k])max[k]=$4} END{for(k in cnt) if(cnt[k]!=max[k]) print k}'` の出力が空。(section,node,order) の三つ組の重複も0件。order の範囲は 1..111。node は184種類あり、いずれも複数のsectionにまたがらない。

**Goでの注意**: order は文字列で保持されている（出力時にそのまま連結される、165行目 `+ $e.order +`）。数値に変換して再フォーマットすると先頭ゼロ等で差異が出うるので、出力には元の文字列をそのまま使うこと。

#### R10. section 列に現れる値は18種類: L01 Ryan 〜 L15 Alexander の15個 + Cutscene + Reaction + Unused。UI は script_order.csv には存在しない。

行数内訳: Cutscene 86, L01 Ryan 57, L02 Conrad 40, L03 Alexander 35, L04 Ryan 81, L05 Conrad 59, L06 Alexander 53, L07 Ryan 50, L08 Conrad 126, L09 Alexander 148, L10 Ryan 205, L11 Conrad 162, L12 Alexander 76, L13 Ryan 181, L14 Conrad 127, L15 Alexander 92, Reaction 24, Unused 237。

**Goでの注意**: section 名にはスペースが含まれる（"L01 Ryan"）。分割や正規化をしないこと。

#### R11. phase 列は12種類（空文字を含む）。Cutscene / Reaction / Unused の行では phase は必ず空。

内訳: 空 347, cum 63, idle 6, intro 385, jerkoff 15, mount_finish 35, mount_start 21, nag 2, outro 293, phone 21, picnic 189, progress 462。空347 = Cutscene 86 + Reaction 24 + Unused 237。speaker は8種類: Alexander 330, Conrad 389, Dragon 16, ErrorHasOccurred 3, Kobold 578, Phone 14, Ryan 488, Start 21。condition は空1316件のほか11種類のフラグ式（`$ryan_romanced`、`$ryan_romanced $conrad_romanced $ryan_conrad_romanced` のように半角スペース区切りの複数トークン）。

**Goでの注意**: speaker には `ErrorHasOccurred`・`Start` というキャラでない値も混じる。ホワイトリスト検証を入れないこと。

#### R12. ハッシュ行の speaker 列は、script_order.csv でそのキーを話す全キャラを「初出順」で `/` 連結した文字列。

hash-strings.ps1 97-103行目で `$speakers[$k]` を List に初出順で追加（`if (-not $speakers[$k].Contains(...)) { .Add(...) }`）、164行目で `$speakers[$k] -join '/'`。97-103行目は `if (-not $e.speaker) { continue }` で空speakerの行を除外する点に注意。実データで1680件すべて照合し不一致0件。複合値は17種類（例: `Ryan/Alexander`、`Phone/Ryan/Alexander/Conrad/Kobold`、`Ryan/Conrad/Kobold/Dragon`）。単独値は Alexander/Conrad/Dragon/ErrorHasOccurred/Kobold/Phone/Ryan/UI の8種類。

**Goでの注意**: Goのmapは反復順が不定なので、初出順を保つには `[]string` + 存在確認用 `map[string]bool` を組で持つこと。

#### R13. ハッシュ行の section/node/order は、そのキーの script_order.csv における「最初の出現」の値。

hash-strings.ps1 148-167行目、`$done` HashSet で二重出力を防ぎ（151行目 `-and -not $done.Contains($k)`、166行目 `$done.Add($k)`）、165行目で `$e.section`/`$e.node`/`$e.order` を出力。実データ照合で不一致0件。

**Goでの注意**: 初回出現のみ採用。2回目以降の行はハッシュ行を生まないが、line行の出力契機にはなる（R15）。

#### R14. line: 行の speaker 列は、その script_order 行の speaker をそのまま使う（`/` 連結はしない）。

hash-strings.ps1 169行目 `(Escape-Csv $e.speaker)`。164行目のハッシュ行とは違い `$speakers` を参照しない。実例: `line:6046bedf,...,9,Ryan,` は script_order の speaker=Ryan をそのまま使い、同じ位置のハッシュ行は `Ryan/Alexander`。

**Goでの注意**: ハッシュ行とline行で speaker の決定ロジックが違う。共通化しないこと。

#### R15. 出力行の生成条件: script_order.csv を先頭から1行ずつ走査し、hashRow または lineRow のどちらかが真のときだけ何かを出力する。両方偽の行は何も出力せず（見出しも出ない）。

hash-strings.ps1 151-153行目:
  `$hashRow = $rows.ContainsKey($k) -and -not $done.Contains($k)`
  `$lineRow = $lid -ne '' -and $lineRows.Contains($lid)`
  `if (-not $hashRow -and -not $lineRow) { continue }`
同一 script_order 行で両方真なら、ハッシュ行を先に、line行を後に出力する（163-171行目の順）。

**Goでの注意**: 「見出しを出すかどうか」の判定は、この continue の後に来る。つまり中身が1行も残らないノードには見出しが出ない（実例: Unused の node `Start` は24行すべて翻訳なしのため `# --- Start ---` が存在しない）。

#### R16. セクション見出しは `$e.section -ne $lastSection` のときだけ出力し、その直前に必ず空行を1つ入れる。セクションが変わったら $lastNode を null にリセットする。

hash-strings.ps1 154-157行目:
  `if ($e.section -ne $lastSection) { AppendLine(''); AppendLine('# ===== ' + (Section-Title $e.section) + ' ====='); $lastSection = $e.section; $lastNode = $null }`
実ファイルの空行数はja/he/tokとも19個 = レベル15 + Cutscene + Reaction + Unused + UI。

**Goでの注意**: $lastNode のリセットを忘れると、セクション境界で同名ノードの見出しが省略される。

#### R17. ノード見出しは `$e.node -ne $lastNode` のときだけ出力する。判定に phase と condition は使わない。書式は `# --- ` + (phaseが非空なら `phase: `) + node + (conditionが非空なら ` | if condition`) + ` ---`。

hash-strings.ps1 158-162行目:
  `$title = $(if ($e.phase) { $e.phase + ': ' } else { '' }) + $e.node + $(if ($e.condition) { ' | if ' + $e.condition } else { '' })`
実例:
  `# --- intro: Ryan_1_intro ---`
  `# --- cum: Conrad_Outro_3 | if $conrad_jerked_off_3 $conrad_used_mount_3 ---`
  `# --- Alexander_SexScene_Cum | if $alexander_sexscene_loops ---`（Cutsceneはphase空なので `phase: ` が付かない）
ノード名が一度離れて戻ると見出しが再出力される。実例（ja）: 1141行目 `# --- intro: Ryan_5_intro_ryan_romanced | if ... ---` の次に 1148行目 `# --- intro: Ryan_5_intro ---`、および 440行目と486行目に同じ `# --- cum: Conrad_finished_jerkoff_3 ---` が2回。

**Goでの注意**: 「node名だけ」で比較する点が肝。phase/condition を比較キーに含めると実ファイルと差が出る。

#### R18. セクション見出しの表示名: Cutscene → `Cutscenes (started by game code)`、Reaction → `Dragon reactions (started by game code)`、Unused → `Unused nodes (not reachable in the current game)`、それ以外は level_flow.csv から作った見出し、辞書になければ section 名そのまま。

hash-strings.ps1 Section-Title（74-81行目）の switch。default 枝は `if ($levels.ContainsKey($s)) { $levels[$s] } else { $s }`。

**Goでの注意**: 辞書ミス時に section 名をそのまま出すフォールバックを必ず実装する（level_flow.csv が無い環境を想定した設計、62-63行目の Test-Path 参照）。

#### R19. レベル見出しの生成: キーは `L{level+1:00} {dragon}`、本文は `Level {level+1}: {dragon}` + (weatherが非空なら ` ({weather})`) + (set_flagsが非空なら ` | sets {set_flags}`) + (end_flagsが非空なら ` | ends {end_flags}`)。flags 内の ` | ` は `, ` に置換する。

hash-strings.ps1 65-71行目:
  `$sec = ('L{0:00} {1}' -f ($idx + 1), $l.dragon)`
  `$h = 'Level {0}: {1}' -f ($idx + 1), $l.dragon`
  `if ($l.weather) { $h += " ($($l.weather))" }`
  `if ($l.set_flags) { $h += ' | sets ' + ($l.set_flags -replace ' \| ', ', ') }`
  `if ($l.end_flags) { $h += ' | ends ' + ($l.end_flags -replace ' \| ', ', ') }`
実例の検算:
  level=0 set_flags=level_1 end_flags=level_1_complete → `# ===== Level 1: Ryan (Sunny) | sets level_1 | ends level_1_complete =====`
  level=4 set_flags=level_5 end_flags=`MedkitCompleted | level_5_complete` → `# ===== Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete =====`
  level=5 set_flags=level_6_started end_flags=空 → `# ===== Level 6: Alexander (Night) | sets level_6_started =====`
  level=7 spawn_flag=PlacedMount set_flags=DeliveredMountFrame end_flags=空 → `# ===== Level 8: Conrad (Sunny) | sets DeliveredMountFrame =====`（spawn_flag は見出しに出ない）
  level=9 set_flags=空 end_flags=PicnicCompleted → `# ===== Level 10: Ryan (Sunny) | ends PicnicCompleted =====`

**Goでの注意**: `fmt.Sprintf("L%02d %s", idx+1, dragon)` と `fmt.Sprintf("Level %d: %s", idx+1, dragon)`。置換は `strings.ReplaceAll(s, " | ", ", ")`（PowerShellの `-replace` は正規表現だがパターンがエスケープ済みパイプなので実質リテラル）。spawn_flag・intro・progress_dialogs 等の他の列は見出しに一切使わない。

#### R20. 入力行の処理順序（1行ごとに上から評価し、continue で打ち切る）。

hash-strings.ps1 110-134行目、そのままの順序:
 1. `$rawKey = key列.Trim()`（key列が無ければ空）
 2. rawKey が line ID パターンに -cmatch → translation が非空 かつ $lineRows に未登録なら `$lineRows[$rawKey] = $tr`。いずれにせよ continue（先勝ち。translation空のline行は黙って捨てる）
 3. `$key = $rawKey.ToLowerInvariant()`
 4. `$src = source_en列`（無ければ空）
 5. src が非空: `$hashed = Get-Key $src`。`if ($key -ne '' -and $key -ne $hashed) { $dropped++; continue }`（key列とハッシュが食い違う行は捨てる）。一致 or key空なら `$key = $hashed`
 6. src が空で `$key -match '^[0-9a-f]{16}$'` → そのまま採用
 7. どちらでもない → dropped、continue
 8. `if ($rows.ContainsKey($key)) { continue }`（ハッシュキーも先勝ち）
 9. `$who = speaker列`、`$tr = translation列`
10. `if ($tr -eq '') { continue }`（未訳行は publish しない。dropped にはカウントしない）
11. `$rows[$key] = @{ Speaker = $who; Translation = $tr }`、`$inputOrder.Add($key)`

**Goでの注意**: ステップ2とステップ8の「先勝ち」を取り違えないこと。またステップ10の空判定は `-eq ''` であって Trim ではない（空白のみの翻訳は通る。ただし tools/check-translations.py 84行目は `translation.strip()` で弾くので、検証器のほうが厳しい）。

#### R21. published ファイルの先頭コメントブロックは、既存の出力ファイルから引き継ぐ。ヘッダ行を除いた2行目以降を順に見て、`#` で始まりかつ `# =====` でも `# ---` でもない行だけを写し、それ以外に当たった時点で打ち切る。

hash-strings.ps1 140-145行目:
  `foreach ($line in (ReadAllLines($t.Output) | Select-Object -Skip 1)) { if (-not $line.StartsWith('#') -or $line.StartsWith('# =====') -or $line.StartsWith('# ---')) { break }; AppendLine($line) }`
実ファイルの差: he と tok は4行のブロックを持つ。
  `# Language: עברית (he)` / `# Language: toki pona (tok)`
  `# Created by TomXV. Provisional translation, not reviewed by a native speaker:`
  `# some lines may read unnaturally. Corrections are welcome as pull requests.`
  `# Only the Japanese (ja) and Simplified Chinese (zh-Hans) files were supervised by the author.`
ja はヘッダ直後がいきなり空行で、このブロックを持たない（`grep -n '^# Language' Translations/ja/strings.csv` は0件）。13ロケールを確認したところ、ブロックを持たないのは ja と zh-Hans のちょうど2つで、これは4行目が名指ししている「作者が監修した言語」と一致する。2行目以降の3行は全ロケールで完全同一文字列。

**Goでの注意**: このブロックは生成対象ではなく「手で書いて、以後は引き継がれるもの」。Go実装でも出力先の既存ファイルを読んで写す必要がある。出力ファイルが存在しなければブロックなし。ヘッダ直後の空行はこのブロックではなく、R16のセクション見出し直前の空行。

#### R22. CSVのクォート規則: フィールドが `,` `"` CR LF のいずれかを含むときだけ全体を `"` で囲み、内部の `"` を `""` に倍化する。それ以外は素のまま。

hash-strings.ps1 Escape-Csv（45-49行目）: `if ($v -match '[,"\r\n]') { return '"' + ($v -replace '"', '""') + '"' }`。適用先は section / node / speaker / translation の4列のみ（165・169行目）。key と order は無加工で連結される。実ファイル: クォートされた物理行数は ja=8, he=407, tok=231。`""`（倍化）を含む行は ja=8, he=10, tok=10。ja の8件はすべて `<gradient="gold">` 等のタグ由来。翻訳文に半角カンマを含む行は ja=0, he=399, tok=223（日本語は全角句読点を使うため）。実例（ja 69行目）:
  `d391aece578e396f,L02 Conrad,Conrad_Intro_1,1,Conrad,"<gradient=""yellow - orange""><size=140%>よお！ </gradient><size=100%>ここが洗い屋だろ、な？"`
実例（he 1653行目、本文中の引用符）:
  `5a208b2886d87937,Unused,Conrad_1,1,Conrad,"אתם עושים פה גם ""סיומים מיוחדים""?"`

**Goでの注意**: Goの `encoding/csv` の Writer を使わず自前で書くこと。Goの fieldNeedsQuotes は「先頭ルーンが空白」と「フィールドが `\.` と等しい」場合にも引用符を付けるため、元実装と出力が一致しなくなる。現行データには先頭空白のフィールドは0件（ja/he/tokとも leading/trailing 空白の translation は0件）なので今は差が出ないが、将来の入力で壊れる。

#### R23. script_order.csv に現れないキー（UI等）は、最後に `# ===== UI and other text (not part of the dialogue script) =====` を付けてまとめて出力する。行の形は `key,UI,,,speaker,translation`（node と order は空）。speaker は入力行の speaker、空なら `UI`。順序は入力ファイル中の初出順。

hash-strings.ps1 173-181行目:
  `$left = @($inputOrder | Where-Object { -not $done.Contains($_) })`
  `if ($order.Count -gt 0) { AppendLine(''); AppendLine('# ===== UI and other text ... =====') }`
  `$who = if ($rows[$k].Speaker) { $rows[$k].Speaker } else { 'UI' }`
  `$sec = if ($order.Count -gt 0) { 'UI' } else { '' }`
  `AppendLine($k + ',' + $sec + ',,,' + (Escape-Csv $who) + ',' + (Escape-Csv $rows[$k].Translation))`
実データ: ja/he/tok とも UI セクションは110行。うち93行は speaker=`UI`、17行は Ryan / Conrad（ピクニックとロマンスの断片、例 `b735b39b4eea8b70,UI,,,Ryan,外でするのは初めてなんだ`）。UIセクション内には `# --- ... ---` 見出しが一切ない。

**Goでの注意**: `$sec` と見出しの両方が `$order.Count -gt 0` で条件分岐している。script_order.csv が空なら見出しも UI ラベルも出ない（section列が空文字になる）。

#### R24. script_order.csv に対応する line_id がなかった line: 行は、末尾に `# ===== Per-line translations not found in the script order =====` を付けて `line:xxx,,,,,translation` の形で出す。

hash-strings.ps1 182-187行目: `AppendLine($lid + ',,,,,' + (Escape-Csv $lineRows[$lid]))`。section/node/order/speaker がすべて空。実ファイルでは13ロケールいずれにもこの見出しは出現しない（`grep -rl 'Per-line translations not found' Translations/` が0件）ため、現データでは常に空のパス。

**Goでの注意**: 実データに例がない経路なので、テストを自作して担保すること。

#### R25. published ファイルの検証条件（tools/check-translations.py）。

1) ヘッダは `key,section,node,order,speaker,translation`。旧形式 `key,speaker,translation` と `key,translation` も受理（53-57行目）。2) `#` で始まる行と空白のみの行は読み飛ばす（46行目 `if line.strip() and not line.startswith("#")`）。3) key は `^[0-9a-f]{16}$` または `^line:[A-Za-z0-9_.\-]{1,59}$`（25-26, 77行目）。4) key の重複禁止（81-83行目）。5) `translation.strip()` が空なら NG（84行目）。6) section と node は `^(?:[A-Za-z0-9_]*|L\d\d [A-Za-z]+|UI)$` に一致すること（27, 87-89行目）。7) source_en 列があってはならない（英文の混入防止、docstring 15行目）。8) `Translations/_discovered/` 以下と `strings.local.csv` は git 管理下にあってはならない（109-119行目）。

**Goでの注意**: IDENT の第1枝 `[A-Za-z0-9_]*` は空文字にも一致するので、UIセクションの空 node が通る。第2枝は `L\d\d ` 固定幅なのでレベルが100以上になると破綻する。

#### R26. published ファイルの行数内訳（実測）。

ja: 全1943行 = ヘッダ1 + 空行19 + コメント202 + データ1721（うちハッシュ行1680、line:行41）。he: 全1907行 = 1 + 19 + 206 + 1681（ハッシュ1680、line:1）。tok: 全1906行 = 1 + 19 + 206 + 1680（ハッシュ1680、line:0）。3言語ともハッシュキーは同一の1680件で、内訳は非UI 1570 + UI 110。コメント202 vs 206 の差4はR21の言語ブロック。

**Goでの注意**: 「ハッシュ行は3言語で完全に同一集合」という不変条件はテストに使える。

#### R27. script_order.csv の distinct key 1602件のうち32件は、どの言語の strings.csv にも存在しない。

該当は L08 Conrad/Conrad_Outro の order 1-3、L13 Ryan/Ryan_5_Required_ryan_romanced の order 36、Unused の Alexander_Outro 11-13 / ConradBeatup_Outro 1-3 / RyanDate_Outro 1-3 / RyanExploded_Outro 1-3、そして Unused/Start の全24行。非UIハッシュ行1570 + 32 = 1602 で整合。結果として node `Start` は1行も残らず、見出し `# --- Start ---` 自体が出力されない（R15）。一方 Conrad_Outro は order 4,5 が残るので見出しは出て、order 1-3 が欠番になる（ja 531-534行目）。同様に Conrad_placed_mount_3 は order 8,9,11,12 で 10 が欠番。

**Goでの注意**: 「order に欠番があること」は正常。連番を前提にした検証や再採番を入れないこと。

### 境界条件

- data/level_flow.csv だけ先頭にUTF-8 BOM (EF BB BF) がある。Goの encoding/csv はBOMを除去しないため、除去しないと最初の列名が "﻿flow_asset" になり、level・dragon 等の列引きは通るのに flow_asset だけ取れないという分かりにくい形で壊れる。
- 改行は全ファイルLFのみ、CRは0バイト。だが生成元 hash-strings.ps1 は StringBuilder.AppendLine（Environment.NewLine）を使っており、Windows で素直に動かすとCRLFが出る。実体がLFなので、Go実装は必ず "\n" を出力すること。
- 全ファイル末尾に改行が1個あり、末尾に余分な空行はない（LFLFで終わらない）。
- 翻訳フィールドに埋め込み改行を含む行は ja/he/tok に0件。ただし Read-Csv は `$_.StartsWith('#')` で物理行単位にコメントを落としてから CSV パースするため（hash-strings.ps1 53行目）、もし引用符内の2行目が `#` で始まる翻訳が入ると、その行だけ削られてCSVが壊れる。同じ弱点が check-translations.py 46行目にもある。Go で先に完全なCSVパースをすると元実装と挙動が変わる点に注意。
- 空行の扱いが2つの実装で違う。Read-Csv は空行を落とさず ConvertFrom-Csv に渡す（PowerShell 側が無視する）。check-translations.py は `line.strip()` で明示的に落とす。Go では「空行はスキップ」で両者と整合する。
- コメント行の判定は必ず「物理行の先頭が `#`」。フィールドの中身を見た判定ではない。
- 翻訳が空文字の行は黙って捨てられる（dropped にもカウントされない、hash-strings.ps1 131行目）。一方 line: 行も翻訳が空なら捨てられる（114行目）。
- key 列と source_en 列の両方があり、key がハッシュと食い違う行は捨てられる（121行目）。key が空で source_en だけある行は正常にハッシュ化される。
- キーの重複は「先勝ち」。ハッシュキー（128行目）も line ID（114行目）も、2件目以降は無視される。
- script_order.csv の order は (section,node) 内で連番だが、出力ファイルでは翻訳のないキーが飛ばされるため欠番が出る。実例: ja 531行目以降の Conrad_Outro は order 4 から始まる（1-3が欠番）、L08 Conrad/Conrad_placed_mount_3 は 8,9,11,12（10が欠番）。
- 1ノードの行が全て欠落するとノード見出し自体が出ない。実例: Unused/Start（24行すべて翻訳なし）は `# --- Start ---` が存在しない。
- 同じノード名が離れて再登場すると見出しが再出力される。判定は node 名の一致のみで、phase や condition は見ない。実例（ja）: 440行目と486行目に同じ `# --- cum: Conrad_finished_jerkoff_3 ---`。
- セクションが変わると $lastNode が null にリセットされるため、セクション境界をまたいだ同名ノードは見出しが再出力される。
- speaker 列は `/` 連結で複合値になる（17種類、最長 `Phone/Ryan/Alexander/Conrad/Kobold` の5人）。speaker にキャラでない値も入る: `UI`, `Start`, `ErrorHasOccurred`, `Dragon`, `Phone`。
- 翻訳文には TextMeshPro のリッチテキストタグが入る。3言語とも出現数が完全一致する: `<i>`45 `</i>`29（閉じタグが足りない行がある）、`<b>`8 `</b>`7、`<gradient="yellow - orange">`1 `<gradient="gold">`7 `</gradient>`8、`<size=NN%>` が10種類計60個。`{0}` 形式のプレースホルダは0件、バックスラッシュエスケープも0件。
- `<gradient="gold">` のようにタグ属性が二重引用符を含むため、そのセルはCSVクォート対象になり `""` 倍化が必要。ja で引用符が出る8行はすべてこれが原因。
- he（ヘブライ語、RTL）ファイルに U+200E / U+200F などの双方向制御文字は1文字も入っていない（Cf/Cc カテゴリの文字が0件）。RTL文中にもASCIIのタグ・句読点がそのまま混在する。
- 翻訳フィールドに先頭・末尾の空白を持つ行は3言語とも0件。ただし `<b> テキスト </b>` のようにタグの内側には前後空白がある。Goの csv.Writer は先頭が空白のフィールドを引用符で囲むため、将来そうした行が入ると元実装と出力が食い違う。
- section 名にスペースが含まれる（"L01 Ryan"）。識別子検証 `^(?:[A-Za-z0-9_]*|L\d\d [A-Za-z]+|UI)$` はこの形だけを特例で許している。レベルが2桁を超えると破綻する。
- UIセクションの行は node と order が空（`key,UI,,,speaker,translation`）。空フィールドが3つ連続する。
- ja と zh-Hans はヘッダ直下のコメントブロックを持たない。残り11言語は4行の定型ブロックを持つ。生成器はこれを既存出力ファイルから読み写すだけなので、出力ファイルが無い状態から生成するとブロックは失われる。
- `# ===== Per-line translations not found in the script order =====` ブロックは13ロケールいずれにも出現しない。実データによる裏取りができない経路。

### 敵対検証で見つかった食い違い

#### [high] 入出力ターゲットの決定ロジック（hash-strings.ps1 84-94行目）が仕様に一切書かれていない。特に「Translations/_discovered/<locale>.working.csv があればそれを入力にし、出力は Translations/<locale>/strings.csv」という入出力分離が抜けている。

- 根拠: 84-94行目: `if ($Path) { foreach ($p in $Path) { $targets += @{ Input = $p; Output = $p } } } else { Get-ChildItem -Path (Join-Path $root 'Translations') -Directory | Where-Object { $_.Name -notlike '_*' } | ForEach-Object { $out = Join-Path $_.FullName 'strings.csv'; $work = Join-Path $root ('Translations/_discovered/' + $_.Name + '.working.csv'); $in = if (Test-Path $work) { $work } else { $out }; if (Test-Path $in) { $targets += @{ Input = $in; Output = $out } } } }`。README.md 206行目より working.csv の列は `key,section,node,order,speaker,source_en,translation`。仕様の dataModel は _discovered を言及するだけで規則化していない。
- 直し方: R0 として追加する。(a) -Path 指定時は Input=Output=その1ファイル、(b) 無指定時は Translations 直下のディレクトリを列挙し `_` 始まりを除外、(c) 各ロケールで working.csv があればそれを入力、なければ published を入力、(d) どちらも無いロケールはスキップ。これが無いとGo版は working.csv を一切読まず、翻訳者の作業結果が反映されない（= このツールの主目的が機能しない）。

#### [medium] R12 がハッシュ行 speaker のフォールバック2段を落としている。実際は `$speakers` にキーが無ければ入力行の speaker、それも空なら `$e.speaker` を使う。

- 根拠: 164行目: `$who = if ($speakers.ContainsKey($k)) { $speakers[$k] -join '/' } elseif ($rows[$k].Speaker) { $rows[$k].Speaker } else { $e.speaker }`。$speakers は100行目 `if (-not $e.speaker) { continue }` により「script_order 側の speaker が全出現で空のキー」を持たない。R12 は第1枝しか記述していない。
- 直し方: R12 を3段の条件式として書き直す。現データでは script_order.csv の speaker 空行が0件（awk で $7=="" が0）なので露見しないが、仕様どおり実装すると将来 speaker 空の行が入った瞬間に Go 版は空 speaker を出力し、元実装は入力側 speaker を出力する。

#### [medium] R20 goNote の「ステップ10の空判定は -eq '' であって Trim ではない（空白のみの翻訳は通る）」が誤り。引用符なしの空白のみフィールドは ConvertFrom-Csv 側で空文字になるため、元実装では捨てられる。さらに引用符なしフィールドの前後空白の扱い自体が Go の encoding/csv と違う。

- 根拠: pwsh 7.6.6 実測: `1111111111111111,A,   `（引用符なし空白3個）→ translation は len=0 の空文字になり 131行目 `if ($tr -eq '')` で continue。`2222222222222222,B,"   "`（引用符付き）だけが len=3 で残る。また `k1,  lead` → `lead`（先頭空白は全部除去）、`x   ` → `x `（末尾空白は1個残して除去）、`"  qlead"` → `  qlead`（引用符付きは完全保持）。Go の encoding/csv は TrimLeadingSpace 既定 false で前後空白をすべて保持する。
- 直し方: R20 goNote を訂正し、入力パース規則として明記する。「引用符なしフィールドは先頭空白を全除去、末尾空白を1個残して除去。引用符付きは無加工」。あわせて Escape-Csv（47行目）は前後空白では引用符を付けないので、published を再入力にすると前後空白が毎回削れて非冪等になることも注記する。Go で encoding/csv をそのまま使うと、空白のみの翻訳行が余分に1行出力される。

#### [medium] $lineRows（`[ordered]@{}`）のキー照合が OrdinalIgnoreCase なのに、R6 goNote は「大小文字を区別するので (?i) を付けないこと」とだけ書き、辞書側が大小無視である点を落としている。正規表現は case-sensitive、辞書は case-insensitive という非対称。

- 根拠: 108行目 `$lineRows = [ordered]@{}`。リフレクションで comparer を確認したところ `System.OrdinalIgnoreCaseComparer`。実測で `$o['line:ABCD1234']='T'` のあと `$o['line:abcd1234']` が 'T' を返し、`$o.Contains(...)`・`$o.Remove(...)` も大小無視で成立。一方 112行目は `-cmatch`（case-sensitive）で、パターン `^line:[A-Za-z0-9_.\-]{1,59}$` は大文字英数字も許容する。
- 直し方: 「line ID の書式判定は case-sensitive だが、$lineRows への登録・照合・削除は OrdinalIgnoreCase」と分けて書く。Go では map[string]string を素で使わず、キーを strings.ToLower して引く別マップを添えるか、正規化キー方式にする。入力に `line:6046BEDF`、script_order に `line:6046bedf` がある場合、元実装は台本内の位置に出力するが、仕様どおりの Go 版は末尾の「Per-line translations not found」ブロックに落ちる。

#### [medium] R23 の detail が 174行目の外側ガード `if ($left.Count -gt 0)` を引用から落としている。引用コードのままだと、残キーが0件でも空行とUI見出しが出る。

- 根拠: 173-181行目: `$left = @($inputOrder | Where-Object { -not $done.Contains($_) })` の次が `if ($left.Count -gt 0) {` で、その内側に `if ($order.Count -gt 0) { AppendLine(''); AppendLine('# ===== UI and other text ... =====') }` がある。R23 の detail は `$left = ...` の次行にいきなり `if ($order.Count -gt 0)` を並べている。
- 直し方: R23 に「$left が空なら見出しも行も一切出さない（174行目）」を明記する。現データは UI キーが3言語とも110件あるので差は出ないが、全キーが script_order に載るロケールでは末尾に空の見出しブロックが残る。

#### [medium] PowerShell の `-eq` / `-ne` による文字列比較が InvariantCultureIgnoreCase であることが仕様に無い。R16・R17 のセクション／ノード見出し判定、および R20 の各種空判定がすべて影響を受ける。

- 根拠: 実測: `'Foo' -eq 'foo'` → True。`("e"+[char]0x0301) -eq [char]0x00E9` → True（Ordinal では False、InvariantCultureIgnoreCase では True）。よって 154行目 `$e.section -ne $lastSection`、158行目 `$e.node -ne $lastNode`、121行目 `$key -ne $hashed`、131行目 `$tr -eq ''`、114行目 `$tr -ne ''`、152行目 `$lid -ne ''` はすべて大小無視かつ文化依存比較。さらに `[char]0x00AD -eq ''` → True、`[char]0x200D -eq ''` → True（無視可能文字だけの文字列は空文字と等しい）。一方 `if ($e.phase)` 等の真偽判定は `IsNullOrEmpty` 相当の長さ判定なので、同じ文字列が「空と等しい」のに「truthy」になる非対称がある。
- 直し方: R16/R17 に「node・section の一致判定は case-insensitive」、R20 に「翻訳・line_id の空判定は Unicode の無視可能文字のみの文字列も空とみなす」を追記。Go の `!=` / `== ""` は純バイト比較なので、大小のみ違うノード名が隣接すると Go だけ見出しを二重に出す。現データは node 184種で大小衝突0件、翻訳に無視可能文字のみの行も0件なので潜在。

#### [low] R24 が 170行目の `$lineRows.Remove($lid)` を書いていない。台本中に出力済みの line 行を保留マップから取り除く処理が仕様から消えている。

- 根拠: 168-171行目: `if ($lineRow) { AppendLine(...); $lineRows.Remove($lid); $lineKept++ }`。182行目の `if ($lineRows.Count -gt 0)` はこの削除後の残りを見ている。R24 の detail は 185行目の AppendLine しか引用していない。
- 直し方: R24 に「台本内に出力した line 行は $lineRows から削除し、末尾ブロックには残りだけを挿入順で出す」を明記する。削除を実装しないと、ja では41件の line 行がすべて末尾ブロックに二重出力され、check-translations.py の duplicate key 検査で落ちる。

#### [low] CSVヘッダ名（列の有無・取得）の照合が PSObject 経由で case-insensitive である点が仕様に無い。InputRow の goNote は「csv.Reader + ヘッダ名→添字」と書くだけで、完全一致を前提にしている。

- 根拠: 111・113・118・129・130行目はいずれも `$r.PSObject.Properties['key']` の形。実測で `[pscustomobject]@{ Key='k1'; TRANSLATION='t1' }` に対し `PSObject.Properties['key']` も `['translation']` も非 null。
- 直し方: 入力ヘッダ名の照合を OrdinalIgnoreCase にする旨を InputRow に追記する。README 206・242行目のとおり現行の working.csv / UI export はすべて小文字ヘッダなので現状影響なしだが、`Key,Translation` のようなヘッダのファイルを食わせると Go 版だけ全行 dropped になって空ファイルを生成する。

#### [low] Section-Title の `switch` と `$levels` ハッシュテーブルの照合が case-insensitive である点が R18 に無い。

- 根拠: 74-81行目の `switch ($s)` は PowerShell 既定で大小無視（実測: `switch ('cutscene') { 'Cutscene' { ... } }` が一致）。`$levels = @{}`（59行目）の comparer はリフレクションで `System.OrdinalIgnoreCaseComparer`。
- 直し方: R18 に「section 名の照合は大小無視。`cutscene` も `l01 ryan` も一致する」を追記。Go の switch と map は大小区別なので、section 表記ゆれがあると Go 版だけ section 名を素通し出力する。

#### [low] コメント行判定 `$_.StartsWith('#')` が .NET 既定の文化依存比較である点が仕様に無い（edgeCases は「物理行の先頭が # 」とだけ書く）。

- 根拠: 53行目 `Where-Object { -not $_.StartsWith('#') }`、142行目 `if (-not $line.StartsWith('#') -or $line.StartsWith('# =====') -or $line.StartsWith('# ---'))`。実測: `("`u{00AD}#hello").StartsWith('#')` → True（ソフトハイフンは照合上無視される）。
- 直し方: 「先頭が無視可能文字（U+00AD、U+200D 等）＋ # の行も元実装ではコメント扱いになる」と注記するか、Go 側は `strings.HasPrefix` の序数判定でよいと明示的に割り切る旨を書く。he ファイルには現在 Cf/Cc が0件なので潜在。

### 抽出が取りこぼしていた規則

- hash-strings.ps1 84-94行目のターゲット決定（-Path 指定時は Input=Output、無指定時は Translations 直下ディレクトリのうち `_` 始まりを除外し、_discovered/<locale>.working.csv があればそれを入力、無ければ published を入力、どちらも無ければスキップ）。上の divergences 1件目と同内容だが、規則そのものが仕様に存在しない。
- Read-Csv 54行目の `if ($lines.Count -lt 2) { return @() }`。コメント行除去後の行数が2未満のファイルは0行として扱う。script_order.csv がヘッダのみだった場合に `$order.Count -eq 0` 経路（UI見出しなし・section列空）に入る条件でもある。
- script_order.csv 側の key も 99行目・149行目で `.Trim().ToLowerInvariant()` してから照合する点。R20 は入力ファイル側の Trim しか書いていない。一方 150行目の `$lid = [string]$e.line_id` は Trim されない（非対称）。
- ConvertFrom-Csv の列数不一致時の挙動。実測でヘッダより多い列は黙って捨てられ、少ない場合は $null（`[string]$null` = 空文字）になる。R25 は check-translations.py 側の `len(row) != width` 検査（73-74行目）も落としている。published 行は6列ちょうどでないと NG。
- カウンタと標準出力のサマリ（109行目の $converted/$kept/$dropped/$lineKept、189行目の Write-Host 書式 `"{0} <- {1}: {2} converted, {3} already hashed, {4} per-line, {5} malformed dropped, {6} in play order, {7} other"`）。$converted は key とハッシュが一致していた行でも加算される（122行目）点も含め、ログ互換が要るなら仕様化が必要。
- 入力ファイル形式の実例が仕様に無い。README.md 206行目の working.csv は `key,section,node,order,speaker,source_en,translation`、242行目の UI export は `key,source_en,translation,object_path`。後者には section/node/order 列が無く、InputRow の「列が無い＝空文字」規則が実際に効く唯一の経路。
- `if ($l.weather)` 等の PowerShell 真偽判定が IsNullOrEmpty 相当の長さ判定であるのに対し、`-eq ''` は文化依存比較であるという非対称（divergences 6件目で触れたが、R19/R17 の条件分岐の規則としては未記述）。

### 未決の点

- script_order.csv に存在するのに全言語で翻訳行が無い32キーの理由を、ファイルからは確定できなかった。内訳（各 *_Outro の order 1-3、Unused/Start 全24行、Ryan_5_Required_ryan_romanced の order 36）と Translations/ignore.txt の除外パターン（`^title: .+$`、`^Test line \d+$`、`^(player|conrad) hooked up with (conrad|ryan)$`、`^Something went wrong with flags!$`、`^Wow!$`/`^Gosh!$`/`^Incredible!$` 等の Yarn Spinner サンプル Start ノード向けパターン）が符合するため「ignore.txt により翻訳対象外になった行」と推測したが、英文そのものがリポジトリに無いため照合できない。Go実装で再現テストを書くなら、この32キーをハードコードするのではなく「入力に翻訳が無いキーは出力しない」というR15の一般則で自然に落ちることを確認すべき。
- hash-strings.ps1 の AppendLine は Environment.NewLine を使うのに、コミットされているファイルはLFのみ。pwsh を Linux/macOS で走らせたのか、Windows で生成したものが core.autocrlf=input により index でLFへ正規化されたのか、実体からは判別できなかった。Go実装の出力は常にLFとするのが安全だが、既存の生成フローに合わせる必要があるなら要確認。
- PowerShell の ConvertFrom-Csv が空行をどう扱うかを実測していない（空行をスキップすると仮定して再実装し、ja/he/tok の3ファイルをバイト単位で完全再現できたため、実務上はこの仮定で問題ないと確認済み）。ただし「空行が全て末尾コメントブロックの直前・セクション見出しの直前にしか現れない」という現データの偏りに助けられている可能性がある。
- UIセクションに入る110キーが、どの工程で収集されるのか（ゲーム内エクスポートか、_discovered/<locale>.working.csv か）はファイルから確定できない。生成器から見れば「script_order.csv が知らないキーが入力に残っていた」というだけで、収集元は関知していない。
- level_flow.csv の spawn_flag / intro / progress_dialogs / idle_dialogs / nag_dialogs / phone / outro / jerkoff_dialog / cum_dialog / mount_start / mount_finish / player_spawn の12列は strings.csv の生成に一切使われていない。これらが別のツール（ゲーム内エディタ等）で使われるのか、参考情報として同梱されているだけなのかは判断できなかった。移植対象に含めるかは要確認。
- `# ===== Per-line translations not found in the script order =====` ブロックと、`$order.Count -eq 0`（script_order.csv が無い）ときのフォールバック経路は、実データに例が無いため挙動をコードからしか確認できていない。
- ハッシュ対象の「exact source string」が、ゲームから取り出した時点で前後の空白や改行をどう扱っているかは不明。ignore.txt には「実際の文字列は末尾に改行を含むが、照合は前後の空白を除いて行われる」という記述があり、照合とハッシュで正規化のルールが違う可能性がある。ハッシュ化は Get-Key に渡された文字列をそのままUTF-8化するだけなので、呼び出し側の正規化仕様を別途確認する必要がある。

## 実装で判明した訂正

この節だけは抽出結果ではなく、Go実装を書きながら pwsh 7.6.6 / CPython 3.14 で実測して分かった訂正である。上の各節と食い違う場合はこちらが正しい。

| 訂正 | 上の節の記述 | 実測 | 影響 |
| ---- | ---- | ---- | ---- |
| key 空行のスキップは C# 側だけの規則 | 「スクリプト順 R3」 | hash-strings.ps1 148-171行目は `foreach ($e in $order)` を無条件に回す。key 空行のスキップは無い | 捨てると台詞ID訳が末尾の孤児ブロックへ落ち、`$order.Count -gt 0` の判定も変わって UI 見出しと section='UI' が出なくなる |
| `$order.Count` は解析後ではなく Read-Csv が返した生の件数 | 「公開CSV生成 R25(b)」 | 同上 | 上と同じ原因。ParseEntries の絞り込みを PowerShell 経路へ持ち込んではいけない |
| ConvertFrom-Csv のヘッダーは「最初にレコードになる行」ではない | 「公開CSV生成 R4」 | 読み飛ばすのは完全な空行だけ。`'   '` も `,` も `""` もヘッダーになる | 先頭に空白だけの行が1本あると、本物のヘッダーがデータ行へずれて全行が捨てられる |
| 行末フィールドを捨てる条件は「空かつ引用符なし」ではない | 「公開CSV生成 R4」 | 捨てるのは「値が空、かつ引用符が閉じていない」もの。`b,"` は捨て、`b,""` は残す | 集計の malformed dropped がずれる |
| section / node は「列が無い」と「値が空文字」で結果が違う | 「公開CSV生成 R24」 | ヘッダーに無い列のプロパティは `$null`。`$null -ne $null` は偽なので先頭行でも見出しが出ない。値が空文字なら `'' -ne $null` が真で見出しが出る | section / node 列を持たない script_order.csv で、余分な空見出しを出してしまう |
| `[int]$l.level` は空文字で例外を投げない | 「スクリプト順 R8」 | `[int]''` と `[int]$null` はどちらも 0。例外になるのは `'abc'` のような完全な非数値と範囲外だけ。`'3.7'` は 4、`'0x10'` は 16 になる | level が空欄の行を捨てると、レベル1の見出し文言が変わる |
| `.NET String.StartsWith(string)` はカルチャ依存 | 「公開CSV生成 R4 / R23」 | 照合上無視される文字（U+00AD など）が先頭にあっても真を返す | Go の序数比較では再現しない。元実装ならコメントとして落ちる行が残る向きにずれる |
| PowerShell の `-eq ''` もカルチャ依存 | 「公開CSV生成 R20」 | U+00AD や U+200D だけからなる値も「空」と判定される | Go の序数比較では再現しない。そうした訳を捨てずに公開する向きにずれる |
| Python の `$` は末尾の改行1個の手前にも当たる | 「形式検証 R18」 | `KEY.match('185f8db32271fe25' + LF)` は真 | Go 実装は通さない。CRLF と LF で判定が割れるため、どちらでも落ちる側にそろえた。過検出の方向にだけずれる |

「未決の点」のうち、次の2件は決着した。

- `hash-strings.ps1` の改行は Windows では CRLF になる。コミットされているファイルがLFなのは`.gitattributes`による正規化の結果と考えるのが自然で、Go実装の出力は常にLFとする。CRLFを除けば、実測した全ケースでバイト単位に一致する
- ConvertFrom-Csv は空行を読み飛ばす。ただし「完全な空行だけ」で、空白だけの行は読み飛ばさない（上の表を参照）
