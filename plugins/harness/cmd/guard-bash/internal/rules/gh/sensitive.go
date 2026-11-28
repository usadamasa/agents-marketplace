package gh

import (
	"slices"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// sensitivePaths は機微情報 (メールアドレス・鍵・secret・token・webhook の secret) を返すことが
// 目的の REST エンドポイント。GraphQL の機微フィールドと同じ区分を、REST ではエンドポイント単位で止める。
// 前方一致で、`{}` は任意の 1 セグメント、`a|b` はどちらか。
// owner / repo などの名前の位置は `{}` にして、名前が機微な語でも巻き添えにしない。
// secret は名前の一覧 (`.../secrets`) を通し、その先の公開鍵と個々の secret だけを止める。
var sensitivePaths = [][]string{
	{"user", "emails|public_emails|keys|gpg_keys|ssh_signing_keys"},
	{"user", "codespaces", "secrets", "{}"},
	{"users", "{}", "keys|gpg_keys|ssh_signing_keys"},
	{"repos", "{}", "{}", "keys|secret-scanning"},
	{"repos", "{}", "{}", "actions|agents|codespaces|dependabot", "secrets", "{}"},
	{"repos", "{}", "{}", "actions|agents", "organization-secrets", "{}"},
	{"repos", "{}", "{}", "environments", "{}", "secrets", "{}"},
	{"repos", "{}", "{}", "hooks", "{}", "config"},
	{"orgs", "{}", "invitations|failed_invitations|private-registries|secret-scanning|personal-access-tokens|personal-access-token-requests"},
	{"orgs", "{}", "actions|agents|codespaces|dependabot", "secrets", "{}"},
	{"orgs", "{}", "teams", "{}", "invitations"},
	{"orgs", "{}", "hooks", "{}", "config"},
	{"teams", "{}", "invitations"},
	{"app", "hook", "config"},
}

// apiSensitivePath は gh api の宛先が sensitivePaths に当たるか。メソッドは問わない。
func apiSensitivePath(cl *argrule.Cmdline) bool {
	v := apiView(cl)
	if v == nil {
		return false
	}
	path := endpointPath(v.Pos[1])
	return slices.ContainsFunc(sensitivePaths, func(p []string) bool { return pathHasPrefix(path, p) })
}

// endpointPath はエンドポイントからホストと query string を外し、`/` で分けて返す。
// gh は `https://<host>/...` の完全 URL もそのまま受ける。
func endpointPath(endpoint string) []string {
	endpoint, _, _ = strings.Cut(endpoint, "?")
	for _, scheme := range []string{"https://", "http://"} {
		if rest, ok := strings.CutPrefix(endpoint, scheme); ok {
			_, endpoint, _ = strings.Cut(rest, "/")
			endpoint = strings.TrimPrefix(endpoint, "api/v3/")
			break
		}
	}
	return strings.Split(strings.TrimPrefix(endpoint, "/"), "/")
}

func pathHasPrefix(path, pattern []string) bool {
	if len(path) < len(pattern) {
		return false
	}
	for i, p := range pattern {
		if p != "{}" && !slices.Contains(strings.Split(p, "|"), path[i]) {
			return false
		}
	}
	return true
}
