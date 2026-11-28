package rules

import (
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/git"
)

// git の --no-verify は pre-commit / commit-msg / pre-push の hook を飛ばす。どのサブコマンドでも止める。
// commit の -n は --no-verify の短縮形だが、他のサブコマンドの -n は別の意味 (push --dry-run、
// log の件数、merge --no-stat) なので commit に限る。
func TestEvaluateGit(t *testing.T) {
	// push はリポジトリを照会するので、タグも設定も無いリポジトリを置く。
	t.Cleanup(git.SetTagsForTest())
	runEvalCases(t, []evalCase{
		// --no-verify はサブコマンドを問わず止める
		{"commit --no-verify", "git commit --no-verify -m x", Deny, false},
		{"commit の末尾に --no-verify", "git commit -m x --no-verify", Deny, false},
		{"push --no-verify", "git push --no-verify origin HEAD", Deny, false},
		{"merge --no-verify", "git merge --no-verify feature", Deny, false},
		{"rebase --no-verify", "git rebase --no-verify main", Deny, false},
		{"am --no-verify", "git am --no-verify patch.mbox", Deny, false},
		// git は一意な前方一致を受け付ける
		{"--no-verify の省略形", "git commit --no-verif -m x", Deny, false},
		{"--no-ve まで省略", "git commit --no-ve -m x", Deny, false},
		{"am は --verbose が無いので --no-v まで省略できる", "git am --no-v patch.mbox", Deny, false},

		// commit の -n
		{"commit -n", "git commit -n -m x", Deny, false},
		{"commit の末尾に -n", "git commit -am x -n", Deny, false},
		{"短縮フラグの束", "git commit -anm x", Deny, false},
		{"束の途中の -n", "git commit -nam x", Deny, false},

		// git のグローバルオプションを挟んでも拾う
		{"-C の後ろ", "git -C sub commit --no-verify -m x", Deny, false},
		{"-C の後ろの -n", "git -C sub commit -n -m x", Deny, false},
		{"-c の後ろの -n", "git -c core.editor=true commit -n -m x", Deny, false},
		{"--no-pager の後ろの -n", "git --no-pager commit -n -m x", Deny, false},

		// シェルへ文字列で渡す形と chain
		{"bash -c の中", "bash -c 'git commit --no-verify -m x'", Deny, false},
		{"eval の中", "eval 'git commit -n -m x'", Deny, false},
		{"chain の後段", "git stash push -m x && git commit --no-verify -m y", Deny, false},

		// 通す形
		{"commit -m", "git commit -m 'test'", Pass, false},
		{"commit -F", "git commit -F ./tmp/msg", Pass, false},
		{"commit -am", "git commit -am x", Pass, false},
		{"commit --amend", "git commit --amend --no-edit", Pass, false},
		{"push --force-with-lease", "git push --force-with-lease origin HEAD", Pass, false},
		{"push -n は dry-run", "git push -n origin HEAD", Pass, false},
		{"log -n は件数", "git log -n 5", Pass, false},
		{"log -n5", "git log -n5 --oneline", Pass, false},
		{"clean -n は dry-run", "git clean -n", Pass, false},
		{"merge -n は --no-stat", "git merge -n feature", Pass, false},
		{"-C status", "git -C sub status", Pass, false},
		{"--no-pager diff", "git --no-pager diff", Pass, false},
		{"stash push -m", "git stash push -m x", Pass, false},
		{"-- の後ろはパス", "git add -- --no-verify", Pass, false},
		{"--no-verbose は別のフラグ", "git commit --no-verbose -m x", Pass, false},
		{"--no-verify-signatures は別のフラグ", "git merge --no-verify-signatures feature", Pass, false},
		{"コミットメッセージ内で言及", `git commit -m "fix: --no-verify を止める"`, Pass, false},
		{"別 CLI 名をトークンに含む", "git remote add gh https://example.com/x.git", Pass, false},
	})
}

// タグの作成と削除は ask にする。リリースの workflow がタグの push で動くリポジトリでは、
// タグを打つことがリリースになり、消すと公開済みの参照が消える。
func TestEvaluateGitTag(t *testing.T) {
	t.Cleanup(git.SetTagsForTest("v1.2.3", "release/v2"))
	runEvalCases(t, []evalCase{
		// ローカルのタグ
		{"tag の作成", "git tag v1.2.3", Ask, false},
		{"コミットを指した作成", "git tag v1.2.3 abc1234", Ask, false},
		{"annotated tag", "git tag -a v1.2.3 -m 'release'", Ask, false},
		{"署名付き", "git tag -s v1.2.3 -m 'release'", Ask, false},
		{"上書き", "git tag -f v1.2.3", Ask, false},
		{"削除", "git tag -d v1.2.3", Ask, false},
		{"削除 (long)", "git tag --delete v1.2.3", Ask, false},
		{"-C の後ろ", "git -C sub tag v1.2.3", Ask, false},
		{"--no-pager の後ろ", "git --no-pager tag v1.2.3", Ask, false},
		{"bash -c の中", "bash -c 'git tag v1.2.3'", Ask, false},
		{"chain の後段", "git fetch && git tag v1.2.3", Ask, false},

		// remote のタグ
		{"push --tags", "git push --tags", Ask, false},
		{"push origin --tags", "git push origin --tags", Ask, false},
		{"push --follow-tags", "git push --follow-tags origin main", Ask, false},
		{"push --mirror", "git push --mirror", Ask, false},
		{"push tag <name>", "git push origin tag v1.2.3", Ask, false},
		{"push refs/tags/", "git push origin refs/tags/v1.2.3", Ask, false},
		{"push <src>:refs/tags/", "git push origin HEAD:refs/tags/v1.2.3", Ask, false},
		// --delete と :<ref> は、名前だけではタグかブランチかを決められない
		{"push --delete", "git push --delete origin v1.2.3", Ask, false},
		{"push -d", "git push origin -d v1.2.3", Ask, false},
		{"push :<ref>", "git push origin :v1.2.3", Ask, false},
		// 名前だけの refspec は、ローカルにその名前のタグがあるかを照会して決める
		{"push <タグ名>", "git push origin v1.2.3", Ask, false},
		{"push -u <タグ名>", "git push -u origin v1.2.3", Ask, false},
		{"push +<タグ名>", "git push origin +v1.2.3", Ask, false},
		{"push <タグ名>:<dst>", "git push origin v1.2.3:v1.2.3", Ask, false},
		{"push HEAD:<タグ名>", "git push origin HEAD:v1.2.3", Ask, false},
		{"push スラッシュを含むタグ名", "git push origin release/v2", Ask, false},
		{"push 2 つ目の refspec がタグ名", "git push origin main v1.2.3", Ask, false},
		{"push -- の後ろのタグ名", "git push origin -- v1.2.3", Ask, false},
		{"push tags/<name>", "git push origin tags/v9.9.9", Ask, false},
		// -C の先のリポジトリは照会できないので、タグでないと決められない
		{"push -C の後ろの refspec", "git -C sub push origin feat/x", Ask, false},
		{"push --git-dir= の後ろの refspec", "git --git-dir=other/.git push origin feat/x", Ask, false},
		// シェルが展開する名前は、実行時の値が分からないのでタグでないと決められない
		{"push 変数の refspec", `git push origin "$TAG"`, Ask, false},
		{"push 波括弧の変数の refspec", "git push origin ${TAG}", Ask, false},
		{"push コマンド置換の refspec", "git push origin $(git describe --tags)", Ask, false},
		{"push バッククォートの refspec", "git push origin `git describe --tags`", Ask, false},
		{"push 変数を dst に置いた refspec", `git push origin HEAD:"$BRANCH"`, Ask, false},
		{"push ブレース展開の refspec", "git push origin {main,v1.2.3}", Ask, false},
		{"push glob の refspec", "git push origin v1.*", Ask, false},
		// 引用符の無い変数は複数の語に割れるので、remote の位置でも refspec を含みうる
		{"push 引数全体が変数", "git push $ARGS", Ask, false},

		// git は長いオプションの一意な前方一致と、短縮フラグの束を受け付ける
		{"tag --del", "git tag --del v1.2.3", Ask, false},
		{"tag -fd", "git tag -fd v1.2.3", Ask, false},
		{"push --tag", "git push --tag", Ask, false},
		{"push --follow", "git push --follow origin main", Ask, false},
		{"push --mir", "git push --mir", Ask, false},
		{"push --del", "git push --del origin v1.2.3", Ask, false},
		{"push -fd", "git push -fd origin v1.2.3", Ask, false},
		// メッセージの値が一覧のフラグと同じ字面でも、作成として扱う
		{"tag -m の値が --list", "git tag -a v1.2.3 -m --list", Ask, false},
		{"tag -m の値が -l", "git tag -m -l v1.2.3", Ask, false},
		{"tag --message= の後ろの名前", "git tag --message=release v1.2.3", Ask, false},

		// 前方一致が別のオプションになる形は通す
		{"push --dry-run", "git push --dry-run origin HEAD", Pass, false},
		{"push --thin", "git push --thin origin HEAD", Pass, false},
		{"push --force", "git push --force origin HEAD", Pass, false},
		{"push -o の値が d を含む", "git push -o ci.skip-deploy origin HEAD", Pass, false},

		// 一覧と検証は通す
		{"tag の一覧", "git tag", Pass, false},
		{"tag -l", "git tag -l 'v1.*'", Pass, false},
		{"tag --list", "git tag --list", Pass, false},
		{"tag -n", "git tag -n5", Pass, false},
		{"tag --sort", "git tag --sort=-v:refname", Pass, false},
		{"tag --contains", "git tag --contains abc1234", Pass, false},
		{"tag --points-at", "git tag --points-at HEAD", Pass, false},
		{"tag --merged", "git tag --merged main", Pass, false},
		{"tag -v", "git tag -v v1.2.3", Pass, false},
		{"describe --tags", "git describe --tags", Pass, false},
		{"fetch --tags", "git fetch --tags", Pass, false},
		{"log でタグを指す", "git log v1.2.3..HEAD", Pass, false},

		// 普通の push は通す
		{"push", "git push", Pass, false},
		{"push -u", "git push -u origin feat/x", Pass, false},
		{"push <src>:<dst>", "git push origin HEAD:feat/x", Pass, false},
		{"タグという語を含むブランチ", "git push origin feat/tag-rule", Pass, false},
		{"コミットメッセージ内で言及", `git commit -m "git tag v1 を ask にする"`, Pass, false},
		{"push 完全修飾のブランチ", "git push origin refs/heads/v1.2.3", Pass, false},
		{"push タグと同名の remote", "git push v1.2.3", Pass, false},
		{"push タグと同名の remote へブランチ", "git push v1.2.3 main", Pass, false},
		// -C の先のリポジトリは設定も読めないので、refspec が無くてもタグを送らないと決められない
		{"push -C の後ろで refspec なし", "git -C sub push origin", Ask, false},
	})
}

// リポジトリを照会できないとき、push はどの形でも ask に落とす。
// refspec が無くても、設定 (push.followTags など) がタグを送りうる。
func TestEvaluateGitPushLookupUnavailable(t *testing.T) {
	t.Cleanup(git.SetTagsForTest())
	git.DisableLookup()
	runEvalCases(t, []evalCase{
		{"名前だけの refspec", "git push -u origin feat/x", Ask, false},
		{"<src>:<dst>", "git push origin HEAD:feat/x", Ask, false},
		{"refspec なし", "git push", Ask, false},
		{"remote だけ", "git push origin", Ask, false},
		{"HEAD", "git push -u origin HEAD", Ask, false},
		{"完全修飾のブランチ", "git push origin refs/heads/feat/x", Ask, false},
		// ブランチの削除は照会に頼らず通す
		{"ブランチの強制削除", "git branch -D feat/x", Pass, false},
	})
}

// 強制 push は、タグを指す形でなければ通す。修飾の無い dst が remote にだけ在るタグに当たる形
// (ローカルに同名のタグが無い) は、remote を照会しないので拾わない。
func TestEvaluateGitPushForce(t *testing.T) {
	t.Cleanup(git.SetTagsForTest("v1.2.3"))
	runEvalCases(t, []evalCase{
		{"-f と修飾の無い dst", "git push -f origin HEAD:feat/x", Pass, false},
		{"--force-with-lease と修飾の無い dst", "git push --force-with-lease origin HEAD:feat/x", Pass, false},
		{"+ と修飾の無い dst", "git push origin +HEAD:feat/x", Pass, false},
		{"-f とローカルのタグと同名の dst", "git push -f origin HEAD:v1.2.3", Ask, false},
		{"-f と名前だけの refspec", "git push -f origin feat/x", Pass, false},
		{"--force-with-lease と名前だけの refspec", "git push --force-with-lease origin feat/x", Pass, false},
		{"-f と完全修飾の dst", "git push -f origin HEAD:refs/heads/feat/x", Pass, false},
		{"強制でない修飾の無い dst", "git push origin HEAD:feat/x", Pass, false},
		{"-f と refspec なし", "git push -f", Pass, false},
	})
}

// 引数に現れないタグの送信は、リポジトリの設定を読んで拾う。
func TestEvaluateGitPushConfig(t *testing.T) {
	tests := []struct {
		name   string
		config []string
		tags   []string
		cases  []evalCase
	}{
		{"設定なし", nil, nil, []evalCase{
			{"refspec なし", "git push", Pass, false},
			{"-c で followTags を立てる", "git -c push.followTags=true push", Ask, false},
			{"-c で remote の push を足す", "git -c remote.origin.push=refs/tags/*:refs/tags/* push", Ask, false},
			{"-c で無関係のキー", "git -c core.quotePath=false push", Pass, false},
		}},
		{"push.followTags が true", []string{"push.followtags true"}, nil, []evalCase{
			{"refspec なし", "git push", Ask, false},
			{"ブランチの refspec", "git push -u origin feat/x", Ask, false},
			{"--no-follow-tags で打ち消す", "git push --no-follow-tags origin feat/x", Pass, false},
		}},
		{"push.followTags が値なし", []string{"push.followtags"}, nil, []evalCase{
			{"refspec なし", "git push", Ask, false},
		}},
		{"push.followTags が false", []string{"push.followtags false"}, nil, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		{"remote の mirror", []string{"remote.origin.mirror true"}, nil, []evalCase{
			{"refspec なし", "git push", Ask, false},
			{"--no-follow-tags では打ち消せない", "git push --no-follow-tags", Ask, false},
		}},
		{"remote の push がタグを含む", []string{"remote.origin.push refs/tags/*:refs/tags/*"}, nil, []evalCase{
			{"refspec なし", "git push", Ask, false},
			{"remote だけ", "git push origin", Ask, false},
			// 引数の refspec があるとき、git は remote.<name>.push を使わない
			{"引数の refspec", "git push origin feat/x", Pass, false},
		}},
		{"remote の push が全ての ref", []string{"remote.origin.push +refs/*:refs/*"}, nil, []evalCase{
			{"refspec なし", "git push", Ask, false},
		}},
		{"remote の push がブランチだけ", []string{"remote.origin.push refs/heads/*:refs/heads/*"}, nil, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		{"remote の push がタグ名", []string{"remote.origin.push v1.2.3"}, []string{"v1.2.3"}, []evalCase{
			{"refspec なし", "git push", Ask, false},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(git.SetRepoForTest(git.RepoStub{Tags: tc.tags, PushConfig: tc.config}))
			runEvalCases(t, tc.cases)
		})
	}
}

// リリース PR の head を含む履歴を push すると、PR が merged 扱いになりリリースが動きうる。
// `git merge tagpr-from-...` の後の push を、送る履歴にリリース PR の head が入っているかで拾う。
func TestEvaluateGitPushReleaseHead(t *testing.T) {
	const release = "refs/remotes/origin/tagpr-from-v1.2.3"
	tests := []struct {
		name  string
		stub  git.RepoStub
		cases []evalCase
	}{
		{"HEAD がリリース PR の head を含む", git.RepoStub{
			Merged:     map[string][]string{"HEAD": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release}},
		}, []evalCase{
			{"refspec なし", "git push", Ask, false},
			{"remote だけ", "git push origin", Ask, false},
			{"HEAD:main", "git push origin HEAD:main", Ask, false},
			{"完全修飾の dst", "git push origin HEAD:refs/heads/main", Ask, false},
			// 別のブランチはリリース PR の head を含まない
			{"含まないブランチ", "git push origin feat/x", Pass, false},
		}},
		{"ローカルのリリースブランチを含む", git.RepoStub{
			Merged: map[string][]string{"main": {"refs/heads/main", "refs/heads/tagpr-from-v1.2.3"}},
		}, []evalCase{
			{"名前だけの refspec", "git push origin main", Ask, false},
			{"+ 付き", "git push origin +main", Ask, false},
		}},
		{"リリース PR の head が remote のブランチに入っている", git.RepoStub{
			Merged:     map[string][]string{"HEAD": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release, "refs/remotes/origin/main"}},
		}, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		// base でないブランチへ先に push してあっても、base へ届く push は止める
		{"リリース PR の head が remote の作業ブランチにだけ入っている", git.RepoStub{
			Merged:     map[string][]string{"HEAD": {"refs/heads/feat/x", release}},
			Containing: map[string][]string{release: {release, "refs/remotes/origin/feat/x"}},
		}, []evalCase{
			{"HEAD:main", "git push origin HEAD:main", Ask, false},
			{"refspec なし", "git push", Ask, false},
		}},
		{"リリース PR の head が remote の既定ブランチに入っている", git.RepoStub{
			Merged:     map[string][]string{"HEAD": {"refs/heads/develop", release}},
			Containing: map[string][]string{release: {release, "refs/remotes/origin/HEAD"}},
		}, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		// refspec の無い push が現在のブランチ以外も送る設定では、全てのブランチを見る
		{"push.default が matching", git.RepoStub{
			PushConfig: []string{"push.default matching"},
			Merged:     map[string][]string{"": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release}},
		}, []evalCase{
			{"refspec なし", "git push", Ask, false},
			// 引数の refspec があれば、その src だけを送る
			{"引数の refspec", "git push origin feat/x", Pass, false},
		}},
		{"remote の push がブランチの pattern", git.RepoStub{
			PushConfig: []string{"remote.origin.push refs/heads/*:refs/heads/*"},
			Merged:     map[string][]string{"": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release}},
		}, []evalCase{
			{"refspec なし", "git push", Ask, false},
		}},
		{"push.default が simple", git.RepoStub{
			PushConfig: []string{"push.default simple"},
			Merged:     map[string][]string{"": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release}},
		}, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		{"リリース PR の head を含まない", git.RepoStub{
			Merged: map[string][]string{"HEAD": {"refs/heads/main", "refs/remotes/origin/feat/tagpr-from-x"}},
		}, []evalCase{
			{"refspec なし", "git push", Pass, false},
		}},
		{"--all はどのブランチも送る", git.RepoStub{
			Merged:     map[string][]string{"": {"refs/heads/main", release}},
			Containing: map[string][]string{release: {release}},
		}, []evalCase{
			{"--all", "git push --all origin", Ask, false},
			{"--branches", "git push --branches origin", Ask, false},
			{"refspec なし", "git push", Pass, false},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(git.SetRepoForTest(tc.stub))
			runEvalCases(t, tc.cases)
		})
	}
}

// ローカルのブランチの削除は、先端がほかの ref から辿れなくても通す。
func TestEvaluateGitBranchDelete(t *testing.T) {
	t.Cleanup(git.SetRepoForTest(git.RepoStub{Containing: map[string][]string{
		"refs/heads/lonely": {"refs/heads/lonely"},
	}}))
	runEvalCases(t, []evalCase{
		{"-D でほかの ref から辿れないブランチ", "git branch -D lonely", Pass, false},
		{"--delete --force", "git branch --delete --force lonely", Pass, false},
		{"変数の名前", `git branch -D "$BRANCH"`, Pass, false},
	})
}
