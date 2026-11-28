package gh

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/gh/graphqldoc"
)

// queryDir は gh api graphql の `query=@<path>` を解決する基準ディレクトリ。
var queryDir string

// SetQueryDir は queryDir を設定する。hook では判定対象のコマンドが動く cwd を渡す。
// 空にすると `@<path>` を一切読まない。
func SetQueryDir(dir string) {
	queryDir = dir
}

// projectMutations は GitHub Project の item を足す・フィールドを書き換えるだけの mutation。
// 件数の多い triage を 1 リクエストへまとめ、二次レート制限を避けるために通す。
var projectMutations = []string{
	"addProjectV2ItemById",
	"updateProjectV2ItemFieldValue",
	"clearProjectV2ItemFieldValue",
}

// discussionMutations は Discussion の title / body を書き換えるだけの mutation。
// ページ移動時に giscus の Discussion へ新しい URL を反映する操作だけを通す。
// deleteDiscussion 等、取り消せない操作は対象外で deny のまま残る。
var discussionMutations = []string{
	"updateDiscussion",
}

// isProjectMutation は gh api graphql で projectMutations だけを送る形か。
func isProjectMutation(cl *argrule.Cmdline) bool {
	doc, ok := graphqlDocument(cl)
	return ok && graphqldoc.OnlyMutationsNamed(doc, projectMutations)
}

// isDiscussionMutation は gh api graphql で discussionMutations だけを送る形か。
//
// Allow の isProjectMutation / isReadOnlyQuery が使う graphqlDocument は、query 以外の
// フィールドに -F の `@<path>` (未読のファイル内容) が付くと文書全体を不一致にする
// (確認なしで中身を GitHub へ送らせないため)。この判定は ask 止まりで、実行前に人が
// コマンド全文 (ファイルパス込み) を見るので、同じ制約を変数の値に課す理由が無い。
// そのため query フィールドだけを見る queryFieldValue を使い、id・title・body 等の
// 変数が `-F body=@<path>` のような形でも対象にする。
func isDiscussionMutation(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil || v.Pos[1] != "graphql" || !methodIsPOSTOrOmitted(v) || v.Changed("input") {
		return false
	}
	query, ok := queryFieldValue(v)
	return ok && graphqldoc.OnlyMutationsNamed(query, discussionMutations)
}

// queryFieldValue は gh api graphql の -f/-F から query フィールドの値を 1 つ返す。
// 他のフィールドの値は見ない (variables の中身を問わない)。query が無い、複数ある、
// `query[x]=` のような添字付きの形は不一致にする。
func queryFieldValue(v *argrule.View) (string, bool) {
	typed, _ := v.FS.GetStringArray("field")
	raw, _ := v.FS.GetStringArray("raw-field")
	found, fromFile := false, false
	var query string
	for i, f := range slices.Concat(raw, typed) {
		key, value := splitField(f)
		if key != "query" {
			continue
		}
		if found || !strings.HasPrefix(f, "query=") {
			return "", false
		}
		query, fromFile, found = value, i >= len(raw), true
	}
	if !found {
		return "", false
	}
	return readQuery(query, fromFile)
}

// isReadOnlyQuery は gh api graphql で機微フィールドを選ばない query operation だけを送る形か。
// --paginate で gh が足すのは応答から取った `$endCursor` だけなので、フラグの有無を問わない。
func isReadOnlyQuery(cl *argrule.Cmdline) bool {
	src, ok := graphqlDocument(cl)
	return ok && graphqldoc.ReadOnlyQuery(src)
}

// graphqlDocument は gh api graphql で送る文書を返す。
//
// query 以外のフィールドは変数として通す。変数は operation と selection を変えられないので、
// 判定は文書だけで行う。-F の `@<path>` は gh がファイルを読んで GitHub へ送るため、変数では通さない。
// トップのキーが query のフィールドは `query=` の 1 つだけにし、guard が読む文書と gh が送る文書をずらさない。
func graphqlDocument(cl *argrule.Cmdline) (string, bool) {
	v := apiView(cl)
	if v == nil || v.Pos[1] != "graphql" || !methodIsPOSTOrOmitted(v) || v.Changed("input") {
		return "", false
	}
	typed, _ := v.FS.GetStringArray("field")
	raw, _ := v.FS.GetStringArray("raw-field")
	var query string
	found, fromFile := false, false
	for i, f := range slices.Concat(raw, typed) {
		// gh が `@<path>` をファイルとして読むのは -F だけで、-f は値をそのまま送る。
		isTyped := i >= len(raw)
		key, value := splitField(f)
		switch {
		case key != "query":
			if isTyped && strings.HasPrefix(value, "@") {
				return "", false
			}
		case found || !strings.HasPrefix(f, "query="):
			return "", false
		default:
			query, fromFile, found = value, isTyped, true
		}
	}
	if !found {
		return "", false
	}
	return readQuery(query, fromFile)
}

// splitField は gh api のフィールドをトップのキーと値に分ける。gh の parseFields に合わせ、
// キーは最初の `[` `]` `=` の手前、値は最初の `=` の後ろ全部になる。`key[]` のように `=` の無いものは値を空にする。
func splitField(f string) (key, value string) {
	_, value, _ = strings.Cut(f, "=")
	if i := strings.IndexAny(f, "[]="); i >= 0 {
		return f[:i], value
	}
	return f, value
}

// readQuery は -F の `@<path>` ならファイルの中身を、それ以外は値そのものを返す。
// stdin (`@-`) と読めないファイルは不一致にする。
func readQuery(value string, fromFile bool) (string, bool) {
	path, ok := strings.CutPrefix(value, "@")
	if !ok || !fromFile {
		return value, true
	}
	// queryDir が空のときは絶対パスも読まない。複合コマンドでは前段がファイルを書き換えたり
	// cd したりでき、guard が読んだ中身と gh が送る中身がずれるため。
	if path == "" || path == "-" || queryDir == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(queryDir, path)
	}
	real, ok := underDir(queryDir, path)
	if !ok {
		return "", false
	}
	b, err := os.ReadFile(real) // #nosec G304 -- underDir で queryDir の配下に閉じたことを確かめた
	if err != nil {
		return "", false
	}
	return string(b), true
}

// underDir は symlink を解決した path の実体が dir の配下にあれば、その実体のパスを返す。
// `..` や symlink で外へ出るパスは不一致にする。
func underDir(dir, path string) (string, bool) {
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return real, true
}
