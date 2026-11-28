// Package git は git のルールを持つ。hook を飛ばす操作は deny、タグの操作とリリース PR の head の
// 送信は ask にする。
//
// 判定に AST が要る git のルールは bashguard 側にある。
package git

import (
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// noVerifyMinPrefix は --no-verify と認める最短の字面。git は一意な前方一致を
// 受け付けるので、--verbose を持たないサブコマンド (am) ではここまで縮めても同じ意味になる。
// --no-verbose や --no-verify-signatures は --no-verify の前方一致にならないので巻き込まない。
const noVerifyMinPrefix = "--no-v"

// gitFlags は git のフラグ。
//
// サブコマンドの手前に置けるグローバルの bool フラグを宣言しておく。pflag は未宣言の
// フラグの直後のトークンを値として読み飛ばすので、宣言が無いと `git --no-pager commit -n`
// の commit を取りこぼす。値を取るグローバルフラグ (`-C <path>`、`-c <key>=<value>`) は
// その読み飛ばしで値ごと消えるので宣言しなくてよい。
//
// 短縮フラグは `-anm` のように束ねられるので、commit で日常的に使う短縮を値の有無まで
// 宣言する。-n は commit では --no-verify だが他では別の意味 (push --dry-run、log の件数)
// なので、--no-verify の短縮としては宣言せず有無だけを取る。
func gitFlags(fs *pflag.FlagSet) {
	for _, long := range []string{
		"no-pager", "bare", "no-optional-locks", "no-replace-objects", "no-lazy-fetch", "no-advice",
		"literal-pathspecs", "glob-pathspecs", "noglob-pathspecs", "icase-pathspecs",
	} {
		fs.Bool(long, false, "")
	}
	for _, s := range []struct{ long, short string }{
		{"paginate", "p"}, {"n", "n"}, {"all", "a"}, {"quiet", "q"}, {"verbose", "v"},
		{"signoff", "s"}, {"edit", "e"},
	} {
		fs.BoolP(s.long, s.short, false, "")
	}
	fs.StringP("message", "m", "", "")
	fs.StringP("file", "F", "", "")
}

// Rules は git の deny と ask のルール。
var Rules = &argrule.Group{
	Tool:  "git",
	Flags: gitFlags,
	Deny: []argrule.Rule{
		argrule.Check{Desc: "git ... --no-verify (前方一致の省略形を含む)", Fn: hasNoVerify},
		argrule.Check{Desc: "git commit -n", Fn: commitShortNoVerify},
	},
	Ask: []argrule.Rule{
		argrule.Check{Desc: "git tag でのタグの作成・削除 (一覧と検証を除く)", Fn: tagWrites},
		argrule.Check{
			Desc: "git push でのタグの送信と ref の削除 (--tags, --follow-tags, --mirror, --delete, tag <name>, refs/tags/, :<ref>、" +
				"ローカルのタグと同名か、タグでないと照会で確定できない refspec、" +
				"タグを足す設定 push.followTags / remote.<name>.push / remote.<name>.mirror)",
			Fn: pushTouchesTags,
		},
		argrule.Check{
			Desc: "git push でのリリース PR の head の送信 (送る履歴に、remote の main / master / 既定ブランチに無い tagpr-from-* の先端が入っている)",
			Fn:   pushLandsReleaseHead,
		},
	},
	DenyReason: "git の --no-verify (commit の -n) はフックによりブロックされました。" +
		"pre-commit や commit-msg の hook を飛ばさず、hook が落ちる原因を直してから実行し直してください",
	AskReason: "git のタグの作成・削除・送信、remote の ref の削除、リリース PR の head を含む履歴の push です。" +
		"タグの push やリリース PR のマージでリリースが動くリポジトリがあるので、実行してよいか確認してください。" +
		"リポジトリを照会して確かめられなかったとき (git リポジトリでない cwd、cd の後ろ、-C / --git-dir) もここに来ます",
}

// globalOptsWithValue は値を 1 つ取る git のグローバルオプション。
var globalOptsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true,
}

// verbArgs はグローバルオプションを読み飛ばして、git の verb とそれ以降の引数を返す。
//
// pflag の解析結果ではなく字面を見る。pflag は未宣言のフラグの直後のトークンを値として
// 読み飛ばすので、`git tag -l 'v1.*'` や `git push -u origin x` の位置引数が欠ける。
func verbArgs(cl *argrule.Cmdline) (string, []string) {
	i := slices.Index(cl.Args, "git")
	if i < 0 {
		return "", nil
	}
	args := cl.Args[i+1:]
	for j := 0; j < len(args); j++ {
		switch {
		case globalOptsWithValue[args[j]]:
			j++
		case strings.HasPrefix(args[j], "-"):
		default:
			return args[j], args[j+1:]
		}
	}
	return "", nil
}

// tagListFlags は git tag を一覧・検証に留めるフラグ。
var tagListFlags = map[string]bool{
	"-l": true, "--list": true, "--contains": true, "--no-contains": true,
	"--merged": true, "--no-merged": true, "--points-at": true, "-v": true, "--verify": true,
}

// tagWrites は git tag がタグを作るか消す形か。
//
// 削除のフラグがあるか、一覧のフラグが無いままタグ名 (位置引数) があれば書き込みとみなす。
// `git tag --sort=-v:refname` のようにフラグだけの形は一覧になる。
func tagWrites(cl *argrule.Cmdline) bool {
	verb, rest := verbArgs(cl)
	if verb != "tag" {
		return false
	}
	named := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case abbrev(a, "--delete", "--d") || shortBundleHas(a, 'd', "mFu"):
			return true
		case tagValueFlags[a]:
			// 値のトークンを一覧のフラグやタグ名として読まない。
			i++
		case tagListFlags[a], strings.HasPrefix(a, "-n"):
			return false
		case !strings.HasPrefix(a, "-"):
			named = true
		}
	}
	return named
}

// tagValueFlags は次のトークンを値として取る git tag のフラグ。
var tagValueFlags = map[string]bool{
	"-m": true, "--message": true, "-F": true, "--file": true, "-u": true, "--local-user": true,
}

// abbrev は tok が長いオプション full の前方一致の省略形か。git は一意な前方一致を受け付ける。
// min は、同じサブコマンドの他のオプションと区別が付く最短の字面。
func abbrev(tok, full, min string) bool {
	return len(tok) >= len(min) && strings.HasPrefix(full, tok)
}

// shortBundleHas は短縮フラグの束 tok (`-fd`) に c があるか。
// valued は値を取る短縮フラグで、その後ろは値なので見ない (`-mdone` の d はメッセージ)。
func shortBundleHas(tok string, c byte, valued string) bool {
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	for i := 1; i < len(tok); i++ {
		if tok[i] == c {
			return true
		}
		if strings.IndexByte(valued, tok[i]) >= 0 {
			return false
		}
	}
	return false
}

// pushArgs は git push の引数の読み取り結果。
type pushArgs struct {
	// tagForm は、字面だけでタグの送信か ref の削除と決まる形か。
	tagForm bool
	// positionals は位置引数。最初が remote で、refspec はその後ろ。
	positionals []string
	// noFollowTags は --no-follow-tags があるか (push.followTags の設定を打ち消す)。
	noFollowTags bool
	// allBranches は全てのブランチを送る形 (--all、--branches) か。
	allBranches bool
}

// refspecs は引数の refspec を返す。
func (p pushArgs) refspecs() []string {
	if len(p.positionals) < 2 {
		return nil
	}
	return p.positionals[1:]
}

// parsePush は git push の verb より後ろの引数を読む。
func parsePush(rest []string) pushArgs {
	var p pushArgs
	flagsEnded := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "tag", strings.HasPrefix(a, ":"), strings.Contains(a, "refs/tags/"),
			// 展開 (変数、コマンド置換) を含む語は argv で空文字になる (bashguard.CallArgs)。
			// 実行時にタグ名にもフラグにも複数の語にもなりうる。
			a == "":
			p.tagForm = true
		case flagsEnded || !strings.HasPrefix(a, "-"):
			p.positionals = append(p.positionals, a)
		case a == "--":
			flagsEnded = true
		case a == "-o" || a == "--push-option" || a == "--repo" || a == "--receive-pack" || a == "--exec":
			i++
		// 最短の字面は --thin、--force* 、--dry-run と区別が付くところまで。
		case abbrev(a, "--tags", "--ta"), abbrev(a, "--follow-tags", "--fol"),
			abbrev(a, "--mirror", "--m"), abbrev(a, "--delete", "--de"),
			shortBundleHas(a, 'd', "o"):
			p.tagForm = true
		case abbrev(a, "--no-follow-tags", "--no-fol"):
			p.noFollowTags = true
		case abbrev(a, "--all", "--al"), abbrev(a, "--branches", "--b"):
			p.allBranches = true
		}
	}
	return p
}

// pushTouchesTags は git push がタグを送るか、remote の ref を消す形か。
//
// --delete と :<ref> は、名前だけではタグかブランチかを決められないので両方を拾う。
// 字面で決まらない形は、コマンドの動くリポジトリを照会して決める (名前がローカルのタグか、
// 設定がタグを足すか)。照会できないときは、タグを送らないと決められないので拾う。
func pushTouchesTags(cl *argrule.Cmdline) bool {
	verb, rest := verbArgs(cl)
	if verb != "push" {
		return false
	}
	p := parsePush(rest)
	if p.tagForm {
		return true
	}
	redirected := repoRedirected(cl)
	for _, spec := range p.refspecs() {
		if refspecMayNameTag(spec, redirected) {
			return true
		}
	}
	return configMaySendTags(cl, p, redirected)
}

// configMaySendTags は、引数に現れないタグを設定が push に足しうるか。
func configMaySendTags(cl *argrule.Cmdline, p pushArgs, redirected bool) bool {
	if redirected || inlineConfigTouchesPush(cl) {
		return true
	}
	lines, ok := pushConfig()
	if !ok {
		return true
	}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, " ")
		switch {
		case key == "push.followtags":
			if configTrue(value) && !p.noFollowTags {
				return true
			}
		case strings.HasSuffix(key, ".mirror"):
			if configTrue(value) {
				return true
			}
		// 引数に refspec があるとき、git は remote.<name>.push を使わない。
		case strings.HasSuffix(key, ".push"):
			if len(p.refspecs()) == 0 && configRefspecMayNameTag(value) {
				return true
			}
		}
	}
	return false
}

// configTrue は git の設定値が真か。値の無いキーは真になる。
func configTrue(value string) bool {
	switch strings.ToLower(value) {
	case "", "true", "yes", "on", "1":
		return true
	}
	return false
}

// configRefspecMayNameTag は remote.<name>.push の refspec がタグを送りうるか。
func configRefspecMayNameTag(spec string) bool {
	if strings.Contains(spec, "refs/tags/") {
		return true
	}
	src, _, _ := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	if prefix, _, glob := strings.Cut(src, "*"); glob {
		// `refs/*` のように、refs/tags/ を覆う pattern。
		return strings.HasPrefix("refs/tags/", prefix)
	}
	return refspecMayNameTag(spec, false)
}

// inlineConfigTouchesPush は `git -c <key>=<value>` が、タグを push に足しうる設定を渡しているか。
func inlineConfigTouchesPush(cl *argrule.Cmdline) bool {
	i := slices.Index(cl.Args, "git")
	if i < 0 {
		return false
	}
	args := cl.Args[i+1:]
	for j := 0; j < len(args) && strings.HasPrefix(args[j], "-"); j++ {
		if args[j] == "-c" && j+1 < len(args) {
			key, _, _ := strings.Cut(strings.ToLower(args[j+1]), "=")
			if key == "push.followtags" || strings.HasPrefix(key, "remote.") &&
				(strings.HasSuffix(key, ".push") || strings.HasSuffix(key, ".mirror")) {
				return true
			}
		}
		if globalOptsWithValue[args[j]] {
			j++
		}
	}
	return false
}

// pushLandsReleaseHead は、git push が送る履歴にリリース PR の head が入っているか。
//
// `git merge tagpr-from-...` の後に base へ push すると、PR が merged 扱いになりリリースが動きうる。
// merge の書き方 (merge、pull、reset) は数えず、送る src から辿れるリリース PR の head のうち、
// remote のほかのブランチにまだ入っていないものがあるかで拾う。
func pushLandsReleaseHead(cl *argrule.Cmdline) bool {
	verb, rest := verbArgs(cl)
	if verb != "push" {
		return false
	}
	p := parsePush(rest)
	if p.tagForm {
		return false
	}
	if repoRedirected(cl) {
		return true
	}
	srcs, ok := pushSources(p)
	return !ok || slices.ContainsFunc(srcs, srcCarriesUnlandedReleaseHead)
}

// pushSources は push が送る src を返す。空文字は「全てのブランチ」を表す。
// refspec の無い push が何を送るかは設定で決まるので、設定を読めないときは ok が false。
func pushSources(p pushArgs) (srcs []string, ok bool) {
	if p.allBranches {
		return []string{""}, true
	}
	if len(p.refspecs()) == 0 {
		lines, ok := pushConfig()
		if !ok {
			return nil, false
		}
		for _, line := range lines {
			key, value, _ := strings.Cut(line, " ")
			// 現在のブランチ以外も送る設定。
			if key == "push.default" && value == "matching" || strings.HasSuffix(key, ".push") {
				return []string{""}, true
			}
		}
		return []string{"HEAD"}, true
	}
	for _, spec := range p.refspecs() {
		if src, _, _ := strings.Cut(strings.TrimPrefix(spec, "+"), ":"); src != "" {
			srcs = append(srcs, src)
		}
	}
	return srcs, true
}

// srcCarriesUnlandedReleaseHead は、src から辿れるリリース PR の head のうち、remote の base の
// ブランチにまだ入っていないものがあるか。照会できないときは true を返す。
//
// 「remote のどれかのブランチに入っている」では済ませない。head を含む作業ブランチを先に push
// しておくと、base へ届く次の push が通ってしまう。
func srcCarriesUnlandedReleaseHead(src string) bool {
	refs, ok := merged(src)
	if !ok {
		return true
	}
	for _, ref := range refs {
		if !isReleaseRef(ref) {
			continue
		}
		holders, ok := containing(ref)
		if !ok {
			return true
		}
		if !slices.ContainsFunc(holders, isRemoteBaseRef) {
			return true
		}
	}
	return false
}

// isRemoteBaseRef は ref が remote の base のブランチ (既定ブランチの HEAD、main、master) か。
func isRemoteBaseRef(ref string) bool {
	rest, ok := strings.CutPrefix(ref, "refs/remotes/")
	if !ok {
		return false
	}
	_, name, _ := strings.Cut(rest, "/")
	return name == "HEAD" || name == "main" || name == "master"
}

// releaseHeadPrefix は tagpr が作るリリース PR の head ブランチ名の接頭辞。
// PR のラベルはローカルの ref から分からないので、push の側は名前で見分ける。
const releaseHeadPrefix = "tagpr-from-"

// isReleaseRef は ref がリリース PR の head ブランチ (ローカルか remote 追跡 ref) か。
func isReleaseRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok {
		rest, remote := strings.CutPrefix(ref, "refs/remotes/")
		if !remote {
			return false
		}
		_, name, _ = strings.Cut(rest, "/")
	}
	return strings.HasPrefix(name, releaseHeadPrefix)
}

// refspecMayNameTag は refspec の src か dst が、タグでないと確定できない名前か。
//
// 完全修飾の ref と HEAD は字面で決まる。それ以外はローカルのタグを照会し、在るときと
// 決められないときに true を返す。dst は remote の ref に当てて解決されるが、remote は
// 照会しないので、ローカルの同名のタグで代える。
// redirected は、コマンドが cwd と別のリポジトリで動く形か (照会しても別のリポジトリを見る)。
func refspecMayNameTag(spec string, redirected bool) bool {
	// glob とブレース展開は、シェルが別の名前に置き換えうる。
	if strings.ContainsAny(spec, "*?[{") {
		return true
	}
	src, dst, _ := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	for _, name := range []string{src, dst} {
		switch {
		case name == "", name == "HEAD", name == "@", strings.HasPrefix(name, "refs/"):
			continue
		// git は tags/<name> を refs/tags/<name> に解決する。
		case strings.HasPrefix(name, "tags/"), redirected, tagStateOf(name) != tagAbsent:
			return true
		}
	}
	return false
}

// repoRedirected は git のグローバルオプションが、cwd と別のリポジトリを指しているか。
func repoRedirected(cl *argrule.Cmdline) bool {
	i := slices.Index(cl.Args, "git")
	if i < 0 {
		return false
	}
	args := cl.Args[i+1:]
	for j := 0; j < len(args) && strings.HasPrefix(args[j], "-"); j++ {
		for _, opt := range []string{"-C", "--git-dir", "--work-tree", "--namespace"} {
			if args[j] == opt || strings.HasPrefix(args[j], opt+"=") {
				return true
			}
		}
		if globalOptsWithValue[args[j]] {
			j++
		}
	}
	return false
}

// hasNoVerify は git の引数に --no-verify (の省略形) があるか。サブコマンドは問わない。
//
// pflag の解析結果ではなく字面を見るのは、git が受け付ける前方一致の省略形を pflag が
// 別名として扱わないため。`--` より後ろはパスなので見ない。
func hasNoVerify(cl *argrule.Cmdline) bool {
	i := slices.Index(cl.Args, "git")
	if i < 0 {
		return false
	}
	for _, tok := range cl.Args[i+1:] {
		if tok == "--" {
			return false
		}
		if len(tok) >= len(noVerifyMinPrefix) && strings.HasPrefix("--no-verify", tok) {
			return true
		}
	}
	return false
}

// commitShortNoVerify は `git commit -n` か。
func commitShortNoVerify(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "git", func(v *argrule.View) bool {
		return len(v.Pos) > 0 && v.Pos[0] == "commit" && v.Changed("n")
	})
}
