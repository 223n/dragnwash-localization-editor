package diff

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// Locale は1ロケール分の読み込み結果。
type Locale struct {
	// Name は Translations 直下のディレクトリ名（"ja"、"pt-BR" など）。
	Name string
	// PublishedPath は Translations/<ロケール>/strings.csv。存在しないこともある。
	PublishedPath string
	// WorkingPath は Translations/_discovered/<ロケール>.working.csv。
	// 存在しなくても値は入る。「どこに置けばよいか」を表示で伝えるため。
	WorkingPath string

	// Published は公開ファイルの行。ファイルが無ければ空。
	Published []Row
	// Working は作業コピーの行。読まなかったときは空。
	Working []Row
	// HasWorking は作業コピーを読んだかどうか。
	// ファイルが無いときと --no-working で読まなかったときの両方で false になる。
	HasWorking bool
	// WorkingExists は作業コピーのファイルが実在するかどうか。
	//
	// HasWorking と分けてあるのは、「ありません」と「読みませんでした」を
	// 区別して伝えるため。--no-working を付けた利用者に「ゲーム内で書き出して
	// ください」と促すと、すでに済んでいる作業をやり直させることになる。
	WorkingExists bool

	// LayoutRisksPath は Translations/_discovered/layout_risks.csv。
	// 作業コピーと同じディレクトリにある。存在しなくても値は入る。
	LayoutRisksPath string
	// LayoutRisks はゲームが測ったはみ出しの記録。読まなかったときは空。
	//
	// ロケールごとのファイルではない。ゲームでそのとき選んでいた言語の結果が
	// 1つのファイルに書かれるので、全ロケールに同じ中身が入る。どの行がこの
	// ロケールの結果かは layout.go が訳を突き合わせて決める。
	LayoutRisks []LayoutRisk
	// HasLayoutRisks はその記録を読んだかどうか。
	HasLayoutRisks bool
	// LayoutRisksExist はその記録のファイルが実在するかどうか。
	//
	// HasLayoutRisks と分けてあるのは、[Locale.WorkingExists] と同じ理由で、
	// 「ありません」と「読みませんでした」を区別して伝えるため。
	LayoutRisksExist bool

	// PublishedUnclosed は、公開ファイルで開いた引用符がファイルの終わりまで
	// 閉じなかったときの、その引用符の物理行（1始まり）。閉じていれば 0。
	//
	// そのときの Published は空である。全体を解釈して読むと、引用符が開いた行から
	// ファイルの終わりまで（英語の原文を含むこともある）が1つの値に崩れる。値を
	// 半分だけ使うと、どこまでが正しい行かを決められないので、1行も使わない。
	// このロケールのカテゴリはどれも判定せず、ほかのロケールでも、ロケールどうしを
	// 比べるカテゴリを判定しない（[Summary.canJudge]）。読み込みの誤りにしないのは、
	// 1つのファイルのせいで全ロケールの報告と画面が止まらないようにするためである
	// （決まったことの 3）。
	PublishedUnclosed int
	// WorkingUnclosed は作業コピーについて同じ。そのときは WorkingExists が true、
	// HasWorking が false になり、作業コピーを要るカテゴリを判定しない。
	WorkingUnclosed int
	// LayoutRisksUnclosed ははみ出しの記録について同じ。そのときは LayoutRisksExist が
	// true、HasLayoutRisks が false になる。
	LayoutRisksUnclosed int
}

// Repo は比較に必要なものを読み終えた状態。
type Repo struct {
	// Root は翻訳リポジトリのルート。
	Root string
	// Order は再生順。ファイルが無くても nil にはならず、行が0件になる。
	Order *order.Data
	// OrderPath は再生順の読み込み元（data/script_order.csv）。
	OrderPath string
	// OrderUnclosed は、再生順で開いた引用符がファイルの終わりまで閉じなかったときの、
	// その引用符の物理行（1始まり）。閉じていれば 0。
	//
	// そのときの Order は行が0件になる（[Locale.PublishedUnclosed] と同じ理由）。
	// 再生順はどのカテゴリの判定にも位置の根拠にも使うので、報告全体を判定しない。
	//
	// 見出しの表（data/level_flow.csv）の閉じない引用符はここに入れない
	// （[Repo.LevelFlowUnclosed]）。
	OrderUnclosed int
	// LevelFlowPath は見出しの表（data/level_flow.csv）の場所。ファイルが無くても
	// 値は入る。
	LevelFlowPath string
	// LevelFlowUnclosed は、見出しの表で開いた引用符がファイルの終わりまで閉じなかった
	// ときの、その引用符の物理行（1始まり）。閉じていれば 0。
	//
	// このパッケージは見出しの文言を使わないので、判定は変わらず、[Report.Unclosed] にも
	// 入れない（終了コードを変えない）。publish と画面の書き出しは形の確かめで止めるので、
	// dwloc diff はこれを見て標準エラーに1行の警告を出す（決まったことの 18）。
	LevelFlowUnclosed int
	// OldOrder は1つ前の版の再生順。取れなかったときは nil。
	// 使うのは引き継ぎ候補だけで、ほかの8カテゴリはこれを見ない。
	OldOrder *order.Data
	// OldOrderReason は [Repo.OldOrder] が nil のときの理由。取れたときは空。
	//
	// 「0 件」と書けない場面を利用者に伝えるために持つ。旧版が取れないのに
	// 引き継ぎ候補を 0 件と書くと、「移すべき訳は無い」と読まれてしまう。
	OldOrderReason string
	// OldOrderReasonID は [Repo.OldOrderReason] に対応する安定した識別子
	// （internal/reason）。名前を付けていない理由のときは空。
	//
	// 文面と別に持つのは、画面（internal/web）がこれを鍵にして目録から訳された
	// 文面を引くためである。[OldOrderSource] を差し替えた呼び出し側が返す誤りには
	// 名前を当てられないので、そのときは空のまま文面へ落ちる。
	OldOrderReasonID string
	// Locales はディレクトリ名順。--locale で絞っても、ここには全ロケールが入る。
	Locales []Locale
	// EmptyLocales は Translations 直下にディレクトリだけがあり、公開ファイルも
	// 作業コピーも無いロケールの名前。ディレクトリ名順。
	//
	// publish.DiscoverTargets は入力の無いロケールを列挙から外す。publish には
	// それが正しい（書き出す元が無い）が、この道具では「訳が1件も無い」という
	// 最大の要作業にあたるので、黙って消さずに別に持つ。
	EmptyLocales []string
}

// Options は [LoadWith] の指定。
//
// 引数を増やさず構造体にしたのは、旧再生順の取り方を差せるようにしたあとも
// [Load] の呼び出し側（cmd/dwloc）を変えずに済ませるため。
type Options struct {
	// Working が true なら作業コピーも読む。
	Working bool
	// Game はゲーム側のプラグインフォルダー。空なら見に行かない。
	//
	// 作業コピーの探し先を1つ増やすだけで、再生順は常にリポジトリ側から読む
	// （publish パッケージの doc コメントを参照）。引き継ぎ候補は git の
	// 履歴にある1つ前の再生順が根拠なので、履歴の無いゲーム側へ替えると
	// 判定そのものが消える。
	Game string
	// OldOrder は1つ前の版の再生順を返す関数。nil なら [GitOldOrder]。
	//
	// テストで旧再生順を直に渡すための穴でもある。ここを固定にすると、
	// 引き継ぎ候補を試すだけで git リポジトリが要る形になってしまう。
	OldOrder OldOrderSource
}

// Load は root 配下を読む。旧再生順の取り方は [GitOldOrder]。
//
// ロケールの列挙と作業コピーの有無の判定は publish.DiscoverTargetsWithGame に
// 委ねる。走査の規則を2か所に持つと、publish が対象にするロケールと、この道具が
// 報告するロケールが食い違う。Target.Input != Target.Output であることが、
// 作業コピーがある状態そのものになる。
//
// --game を渡したときの探し先も publish と同じになる。publish もゲーム側の
// 作業コピーを入力にするので、この道具の「publish を回すとこうなる」という
// 読み方は、ゲーム側の作業コピーを読んだときにも成り立つ。
//
// useWorking が false のときは作業コピーを読まない。公開ファイルだけで何が
// 言えるかを再現したいとき（作業コピーが古いときを含む）に使う。
//
// エラーを返すのは Translations を走査できないとき、再生順のヘッダーに列名の
// 重複があるとき、ファイルを読めないとき、公開ファイルか作業コピーのヘッダーに
// 列名の重複があるとき。公開ファイルが存在しないロケール（作業コピーだけがある
// 状態）はエラーにせず、公開0行として扱う。
//
// 閉じない引用符（開いた引用符がファイルの終わりまで閉じない）もエラーにしない。
// そのファイルの行は使わず、どの行で開いたかを [Repo.OrderUnclosed] や
// [Locale.PublishedUnclosed] などに残す。そのファイルに依るカテゴリは
// 「判定していません」になる（決まったことの 3）。1つのファイルのせいで報告と画面が
// まるごと止まると、直すための画面まで開けなくなる。
func Load(root string, useWorking bool) (*Repo, error) {
	return LoadWith(root, Options{Working: useWorking})
}

// LoadWith は [Load] と同じことを、指定を変えられる形で行う。
func LoadWith(root string, opt Options) (*Repo, error) {
	data, unclosedOrder, err := publish.LoadOrderMarked(root)
	if err != nil {
		return nil, fmt.Errorf("再生順のデータを読めません: %w", err)
	}
	targets, err := publish.DiscoverTargetsWithGame(root, opt.Game)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", publish.TranslationsDir, err)
	}

	repo := &Repo{
		Root:          root,
		Order:         data,
		OrderPath:     data.Source,
		LevelFlowPath: publish.LevelFlowPath(root),
		Locales:       make([]Locale, 0, len(targets)),
	}
	for _, u := range unclosedOrder {
		// 見出しの表（level_flow.csv）の閉じない引用符は、判定を止めるものとしては
		// 数えない。このパッケージは見出しの文言を使わない（[Repo.LevelFlowUnclosed]）。
		switch u.Path {
		case data.Source:
			repo.OrderUnclosed = u.Line
		case repo.LevelFlowPath:
			repo.LevelFlowUnclosed = u.Line
		}
	}
	// 旧再生順が取れなくてもエラーにはしない。引き継ぎ候補1カテゴリだけが
	// 判定できなくなる話で、残り8カテゴリは旧版が無くても成り立つ。
	// git の無い環境で道具そのものが動かなくなるほうが困る。
	var oldOrderWhy reason.Reason
	repo.OldOrder, oldOrderWhy = loadOldOrder(root, repo.OrderPath, opt.OldOrder)
	repo.OldOrderReason, repo.OldOrderReasonID = oldOrderWhy.Text, oldOrderWhy.ID

	for _, t := range targets {
		workingPath := publish.WorkingPath(root, opt.Game, t.Locale)
		loc := Locale{
			Name:          t.Locale,
			PublishedPath: t.Output,
			WorkingPath:   workingPath,
			// はみ出しの記録は作業コピーと同じ場所にある。作業コピーが無くても
			// 記録だけがあることはあるので、パスは作業コピーの有無に関わらず持つ。
			LayoutRisksPath: filepath.Join(filepath.Dir(workingPath), LayoutRisksFile),
		}

		published, unclosed, err := readRowsFile(t.Output)
		if err != nil {
			return nil, err
		}
		loc.Published, loc.PublishedUnclosed = published, unclosed

		// 作業コピーの有無は走査と同じ根拠で決める。
		// Input と Output が違えば、そのロケールには作業コピーがある。
		if t.Input != t.Output {
			loc.WorkingExists = true
			// 実際に読むパスを覚えておく。_discovered 以外の場所を入力にする
			// 将来の変更があっても、表示が嘘にならないようにする。
			loc.WorkingPath = t.Input
			if opt.Working {
				working, unclosed, err := readRowsFile(t.Input)
				if err != nil {
					return nil, err
				}
				// 閉じない引用符で読めなかった作業コピーは、読まなかったものとして
				// 扱う（HasWorking は false のまま）。作業コピーを要るカテゴリは
				// 判定せず、タグの開閉などは --no-working と同じく公開ファイルを見る。
				loc.Working, loc.WorkingUnclosed = working, unclosed
				loc.HasWorking = unclosed == 0
			}
		}
		// はみ出しの記録。ゲーム側が書くファイルで、原文が入っているので、
		// 判定に使うかどうかは作業コピーと同じ指定（--no-working）で決める。
		// --no-working は「手元の再現できる入力だけで判定する」ための指定で、
		// ゲームが測った値はその外にある。
		//
		// ファイルの有無だけは指定に関わらず見る。「ありません」と
		// 「読みませんでした」を書き分けるのに要る。--no-working のときに
		// 中身まで読むと、読まないと言ったファイルが壊れているだけで
		// 読み込み全体が止まる（作業コピーは --no-working なら読まないのと同じ扱い）。
		if opt.Working {
			risks, unclosed, err := readLayoutRisks(loc.LayoutRisksPath)
			if err != nil {
				return nil, err
			}
			loc.LayoutRisksExist = risks != nil || unclosed > 0
			loc.LayoutRisksUnclosed = unclosed
			if risks != nil {
				loc.LayoutRisks = risks
				loc.HasLayoutRisks = true
			}
		} else if _, err := os.Stat(loc.LayoutRisksPath); err == nil {
			loc.LayoutRisksExist = true
		}
		repo.Locales = append(repo.Locales, loc)
	}

	// 走査そのものを publish と重ねているのは、両者の判定が食い違うと「publish を
	// 回すとどうなるか」という主張が崩れるため。拾うのは「DiscoverTargets が
	// 返さなかったディレクトリ」だけで、判定の規則はこのパッケージに作らない。
	empty, err := publish.EmptyLocales(root, targets)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", publish.TranslationsDir, err)
	}
	repo.EmptyLocales = empty
	return repo, nil
}

// loadOldOrder は1つ前の版の再生順を読む。読めなかったときは理由を返す。
//
// 理由を error のまま持ち回らないのは、呼び出し側（表示）で種類ごとに場合分けする
// 予定が無いのに型を残すと、「どの理由なら何を書くか」の分岐が表示側へ漏れるから。
// 文面に識別子を添えても、その性質は変わらない。表示側は識別子を目録の鍵にする
// だけで、どの理由かを見て書き分けることはしない。
func loadOldOrder(root, orderPath string, src OldOrderSource) (*order.Data, reason.Reason) {
	if src == nil {
		src = GitOldOrder
	}
	raw, err := src(root, orderPath)
	if err != nil {
		return nil, reason.New(oldOrderReasonID(err), err.Error())
	}
	data, err := order.LoadPowerShell(raw, nil)
	if err != nil {
		return nil, reason.New(reason.OldOrderUnreadable, "1つ前の再生順を読めません")
	}
	if !hasLineIDKeys(data) {
		// 取り出せはしたが、台詞IDとキーの組が1つも無い。列名が変わった古い版か、
		// 取り違えた別のファイル。突き合わせる手がかりが無いので判定しない。
		// ここで 0 件と書くと「移すべき訳は無い」と読まれる。
		return nil, reason.New(reason.OldOrderNoLineIDs, "1つ前の再生順に台詞IDとキーの組がありません")
	}
	return data, reason.Reason{}
}

// oldOrderReasonID は [OldOrderSource] が返した誤りに識別子を当てる。
//
// 当てるのはこのパッケージが定義した番兵だけである。差し替えた呼び出し側が返す
// 誤りには名前が無いので空を返し、画面はその文面をそのまま出す。当てずっぽうで
// 近い識別子を付けると、画面には関係のない英文が出る。日本語が1行残るほうが、
// 別の理由に読み替えられるよりよい。
func oldOrderReasonID(err error) string {
	switch {
	case errors.Is(err, ErrNoGit):
		return reason.OldOrderNoGit
	case errors.Is(err, ErrNoRepository):
		return reason.OldOrderNoRepository
	case errors.Is(err, ErrNotTracked):
		return reason.OldOrderNotTracked
	case errors.Is(err, ErrNotCommitted):
		return reason.OldOrderNotCommitted
	case errors.Is(err, ErrOnlyOneVersion):
		return reason.OldOrderOnlyOneVersion
	case errors.Is(err, ErrGitFailed):
		return reason.OldOrderGitFailed
	}
	return ""
}

// hasLineIDKeys は台詞IDとキーがそろった行が1つでもあるかを返す。
func hasLineIDKeys(data *order.Data) bool {
	for _, e := range data.Entries {
		if e.LineID != "" && e.Key != "" {
			return true
		}
	}
	return false
}

// readRowsFile はCSVファイルを読んで [Row] に直す。
// ファイルが無ければ0行を返しエラーにしない（作業コピーだけがあるロケール、
// あるいはディレクトリだけ作った直後のロケールのため）。
//
// 開いた引用符がファイルの終わりまで閉じなければ、行を返さずに、引用符が開いた
// 物理行を2つ目の戻り値で返す（誤りにしない。[LoadWith] の説明）。閉じていれば 0。
func readRowsFile(path string) ([]Row, int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := ReadRows(data)
	if line, ok := unclosedLine(err); ok {
		return nil, line, nil
	}
	if err != nil {
		// パスはそのまま返し、相対化は表示側（cmd/dwloc の displayPath）に任せる。
		// このパッケージは表示の基準になるルートを知らない。
		return nil, 0, &FileError{Path: path, Err: err}
	}
	return rows, 0, nil
}

// unclosedLine は、err が閉じない引用符の誤りなら、引用符が開いた物理行を返す。
//
// 読み手は、閉じない引用符と列名の重複の両方に当たるときは閉じない引用符を返す
// （csvfile.PowerShellFile.Err）。列名にファイルの終わりまでが入っていることが
// あるので、重複より先に直すべきだからである。ここも同じ順になる。
func unclosedLine(err error) (int, bool) {
	var unclosed *csvfile.UnclosedQuoteError
	if errors.As(err, &unclosed) {
		return unclosed.Line, true
	}
	return 0, false
}

// LayoutRisksFile はゲームがはみ出しの記録を書くファイルの名前。
//
// ロケール名が入らないのは、ゲームがそのとき選んでいた言語の結果を1つの
// ファイルへ上書きで書くからである（翻訳リポジトリの CONTRIBUTING.ja.md）。
const LayoutRisksFile = "layout_risks.csv"

// readLayoutRisks は layout_risks.csv を読む。ファイルが無ければ nil を返す。
//
// 記録が無いことと、記録が空であることは区別する。前者は「測っていない」で、
// 後者は「測ったがはみ出す行は無かった」である。0 件と書いてよいのは後者だけ
// なので、呼び出し側は nil かどうかで HasLayoutRisks を決める。
//
// 開いた引用符がファイルの終わりまで閉じなければ、nil と、引用符が開いた物理行を
// 返す（[readRowsFile] と同じ）。
func readLayoutRisks(path string) ([]LayoutRisk, int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	risks, err := ParseLayoutRisks(data)
	if line, ok := unclosedLine(err); ok {
		return nil, line, nil
	}
	if err != nil {
		return nil, 0, &FileError{Path: path, Err: err}
	}
	if risks == nil {
		// 中身が0行でも「読んだ」ことは伝えたい。nil は「ファイルが無い」の
		// 意味に使っているので、空の並びへ置き換える。
		risks = []LayoutRisk{}
	}
	return risks, 0, nil
}

// FileError はどのファイルで失敗したかを持つエラー。
//
// パスを文面に埋め込まずに持ち回るのは、表示のときに相対化するため。
// 手元の絶対パスには利用者名が入ることがあり、CIのログや不具合報告へ
// 貼られるとそのまま漏れる。
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("%s: %v", filepath.ToSlash(e.Path), e.Err)
}

func (e *FileError) Unwrap() error { return e.Err }
