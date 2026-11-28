package rules

import (
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// curlSendFlags はボディ・フォーム・アップロード・資格情報を送るフラグ。
// -K/--config は別ファイルから任意のオプションを読み込むので、上のフラグを隠せる。
var curlSendFlags = []string{
	"config",
	"cookie",
	"data",
	"data-ascii",
	"data-binary",
	"data-raw",
	"data-urlencode",
	"form",
	"form-string",
	"json",
	"oauth2-bearer",
	"upload-file",
	"user",
}

// curlWriteMethods は明示されたら止める HTTP メソッド。GET / HEAD だけを対象外に
// するのではなく危険な側を列挙する (allow に置くと同 group の deny を追い越すため)。
var curlWriteMethods = []string{"CONNECT", "DELETE", "OPTIONS", "PATCH", "POST", "PUT", "TRACE"}

// curlFlags は curl のフラグ。短縮フラグは `-sSd` のように束ねられるので、
// 日常的に使う短縮フラグは値の有無まで宣言しておく。未宣言の短縮フラグが束に
// 混ざると、pflag は後ろの位置引数をその値として読み飛ばしてしまう。
func curlFlags(fs *pflag.FlagSet) {
	for _, s := range []struct{ long, short string }{
		{"data", "d"}, {"form", "F"}, {"upload-file", "T"}, {"config", "K"},
		{"cookie", "b"}, {"user", "u"}, {"request", "X"},
		{"header", "H"}, {"output", "o"}, {"write-out", "w"}, {"user-agent", "A"},
		{"referer", "e"}, {"range", "r"}, {"cookie-jar", "c"}, {"dump-header", "D"},
		{"proxy", "x"}, {"max-time", "m"}, {"cert", "E"}, {"continue-at", "C"},
	} {
		fs.StringArrayP(s.long, s.short, nil, "")
	}
	for _, long := range []string{
		"data-ascii", "data-binary", "data-raw", "data-urlencode", "json",
		"form-string", "oauth2-bearer",
	} {
		fs.StringArray(long, nil, "")
	}
	for _, s := range []struct{ long, short string }{
		{"silent", "s"}, {"show-error", "S"}, {"fail", "f"}, {"insecure", "k"},
		{"location", "L"}, {"verbose", "v"}, {"include", "i"}, {"head", "I"},
		{"get", "G"}, {"globoff", "g"}, {"no-buffer", "N"}, {"remote-name", "O"},
		{"remote-header-name", "J"}, {"junk-session-cookies", "j"}, {"netrc", "n"},
		{"list-only", "l"}, {"progress-bar", "#"}, {"ipv4", "4"}, {"ipv6", "6"},
		{"http1.0", "0"}, {"compressed", ""},
	} {
		fs.BoolP(s.long, s.short, false, "")
	}
}

// curlSends は curl がデータを送る形か。
func curlSends(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "curl", func(v *argrule.View) bool {
		if v.Changed(curlSendFlags...) {
			return true
		}
		methods, _ := v.FS.GetStringArray("request")
		return slices.ContainsFunc(methods, func(m string) bool {
			return slices.ContainsFunc(curlWriteMethods, func(w string) bool { return strings.HasPrefix(m, w) })
		})
	})
}

// curlRules は curl のうちデータを送る形だけを止める。
//
// permissions の `Bash(curl:*)` deny は prefix マッチでフラグを解析できないため、
// 取得のためだけの curl まで巻き込んで止めていた (直近30日で 20 件)。curl は
// sandbox.excludedCommands に無いので sandbox の中で動き、到達先は
// sandbox.network.allowedDomains に縛られている。egress 先の制御はそちらに任せ、
// ここでは「ボディを送る形」と「資格情報をフラグで渡す形」だけを落とす。
//
// `-H 'Authorization: ...'` のような自前ヘッダは通す。ヘッダは取得のために日常的に
// 要るもので、これを止めると deny の範囲が元の blanket deny に戻ってしまう。
var curlRules = &argrule.Group{
	Tool:  "curl",
	Flags: curlFlags,
	Deny: []argrule.Rule{
		argrule.Check{
			Desc: "curl のボディ・フォーム・アップロード・資格情報のフラグ、非 GET の --request",
			Fn:   curlSends,
		},
	},
	DenyReason: "curl のデータ送信 (ボディ・アップロード・資格情報・非 GET メソッド) をブロックしました",
}
