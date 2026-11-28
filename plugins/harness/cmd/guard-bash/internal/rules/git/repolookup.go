package git

// リポジトリの設定と ref の包含関係の照会。taglookup.go と同じ実行位置と停止に従う。

import (
	"context"
	"os/exec"
	"strings"
)

// 照会の実体。テストで差し替える。
var (
	lookupPushConfig = localPushConfig
	lookupMerged     = mergedRefs
	lookupContaining = containingRefs
)

// repoMarkerKey はどのリポジトリの local の設定にも在るキー。
// `git config --get-regexp` はリポジトリの外でも global の設定を読んで成功するので、
// このキーが返るかで「リポジトリの設定を読めた」ことを確かめる。
const repoMarkerKey = "core.repositoryformatversion"

// pushConfigPattern は、引数に現れない ref を push に足しうる設定のキー。
const pushConfigPattern = `^(core\.repositoryformatversion|push\.followtags|push\.default|remote\..*\.(push|mirror))$`

// gitLines は dir で git を実行し、空でない出力行を返す。失敗 (タイムアウトを含む) は ok が false。
func gitLines(dir string, args ...string) (lines []string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	// #nosec G204 -- シェルを介さず argv で渡す。呼び出し側が、フラグに化けない位置にだけ名前を置く。
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return nil, false
	}
	for line := range strings.Lines(string(out)) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, true
}

// localPushConfig は、引数に現れない ref を push に足しうる設定 (push.followTags、push.default、
// remote.<name>.push、remote.<name>.mirror) を "<key> <value>" の行で返す。
func localPushConfig(dir string) ([]string, bool) {
	lines, ok := gitLines(dir, "config", "--get-regexp", pushConfigPattern)
	if !ok {
		return nil, false
	}
	var config []string
	inRepo := false
	for _, line := range lines {
		if strings.HasPrefix(line, repoMarkerKey+" ") {
			inRepo = true
			continue
		}
		config = append(config, line)
	}
	return config, inRepo
}

// mergedRefs は src から辿れるブランチと remote 追跡 ref を返す。src が空なら絞らない。
func mergedRefs(dir, src string) ([]string, bool) {
	args := []string{"for-each-ref", "--format=%(refname)"}
	if src != "" {
		// `=` で繋ぎ、src がフラグとして読まれないようにする。
		args = append(args, "--merged="+src)
	}
	refs, ok := gitLines(dir, append(args, "refs/heads", "refs/remotes")...)
	if ok || src == "" {
		return refs, ok
	}
	// 失敗が「src がリポジトリに無い」のか「リポジトリを読めない」のかを分ける。
	// 無い名前からは何も辿れない (git の push も失敗する)。
	_, inRepo := gitLines(dir, "rev-parse", "--git-dir")
	return nil, inRepo
}

// containingRefs は ref の先端を含むブランチ・remote 追跡 ref・タグを返す (ref 自身を含む)。
func containingRefs(dir, ref string) ([]string, bool) {
	return gitLines(dir, "for-each-ref", "--format=%(refname)", "--contains="+ref, "refs/heads", "refs/remotes", "refs/tags")
}

// pushConfig / merged / containing は、コマンドの動くリポジトリへの照会。
// どのリポジトリで動くか分からないときは ok が false。

func pushConfig() ([]string, bool) {
	if disabled || workdir == "" {
		return nil, false
	}
	return lookupPushConfig(workdir)
}

func merged(src string) ([]string, bool) {
	if disabled || workdir == "" {
		return nil, false
	}
	return lookupMerged(workdir, src)
}

func containing(ref string) ([]string, bool) {
	if disabled || workdir == "" {
		return nil, false
	}
	return lookupContaining(workdir, ref)
}

// RepoStub はテストで照会の代わりに答えるリポジトリの状態。
type RepoStub struct {
	// Tags は在るタグの名前。
	Tags []string
	// PushConfig は localPushConfig が返す行。
	PushConfig []string
	// Merged は src ごとの、src から辿れる ref。キーの無い src は何も辿れない。
	Merged map[string][]string
	// Containing は ref ごとの、その先端を含む ref。キーの無い ref はどこからも辿れない。
	Containing map[string][]string
}
