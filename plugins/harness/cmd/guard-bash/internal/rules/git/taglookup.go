package git

// git push の refspec がローカルのタグを指すかの照会。
//
// `git push origin v1.2.3` は、字面だけではタグかブランチかを決められない。コマンドが動く
// リポジトリに refs/tags/<name> があるかを git に聞く。決められないとき (照会の失敗、
// どのリポジトリで動くか分からない形) は tagUnknown を返し、呼び出し側が ask に落とす。

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// lookupTimeout は照会 1 回の上限。ローカルの ref を読むだけなので短くてよい。
const lookupTimeout = 2 * time.Second

// tagState は名前がローカルのタグかの照会結果。
type tagState int

const (
	// tagUnknown は決められなかった。ゼロ値をこれにして、未設定の経路が「無い」に倒れないようにする。
	tagUnknown tagState = iota
	tagAbsent
	tagPresent
)

// workdir は照会で git を実行するディレクトリ (判定対象のコマンドが動く cwd)。
var workdir string

// disabled は照会を止めるフラグ。コマンドがどのリポジトリで動くか分からなくなった時点で立てる。
var disabled bool

// lookupTag は dir のリポジトリに refs/tags/<name> があるかを返す。テストで差し替える。
var lookupTag = localTag

// SetWorkdir は照会で git を実行するディレクトリを設定する。
// 1 回の判定の始まりでもあるので、DisableLookup の効果もここで解除する。
func SetWorkdir(dir string) {
	workdir = dir
	disabled = false
}

// DisableLookup は以降の照会を止める。cd を挟んだ後ろや GIT_DIR を代入した形で使う。
func DisableLookup() { disabled = true }

// tagStateOf は name がコマンドの動くリポジトリのタグかを返す。
func tagStateOf(name string) tagState {
	if disabled || workdir == "" {
		return tagUnknown
	}
	return lookupTag(workdir, name)
}

// localTag は dir のリポジトリに refs/tags/<name> があるかを git に聞く。
//
// for-each-ref は ref が無くても exit 0 で空を返し、リポジトリでないときだけ失敗するので、
// 「無い」と「決められない」を終了コードで分けられる。パターンは前方一致と glob でも当たるので、
// 出力を完全一致で確かめる。
func localTag(dir, name string) tagState {
	ref := "refs/tags/" + name
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	// #nosec G204 -- シェルを介さず argv で渡す。name は refs/tags/ を前置きするのでフラグに化けない。
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "for-each-ref", "--format=%(refname)", ref).Output()
	if err != nil {
		return tagUnknown
	}
	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) == ref {
			return tagPresent
		}
	}
	return tagAbsent
}

// SetTagsForTest は照会を「tags だけがタグとして在るリポジトリ」に差し替え、元へ戻す関数を返す。
func SetTagsForTest(tags ...string) (restore func()) {
	return SetRepoForTest(RepoStub{Tags: tags})
}

// SetRepoForTest は照会を stub のリポジトリに差し替え、元へ戻す関数を返す。
func SetRepoForTest(stub RepoStub) (restore func()) {
	prevTag, prevConfig, prevMerged, prevContaining := lookupTag, lookupPushConfig, lookupMerged, lookupContaining
	prevWorkdir, prevDisabled := workdir, disabled
	lookupTag = func(_, name string) tagState {
		if slices.Contains(stub.Tags, name) {
			return tagPresent
		}
		return tagAbsent
	}
	lookupPushConfig = func(string) ([]string, bool) { return stub.PushConfig, true }
	lookupMerged = func(_, src string) ([]string, bool) { return stub.Merged[src], true }
	lookupContaining = func(_, ref string) ([]string, bool) { return stub.Containing[ref], true }
	workdir, disabled = "stub", false
	return func() {
		lookupTag, lookupPushConfig, lookupMerged, lookupContaining = prevTag, prevConfig, prevMerged, prevContaining
		workdir, disabled = prevWorkdir, prevDisabled
	}
}
