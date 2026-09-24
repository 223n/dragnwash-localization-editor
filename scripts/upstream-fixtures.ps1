#Requires -Version 7.0

<#
.SYNOPSIS
上流の tools/hash-strings.ps1 に合成した入力を読ませ、試験の正解（testdata/upstream/expected.json）を作る。

.DESCRIPTION
入力の表は testdata/upstream/cases.json にある。英文と訳はどれも架空の文で、ゲームの台本は含まない。
正解には、1つの入力ごとに次の2つを入れる。

  read  上流の Read-Csv（と、それが呼ぶ Remove-NonRecords）だけを取り出して読ませた結果。
        レコードの値と、そのレコードが入力のどの物理行から来たか（開始行と終了行）。
  run   一時ディレクトリに tools/・data/・Translations/ を置き、上流のスクリプトを通しで
        走らせた結果。書き出した strings.csv のバイト列と、集計の1行の項目ごとの値。

2つの関数は、上流のスクリプトを AST で読み、関数の定義だけを取り出して使う。
スクリプトを書き換えたり、同じ規則を書き直したりはしない。書き直すと、試したいもの
（上流）ではなく書き直したもの（自分）を試すことになるからである。

物理行の範囲は、上流の関数に行の番号を尋ねる手段が無いので、外から求める。
入力の先頭 k 行（k = 0〜N）だけを読ませ、各レコードが初めて現れる k を開始行とする。
終了行は、先頭 k 行の後ろに印の行を1つ足して読ませ、そのレコードが全体を読んだときと
同じになる最小の k とする（引用が開いたままなら、印の行が値に飲み込まれて値が変わる）。
行の分け方は dwloc と同じ '\r\n' / '\n' / '\r' の3種で、BOM は数えない（エディターの
行番号と同じ）。

pwsh 7.4.6（上流の docker の hash 経路）と手元の pwsh の両方で作れるように書いてある。
手順は testdata/upstream/README.md にある。

.PARAMETER Script
上流の tools/hash-strings.ps1 の写し。git show <コミット>:tools/hash-strings.ps1 で取り出したもの。

.PARAMETER Commit
Script を取り出したコミット。正解に記録するだけで、ここでは確かめない。
Script の git の blob の名前も記録するので、あとで照らし合わせられる。

.PARAMETER Out
正解を書き出す先。

.PARAMETER Cases
入力の表。省略するとリポジトリの testdata/upstream/cases.json を使う。

.PARAMETER WorkDir
一時ファイルを置く場所。この下に作業用のフォルダーを1つ作り、終わったら消す。
省略すると OS の一時フォルダーを使う。

.EXAMPLE
pwsh -File scripts/upstream-fixtures.ps1 -Script ../hash-strings.ps1 -Commit dc55c9eb67d3623d23ef4c272ccd1f4152eaab2f -Out testdata/upstream/expected.json
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Script,
    [Parameter(Mandatory)][string]$Commit,
    [Parameter(Mandatory)][string]$Out,
    [string]$Cases = '',
    [string]$WorkDir = ''
)

# Set-StrictMode は使わない。上流のスクリプトと取り出した関数はこのスコープの下で動き、
# 厳格モードを引き継ぐ。上流は厳格モードを前提に書かれていないので、使うと上流の
# 振る舞いそのものが変わる。
$ErrorActionPreference = 'Stop'
# ConvertFrom-Csv は名前の空の列に既定の名前を振るとき警告を出す。結果は変わらないので黙らせる。
$WarningPreference = 'SilentlyContinue'

# 日本語が化けないようにする。リダイレクト先によっては変えられないため、失敗しても進む
# （scripts/setup.ps1 と同じ）。
try {
    [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
    $OutputEncoding = [Console]::OutputEncoding
} catch {
    Write-Verbose "出力の文字コードを UTF-8 にできなかった: $_"
}

$utf8 = [System.Text.UTF8Encoding]::new($false)
$locale = 'xx'

if ($Cases -eq '') {
    $Cases = Join-Path $PSScriptRoot '..' 'testdata' 'upstream' 'cases.json'
}
$Script = (Resolve-Path -LiteralPath $Script).Path
$Cases = (Resolve-Path -LiteralPath $Cases).Path
$Out = [System.IO.Path]::GetFullPath($Out)

# ---- JSON ----------------------------------------------------------------
# 読むのは System.Text.Json にする。ConvertFrom-Json は版によって日付に見える文字列を
# DateTime に変えるので、入力の文字列がそのまま届く保証が無い。
# 書くのは手で組み立てる。ConvertTo-Json は改行が OS で変わり、どの文字を \u で書くかも
# 版で違いうる。版や OS を替えて作り直した正解を、テキストの差分でそのまま見比べられる
# ように、書式は版に依らず同じにする。

function Get-JsonString($element, [string]$name) {
    # TryGetProperty は out 引数の型を PowerShell から渡しにくいので、並びをたどる。
    foreach ($property in $element.EnumerateObject()) {
        if ($property.Name -ceq $name -and $property.Value.ValueKind -eq [System.Text.Json.JsonValueKind]::String) {
            return $property.Value.GetString()
        }
    }
    return $null
}

# ConvertTo-JsonText は、順序付きの辞書・配列・文字列・整数・真偽値・$null だけを書く。
function ConvertTo-JsonText($value, [int]$depth = 0) {
    $pad = '  ' * ($depth + 1)
    $end = '  ' * $depth
    if ($null -eq $value) { return 'null' }
    if ($value -is [string]) { return (ConvertTo-JsonStringLiteral $value) }
    if ($value -is [bool]) { return $(if ($value) { 'true' } else { 'false' }) }
    if ($value -is [int] -or $value -is [long]) { return $value.ToString([System.Globalization.CultureInfo]::InvariantCulture) }
    if ($value -is [System.Collections.IDictionary]) {
        if ($value.Count -eq 0) { return '{}' }
        $parts = foreach ($k in $value.Keys) {
            $pad + (ConvertTo-JsonStringLiteral ([string]$k)) + ': ' + (ConvertTo-JsonText $value[$k] ($depth + 1))
        }
        return "{`n" + ($parts -join ",`n") + "`n$end}"
    }
    if ($value -is [System.Collections.IEnumerable]) {
        $items = @($value)
        if ($items.Count -eq 0) { return '[]' }
        # 文字列と null だけの短い配列（レコードの値や列名）は1行にまとめる。見比べやすくするため。
        $flat = $true
        foreach ($item in $items) { if ($null -ne $item -and $item -isnot [string]) { $flat = $false; break } }
        if ($flat) {
            $line = '[' + (($items | ForEach-Object { ConvertTo-JsonText $_ ($depth + 1) }) -join ', ') + ']'
            if ($line.Length -le 200) { return $line }
        }
        $parts = foreach ($item in $items) { $pad + (ConvertTo-JsonText $item ($depth + 1)) }
        return "[`n" + ($parts -join ",`n") + "`n$end]"
    }
    throw "JSON に書けない値: $($value.GetType().FullName)"
}

# ConvertTo-JsonStringLiteral は文字列を JSON の文字列にする。制御文字と、目に見えない
# 文字（空白の仲間や書式の文字）は \uXXXX で書く。読む人が気付けるようにするため。
function ConvertTo-JsonStringLiteral([string]$s) {
    $b = [System.Text.StringBuilder]::new($s.Length + 2)
    [void]$b.Append('"')
    foreach ($c in $s.ToCharArray()) {
        $code = [int]$c
        $invisible = $code -lt 0x20 -or ($code -ge 0x7f -and $code -le 0xa0) -or $code -eq 0xad -or
            ($code -ge 0x2000 -and $code -le 0x200f) -or $code -eq 0x2028 -or $code -eq 0x2029 -or
            $code -eq 0x3000 -or $code -eq 0xfeff -or ($code -ge 0xd800 -and $code -le 0xdfff)
        if ($code -eq 0x22) {
            [void]$b.Append('\"')
        } elseif ($code -eq 0x5c) {
            [void]$b.Append('\\')
        } elseif ($code -eq 0x0a) {
            [void]$b.Append('\n')
        } elseif ($code -eq 0x0d) {
            [void]$b.Append('\r')
        } elseif ($code -eq 0x09) {
            [void]$b.Append('\t')
        } elseif ($invisible) {
            [void]$b.Append('\u').Append($code.ToString('x4'))
        } else {
            [void]$b.Append($c)
        }
    }
    [void]$b.Append('"')
    return $b.ToString()
}

# ---- 上流の関数を取り出す -------------------------------------------------

$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($Script, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) {
    throw "上流のスクリプトを解析できない: $($parseErrors[0])"
}
$wanted = @('Read-Csv', 'Remove-NonRecords')
$definitions = $ast.FindAll({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $wanted -contains $node.Name
    }, $true)
foreach ($name in $wanted) {
    if (-not ($definitions | Where-Object Name -EQ $name)) {
        throw "上流のスクリプトに関数 $name が無い"
    }
}
foreach ($definition in $definitions) {
    # 定義だけをこのスコープで評価する。上流のスクリプトの本体（変換の処理）は走らない。
    . ([scriptblock]::Create($definition.Extent.Text))
}

# ---- 読み方 ---------------------------------------------------------------

# Get-LineEnds は、BOM の後ろを '\r\n' / '\n' / '\r' で物理行に分け、各行の終わり
# （改行を含む）の位置を返す。末尾の改行の後ろに空の行は数えない。
function Get-LineEnds([string]$text) {
    $ends = [System.Collections.Generic.List[int]]::new()
    $i = 0
    if ($text.Length -gt 0 -and $text[0] -eq [char]0xfeff) { $i = 1 }
    $start = $i
    while ($i -lt $text.Length) {
        $c = $text[$i]
        if ($c -eq "`r") {
            if ($i + 1 -lt $text.Length -and $text[$i + 1] -eq "`n") { $i++ }
            $ends.Add($i + 1)
            $start = $i + 1
        } elseif ($c -eq "`n") {
            $ends.Add($i + 1)
            $start = $i + 1
        }
        $i++
    }
    if ($start -lt $text.Length) { $ends.Add($text.Length) }
    return , $ends
}

# ConvertTo-ErrorInfo は例外を、版と言語に依らない名前と、読むための文面にする。
function ConvertTo-ErrorInfo($errorRecord) {
    return [ordered]@{
        id      = [string]$errorRecord.FullyQualifiedErrorId
        type    = $errorRecord.Exception.GetType().FullName
        message = $errorRecord.Exception.Message
    }
}

# Read-Text は text をファイルに書き、上流の Read-Csv で読む。
function Read-Text([string]$text, [string]$path) {
    [System.IO.File]::WriteAllText($path, $text, $utf8)
    try {
        $rows = @(Read-Csv $path)
    } catch {
        return [pscustomobject]@{ Error = (ConvertTo-ErrorInfo $_); Rows = @() }
    }
    $out = foreach ($row in $rows) {
        $columns = [System.Collections.Generic.List[string]]::new()
        $values = [System.Collections.Generic.List[object]]::new()
        foreach ($p in $row.PSObject.Properties) {
            $columns.Add($p.Name)
            if ($null -eq $p.Value) { $values.Add($null) } else { $values.Add([string]$p.Value) }
        }
        [pscustomobject]@{
            Columns = $columns
            Values  = $values
            # Key は値の比較に使う。null と空文字を分けて、列の名前も含める。
            Key     = (ConvertTo-JsonText $columns) + '=' + (ConvertTo-JsonText $values)
        }
    }
    return [pscustomobject]@{ Error = $null; Rows = @($out) }
}

# Get-ReadResult は上流の読み方で text を読み、レコードごとに物理行の範囲を付ける。
function Get-ReadResult([string]$text, [string]$path) {
    $final = Read-Text $text $path
    $result = [ordered]@{ error = $null; columns = @(); records = @() }
    if ($null -ne $final.Error) {
        $result.error = $final.Error
        return $result
    }
    $rows = $final.Rows
    if ($rows.Count -eq 0) { return $result }
    $result.columns = $rows[0].Columns

    # 先頭 k 行だけを読んだ結果（plain）と、その後ろに印の行を足して読んだ結果（marked）。
    # k = 0 は BOM だけ（あれば）。
    #
    # 開始行は、そのレコードが plain に初めて現れる k である。
    # 終了行は、marked のそのレコードが全体を読んだときと同じになる最小の k である。
    # 引用が開いたまま k 行目で切れていれば、足した印の行は値に飲み込まれるので
    # 値が変わる。閉じていれば印は別のレコードになり、そのレコードは変わらない。
    # 印を足さずに比べると、途中で切った値がたまたま全体と同じになる形（訳が
    # 改行1つだけの "\n" など）で、終了行を早く見誤る。
    $ends = Get-LineEnds $text
    $bom = if ($text.Length -gt 0 -and $text[0] -eq [char]0xfeff) { 1 } else { 0 }
    $marker = [string][char]0x2063 + "dwloc-prefix-marker`n"
    $plain = [System.Collections.Generic.List[object]]::new()
    $marked = [System.Collections.Generic.List[object]]::new()
    for ($k = 0; $k -le $ends.Count; $k++) {
        $cut = if ($k -eq 0) { $bom } else { $ends[$k - 1] }
        $prefix = $text.Substring(0, $cut)
        $plain.Add((Read-Text $prefix $path).Rows)
        if ($k -lt $ends.Count) {
            $marked.Add((Read-Text ($prefix + $marker) $path).Rows)
        } else {
            $marked.Add($rows)
        }
    }

    $records = for ($i = 0; $i -lt $rows.Count; $i++) {
        $first = -1
        for ($k = 0; $k -le $ends.Count; $k++) {
            if ($plain[$k].Count -gt $i) { $first = $k; break }
        }
        $end = $ends.Count
        for ($k = [Math]::Max($first, 0); $k -le $ends.Count; $k++) {
            if ($marked[$k].Count -gt $i -and $marked[$k][$i].Key -eq $rows[$i].Key) { $end = $k; break }
        }
        [ordered]@{ line = $first; end = $end; values = $rows[$i].Values }
    }
    $result.records = @($records)
    return $result
}

# ---- 通しの実行 -----------------------------------------------------------

$summaryPattern = ': (\d+) converted, (\d+) already hashed, (\d+) per-line, (\d+) malformed dropped, ' +
    '(\d+) kept from the published file, (\d+) in play order, (\d+) other$'

function Write-TextFile([string]$path, [string]$text) {
    [void][System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($path))
    [System.IO.File]::WriteAllText($path, $text, $utf8)
}

# Invoke-Upstream は1つの入力で上流のスクリプトを通しで走らせる。
function Invoke-Upstream($case, [string]$root, [string]$scriptBytesPath, $shared) {
    $tool = Join-Path $root 'tools' 'hash-strings.ps1'
    [void][System.IO.Directory]::CreateDirectory((Split-Path -Parent $tool))
    [System.IO.File]::Copy($scriptBytesPath, $tool)
    Write-TextFile (Join-Path $root 'data' 'script_order.csv') $shared.ScriptOrder
    Write-TextFile (Join-Path $root 'data' 'level_flow.csv') $shared.LevelFlow
    $published = Join-Path $root 'Translations' $locale 'strings.csv'
    switch ($case.As) {
        'working' {
            Write-TextFile (Join-Path $root 'Translations' '_discovered' "$locale.working.csv") $case.Text
            $base = if ($null -ne $case.Published) { $case.Published } else { $shared.PublishedDefault }
            Write-TextFile $published $base
        }
        'published' { Write-TextFile $published $case.Text }
        default { throw "$($case.Name): as の値が分からない: $($case.As)" }
    }

    $messages = [System.Collections.Generic.List[string]]::new()
    $failure = $null
    try {
        # Write-Host は情報ストリーム（6）に出るので、それを受け取る。
        & $tool 6>&1 | ForEach-Object { $messages.Add([string]$_) }
    } catch {
        $failure = ConvertTo-ErrorInfo $_
    }

    $result = [ordered]@{ error = $failure; summary = $null; output = $null }
    if ([System.IO.File]::Exists($published)) {
        # GetString は BOM を剥がさないので、書かれたバイト列がそのまま文字列になる。
        $result.output = $utf8.GetString([System.IO.File]::ReadAllBytes($published))
    }
    if ($null -eq $failure) {
        if ($messages.Count -ne 1) {
            throw "$($case.Name): 集計の行が1行ではない: $($messages -join ' / ')"
        }
        $m = [regex]::Match($messages[0], $summaryPattern)
        if (-not $m.Success) {
            throw "$($case.Name): 集計の行の形が違う: $($messages[0])"
        }
        $n = { param($i) [int]$m.Groups[$i].Value }
        $result.summary = [ordered]@{
            converted           = & $n 1
            already_hashed      = & $n 2
            per_line            = & $n 3
            malformed_dropped   = & $n 4
            kept_from_published = & $n 5
            in_play_order       = & $n 6
            other               = & $n 7
        }
    }
    return $result
}

# ---- 本体 -----------------------------------------------------------------

$casesBytes = [System.IO.File]::ReadAllBytes($Cases)
$document = [System.Text.Json.JsonDocument]::Parse([string]$utf8.GetString($casesBytes), [System.Text.Json.JsonDocumentOptions]::new())
$rootElement = $document.RootElement
$data = $rootElement.GetProperty('data')
$shared = [pscustomobject]@{
    ScriptOrder      = Get-JsonString $data 'script_order'
    LevelFlow        = Get-JsonString $data 'level_flow'
    PublishedDefault = Get-JsonString $rootElement 'published_default'
}
$caseList = foreach ($element in $rootElement.GetProperty('cases').EnumerateArray()) {
    [pscustomobject]@{
        Name      = Get-JsonString $element 'name'
        As        = Get-JsonString $element 'as'
        Text      = Get-JsonString $element 'text'
        Published = Get-JsonString $element 'published'
    }
}

$scriptBytes = [System.IO.File]::ReadAllBytes($Script)
# git の blob の名前（git hash-object と同じ）。どのファイルから作ったかを後で照らし合わせる。
$blobHeader = [System.Text.Encoding]::ASCII.GetBytes("blob $($scriptBytes.Length)`0")
$blob = [System.Security.Cryptography.SHA1]::HashData([byte[]]($blobHeader + $scriptBytes))
$casesHash = [System.Security.Cryptography.SHA256]::HashData($casesBytes)

if ($WorkDir -eq '') { $WorkDir = [System.IO.Path]::GetTempPath() }
$work = Join-Path $WorkDir ('upstream-fixtures-' + [guid]::NewGuid().ToString('N'))
[void][System.IO.Directory]::CreateDirectory($work)
$scriptCopy = Join-Path $work 'hash-strings.ps1'
[System.IO.File]::WriteAllBytes($scriptCopy, $scriptBytes)

try {
    $index = 0
    $results = foreach ($case in $caseList) {
        $index++
        $readPath = Join-Path $work ('read-{0:d3}.csv' -f $index)
        $read = Get-ReadResult $case.Text $readPath
        $run = Invoke-Upstream $case (Join-Path $work ('run-{0:d3}' -f $index)) $scriptCopy $shared
        [ordered]@{ name = $case.Name; read = $read; run = $run }
    }
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force
}

$os = if ($IsWindows) { 'windows' } elseif ($IsLinux) { 'linux' } elseif ($IsMacOS) { 'macos' } else { 'unknown' }
$fixture = [ordered]@{
    about     = @(
        '上流 tools/hash-strings.ps1 に testdata/upstream/cases.json を読ませた結果。scripts/upstream-fixtures.ps1 が書く。手で直さない。',
        'read は Read-Csv だけを取り出して読ませた結果。line と end は物理行の範囲（\r\n / \n / \r で分け、BOM は数えない）。',
        'run は上流のスクリプトを通しで走らせた結果。output は書き出した strings.csv、summary は集計の1行の値。',
        'error の message は pwsh の言語で変わる。比べるときは id と type を見る。'
    )
    upstream  = [ordered]@{
        commit = $Commit
        script = 'tools/hash-strings.ps1'
        blob   = ([System.BitConverter]::ToString($blob) -replace '-', '').ToLowerInvariant()
    }
    pwsh      = [ordered]@{
        version = $PSVersionTable.PSVersion.ToString()
        dotnet  = [System.Runtime.InteropServices.RuntimeInformation]::FrameworkDescription
        os      = $os
        culture = [System.Globalization.CultureInfo]::CurrentCulture.Name
    }
    cases_sha256 = ([System.BitConverter]::ToString($casesHash) -replace '-', '').ToLowerInvariant()
    cases     = @($results)
}

Write-TextFile $Out ((ConvertTo-JsonText $fixture) + "`n")
Write-Host "$Out に $($caseList.Count) 件の正解を書いた（pwsh $($PSVersionTable.PSVersion)、$os）"
