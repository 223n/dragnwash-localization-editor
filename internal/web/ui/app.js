"use strict";

/*
  画面側。素の JavaScript だけで書く。枠組みも束ね役も使わない。

  この頁が守ること。

  - 判断を1つも持たない。状態バッジも件数も、待ち受けが渡したものをそのまま描く。
    ここで数え始めると、internal/diff が避けている誤検出を作り直すことになる。
  - 文言を1つも持たない。すべて /api/bootstrap の目録から来る。
  - 行の中身を innerHTML に渡さない。訳には <i> のような字が実際に入っている
    （ゲームの書式）。textContent で入れれば、その字はその字として見える。
  - 取りにいく先は自分自身だけ。外向きの通信はこの頁からも出さない。
  - 題名に行の中身を入れない。題名はブラウザーの履歴に残る。

  編集について守ること。いちばん大事なのは訳を失わないこと。

  - 409（手前でファイルが変わった）で、未保存の編集を捨てない。読み直した内容を
    出したうえで、どちらを載せるかを人に選ばせる。黙って上書きも、黙って破棄もしない。
  - 保存できなかった行は「保存できていない行」として画面に残す。入力は消さない。
  - 未保存のまま頁を閉じようとしたら beforeunload で止める。
  - 読み直しと切り替えの読み込みが返るまで、一覧を編集させない。読めた時点で
    抱えている訳は片付くので、そのあいだに打てると、打った訳が黙って消える。
  - 入力欄は1つだけ作って、いま触っている行へ差し込む。1721行ぶんの入力欄を
    常設すると、開くだけで重くなる。

  絞り込みと検索について守ること。

  - 未保存の訳がある行、保存できなかった行、いま入力欄が開いている行は、条件に
    当たらなくても隠さない。隠すと、直すべき行と触っている行が画面から消え、
    翻訳者は消えたことに気づけない。
  - 検索語を外へ出さない。URL にも待ち受けへの要求にも載せない。行はもう
    ブラウザーの中にあるので、待ち受けに聞く必要がない。聞けば、検索語
    （＝原文の断片）が待ち受けの記録に残りうる。
  - localStorage にも sessionStorage にも、条件も検索語も残さない。ディスクに残る。
  - カテゴリの一覧も名前も、待ち受けが返した件数から取る。画面で定義し直さない。
  - 変換中（compositionstart から compositionend、e.isComposing、keyCode 229）は
    キーを1つも横取りしない。確定の直後（composedGrace）に来た Enter も
    行送りには使わない。ja / ko / zh-Hans / zh-Hant のためにこの入力方式を
    選んでいる。
  - 絞り込みの一帯は貼り付けない。貼り付けると、その高さぶんだけ競合の引き止めの
    ボタンが .top の中で押し出され、押せなくなる。
*/

(function () {
  var ui = null;
  var locales = [];

  var el = {
    title: document.getElementById("app-title"),
    localeLabel: document.getElementById("locale-label"),
    locale: document.getElementById("locale"),
    reload: document.getElementById("reload"),
    /*
      ボタンの文字。ボタンの中にはアイコンも入っているので、文字は隣の span へ
      入れる。ボタンそのものへ textContent で入れるとアイコンが消える。
      条件を外す・競合の2つのボタンも同じ。
    */
    reloadLabel: document.getElementById("reload-label"),
    rows: document.getElementById("rows"),
    saveState: document.getElementById("save-state"),
    /* 一覧の上の案内。アイコンを隣に置いてあるので、文字は中の span に入れる。 */
    noticeText: document.getElementById("edit-notice-text"),
    message: document.getElementById("message"),
    /* 列の見出し。文言は目録から入れる。 */
    colLine: document.getElementById("col-line"),
    colStatus: document.getElementById("col-status"),
    colSpeaker: document.getElementById("col-speaker"),
    colSource: document.getElementById("col-source"),
    colTranslation: document.getElementById("col-translation"),
    path: document.getElementById("file-path"),
    gamePath: document.getElementById("game-path"),
    notes: document.getElementById("notes"),
    /*
      起動したときの状態の一帯そのものと、その見出しに添える字。畳みなので、
      文言が入るまで出さないために持つ（applyCatalog を見よ）。書き込む先は
      今までどおり el.path・el.gamePath・el.notes・el.counts・el.stats のままで、
      畳みにしたことで変わったのは #file-path が summary の中に入ったことだけ。
    */
    panelFold: document.getElementById("panel-fold"),
    panelMore: document.getElementById("panel-more"),
    /*
      書き出し。帯のボタンと、それが開くメニュー、その中の2つのボタン
      （exportCsv を見よ）。結果の欄（exportState）はメニューの外、帯の中にある。
    */
    exportOpen: document.getElementById("export-open"),
    exportTitle: document.getElementById("export-title"),
    exportMenu: document.getElementById("export-menu"),
    exportHelp: document.getElementById("export-help"),
    exportPublished: document.getElementById("export-published"),
    exportPublishedLabel: document.getElementById("export-published-label"),
    exportPublishedNote: document.getElementById("export-published-note"),
    exportWorking: document.getElementById("export-working"),
    exportWorkingLabel: document.getElementById("export-working-label"),
    exportWorkingNote: document.getElementById("export-working-note"),
    exportState: document.getElementById("export-state"),
    countsTitle: document.getElementById("counts-title"),
    counts: document.getElementById("counts"),
    statsTitle: document.getElementById("stats-title"),
    stats: document.getElementById("stats"),
    list: document.getElementById("list"),
    conflict: document.getElementById("conflict"),
    conflictTitle: document.getElementById("conflict-title"),
    conflictHelp: document.getElementById("conflict-help"),
    conflictKeep: document.getElementById("conflict-keep"),
    conflictTake: document.getElementById("conflict-take"),
    conflictKeepLabel: document.getElementById("conflict-keep-label"),
    conflictTakeLabel: document.getElementById("conflict-take-label"),
    /* 貼り付く帯そのもの。高さを測って余白へ渡すために持つ（watchTopHeight）。 */
    top: document.querySelector(".top"),
    /* 絞り込みの一帯そのもの。スラッシュの近道が画面へ送るために持つ。 */
    finder: document.querySelector(".finder"),
    filterLabel: document.getElementById("filter-label"),
    filters: document.getElementById("filters"),
    filterClear: document.getElementById("filter-clear"),
    filterClearLabel: document.getElementById("filter-clear-label"),
    finderTitle: document.getElementById("finder-title"),
    /* 左の列と、その開閉に使うもの（toggleSidebar を見よ）。 */
    sidebar: document.getElementById("sidebar"),
    menu: document.getElementById("menu"),
    sidebarClose: document.getElementById("sidebar-close"),
    backdrop: document.getElementById("backdrop"),
    searchLabel: document.getElementById("search-label"),
    search: document.getElementById("search"),
    finderNote: document.getElementById("finder-note"),
    finderNoteTitle: document.getElementById("finder-note-title"),
    /* 畳みそのもの。文言が入るまで出さないために持つ（applyCatalog を見よ）。 */
    finderFold: document.getElementById("finder-fold"),
    shown: document.getElementById("shown"),
    empty: document.getElementById("empty"),
    keys: document.getElementById("keys"),
    keysTitle: document.getElementById("keys-title"),
    keysFold: document.getElementById("keys-fold"),
    orphans: document.getElementById("orphans"),
    orphansTitle: document.getElementById("orphans-title"),
    orphansList: document.getElementById("orphans-list")
  };

  /*
    state.pending   まだ保存していない訳（行番号 → 値）。
    state.failed    保存できなかった行（行番号 → { reason, value }）。自動保存の
                    対象からだけ外す。書き換えれば pending へ戻る。
                    値まで持つのは、欄の字としてだけ残すと消えるからである。
                    入力欄を開き直す・競合を解く・読み直すのどれでも行は描き
                    直され、そのとき出るのは古い保存値になり、1字打った瞬間に
                    保存できなかった訳が上書きされる。shownValue がここを見る。
    state.mine      競合のあいだ抱えている自分の編集。選ばせるまで捨てない。
    state.orphans   載せる先の行がファイルから無くなった訳。捨てずに画面へ出す。
    state.rows      行番号 → 描いた要素。保存の結果を差し込むために持つ。
    state.byKey     キー → そのキーの行（描いた要素の並び）。待ち受けはバッジを
                    キー単位で付け直すので、描き直す先もキーで引く。
    state.items     描いた順そのまま（見出しと行）。絞り込みは この並びを
                    上から1回なぞるだけで済ませる。
    state.filter    選ばれている絞り込みの条件。"cat:<識別子>" が待ち受けから
                    来たカテゴリ、"state:pending" と "state:failed" が画面の状態。
                    どれかに当たれば出す（複数選べる）。
    state.gen       読み直しとロケール切り替えのたびに進める番号。送りかけの
                    保存の応答が「もう画面のものではない」と分かるようにする。
    state.saveError 要求そのものが落ちているか（届かない、404、503）。行ごとの
                    理由（state.failed）とは別に持つ。
    state.inflight  いま送っている訳（行番号 → 値）。送っていなければ null。
                    返るまでのあいだ、その行の「ファイルの値」は entry.saved では
                    なくこちらになる見込みなので、onInput が未保存かどうかを決める
                    ときに見る。
    state.sending   いま送っている保存の終わり（Promise）。読み直しと切り替えが、
                    送り終えるのを待ってから尋ねるために持つ（settle を見よ）。
    state.asking    読み直しと切り替えを押した回数。送り終えるのを待っている
                    あいだにもう一度押されたら、前に押したほうは何もしない。
    state.discarding 捨てると答えた読み直し（切り替え）の最中の印。立っている
                    あいだは保存を送らない（flush を見よ）。読めたら捨て、読めなければ
                    倒して送り直しへ戻す（load を見よ）。
    state.loading   最後に始めた読み込みの札（{ locale: 読みにいったロケール }）。
                    読んでいなければ null。応答は、札がこれと同じときだけ描く
                    （load を見よ）。ロケールの欄もこれを見てそろえる（syncLocale）。
                    立っているあいだは一覧を編集させない（openEditor と syncBusy）。
  */
  var state = {
    locale: "",
    version: "",
    canEdit: false,
    autosaveDelay: 1500,
    data: null,
    pending: new Map(),
    failed: new Map(),
    mine: null,
    orphans: [],
    rows: new Map(),
    byKey: new Map(),
    items: [],
    filter: new Set(),
    /*
      条件のチップ。カテゴリ識別子 → { rows: 数の欄, chip: チップそのもの }。
      保存のあとに、条件を組み直さずに数と添え書きだけ書き換えるために持つ
      （updateChipRows を見よ）。
    */
    chipRows: new Map(),
    gen: 0,
    editing: null,
    composing: false,
    /*
      変換が確定した時刻（performance.now）。確定の Enter を行送りに使わないために
      控える。測り方は composedGrace に書いてある。

      始めは -Infinity にしておく。performance.now は頁を開いた瞬間を 0 に数えるので、
      0 で始めると、開いてすぐの Enter まで確定の直後に見える。
    */
    composedAt: -Infinity,
    searchComposing: false,
    searchTimer: null,
    saving: false,
    saveError: false,
    inflight: null,
    sending: null,
    asking: 0,
    discarding: null,
    loading: null,
    /* 書き出しの最中かどうか。2つのボタンを二重に押させないために持つ。 */
    exporting: false,
    timer: null,
    retry: 0,
    /*
      貼り付く帯の高さの見張り（ResizeObserver）。参照を残すためだけに持つ。
      new した先を捨てると、1度も呼ばれないまま回収されることがある
      （watchTopHeight を見よ）。
    */
    topWatch: null
  };

  /*
    保存に失敗したあと、もう一度送るまでの待ち時間。

    Windows では、ゲームがホットリロードでファイルを開いている最中の rename が
    共有違反で失敗する。実測で、読み手がいる状態の連続保存は数パーセント落ちた
    （待ち受け側も短い間隔で3回まで試すが、それでも落ちる）。待てば直る種類の
    失敗なので、画面からももう一度送る。

    諦めない。決めた回数を使い切ったあとは、最後の間隔のまま送り続ける。
    諦めると、原因（ゲームがファイルを開いている）が消えたあとも、その訳は
    二度と送られない。翻訳者が別の行を触るまで、訳はブラウザーの中だけに残る。
    送り先は自分自身なので、間隔さえ広げれば送り続けても重くない。
  */
  var retryDelays = [500, 1000, 2000, 5000, 15000, 30000];

  /*
    検索の字を打ってから、絞り込みを走らせるまでの待ち。

    実測（データ行 1721、見出し 202、Windows の Chromium）。1回のなぞり直しは
    条件の当てはめだけなら 0.2〜17ms で終わる。重いのはそのあとの描き直しで、
    出す行が大きく入れ替わるとき（1字消して全行に戻るときなど）は 100〜173ms
    かかった。打鍵ごとに走らせると、この描き直しが打つ手に追いつかない。

    だから、間引く。待ちを 120ms にしてあるのは、打ち終わりから結果までを
    ひと呼吸に収めるためである。1回ぶんの重さは変わらないが、走る回数が
    「打った字の数」から「打ち終わった数」に減る。

    自動保存の 1.5 秒とは別物である。あちらはファイルへ書くまでの待ちで、
    こちらは画面を描き直すまでの待ちである。長くすると、打ったのに画面が
    変わらない時間ができる。
  */
  var searchDelay = 120;

  /*
    変換が確定してから、Enter を行送りに使わないでおく長さ。

    見張りが3つ（state.composing / e.isComposing / keyCode 229）あっても、
    compositionend が確定の keydown より先に届く並びだと全部すり抜ける。
    そのときの Enter は state.composing も e.isComposing も false、keyCode は
    13 で、素の Enter と区別が付かない。翻訳者から見ると、変換を確定した
    だけで行が飛ぶ。ja / ko / zh-Hans / zh-Hant のためにこの入力方式を
    選んでいるので、ここは崩せない。

    だから時刻で見分ける。確定とその Enter は同じ1打鍵から出るので、間は
    ほぼ0である。実測は 0.0〜0.2ms（Windows の Chromium、5回）。これは投げた
    事象で測った値で、実機の IME での間隔は測っていない。100ms にしてあるのは、
    事象の並びがブラウザーや IME で違っても収まる幅を取るためである。

    代わりに「確定した直後に、もう一度 Enter を押して次の行へ行く」操作は
    100ms 待たされる。行が勝手に飛ぶほうが重い事故なので、こちらを選ぶ。

    この値で実機が通ることは確かめてある（Windows の実機の IME、dwloc 0.4.1、
    2026-09-17）。変換の確定で行が飛ばず、そのあとの Enter で次の行へ進んだ。
    短くするなら、実機の IME で間隔を測ってからにすること。

    間は performance.now で測る。Date.now（壁時計）で測ると、確定と Enter の
    あいだに OS の時計が後ろへ動いたとき（NTP の段差、休止からの復帰）、差が
    負のまま 100 を超えるまで Enter の行送りが効かなくなる。時計を1時間戻せば、
    1時間効かない。performance.now は後ろへ戻らない。
  */
  var composedGrace = 100;

  /*
    文言を引く。置換は {name} の1形式だけ。複数形の規則は持ち込まない。
    Go 側の expand と同じ規則にしてある。
  */
  function t(key, params) {
    var text = ui && ui.messages ? ui.messages[key] : null;
    if (!text) {
      return key;
    }
    if (!params) {
      return text;
    }
    return text.replace(/\{(\w+)\}/g, function (whole, name) {
      return Object.prototype.hasOwnProperty.call(params, name)
        ? String(params[name])
        : whole;
    });
  }

  function clear(node) {
    node.replaceChildren();
  }

  function span(className, text) {
    var e = document.createElement("span");
    if (className) {
      e.className = className;
    }
    e.textContent = text === undefined || text === null ? "" : String(text);
    return e;
  }

  /*
    アイコン。index.html の先頭に埋めた図形（<symbol id="i-…">）を指す。

    svg は createElementNS で作る。createElement で作ると HTML の要素になり、
    描かれない。名前空間は、頁に埋めてある図形の svg から読む。文字列で書くと
    その中に URL が入り、資産に URL を1つも置かないという試験と食い違う
    （TestAssetsHaveNoExternalReference）。

    aria-hidden を付けるのは、隣の文字が名前になるからである。アイコンだけの
    要素には、呼ぶ側が aria-label を付けること。
  */
  var svgNS = document.querySelector("svg").namespaceURI;

  function icon(name) {
    var svg = document.createElementNS(svgNS, "svg");
    svg.setAttribute("class", "icon");
    svg.setAttribute("aria-hidden", "true");
    var use = document.createElementNS(svgNS, "use");
    use.setAttribute("href", "#i-" + name);
    svg.appendChild(use);
    return svg;
  }

  /*
    畳みの見出し（summary）を組む。アイコン、文言、開閉を示す山形の順。
    summary そのものが id を持つ（TestNoticesStartFolded が見る）ので、文字だけを
    差し替えることはできず、中身ごと組み直す。
  */
  function foldTitle(summary, name, text) {
    var chev = icon("chevron-down");
    chev.setAttribute("class", "icon chev");
    summary.replaceChildren(icon(name), span(null, text), chev);
  }

  function li(className, text) {
    var e = document.createElement("li");
    if (className) {
      e.className = className;
    }
    e.textContent = text;
    return e;
  }

  /*
    貼り付く帯（.top）の高さを測って、CSS 変数 --top-height に入れる。

    これは配置の測定であって、行やカテゴリの判定ではない。「画面に新しい判断を
    置かない」という決めごとが禁じているのは、待ち受けが決めたカテゴリや状態を
    画面が導き出すことである。要素が画面上で何ピクセルを占めているかは、
    待ち受けが知りようのない、ブラウザーしか持っていない値なので、ここで測る
    ほかにない。internal/diff の判定は1つも触らない。

    なぜ測るか。焦点の入る要素の余白（scroll-margin-top）を 8em（112px）の
    固定値にしていたが、.top は競合と行き先の無い訳が出ると max-height: 50vh
    まで伸びる。実測（800x600、Chromium）では、行き先の無い訳が1件出ている
    だけで .top は 150.6px、競合と両方出ると 300px（50vh そのもの）になった。
    幅320pxでは、何も出ていなくても 121.4px ある。どれも 112px より高い。

    112px のまま同じ場面を踏むと、こうなった（800x600、競合＋行き先の無い訳、
    帯の下端は 300px）。

      Shift+Tab   開いた入力欄の上端 238.4px。elementFromPoint は入力欄では
                  なく #orphans（帯の中身）を返した。
      スラッシュ  絞り込みの一帯の上端 111.8px、検索の欄の上端 223.7px。
                  elementFromPoint は #conflict を返した。

    貼り付けるのをやめた代償をスラッシュで補う設計なのに、いちばん補って
    ほしい競合中に補えていなかった。測った値にすると、同じ場面で入力欄の上端は
    308.4px、検索の欄は 419.7px になり、elementFromPoint はどちらもその要素を
    返す。

    測った値が反映されるのは、ブラウザーが画面を描き直す段である。描き直しが
    止まっている頁（背面のタブなど）では更新されないが、そこでは
    scrollIntoView も起きないので困らない。

    ResizeObserver が無い環境では変数が入らない。そのときは CSS の var() の
    第2引数（既定値の 10em＝140px）がそのまま効く。既定値は、競合も行き先の無い
    訳も出ていないときの、いちばん狭い幅（320px）の帯（121.4px）を覆う高さに
    してある。競合や行き先の無い訳が出て帯が伸びたぶんは覆えない。
  */
  function watchTopHeight() {
    /*
      測る前に見張りの有無を確かめる。測ってから確かめていたころは、
      ResizeObserver が無い環境でも、目録が届く前（ロケールの一覧も文言も空）の
      低い帯を1度だけ測った値が残り、既定値が効かなかった（実測: 375x812 で
      --top-height は 63px、帯は 117.8px）。
    */
    if (!el.top || typeof ResizeObserver !== "function") {
      return;
    }
    var apply = function () {
      /*
        border-box の高さをそのまま渡す。max-height: 50vh で頭打ちになった値が
        返るので、帯が伸びきっているときも実際に見えている高さになる。
      */
      var h = el.top.getBoundingClientRect().height;
      document.documentElement.style.setProperty("--top-height", h + "px");
    };
    apply();
    /*
      帯の高さは、競合の引き止めや行き先の無い訳が出入りしたときだけでなく、
      窓の幅が変わって中身が折り返したときにも変わる。どちらも ResizeObserver
      が拾う。窓の高さが変わったときの 50vh も、帯自身の高さが変わるので拾える。

      作った見張りは state に持たせて、参照を残す。new した先を捨てると、
      1度も呼ばれないまま回収されることがある（実際に起きた。窓を 0x0 から
      800x600 へ変えても --top-height は 0px のままで、目録が届いてロケールの
      一覧が入り、帯が 48px から 49.4px に伸びたときも動かなかった）。
    */
    state.topWatch = new ResizeObserver(apply);
    state.topWatch.observe(el.top);
  }

  /*
    失敗の理由を出す。空なら中身を空にするだけで、hidden は触らない。

    hidden を出し入れしていた。hidden の要素は支援技術の木から外れるので、
    文字が変わる瞬間に読み上げの見張り（role="status"）が木に居らず、
    告知しない実装があり得る。中身の入れ替えなら、要素はずっと木に居る。
    空のときに見えなくなるのは app.css の .notice:empty が受け持つ
    （余白と下線を落とすと、中身の無いブロックは高さ0になる）。
  */
  function showMessage(text) {
    el.message.textContent = text ? text : "";
  }

  /* 取りにいく先は同じ生成元だけ。相対のパスしか書かない。 */
  function getJSON(path) {
    return fetch(path, {
      credentials: "same-origin",
      headers: { Accept: "application/json" }
    }).then(function (res) {
      if (!res.ok) {
        throw new Error(String(res.status));
      }
      return res.json();
    });
  }

  /*
    保存の要求。Content-Type: application/json を必ず付ける。待ち受けはこれと
    Origin の2つを要求するので、素のフォーム送信では届かない（form が送れる
    Content-Type は3種あるが、そこに application/json は無い）。

    状態コードで投げ分けない。409 も 422 も本文に理由が入っているので、
    呼び出し側が本文ごと受け取って扱う。
  */
  function postJSON(path, body) {
    return fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json"
      },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().then(
        function (data) {
          return { status: res.status, body: data };
        },
        function () {
          /* 本文が JSON でない（404 の平文など）。状態コードだけ返す。 */
          return { status: res.status, body: null };
        }
      );
    });
  }

  function applyCatalog() {
    document.documentElement.lang = ui.lang;
    document.documentElement.dir = ui.dir;
    document.title = t("app.title");
    el.title.textContent = t("app.title");
    el.localeLabel.textContent = t("ui.locale");
    el.reloadLabel.textContent = t("ui.reload");
    /*
      左の列の開閉ボタンと閉じるボタンはアイコンだけなので、名前を属性で入れる。
      列の見出しと列名も目録から。
    */
    el.menu.setAttribute("aria-label", t("ui.sidebar"));
    el.sidebarClose.setAttribute("aria-label", t("ui.sidebar_close"));
    el.finderTitle.textContent = t("ui.finder_title");
    el.colLine.textContent = t("ui.col_line");
    el.colStatus.textContent = t("ui.col_status");
    el.colSpeaker.textContent = t("ui.col_speaker");
    el.colSource.textContent = t("ui.col_source");
    el.colTranslation.textContent = t("ui.edit_label");
    el.countsTitle.textContent = t("ui.counts");
    el.statsTitle.textContent = t("ui.stats");
    el.noticeText.textContent = t("ui.edit_notice");
    el.conflictTitle.textContent = t("ui.conflict_title");
    el.conflictHelp.textContent = t("ui.conflict_help");
    el.conflictKeepLabel.textContent = t("ui.conflict_keep_mine");
    el.conflictTakeLabel.textContent = t("ui.conflict_take_file");
    el.orphansTitle.textContent = t("ui.orphans_title");
    el.filterLabel.textContent = t("ui.filter");
    el.filterClearLabel.textContent = t("ui.filter_clear");
    el.searchLabel.textContent = t("ui.search");
    /* 入力欄の中の案内も目録から。画面に文字列を直接書かない。 */
    el.search.setAttribute("placeholder", t("ui.search_placeholder"));
    el.search.setAttribute("aria-label", t("ui.search"));
    el.finderNote.textContent = t("ui.finder_note");
    /*
      畳んである3つの見出し。畳んだままでも読まれるのはここだけなので、
      絞り込みのほうには「無条件に本当のこと」（検索がブラウザーの中だけで
      完結すること）を入れてある。条件つきでしか本当でないことを入れると、
      いちばん読まれる1文がいちばん当てにならない1文になる。

      一帯（#panel-fold）の見出しは文ではなく値である。いま読み書きしている
      ファイルの名前（#file-path）が summary の中に入っていて、ここで入れる
      のは、その後ろに添える「中に何が入っているか」だけ。畳んだままでも
      ファイル名が読めることが、この畳みの条件だった。
    */
    foldTitle(el.finderNoteTitle, "circle-info", t("ui.finder_note_title"));
    foldTitle(el.keysTitle, "keyboard", t("ui.keys_title"));
    el.exportTitle.textContent = t("ui.export_title");
    el.exportHelp.textContent = t("ui.export_help");
    el.exportPublishedLabel.textContent = t("ui.export_published");
    el.exportPublishedNote.textContent = t("ui.export_published_note");
    el.exportWorkingLabel.textContent = t("ui.export_working");
    el.exportWorkingNote.textContent = t("ui.export_working_note");
    el.panelMore.textContent = t("ui.panel_more");
    /*
      文言が入ったので出す。入るまでは hidden にしてある（index.html）。
      空の summary と空のボタンは、名前を持たない焦点の止まり場になる。

      一帯（#panel-fold）はここでは出さない。あの summary の主役は目録の文では
      なく、いま読み書きしているファイルの名前で、それが入るのは /api/lines を
      受けた render である。出すのもあちらに置いてある。
    */
    el.keysFold.hidden = false;
    el.finderFold.hidden = false;
    el.exportOpen.hidden = false;
    /*
      「1行もありません」はここでは入れない。applyView が、出す行が0のときだけ
      入れて、そうでないときは空にする。中身の入れ替えで出し入れするので、
      ここで入れてしまうと読み込み中からずっと出たままになる。
    */
    el.keys.textContent = t("ui.keys_help");
  }

  /*
    作業コピーを探しているゲームのフォルダーを画面に出す。--game が無ければ隠す。

    「読んでいます」とは書かない。ロケールによっては、そこに作業コピーが無くて
    1バイトも読まないためである。実際に読んだファイルは「ファイル」の欄に出る。

    ロケールを切り替えても消さない。探し先は待ち受けを起動したときに決まっていて、
    画面の操作では変わらないためである。
  */
  function showGamePath(game) {
    if (!game) {
      el.gamePath.hidden = true;
      el.gamePath.textContent = "";
      return;
    }
    el.gamePath.textContent = t("ui.game_folder") + ": " + game;
    el.gamePath.hidden = false;
  }

  function fillLocales(selected) {
    clear(el.locale);
    if (!selected) {
      var placeholder = document.createElement("option");
      placeholder.value = "";
      placeholder.textContent = t("ui.select_locale");
      el.locale.appendChild(placeholder);
    }
    locales.forEach(function (name) {
      var option = document.createElement("option");
      /* ロケール名はデータ側の語彙なので、そのまま出す。訳さない。 */
      option.value = name;
      option.textContent = name;
      if (name === selected) {
        option.selected = true;
      }
      el.locale.appendChild(option);
    });
  }

  function renderNotes(notes) {
    clear(el.notes);
    (notes || []).forEach(function (note) {
      el.notes.appendChild(li(null, note));
    });
  }

  /*
    件数。判定できていないカテゴリは数を出さず、待ち受けが付けた理由を出す。
    0 件と書くと「もう何も残っていない」と読まれる。

    保存のあとに来る件数も、待ち受けが数え直したものである。ここでは足しも引きもしない。
  */
  function renderCounts(counts) {
    clear(el.counts);
    (counts || []).forEach(function (c) {
      var item = document.createElement("li");
      item.className = c.status + (c.judged ? "" : " held");
      item.appendChild(span(null, c.statusLabel + " / " + c.label + " "));
      item.appendChild(span(null, countText(c)));
      el.counts.appendChild(item);
    });
  }

  /*
    件数の欄に出す1つぶんの文。

    判定できていないカテゴリは数を出さず、理由を出す。

    件数（diff が見つけた数）と、この一覧に並べられる行数が食い違うカテゴリでは、
    その場で両方を言う。言わないと、同じカテゴリ名の隣に別の数が2か所（件数の欄と
    条件のチップ）出たまま、どちらが何なのかは畳んだ断り書きの中にしか無くなる。
    食い違っているかどうかを決めたのは待ち受けである（countView.rowsDiffer）。
    ここで c.count と c.rows を比べると、それが画面の独自判断になる。
  */
  function countText(c) {
    if (!c.judged) {
      return t("ui.not_judged", { reason: c.reason });
    }
    if (c.rowsDiffer) {
      return t("ui.count_and_rows", { count: c.count, rows: c.rows });
    }
    return t("ui.count_value", { count: c.count });
  }

  /*
    絞り込みの条件を組む。

    カテゴリの一覧も名前も、待ち受けが返した件数（countView）から取る。画面で
    カテゴリを定義し直さない。internal/diff にカテゴリが増えたら、ここも黙って
    増える。並びも待ち受けの並び（要作業 → 要確認 → 参考）のままにする。

    「未保存」と「保存できない」の2つだけは、待ち受けが知らない画面の状態
    なので目録の文言で足す。この2つは絞り込みの的でもあり、同時に
    「条件に当たらなくても隠さない行」でもある（applyView を見よ）。

    判定できていないカテゴリも条件として出す。選んでも1行も出ないが、その
    理由は件数の欄が「判定していません（…）」と言っている。ここで
    「行が無いから外す」と決めると、それが画面の独自判断になる。

    数（countView.rows）は待ち受けが数えたもので、そのまま添える。押す前に、
    その条件の行がこの一覧に何行あるかを読めるようにするためである。実データ
    （ja、公開ファイル）では9つのうち7つが押しても0行で、どれがそうなのかは
    押すまで分からなかった。

    この数は「押したときに並ぶ行数」ではない。3つの点でずれる。どれも実機で
    測ってある（実データの写し、ja）。

      保存 … 訳を1行入れると、チップは「32 行」→「31 行」になるが、一覧は
              組み直さないので 32 行のまま並び続ける。27 行まで訳し進めると、
              チップ「未翻訳（0 行）」の下に 27 行が並ぶ。行のバッジはその場で
              消えるので、並んでいるのが「もう当たらない行」だとは読める。
              組み直さないのは、打っている最中に行が目の前から消えないように
              するためである（applyView の注記）。
      keepAlways … 入力欄が開いている行・未保存・保存できない・競合の行は
              条件に当たらなくても隠さない。入力欄を1つ開いたまま「台本に無い
              台詞行（17 行）」を押すと 18 行出た（18行目にそのバッジは無い）。
      検索 … 検索語は数に入っていない。「未翻訳（27 行）」のまま kobold と
              打つと 4 行になった。

    どの数がいま画面に出ている行数かを言うのは、上の帯の「表示中 N 行」だけに
    する。ずれる3つは断り書き（目録の ui.finder_note）に書いてある。数が何の数
    なのかは、畳めない見出し（目録の ui.filter）のほうに置いてある。畳みの中に
    置くと、既定では読めない。
  */
  function buildFilters(counts) {
    clear(el.filters);
    /*
      数を入れた欄を控えておく。保存のあとは、条件を組み直さずにここだけ
      書き換える（updateChipRows）。組み直すと、触っている最中の選択が跳ねる。
    */
    state.chipRows = new Map();
    (counts || []).forEach(function (c) {
      var rows = span("chip-rows", chipRowsText(c));
      /*
        件数の欄の同じ行を、そのまま添え書き（title）にする。件数（件）と
        チップの数（行）は別のものを数えた別の数で、画面の離れた2か所に出る。
        375px 幅では、条件の一帯から件数の欄まで数百px離れている。押す手の下で、
        もう一方の数も読めるようにしておく。判定できていないカテゴリでは、その
        理由（作業コピーがありません、など）がここに出る。押したあとに「条件に
        合う行がありません」としか出ないのでは、なぜ出ないのかが押した場所の
        どこにも無い。文は countText が組んだもので、新しい判断はしていない。
      */
      var chip = filterChip("cat:" + c.category, c.label, c.status, c.statusLabel, rows, countText(c));
      state.chipRows.set(c.category, { rows: rows, chip: chip });
      el.filters.appendChild(chip);
    });
    /*
      画面の状態の2つ。重さの名前は待ち受けが持っていないので、目録から入れる。
      internal/diff の重さ（要作業／要確認／参考）とは別のものなので、同じ名前を
      当てずに「画面の状態」と呼ぶ。

      この2つには数を添えない。数えるのは待ち受けの仕事だが、未保存と保存できない
      は待ち受けが知らない画面の状態なので、添えるなら画面が数えることになる。
      量は上の帯が既に出している（「未保存 N 件」）ので、二重に持たせない。
    */
    el.filters.appendChild(
      filterChip("state:pending", t("ui.filter_pending"), "pending", t("ui.filter_state"), null, "")
    );
    el.filters.appendChild(
      filterChip("state:failed", t("ui.filter_failed"), "failed", t("ui.filter_state"), null, "")
    );
  }

  /*
    チップに添える数の文。

    判定できていないカテゴリには数を出さず、「未判定」と書く。0 と書くと
    「もう何も残っていない」と読まれる（internal/diff の doc.go と buildCounts が
    名指ししている約束）。実データの16ロケールでは、作業コピーが無いあいだ
    「未翻訳」がこれになる。ここで「（0 行）」と出すと、訳が1行も入っていない
    ロケールのチップが「訳し終わった」と読める。

    件数が立っているのに、この一覧には1行も出せないカテゴリにも数を出さない
    （countView.noRowHere）。0 行なのは本当だが、「（0 行）」と書くと上と同じ
    読み違いをされる。実データで公開ファイルを並べているロケールでは、「どの
    ロケールにも訳が無い行」が 32件／0行 でこれになる。行として出せないのは、
    そのキーがこのロケールのファイルに無いからこそ見つかったカテゴリだからで、
    「片付いた」の逆である。件数の欄はこの食い違いを「32 件（この一覧には 0 行）」
    と書くが、そこは離れた場所にある。押す前に、押した手の下で読めるようにする。

    どちらを書くかは待ち受けが決めている（judged と noRowHere）。ここで
    c.count と c.rows を比べると、それが画面の独自判断になる。

    単位を「行」にして、件数の欄の「件」と語を分ける。同じ数の言い換えではなく、
    別のものを数えた別の数だからである。
  */
  function chipRowsText(c) {
    if (!c.judged) {
      return t("ui.chip_not_judged");
    }
    if (c.noRowHere) {
      return t("ui.chip_no_row_here");
    }
    return t("ui.chip_rows", { count: c.rows });
  }

  /*
    チップの数と添え書きを書き換える。保存の応答から呼ぶ。

    条件そのものは組み直さない。組み直すと、触っている最中の選択が跳ねる
    （render の注記を見よ）。数を置いていくと、訳を1行入れた直後のチップが
    「この一覧にあるその条件の行数」ではなくなる。

    添え書き（件数の欄の同じ行）も一緒に書き換える。片方だけ写すと、押した
    ときに出る2つの数が別の時点のものになる。

    ここで数えてはいない。待ち受けが数え直した countView.rows を写すだけである。
  */
  function updateChipRows(counts) {
    (counts || []).forEach(function (c) {
      var node = state.chipRows.get(c.category);
      if (node) {
        node.rows.textContent = chipRowsText(c);
        node.chip.title = countText(c);
      }
    });
  }

  /*
    条件1つ。選ばれているかは state.filter が持ち、組み直しても残る。

    重さの名前（要作業／要確認／参考）も添える。縁と文字の色だけで重さを伝えて
    いたが、以前の --todo #8a4b00 と --review #9a1c1c は色覚によっては近く、app.css の
    冒頭に書いた「色だけで意味を伝えない」に反していた。件数の欄は最初から
    名前を出している。名前は待ち受けが返したものをそのまま使う。

    rows は数を入れた欄。画面の状態の2つには添えないので、null が来る。
    note は添え書き（件数の欄の同じ行）。画面の状態の2つには無いので空が来る。
  */
  function filterChip(id, label, status, statusLabel, rows, note) {
    var wrap = document.createElement("label");
    wrap.className = "chip " + status;
    if (note) {
      /* 文は待ち受けが返した件数から組んだもの。行の中身ではない。 */
      wrap.title = note;
    }
    var box = document.createElement("input");
    box.type = "checkbox";
    box.checked = state.filter.has(id);
    box.addEventListener("change", function () {
      if (box.checked) {
        state.filter.add(id);
      } else {
        state.filter.delete(id);
      }
      applyView();
    });
    wrap.appendChild(box);
    if (statusLabel) {
      wrap.appendChild(span("chip-status", statusLabel));
    }
    wrap.appendChild(span(null, label));
    if (rows) {
      wrap.appendChild(rows);
    }
    return wrap;
  }

  function renderStats(stats) {
    clear(el.stats);
    (stats || []).forEach(function (s) {
      el.stats.appendChild(li(null, s.label + ": " + s.value));
    });
  }

  function badgeNode(badge) {
    var e = span("badge " + badge.status, badge.label);
    if (badge.note) {
      /* 理由は待ち受けが付けたもの。行の中身ではない。 */
      e.title = badge.note;
    }
    return e;
  }

  /*
    バッジを描く。ついでに、その行に当たっているカテゴリを控える。

    控えるのは絞り込みの当て先にするためで、画面で決めているわけではない。
    バッジと同じものを、同じ順で、同じ数だけ持つ。
  */
  function renderBadges(entry, badges) {
    clear(entry.badges);
    entry.cats = new Set();
    (badges || []).forEach(function (b) {
      entry.cats.add(b.category);
      entry.badges.appendChild(badgeNode(b));
    });
  }

  /*
    保存できた行のバッジを描き直す。同じキーの行がほかにもあれば、そちらも
    描き直す。

    待ち受けはキー単位でバッジを付け直す（internal/web の badgesByKey）。訳が
    入ったキーのバッジは、そのキーのどの行からも落ちる。応答に入っている行だけを
    描き直すと、もう一方の行が古いバッジを付けたまま残り、チップの数（待ち受けが
    数え直したもの）だけが2つぶん減る。そのバッジでしか選べない条件を押すと、
    数に入っていない行が一覧に混ざる。

    実データの16ロケールと ja の作業コピーに重複キーは1件も無いが、作業コピーは
    Mod が書き出す CSV で、dwloc validate も公開ファイルしか見ない。実際に
    1174行目を複製して試すと、チップ31に対して32行出た。
  */
  function renderBadgesForKey(entry, badges) {
    var same = entry.key ? state.byKey.get(entry.key) : null;
    if (!same) {
      renderBadges(entry, badges);
      return;
    }
    same.forEach(function (e) {
      renderBadges(e, badges);
    });
  }

  /*
    入力欄。頁に1つだけ作り、いま触っている行へ差し込む。

    textarea にしてあるのは、欄の幅に収まらない訳を折り返して全部見せるため
    である。input だと1行へ流れ、打っている場所の前後しか見えない。出ている
    側の欄（.value）は white-space: pre-wrap で折り返しているので、input の
    ままだと、打ち始めた瞬間に見え方まで変わることになる。

    値が複数行になる道は開けていない。Enter は行送りに使っており（keydown で
    preventDefault）、貼り付けも改行を空白へ置き換える（sanitize）。textarea に
    したのは折り返しのためだけで、internal/edit が CR / LF を含む値を拒む
    という前提はそのままである。

    高さは中身に合わせて伸ばす（fitEditor）。rows は伸ばす前の下限にあたる。
  */
  var editor = document.createElement("textarea");
  editor.rows = 1;
  editor.className = "cell translation editor notranslate";
  /*
    綴り検査を切る。ブラウザーの綴り検査は、内蔵翻訳と同じく入力の中身を
    外部のサービスへ送りうる経路である。原文と訳を外へ出さないという約束は、
    この頁が出す通信だけでは守りきれない。
    autocorrect / autocapitalize / autocomplete も、同じ理由と、訳を勝手に
    書き換えさせないために切る。
  */
  editor.setAttribute("spellcheck", "false");
  editor.setAttribute("autocorrect", "off");
  editor.setAttribute("autocapitalize", "off");
  editor.setAttribute("autocomplete", "off");
  editor.setAttribute("translate", "no");

  /*
    入力欄の高さを中身に合わせる。

    textarea は rows で決まった高さのままなので、折り返して増えたぶんは自分で
    伸ばすしかない。いったん auto へ戻してから scrollHeight を測るのは、戻さ
    ないと前の高さが下限として残り、字を消しても縮まないためである。

    枠線のぶんを足すのを忘れないこと。この頁は * { box-sizing: border-box } な
    ので、height に入れた値は枠線を含んだ高さになる。一方 scrollHeight は枠線を
    含まない。そのまま入れると内側が枠線のぶんだけ足りず、折り返した訳に2pxの
    送りの棒が出る（実際に出た。clientHeight 92 に対して scrollHeight 94）。

    頁に差し込んだあとでしか測れない。幅が決まっていなければ、どこで折り返すかも
    決まらない。
  */
  function fitEditor() {
    if (!editor.parentNode) {
      return;
    }
    editor.style.height = "auto";
    var style = window.getComputedStyle(editor);
    var border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
    editor.style.height = editor.scrollHeight + (border || 0) + "px";
  }

  /*
    窓の幅が変わったら測り直す。折り返す位置が変わるので、開いたままにして
    おくと高さが合わなくなる。合わないぶんは overflow-y: auto が引き受けるが、
    1行の欄に送りの棒が出るのは見た目がよくない。
  */
  window.addEventListener("resize", fitEditor);

  /*
    訳に入れられない字を落とす。

    改行は空白へ置き換える。internal/edit は CR / LF を含む値を拒む（公開ファイルの
    読み手が1物理行=1レコードで読むため）ので、拒まれる値を送らない。入ってくるのは
    ほとんど貼り付けなので、落とすのではなく空白にしてその行に収める。
    NUL も internal/edit が拒む値なので、同じく落とす。
  */
  function sanitize(value) {
    return String(value).replace(/[\r\n]+/g, " ").replace(/\u0000/g, "");
  }

  /*
    いま欄に出す値。未保存があればそれ、保存できなかった値があればそれ、
    どちらも無ければ保存済みの値。

    保存できなかった値をここで見るのが要点である。見ないと、その行を開き直した
    ときに入力欄へ入るのは古い保存値になり、1字打った瞬間に、保存できなかった
    訳が黙って上書きされる。競合を解いたときと読み直したときの描き直しも同じ。
  */
  function shownValue(n, saved) {
    if (state.pending.has(n)) {
      return state.pending.get(n);
    }
    var bad = state.failed.get(n);
    if (bad) {
      return bad.value;
    }
    return saved;
  }

  /*
    保存できなかった行を控える。理由だけでなく値も持つ。

    値は、いま未保存として抱えているものを採る。無ければ送った値に戻す。
    どちらも無いときだけ保存値のままになる。
  */
  function markFailed(line, reason, fallback) {
    var value = state.pending.has(line) ? state.pending.get(line) : fallback;
    state.failed.set(line, {
      reason: reason ? reason : "",
      value: value === undefined || value === null ? "" : value
    });
    state.pending.delete(line);
  }

  /*
    欄に出す字を入れ、同じものを小文字で控える。

    控えるのは検索のためである。打つたびに1943行ぶんの toLowerCase を
    走らせると、字を打つ手が引っかかる。訳は書き換わるたびにここを通るので、
    控えは常に画面と同じになる。
  */
  function setShownText(entry, text) {
    var value = text === undefined || text === null ? "" : String(text);
    entry.value.textContent = value;
    entry.low = value.toLowerCase();
  }

  /*
    この行は、条件に当たらなくても必ず出す行か。

    未保存の訳がある行、保存できなかった行、競合で抱えている行、そしていま
    入力欄が開いている行の4つ。隠すと、直すべき行が画面から消え、翻訳者は
    消えたことに気づけない。訳を失わないという約束は、絞り込みより重い。

    4つ目（state.editing）を足したのは、検索の待ちと入力欄の開きが重なると
    訳が落ちたためである。検索欄に打ってから 120ms 以内に行の訳欄をクリック
    すると、入力欄が開いた直後に applyView が走り、まだ1字も打っていない
    （＝未保存でない）その行を閉じて隠した。焦点は body へ落ち、以後打った字は
    どこにも入らないのに、保存の欄は「保存済み」のままだった（実際に起きた）。
    触っている行は、条件に合わなくても隠さない。
  */
  function keepAlways(n) {
    if (state.pending.has(n) || state.failed.has(n)) {
      return true;
    }
    if (state.editing === n) {
      return true;
    }
    return Boolean(state.mine && state.mine.has(n));
  }

  /* 選ばれている条件のどれかに当たるか。1つも選ばれていなければ全部出す。 */
  function matchesFilter(entry) {
    if (state.filter.size === 0) {
      return true;
    }
    if (state.filter.has("state:pending") && state.pending.has(entry.line)) {
      return true;
    }
    if (state.filter.has("state:failed") && state.failed.has(entry.line)) {
      return true;
    }
    var hit = false;
    entry.cats.forEach(function (id) {
      if (state.filter.has("cat:" + id)) {
        hit = true;
      }
    });
    return hit;
  }

  /*
    打った字を含むか。当てるのは speaker / 原文 / 訳 / キー。

    entry.hay は書き換わらないぶん（キー・speaker・原文・編集できない行の生の
    テキスト）を小文字で1度だけ組んだもの。entry.low はいま欄に出ている訳。
    どちらも控えてあるので、1行あたり indexOf 2回で済む。
  */
  function matchesSearch(entry, q) {
    if (!q) {
      return true;
    }
    if (entry.hay.indexOf(q) >= 0) {
      return true;
    }
    return entry.low.indexOf(q) >= 0;
  }

  /*
    この行を出すか。q は小文字にした検索語。

    applyView と reviewClosed の両方から呼ぶ。同じ決め方を2か所に書くと、
    片方だけ直したときに「当て直したら消えるはずの行が消えない」といった
    食い違いが出る。ここでも新しい判断はしていない（keepAlways と
    matchesFilter と matchesSearch を並べただけ）。
  */
  function shouldShow(entry, q) {
    if (keepAlways(entry.line)) {
      return true;
    }
    return matchesFilter(entry) && matchesSearch(entry, q);
  }

  /* 同じ値は書き込まない。1943行ぶんを毎回書き換えると、描き直しが無駄に走る。 */
  function setHidden(node, hide) {
    if (node.hidden !== hide) {
      node.hidden = hide;
    }
  }

  /*
    条件を当て直して、出す行と隠す行を決める。

    数え上げも判定もしない。カテゴリはバッジから、状態は pending / failed から
    そのまま引くだけで、画面が新しい判断を持つ場所はここにも無い。

    保存のたびには呼ばない。呼ぶと、いま訳し終えた行が「未翻訳」の条件から
    外れて目の前で消える。条件は翻訳者が決めた時点の写しのままにしておき、
    読み直すか、条件を触ったときにだけ当て直す。

    見出しは、その下に出ている行があるときだけ出す。深さ（section / node）は
    待ち受けが付けたものを使う。出ている行が1つも無い見出しだけを並べても、
    読む手がかりにならない。

    ここで入力欄を閉じることはしない。閉じる必要が無い。開いている行は
    keepAlways が必ず出すので、入力欄を差し込んだまま行が隠れる道が無い。
    閉じていたころは、開いた直後の入力欄をここが閉じて打鍵を捨てていた
    （keepAlways を見よ）。
  */
  function applyView() {
    var q = el.search.value.toLowerCase();
    var heads = { section: null, node: null, other: null };
    var shown = 0;
    state.items.forEach(function (item) {
      if (item.heading) {
        setHidden(item.heading, true);
        if (item.level === "section") {
          /* 節が変われば、その前の節に属していた見出しはもう関わらない。 */
          heads.node = null;
          heads.other = null;
        } else if (item.level === "node") {
          /*
            節点が変われば、その前の節点に属していた見出しももう関わらない。
            捨てないと、# ===== でも # --- でもないコメント行（翻訳者が書いた
            1行メモなど）が、その下の行が1つも出ていないのに、後ろの節点の
            行の見出しとして出続ける。internal/edit は先頭が # の行をすべて
            コメントとして扱うので、メモを1行書いた時点で起きる。
          */
          heads.other = null;
        }
        heads[item.level] = item.heading;
        return;
      }
      var entry = item.entry;
      var show = shouldShow(entry, q);
      setHidden(entry.row, !show);
      if (!show) {
        return;
      }
      shown = shown + 1;
      showHeads(heads);
    });

    /*
      いま何行出ているか。件数の欄（internal/diff 由来）とは別の場所に、別の
      文言で出す。混ぜると、diff が判定した数と画面が出している数が同じものに
      見える。
    */
    el.shown.textContent = t("ui.shown", { count: shown });
    /*
      1行も出なかったら、一覧の場所でそう言う。真っ白な一覧と隅の「表示中 0 行」
      だけでは、壊れたのか条件に当たっていないのかが読み取れず、直し方も画面に
      無い（実際に、ja で打った検索語のまま ko へ移ると、ヘッダーが「行: 1713」
      と言っているのに一覧が空になった）。

      読むものが1行も無いとき（ロケールを選ぶ前）は出さない。そこで
      「条件に合う行がありません」と言うと、条件のせいだと読まれる。

      hidden ではなく中身の入れ替えで出し入れする。hidden の要素は支援技術の
      木から外れるので、文字が変わる瞬間に role="status" が木に居らず、
      告知しない実装があり得る（showMessage も同じ形にしてある）。

      検索の欄に字が入っているときは、別の文言にする。「条件を外すと全部出ます」
      は、検索語のせいで0行になったときには嘘になる。実データの de で
      zzzznotfound と打つと、条件を1つも選んでいないのに0行になり、外す条件が
      無いのに「条件を外せ」と言われる（外しても0行のままである）。まず検索の欄を
      空にすれば戻る、と言うほうが次の一手になる。空にしてなお0行なら、そのときに
      条件のほうの文言が出る。

      ここで見ているのは検索の欄に字があるかどうかだけで、数は数えていない。
    */
    var nothing = shown === 0 && state.items.length > 0;
    if (!nothing) {
      el.empty.textContent = "";
    } else if (q) {
      el.empty.textContent = t("ui.no_rows_search");
    } else {
      el.empty.textContent = t("ui.no_rows");
    }
  }

  /* いま関わっている見出しを出す。節と節点の両方を出さないと、上が欠ける。 */
  function showHeads(heads) {
    if (heads.section) {
      setHidden(heads.section, false);
    }
    if (heads.node) {
      setHidden(heads.node, false);
    }
    if (heads.other) {
      setHidden(heads.other, false);
    }
  }

  function markRow(entry) {
    if (!entry) {
      return;
    }
    entry.row.classList.toggle("unsaved", state.pending.has(entry.line));
    entry.row.classList.toggle("save-failed", state.failed.has(entry.line));
  }

  /* 行に添える1言（保存できない理由、値が変わった断り）。空なら消す。 */
  function setRowNote(entry, text) {
    if (!entry) {
      return;
    }
    /*
      アイコンを添える。編集できない行は鍵、それ以外（保存できない理由、値が
      変わった断り）は注意の三角。競合中の新旧は rowNode が自分で組む。
    */
    clear(entry.note);
    if (text) {
      entry.note.appendChild(icon(entry.editable ? "triangle-exclamation" : "lock"));
      entry.note.appendChild(span(null, text));
    }
    entry.note.hidden = !text;
  }

  /*
    その行は、どちらを残すか決まっていない（競合している）か。

    決まっていない行は編集させない。理由は openEditor に書いてある。
    ここは state.mine を引くだけで、新しい判断はしていない。
  */
  function isLocked(n) {
    return Boolean(state.mine && state.mine.has(n));
  }

  function openEditor(n) {
    if (!state.canEdit || state.editing === n) {
      return;
    }
    /*
      競合している行は、どちらを残すか人が選ぶまで開かない。

      決まっていない行への入力は「自分の訳を上に載せる」の意味にも「ファイルの
      訳を採る」の意味にも取れる。どちらへ寄せても、押したボタンと違う結果に
      なる道が残った。起点をファイルの訳にすると、1字打った瞬間に自分の版が
      画面から消えた。起点を自分の訳にすると、開き直したときに打ち直した訳
      （pending）が捨てられたうえ、「ファイルの訳を採る」を押したのに pending に
      残っていた自分の競合版がファイルへ書かれた（どちらも実測で再現した）。
      曖昧さを別の側へ動かすのではなく、曖昧な状態そのものを作らないことにした。

      これで keepMine は mine を pending へ足すだけ、takeFile は mine を捨てて
      pending を残すだけになり、どちらのボタンも押したとおりに効く。
      翻訳者は片方を選んでから直すので2手になるが、押したボタンと逆の結果に
      なるよりはよい。行には「ファイルの訳: … / あなたの訳: …」に続けて、
      選んでから直せるという断りを出してある（rowNode を見よ）。

      競合していない行は、この引き止めが出ているあいだも今までどおり編集できる。
      止めるのは、どちらを残すか決まっていない行だけである。
    */
    if (isLocked(n)) {
      return;
    }
    /*
      読み込みの最中も開かない（load を見よ）。読めた時点で一覧は描き直され、抱えて
      いる訳（state.pending）も片付くので、そのあいだに打った訳は黙って消える。
      マウス（mousedown）も Tab（focusin）も Enter の行送りもここを通るので、ここで
      止めれば全部止まる。読めなければ load が札を下ろし、また開けるようになる。
    */
    if (state.loading) {
      return;
    }
    var entry = state.rows.get(n);
    if (!entry || !entry.editable) {
      return;
    }
    /*
      いま開いている行を控えておく。開き終えてから照らし直すためである
      （reviewClosed のコメントを見よ）。控えないと、マウスで行から行へ移る道
      だけが照らし直しを通らない。closeEditor が state.editing を空にしたあとに
      blur が走るので、blur 側の commitEditor は null を受け取って何もしない。
      実測で、条件に当たらなくなった行が一覧に残り「表示中 N 行」も減らなかった。
    */
    var leaving = state.editing;
    closeEditor();
    state.editing = n;
    editor.value = shownValue(n, entry.saved);
    /*
      向きは中身から決めさせ、lang にはロケール名をそのまま入れる（ヘブライ語の
      確認用）。字形の選び方がこれで変わる。
    */
    editor.dir = "auto";
    editor.lang = state.locale;
    editor.setAttribute("aria-label", t("ui.edit_label"));
    /*
      差し込んでから焦点を移し、そのあとで元の欄を隠す。順番を逆にすると、
      焦点の載った欄を隠した瞬間に焦点が body へ飛び、入力欄を出した直後に
      blur が走って閉じてしまう（実際に起きた）。
    */
    entry.row.insertBefore(editor, entry.value);
    /*
      焦点より先に高さを決める。focus はこの欄を画面へ入れようとするので、
      あとから伸ばすと、送った先が実際の欄の位置とずれる。
    */
    fitEditor();
    editor.focus();
    entry.value.hidden = true;
    if (leaving !== null && leaving !== n) {
      /* 離れた行を保存へ回し、条件に照らし直す。順番は Enter の行送りと同じ。 */
      flush();
      reviewClosed(leaving);
    }
  }

  /*
    確定して閉じる。閉じる前に必ず保存へ回す。

    欄から離れたときだけでなく、絞り込みでその行が隠れるときにもここを通す。
    editor.blur() で済ませないのは、既に焦点が他所（検索の欄など）へ移って
    いると blur が起きず、入力欄を差し込んだまま行が隠れるためである。
  */
  function commitEditor() {
    var n = state.editing;
    closeEditor();
    flush();
    reviewClosed(n);
  }

  /*
    閉じた行を条件に照らし直す。もう出す行でなければ、そこで当て直す。

    入力欄が開いているあいだ、その行は keepAlways が条件を無視して出している
    （開いた直後の入力欄を隠して打鍵を捨てた事故の直し）。閉じたあともそのままに
    すると、条件に当たらない行が一覧に残り、「表示中 N 行」もその行を
    数えたままになる（残るのは1行とは限らない。上から順に Enter で打ち進めた
    ぶんだけ、まだ保存が済んでいない行として残る）。食い違いは、翻訳者が次に条件を触るまで消えない。

    「閉じるたびに applyView を呼ぶ」ではなく「閉じた行がもう出す行でないときだけ
    呼ぶ」にしてある。理由は実際に踏んで決めた。

      上から順に Enter で打っていくとき、閉じる時点のその行はまだ未保存なので
      keepAlways が出す。つまりここでは当て直しが走らない。走らせてしまうと、
      数行前に打ち終わって保存が済んだ行が条件から外れて次々に消え、一覧が
      1行ぶんずつせり上がる。それは「保存のたびには当て直さない」（訳し終えた
      行が目の前で消えないようにする）と決めたことを、1行遅れでやり直すだけに
      なる。

    呼ぶ順番も踏んで決めた。Enter の行送りでは、次の行を開いてからここを呼ぶ。
    逆にすると、当て直しの時点で state.editing が空なので、これから開く行が
    条件に当たらなければその場で隠れる。そのあと openEditor が隠れた行へ入力欄を
    差し込み、焦点は入っているのに欄は見えない（実測: 「未保存」で絞った状態で
    保存済みの行から Enter を押すと、入力欄は行番号 41 の行に入ったが、その行は
    hidden、入力欄の高さは 0、一覧は「条件に合う行がありません」になった）。
  */
  function reviewClosed(n) {
    if (n === null || n === undefined) {
      return;
    }
    var entry = state.rows.get(n);
    if (!entry) {
      return;
    }
    if (shouldShow(entry, el.search.value.toLowerCase())) {
      return;
    }
    applyView();
  }

  /*
    入力欄を閉じる。

    条件の当て直しはここではしない。閉じ方によって、当て直してよい時機が違う
    （reviewClosed を見よ）。一覧を描き直す手前（renderLines）や、別の行を開く
    手前（openEditor）から呼ばれることもある。
  */
  function closeEditor() {
    var n = state.editing;
    if (n === null) {
      return;
    }
    state.editing = null;
    state.composing = false;
    if (editor.parentNode) {
      editor.parentNode.removeChild(editor);
    }
    var entry = state.rows.get(n);
    if (entry) {
      entry.value.hidden = false;
    }
  }

  /* 入力のたび。値を控えて、自動保存の時計を引き直す。 */
  function onInput() {
    var n = state.editing;
    if (n === null) {
      return;
    }
    var clean = sanitize(editor.value);
    if (clean !== editor.value) {
      var caret = editor.selectionStart;
      editor.value = clean;
      editor.setSelectionRange(caret, caret);
    }
    /* 折り返しが1行増えた（減った）ぶんを追う。 */
    fitEditor();
    var entry = state.rows.get(n);
    if (entry) {
      setShownText(entry, clean);
      /*
        保存値と同じなら未保存から外す。ただし、その行をいま送っている最中は
        外さない。entry.saved は送る前の古い値で、応答が返ればファイルは送った
        値になる。そこで「保存値へ戻した」を未保存から外すと、ファイルには送った
        値が残ったまま画面は「保存済み」になり、戻した訳は二度と送られない
        （実際に起きた）。控えておけば、応答のあとで送った値と違うので残り、
        次の保存で送られる（applyResults と onSaved）。
      */
      if (clean === entry.saved && !(state.inflight && state.inflight.has(n))) {
        state.pending.delete(n);
      } else {
        state.pending.set(n, clean);
      }
      /* 書き換えたら「保存できない行」から外す。次の保存でまた試す。 */
      state.failed.delete(n);
      /*
        競合中の行では1言を消さない。そこに出ているのは「ファイルの訳: … /
        あなたの訳: …」と、選んでから直せるという断りで、消すと自分の版が
        画面から消える。

        openEditor が競合中の行を開かなくなったので、いまここは通らない。
        それでも残すのは二重の鍵としてである。競合中の行をまた開けるように
        したくなったときに、この1言が先に消えていると、自分の訳が画面の
        どこにも無い状態を作り直すことになる。
      */
      if (!(state.mine && state.mine.has(n))) {
        setRowNote(entry, "");
      }
      markRow(entry);
    }
    /*
      打ち直したら、送り直しの間隔を最初に戻す。広がったままだと、直したのに
      30秒待たされる。
    */
    state.retry = 0;
    updateStatus();
    if (!state.composing) {
      schedule();
    }
  }

  editor.addEventListener("input", onInput);

  /*
    変換中（compositionstart から compositionend まで）は Enter を横取りしない。
    横取りすると、ja / ko / zh-Hans / zh-Hant で変換の確定ができなくなる。
    この4言語のためにこの入力方式を選んでいる。
    自動保存も変換が終わるまで待つ。打ちかけの読みを保存しないため。
  */
  editor.addEventListener("compositionstart", function () {
    state.composing = true;
    /*
      走っている時計を止める。止めないと、変換を始める前の1打鍵で動き出した
      時計が変換の途中で切れて、打ちかけの読み（「あこ」など）がそのまま
      保存される。作業コピーへ書けば、ゲームが約2秒でそれを読み込む。
      確定したら compositionend が onInput から時計を引き直す。
    */
    if (state.timer) {
      clearTimeout(state.timer);
      state.timer = null;
    }
  });
  editor.addEventListener("compositionend", function () {
    state.composing = false;
    /* 確定の時刻を控える。直後に来る Enter を行送りに使わないため。 */
    state.composedAt = performance.now();
    onInput();
  });

  editor.addEventListener("keydown", function (e) {
    if (state.composing || e.isComposing || e.keyCode === 229) {
      return;
    }
    if (e.key === "Enter") {
      /*
        改行は入れない。確定して、次の（いま出ている）編集できる行を開く。
        上から順に打っていける形にする。これが翻訳作業のいちばん太い道になる。

        次の行を先に決めてから閉じる。閉じると state.editing が空になるので、
        順番を逆にすると「次」が分からなくなる。打った訳は onInput のときに
        未保存の控えへ入っており、閉じるときの flush がまとめて送る。
      */
      e.preventDefault();
      /*
        変換を確定した Enter は行送りに使わない。compositionend が確定の
        keydown より先に届く並びだと、上の3つの見張りを全部すり抜けて
        （composing も isComposing も false、keyCode は 13）行が飛ぶ。
        改行が入らないよう preventDefault だけはして、ここで戻す。
        待ちの長さと理由は composedGrace に書いてある。
      */
      if (performance.now() - state.composedAt < composedGrace) {
        return;
      }
      var from = state.editing;
      var next = nextEditable(from);
      if (next === null) {
        /*
          次が無い（出ている最後の編集できる行）。閉じずにここに留まり、
          保存だけ走らせる。閉じると焦点が body へ落ち、終わりまで来たという
          合図も出ないので、キーボードだけで打っている人は自分がどこにいるか
          分からなくなる。焦点を失わせないほうを採る。
        */
        flush();
        return;
      }
      /*
        閉じて、送って、次を開いてから、閉じた行を照らし直す。

        照らし直しを最後に回すのが肝である。先に回すと、そのとき state.editing は
        空なので、これから開く行が条件に当たらなければその場で隠れ、そのあと
        入力欄が隠れた行へ差し込まれる（reviewClosed に実測を書いた）。
        次を開いてからなら、その行は keepAlways が必ず出す。
      */
      closeEditor();
      flush();
      openEditor(next);
      reviewClosed(from);
      return;
    }
    if (e.key === "Escape") {
      /* 閉じるだけ。入力は消さない（消すと黙って破棄したことになる）。 */
      e.preventDefault();
      editor.blur();
    }
  });

  /*
    貼り付け。ブラウザー任せにすると改行の扱いが実装ごとに違う（落とすもの、
    詰めるものがある）。空白へ置き換えると決めて、ここで差し込む。
  */
  editor.addEventListener("paste", function (e) {
    var text = e.clipboardData ? e.clipboardData.getData("text") : "";
    e.preventDefault();
    editor.setRangeText(sanitize(text), editor.selectionStart, editor.selectionEnd, "end");
    onInput();
  });

  /* 欄から離れたら待たずに保存する。 */
  editor.addEventListener("blur", commitEditor);

  /*
    次の、いま画面に出ている編集できる行。Enter の行き先である。

    隠れている行は飛ばす。絞り込んだ一覧を上から順に打っていけるようにする
    ためで、飛ばさないと、条件に当たらない行の入力欄が画面の外で開く。

    競合している行も飛ばす（isLocked）。飛ばさないと、Enter でそこへ移った
    ときに openEditor が開かずに戻り、いま閉じたばかりなので焦点が body へ
    落ちる。キーボードだけで打っている人は、そこで自分がどこにいるか分から
    なくなる。開けない行は行き先にしない。
  */
  function nextEditable(n) {
    var found = null;
    var passed = false;
    /* state.rows は描いた順（＝行番号の順）で並んでいる。 */
    state.rows.forEach(function (entry, line) {
      if (found !== null) {
        return;
      }
      if (line === n) {
        passed = true;
        return;
      }
      if (!passed || !entry.editable || entry.row.hidden || isLocked(line)) {
        return;
      }
      found = line;
    });
    return found;
  }

  /* 焦点を受けた訳欄の行番号。訳欄でなければ null。 */
  function lineOf(target) {
    if (!target || target === editor || !target.dataset || !target.dataset.line) {
      return null;
    }
    return Number(target.dataset.line);
  }

  /*
    押した時点で入力欄に差し替える。

    mousedown の既定の動作（押した要素へ焦点を移す）を止めてから差し替える。
    止めないと、この直後にブラウザーが元の欄へ焦点を戻し、その欄はもう隠れて
    いるので焦点が body へ落ちる。入力欄は出た瞬間に閉じる（実際に起きた）。
  */
  el.list.addEventListener("mousedown", function (e) {
    var n = lineOf(e.target);
    if (n === null) {
      return;
    }
    /*
      競合している行は開かない。既定の動作も止めない。止めると、ファイルの訳を
      マウスで選んで写すことすらできなくなる。どちらを残すか決める場面で、
      両方の訳を読めなくするのは筋が悪い。
    */
    if (isLocked(n)) {
      return;
    }
    /*
      読み込みの最中も同じ。開かず（openEditor が止める）、既定の動作も止めない。
      読み終えるまでのあいだも、訳をマウスで選んで写せるようにしておく。
    */
    if (state.loading) {
      return;
    }
    e.preventDefault();
    openEditor(n);
  });

  /* Tab で移ってきたとき。こちらはブラウザーが焦点を移し終えている。 */
  el.list.addEventListener("focusin", function (e) {
    var n = lineOf(e.target);
    if (n === null) {
      return;
    }
    openEditor(n);
  });

  function schedule() {
    if (state.timer) {
      clearTimeout(state.timer);
    }
    state.timer = setTimeout(function () {
      state.timer = null;
      flush();
    }, state.autosaveDelay);
  }

  /*
    未保存の行をまとめて送る。

    送ったぶんを pending から先に消さない。消してから応答が来ないと、その訳は
    どこにも残らない。応答で「保存できた」と分かった行だけ、送った値と同じなら消す。
    送ったあとに打ち直した行は値が違うので残り、次の保存で送られる。
  */
  function flush() {
    if (state.timer) {
      clearTimeout(state.timer);
      state.timer = null;
    }
    /*
      変換の途中では送らない。送ると打ちかけの読みがファイルに入る。
      確定したら compositionend から onInput が時計を引き直す。
    */
    if (state.composing) {
      return Promise.resolve();
    }
    /*
      読み直し（切り替え）で「その訳は消えます」と尋ね、翻訳者が受けたあとは送らない。
      送ると、捨てると答えた訳がファイルに入り、尋ねた文と結果が逆になる。読み込みの
      あいだにも送り直しの時計は切れるので、ここで止める。読めなければ load が
      送り直しへ戻す。
    */
    if (state.discarding) {
      return Promise.resolve();
    }
    if (state.saving || state.mine || state.pending.size === 0) {
      if (!state.saving && !state.mine && state.saveError) {
        /*
          送り直しを待っているあいだに、送るものが無くなった（保存値へ打ち戻した、
          競合で「ファイルの訳を採る」を選んだ、など）。落ちていた要求はもう無い
          ので、「保存できません（もう一度試しています）」を下ろす。下ろさないと、
          閉じても何も失われないのに、翻訳者は存在しない失敗を直しにいく。
          scheduleRetry の早い戻りと同じ扱いで、1行ずつの理由（state.failed）は
          そちらの表示に任せる。
        */
        state.saveError = false;
        state.retry = 0;
        showMessage("");
        updateStatus();
      }
      return Promise.resolve();
    }
    var edits = [];
    var sent = new Map();
    state.pending.forEach(function (value, line) {
      var entry = state.rows.get(line);
      edits.push({
        line: line,
        /*
          キーも送る。行番号だけで送ると、手前でよそが行を足したり消したり
          していたときに、訳が別のキーの行へ入る。待ち受けは食い違いを見つけ
          たら書かずに断る。
        */
        key: entry && entry.key ? entry.key : "",
        translation: value
      });
      sent.set(line, value);
    });
    /*
      送った時点の世代を覚えておく。返ってくるまでにロケールが変わっていたら、
      その応答は画面のものではない。載せると、別ロケールの版と件数が入り、
      誰も触っていないファイルで 409 が出る。

      読み直しと切り替えは送り終えてから読みにいき（settle）、読み込みのあいだは
      一覧を編集させない（load）ので、送りかけの応答が読み込みのあとに返る道は、
      いまは画面の操作では見つかっていない。世代の見分けは守りとして残す。
    */
    var gen = state.gen;
    state.saving = true;
    state.inflight = sent;
    updateStatus();
    /*
      送り終わりを返す。書き出し（exportCsv）が、送っていない訳を落としたまま
      ファイルを読まないようにするためと、読み直し（settle）が送り終えるのを
      待ってから尋ねるためにある。失敗も下の catch が受け止めるので、ここが
      投げっぱなしになることは無い。

      state.sending にも持たせる。送っている最中に呼ばれた flush は、上の早い戻りで
      すぐに解ける（書き出しはそれを「まだ送っている」と読む）。送り終わりそのものを
      待ちたい側は、こちらを待つ。
    */
    state.sending = postJSON("/api/rows", {
      locale: state.locale,
      baseVersion: state.version,
      edits: edits
    })
      .then(function (res) {
        state.saving = false;
        if (gen !== state.gen) {
          updateStatus();
          return;
        }
        state.inflight = null;
        onSaved(res, sent);
      })
      .catch(function () {
        /*
          届かなかった。未保存の訳はそのまま抱えたままにする。失敗したことは
          画面に出す（黙って成功したように見せない）。
        */
        state.saving = false;
        if (gen !== state.gen) {
          updateStatus();
          return;
        }
        state.inflight = null;
        showMessage(t("ui.save_failed_detail"));
        scheduleRetry();
        updateStatus();
      });
    return state.sending;
  }

  /*
    書き出し。いま開いているロケールのCSVを、翻訳者が選んだ場所へ保存させる。

    置き場を決めるのはブラウザーである。ここが行うのは、待ち受けから中身を
    受け取って、ブラウザーへ「これを保存して」と渡すところまでで、保存先の
    パスはどこにも作らないし、待ち受けへも送らない。画面からパスを受け取る
    経路を開けると、そこが待ち受けの中でいちばん弱い場所になる。

    先に未保存を送りきる。待ち受けはファイルを読んで書き出すので、送っていない
    訳は書き出したものに入らない。押した時点の画面と、保存されたファイルの
    中身が違うのは、この道具がいちばんやってはいけないことである。
  */
  function exportCsv(form) {
    if (!state.locale || state.exporting) {
      return;
    }
    /*
      開いている入力欄を閉じ、打った訳を未保存の控えへ入れる。ボタンを押した
      時点で blur は走っているが、走らない経路（キーボードでたどり着いた場合）を
      当てにしない。
    */
    closeEditor();
    state.exporting = true;
    updateExportButtons();
    setExportState("", false);

    flush().then(function () {
      if (state.mine) {
        /*
          競合している。送った訳が 409 で返ると、onConflict がそれを state.mine へ
          移して state.pending から外すので、下の判定だけでは「送りきった」に見える。
          そのまま取りにいくと、選ぶ前のよその訳の中身を「書き出しました」と渡す。
          書き出したものの中では、黙って破棄したのと同じになる。どちらを残すかを
          選んでもらってから、押し直してもらう。

          「送っています」とは言わない。競合のあいだは自動保存を止めていて、
          何も送っていない。
        */
        setExportState(t("ui.export_conflict"), true);
        return null;
      }
      /*
        保存できない行と、行き先の無い訳も、競合と同じくファイルに入っていない訳で
        ある（hasUnsaved が未保存に数えるのと同じ考え）。そのまま取りにいくと、画面に
        出ているその訳の入らない中身を「書き出しました」と渡す。送り直しでは片付かない
        （保存できない行は自動保存の対象から外してあり、行き先の無い訳は載せる行が
        無い）ので、「送っています」とも言わない。何を片付ければ書き出せるかを言う。
      */
      if (state.failed.size) {
        setExportState(t("ui.export_row_failed"), true);
        return null;
      }
      if (state.orphans.length) {
        setExportState(t("ui.export_orphans"), true);
        return null;
      }
      if (state.saving || state.pending.size > 0) {
        /*
          まだ送り終わっていない。待って勝手に書き出すのではなく、押し直して
          もらう。待つ作りにすると、押したのに何も起きない時間ができる。
        */
        setExportState(t("ui.export_wait"), true);
        return null;
      }
      return fetchCsv(form)
        .then(function (got) {
          return saveCsv(got.blob, got.name);
        })
        .then(function (saved) {
          if (saved) {
            setExportState(t("ui.export_done"), false);
          } else {
            /* 人が保存ダイアログを閉じた。何も無かったことにする。 */
            setExportState("", false);
          }
        })
        .catch(function (err) {
          /*
            待ち受けが返した理由（目録の文）だけをそのまま出す。ブラウザーが投げた
            誤り（届かなかったときの "Failed to fetch"、保存ダイアログの書き込みの
            失敗など）は、目録に無い英文なので出さず、目録の文で伝える。
          */
          setExportState(err && err.fromServer ? err.message : t("ui.export_failed"), true);
        });
    }).then(function () {
      state.exporting = false;
      updateExportButtons();
    });
  }

  /*
    書き出しの結果を、帯の中（押したボタンのすぐ下）に出す。メニューの中には
    置かない。選んだ時点でメニューは閉じ、閉じたメニューの中身は支援技術の木から
    外れるので、そこでは見えも告知されもしない。

    うまくいかなかったときは、失敗の理由（#message）と同じ出し方にする。色だけに
    頼らないのも同じで、文言がそのまま理由になっている。

    うまくいったときだけ .timed を付ける。下に残り時間の帯が出て（app.css）、
    帯が尽きたら消える（boot の animationend）。帯は貼り付いているので、
    「書き出しました」が次の書き出しまで居座ると、そのぶん一覧が狭くなる。
    失敗の理由と「送り終わってから押して」は消さない。読み終わる前に消えると、
    何が起きたかを知るすべが無くなる。
  */
  function setExportState(text, bad) {
    var name = "notice export-state";
    if (bad) {
      name += " error";
    } else if (text) {
      name += " timed";
    }
    el.exportState.textContent = text ? text : "";
    el.exportState.className = name;
  }

  /*
    書き出しのメニューを閉じる。2つのうちどちらかを選んだら閉じる。開いたままだと、
    名前を付けて保存のダイアログと結果の欄の手前にメニューが居座る。焦点は、
    メニューを開いた帯のボタンへ戻る（popover の既定）。

    popover を持たないブラウザーでは、メニューは帯の中にそのまま出ていて、
    閉じるものが無い。
  */
  function closeExportMenu() {
    if (typeof el.exportMenu.hidePopover !== "function") {
      return;
    }
    if (el.exportMenu.matches(":popover-open")) {
      el.exportMenu.hidePopover();
    }
  }

  function updateExportButtons() {
    el.exportPublished.disabled = state.exporting;
    el.exportWorking.disabled = state.exporting;
  }

  /*
    中身を取りにいく。誤りのときは本文が理由そのものなので、そのまま投げる。
    状態コードだけを出しても、翻訳者には直しようがない。

    投げる誤りには fromServer の印を付ける。exportCsv が出してよいのは、この
    印の付いた文（待ち受けが目録から組んだもの）だけである。
  */
  function fetchCsv(form) {
    var url = "/api/export?locale=" + encodeURIComponent(state.locale) +
      "&form=" + encodeURIComponent(form);
    return fetch(url, { credentials: "same-origin" }).then(function (res) {
      if (!res.ok) {
        return res.text().then(function (text) {
          var err = new Error(text ? text.trim() : t("ui.export_failed"));
          err.fromServer = true;
          throw err;
        });
      }
      var name = nameFromDisposition(res.headers.get("Content-Disposition"));
      return res.blob().then(function (blob) {
        return { blob: blob, name: name };
      });
    });
  }

  /*
    Content-Disposition から名前を取る。待ち受けが付ける名前は
    [A-Za-z0-9._-] だけなので（internal/web の exportName）、当てるのも
    その形に限る。当たらなければ決め打ちにする。応答の文字列がそのまま
    ファイル名になる道を作らない。

    字の種類だけでなく、exportName と同じく、先頭がドットの名前（".csv"、"..")と
    ".." を含む名前も落とす。字の種類だけで見ていたころは、".." がそのまま保存の
    ダイアログへ渡った。待ち受けがこの名前を返すことは無いが、守りを待ち受けと
    同じ規則にそろえておく。
  */
  function nameFromDisposition(value) {
    var found = value ? /filename="([A-Za-z0-9._-]+)"/.exec(value) : null;
    if (!found || found[1].charAt(0) === "." || found[1].indexOf("..") >= 0) {
      return "strings.csv";
    }
    return found[1];
  }

  /*
    保存先をブラウザーに決めさせる。

    showSaveFilePicker があれば「名前を付けて保存」が出るので、フォルダーも
    名前もその場で選べる。無いブラウザーでは <a download> に落ちる。そちらは
    ブラウザーが決めた落とし先へ入る。

    人がダイアログを閉じたとき（AbortError）は false を返す。「書き出しました」と
    言わないのが大事で、言えば、どこにも無いファイルを探しにいかせることになる。
  */
  function saveCsv(blob, name) {
    if (!window.showSaveFilePicker) {
      return Promise.resolve(downloadCsv(blob, name));
    }
    return window.showSaveFilePicker({
      suggestedName: name,
      types: [{ accept: { "text/csv": [".csv"] } }]
    }).then(function (handle) {
      return handle.createWritable().then(function (stream) {
        return stream.write(blob).then(function () {
          return stream.close();
        });
      }).then(function () {
        return true;
      });
    }, function (err) {
      if (err && err.name === "AbortError") {
        return false;
      }
      /*
        ダイアログを出せなかった。押してから中身が届くまでの間に、ブラウザーが
        「人の操作から続いている」と見なす時間が切れることがある。決まった
        落とし先へ入れるほうへ戻す。何も保存されないよりはよい。
      */
      return downloadCsv(blob, name);
    });
  }

  function downloadCsv(blob, name) {
    var url = URL.createObjectURL(blob);
    var link = document.createElement("a");
    link.href = url;
    link.download = name;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    /*
      すぐ剥がすと、保存が始まる前に中身が消える実装がある。少し置いてから
      捨てる。置いてあるあいだもブラウザーの中だけで、外へは出ない。
    */
    setTimeout(function () {
      URL.revokeObjectURL(url);
    }, 60000);
    return true;
  }

  function onSaved(res, sent) {
    var body = res.body || {};
    /*
      応答が、いま画面に出ているロケールのものか確かめる。世代でも弾いているが、
      ここでも見る。別ロケールの版を載せると、誰も触っていないファイルで
      409 が出て、ありもしない競合を人に選ばせることになる。
    */
    if (body.locale && body.locale !== state.locale) {
      return;
    }
    if (res.status === 409 && body.current) {
      if (body.current.locale !== state.locale) {
        return;
      }
      onConflict(body);
      return;
    }
    if (res.status === 200) {
      state.retry = 0;
      state.saveError = false;
      applyResults(body.results || [], sent);
      state.version = body.version;
      showMessage("");
      renderCounts(body.counts);
      /*
        条件は組み直さず、チップの数だけ写す。組み直すと、触っている最中の
        選択が跳ねる。写さないと、訳を入れた行のバッジだけが消えて、チップの
        数は入れる前のままになる。
      */
      updateChipRows(body.counts);
      renderNotes(body.notes);
      updateStatus();
      if (state.pending.size) {
        /* 送っているあいだに足されたぶん。続けて保存する。 */
        schedule();
      }
      return;
    }
    /*
      200 でないときは、結果の saved を信じない。ファイルに入っていない値を
      「保存できた」と読むと、未保存の控えを捨ててしまう。行ごとの理由
      （編集できない行、書けない値）だけを取り出して、残りは未保存のまま抱える。
    */
    applyRowErrors(body.results || []);
    showMessage(body.message ? body.message : t("ui.save_failed_detail"));
    scheduleRetry();
    updateStatus();
  }

  /* 行ごとの理由だけを取り出す。値は欄に残したまま、自動保存の対象から外す。 */
  function applyRowErrors(results) {
    results.forEach(function (r) {
      if (!r.error) {
        return;
      }
      var entry = state.rows.get(r.line);
      markFailed(r.line, r.error, entry ? entry.saved : "");
      setRowNote(entry, t("ui.row_error", { reason: r.error }));
      markRow(entry);
    });
  }

  /*
    待ってからもう一度送る。待ち受けが 503 を返す失敗（Windows の共有違反）は
    待てば直る。

    諦めない。決めた間隔を使い切ったあとは、最後の間隔のまま送り続ける。
    途中で諦めると、原因が消えたあともその訳は二度と送られない（実際に、
    読み取り専用を解除して12秒待っても送られなかった）。
  */
  function scheduleRetry() {
    if (state.pending.size === 0) {
      /*
        送るものが残っていない。1行ずつの理由（state.failed）のほうで出るので、
        「保存できません」の表示はそちらに任せる。
      */
      state.saveError = false;
      return;
    }
    state.saveError = true;
    var i = state.retry;
    if (i >= retryDelays.length) {
      i = retryDelays.length - 1;
    }
    state.retry = state.retry + 1;
    if (state.timer) {
      clearTimeout(state.timer);
    }
    state.timer = setTimeout(function () {
      state.timer = null;
      flush();
    }, retryDelays[i]);
  }

  /* 200 のときだけ呼ぶ。saved はファイルに入ったことを意味する。 */
  function applyResults(results, sent) {
    results.forEach(function (r) {
      var entry = state.rows.get(r.line);
      if (r.saved) {
        if (state.pending.get(r.line) === sent.get(r.line)) {
          state.pending.delete(r.line);
        }
        state.failed.delete(r.line);
        if (entry) {
          entry.saved = r.translation;
          if (state.editing !== r.line) {
            /*
              ファイルから読み直した値を出す。画面とファイルを同じにする。

              ただし、送ったあとに打ち直した訳が未保存として残っていれば、そちらを
              出す（shownValue）。行には「未保存」の印が付いたままで、次の保存で
              送るのもそちらである。ファイルの値で描き直すと、欄と入力欄と送り直して
              いる訳が別物になり、次の保存が落ちたときに翻訳者が見ている訳が
              どこにも無い値になる。
            */
            setShownText(entry, shownValue(r.line, r.translation));
          }
          renderBadgesForKey(entry, r.badges);
          setRowNote(entry, r.warning ? r.warning : "");
        }
      } else {
        /*
          この行は保存できない。値は控えごと持ったまま、自動保存の対象からだけ
          外す。外さないと、同じ要求を投げ続けることになる。書き換えれば戻る。
        */
        markFailed(r.line, r.error, sent.get(r.line));
        setRowNote(entry, t("ui.row_error", { reason: r.error ? r.error : "" }));
      }
      markRow(entry);
    });
  }

  /* いま描いている行の 行番号 → キー。読み直す前に控えておく。 */
  function keyIndex() {
    var keys = new Map();
    state.rows.forEach(function (entry, line) {
      keys.set(line, entry.key);
    });
    return keys;
  }

  /*
    読み直した内容の上に、抱えている編集を載せ直す。

    行番号だけで載せ直すと危ない。読み直すまでのあいだによそが行を足したり
    消したりしていると、同じ行番号が別のキーの行を指す。訳が別の行に入り、
    その行にもとからあった訳が消える（実際に起きた）。だから載せ直しはキーで行う。

      同じキーが同じ行番号にある  → その行番号のまま（ずれていない）
      どこか1か所にだけある       → その行番号へ移す
      無い、2か所以上ある、先が埋まっている
                                  → 載せる先を決められない。捨てずに
                                    「行き先が見つからない訳」として画面に出す

    キーを持たない行（キー列が空の作業コピー）は、行番号で載せるしかない。
    待ち受けもキーの無い要求は照合しないので、扱いはそろっている。
  */
  function remap(edits, oldKeys, data) {
    var byKey = new Map();
    (data.lines || []).forEach(function (line) {
      if (line.kind !== "data" || !line.key) {
        return;
      }
      var seen = byKey.get(line.key);
      if (seen) {
        seen.push(line.n);
        return;
      }
      byKey.set(line.key, [line.n]);
    });

    var moved = new Map();
    var lost = [];
    edits.forEach(function (value, line) {
      var k = oldKeys.get(line);
      if (!k) {
        moved.set(line, value);
        return;
      }
      var seen = byKey.get(k);
      var to = null;
      if (seen && seen.indexOf(line) >= 0) {
        to = line;
      } else if (seen && seen.length === 1) {
        to = seen[0];
      }
      if (to === null || moved.has(to)) {
        lost.push({ key: k, text: value });
        return;
      }
      moved.set(to, value);
    });
    return { edits: moved, lost: lost };
  }

  /*
    行き先が見つからなくなった訳を控える。捨てない。

    ここが、その訳が残っている最後の場所になる。翻訳者が控えるまで出し続け、
    残っているあいだは beforeunload でも引き止める。
  */
  function addOrphans(lost) {
    lost.forEach(function (item) {
      state.orphans.push(item);
    });
    renderOrphans();
  }

  function renderOrphans() {
    clear(el.orphansList);
    state.orphans.forEach(function (item) {
      var row = li(null, "");
      row.appendChild(span("note-label", item.key + ": "));
      var text = span("note-value", item.text);
      /* 訳なので向きは中身から決めさせる。 */
      text.dir = "auto";
      text.lang = state.locale;
      row.appendChild(text);
      el.orphansList.appendChild(row);
    });
    el.orphans.hidden = state.orphans.length === 0;
  }

  /*
    読み直した内容で、その行をよそが本当に書き換えたかを返す。

    見分け方は1つ。その行についてこちらが最後に見たファイルの値（entry.saved）と、
    読み直した内容のその行の値を比べるだけである。同じならよそは触っていない。

    どの行と比べるかは remap と同じ規則で決める（同じ行番号に同じキーがあれば
    その行、どこか1か所にだけあればその行）。決められないときは「変わった」と
    見なす。決められない行をそのまま保存し直すより、人に見せるほうが安全である。
  */
  function fileChanged(line, key, was, byKey) {
    if (!key) {
      /* キーを持たない行は照合できない。変わったものとして扱う。 */
      return true;
    }
    var seen = byKey.get(key);
    if (!seen) {
      return true;
    }
    var at = null;
    seen.forEach(function (x) {
      if (x.n === line) {
        at = x;
      }
    });
    if (at === null && seen.length === 1) {
      at = seen[0];
    }
    if (at === null) {
      return true;
    }
    return at.tr !== was;
  }

  /*
    409。手前でファイルが変わっている。1バイトも書かれていない。

    未保存の編集を捨てない。読み直した内容の上へキーで載せ直したうえで、
    「よそが本当に書き換えた行」と「そうでない行」に分ける。

    分けるのは、選ばせる対象を実際に競合した行だけにするためである。分けないと、
    よそが触ったのが別の行でも、こちらの未保存の訳が全部「競合したもの」になる。
    そのまま 409 がもう一度起きると、選んでいるあいだに打った訳まで競合したものへ
    移り、「ファイルの訳を採る」で一緒に捨てられる。実測で、翻訳者が打った訳が
    ファイルにも画面にも残らず、beforeunload の引き止めも効かない形になった。

    よそが触ったのがこちらの触っていない行だけだったときは、選ばせることが無い。
    引き止めは出さず、読み直した版の上へそのまま保存し直す。flush ではなく
    schedule を通すのは、よそが書き続けているあいだ 409 と再送で回り続けない
    ようにするためである。
  */
  function onConflict(body) {
    var oldKeys = keyIndex();
    var byKey = new Map();
    ((body.current || {}).lines || []).forEach(function (line) {
      if (line.kind !== "data" || !line.key) {
        return;
      }
      var item = { n: line.n, tr: line.translation ? line.translation : "" };
      var seen = byKey.get(line.key);
      if (seen) {
        seen.push(item);
        return;
      }
      byKey.set(line.key, [item]);
    });

    var clashed = new Map();
    var untouched = new Map();
    state.pending.forEach(function (value, line) {
      var entry = state.rows.get(line);
      var was = entry ? entry.saved : "";
      if (fileChanged(line, oldKeys.get(line), was, byKey)) {
        clashed.set(line, value);
        return;
      }
      untouched.set(line, value);
    });

    var mine = remap(clashed, oldKeys, body.current);
    var keep = remap(untouched, oldKeys, body.current);
    /*
      保存できなかった訳（state.failed）も、同じ規則でキーから載せ直す。値は
      { reason, value } のまま移す。行番号のままにしていたころは、よそが上に
      1行足しただけで、goodbye の訳が hello の行に「保存できない行」として出た。
      そのまま hello の行を1字直すと、goodbye の訳が hello のキーへ保存され、
      hello の訳は消えた（実際に起きた）。載せる先を決められないものは、
      ほかと同じく行き先の無い訳として出す。
    */
    var failed = remap(state.failed, oldKeys, body.current);

    /*
      入力欄で打っている最中なら、その行も同じ規則で引き直しておく。描き直しは
      一覧を作り直すので、入力欄はいったん外れる。開き直さないと、焦点は body へ
      落ち、続けて打った字はどこにも入らないのに、保存の欄は「保存済み」と言う
      （keepAlways に書いた事故と同じ形。実際に起きた）。
    */
    var typing = null;
    var caret = null;
    var wasTyping = state.editing !== null && document.activeElement === editor;
    if (wasTyping) {
      remap(new Map([[state.editing, true]]), oldKeys, body.current).edits.forEach(function (value, line) {
        typing = line;
      });
      caret = { start: editor.selectionStart, end: editor.selectionEnd };
    }

    state.mine = mine.edits.size ? mine.edits : null;
    state.pending = keep.edits;
    state.failed = failed.edits;
    addOrphans(mine.lost);
    addOrphans(keep.lost);
    addOrphans(failed.lost.map(function (item) {
      return { key: item.key, text: item.text.value };
    }));
    /*
      描き直すあいだは、入力欄の blur で保存へ回さない。入力欄を頁から外すと
      Chromium は blur を出し、commitEditor がその場で flush する。それでは
      下の「すぐに送らず自動保存の時計を通す」が破れる。
    */
    editor.removeEventListener("blur", commitEditor);
    try {
      render(body.current);
    } finally {
      editor.addEventListener("blur", commitEditor);
    }
    /*
      打っていた行を開き直し、キャレットも戻す。競合した行（isLocked）は
      openEditor が開かない。どちらを残すか選ぶまで編集させないためである。

      描き直しで隠れた行には差し込まない。未保存の訳がある行は keepAlways が
      出すので、隠れるのは打った訳が保存値と同じ行だけで、失う訳は無い。
      隠れた行へ差し込むと、焦点は入らず高さ0の入力欄が残る（reviewClosed を見よ）。
    */
    var reopen = typing !== null ? state.rows.get(typing) : null;
    if (reopen && !reopen.row.hidden) {
      openEditor(typing);
      if (state.editing === typing) {
        editor.setSelectionRange(caret.start, caret.end);
      }
    }
    if (state.mine) {
      showMessage(body.message ? body.message : t("ui.save_conflict"));
      el.conflict.hidden = false;
      updateStatus();
      /*
        打っていた行そのものが競合した（または行き先を失った）ときは、入力欄を
        開き直せない。焦点は body へ落ち、そのあと打った字は黙ってどこにも入らない
        （keepAlways に書いた事故と同じ形）。引き止めの枠そのものへ焦点を移す。
        字は入らないままだが、焦点がどこにあり次に何を選ぶのかが、画面にも読み上げ
        にも出る。

        ボタンには移さない。翻訳者は打っている最中なので、続けて打った Space や
        Enter（日本語の入力では変換と確定に使う）がそのボタンを押す。最初の
        ボタンは「自分の訳を上に載せる」で、よそが書いた訳を読まないまま上書き
        することになる。枠は tabindex="-1" で、打鍵では何も押されない。Tab で
        最初のボタンへ進める。

        出してから移す。hidden のままの要素には焦点が入らない。
      */
      if (wasTyping && state.editing === null) {
        el.conflict.focus();
      }
      return;
    }
    showMessage("");
    el.conflict.hidden = true;
    updateStatus();
    schedule();
  }

  function keepMine() {
    /*
      自分の訳を、読み直した内容の上に載せ直して保存する。

      競合した行は選ぶまで編集できないので（openEditor を見よ）、state.mine の
      行が state.pending にも居ることはもう起こらない。それでも上書きしない形で
      足すのは、選んでいるあいだに打った訳のほうが常に新しいからである。
      丸ごと置き換えていたころは、選んでいるあいだの入力が黙って消えた。
      「黙って破棄もしない」が破れるのは、まさにこの場面だった。
    */
    state.mine.forEach(function (value, line) {
      if (!state.pending.has(line)) {
        state.pending.set(line, value);
      }
    });
    state.mine = null;
    el.conflict.hidden = true;
    showMessage("");
    render(state.data);
    flush();
  }

  function takeFile() {
    /*
      ファイルの訳を採る。ここで初めて自分の編集を捨てる。人が選んだ結果であって、
      待ち受けも画面も黙って捨ててはいない。

      捨てるのは競合したぶん（state.mine）だけである。選んでいるあいだに打った
      訳は、人が捨てると言っていないので残す。競合した行は選ぶまで編集できない
      ので（openEditor を見よ）、ここで残る pending は必ず別の行のものになる。
      競合した行がファイルの訳へ戻ることと、打った訳が残ることが両立する。

      最後に flush を呼ぶのは、競合のあいだ止めていた自動保存をここで動かし直す
      ためである。呼ばないと、その訳は別の行を触るまで送られない。
    */
    state.mine = null;
    el.conflict.hidden = true;
    showMessage("");
    render(state.data);
    flush();
  }

  /*
    保存の状態に添えるアイコン。文言と色だけでなく形でも伝える。
    回るのは saving だけ（app.css の .save-state.saving）。
  */
  var saveIcons = {
    clean: "circle-check",
    saving: "spinner",
    pending: "floppy-disk",
    failed: "triangle-exclamation",
    conflict: "code-merge"
  };

  function updateStatus() {
    var text = t("ui.save_clean");
    var kind = "clean";
    if (state.mine) {
      text = t("ui.save_conflict");
      kind = "conflict";
    } else if (state.saving) {
      text = t("ui.save_saving");
      kind = "saving";
    } else if (state.saveError) {
      /*
        要求そのものが落ちている（届かない、404、503を出し切った）。ここで
        「未保存 N 件」と出すと、打ったばかりでまだ送っていない状態と
        見分けが付かない。常に見えている場所で、保存できていないことを言う。
      */
      text = t("ui.save_retrying");
      kind = "failed";
    } else if (state.failed.size) {
      text = t("ui.save_failed");
      kind = "failed";
    } else if (state.pending.size) {
      text = t("ui.save_pending", { count: state.pending.size });
      kind = "pending";
    } else if (state.orphans.length) {
      /*
        行き先の見つからない訳だけが残っている。ファイルには1つも入っていないので
        「保存済み」とは言えない。ここが、その訳が残っている最後の場所である。
        言わずにおくと、翻訳者は「保存済み」を見て閉じにいき、beforeunload で
        初めて何かが残っていると知ることになる。順序が逆になる。
      */
      text = t("ui.save_orphans", { count: state.orphans.length });
      kind = "failed";
    }
    el.saveState.replaceChildren(icon(saveIcons[kind]), span(null, text));
    el.saveState.className = "save-state " + kind;
  }

  /* 未保存のものがあるか。競合で抱えているぶんと、行き先が無いぶんも入れる。 */
  function hasUnsaved() {
    if (state.pending.size || state.failed.size || state.orphans.length) {
      return true;
    }
    return Boolean(state.mine && state.mine.size);
  }

  /*
    1行を組む。

    訳の欄には入力欄を常設しない。1721行ぶんの入力欄を置くと、開くだけで重くなる。
    焦点が入った1行にだけ、頁に1つだけ作った入力欄を差し込む。
  */
  function rowNode(line, locale) {
    var row = document.createElement("div");
    row.className = "row" + (line.editable ? "" : " not-editable");

    row.appendChild(span("cell num", line.n));

    var badges = document.createElement("div");
    badges.className = "cell badges";
    row.appendChild(badges);

    row.appendChild(span("cell speaker", line.speaker));

    /* 原文は常に英語。右から左の訳に引きずられて崩れないよう ltr に固定する。 */
    var source = span("cell source", line.source);
    source.dir = "ltr";
    row.appendChild(source);

    var value = span("cell translation", line.translation);
    /*
      訳の向きは中身から決めさせる（ヘブライ語の確認用）。lang はロケール名を
      そのまま入れる。字形の選び方がこれで変わる。
    */
    value.dir = "auto";
    value.lang = locale;
    if (!line.editable) {
      /*
        編集できない行。中身は生の行のまま出す（列がずれているので、最終
        フィールドが訳とは限らない）。理由は行に添える1言のほうへ回す。
        中身を理由で置き換えると、直せない行を読むことすらできなくなる。
      */
      value.className = "cell raw";
      value.dir = "ltr";
      value.textContent = line.text ? line.text : "";
    }
    row.appendChild(value);

    var note = document.createElement("div");
    note.className = "cell row-note";
    note.hidden = true;
    row.appendChild(note);

    var entry = {
      line: line.n,
      row: row,
      value: value,
      badges: badges,
      note: note,
      /* キーは行の同定に使う。409 のあとに編集を載せ直すのはこれが頼り。 */
      key: line.key ? line.key : "",
      editable: Boolean(line.editable) && state.canEdit,
      saved: line.translation ? line.translation : "",
      /*
        絞り込みの当て先。待ち受けが付けたバッジのカテゴリを写すだけで、
        画面では決めない。renderBadges が入れる。
      */
      cats: new Set(),
      /*
        検索の当て先のうち、書き換わらないぶん。キー・speaker・原文と、
        編集できない行の生のテキストを、小文字にして1度だけ組む。
        訳は打つたびに変わるので entry.low のほうで別に持つ。
      */
      hay: (
        (line.key ? line.key : "") + "\n" +
        (line.speaker ? line.speaker : "") + "\n" +
        (line.source ? line.source : "") + "\n" +
        (line.text ? line.text : "")
      ).toLowerCase(),
      low: ""
    };
    state.rows.set(line.n, entry);
    entry.low = value.textContent.toLowerCase();
    renderBadges(entry, line.badges);
    if (!line.editable && line.reason) {
      setRowNote(entry, t("ui.not_editable", { reason: line.reason }));
    }

    if (entry.editable) {
      /*
        焦点を受けられるようにする。ここに焦点が入ると入力欄へ差し替わる。
        Tab で行から行へ移れるので、キーボードだけでも打っていける。
      */
      value.tabIndex = 0;
      value.dataset.line = String(line.n);
      setShownText(entry, shownValue(line.n, entry.saved));
    }
    /*
      保存できなかった行は、描き直しても理由を出し直す。控えは state.failed に
      あるので出せる。出さないと、行は「保存できない」色と枠のままなのに、
      なぜ保存できないかが画面のどこにも無くなる。
    */
    var bad = state.failed.get(line.n);
    if (bad) {
      setRowNote(entry, t("ui.row_error", { reason: bad.reason }));
    }
    if (state.mine && state.mine.has(line.n)) {
      /*
        競合中の行。ファイルの値と自分の値を両方出す。どちらを残すか選ぶのは
        人で、画面はそのための材料を並べるだけ。

        最後に「選んでから直せます」を添える。この行は openEditor が開かない
        ので、断りが無いと、押しても入力欄が開かない行が黙って1つある形に
        なる。効かない理由は画面に出す。
      */
      row.classList.add("conflicted");
      /*
        焦点も受けさせない。受けると、Tab で来ても入力欄が開かない欄になり、
        キーボードだけで打っている人には行き止まりに見える。打てない欄に
        「打てそうな」破線の枠を出さないためでもある。
      */
      value.removeAttribute("tabindex");
      setShownText(entry, entry.saved);
      clear(note);
      note.hidden = false;
      note.appendChild(icon("code-merge"));
      /* 文はまとめて1つの span に入れる。1言は flex なので、ばらすと隙間が空く。 */
      var body = span(null, "");
      body.appendChild(span("note-label", t("ui.conflict_file") + ": "));
      body.appendChild(span("note-value", entry.saved));
      body.appendChild(span("note-label", " / " + t("ui.conflict_mine") + ": "));
      body.appendChild(span("note-value", state.mine.get(line.n)));
      body.appendChild(span("note-locked", " " + t("ui.conflict_locked")));
      note.appendChild(body);
    }
    markRow(entry);
    return row;
  }

  function headingNode(line) {
    var e = document.createElement("div");
    e.className = "heading " + (line.heading || "other");
    e.appendChild(icon("hashtag"));
    /* ファイルにあるコメント行をそのまま出す。組み直さない。 */
    e.appendChild(span(null, line.text));
    return e;
  }

  /* 見出しの深さ。待ち受けが付けた値をそのまま使い、知らない値は other に寄せる。 */
  function headingLevel(line) {
    if (line.heading === "section") {
      return "section";
    }
    if (line.heading === "node") {
      return "node";
    }
    return "other";
  }

  function renderLines(data) {
    closeEditor();
    state.rows = new Map();
    /*
      描いた順をそのまま控える。絞り込みは、この並びを上から1回なぞるだけで
      済む。見出しと行が交ざった並びなので、見出しの下に出ている行があるか
      どうかも、同じ1回で分かる。
    */
    state.items = [];
    /*
      キー → その キーの行。保存のあとにバッジを描き直す先を引くために持つ
      （renderBadgesForKey）。ふつうは1キー1行だが、作業コピーは Mod が書き
      出す CSV なので、同じキーが2行あることを止める仕掛けはどこにも無い。
    */
    state.byKey = new Map();
    var fragment = document.createDocumentFragment();
    (data.lines || []).forEach(function (line) {
      if (line.kind === "heading") {
        var head = headingNode(line);
        state.items.push({ heading: head, level: headingLevel(line) });
        fragment.appendChild(head);
        return;
      }
      fragment.appendChild(rowNode(line, data.locale));
      var entry = state.rows.get(line.n);
      state.items.push({ entry: entry });
      if (entry.key) {
        var same = state.byKey.get(entry.key);
        if (!same) {
          same = [];
          state.byKey.set(entry.key, same);
        }
        same.push(entry);
      }
    });
    el.list.replaceChildren(fragment);
    /* 行数は待ち受けが数えたもの。ここでは数えない。 */
    el.rows.textContent = t("ui.rows", { count: data.rows });
    /*
      読み直したあとも、いま選んでいる条件をそのまま当て直す。当て直さないと、
      競合のあとの描き直しで条件だけが残り、画面には全行が出る。
    */
    applyView();
  }

  function render(data) {
    state.data = data;
    state.locale = data.locale;
    state.version = data.version;
    /*
      検索の欄も、訳の入力欄と同じ扱いにする。向きは中身から決めさせ、lang には
      ロケール名をそのまま入れる。入れないと、he や ar の翻訳者が検索語を打った
      ときにキャレットと並びが左から右のままになる。当てる先（原文・訳）は
      そのロケールの字なので、打つ側だけ英語向きにしておく理由が無い。
    */
    el.search.dir = "auto";
    el.search.lang = data.locale;
    el.path.textContent = t("ui.file") + ": " + data.path;
    /*
      名前が入ったので、一帯の畳みを出す。ほかの2つ（近道の一覧、絞り込みの
      断り書き）は目録が届いた時点で出すが、この1つだけは /api/lines を待つ。
      畳んだままでも読めるのは summary だけで、そこに入るのは目録の文ではなく
      ファイルの名前だからである。

      applyCatalog で出すと、--locale を省いたときに名前の無い畳みが残る。
      あの経路はロケールを選ぶまで /api/lines を叩かないので、summary が
      添え字だけ（「（断り書き・件数・数えたもの）」）になり、それでも焦点は
      受ける（実測: 1280幅で一帯の高さ 47.4px、tabIndex 0、#file-path と
      #notes と #counts はどれも空）。--locale の既定は省略で、ダブルクリック
      で開いたときもこの経路なので、いちばん多くの人が最初に見る画面になる。
    */
    el.panelFold.hidden = false;
    renderNotes(collectNotes(data));
    renderCounts(data.counts);
    /*
      条件の並びは、いま読んだ件数から組み直す。選ばれているかは state.filter が
      持っているので、組み直しても選択は残る。保存のあとに来る件数
      （onSaved の renderCounts）では組み直さない。条件の並びが保存のたびに
      作り直されると、触っている最中の選択が跳ねる。
    */
    buildFilters(data.counts);
    renderStats(data.stats);
    renderLines(data);
    updateStatus();
  }

  /*
    断り書きを1つにまとめる。文面はすべて待ち受けから来たもので、
    ここで新しい判断はしない。読み取り専用の理由だけは、待ち受けが返した
    文面を決まった枠に入れて出す。
  */
  function collectNotes(data) {
    var notes = (data.notes || []).slice();
    if (data.readOnlyReason) {
      notes.push(t("ui.file_readonly", { reason: data.readOnlyReason }));
    }
    return notes;
  }

  /*
    ロケールの行を読む。

    resetFinder は「読めたら条件と検索語を外す」。外すのを成功したときだけに
    するのは、失敗したときに画面が三者バラバラになるためである。読む前に外して
    いたころは、読み込みに失敗すると state.filter と検索欄だけが空になり、
    チップの checked と一覧は前のロケールのまま残った（実測で「チップ2つが
    選ばれたまま、一覧も表示中の行数も前のロケールのまま」になった）。
    失敗したときは何も変わっていないのが正しい。

    保存の世代と送り直しの時計も、読めたときだけ片付ける（下の .then）。読む前に
    片付けていたころは、読み込みに失敗すると送り直しの時計が止まったまま戻らず、
    画面は「未保存 1 件」と言ったまま、その訳を二度と送らなかった（原因が消えた
    あとも、同じ行を打ち直すまでファイルに入らない）。retryDelays の「諦めない」が、
    失敗した読み直しを1回挟むだけで破れていた。

    描くのは、最後に始めた読み込みの応答だけにする（state.loading の札）。返る前に
    もう1つ選ぶと（欄に焦点を置いて↓を続けて押すと起きる）、応答は選んだ順にも
    逆の順にも返る。どちらも描いていたころは、あとから返ったほうが一覧・ファイルの
    名前・保存の宛先（state.locale）を決め、欄は別のロケールを指したまま残った。
    欄に出ているロケールを選び直しても change は起きないので、欄が指すロケールへは
    欄からたどり着けない。欄は、描いたあとと失敗したあとに必ずそろえる（syncLocale）。
  */
  function load(locale, resetFinder) {
    /*
      捨てると答えたあとの読み込みか（askDiscard が立てた印）。読めたら捨て、
      読めなければ倒して送り直しへ戻す（stopHolding）。
    */
    var holding = state.discarding;
    /*
      この読み込みの札。先に始めた読み込みの札はここで置き換わり、その応答は
      描かれなくなる。空のロケール（選ぶ前へ戻した）は読みにいかないので札を持たない。
    */
    var ticket = locale ? { locale: locale } : null;
    state.loading = ticket;
    syncLocale();
    syncBusy();
    if (!locale) {
      stopHolding(holding);
      clear(el.list);
      el.rows.textContent = "";
      showMessage(t("ui.select_locale"));
      return;
    }
    /*
      読み終えるまで一覧を編集させない（openEditor が state.loading を見る）。

      返るまでのあいだも前の一覧は出たままで、以前はそこで打てた。読めた時点で
      下の .then が抱えている訳ごと片付けるので、打った訳は黙って消えた（実際に
      起きた。ファイルにも画面にも残らず、保存の欄は「保存済み」になった）。捨てると
      答えたあとは flush が送らないので必ず消え、答えていなくても、保存が落ちれば
      同じだった。捨てると答えた訳の上に、新しい訳を打ち足させない。

      開いている入力欄は閉じ、閉じるときの保存へ回す（commitEditor）。捨てると
      答えたあとなら flush が送らないので、捨てると答えた訳は送られない。答えて
      いなければ送る。尋ねずに読みにくるのは抱えている訳が無いときなので、送る
      ものはふつう無い。

      読めなければ、下の .catch が札を下ろして元に戻す。
    */
    if (state.editing !== null) {
      commitEditor();
    }
    showMessage(t("ui.loading"));
    /* URL に載せるのはロケール名だけ。原文も訳も URL には載せない。 */
    getJSON("/api/lines?locale=" + encodeURIComponent(locale))
      .then(function (data) {
        if (state.loading !== ticket) {
          /*
            あとから別の読み込みが始まっている。描かない。世代も送り直しの時計も
            進めない。まだ画面に出ているのは前の内容で、あとの読み込みが片付ける。

            捨てると答えた印の後始末だけはする（stopHolding）。印がまだこの読み込みの
            ものなら倒して送り直しへ戻す。あとの切り替えで捨てると答え直していれば、
            印はそちらのものに替わっているので触らない（stopHolding は自分の印の
            ときだけ倒す）。そちらを倒すと、あとの読み込みが返るまでに保存へ回った
            訳が送られ、捨てると答えた訳がファイルに入る。
            あとの読み込みが同じ印を持ったままなのは、あとで押したときに捨てる訳が
            もう無く、尋ねなかったとき（askDiscard）だけである。そのとき印が止めて
            いるのは、そのあと打った訳の保存だけなので、倒して送らせてよい。
          */
          stopHolding(holding);
          return;
        }
        /*
          世代を1つ進める。進めておくと、送りかけの保存の応答が返ってきたときに
          「もう画面のものではない」と分かる。別ロケールの版と件数を載せない。

          読めたここで進める。読み込みの途中に返った応答は、まだ画面に出ている
          （前の）内容のものなので、そのまま載せてよい。
        */
        state.gen = state.gen + 1;
        if (state.timer) {
          clearTimeout(state.timer);
          state.timer = null;
        }
        state.retry = 0;
        state.saveError = false;
        /* 送りかけの訳は前の内容のもので、応答も上の世代で捨てる。 */
        state.inflight = null;
        showMessage("");
        /*
          選んだあとは「ロケールを選んでください」を選択肢から外す。起動時に
          ロケールを指定したときと同じ並びになる（fillLocales）。

          残しておくと、行が並んでいるのに空へ戻せる。戻すと一覧だけが消え、
          ファイルの名前・条件のチップ・件数・「表示中 N 行」・抱えている訳は
          前のロケールのまま残った。state.locale も前のロケールのままなので、
          書き出しや保存は画面に出ていないロケールへ向かう。片付ける先を1つずつ
          足すより、戻す道そのものを作らない。

          外すのは空の選択肢1つだけで、欄の値は描いたあとでそろえる（syncLocale）。
          選択肢を組み直して値もここで決めていたころは、組み直しが最初の応答でしか
          走らず、あとから返った応答は欄に触れずに一覧だけを描いた。
        */
        var blank = el.locale.querySelector('option[value=""]');
        if (blank) {
          el.locale.removeChild(blank);
        }
        /*
          条件を外すのはここ。render より先に外すと、チップも一覧も新しい
          ロケールのぶんが最初から外れた形で描かれる（buildFilters は
          state.filter を見て checked を決める）。
        */
        if (resetFinder) {
          clearFinder();
        }
        state.pending = new Map();
        state.failed = new Map();
        state.mine = null;
        state.orphans = [];
        /* 捨てると答えた訳は、ここで捨て終わった。送らない印も倒す。 */
        if (state.discarding === holding) {
          state.discarding = null;
        }
        renderOrphans();
        el.conflict.hidden = true;
        render(data);
        /*
          札を下ろすのは描き終えてから。描く途中で投げたら、下の .catch がこの
          読み込みの失敗として受ける（札がまだ残っているので、あとの読み込みの
          ものと取り違えない）。
        */
        state.loading = null;
        syncLocale();
        syncBusy();
      })
      .catch(function () {
        /*
          読めなかったので何も捨てていない。捨てると答えた印を倒し、送り直しへ戻す。
          あとから別の読み込みが始まっていても同じにする（理由は上の .then の注記）。
        */
        stopHolding(holding);
        if (state.loading !== ticket) {
          /*
            あとから別の読み込みが始まっている。失敗を出さない。あとの読み込みが
            読めていれば、読めている画面の上に「読めませんでした」が載る。
          */
          return;
        }
        state.loading = null;
        /*
          失敗の中身は出さない。翻訳者にできるのは読み直すことだけ。
          ロケールの欄は元に戻す。戻さないと、欄だけが新しいロケールを指した
          まま中身は前のロケール、という食い違いが画面に残る。
          一覧もまた編集できるようにする。前の一覧のまま、何も変わっていない。
        */
        syncLocale();
        syncBusy();
        showMessage(t("ui.load_failed"));
        updateStatus();
      });
  }

  /*
    ロケールの欄を、画面が向かっている先にそろえる。読んでいる最中なら最後に
    始めた読み込みの行き先、読んでいなければ画面に出ているロケール（state.locale）。

    欄はこの画面でいちばん目立つ「どのロケールを見ているか」の表示である。一覧と
    別のロケールを指していると、翻訳者は別のロケールのつもりで訳を打つ。欄に
    出ているロケールを選び直しても change は起きないので、自分では直せない。
  */
  function syncLocale() {
    el.locale.value = state.loading ? state.loading.locale : state.locale;
  }

  /*
    一覧が読み込みの最中かどうかを、一覧そのものに出す（aria-busy）。読み終えるまで
    訳の欄は開かない（openEditor）。何も出さないと、押しても開かない欄が黙って
    並ぶ。app.css がこれを見て、打てそうな印（cursor: text）を下ろす。「読み込んで
    います」の文は、load が #message に出している。
  */
  function syncBusy() {
    el.list.setAttribute("aria-busy", state.loading ? "true" : "false");
  }

  /*
    捨てると答えたのに、捨てずに終わった（読めなかった）。止めていた送り直しを戻す。

    読み込みのあいだに送り直しの時計が切れていたら、flush は送らずに戻っており、
    時計はもう無い。引き直さないと、原因が消えてもその訳は二度と送られない
    （retryDelays の「諦めない」が破れる）。時計がまだ残っていれば、それに任せる。
    holding が空（捨てると答えていない読み込み）なら何もしない。
  */
  function stopHolding(holding) {
    if (!holding || state.discarding !== holding) {
      return;
    }
    state.discarding = null;
    if (state.timer || state.mine || state.pending.size === 0) {
      return;
    }
    if (state.saveError) {
      scheduleRetry();
      return;
    }
    schedule();
  }

  /*
    条件と検索語をまとめて外す。走っている検索の待ちも止める。

    止めないと、外した直後に古い検索語で当て直す（もう空の欄を読むので実害は
    無いが、無駄な描き直しが1回走る）。
  */
  function clearFinder() {
    state.filter = new Set();
    el.search.value = "";
    if (state.searchTimer) {
      clearTimeout(state.searchTimer);
      state.searchTimer = null;
    }
  }

  /* 打ち終わりを待ってから当て直す。待ちの理由は searchDelay に書いてある。 */
  function scheduleSearch() {
    if (state.searchTimer) {
      clearTimeout(state.searchTimer);
    }
    state.searchTimer = setTimeout(function () {
      state.searchTimer = null;
      applyView();
    }, searchDelay);
  }

  /*
    いま焦点がある先は、字を打つところか。

    INPUT を一括りにしない。チェックボックスとラジオは字を打つところではないので
    「打っている」に数えない。数えていたころは、絞り込みの条件に焦点があるあいだ
    スラッシュが効かず、「条件を選んだ直後にスラッシュで検索へ移る」という
    いちばんありそうな流れで黙って何も起きなかった。画面の説明（ui.keys_help）は
    「入力欄の外でスラッシュを押すと検索の欄へ移ります」と言っている。
  */
  function isTyping(target) {
    if (!target || !target.tagName) {
      return false;
    }
    var tag = target.tagName;
    if (tag === "INPUT") {
      var type = String(target.type || "").toLowerCase();
      return type !== "checkbox" && type !== "radio";
    }
    if (tag === "TEXTAREA" || tag === "SELECT") {
      return true;
    }
    return target.isContentEditable === true;
  }

  /*
    左の列の開閉。

    狭い画面（900px 以下。app.css の @media と同じ幅）では列は引き出しで、
    開いているあいだだけ body に sidebar-open を立てる。広い画面では列を畳んで
    一覧を広げるだけで、sidebar-collapsed を立てる。2つを分けるのは、狭い画面で
    開いた状態を広い画面へ持ち越さないためである。幅が変わったら引き出しは閉じる。

    状態はどこにも残さない。この道具は画面の状態をどこにも残さないと決めてある。
    開き直せば列は出ている。

    aria-expanded は開閉のたびに書く。ボタンが何を開閉するかは aria-controls
    （index.html）が結んでいる。
  */
  var narrow = window.matchMedia("(max-width: 900px)");

  function sidebarOpen() {
    return document.body.classList.contains("sidebar-open");
  }

  function syncMenu() {
    var expanded = narrow.matches
      ? sidebarOpen()
      : !document.body.classList.contains("sidebar-collapsed");
    el.menu.setAttribute("aria-expanded", expanded ? "true" : "false");
  }

  function setDrawer(open) {
    document.body.classList.toggle("sidebar-open", open);
    el.backdrop.hidden = !open;
    syncInert();
    syncMenu();
  }

  /*
    狭い画面で引き出しが閉じているあいだは、列の中身に焦点を入れさせない（inert）。

    閉じた引き出しは transform で画面の外へ出してあるだけで、中身はタブ順に残る。
    入れさせていたころは、#reload から Tab で進むと、画面の外にある閉じるボタン・
    検索の欄・条件のチップに焦点が入った。見えない検索の欄に字を打つと、一覧だけが
    黙って絞られる。開けば入れる。広い画面では列は画面に出ているので、入れさせる。

    閉じる瞬間に焦点が列の中にあれば、開くボタン（#menu）へ戻す。inert にした
    要素からは焦点が外れ、body へ落ちる。
  */
  function syncInert() {
    var shut = narrow.matches && !sidebarOpen();
    if (shut && el.sidebar.contains(document.activeElement)) {
      el.menu.focus();
    }
    el.sidebar.inert = shut;
  }

  function toggleSidebar() {
    if (narrow.matches) {
      setDrawer(!sidebarOpen());
      return;
    }
    document.body.classList.toggle("sidebar-collapsed");
    syncMenu();
  }

  el.menu.addEventListener("click", toggleSidebar);
  el.sidebarClose.addEventListener("click", function () {
    setDrawer(false);
    /* 閉じたら焦点を開いたボタンへ戻す。落とすと body へ飛ぶ。 */
    el.menu.focus();
  });
  el.backdrop.addEventListener("click", function () {
    setDrawer(false);
  });
  narrow.addEventListener("change", function () {
    setDrawer(false);
  });
  syncInert();
  syncMenu();

  /*
    一文字の近道。スラッシュで検索の欄へ移る。Escape で引き出しを閉じる。

    入力欄に焦点があるときは絶対に効かせない。効くと、訳にスラッシュが打てなく
    なる。変換中も横取りしない。修飾キーが付いているもの（Ctrl+/ など）も
    ブラウザー側の意味があるので触らない。
  */
  document.addEventListener("keydown", function (e) {
    if (state.composing || e.isComposing || e.keyCode === 229) {
      return;
    }
    if (e.ctrlKey || e.metaKey || e.altKey) {
      return;
    }
    /*
      引き出しが開いているときの Escape は、引き出しを閉じる。検索の欄に焦点が
      あっても閉じる（欄は引き出しの中にある）。訳の入力欄の Escape は入力欄
      自身が受けて閉じるが、引き出しは一覧の上に被さるので、両方閉じてよい。
    */
    if (e.key === "Escape" && sidebarOpen()) {
      e.preventDefault();
      setDrawer(false);
      el.menu.focus();
      return;
    }
    if (isTyping(e.target)) {
      return;
    }
    if (e.key === "/") {
      e.preventDefault();
      /*
        絞り込みの一帯を画面へ送る。この一帯は貼り付けるのをやめた（貼り付けると
        競合の引き止めのボタンが押せなくなる）ので、1721行の途中からでは画面の
        外にある。貼り付けをやめたぶんはここで補う。焦点だけ移すと、打っている
        欄が見えないまま字だけが入る。

        焦点を移してから送る。順番を逆にすると、そのあとの focus がブラウザーの
        既定の送り方（入力欄そのものを画面へ入れる。一帯の余白は使わない）で
        上書きし、検索の欄が貼り付く帯の下へ潜る。
      */
      /* 狭い画面では、先に引き出しを開く。広い画面で畳んでいれば出す。 */
      if (narrow.matches) {
        setDrawer(true);
      } else {
        document.body.classList.remove("sidebar-collapsed");
        syncMenu();
      }
      el.search.focus();
      el.search.select();
      if (el.finder) {
        el.finder.scrollIntoView({ block: "start" });
      }
    }
  });

  /*
    いま送っている保存と、まだ送っていない訳を送り終えるまで待つ。

    送っている最中の flush は早く戻る（書き出しはそれを「まだ送っている」と読む）
    ので、送り終わりそのもの（state.sending）を先に待つ。そのあとの flush が、
    送っているあいだに打ち足されたぶんと、送り直しを待っていた訳を送る。落ちても
    投げない（flush の catch が受け止め、送り直しの時計を引き直す）。

    待ち終えたら、送っている最中かどうかをもう一度見て、そうならその送り終わりを
    待ってからやり直す。同じ送り終わりを待っているのが自分だけとは限らないためで
    ある。読み直しを2度押すと、2つの settle が同じ送り終わりを待つ。先に動いたほうの
    flush が、待つあいだに打ち足された訳を送り始めると、あとのほうの flush は送って
    いる最中なので早く戻り、その保存が返る前に尋ねていた（実際に起きた）。受けると
    読み込みが走り、「その訳は消えます」と尋ねた訳が、そのあと返った保存でファイルに
    入った。

    同じ送り終わりを待つ settle は、待ち始めた順に動く。先に動いたほうが送れば、
    残りはその送り終わりを同じ順で待ち直す。最後に押したほうはいつも最後に動くので、
    それが送り終えたあとに、ほかの settle が次を送り始めることは無い。
  */
  function settle() {
    if (state.saving && state.sending) {
      return state.sending.then(settle);
    }
    return flush();
  }

  /*
    読み直しとロケールの切り替えは、未保存の訳を消す。消す前に必ず尋ねる。
    key は尋ねる文（読み直しは ui.discard_confirm、切り替えは ui.switch_confirm）。

    尋ねるのは、送れるものを送り終えてからにする。押した時点で決めていたころは、
    打った直後に押すと、欄から離れたときの保存（blur）が返る前か後かで、尋ねたり
    尋ねなかったりした。尋ねた文は「その訳は消えます」なのに、その訳は送られて
    ファイルに残った。送り終えてから決めれば、尋ねるのは送っても残るもの（保存
    できない行、競合、行き先の無い訳、届かなかった訳）があるときだけになる。

    受けたら state.discarding を立てる。読み込みのあいだに送り直しの時計が切れても、
    捨てると答えた訳は送らない（flush）。立てたあとは必ず load を呼ぶこと。

    返すのは次のどれか。
      "go"     読んでよい（捨てるものが無いか、捨てると答えた）
      "keep"   断った。何も変えない
      "stale"  送り終えるのを待っているあいだに、また押された。そちらに任せる
  */
  function askDiscard(key) {
    state.asking = state.asking + 1;
    var ask = state.asking;
    return settle().then(function () {
      if (ask !== state.asking) {
        return "stale";
      }
      if (!hasUnsaved()) {
        return "go";
      }
      if (!window.confirm(t(key))) {
        return "keep";
      }
      state.discarding = {};
      return "go";
    });
  }

  function boot() {
    getJSON("/api/bootstrap")
      .then(function (data) {
        ui = data.ui;
        locales = data.locales || [];
        state.canEdit = Boolean(data.canEdit);
        if (data.autosaveDelayMs) {
          state.autosaveDelay = data.autosaveDelayMs;
        }
        applyCatalog();
        showGamePath(data.game);
        fillLocales(data.selected);
        el.locale.addEventListener("change", function () {
          /*
            選んだロケールはここで控える。尋ねるのは送り終えてからなので、そのあいだに
            先に始めた読み込みが返ると、欄は描いたロケールへそろえ直される（load）。
            欄の値を読み直すと、選んでいないロケールを読みにいく。
            読み込みのあいだは一覧を編集させない（load）ので、読み込みと保存が重なる
            この並びは、いまは画面の操作では起きない。控えるのは守りとして残す。
          */
          var chosen = el.locale.value;
          askDiscard("ui.switch_confirm").then(function (answer) {
            if (answer === "stale") {
              /*
                あとから押されたほうに任せる。欄にも触らない。あとから選び直したなら
                欄はもうそのロケールを指しており、読み直しなら押した時点で欄を
                そろえてある。
              */
              return;
            }
            if (answer === "keep") {
              /*
                断ったので何も変えない。欄は選ぶ前に指していた先へ戻す。読んでいる
                最中ならその行き先で、画面に出ている前のロケールではない。前のロケールへ
                戻すと、読み込みが返ったときに欄と一覧が食い違う。
              */
              syncLocale();
              return;
            }
            /*
              前のロケールの条件と検索語を持ち越さない。持ち越すと、ja で打った
              「もしもし」のまま ko へ移ったときに、ヘッダーが「行: 1713」と
              言っているのに一覧が空になる。条件はそのロケールを見ながら決めた
              ものなので、ロケールが変われば外すのが素直である。
              読み直し（el.reload）では外さない。同じロケールを見続けている。

              外すのは load に任せる。読めたときだけ外させるためで、ここで先に
              外すと、読み込みに失敗したときに条件と検索欄だけが空になり、
              チップと一覧は前のロケールのまま残る（load を見よ）。
            */
            load(chosen, true);
          });
        });
        el.reload.addEventListener("click", function () {
          /*
            読み直すのは画面に出ているロケール（state.locale）で、欄の値ではない。

            切り替えを選んで、送り終えるのを待っているあいだに押すと、その切り替えは
            この読み直しに取って代わられる（askDiscard が stale を返す）。欄の値を
            読んでいたころは、欄に残った切り替え先を、前のロケールの条件と検索語を
            付けたまま読んだ（切り替えなら外す）。送れずに読み直しを尋ねて断られると、
            欄だけが切り替え先を指したまま残り、一覧・ファイルの名前・送り直しの宛先は
            前のロケールのままだった。そこで欄から前のロケールを選び直すと「切り替えると
            消えます」と尋ね、受けると、同じロケールのまま送り直している訳を捨てた。

            欄は押した時点でそろえる（syncLocale）。取って代わられた切り替えはもう
            起きないので、その行き先を欄に残さない。そろえておけば、断ったとき
            （keep）も、あとから押されたとき（stale）も、欄に触らなくてよい。

            まだ何も出ていない（--locale を省いて起動し、最初の読み込みが返る前）なら、
            読み直すものが無い。load("") を呼ぶと、読んでいる最初のロケールを
            取りやめて「ロケールを選んでください」へ戻してしまう。
          */
          if (!state.locale) {
            return;
          }
          syncLocale();
          askDiscard("ui.discard_confirm").then(function (answer) {
            if (answer !== "go") {
              return;
            }
            load(state.locale);
          });
        });
        el.filterClear.addEventListener("click", function () {
          /* 条件も検索語もまとめて外す。「全部見たい」は1手でできるようにする。 */
          clearFinder();
          /*
            チップは組み直さず、選択だけを外す。組み直すと、数と添え書きが
            最後に読み込んだときの件数（state.data.counts）に戻る。保存の応答で
            待ち受けが数え直した数（updateChipRows）が消え、件数の欄とも別の
            時点の数になる。
          */
          el.filters.querySelectorAll('input[type="checkbox"]').forEach(function (box) {
            box.checked = false;
          });
          applyView();
        });
        /*
          検索は待ち受けに聞かない。行はもうブラウザーの中にある。聞けば、
          検索語（＝原文の断片）が待ち受けの記録に残りうるし、URL に載せれば
          ブラウザーの履歴に残る。ここから出る要求は1つも増やさない。
        */
        el.search.addEventListener("input", function () {
          if (state.searchComposing) {
            /*
              変換中は走らせない。打ちかけの読み（「あこ」など）で当てると、
              1行も出ない画面になって、打っている途中の手が止まる。
            */
            return;
          }
          scheduleSearch();
        });
        el.search.addEventListener("compositionstart", function () {
          state.searchComposing = true;
        });
        el.search.addEventListener("compositionend", function () {
          state.searchComposing = false;
          scheduleSearch();
        });
        el.conflictKeep.addEventListener("click", keepMine);
        el.conflictTake.addEventListener("click", takeFile);
        el.exportPublished.addEventListener("click", function () {
          closeExportMenu();
          exportCsv("published");
        });
        el.exportWorking.addEventListener("click", function () {
          closeExportMenu();
          exportCsv("working");
        });
        /*
          残り時間の帯が尽きたら「書き出しました」を消す。帯は .timed の
          ::after なので、終わりの知らせはこの要素に届く。.timed を見直すのは、
          消すのがうまくいった知らせだけで、失敗の理由ではないことを確かめるため。
        */
        el.exportState.addEventListener("animationend", function () {
          if (el.exportState.classList.contains("timed")) {
            setExportState("", false);
          }
        });
        window.addEventListener("beforeunload", function (e) {
          if (!hasUnsaved()) {
            return;
          }
          /*
            未保存のまま閉じさせない。文面はブラウザーが決めるので出ないことが
            多いが、returnValue を入れないと引き止めそのものが効かない。
          */
          e.preventDefault();
          e.returnValue = t("ui.unsaved");
        });
        updateStatus();
        load(data.selected || "");
      })
      .catch(function () {
        showMessage(t("ui.load_failed"));
      });
  }

  /*
    目録が届く前から測り始める。届かなくても（/api/bootstrap が落ちても）
    失敗の理由は帯の中に出るので、帯の高さは正しくしておく。
  */
  watchTopHeight();
  boot();
})();
