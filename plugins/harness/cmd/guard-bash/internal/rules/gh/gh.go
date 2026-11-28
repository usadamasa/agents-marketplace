// Package gh は gh CLI の書き込み操作を判定するルールを持つ。
//
// rules.Evaluate が他の CLI のルールと合わせて評価する。opt-in で ask を
// 飛ばしてよい操作の判定 (PRWriteOptIn / IssueWriteOptIn) もここに置く。
package gh

import (
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/ghauthor"
)

// ghFlags は gh 全体で意味の変わらないフラグ。
//
// 短縮フラグはサブコマンドごとに意味が違う (`-f` は gh api では --raw-field で値を
// 取るが、gh pr create では --fill、gh pr checkout では --force で値を取らない)。
// 値の有無を取り違えると解析に失敗するので、ここでは意味の揃った短縮だけを宣言し、
// gh api の短縮フラグは apiFlags で解析し直す。`-t` も同じ理由で宣言しない
// (`gh auth status -t` は --show-token、`gh pr create -t` は --title)。
func ghFlags(fs *pflag.FlagSet) {
	// pflag は未宣言の -h を help の要求として扱い、解析を打ち切る。gh の -h は
	// --hostname なので宣言しておく (末尾の -h は Evaluate が先に help として通す)。
	fs.StringP("hostname", "h", "", "")
	fs.StringP("repo", "R", "", "")
	// --attach は短縮形が無い (-a は --assignee)。
	fs.StringArray("attach", nil, "")
	fs.Bool("admin", false, "")
}

// apiFlags は gh api のフラグ。
func apiFlags(fs *pflag.FlagSet) {
	ghFlags(fs)
	fs.StringP("method", "X", "", "")
	fs.StringArrayP("field", "F", nil, "")
	fs.StringArrayP("raw-field", "f", nil, "")
	fs.String("input", "", "")
	fs.StringArrayP("header", "H", nil, "")
	fs.StringP("jq", "q", "", "")
	fs.StringP("template", "t", "", "")
	fs.StringP("preview", "p", "", "")
	fs.Bool("paginate", false, "")
	fs.Bool("slurp", false, "")
	fs.Bool("silent", false, "")
	fs.BoolP("include", "i", false, "")
	fs.Bool("verbose", false, "")
}

// apiView は argv[0] が `gh api` のときの解析結果を、gh api のフラグで解析し直して返す。
// それ以外は nil。
func apiView(cl *argrule.Cmdline) *argrule.View {
	if cl.Args[0] != "gh" {
		return nil
	}
	if v := cl.HeadView(); v == nil || len(v.Pos) < 2 || v.Pos[0] != "api" {
		return nil
	}
	v := argrule.NewCmdline(cl.Args, func(string) argrule.FlagSpec { return apiFlags }).HeadView()
	if v == nil || len(v.Pos) < 2 {
		return nil
	}
	return v
}

// apiMethod は明示された HTTP メソッドを返す。省略時は空文字。
func apiMethod(v *argrule.View) string {
	m, _ := v.FS.GetString("method")
	return m
}

// apiHasBody はリクエストのフィールドか本文を伴うか。明示 GET でなければ
// gh はメソッドを POST にする。
func apiHasBody(v *argrule.View) bool {
	return v.Changed("field", "raw-field", "input")
}

// apiFields は -F / -f で渡したフィールドを全て返す。
func apiFields(v *argrule.View) []string {
	f, _ := v.FS.GetStringArray("field")
	r, _ := v.FS.GetStringArray("raw-field")
	return append(f, r...)
}

// apiPath はエンドポイントのパスを `/` で分けて返す。先頭の `/` は gh と同じく読み飛ばす。
func apiPath(v *argrule.View) []string {
	return strings.Split(strings.TrimPrefix(v.Pos[1], "/"), "/")
}

// isPRPath は path が repos/{owner}/{repo}/pulls/{n}/<rest...> の形かを返す。
func isPRPath(path []string, rest ...string) bool {
	if len(path) != 5+len(rest) || path[0] != "repos" || path[3] != "pulls" || !isNumber(path[4]) {
		return false
	}
	for i, r := range rest {
		if r == "{id}" {
			if !isNumber(path[5+i]) {
				return false
			}
			continue
		}
		if path[5+i] != r {
			return false
		}
	}
	return path[1] != "" && path[2] != ""
}

func isNumber(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// methodIsPOSTOrOmitted はメソッドを書くなら POST だけか。省略形はフィールドを伴うので POST になる。
func methodIsPOSTOrOmitted(v *argrule.View) bool {
	m := apiMethod(v)
	return m == "" || m == "POST"
}

// isPRReviewWrite は PR review の書き込みのうち、本文を投稿・編集するだけの形か。
// 非 GET の gh api を一律 deny する規則から外して ask に落とす。
//
//   - POST .../pulls/{n}/comments (in_reply_to での返信を含む) と .../comments/{id}/replies
//   - POST .../pulls/{n}/reviews と .../reviews/{id}/events (submit)
//   - PUT .../pulls/{n}/reviews/{id} (本文の更新)
//   - PATCH / DELETE .../pulls/comments/{id}
//
// 他人の review を取り下げさせる .../reviews/{id}/dismissals と、
// pending review の削除 (DELETE .../reviews/{id}) は対象外 (deny のまま)。
func isPRReviewWrite(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil || !apiWrites(cl) {
		return false
	}
	path := apiPath(v)
	switch apiMethod(v) {
	case "", "POST":
		return isPRPath(path, "comments") || isPRPath(path, "comments", "{id}", "replies") ||
			isPRPath(path, "reviews") || isPRPath(path, "reviews", "{id}", "events")
	case "PUT":
		return isPRPath(path, "reviews", "{id}")
	case "PATCH", "DELETE":
		return isReviewCommentPath(path)
	}
	return false
}

// isReviewCommentPath は path が repos/{owner}/{repo}/pulls/comments/{id} の形か
// (pulls の後ろが PR 番号ではなく comments)。
func isReviewCommentPath(path []string) bool {
	return len(path) == 6 && path[0] == "repos" && path[1] != "" && path[2] != "" &&
		path[3] == "pulls" && path[4] == "comments" && isNumber(path[5])
}

// reviewCommentByOther は review comment の編集・削除で、コメントを書いた人が自分以外だと
// 確定したか。管理者は他人のコメントも消せるので、OthersRules の deny の事前条件に使う。
// --hostname で別ホストを指す形は照会先がずれるので確定させない (Rules.Ask へ落ちる)。
func reviewCommentByOther(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil || v.Changed("hostname") || !slices.Contains([]string{"PATCH", "DELETE"}, apiMethod(v)) {
		return false
	}
	path := apiPath(v)
	return isReviewCommentPath(path) && ghauthor.ReviewCommentByOther(path[1]+"/"+path[2], path[5])
}

// apiWriteHint は非 GET の gh api を止めたときに返す、確認を経て通せる書き込みの形。
const apiWriteHint = "gh api の書き込みで確認を経て通るのは、PR review (POST .../pulls/{n}/comments・" +
	".../pulls/{n}/reviews・.../reviews/{id}/events、PUT .../reviews/{id}、PATCH / DELETE .../pulls/comments/{id})、" +
	"sub-issue の登録・並べ替え、Discussion の updateDiscussion mutation です。" +
	"issue や PR の作成・編集は gh issue / gh pr のサブコマンドを使ってください"

// isSubIssueWrite は sub-issue の登録 (POST repos/{o}/{r}/issues/{n}/sub_issues、
// フィールドは sub_issue_id だけ) か並べ替え (PATCH .../sub_issues/priority、
// フィールドは sub_issue_id と after_id | before_id の一方だけ) で、値が全て数値の形か。
// 登録は gh issue edit --add-sub-issue と同じ操作で、並べ替えは同じ親の配下の順序を
// 変えるだけなので、どちらも同じ基準で振り分ける。既存の親から付け替える replace_parent は対象外。
func isSubIssueWrite(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil || v.Changed("input") {
		return false
	}
	path := apiPath(v)
	if len(path) < 6 || path[0] != "repos" || path[3] != "issues" || !isNumber(path[4]) || path[5] != "sub_issues" {
		return false
	}
	fields := apiFields(v)
	switch {
	case len(path) == 6 && methodIsPOSTOrOmitted(v):
		return numericFields(fields, "sub_issue_id")
	case len(path) == 7 && path[6] == "priority" && apiMethod(v) == "PATCH":
		return numericFields(fields, "sub_issue_id", "after_id") || numericFields(fields, "sub_issue_id", "before_id")
	}
	return false
}

// numericFields は fields が keys を 1 つずつ数値の値で並べた形か (順不同)。
func numericFields(fields []string, keys ...string) bool {
	if len(fields) != len(keys) {
		return false
	}
	for _, k := range keys {
		if !slices.ContainsFunc(fields, func(f string) bool {
			val, ok := strings.CutPrefix(f, k+"=")
			return ok && isNumber(val)
		}) {
			return false
		}
	}
	return true
}

// subIssueParentBySelf は sub-issue を登録・並べ替えする親の issue を自分が作ったと確定したか。
// パスの owner/repo と番号を gh issue edit の形に組み直して作成者を照会する
// ({owner} のようなプレースホルダは repo の形に当たらず、確定しない)。
func subIssueParentBySelf(cl *argrule.Cmdline) bool {
	path := apiPath(apiView(cl))
	return ghauthor.AuthoredBySelf([]string{"gh", "issue", "edit", path[4], "--repo", path[1] + "/" + path[2]})
}

// notAPIWriteException は deny の事前条件。PR review の書き込み、
// sub-issue の登録・並べ替え、Discussion の updateDiscussion mutation のどれでもない
// 形だけを deny に残す (当たる形は下の ask が受け止める)。
func notAPIWriteException(cl *argrule.Cmdline) bool {
	return !isPRReviewWrite(cl) && !isSubIssueWrite(cl) && !isDiscussionMutation(cl)
}

// apiWrites は gh api が非 GET のリクエストを送る形か。
func apiWrites(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil {
		return false
	}
	switch apiMethod(v) {
	case "GET":
		return false
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return apiHasBody(v)
}

// apiUnsubscribe は issue / PR の購読解除 (DELETE .../issues/{n}/subscription) か。
func apiUnsubscribe(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil || apiMethod(v) != "DELETE" {
		return false
	}
	path := apiPath(v)
	n := len(path)
	return n >= 3 && path[n-3] == "issues" && isNumber(path[n-2]) && path[n-1] == "subscription"
}

// apiIgnoreThread は notification thread の購読を ignored=true にするだけの形か。
//
// DELETE .../issues/.../subscription は review_requested 等の participant 起因の
// 暗黙購読を解除できない (issues/{n}/subscription への PUT も 404 で機能しないことを確認済み)。
// 実際に Unsubscribe と等価な効果があるのは notification thread 単位の
// PUT /notifications/threads/{id}/subscription {"ignored":true} のみ。
// ボディを ignored=true 単体に限定して許可する。他のフラグが付く形は通さない。
func apiIgnoreThread(cl *argrule.Cmdline) bool {
	if cl.Args[0] != "gh" {
		return false
	}
	fs, pos, ok := argrule.ParseStrict(cl.Args[1:], func(fs *pflag.FlagSet) {
		fs.StringP("method", "X", "", "")
		fs.StringArrayP("field", "F", nil, "")
	})
	if !ok || len(pos) != 2 || pos[0] != "api" {
		return false
	}
	method, _ := fs.GetString("method")
	fields, _ := fs.GetStringArray("field")
	path := strings.Split(pos[1], "/")
	return method == "PUT" && slices.Equal(fields, []string{"ignored=true"}) &&
		len(path) == 4 && path[0] == "notifications" && path[1] == "threads" &&
		isNumber(path[2]) && path[3] == "subscription"
}

// authStatusReadOnly は認証状態を参照するだけの `gh auth status` か。
// -t/--show-token や整形用のフラグは token を出力できるため、--active と
// --hostname 以外のフラグが付く形は通さない。
func authStatusReadOnly(cl *argrule.Cmdline) bool {
	if cl.Args[0] != "gh" {
		return false
	}
	fs, pos, ok := argrule.ParseStrict(cl.Args[1:], func(fs *pflag.FlagSet) {
		fs.BoolP("active", "a", false, "")
		fs.StringP("hostname", "h", "", "")
	})
	if !ok || !slices.Equal(pos, []string{"auth", "status"}) {
		return false
	}
	host, _ := fs.GetString("hostname")
	return !fs.Changed("hostname") || isHostname(host)
}

// isHostname は英数字で始まり、英数字・ドット・ハイフンだけの値か。
func isHostname(s string) bool {
	return s != "" && s[0] != '.' && s[0] != '-' &&
		strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-") == ""
}

// hasAttach は --attach (gh 2.100.0 の画像・動画のアップロード) を伴うか。
func hasAttach(cl *argrule.Cmdline) bool {
	v := cl.HeadView()
	return v != nil && v.Changed("attach")
}

// noAttach は hasAttach の否定。allow と opt-in へ「--attach が無いこと」を
// 事前条件として付けるために使う。
func noAttach(cl *argrule.Cmdline) bool { return !hasAttach(cl) }

// inProject は対象がプロジェクトのリポジトリだと確定したか。opt-in の事前条件に付ける。
func inProject(cl *argrule.Cmdline) bool { return ghauthor.InProject(cl.Args) }

func authoredBySelf(cl *argrule.Cmdline) bool  { return ghauthor.AuthoredBySelf(cl.Args) }
func authoredByOther(cl *argrule.Cmdline) bool { return ghauthor.AuthoredByOther(cl.Args) }
func authoredByBot(cl *argrule.Cmdline) bool   { return ghauthor.AuthoredByBot(cl.Args) }

// confirmedNotReleasePR は対象がリリース PR でないと確定したか。opt-in の事前条件に付ける。
func confirmedNotReleasePR(cl *argrule.Cmdline) bool { return ghauthor.ConfirmedNotReleasePR(cl.Args) }

// closeDeletesBranch は `gh pr close` が --delete-branch (-d) を伴うか。
//
// pflag の解析結果ではなく字面を見る。-d を ghFlags に宣言すると、-d が値を取る
// 別のサブコマンド (gh repo create -d <description>) の位置引数がずれる。
// gh pr close の短縮フラグは -d のほか値を取る -c と -R だけなので、束の中で
// その手前に d があれば --delete-branch として読む。-c の値が -d のような形は
// 取り違えるが、ask に倒れるだけで済む。
func closeDeletesBranch(cl *argrule.Cmdline) bool {
	if len(cl.Args) < 3 || cl.Args[2] != "close" {
		return false
	}
	for _, tok := range cl.Args[3:] {
		if tok == "--" {
			return false
		}
		if tok == "--delete-branch" || strings.HasPrefix(tok, "--delete-branch=") {
			return true
		}
		if strings.HasPrefix(tok, "--") || !strings.HasPrefix(tok, "-") {
			continue
		}
		if i := strings.IndexAny(tok[1:], "dcR"); i >= 0 && tok[1+i] == 'd' {
			return true
		}
	}
	return false
}

// textOrAssigneeEdit は文面 (title / body) と assignee の変更、issue への sub-issue の
// 登録だけの gh pr/issue edit か。base / label / milestone / reviewer / project など
// 他のフラグが混ざる形と、複数の issue を一度に編集する形は ask に落とす。
//
// 短縮フラグ -t / -b / -F は pr edit と issue edit で同じ意味 (title / body / body-file)
// なので、この strict な解析の中では宣言してよい (全体の ghFlags で避けている理由は
// サブコマンドごとに意味が違うため)。-R / --repo は別リポジトリの対象を指す形で、
// 作成者の照会 (ghauthor) が同じフラグを読んでそのリポジトリを見るので受け付ける。
func textOrAssigneeEdit(cl *argrule.Cmdline) bool {
	if cl.Args[0] != "gh" {
		return false
	}
	fs, pos, ok := argrule.ParseStrict(cl.Args[1:], func(fs *pflag.FlagSet) {
		fs.StringP("repo", "R", "", "")
		fs.StringP("title", "t", "", "")
		fs.StringP("body", "b", "", "")
		fs.StringP("body-file", "F", "", "")
		fs.StringArray("add-assignee", nil, "")
		fs.StringArray("remove-assignee", nil, "")
		fs.StringArray("add-sub-issue", nil, "")
	})
	if !ok || len(pos) < 2 || len(pos) > 3 || (pos[0] != "pr" && pos[0] != "issue") || pos[1] != "edit" {
		return false
	}
	if fs.Changed("add-sub-issue") && pos[0] != "issue" {
		return false
	}
	return fs.Changed("title") || fs.Changed("body") || fs.Changed("body-file") ||
		fs.Changed("add-assignee") || fs.Changed("remove-assignee") || fs.Changed("add-sub-issue")
}

// subIssueAddOnly は issue への sub-issue の登録だけの gh issue edit か。
// 他人の issue でも deny にせず ask に残すため、OthersRules の事前条件で使う
// (gh api の登録が他人の issue で ask になるのと揃える)。
func subIssueAddOnly(cl *argrule.Cmdline) bool {
	if cl.Args[0] != "gh" {
		return false
	}
	fs, pos, ok := argrule.ParseStrict(cl.Args[1:], func(fs *pflag.FlagSet) {
		fs.StringP("repo", "R", "", "")
		fs.StringArray("add-sub-issue", nil, "")
	})
	return ok && len(pos) == 3 && pos[0] == "issue" && pos[1] == "edit" && fs.Changed("add-sub-issue")
}

const subIssueWriteDesc = "gh api で sub-issue を登録 (POST .../sub_issues -F sub_issue_id=<id> だけ) | " +
	"並べ替え (PATCH .../sub_issues/priority -F sub_issue_id=<id> と -F after_id | before_id=<id> だけ)"

// Rules は gh の書き込み操作を deny / ask に振り分ける。
var Rules = &argrule.Group{
	Tool:  "gh",
	Flags: ghFlags,
	Allow: []argrule.Rule{
		argrule.Check{Desc: "gh auth status [--active] [--hostname <host>]", Fn: authStatusReadOnly},
		// PR 作成 / PR コメント / Issue コメントは日常操作として ask をスキップして通す。
		// --attach が付いた形はローカルのファイルを GitHub へ上げるので対象外にし、
		// AttachRules の ask へ落とす。
		argrule.When{
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"pr"}, Verbs: []string{
				"comment",
				"create",
			}},
			Cond: noAttach,
			Note: "--attach を伴わない",
		},
		argrule.When{
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"issue", "comment"}},
			Cond: noAttach,
			Note: "--attach を伴わない",
		},
		// gh stack: 対象は自分が積んだスタックに限られ、リモート操作も git push /
		// git rebase 相当なので通す。PR を一括マージする merge のみ列挙から外し、
		// 汎用 ask 側で拾わせる。
		argrule.CmdAt{Tool: "gh", Path: []string{"stack"}, Exact: true, Verbs: []string{
			"add",
			"adopt",
			"alias",
			"bottom",
			"down",
			"feedback",
			"init",
			"link",
			"push",
			"rebase",
			"submit",
			"switch",
			"sync",
			"top",
			"trunk",
			"up",
			"view",
		}},
		// 文面 (title / body) と assignee の変更だけの gh pr/issue edit は通す。
		// 自分の PR / issue の本文を書き直すのは日常操作で、opt-in の無い
		// プロジェクトでも確認を挟まない。--body-file の中身が GitHub へ出るのは
		// allow 済みの `gh pr create --body-file` と同じ露出。
		//
		// 自分が作った PR / issue だと確定したときに限る。他人のものは
		// OthersRules で deny に落とすが、allow は deny より先に評価されるので
		// ここで通してしまうと deny へ届かない。作成者を特定できない形 (番号を
		// 書かない `gh pr edit --body ...` 等) も allow から外れて ask になる。
		// 対象は -R / URL で指した別リポジトリでもよい (作成者の照会がそのリポジトリを見る)。
		argrule.When{
			Rule: argrule.Check{Desc: "gh pr | issue edit <target> --title | --body | --body-file | --add-assignee | --remove-assignee (issue は --add-sub-issue も)", Fn: textOrAssigneeEdit},
			Cond: authoredBySelf,
			Note: "自分が作成したもの",
		},
		// -X GET / --method GET を明示した gh api は read-only。この形の -f/-F/--field は
		// リクエストボディではなく query parameter になるため、下の「非 GET 判定」から外す
		// (`gh api -X GET search/repositories -f q=...` が deny されていた)。
		// 機微情報を返すエンドポイントは GET でも Deny の apiSensitivePath へ落とす。
		argrule.When{
			Rule: argrule.Check{Desc: "gh api --method GET", Fn: func(cl *argrule.Cmdline) bool {
				v := apiView(cl)
				return v != nil && apiMethod(v) == "GET"
			}},
			Cond: func(cl *argrule.Cmdline) bool { return !apiSensitivePath(cl) },
			Note: "機微情報を返すエンドポイントを除く",
		},
		argrule.Check{Desc: "gh api graphql -F query=@<file> (Project の item 追加・フィールド更新の mutation だけ。変数は -f か、@ で始まらない -F)", Fn: isProjectMutation},
		argrule.Check{Desc: "gh api graphql -f query=... | -F query=@<file> (機微フィールドを選ばない query operation だけ。変数は -f か、@ で始まらない -F)", Fn: isReadOnlyQuery},
		argrule.Check{Desc: "gh api --method DELETE .../issues/{n}/subscription", Fn: apiUnsubscribe},
		argrule.Check{Desc: "gh api --method PUT notifications/threads/{id}/subscription -F ignored=true", Fn: apiIgnoreThread},
		// 自分が作った issue への sub-issue の登録・並べ替えは、上の gh issue edit --add-sub-issue の
		// allow と揃えて通す。作成者を確定できない形は Ask の isSubIssueWrite へ落ちる。
		argrule.When{
			Rule: argrule.Check{Desc: subIssueWriteDesc, Fn: isSubIssueWrite},
			Cond: subIssueParentBySelf,
			Note: "親の issue が自分の作成したもの",
		},
	},
	Deny: []argrule.Rule{
		argrule.CmdAt{Tool: "gh", Path: []string{"auth"}},
		argrule.CmdAt{Tool: "gh", Path: []string{"repo|gist", "delete"}},
		argrule.CmdAt{Tool: "gh", Path: []string{"release", "delete"}},
		argrule.CmdAt{Tool: "gh", Path: []string{"secret"}, Verbs: []string{
			"delete",
			"set",
		}},
		// list は apiSensitivePath で止める user/keys などと同じ鍵を返す。
		argrule.CmdAt{Tool: "gh", Path: []string{"gpg-key|ssh-key"}, Verbs: []string{
			"add",
			"delete",
			"list",
		}},
		argrule.Check{Desc: "gh api で機微情報 (メールアドレス・鍵・secret・token・webhook の設定) を返すエンドポイント", Fn: apiSensitivePath},
		// PR review の書き込み、sub-issue の登録・並べ替えは下の ask が受け止めるので、
		// ここでは落とさない (deny に事前条件を付けてよいのは ask の落とし先があるとき)。
		argrule.Hinted{
			Rule: argrule.When{
				Rule: argrule.Check{Desc: "gh api の非 GET (メソッドの明示か、フィールド・--input の付与)", Fn: apiWrites},
				Cond: notAPIWriteException,
				Note: "PR review の書き込み、sub-issue の登録・並べ替え、" +
					"Discussion の updateDiscussion mutation を除く",
			},
			Hint: apiWriteHint,
		},
	},
	Ask: []argrule.Rule{
		argrule.Check{Desc: "gh api で PR review を書き込む (comment の作成・返信・編集・削除、review の作成・submit・本文の更新)", Fn: isPRReviewWrite},
		argrule.Check{Desc: subIssueWriteDesc, Fn: isSubIssueWrite},
		argrule.Check{Desc: "gh api graphql -f query=... | -F query=@<file> (Discussion の title / body を書き換える updateDiscussion mutation だけ。変数 (id・title・body 等) の値は問わない)", Fn: isDiscussionMutation},
		// noun 直後の verb (gh <noun> <verb>) のみ判定する。verb をフラグの値から
		// 探すと --json createdAt や --label needs-review が誤マッチする。
		// verb がさらに深い gh サブコマンドは漏れる。
		argrule.CmdAt{Tool: "gh", Nouns: argrule.NounOne, Verbs: []string{
			"add",
			"archive",
			"cancel",
			"close",
			"comment",
			"copy",
			"create",
			"delete",
			"develop",
			"edit",
			"fork",
			"link",
			"lock",
			"merge",
			"pin",
			"ready",
			"rename",
			"reopen",
			"review",
			"run",
			"set",
			"sync",
			"transfer",
			"unlock",
			"unpin",
			"upload",
		}},
	},
	DenyReason: "gh の危険な操作 (認証・リポジトリ/リリース削除・シークレット/キー操作・非 GET の gh api) をブロックしました",
	AskReason:  "gh の書き込み操作です。実行してよいか確認してください",
}

// AttachRules は --attach (gh 2.100.0) を伴う PR / issue の書き込みを ask にする。
// Rules と別の group にしてあるのは、専用の askReason を返すため。アップロードする
// ファイルを名指しした確認にしないと、ask を見たときにどのファイルが出ていくのか
// 分からない。
//
// Rules より前に評価させる。ask は最初に一致した group の reason を返すので、後ろに
// 置くと汎用の「gh の書き込み操作です」に飲まれる。
//
// allow の側で --attach を除外しておかないとここへ届かない (allow は ask より
// 先に評価される)。opt-in からも外してある (PROptIn / IssueOptIn)。
var AttachRules = &argrule.Group{
	Tool: "gh",
	Ask: []argrule.Rule{
		argrule.When{
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"pr|issue"}, Exact: true, Verbs: []string{
				"comment",
				"create",
				"edit",
			}},
			Cond: hasAttach,
			Note: "--attach を伴う",
		},
	},
	AskReason: "ローカルのファイルを GitHub へアップロードします (gh --attach)。" +
		"上げてよいファイルか確認してください",
}

// OthersRules は他人が作った PR / issue への書き込みを止める。Rules と別の
// group にしてあるのは、専用の denyReason を返すため。
//
// deny に置くので opt-in では覆せない (opt-in が触れるのは ask に落ちたコマンドだけ)。
// 作成者が自分以外だと確定したときにだけ deny し、特定できない形は Rules.Ask が
// 受け止める。comment は他人のものへも通す (allow 側) ので、ここでは列挙しない。
var OthersRules = &argrule.Group{
	Tool: "gh",
	Deny: []argrule.Rule{
		argrule.When{
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"pr"}, Exact: true, Verbs: []string{
				"close",
				"edit",
				"ready",
				"reopen",
			}},
			Cond: authoredByOther,
			Note: "他人が作成したもの",
		},
		argrule.When{
			// merge だけは bot (dependabot 等の GitHub App) の PR を除く。
			// dependabot の `@dependabot merge` コマンドは廃止済みで、他に
			// 手段が無いユーザーの手動マージまで deny で塞ぐと詰む。bot の
			// PR に限り、Rules.Ask の確認プロンプトへ委ねる。
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"pr"}, Exact: true, Verbs: []string{
				"merge",
			}},
			Cond: func(cl *argrule.Cmdline) bool { return authoredByOther(cl) && !authoredByBot(cl) },
			Note: "他人が作成したもの (bot は Rules.Ask へ委ねる)",
		},
		argrule.When{
			// create は対象がまだ無いので列挙しない。delete / transfer は自分の
			// issue でも ask のままだが、他人のものはここで止める。sub-issue の
			// 登録だけの edit は Rules.Ask へ委ねる。
			Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"issue"}, Exact: true, Verbs: []string{
				"close",
				"delete",
				"edit",
				"lock",
				"pin",
				"reopen",
				"transfer",
				"unlock",
				"unpin",
			}},
			Cond: func(cl *argrule.Cmdline) bool { return authoredByOther(cl) && !subIssueAddOnly(cl) },
			Note: "他人が作成したもの (sub-issue の登録だけの edit は Rules.Ask へ委ねる)",
		},
		argrule.When{
			Rule: argrule.Check{Desc: "gh api --method PATCH | DELETE repos/{owner}/{repo}/pulls/comments/{id}", Fn: isPRReviewWrite},
			Cond: reviewCommentByOther,
			Note: "他人が書いた review comment",
		},
	},
	DenyReason: "他人が作成した PR / issue / review comment への書き込みです。作成者本人に依頼してください",
}

// PROptIn は環境変数 CLAUDE_ALLOW_GH_PR_WRITES による opt-in が有効なとき、
// ask を pass へ格上げしてよい PR 操作。IssueOptIn は CLAUDE_ALLOW_GH_ISSUE_WRITES
// による opt-in が有効なときの issue 操作で、キーごとに独立して判定する。
//
// Rules.Allow ではなくここに置くのは、opt-in で deny を覆せないようにするため。
// allow は deny より先に評価されるので、opt-in のルールを allow へ入れると
// 環境変数ひとつで deny 側のルールまで無効化できてしまう。呼び出し側は
// Evaluate の判定が Ask だったコマンドに対してだけこの判定を使う。
//
// 先頭が gh の形だけを対象にする。op run -- gh pr merge のようなラップ形は
// ask のまま残す (安全側)。
//
// 対象はプロジェクトのリポジトリに限る (inProject)。opt-in はプロジェクトスコープの
// settings で有効にするもので、-R / URL / GH_REPO で指した別リポジトリには効かせない。
var PROptIn = []argrule.Rule{
	argrule.When{
		// --admin (ブランチ保護のバイパス) と --attach が付いた形は opt-in の対象外。
		//
		// リリース PR は全ての verb で対象外 (ready は auto-merge の引き金になる)。
		// close --delete-branch も対象外。merge --delete-branch はマージ済みの head を消すだけなので通す。
		//
		// 自分が作った PR だと確定した形にも限る。他人の PR は OthersRules が
		// deny にするが、作成者を特定できない形 (引数なしの `gh pr edit` 等) は
		// deny にも当たらないので、ここで opt-in から外して ask に残す。
		Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"pr"}, Exact: true, Verbs: []string{
			"close",
			"edit",
			"merge",
			"ready",
			"reopen",
		}},
		Cond: func(cl *argrule.Cmdline) bool {
			v := cl.HeadView()
			return v != nil && !v.Changed("admin") && noAttach(cl) && !closeDeletesBranch(cl) &&
				inProject(cl) && authoredBySelf(cl) && confirmedNotReleasePR(cl)
		},
		Note: "--admin と --attach を伴わず、プロジェクトのリポジトリで自分が作成したもの。" +
			"リリース PR (ラベル tagpr) と close --delete-branch を除く",
	},
}

// IssueOptIn は opt-in で ask を飛ばしてよい issue 操作。取り消せない delete と、
// 別リポジトリへ移してしまう transfer は列挙から外し、ask のまま残す
// (PR 側で --admin を対象外にしているのと同じ理由)。
//
// create は作成される issue の作成者が自分になるので、作成者の照会をしない。
// 既存の issue を触る verb は、自分が作ったものだと確定した形だけを対象にする。
var IssueOptIn = []argrule.Rule{
	argrule.When{
		Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"issue", "create"}},
		Cond: func(cl *argrule.Cmdline) bool { return noAttach(cl) && inProject(cl) },
		Note: "--attach を伴わず、プロジェクトのリポジトリへのもの",
	},
	argrule.When{
		Rule: argrule.CmdAt{Tool: "gh", Head: true, Path: []string{"issue"}, Exact: true, Verbs: []string{
			"close",
			"edit",
			"lock",
			"pin",
			"reopen",
			"unlock",
			"unpin",
		}},
		Cond: func(cl *argrule.Cmdline) bool { return noAttach(cl) && inProject(cl) && authoredBySelf(cl) },
		Note: "--attach を伴わず、プロジェクトのリポジトリで自分が作成したもの",
	},
}

// specFor は opt-in の判定で使う FlagSpec。opt-in は先頭が gh の形だけを見る。
func specFor(tool string) argrule.FlagSpec {
	if tool == "gh" {
		return ghFlags
	}
	return nil
}

// PRWriteOptIn は argv が opt-in で ask をスキップしてよい gh の PR 操作かを返す。
func PRWriteOptIn(args []string) bool {
	_, ok := argrule.FirstMatch(PROptIn, argrule.NewCmdline(args, specFor))
	return ok
}

// IssueWriteOptIn は argv が opt-in で ask をスキップしてよい gh の issue 操作かを返す。
func IssueWriteOptIn(args []string) bool {
	_, ok := argrule.FirstMatch(IssueOptIn, argrule.NewCmdline(args, specFor))
	return ok
}
