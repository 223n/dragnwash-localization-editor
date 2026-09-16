package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

const (
	// tokenParam は最初の1回だけ受け取る問い合わせ文字列の名前。
	tokenParam = "t"
	// cookieName は移し替えたあとの Cookie の名前。
	cookieName = "dwloc_session"
	// tokenBytes はトークンの長さ（バイト）。
	//
	// 32バイトにしてあるのは、総当たりを問題にしないため。手元の待ち受けなので
	// 外から叩かれる前提ではないが、ブラウザーの中で動く他の頁からの当てずっぽうは
	// 経路として残る。短くする理由が無い。
	tokenBytes = 32
)

// newToken は起動のたびに新しいトークンを作る。
//
// crypto/rand を使う。math/rand だと、起動時刻から当てられる形になる。
// 失敗したら待ち受けを始めない。弱いトークンで開くくらいなら開かないほうがよい。
func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// URL に一度だけ載るので、記号が増えない符号化にする。
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// sameToken は2つのトークンが等しいかを返す。
//
// [subtle.ConstantTimeCompare] を使うのは、比較にかかる時間から先頭何文字が
// 合っていたかを読み取られないようにするため。手元の待ち受けでは効きにくい
// 攻撃だが、正しい比較を書かない理由にはならない。
func sameToken(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// sessionCookie は移し替えたあとの Cookie を作る。
//
//   - HttpOnly:  頁の JavaScript から読ませない。読めても使い道は無いが、
//     読めない形にしておけば、将来どんな頁を足しても漏れる経路が増えない。
//   - SameSite=Strict: 他の生成元からの要求に一切載せない。載ると、開いている
//     別の頁から /api/lines を叩けることになる。
//   - Path=/:   経路はすべてこの1つの Cookie で通す。
//
// Secure は付けない。http://127.0.0.1 で話すので、付けると送られてこなくなる。
// 経路が loopback に固定されていること自体が、ここでの守りになっている。
//
// 期限は付けない（セッション Cookie）。待ち受けが終われば、その Cookie で
// 開ける先はもう無い。トークンは起動のたびに変わる。
func sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// allowedHosts は Host ヘッダーとして通す綴りの一覧を作る。
//
// 完全一致でしか通さない。DNS リバインディングへの対策である。攻撃者の頁が
// 自分の名前を 127.0.0.1 に向け直すと、ブラウザーから見れば同一生成元のまま
// この待ち受けへ届く。そのとき Host はその名前になるので、名前を数えあげて
// 弾くのではなく、通す綴りを数えあげる。
//
// ポートを含めるのは、同じ 127.0.0.1 の別のポートで動く何かに向けた要求が
// 流れ込まないようにするため。
func allowedHosts(port string) map[string]struct{} {
	return map[string]struct{}{
		"127.0.0.1:" + port: {},
		"localhost:" + port: {},
		"[::1]:" + port:     {},
	}
}
