package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const addItem = `addProjectV2ItemById(input: {projectId: "PVT_x", contentId: "I_x"}) { item { id } }`

const updateKind = `updateProjectV2ItemFieldValue(input: {projectId: "PVT_x", itemId: "PVTI_x", fieldId: "PVTSSF_x", value: {singleSelectOptionId: "a1"}}) { projectV2Item { id } }`

// writeQuery は dir に GraphQL の文書を書き、そのファイル名を返す。
func writeQuery(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestGHGraphQLProjectMutation(t *testing.T) {
	dir := t.TempDir()
	SetGHQueryDir(dir)
	t.Cleanup(func() { SetGHQueryDir("") })

	var many strings.Builder
	many.WriteString("mutation {\n")
	for i := range 50 {
		fmt.Fprintf(&many, "  a%d: %s\n", i, addItem)
	}
	many.WriteString("}\n")

	writeQuery(t, dir, "add.graphql", many.String())
	writeQuery(t, dir, "mixed.graphql", "mutation {\n a: "+addItem+"\n b: "+updateKind+"\n}\n")
	writeQuery(t, dir, "clear.graphql", `mutation { clearProjectV2ItemFieldValue(input: {projectId: "PVT_x", itemId: "PVTI_x", fieldId: "F"}) { projectV2Item { id } } }`)
	writeQuery(t, dir, "with-issue.graphql", "mutation {\n a: "+addItem+"\n b: createIssue(input: {repositoryId: \"R\", title: \"t\"}) { issue { id } }\n}\n")
	writeQuery(t, dir, "broken.graphql", "mutation { "+addItem)
	writeQuery(t, dir, "two-ops.graphql", "mutation A { "+addItem+" }\nquery B { viewer { login } }")
	writeQuery(t, dir, "fragment.graphql", "mutation { ...F }\nfragment F on Mutation { deleteRepository(input: {}) { clientMutationId } }")

	runEvalCases(t, []evalCase{
		// 通す形
		{"インラインの Project mutation", "gh api graphql -f query='mutation { " + addItem + " }'", Pass, true},
		{"ファイルから 50 件の追加", "gh api graphql -F query=@add.graphql", Pass, true},
		{"./ 付きのパス", "gh api graphql -F query=@./add.graphql", Pass, true},
		{"追加と更新の混在", "gh api graphql -F query=@mixed.graphql", Pass, true},
		{"フィールドの値を消す", "gh api graphql -F query=@clear.graphql", Pass, true},
		{"POST の明示と --jq", "gh api graphql -X POST -F query=@add.graphql --jq '.data'", Pass, true},

		// 通さない形
		{"許可外の mutation が混ざる", "gh api graphql -F query=@with-issue.graphql", Deny, false},
		{"解析できない文書", "gh api graphql -F query=@broken.graphql", Deny, false},
		{"mutation と query の同居", "gh api graphql -F query=@two-ops.graphql", Deny, false},
		{"トップレベルの fragment spread", "gh api graphql -F query=@fragment.graphql", Deny, false},
		{"存在しないファイル", "gh api graphql -F query=@missing.graphql", Deny, false},
		{"stdin から", "gh api graphql -F query=@-", Deny, false},
		{"--input", "gh api graphql --input add.graphql", Deny, false},
		{"graphql 以外のパス", "gh api repos/o/r/issues -F query=@add.graphql", Deny, false},
		{"PUT", "gh api graphql -X PUT -F query=@add.graphql", Deny, false},
	})
}

const viewsQuery = `query { user(login: "octocat") { projectV2(number: 5) { views(first: 20) { nodes { number name layout filter fields(first: 30) { nodes { ... on ProjectV2FieldCommon { name } } } groupByFields(first: 5) { nodes { ... on ProjectV2FieldCommon { name } } } verticalGroupByFields(first: 5) { nodes { ... on ProjectV2FieldCommon { name } } } sortByFields(first: 5) { nodes { direction field { ... on ProjectV2FieldCommon { name } } } } } } } } }`

// 機微フィールドを選ばない query operation だけを通す。alias や fragment に包んでも
// 機微フィールドは見つけて deny に落とす。
func TestGHGraphQLReadOnlyQuery(t *testing.T) {
	dir := t.TempDir()
	SetGHQueryDir(dir)
	t.Cleanup(func() { SetGHQueryDir("") })
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}

	writeQuery(t, dir, "tmp/views.graphql", viewsQuery)
	writeQuery(t, dir, "query.graphql", "query { viewer { login } }")
	writeQuery(t, dir, "named-fragment.graphql", "query { viewer { ...U } }\nfragment U on User { login name }")
	writeQuery(t, dir, "paginate.graphql", "query($endCursor: String) { viewer { repositories(first: 100, after: $endCursor) { nodes { name } pageInfo { hasNextPage endCursor } } } }")
	writeQuery(t, dir, "email.graphql", "query { viewer { login email } }")
	writeQuery(t, dir, "alias-email.graphql", "query { viewer { login e: email } }")
	writeQuery(t, dir, "inline-email.graphql", `query { repositoryOwner(login: "x") { ... on User { email } } }`)
	writeQuery(t, dir, "fragment-keys.graphql", "query { viewer { ...K } }\nfragment K on User { publicKeys(first: 10) { nodes { key } } }")
	writeQuery(t, dir, "nested-fragment.graphql", "query { viewer { ...A } }\nfragment A on User { login ...B }\nfragment B on User { organizationVerifiedDomainEmails(login: \"o\") }")
	writeQuery(t, dir, "cycle.graphql", "query { viewer { ...A } }\nfragment A on User { ...B }\nfragment B on User { ...A }")
	writeQuery(t, dir, "undefined-fragment.graphql", "query { viewer { ...Missing } }")
	writeQuery(t, dir, "commit-email.graphql", `query { repository(owner: "o", name: "r") { defaultBranchRef { target { ... on Commit { author { name email } } } } } }`)
	writeQuery(t, dir, "deploy-keys.graphql", `query { repository(owner: "o", name: "r") { deployKeys(first: 10) { nodes { title } } } }`)
	writeQuery(t, dir, "clone-token.graphql", `query { repository(owner: "o", name: "r") { tempCloneToken } }`)
	writeQuery(t, dir, "saml.graphql", `query { organization(login: "o") { samlIdentityProvider { ssoUrl } } }`)
	writeQuery(t, dir, "ip-allow.graphql", `query { organization(login: "o") { ipAllowListEntries(first: 10) { nodes { allowListValue } } } }`)
	writeQuery(t, dir, "node-email.graphql", `query { node(id: "U_x") { ... on User { email } } }`)
	writeQuery(t, dir, "subscription.graphql", "subscription { viewer { login } }")
	writeQuery(t, dir, "query-and-mutation.graphql", "query A { viewer { login } }\nmutation B { "+addItem+" }")

	runEvalCases(t, []evalCase{
		// 通す形
		{"依頼の view query (インライン)", "gh api graphql -f query='" + viewsQuery + "'", Pass, true},
		{"依頼の view query (ファイル)", "gh api graphql -F query=@./tmp/views.graphql", Pass, true},
		{"query operation", "gh api graphql -F query=@query.graphql", Pass, true},
		{"インラインの query operation", "gh api graphql -f query='query { viewer { login } }'", Pass, true},
		{"named fragment を辿る", "gh api graphql -F query=@named-fragment.graphql", Pass, true},
		{"--paginate", "gh api graphql --paginate -F query=@paginate.graphql", Pass, true},
		{"--paginate --slurp と --jq", "gh api graphql --paginate --slurp -F query=@paginate.graphql --jq '.'", Pass, true},

		// 機微フィールド
		{"viewer の email", "gh api graphql -F query=@email.graphql", Deny, false},
		{"インラインの email", "gh api graphql -f query='query { viewer { email } }'", Deny, false},
		{"alias で包んだ email", "gh api graphql -F query=@alias-email.graphql", Deny, false},
		{"inline fragment 内の email", "gh api graphql -F query=@inline-email.graphql", Deny, false},
		{"named fragment 内の publicKeys", "gh api graphql -F query=@fragment-keys.graphql", Deny, false},
		{"fragment の入れ子の先", "gh api graphql -F query=@nested-fragment.graphql", Deny, false},
		{"commit author の email", "gh api graphql -F query=@commit-email.graphql", Deny, false},
		{"deployKeys", "gh api graphql -F query=@deploy-keys.graphql", Deny, false},
		{"tempCloneToken", "gh api graphql -F query=@clone-token.graphql", Deny, false},
		{"samlIdentityProvider", "gh api graphql -F query=@saml.graphql", Deny, false},
		{"ipAllowListEntries", "gh api graphql -F query=@ip-allow.graphql", Deny, false},
		{"node から辿る email", "gh api graphql -F query=@node-email.graphql", Deny, false},

		// 文書の形
		{"fragment の循環", "gh api graphql -F query=@cycle.graphql", Deny, false},
		{"未定義の fragment", "gh api graphql -F query=@undefined-fragment.graphql", Deny, false},
		{"subscription", "gh api graphql -F query=@subscription.graphql", Deny, false},
		{"query と mutation の同居", "gh api graphql -F query=@query-and-mutation.graphql", Deny, false},

		// コマンドラインの形
		{"-f は @ をファイルとして読まない", "gh api graphql -f query=@query.graphql", Deny, false},
		{"--input", "gh api graphql --input query.graphql", Deny, false},
		{"PUT", "gh api graphql -X PUT -F query=@query.graphql", Deny, false},
	})
}

const searchQuery = `query($q: String!) { search(query: $q, type: DISCUSSION, first: 5) { nodes { ... on Discussion { title url } } } }`

// query 以外のフィールドは変数として通す。文書の判定はそのままで、-F の `@<path>` と
// query フィールドの重複だけを変数側で落とす。
func TestGHGraphQLVariables(t *testing.T) {
	dir := t.TempDir()
	SetGHQueryDir(dir)
	t.Cleanup(func() { SetGHQueryDir("") })

	writeQuery(t, dir, "query.graphql", "query { viewer { login } }")
	writeQuery(t, dir, "add.graphql", "mutation($p: ID!) { addProjectV2ItemById(input: {projectId: $p, contentId: \"I_x\"}) { item { id } } }")
	writeQuery(t, dir, "email.graphql", "query { viewer { email } }")
	writeQuery(t, dir, "vars.json", "{}")
	abs := filepath.Join(dir, "vars.json")

	runEvalCases(t, []evalCase{
		// 通す形 (read-only query)
		{"issue の検索 (-f query と -F q)", "gh api graphql -f query='" + searchQuery + "' -F q='repo:o/r category:\"Page Comments\" in:title \"x\"'", Pass, true},
		{"-f の変数", "gh api graphql -F query=@query.graphql -f owner=x", Pass, true},
		{"-F variables", "gh api graphql -F query=@query.graphql -F variables='{}'", Pass, true},
		{"-F の数値", "gh api graphql -F query=@query.graphql -F n=5", Pass, true},
		{"-F の true", "gh api graphql -F query=@query.graphql -F b=true", Pass, true},
		{"-F の null", "gh api graphql -F query=@query.graphql -F x=null", Pass, true},
		{"-f の @ は文字列", "gh api graphql -F query=@query.graphql -f q=@vars.json", Pass, true},
		{"-F の入れ子", "gh api graphql -F query=@query.graphql -F 'a[b]=c'", Pass, true},
		{"-F の配列", "gh api graphql -F query=@query.graphql -F 'a[]=1' -F 'a[]=2'", Pass, true},
		{"-F の空の配列 (= 無し)", "gh api graphql -F query=@query.graphql -F 'a[]'", Pass, true},
		{"operationName", "gh api graphql -F query=@query.graphql -f operationName=Foo", Pass, true},
		{"--raw-field と --field", "gh api graphql --raw-field query='query { viewer { login } }' --field n=5", Pass, true},

		// 通す形 (Project の mutation)
		{"Project mutation と -F の変数", "gh api graphql -F query=@add.graphql -F p=PVT_x", Pass, true},
		{"Project mutation と -f の変数", "gh api graphql -F query=@add.graphql -f p=PVT_x -F owner=x", Pass, true},

		// 変数があっても文書の判定は変わらない
		{"機微フィールドと変数", "gh api graphql -F query=@email.graphql -f q=x", Deny, false},
		{"変数だけで query が無い", "gh api graphql -f q=x", Deny, false},

		// -F の @ はファイルを読むので変数では通さない
		{"-F の @ (cwd 配下)", "gh api graphql -F query=@query.graphql -F q=@vars.json", Deny, false},
		{"-F の @ (cwd 配下の絶対パス)", "gh api graphql -F query=@query.graphql -F q=@" + abs, Deny, false},
		{"-F の @-", "gh api graphql -F query=@query.graphql -F q=@-", Deny, false},
		{"-F の配列の @", "gh api graphql -F query=@query.graphql -F 'q[]=@vars.json'", Deny, false},
		{"-F の入れ子の @", "gh api graphql -F query=@query.graphql -F 'q[sub]=@vars.json'", Deny, false},
		{"--field の @", "gh api graphql -F query=@query.graphql --field q=@vars.json", Deny, false},
		{"Project mutation と -F の @", "gh api graphql -F query=@add.graphql -F p=@vars.json", Deny, false},

		// query フィールドはちょうど 1 つ
		{"query の重複 (-f と -F)", "gh api graphql -f query='query { viewer { email } }' -F query=@query.graphql", Deny, false},
		{"query の重複 (-f 2 つ)", "gh api graphql -f query='query { viewer { email } }' -f query='query { viewer { login } }'", Deny, false},
		{"query の配列", "gh api graphql -F query=@query.graphql -f 'query[]=x'", Deny, false},
		{"query の空の配列 (= 無し)", "gh api graphql -F query=@query.graphql -f 'query[]'", Deny, false},
		{"query の入れ子", "gh api graphql -F query=@query.graphql -f 'query[x]=y'", Deny, false},
		{"--input と変数", "gh api graphql --input query.graphql -f q=x", Deny, false},
	})
}

// ファイルは cwd の配下にあるものだけを読む。外を指すパスは中身が Project の mutation でも通さない。
func TestGHGraphQLPathScope(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	SetGHQueryDir(cwd)
	t.Cleanup(func() { SetGHQueryDir("") })
	body := "mutation { " + addItem + " }"
	writeQuery(t, cwd, "add.graphql", body)
	writeQuery(t, outside, "add.graphql", body)
	if err := os.Symlink(filepath.Join(outside, "add.graphql"), filepath.Join(cwd, "link.graphql")); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, filepath.Join(outside, "add.graphql"))
	if err != nil {
		t.Fatal(err)
	}

	runEvalCases(t, []evalCase{
		{"cwd 配下の絶対パス", "gh api graphql -F query=@" + filepath.Join(cwd, "add.graphql"), Pass, true},
		{"cwd の外の絶対パス", "gh api graphql -F query=@" + filepath.Join(outside, "add.graphql"), Deny, false},
		{".. で cwd の外へ出る", "gh api graphql -F query=@" + rel, Deny, false},
		{"cwd の外を指す symlink", "gh api graphql -F query=@link.graphql", Deny, false},
		{"-f は @ をファイルとして読まない", "gh api graphql -f query=@add.graphql", Deny, false},
	})
}
