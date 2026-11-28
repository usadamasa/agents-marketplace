// Package ghauthor は gh の書き込み対象 (PR / issue / PR の review comment) を誰が作ったか、どのリポジトリの
// ものかを判定する。リポジトリの判定は repo.go に置く。
//
// 他人が作った PR / issue への書き込みを deny へ、自分が作ったものだけを opt-in の
// 対象へ振り分けるために rules から使う。判定は「確定したときだけ true」で、対象を
// 特定できない形も照会に失敗した形も false を返す (fail-closed)。呼び出し側では
// deny の事前条件 → 当たらないので ask、allow / opt-in の事前条件 → 当たらないので
// ask となり、どちらも安全側に倒れる。
//
// 照会は gh CLI の実行を伴う。ルールの正規表現が当たった segment に対してだけ走り、
// 同じ対象への照会はプロセス内で 1 回に抑える。
package ghauthor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
)

// lookupBudget は 1 プロセスが照会に使ってよい実時間の上限。
// PreToolUse は ask のプロンプトが出る前に走るため、待たせすぎない値にする。
// self と対象の 2 回の照会でこの budget を分け合う。
const lookupBudget = 3 * time.Second

// target は gh の書き込み対象。
type target struct {
	// kind は "pr" か "issue"。gh のサブコマンド (view) の選択に使う。
	kind string
	// repo は --repo / -R で明示されたリポジトリ。空ならカレントディレクトリから解決させる。
	repo string
	// arg は gh へそのまま渡す位置引数 (番号または URL)。
	arg string
}

// workdir は gh を実行するディレクトリ。--repo の無い形はここから解決される。
var workdir string

// disabled は照会を止めるフラグ。どのディレクトリでコマンドが動くか分からなく
// なった時点で立てる。
var disabled bool

// SetWorkdir は照会で gh を実行するディレクトリを設定する。
// 判定対象のコマンドが動くディレクトリ (PreToolUse の cwd) を渡す。
// 1 回の判定の始まりでもあるので、DisableLookup の効果もここで解除する。
func SetWorkdir(dir string) {
	workdir = dir
	disabled = false
}

// DisableLookup は以降の照会を止める。呼んだ後は作成者を確定できなくなり、
// 呼び出し側は ask へ落ちる。
//
// chain の途中に cd がある形 (cd other-repo && gh pr edit 5) で使う。後ろの
// segment がどのリポジトリで動くかは segment だけでは決まらず、カレント
// リポジトリへ問い合わせると同じ番号の別の PR の作成者を見てしまう。
func DisableLookup() { disabled = true }

var (
	// numberRe は位置引数が PR / issue 番号の形か。
	numberRe = regexp.MustCompile(`^[0-9]+$`)
	// urlRe は位置引数が PR / issue の URL の形か。ホストは github.com に限らない
	// (GHE) ため、末尾の /pull/<n> /issues/<n> だけを見る。
	urlRe = regexp.MustCompile(`^https?://[^[:space:]]+/(pull|issues)/[0-9]+/?$`)
	// repoRe は --repo に渡してよい値の形 (owner/repo または host/owner/repo)。
	// gh へは argv の 1 要素として渡すのでシェルの解釈は挟まらないが、
	// フラグに化ける値や親ディレクトリを辿る値を持ち込まないためここで絞る。
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*){1,2}$`)
)

// parseTarget は argv から書き込み対象を取り出す。
//
// 位置引数は verb の直後に置かれた形だけを見る。フラグの後ろに置いた形
// (gh pr merge --squash 123) まで拾おうとすると「フラグが値を取るか」の表を
// gh のサブコマンドごとに持つことになり、取り違えると他人の PR を自分のものと
// 誤判定しかねない。特定できない形は ok=false にして ask へ落とす。
func parseTarget(fields []string) (target, bool) {
	if len(fields) < 4 || fields[0] != "gh" {
		return target{}, false
	}
	kind := fields[1]
	if kind != "pr" && kind != "issue" {
		return target{}, false
	}
	arg := fields[3]
	switch {
	case numberRe.MatchString(arg):
		// 番号だけではリポジトリが決まらないので --repo を探す。値が owner/repo の
		// 形でないときは、カレントリポジトリへ問い合わせて別の PR の作成者を
		// 見てしまわないよう、対象を特定できなかったことにする。
		repo, found := parseRepoFlag(fields[4:])
		if found && !repoRe.MatchString(repo) {
			return target{}, false
		}
		return target{kind: kind, repo: repo, arg: arg}, true
	case urlRe.MatchString(arg):
		// URL はリポジトリを含む。--repo との併用は gh 自身がエラーにする。
		return target{kind: kind, arg: arg}, true
	default:
		return target{}, false
	}
}

// parseRepoFlag は --repo / -R の値と、フラグが在ったかを返す。
// 値の無い --repo のように解析できない形は、在ったが値が空として扱う
// (呼び出し側で owner/repo の形に当たらず、対象を特定できなかったことになる)。
func parseRepoFlag(fields []string) (string, bool) {
	fs := pflag.NewFlagSet("gh", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.ParseErrorsAllowlist.UnknownFlags = true
	repo := fs.StringP("repo", "R", "", "")
	if err := fs.Parse(fields); err != nil {
		return "", true
	}
	return *repo, fs.Changed("repo")
}

// AuthoredBySelf は対象の作成者が自分だと確定したときだけ true を返す。
func AuthoredBySelf(args []string) bool {
	author, self, ok := authorship(args)
	return ok && strings.EqualFold(author, self)
}

// AuthoredByOther は対象の作成者が自分以外だと確定したときだけ true を返す。
func AuthoredByOther(args []string) bool {
	author, self, ok := authorship(args)
	return ok && !strings.EqualFold(author, self)
}

// reviewCommentKind は PR の review comment を指す target.kind。repo は owner/repo、arg は comment の id。
const reviewCommentKind = "review-comment"

// ReviewCommentByOther は repo (owner/repo) の review comment id を書いた人が
// 自分以外の人 (bot を除く) だと確定したときだけ true を返す。bot (Copilot 等) の
// コメントは書き手に直してもらう相手がいないので、他人のものとして扱わない。
func ReviewCommentByOther(repo, id string) bool {
	if disabled || !repoRe.MatchString(repo) || strings.Count(repo, "/") != 1 || !numberRe.MatchString(id) {
		return false
	}
	info, err := lookups.authorOf(target{kind: reviewCommentKind, repo: repo, arg: id})
	if err != nil || info.isBot {
		return false
	}
	self, err := lookups.selfLogin()
	return err == nil && !strings.EqualFold(info.login, self)
}

// AuthoredByBot は対象の作成者が GitHub App / bot アカウントだと確定したときだけ
// true を返す。gh が返す author.is_bot をそのまま見るので、dependabot に限らず
// renovate[bot] 等も対象になる。bot のログイン名を列挙しないための判定方法で、
// 自分のログイン名の照会は不要なので authorOnly だけで判定する。
func AuthoredByBot(args []string) bool {
	info, ok := authorOnly(args)
	return ok && info.isBot
}

// authorship は argv の対象の作成者と自分のログイン名を返す。
// 対象を特定できない・どちらかを取得できない場合は ok=false。
func authorship(args []string) (author, self string, ok bool) {
	info, ok := authorOnly(args)
	if !ok {
		return "", "", false
	}
	self, err := lookups.selfLogin()
	if err != nil {
		return "", "", false
	}
	return info.login, self, true
}

// authorOnly は対象の作成者だけを返す。自分のログイン名は問わない。
func authorOnly(args []string) (info authorInfo, ok bool) {
	if disabled {
		return authorInfo{}, false
	}
	t, ok := parseTarget(args)
	if !ok {
		return authorInfo{}, false
	}
	info, err := lookups.authorOf(t)
	if err != nil {
		return authorInfo{}, false
	}
	return info, true
}

// authorInfo は gh が返す対象の作成者情報。is_bot は GitHub API がボット
// アカウント (GitHub App の実行主体) に付ける属性で、dependabot や renovate 等の
// ログイン名を列挙しなくても bot かどうかを判定できる。
type authorInfo struct {
	login string
	isBot bool
	// labelsKnown はラベルを取れたか。PR のときだけ true になる。
	labelsKnown bool
	// release はリリース PR のラベルが付いているか (release.go)。
	release bool
}

// lookups はプロセス内のメモ化キャッシュ。deny の事前条件と opt-in の事前条件で
// 同じ対象を二度照会するため、gh の実行回数をここで 1 回に抑える。
var lookups = newCache(selfLoginFromCLI, authorOfCLI)

type selfResult struct {
	login string
	err   error
}

type authorResult struct {
	info authorInfo
	err  error
}

type cache struct {
	lookupSelf   func() (string, error)
	lookupAuthor func(target) (authorInfo, error)

	mu      sync.Mutex
	selfHit *selfResult
	targets map[target]authorResult
}

func newCache(self func() (string, error), authorOf func(target) (authorInfo, error)) *cache {
	return &cache{lookupSelf: self, lookupAuthor: authorOf, targets: map[target]authorResult{}}
}

func (c *cache) selfLogin() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.selfHit == nil {
		login, err := c.lookupSelf()
		c.selfHit = &selfResult{login: login, err: err}
	}
	return c.selfHit.login, c.selfHit.err
}

func (c *cache) authorOf(t target) (authorInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hit, ok := c.targets[t]; ok {
		return hit.info, hit.err
	}
	info, err := c.lookupAuthor(t)
	c.targets[t] = authorResult{info: info, err: err}
	return info, err
}

// deadline は照会に使ってよい実時間の期限。初回の照会で決まり、以降の照会は
// 残りを分け合う。1 回の判定が budget を超えて延びないようにするため。
var deadline = sync.OnceValue(func() time.Time { return time.Now().Add(lookupBudget) })

// selfLoginFromCLI は認証中のユーザーのログイン名を返す。
func selfLoginFromCLI() (string, error) {
	return runGH("api", "user", "--jq", ".login")
}

// authorOfCLI は対象の作成者のログイン名と is_bot を返す。
func authorOfCLI(t target) (authorInfo, error) {
	if t.kind == reviewCommentKind {
		// REST の user.type は bot アカウントで "Bot" になる (gh pr view の author.is_bot に当たる)。
		out, err := runGH("api", "repos/"+t.repo+"/pulls/comments/"+t.arg, "--jq", `.user.login + " " + .user.type`)
		if err != nil {
			return authorInfo{}, err
		}
		login, typ, ok := strings.Cut(out, " ")
		if !ok || login == "" {
			return authorInfo{}, fmt.Errorf("gh api: review comment の user を読めない: %q", out)
		}
		return authorInfo{login: login, isBot: typ == "Bot"}, nil
	}
	args := []string{t.kind, "view", t.arg, "--json", viewFields(t.kind)}
	if t.repo != "" {
		args = append(args, "--repo", t.repo)
	}
	out, err := runGH(args...)
	if err != nil {
		return authorInfo{}, err
	}
	info, err := parseView(out)
	if err != nil {
		return authorInfo{}, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return info, nil
}

// viewFields は gh <kind> view の --json に渡すフィールド。ラベルを見るのは PR だけ。
func viewFields(kind string) string {
	if kind == "pr" {
		return "author,labels"
	}
	return "author"
}

// parseView は gh pr view / gh issue view の --json 出力を読む。
func parseView(out string) (authorInfo, error) {
	var payload struct {
		Author struct {
			Login string `json:"login"`
			IsBot bool   `json:"is_bot"`
		} `json:"author"`
		// ポインタにして、フィールドが無い出力と空の配列を分ける。
		Labels *[]struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return authorInfo{}, fmt.Errorf("JSON の解析に失敗: %w", err)
	}
	info := authorInfo{login: payload.Author.Login, isBot: payload.Author.IsBot}
	if payload.Labels != nil {
		info.labelsKnown = true
		for _, l := range *payload.Labels {
			info.release = info.release || slices.Contains(releaseLabels, l.Name)
		}
	}
	return info, nil
}

// runGH は gh を実行して stdout を返す。出力が空なら失敗として扱う。
func runGH(args ...string) (string, error) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline())
	defer cancel()

	// #nosec G204 -- 引数は固定のサブコマンドと、番号 / URL / owner-repo の形を
	// 正規表現で確かめた値だけ。シェルを介さず argv として渡す。
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		err = fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		// 照会に失敗すると判定は ask に落ちる。黙って落ちると「なぜ deny に
		// ならないのか」を後から追えないため、理由を hook のログに残す。
		fmt.Fprintf(os.Stderr, "[guard-bash] INFO   作成者を照会できませんでした: %v\n", err)
		return "", err
	}
	login := strings.TrimSpace(stdout.String())
	if login == "" {
		return "", fmt.Errorf("gh %s: 出力が空", strings.Join(args, " "))
	}
	return login, nil
}

// Stub はテストから照会を差し替えるための値。
type Stub struct {
	// Self は自分のログイン名。空なら「取得できない」として扱う。
	Self string
	// Authors は "<kind> <位置引数>" (例 "pr 5"、review comment は "review-comment <id>") から
	// 作成者のログイン名への対応。
	// 未登録のキーは「照会できない」として扱う。
	Authors map[string]string
	// Bots は同じキーから is_bot への対応。未登録のキーは false (bot ではない) 扱い。
	Bots map[string]bool
	// Labels は同じキーから PR のラベル名への対応。未登録のキーはラベルの無い PR 扱い。
	Labels map[string][]string
	// LabelsUnknown は、ラベルを取れなかった形として扱うキー。
	LabelsUnknown map[string]bool
}

// SetForTest は照会を stub に差し替え、元へ戻す関数を返す。
func SetForTest(s Stub) (restore func()) {
	return setFuncs(
		func() (string, error) {
			if s.Self == "" {
				return "", fmt.Errorf("stub: 自分のログイン名が未設定")
			}
			return s.Self, nil
		},
		func(t target) (authorInfo, error) {
			key := t.kind + " " + t.arg
			login, ok := s.Authors[key]
			if !ok {
				return authorInfo{}, fmt.Errorf("stub: %s %s は未登録", t.kind, t.arg)
			}
			info := authorInfo{login: login, isBot: s.Bots[key]}
			if t.kind == "pr" && !s.LabelsUnknown[key] {
				info.labelsKnown = true
				info.release = slices.ContainsFunc(s.Labels[key], func(l string) bool {
					return slices.Contains(releaseLabels, l)
				})
			}
			return info, nil
		},
	)
}

// setFuncs は照会関数を差し替える。メモ化キャッシュも作り直し、DisableLookup の
// 効果も落とす (テスト間で状態を持ち越さないため)。
func setFuncs(self func() (string, error), authorOf func(target) (authorInfo, error)) (restore func()) {
	prevLookups, prevDisabled := lookups, disabled
	lookups, disabled = newCache(self, authorOf), false
	return func() { lookups, disabled = prevLookups, prevDisabled }
}
