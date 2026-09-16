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
  - 入力欄は1つだけ作って、いま触っている行へ差し込む。1839行ぶんの入力欄を
    常設すると、開くだけで重くなる。

  絞り込みと検索について守ること。

  - 未保存の訳がある行と、保存できなかった行は、条件に当たらなくても隠さない。
    隠すと、直すべき行が画面から消え、翻訳者は消えたことに気づけない。
  - 条件で行が消えるとき、その行の入力欄が開いていたら、先に保存してから閉じる。
  - 検索語を外へ出さない。URL にも待ち受けへの要求にも載せない。行はもう
    ブラウザーの中にあるので、待ち受けに聞く必要がない。聞けば、検索語
    （＝原文の断片）が待ち受けの記録に残りうる。
  - localStorage にも sessionStorage にも、条件も検索語も残さない。ディスクに残る。
  - カテゴリの一覧も名前も、待ち受けが返した件数から取る。画面で定義し直さない。
  - 変換中（compositionstart から compositionend、e.isComposing、keyCode 229）は
    キーを1つも横取りしない。ja / ko / zh-Hans / zh-Hant のためにこの入力方式を
    選んでいる。
*/

(function () {
  var ui = null;
  var locales = [];

  var el = {
    title: document.getElementById("app-title"),
    localeLabel: document.getElementById("locale-label"),
    locale: document.getElementById("locale"),
    reload: document.getElementById("reload"),
    rows: document.getElementById("rows"),
    saveState: document.getElementById("save-state"),
    notice: document.getElementById("edit-notice"),
    message: document.getElementById("message"),
    path: document.getElementById("file-path"),
    notes: document.getElementById("notes"),
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
    filterLabel: document.getElementById("filter-label"),
    filters: document.getElementById("filters"),
    filterClear: document.getElementById("filter-clear"),
    searchLabel: document.getElementById("search-label"),
    search: document.getElementById("search"),
    finderNote: document.getElementById("finder-note"),
    shown: document.getElementById("shown"),
    keys: document.getElementById("keys"),
    orphans: document.getElementById("orphans"),
    orphansTitle: document.getElementById("orphans-title"),
    orphansList: document.getElementById("orphans-list")
  };

  /*
    state.pending   まだ保存していない訳（行番号 → 値）。
    state.failed    保存できなかった行（行番号 → 理由）。値は欄に残したまま、
                    自動保存の対象からだけ外す。書き換えれば pending へ戻る。
    state.mine      競合のあいだ抱えている自分の編集。選ばせるまで捨てない。
    state.orphans   載せる先の行がファイルから無くなった訳。捨てずに画面へ出す。
    state.rows      行番号 → 描いた要素。保存の結果を差し込むために持つ。
    state.items     描いた順そのまま（見出しと行）。絞り込みは この並びを
                    上から1回なぞるだけで済ませる。
    state.filter    選ばれている絞り込みの条件。"cat:<識別子>" が待ち受けから
                    来たカテゴリ、"state:pending" と "state:failed" が画面の状態。
                    どれかに当たれば出す（複数選べる）。
    state.gen       読み直しとロケール切り替えのたびに進める番号。送りかけの
                    保存の応答が「もう画面のものではない」と分かるようにする。
    state.saveError 要求そのものが落ちているか（届かない、404、503）。行ごとの
                    理由（state.failed）とは別に持つ。
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
    items: [],
    filter: new Set(),
    gen: 0,
    editing: null,
    composing: false,
    searchComposing: false,
    searchTimer: null,
    saving: false,
    saveError: false,
    timer: null,
    retry: 0
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

  function li(className, text) {
    var e = document.createElement("li");
    if (className) {
      e.className = className;
    }
    e.textContent = text;
    return e;
  }

  function showMessage(text) {
    if (!text) {
      el.message.hidden = true;
      el.message.textContent = "";
      return;
    }
    el.message.textContent = text;
    el.message.hidden = false;
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
    el.reload.textContent = t("ui.reload");
    el.countsTitle.textContent = t("ui.counts");
    el.statsTitle.textContent = t("ui.stats");
    el.notice.textContent = t("ui.edit_notice");
    el.conflictTitle.textContent = t("ui.conflict_title");
    el.conflictHelp.textContent = t("ui.conflict_help");
    el.conflictKeep.textContent = t("ui.conflict_keep_mine");
    el.conflictTake.textContent = t("ui.conflict_take_file");
    el.orphansTitle.textContent = t("ui.orphans_title");
    el.filterLabel.textContent = t("ui.filter");
    el.filterClear.textContent = t("ui.filter_clear");
    el.searchLabel.textContent = t("ui.search");
    /* 入力欄の中の案内も目録から。画面に文字列を直接書かない。 */
    el.search.setAttribute("placeholder", t("ui.search_placeholder"));
    el.search.setAttribute("aria-label", t("ui.search"));
    el.finderNote.textContent = t("ui.finder_note");
    el.keys.textContent = t("ui.keys_help");
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
      item.appendChild(
        span(
          null,
          c.judged
            ? t("ui.count_value", { count: c.count })
            : t("ui.not_judged", { reason: c.reason })
        )
      );
      el.counts.appendChild(item);
    });
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
  */
  function buildFilters(counts) {
    clear(el.filters);
    (counts || []).forEach(function (c) {
      el.filters.appendChild(filterChip("cat:" + c.category, c.label, c.status));
    });
    el.filters.appendChild(filterChip("state:pending", t("ui.filter_pending"), "pending"));
    el.filters.appendChild(filterChip("state:failed", t("ui.filter_failed"), "failed"));
  }

  /* 条件1つ。選ばれているかは state.filter が持ち、組み直しても残る。 */
  function filterChip(id, label, status) {
    var wrap = document.createElement("label");
    wrap.className = "chip " + status;
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
    wrap.appendChild(span(null, label));
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

  /* 入力欄。頁に1つだけ作り、いま触っている行へ差し込む。 */
  var editor = document.createElement("input");
  editor.type = "text";
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
    訳に入れられない字を落とす。

    改行は空白へ置き換える。internal/edit は CR / LF を含む値を拒む（公開ファイルの
    読み手が1物理行=1レコードで読むため）ので、拒まれる値を送らない。入ってくるのは
    ほとんど貼り付けなので、落とすのではなく空白にしてその行に収める。
    NUL も internal/edit が拒む値なので、同じく落とす。
  */
  function sanitize(value) {
    return String(value).replace(/[\r\n]+/g, " ").replace(/\u0000/g, "");
  }

  /* いま欄に出す値。未保存があればそれ、無ければ保存済みの値。 */
  function shownValue(n, saved) {
    return state.pending.has(n) ? state.pending.get(n) : saved;
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

    未保存の訳がある行、保存できなかった行、競合で抱えている行の3つ。隠すと、
    直すべき行が画面から消え、翻訳者は消えたことに気づけない。訳を失わない
    という約束は、絞り込みより重い。
  */
  function keepAlways(n) {
    if (state.pending.has(n) || state.failed.has(n)) {
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
  */
  function applyView() {
    var q = el.search.value.toLowerCase();

    /*
      いま入力欄が開いている行が隠れるなら、先に保存してから閉じる。
      入力欄を差し込んだまま行ごと隠すと、打った訳が画面からも消える。
    */
    if (state.editing !== null) {
      var open = state.rows.get(state.editing);
      if (open && !keepAlways(open.line) &&
        !(matchesFilter(open) && matchesSearch(open, q))) {
        commitEditor();
      }
    }

    var heads = { section: null, node: null, other: null };
    var shown = 0;
    state.items.forEach(function (item) {
      if (item.heading) {
        setHidden(item.heading, true);
        if (item.level === "section") {
          /* 節が変われば、その前の節に属していた見出しはもう関わらない。 */
          heads.node = null;
          heads.other = null;
        }
        heads[item.level] = item.heading;
        return;
      }
      var entry = item.entry;
      var show = keepAlways(entry.line) ||
        (matchesFilter(entry) && matchesSearch(entry, q));
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
    entry.note.textContent = text || "";
    entry.note.hidden = !text;
  }

  function openEditor(n) {
    if (!state.canEdit || state.editing === n) {
      return;
    }
    var entry = state.rows.get(n);
    if (!entry || !entry.editable) {
      return;
    }
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
    editor.focus();
    entry.value.hidden = true;
  }

  /*
    確定して閉じる。閉じる前に必ず保存へ回す。

    欄から離れたときだけでなく、絞り込みでその行が隠れるときにもここを通す。
    editor.blur() で済ませないのは、既に焦点が他所（検索の欄など）へ移って
    いると blur が起きず、入力欄を差し込んだまま行が隠れるためである。
  */
  function commitEditor() {
    closeEditor();
    flush();
  }

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
    var entry = state.rows.get(n);
    if (entry) {
      setShownText(entry, clean);
      if (clean === entry.saved) {
        state.pending.delete(n);
      } else {
        state.pending.set(n, clean);
      }
      /* 書き換えたら「保存できない行」から外す。次の保存でまた試す。 */
      state.failed.delete(n);
      setRowNote(entry, "");
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
      var next = nextEditable(state.editing);
      commitEditor();
      if (next !== null) {
        openEditor(next);
      }
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
      if (!passed || !entry.editable || entry.row.hidden) {
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
      return;
    }
    if (state.saving || state.mine || state.pending.size === 0) {
      return;
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
    */
    var gen = state.gen;
    state.saving = true;
    updateStatus();
    postJSON("/api/rows", {
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
        showMessage(t("ui.save_failed_detail"));
        scheduleRetry();
        updateStatus();
      });
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
      state.failed.set(r.line, r.error);
      state.pending.delete(r.line);
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
            /* ファイルから読み直した値を出す。画面とファイルを同じにする。 */
            setShownText(entry, r.translation);
          }
          renderBadges(entry, r.badges);
          setRowNote(entry, r.warning ? r.warning : "");
        }
      } else {
        /*
          この行は保存できない。値は欄に残したまま、自動保存の対象からだけ外す。
          外さないと、同じ要求を投げ続けることになる。書き換えれば戻る。
        */
        state.failed.set(r.line, r.error ? r.error : "");
        state.pending.delete(r.line);
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
    409。手前でファイルが変わっている。1バイトも書かれていない。

    未保存の編集を捨てない。読み直した内容を描いたうえで、どちらを載せるかを
    人に選ばせる。選ぶまで自動保存は止める（止めないと、選ぶ前に片方が消える）。
  */
  function onConflict(body) {
    var carried = remap(state.pending, keyIndex(), body.current);
    state.mine = carried.edits;
    state.pending = new Map();
    addOrphans(carried.lost);
    render(body.current);
    showMessage(body.message ? body.message : t("ui.save_conflict"));
    el.conflict.hidden = false;
    updateStatus();
  }

  function keepMine() {
    /*
      自分の訳を、読み直した内容の上に載せ直して保存する。

      選んでいるあいだに打った訳（state.pending）のほうが新しいので、そちらを
      残す。丸ごと置き換えると、選んでいるあいだの入力が黙って消える。
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
      訳は、人が捨てると言っていないので残す。最後に flush を呼ぶのは、
      競合のあいだ止めていた自動保存をここで動かし直すためである。呼ばないと、
      その訳は別の行を触るまで送られない。
    */
    state.mine = null;
    el.conflict.hidden = true;
    showMessage("");
    render(state.data);
    flush();
  }

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
    }
    el.saveState.textContent = text;
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

    訳の欄には入力欄を常設しない。1839行ぶんの入力欄を置くと、開くだけで重くなる。
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
    if (state.mine && state.mine.has(line.n)) {
      /*
        競合中の行。ファイルの値と自分の値を両方出す。どちらを残すか選ぶのは
        人で、画面はそのための材料を並べるだけ。
      */
      row.classList.add("conflicted");
      setShownText(entry, entry.saved);
      clear(note);
      note.hidden = false;
      note.appendChild(span("note-label", t("ui.conflict_file") + ": "));
      note.appendChild(span("note-value", entry.saved));
      note.appendChild(span("note-label", " / " + t("ui.conflict_mine") + ": "));
      note.appendChild(span("note-value", state.mine.get(line.n)));
    }
    markRow(entry);
    return row;
  }

  function headingNode(line) {
    var e = document.createElement("div");
    e.className = "heading " + (line.heading || "other");
    /* ファイルにあるコメント行をそのまま出す。組み直さない。 */
    e.textContent = line.text;
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
    var fragment = document.createDocumentFragment();
    (data.lines || []).forEach(function (line) {
      if (line.kind === "heading") {
        var head = headingNode(line);
        state.items.push({ heading: head, level: headingLevel(line) });
        fragment.appendChild(head);
        return;
      }
      fragment.appendChild(rowNode(line, data.locale));
      state.items.push({ entry: state.rows.get(line.n) });
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
    el.path.textContent = t("ui.file") + ": " + data.path;
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

  function load(locale) {
    /*
      世代を1つ進める。進めておくと、送りかけの保存の応答が返ってきたときに
      「もう画面のものではない」と分かる。別ロケールの版と件数を載せない。
    */
    state.gen = state.gen + 1;
    if (state.timer) {
      clearTimeout(state.timer);
      state.timer = null;
    }
    state.retry = 0;
    state.saveError = false;
    if (!locale) {
      clear(el.list);
      el.rows.textContent = "";
      showMessage(t("ui.select_locale"));
      return;
    }
    showMessage(t("ui.loading"));
    /* URL に載せるのはロケール名だけ。原文も訳も URL には載せない。 */
    getJSON("/api/lines?locale=" + encodeURIComponent(locale))
      .then(function (data) {
        showMessage("");
        state.pending = new Map();
        state.failed = new Map();
        state.mine = null;
        state.orphans = [];
        renderOrphans();
        el.conflict.hidden = true;
        render(data);
      })
      .catch(function () {
        /*
          失敗の中身は出さない。翻訳者にできるのは読み直すことだけ。
          ロケールの欄は元に戻す。戻さないと、欄だけが新しいロケールを指した
          まま中身は前のロケール、という食い違いが画面に残る。
        */
        el.locale.value = state.locale;
        showMessage(t("ui.load_failed"));
        updateStatus();
      });
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

  /* いま焦点がある先は、字を打つところか。 */
  function isTyping(target) {
    if (!target || !target.tagName) {
      return false;
    }
    var tag = target.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") {
      return true;
    }
    return target.isContentEditable === true;
  }

  /*
    一文字の近道。スラッシュで検索の欄へ移る。

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
    if (isTyping(e.target)) {
      return;
    }
    if (e.key === "/") {
      e.preventDefault();
      el.search.focus();
      el.search.select();
    }
  });

  /*
    読み直しとロケールの切り替えは、未保存の訳を消す。消す前に必ず尋ねる。
    自動保存があるので未保存が残るのは、保存できなかったときと競合中だけである。
  */
  function confirmDiscard() {
    if (!hasUnsaved()) {
      return true;
    }
    return window.confirm(t("ui.discard_confirm"));
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
        fillLocales(data.selected);
        el.locale.addEventListener("change", function () {
          if (!confirmDiscard()) {
            el.locale.value = state.locale;
            return;
          }
          load(el.locale.value);
        });
        el.reload.addEventListener("click", function () {
          if (!confirmDiscard()) {
            return;
          }
          load(el.locale.value);
        });
        el.filterClear.addEventListener("click", function () {
          /* 条件も検索語もまとめて外す。「全部見たい」は1手でできるようにする。 */
          state.filter = new Set();
          el.search.value = "";
          buildFilters(state.data ? state.data.counts : []);
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

  boot();
})();
