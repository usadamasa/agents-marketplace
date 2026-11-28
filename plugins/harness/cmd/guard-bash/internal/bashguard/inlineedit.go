package bashguard

// Bash 経由のファイル書き換えガード。
//
// エージェントは auto mode の指示に引かれて heredoc・リダイレクト・sed -i で
// ファイルを書き換えることがある。これは差分が確認できず、既存の内容を読まずに
// 上書きしやすく、失敗しても部分適用の形で残る。ファイルの変更は Edit / Write
// ツールに寄せ、shell からの書き込みは一時ディレクトリ配下だけに許す。
//
// 検出するのは 3 つ。
//  1. `>` / `>>` の宛先が一時ディレクトリでも fd 複製でもないパス
//  2. `tee` の引数が同上のパス
//  3. in-place 編集コマンド (sed -i / perl -i / ruby -i)
//
// heredoc 自体は禁止しない。`crit ... <<'EOF'` のように標準入力へ渡す形は通す。
// 判定に引っかかるのは heredoc の出力がファイルへ向いているときだけ。

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const inlineFileEditReason = "「%s」 のように shell からファイルを書き換える形はフックによりブロックされました。" +
	"ファイルの変更は Edit ツール (既存ファイルの一部を直す) か Write ツール (新規作成・全体の置き換え) で行ってください。" +
	"差分が残り、読まずに上書きする事故を防げます。" +
	"コマンドの出力を保存したいだけなら ./tmp/ 配下 ($TMPDIR と ~/tmp/ も可) へリダイレクトすれば通ります。" +
	"heredoc をコマンドの標準入力へ渡す形 (cmd ... <<'EOF') は禁止していません。"

// InlineFileEdit は shell からのファイル書き換えを見つけたら deny を返す。
func InlineFileEdit(src string, f *syntax.File) *Verdict {
	var hit string
	syntax.Walk(f, func(n syntax.Node) bool {
		if hit != "" {
			return false
		}
		switch node := n.(type) {
		case *syntax.Redirect:
			if target := redirectTarget(src, node); target != "" {
				hit = "> " + target
			}
		case *syntax.CallExpr:
			hit = callWriteTarget(src, node)
		}
		return hit == ""
	})
	if hit == "" {
		return nil
	}
	return Deny(inlineFileEditReason, hit)
}

// redirectTarget は出力リダイレクトの宛先のうち、許可されないパスを返す。
// fd 複製 (`2>&1`) とプロセス置換 (`> >(cmd)`) はファイルではないので対象外。
func redirectTarget(src string, r *syntax.Redirect) string {
	switch r.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.RdrClob:
	default:
		return ""
	}
	if r.Word == nil || isProcSubst(r.Word) {
		return ""
	}
	target := WordText(src, r.Word)
	if isExemptPath(target) {
		return ""
	}
	return unquote(target)
}

// callWriteTarget は簡単コマンドが書き込みを起こす形なら、その説明を返す。
func callWriteTarget(src string, call *syntax.CallExpr) string {
	name := CommandName(call)
	switch name {
	case "tee":
		for _, arg := range call.Args[1:] {
			text := WordText(src, arg)
			if strings.HasPrefix(text, "-") {
				continue
			}
			if !isExemptPath(text) {
				return "tee " + unquote(text)
			}
		}
	case "sed", "perl", "ruby":
		if !inPlaceFlag(src, call) {
			return ""
		}
		// 一時ディレクトリを対象にした in-place 編集は通す
		for _, arg := range call.Args[1:] {
			if isExemptPath(WordText(src, arg)) {
				return ""
			}
		}
		return name + " -i"
	}
	return ""
}

// inPlaceFlag は in-place 編集のフラグが付いているか。
// 短いフラグは結合形を取る (sed -i.bak / perl -pi)。i を持つ短いフラグは
// sed / perl / ruby のいずれでも in-place しか無いので、含むかどうかだけを見る。
func inPlaceFlag(src string, call *syntax.CallExpr) bool {
	for _, arg := range call.Args[1:] {
		text := WordText(src, arg)
		if text == "--in-place" {
			return true
		}
		if len(text) < 2 || text[0] != '-' || text[1] == '-' {
			continue
		}
		if strings.ContainsRune(text[1:], 'i') {
			return true
		}
	}
	return false
}

// isProcSubst は語がプロセス置換 (`>(cmd)`) か。
func isProcSubst(w *syntax.Word) bool {
	if len(w.Parts) != 1 {
		return false
	}
	_, ok := w.Parts[0].(*syntax.ProcSubst)
	return ok
}

// isExemptPath は書き込み先として許可するパスか。
// この guard が書き込みを許す一時ファイルの置き場 (./tmp、$TMPDIR、~/tmp)。
//
// 判定はコマンドの字面に対して行い、チルダも変数も展開しない。そのため ~/tmp は
// 書き手が実行しうる 3 形をそれぞれ並べる。端末ごとに変わる絶対パスの字面は受けない。
// どのプレフィックスも末尾を "/" で切る。"~/tmp" 単体だと ~/tmp-foo まで通る。
func isExemptPath(p string) bool {
	p = unquote(p)
	switch p {
	case "/dev/null", "/dev/stdout", "/dev/stderr", "/dev/tty":
		return true
	}
	// 途中の .. は置き場の外へ抜けられるので、前方一致を見る前に落とす。
	// 先頭の "../tmp/" は許可プレフィックスなので、区切り付きの形だけを見る。
	if strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") {
		return false
	}
	for _, prefix := range []string{
		"/dev/fd/",
		"./tmp/", "tmp/", "../tmp/",
		"/tmp/", "/private/tmp/",
		"$TMPDIR/", "${TMPDIR}/",
		"~/tmp/", "$HOME/tmp/", "${HOME}/tmp/",
	} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// unquote は字面からクォート文字だけを落とす。
func unquote(s string) string {
	return strings.NewReplacer(`"`, "", `'`, "").Replace(s)
}
