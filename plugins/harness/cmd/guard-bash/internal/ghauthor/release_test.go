package ghauthor

import "testing"

// ConfirmedNotReleasePR は「リリース PR でないと確定したとき」だけ true を返す。
// ラベルを確かめられない形で true を返すと、リリース PR が opt-in で通る。
func TestConfirmedNotReleasePR(t *testing.T) {
	defer SetForTest(Stub{
		Self: "octocat",
		Authors: map[string]string{
			"pr 5":     "octocat",
			"pr 7":     "octocat",
			"pr 8":     "app/tagpr-bot",
			"pr 9":     "octocat",
			"pr 10":    "octocat",
			"pr 11":    "octocat",
			"issue 42": "octocat",
		},
		Bots: map[string]bool{"pr 8": true},
		Labels: map[string][]string{
			"pr 5":  {"type: feature", "area: guard"},
			"pr 7":  {"tagpr"},
			"pr 8":  {"area: dev", "tagpr"},
			"pr 10": {"tagpr-config"},
		},
		LabelsUnknown: map[string]bool{"pr 9": true},
	})()

	cases := []struct {
		name string
		seg  string
		want bool
	}{
		{"リリースのラベルが無い", "gh pr merge 5 --squash", true},
		{"ラベルが 1 つも無い", "gh pr merge 11 --squash", true},
		{"リリースのラベルを名前の一部に含むだけ", "gh pr merge 10 --squash", true},
		// tagpr を PAT で動かすリポジトリでは、リリース PR の作成者が自分になる
		{"自分が作成した tagpr の PR", "gh pr merge 7 --squash", false},
		{"bot が作成した tagpr の PR", "gh pr merge 8 --merge", false},
		{"merge 以外の verb でも同じ", "gh pr ready 7", false},
		{"ラベルを取れなかった", "gh pr merge 9 --squash", false},
		{"未登録 (照会できない)", "gh pr merge 1234 --squash", false},
		{"対象を特定できない", "gh pr merge --squash", false},
		{"issue は PR ではない", "gh issue close 42", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfirmedNotReleasePR(argv(tc.seg)); got != tc.want {
				t.Errorf("ConfirmedNotReleasePR(%q) = %v, want %v", tc.seg, got, tc.want)
			}
		})
	}
}

// DisableLookup の後は、リリースのラベルが無い PR でも確定させない。
func TestConfirmedNotReleasePRDisabled(t *testing.T) {
	defer SetForTest(Stub{Self: "octocat", Authors: map[string]string{"pr 5": "octocat"}})()

	if !ConfirmedNotReleasePR(argv("gh pr merge 5")) {
		t.Fatal("前提: リリースのラベルが無い PR は確定するはず")
	}
	DisableLookup()
	if ConfirmedNotReleasePR(argv("gh pr merge 5")) {
		t.Error("DisableLookup の後なのに確定した")
	}
}

// parseView は gh pr view --json author,labels の出力からラベルまで読む。
func TestParseView(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want authorInfo
	}{
		{
			"リリースのラベルが付いた PR",
			`{"author":{"is_bot":true,"login":"app/tagpr-bot"},"labels":[{"id":"LA_x","name":"tagpr","description":"","color":"ededed"}]}`,
			authorInfo{login: "app/tagpr-bot", isBot: true, labelsKnown: true, release: true},
		},
		{
			"ラベルの無い PR",
			`{"author":{"is_bot":false,"login":"octocat"},"labels":[]}`,
			authorInfo{login: "octocat", labelsKnown: true},
		},
		{
			"別のラベルだけの PR",
			`{"author":{"is_bot":false,"login":"octocat"},"labels":[{"name":"area: guard"}]}`,
			authorInfo{login: "octocat", labelsKnown: true},
		},
		{
			// issue の照会は labels を頼まないので、フィールドが無い
			"labels の無い出力",
			`{"author":{"is_bot":false,"login":"octocat"}}`,
			authorInfo{login: "octocat"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseView(tc.out)
			if err != nil {
				t.Fatalf("parseView() = %v", err)
			}
			if got != tc.want {
				t.Errorf("parseView() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// gh へ渡す --json のフィールドは kind で分ける。ラベルを見るのは PR だけ。
func TestViewFields(t *testing.T) {
	if got, want := viewFields("pr"), "author,labels"; got != want {
		t.Errorf(`viewFields("pr") = %q, want %q`, got, want)
	}
	if got, want := viewFields("issue"), "author"; got != want {
		t.Errorf(`viewFields("issue") = %q, want %q`, got, want)
	}
}
