package rules

import "testing"

// 機微情報を返すことが目的の REST エンドポイントは、GET でも deny にする。
// owner / repo などの名前の位置は見ないので、名前が機微な語でも巻き添えにしない。
func TestGHAPISensitivePath(t *testing.T) {
	runEvalCases(t, []evalCase{
		// メール
		{"user/emails", "gh api user/emails", Deny, false},
		{"-X GET user/emails", "gh api -X GET user/emails", Deny, false},
		{"先頭の / 付き", "gh api /user/emails", Deny, false},
		{"query string 付き", "gh api 'user/emails?per_page=1'", Deny, false},
		{"完全 URL", "gh api https://api.github.com/user/emails", Deny, false},
		{"user/public_emails", "gh api user/public_emails", Deny, false},
		{"org の招待", "gh api orgs/o/invitations", Deny, false},
		{"org の失敗した招待", "gh api orgs/o/failed_invitations", Deny, false},
		{"team の招待", "gh api orgs/o/teams/t/invitations", Deny, false},

		// 鍵
		{"user/keys", "gh api user/keys", Deny, false},
		{"user/keys/{id}", "gh api user/keys/1", Deny, false},
		{"user/gpg_keys", "gh api user/gpg_keys", Deny, false},
		{"user/ssh_signing_keys", "gh api user/ssh_signing_keys", Deny, false},
		{"users/{u}/keys", "gh api users/u/keys", Deny, false},
		{"users/{u}/gpg_keys", "gh api users/u/gpg_keys", Deny, false},
		{"deploy key", "gh api repos/o/r/keys", Deny, false},
		{"gh ssh-key list", "gh ssh-key list", Deny, false},
		{"gh gpg-key list", "gh gpg-key list", Deny, false},

		// secret: 名前の一覧は通し、公開鍵と個々の secret は止める
		{"repo の secret の公開鍵", "gh api repos/o/r/actions/secrets/public-key", Deny, false},
		{"repo の dependabot secret", "gh api repos/o/r/dependabot/secrets/NAME", Deny, false},
		{"repo から見た org secret の個別", "gh api repos/o/r/actions/organization-secrets/NAME", Deny, false},
		{"environment の secret の個別", "gh api repos/o/r/environments/prod/secrets/NAME", Deny, false},
		{"org の secret の公開先", "gh api orgs/o/codespaces/secrets/NAME/repositories", Deny, false},
		{"user の codespaces secret の個別", "gh api user/codespaces/secrets/NAME", Deny, false},
		{"private registry", "gh api orgs/o/private-registries", Deny, false},
		{"secret scanning の alert", "gh api repos/o/r/secret-scanning/alerts/1/locations", Deny, false},
		{"org の secret scanning", "gh api orgs/o/secret-scanning/alerts", Deny, false},
		{"repo の secret 一覧", "gh api -X GET repos/o/r/actions/secrets", Pass, true},
		{"repo から見た org secret 一覧", "gh api -X GET repos/o/r/actions/organization-secrets", Pass, true},
		{"environment の secret 一覧", "gh api -X GET repos/o/r/environments/prod/secrets", Pass, true},
		{"org の codespaces secret 一覧", "gh api -X GET orgs/o/codespaces/secrets", Pass, true},
		{"user の codespaces secret 一覧", "gh api -X GET 'user/codespaces/secrets?per_page=100'", Pass, true},
		{"gh secret list", "gh secret list", Pass, false},

		// token・webhook
		{"org の PAT", "gh api orgs/o/personal-access-tokens", Deny, false},
		{"org の PAT リクエスト", "gh api orgs/o/personal-access-token-requests", Deny, false},
		{"repo の webhook 設定", "gh api repos/o/r/hooks/1/config", Deny, false},
		{"org の webhook 設定", "gh api orgs/o/hooks/1/config", Deny, false},
		{"app の webhook 設定", "gh api app/hook/config", Deny, false},

		// 名前の位置は見ない
		{"keys という名前のリポジトリ", "gh api -X GET repos/o/keys", Pass, true},
		{"secrets という名前のリポジトリの issue", "gh api -X GET repos/o/secrets/issues", Pass, true},
		{"emails という名前のユーザー", "gh api -X GET users/emails", Pass, true},
		{"secrets という名前の environment", "gh api -X GET repos/o/r/environments/secrets", Pass, true},
		{"ファイル名が secrets のパス", "gh api -X GET repos/o/r/contents/actions/secrets", Pass, true},

		// 機微パスでない GET は今までどおり
		{"user", "gh api -X GET user", Pass, true},
		{"repo の webhook 一覧", "gh api -X GET repos/o/r/hooks", Pass, true},
	})
}
