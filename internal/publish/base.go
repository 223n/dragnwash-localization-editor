package publish

import (
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// baseDriftListMax は [CheckBase] が1ロケールにつき何件まで持ち帰るか。
//
// 数え上げは全件行うが、持ち帰るのは見本だけにする。土台がずれているときは
// たいてい版まるごとがずれていて、件数が数百に達する。呼び出し側が全件を
// 受け取っても、端末へ並べれば「なぜ止めたか」が流れて消える。
const baseDriftListMax = 5

// driftLeadRunes は、食い違う位置の手前に何文字残すか（[driftHeads]）。
const driftLeadRunes = 4

// BaseDrift は、ゲーム側の公開ファイルとコミット済みの公開ファイルで
// 訳が食い違っているキー1件。
type BaseDrift struct {
	// Locale はロケール名。
	Locale string
	// Key は食い違っているキー。コミット済みのファイルにあった綴りのまま。
	//
	// 畳んだ形（[csvfile.FoldASCII]）では返さない。人はこの値をファイルから
	// 探すので、綴りが変わっていると引き当てられない。
	Key string
	// Repo はコミット済みの訳の先頭だけ（[lossHeadRunes] 文字）。制御文字は見える
	// 印に置き換えてある（[Visible]）。
	Repo string
	// Game はゲームに入っている訳の先頭だけ。Repo と同じく印に置き換えてある。
	Game string
}

// BaseResult は [CheckBase] の結果。
type BaseResult struct {
	// Locale はロケール名。
	Locale string
	// Count は食い違っているキーの総数。
	Count int
	// Sample は見本。多くても [baseDriftListMax] 件。
	Sample []BaseDrift
}

// CheckBase は、ゲーム側の作業コピーが建っている土台がコミット済みと
// そろっているかを確かめる。
//
// 確かめるのは t.GameBase が埋まっているとき、つまり入力がゲーム側の作業コピーに
// なったときだけである。リポジトリの作業コピーや公開ファイル自身を入力にした
// ロケールには、見比べる土台が無い。
//
// なぜ要るか。Modは「いま読み込んでいる訳」を作業コピーへ書き出す。ゲームに
// 入っている公開ファイルが古ければ、作業コピーの訳も古い。それを入力にすると、
// 新しいコミットが古い版へ静かに巻き戻る。[CheckLoss] はこれを捕まえられない。
// 訳は消えておらず、書き換わっただけだからである。
//
// では「書き換わったら止める」にすればよいかというと、それはできない。訳を
// 書き換えることこそ翻訳者の仕事で、止めれば publish が使えなくなる。
// 編集と巻き戻りを見分けるには3点目が要る。それがこの土台である。
//
//   - コミット済み == ゲーム側 … 作業コピーの違いは翻訳者が入れたもの
//   - コミット済み != ゲーム側 … 作業コピーは別の土台の上に建っている
//
// 見るのは訳だけである。section や order の食い違いは見ない。ゲーム側の
// 再生順はリポジトリのものを使うので、そこがずれていても書き出す中身は変わらない。
// 訳は値の中の CRLF を LF にそろえてから比べる（[sameTranslation]）。
//
// 読み方は publish と同じく全体を解釈する。ゲーム側の公開ファイルの形の崩れ
// （閉じない引用符など）は、呼び出し側が先に形の確かめ（[CheckTargetShape]）で止める。
//
// ゲーム側に公開ファイルが無いときは、そろっているともいないとも言えないので
// 何も返さない。そのロケールで訳が実際に消えるなら [CheckLoss] が捕まえる。
func CheckBase(t Target, repoCurrent []byte) (BaseResult, error) {
	res := BaseResult{Locale: t.Locale}
	if t.GameBase == "" {
		return res, nil
	}
	gameBytes, err := readIfExists(t.GameBase)
	if err != nil {
		return res, err
	}
	if gameBytes == nil {
		return res, nil
	}

	repo, err := readTranslations(repoCurrent)
	if err != nil {
		return res, err
	}
	game, err := readTranslations(gameBytes)
	if err != nil {
		return res, err
	}

	// 片方にしか無いキーは数えない。ゲームの版が古ければ、行そのものの
	// 増減は当たり前に起きる。ここで見たいのは「同じ行の訳が違う」ことだけで、
	// 行の増減で実際に訳が消えるなら [CheckLoss] が捕まえる。
	//
	// なぞるのは order（公開ファイルに現れた順）である。map を直になぞると、
	// 見本に入る5件とその並びが実行のたびに変わる。
	for _, mine := range repo.order {
		theirs, both := game.at[mine.folded]
		if !both || sameTranslation(mine.text, theirs.text) {
			continue
		}
		res.Count++
		if len(res.Sample) < baseDriftListMax {
			repoHead, gameHead := driftHeads(mine.text, theirs.text)
			res.Sample = append(res.Sample, BaseDrift{
				Locale: t.Locale, Key: mine.key,
				Repo: repoHead, Game: gameHead,
			})
		}
	}
	return res, nil
}

// sameTranslation は、コミット済みの訳とゲーム側の訳が同じかを返す。値の中の
// CRLF は LF にそろえてから比べる（決まったことのそのほか 1）。
//
// 翻訳リポジトリの .gitattributes は `*.csv text eol=lf` なので、コミットで値の中の
// CRLF も LF になる。一方ゲーム側の公開ファイルは Mod や翻訳者が書いたままで、CRLF の
// ことがある。改行コードだけの違いで「ゲームが古い」と止めると、同じ訳なのに publish が
// 通らない。単独の CR はそろえない。git も変えず、上流の道具はその行を落とすので、
// 同じ訳とは言えない（形の確かめが止める）。台詞ID の行も同じ規則で比べる。
func sameTranslation(repo, game string) bool {
	return repo == game || strings.ReplaceAll(repo, "\r\n", "\n") == strings.ReplaceAll(game, "\r\n", "\n")
}

// driftHeads は、2つの訳の「違っているところが見える」切り出しを返す。
//
// 先頭から [lossHeadRunes] 文字を出すだけでは足りない。実測（実機のゲーム
// フォルダー）で出た3件は、3件とも先頭12文字が同じで、並べても同じ文字列が
// 2行出るだけだった。違いを見せるための報告なのに、違いが切り落とされていた。
//
// そこで、最初に食い違う位置が窓に入るように窓をずらす。前を削ったときは
// 頭にも省略の印を付ける。どちらかが他方の先頭部分そのものというときは
// （閉じタグの付け外しなど）、短いほうの終わりが食い違う位置になる。
func driftHeads(repo, game string) (string, string) {
	a, b := []rune(repo), []rune(game)
	at := 0
	for at < len(a) && at < len(b) && a[at] == b[at] {
		at++
	}
	// 違いの手前を少しだけ残す。どこが変わったのかは、前後があるほうが読める。
	start := 0
	if at >= lossHeadRunes {
		start = at - driftLeadRunes
	}
	return window(a, start), window(b, start)
}

// window は runes の start から [lossHeadRunes] 文字を切り出し、
// 前後を削ったぶんだけ省略の印を付ける。
func window(runes []rune, start int) string {
	if start > len(runes) {
		start = len(runes)
	}
	end := start + lossHeadRunes
	cut := end < len(runes)
	if cut {
		runes = runes[:end]
	}
	// 制御文字（値の中の改行など）は見える印に置き換える（[Visible]）。改行が入ったまま
	// 並べると、コミット済みとゲーム側の2行の見本が何行にも割れて読めない。
	out := Visible(string(runes[start:]))
	if start > 0 {
		out = lossHeadEllipsis + out
	}
	if cut {
		out += lossHeadEllipsis
	}
	return out
}

// BaseReason は土台がずれていることの理由。置換は locale と count。
func BaseReason(locale string, count int) reason.Reason {
	n := strconv.Itoa(count)
	return reason.New(reason.PublishBaseDrift,
		"ゲームに入っている訳が "+locale+" で "+n+" 件ちがう",
		"locale", locale, "count", n)
}

// translations は公開ファイルの「訳のある行」を2通りに持つ。
//
// order はファイルに現れた順、at は畳んだキーで引く表である。両方持つのは、
// 突き合わせでは引き当てが要るのに、報告では順が要るからである。map だけに
// すると、報告に並ぶ見本が実行のたびに入れ替わる（Go の map の反復順は
// わざと毎回違う）。同じ入力で違う報告が出る道具は、貼られた報告から元の
// 状態を読み取れない。
type translations struct {
	// order はファイルに現れた順。重複したキーは先勝ちで、2件目以降は入らない。
	order []keyedText
	// at は畳んだキーで引く表。中身は order と同じものを指す。
	at map[string]keyedText
}

// readTranslations は公開ファイルから [translations] を作る。
//
// 引き当ては [survivors] と同じく [csvfile.FoldASCII] で畳む。訳の入っていない
// 行は入れない。土台がそろっているかは、訳のある行だけで決まる。読み方は publish が
// 入力を読むときと同じ（全体を解釈する [csvfile.ReadPowerShell]）。
func readTranslations(data []byte) (translations, error) {
	file, err := csvfile.ReadPowerShell(data)
	if err != nil {
		return translations{}, err
	}
	rows := file.Rows()
	out := translations{
		order: make([]keyedText, 0, len(rows)),
		at:    make(map[string]keyedText, len(rows)),
	}
	for _, row := range rows {
		k := strings.TrimSpace(row.Get(colKey))
		if k == "" {
			continue
		}
		tr := row.Get(colTranslation)
		if tr == "" {
			continue
		}
		folded := csvfile.FoldASCII(k)
		if _, dup := out.at[folded]; dup {
			continue // 先勝ち（R18）
		}
		entry := keyedText{key: k, folded: folded, text: tr}
		out.order = append(out.order, entry)
		out.at[folded] = entry
	}
	return out, nil
}

// keyedText は1行ぶんのキーと訳。
type keyedText struct {
	// key はファイルにあった綴りのまま。報告に出すのはこちらである。
	key string
	// folded は引き当て用に畳んだキー（[csvfile.FoldASCII]）。
	folded string
	// text はその行の訳。
	text string
}
