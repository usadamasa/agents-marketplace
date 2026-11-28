package ghauthor

// gh の書き込み対象のリポジトリが、opt-in したプロジェクトのリポジトリと同じかの判定。
//
// opt-in (CLAUDE_ALLOW_GH_PR_WRITES 等) はプロジェクトスコープの settings で有効にする。
// 効かせてよいのはそのプロジェクトのリポジトリの PR / issue だけで、-R / --repo /
// URL / GH_REPO で別のリポジトリを指した形には効かせない。
//
// 対象の決め方は gh に合わせる。
//   - 明示 (-R / --repo、PR / issue の URL、環境変数 GH_REPO) があればそれ。
//     プロジェクトのリポジトリ名と比べる。
//   - 明示が無ければ、コマンドが動く cwd の git remote。cwd がプロジェクトと同じ
//     git リポジトリ (worktree を含む) かを git-common-dir で比べる。名前を比べないのは、
//     同じリポジトリなら gh が解決する remote もプロジェクトと同じになるため。
//
// 判定は fail-closed で、どこかで決められなければ false (opt-in を効かせない) を返す。
//
// GH_REPO / GH_HOST は hook のプロセスの環境変数で見る。CLAUDE_ENV_FILE で Bash にだけ
// 載る値は PreToolUse の hook に渡らないので、呼び出し側がセッションの env ファイルを読み、
// 代入があれば DisableLookup で照会を止める。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// defaultHost は host を省いた OWNER/REPO が指すホスト。GH_HOST が立っているときは
// InProject が判定を諦めるので、ここでは github.com に固定してよい。
const defaultHost = "github.com"

// gitTimeout は git の照会 1 回の上限。ローカルの照会だけなので短くてよい。
const gitTimeout = 2 * time.Second

// repository は GitHub のリポジトリ。比較は大文字小文字を区別しない (GitHub と同じ)。
type repository struct {
	Host  string
	Owner string
	Name  string
}

func (r repository) equal(o repository) bool {
	return strings.EqualFold(r.Host, o.Host) &&
		strings.EqualFold(r.Owner, o.Owner) &&
		strings.EqualFold(r.Name, o.Name)
}

var (
	// segmentRe は owner / repo / host の 1 区切りとして受け付ける形。
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	// targetURLRe は PR / issue の URL。gh は位置引数の URL からリポジトリを決める。
	// scheme は gh (net/url) と同じく大文字小文字を区別しない。
	targetURLRe = regexp.MustCompile(`^(?i:https?)://[^[:space:]]+/(pull|issues)/[0-9]+/?$`)
	// scpRe は git@host:owner/repo.git の形の remote。
	scpRe = regexp.MustCompile(`^[^@/:]+@([^:/]+):(.+)$`)
)

// parsePath は "owner/repo" を host と組み合わせて repository にする。末尾の .git と / は落とす。
func parsePath(host, path string) (repository, bool) {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || !segmentRe.MatchString(host) {
		return repository{}, false
	}
	for _, p := range parts {
		if !segmentRe.MatchString(p) {
			return repository{}, false
		}
	}
	return repository{Host: host, Owner: parts[0], Name: parts[1]}, true
}

// parseFullName は -R / GH_REPO の値 ([HOST/]OWNER/REPO、または URL) を repository にする。
func parseFullName(s string) (repository, bool) {
	if strings.Contains(s, "://") {
		return parseRemoteURL(s)
	}
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		return parsePath(defaultHost, s)
	case 3:
		return parsePath(parts[0], parts[1]+"/"+parts[2])
	default:
		return repository{}, false
	}
}

// parseRemoteURL は git remote の URL (https / ssh / git@host:path) を repository にする。
func parseRemoteURL(s string) (repository, bool) {
	if m := scpRe.FindStringSubmatch(s); m != nil && !strings.Contains(s, "://") {
		return parsePath(m[1], m[2])
	}
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok || (scheme != "https" && scheme != "http" && scheme != "ssh" && scheme != "git") {
		return repository{}, false
	}
	hostPart, path, ok := strings.Cut(rest, "/")
	if !ok {
		return repository{}, false
	}
	if _, h, found := strings.Cut(hostPart, "@"); found {
		hostPart = h
	}
	host, _, _ := strings.Cut(hostPart, ":")
	return parsePath(host, path)
}

// parseTargetURL は PR / issue の URL から repository を取り出す。
func parseTargetURL(s string) (repository, bool) {
	if !targetURLRe.MatchString(s) {
		return repository{}, false
	}
	_, rest, _ := strings.Cut(s, "://")
	parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	// host/owner/repo/(pull|issues)/N
	if len(parts) != 5 {
		return repository{}, false
	}
	return parsePath(parts[0], parts[1]+"/"+parts[2])
}

// explicitRepos は argv で明示された対象リポジトリを全て返す。
// 明示の形をしているのに読めない値があれば ok=false。
//
// フラグが値を取るかの表を持たずに字面で拾う。pflag で未知のフラグを読み飛ばすと、
// `--squash https://.../pull/1` の URL が --squash の値として消える。取りこぼすと
// 「明示なし」として cwd で判定され、別リポジトリに opt-in が効いてしまう。
// 拾いすぎた場合は opt-in が効かないだけ (ask に残る) なので安全側に倒れる。
func explicitRepos(args []string) (repos []repository, ok bool) {
	add := func(v string) bool {
		r, ok := parseFullName(v)
		if ok {
			repos = append(repos, r)
		}
		return ok
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-R" || a == "--repo":
			if i+1 >= len(args) || !add(args[i+1]) {
				return nil, false
			}
			i++
		case strings.HasPrefix(a, "--repo="):
			if !add(strings.TrimPrefix(a, "--repo=")) {
				return nil, false
			}
		case strings.HasPrefix(a, "-R"):
			if !add(strings.TrimPrefix(a, "-R")) {
				return nil, false
			}
		case len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'R'):
			// -dR owner/repo のように短縮フラグをまとめた形。値の位置を決めきれない。
			return nil, false
		case strings.Contains(a, "://"):
			// URL らしい引数は、PR / issue の URL として読めたときだけ対象に数える。
			// gh は scheme の大文字や /pull/<n>/files のような後続も受け付けるので、
			// 字面の取りこぼしを残さないよう、読めないものは本文の中の URL でも判定を諦める。
			r, found := parseTargetURL(a)
			if !found {
				return nil, false
			}
			repos = append(repos, r)
		}
	}
	return repos, true
}

// projectDir は opt-in を有効にしたプロジェクトのルート。コマンドの実行位置 (workdir) と、
// 照会を止めるフラグ (disabled) は作成者の照会と共有する。
var projectDir string

// SetProjectDir は opt-in を有効にしたプロジェクトのルートを設定する。
func SetProjectDir(dir string) { projectDir = dir }

// InProject は argv の gh コマンドの対象がプロジェクトのリポジトリだと確定したときだけ true を返す。
// DisableLookup の後は、対象が字面から決まらないので常に false。
func InProject(args []string) bool {
	if disabled || projectDir == "" {
		return false
	}
	// GH_HOST はリポジトリの解決に使うホストを変える。組み合わせを追わずに諦める。
	if os.Getenv("GH_HOST") != "" {
		return false
	}
	repos, ok := explicitRepos(args)
	if !ok {
		return false
	}
	if v := os.Getenv("GH_REPO"); v != "" {
		r, ok := parseFullName(v)
		if !ok {
			return false
		}
		repos = append(repos, r)
	}
	if len(repos) == 0 {
		return sameGitRepo(workdir, projectDir)
	}
	project, ok := projectRepo(projectDir)
	if !ok {
		return false
	}
	for _, r := range repos {
		if !r.equal(project) {
			return false
		}
	}
	return true
}

// sameGitRepo は 2 つのディレクトリが同じ git リポジトリ (worktree を含む) に属するかを返す。
// 同じパスでも git に聞く。git 管理外のプロジェクトはリポジトリの一致を確かめられない。
func sameGitRepo(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, ok := commonDir(a)
	if !ok {
		return false
	}
	cb, ok := commonDir(b)
	return ok && ca == cb
}

// commonDir は dir が属する git リポジトリの共通ディレクトリ (worktree 間で共有される .git) を返す。
func commonDir(dir string) (string, bool) {
	out, ok := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok {
		return "", false
	}
	// macOS の /var → /private/var のような symlink の違いで食い違わないよう解決する。
	resolved, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		return "", false
	}
	return resolved, true
}

// projectRepo はプロジェクトの gh 上のリポジトリを remote から決める。
//
// gh と同じく、gh repo set-default が gh-resolved を付けた remote を優先し、
// 無ければ remote が 1 つのときだけそれを使う。複数あって決まらないときは
// gh も非対話では解決しないので、ここでも決めない。
func projectRepo(dir string) (repository, bool) {
	out, ok := git(dir, "config", "--get-regexp", `^remote\..*\.(url|gh-resolved)$`)
	if !ok {
		return repository{}, false
	}
	urls := map[string]string{}
	resolved := map[string]string{}
	for line := range strings.Lines(out) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		name := strings.TrimPrefix(key, "remote.")
		switch {
		case strings.HasSuffix(name, ".url"):
			urls[strings.TrimSuffix(name, ".url")] = value
		case strings.HasSuffix(name, ".gh-resolved"):
			resolved[strings.TrimSuffix(name, ".gh-resolved")] = value
		}
	}

	switch len(resolved) {
	case 0:
		if len(urls) != 1 {
			return repository{}, false
		}
		for _, u := range urls {
			return parseRemoteURL(u)
		}
	case 1:
		for name, value := range resolved {
			remote, ok := parseRemoteURL(urls[name])
			if !ok {
				return repository{}, false
			}
			if value == "base" {
				return remote, true
			}
			// fork の remote に親リポジトリ (OWNER/REPO) を記録した形。
			return parsePath(remote.Host, value)
		}
	}
	return repository{}, false
}

// git は dir で git を実行して stdout を返す。失敗 (設定が無いときの exit 1 を含む) は ok=false。
func git(dir string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	// #nosec G204 -- 引数は固定のサブコマンドだけ。dir は hook の入力 (cwd / CLAUDE_PROJECT_DIR)。
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
