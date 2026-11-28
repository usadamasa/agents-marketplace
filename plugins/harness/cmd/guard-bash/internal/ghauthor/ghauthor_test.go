package ghauthor

import (
	"strings"
	"testing"
)

// argv はテストの入力を argv に分ける。入力はクォートを含まない形だけにする。
func argv(s string) []string { return strings.Fields(s) }

func TestParseTarget(t *testing.T) {
	cases := []struct {
		name string
		seg  string
		want target
		ok   bool
	}{
		{"pr 番号", "gh pr edit 123 --title changed", target{kind: "pr", arg: "123"}, true},
		{"issue 番号", "gh issue close 42", target{kind: "issue", arg: "42"}, true},
		{"--repo 付き", "gh pr merge --repo owner/repo 123", target{}, false},
		{"--repo が番号の後ろ", "gh pr merge 123 --repo owner/repo", target{kind: "pr", repo: "owner/repo", arg: "123"}, true},
		{"--repo=owner/repo", "gh pr edit 7 --repo=owner/repo --title t", target{kind: "pr", repo: "owner/repo", arg: "7"}, true},
		{"-R owner/repo", "gh issue edit 7 -R owner/repo --body b", target{kind: "issue", repo: "owner/repo", arg: "7"}, true},
		{"PR の URL", "gh pr edit https://github.com/o/r/pull/9 --title t", target{kind: "pr", arg: "https://github.com/o/r/pull/9"}, true},
		{"issue の URL", "gh issue close https://github.com/o/r/issues/9", target{kind: "issue", arg: "https://github.com/o/r/issues/9"}, true},

		// 特定できない形
		{"引数なし", "gh pr edit", target{}, false},
		{"フラグのみ", "gh pr edit --add-assignee octocat", target{}, false},
		{"verb の直後がフラグ", "gh pr merge --squash 123", target{}, false},
		{"番号でも URL でもない", "gh issue edit main", target{}, false},
		{"--repo の値が owner/repo の形でない", "gh pr edit 7 --repo ../evil", target{}, false},
		{"--repo の値が無い", "gh pr edit 7 --repo", target{}, false},
		{"別リポジトリの URL 形式でない", "gh pr edit https://github.com/o/r/commit/abc", target{}, false},
		{"noun が対象外", "gh release edit v1.0.0", target{}, false},
		{"先頭が gh でない", "op run -- gh pr edit 123", target{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTarget(argv(tc.seg))
			if ok != tc.ok {
				t.Fatalf("parseTarget(%q) ok = %v, want %v", tc.seg, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("parseTarget(%q) = %+v, want %+v", tc.seg, got, tc.want)
			}
		})
	}
}

// AuthoredBySelf / AuthoredByOther はどちらも「確定したときだけ true」を返す。
// 照会できない segment では両方 false になり、呼び出し側は安全側 (ask) に倒れる。
func TestAuthoredBy(t *testing.T) {
	defer SetForTest(Stub{
		Self: "octocat",
		Authors: map[string]string{
			"pr 5":     "octocat",
			"pr 9":     "OCTOCAT", // ログイン名は大文字小文字を区別しない
			"pr 99":    "someone-else",
			"issue 42": "octocat",
			"issue 77": "someone-else",
		},
	})()

	cases := []struct {
		name      string
		seg       string
		wantSelf  bool
		wantOther bool
	}{
		{"自分の PR", "gh pr edit 5 --title changed", true, false},
		{"自分の PR (大文字小文字違い)", "gh pr merge 9 --squash", true, false},
		{"他人の PR", "gh pr edit 99 --body b", false, true},
		{"自分の issue", "gh issue close 42", true, false},
		{"他人の issue", "gh issue edit 77 --body b", false, true},
		{"未登録 (照会できない)", "gh pr edit 1234", false, false},
		{"対象を特定できない", "gh pr edit --title changed", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AuthoredBySelf(argv(tc.seg)); got != tc.wantSelf {
				t.Errorf("AuthoredBySelf(%q) = %v, want %v", tc.seg, got, tc.wantSelf)
			}
			if got := AuthoredByOther(argv(tc.seg)); got != tc.wantOther {
				t.Errorf("AuthoredByOther(%q) = %v, want %v", tc.seg, got, tc.wantOther)
			}
		})
	}
}

// AuthoredByBot は is_bot だけで判定する。ログイン名 (dependabot / renovate 等) を
// 列挙しないので、is_bot が立っていればどの bot でも true になる。
func TestAuthoredByBot(t *testing.T) {
	defer SetForTest(Stub{
		Self: "octocat",
		Authors: map[string]string{
			"pr 501": "app/dependabot",
			"pr 504": "app/renovate",
			"pr 99":  "someone-else",
			"pr 5":   "octocat",
		},
		Bots: map[string]bool{
			"pr 501": true,
			"pr 504": true,
			// pr 99 / pr 5 は未登録 (= false) のまま
		},
	})()

	cases := []struct {
		name string
		seg  string
		want bool
	}{
		{"dependabot", "gh pr merge 501 --squash", true},
		{"別の bot (renovate)", "gh pr merge 504 --squash", true},
		{"bot ではない他人", "gh pr merge 99 --squash", false},
		{"bot ではない自分", "gh pr merge 5 --squash", false},
		{"未登録 (照会できない)", "gh pr merge 1234 --squash", false},
		{"対象を特定できない", "gh pr merge --squash", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AuthoredByBot(argv(tc.seg)); got != tc.want {
				t.Errorf("AuthoredByBot(%q) = %v, want %v", tc.seg, got, tc.want)
			}
		})
	}
}

// AuthoredByBot は自分のログイン名を問わないので、その照会に失敗しても
// 対象の作成者だけで確定する (AuthoredBySelf / AuthoredByOther とは異なる)。
func TestAuthoredByBotSelfUnknown(t *testing.T) {
	defer SetForTest(Stub{
		Authors: map[string]string{"pr 501": "app/dependabot"},
		Bots:    map[string]bool{"pr 501": true},
	})()

	if !AuthoredByBot(argv("gh pr merge 501")) {
		t.Error("自分のログイン名を取れなくても is_bot だけで確定するはず")
	}
	if AuthoredByOther(argv("gh pr merge 501")) || AuthoredBySelf(argv("gh pr merge 501")) {
		t.Error("自分のログイン名を取れないのに AuthoredBySelf/Other が確定した")
	}
}

// 自分のログイン名を取れないときは、作成者が取れても判定を確定させない。
func TestAuthoredBySelfUnknown(t *testing.T) {
	defer SetForTest(Stub{Authors: map[string]string{"pr 5": "octocat"}})()

	if AuthoredBySelf(argv("gh pr edit 5")) || AuthoredByOther(argv("gh pr edit 5")) {
		t.Error("自分のログイン名を取れないのに判定が確定した")
	}
}

// DisableLookup の後は、作成者が分かる対象でも判定を確定させない。
// SetWorkdir は 1 回の判定の始まりなので、この効果を解除する。
func TestDisableLookup(t *testing.T) {
	defer SetForTest(Stub{Self: "octocat", Authors: map[string]string{"pr 99": "someone-else"}})()

	if !AuthoredByOther(argv("gh pr edit 99")) {
		t.Fatal("前提: 他人の PR だと確定するはず")
	}
	DisableLookup()
	if AuthoredByOther(argv("gh pr edit 99")) || AuthoredBySelf(argv("gh pr edit 99")) {
		t.Error("DisableLookup の後なのに判定が確定した")
	}
	SetWorkdir("")
	if !AuthoredByOther(argv("gh pr edit 99")) {
		t.Error("SetWorkdir で照会が再開しなかった")
	}
}

// 同じ対象への照会はプロセス内で 1 回だけに抑える (deny の事前条件と opt-in の
// 事前条件で二重に呼ばれるため)。
func TestAuthorLookupMemoized(t *testing.T) {
	calls := 0
	defer setFuncs(
		func() (string, error) { return "octocat", nil },
		func(target) (authorInfo, error) { calls++; return authorInfo{login: "someone-else"}, nil },
	)()

	for range 3 {
		AuthoredByOther(argv("gh pr edit 99"))
	}
	if calls != 1 {
		t.Errorf("author の照会が %d 回走った, want 1", calls)
	}
}
