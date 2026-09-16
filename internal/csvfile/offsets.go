package csvfile

// FieldOffsets は1物理行を走査し、各フィールドが始まるバイト位置を返す。
// 位置は「先頭（0）」と「区切りのカンマの次のバイト」で、返す個数は
// 引用符の外にあるカンマの数 + 1 に等しい。
//
// 引用符の解釈は [ParsePowerShellRecord] と同じ実装を共有する
// （[parsePowerShellField] をそのまま使う）。読みと書きで解釈が割れると
// 別のフィールドを書き換えてしまうため、ここは必ず同じ規則でなければならない。
// 引用フィールドの中のカンマは区切りにならず、閉じ引用符の後ろに文字が続く
// `"a"x,b` は2フィールド、閉じていない `a,"b,c` は2フィールドになる。
//
// [ParsePowerShellRecord] と契約が違う。
//
// 末尾の空フィールドを落とさない。ParsePowerShellRecord（正確には
// [parsePowerShellFields]）は、行末のフィールドが「値が空、かつ引用符で
// 閉じられていない」ときそれを捨てるので、`a,b,` から2フィールドを返す。
// FieldOffsets は3つ返す。
//
// この違いは意図的で、作業コピーの未訳行がまさに `a,b,` の形（最終列の
// translation が空）だからである。落としてしまうと、編集対象として最も多い
// 未訳行の最終フィールドの位置が求められず、訳を書き戻せない。
// 同じ理由で、空行相当（ParsePowerShellRecord の第2戻り値が false）でも
// 位置は返す。空文字の行には [0] を返す。
//
// 返り値は必ず1個以上で、昇順、先頭は常に 0。すべての位置は len(line) 以下。
func FieldOffsets(line string) []int {
	offsets := []int{0}
	i := 0
	for {
		// 値は使わない。欲しいのは「このフィールドがどこで終わるか」だけ。
		_, _, end := parsePowerShellField(line, i)
		if end >= len(line) {
			// 行末に達した。末尾が空でもフィールドは1つ数えたままにする
			// （offsets には既にこのフィールドの開始位置が入っている）。
			return offsets
		}
		// end は区切りのカンマの位置。次のフィールドはその次から始まる。
		i = end + 1
		offsets = append(offsets, i)
	}
}
