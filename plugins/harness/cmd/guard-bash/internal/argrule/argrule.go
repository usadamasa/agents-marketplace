// Package argrule は簡単コマンド 1 つの argv に当てる判定ルールの部品を持つ。
//
// 引数の解釈は pflag に任せ、ルールは位置引数とフラグの有無・値で書く。
// CLI ごとのルールの中身は呼び出し側 (internal/rules とその下) が宣言する。
package argrule

import (
	"io"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

// FlagSpec は CLI の既知フラグを FlagSet へ宣言する。
//
// 宣言するのは次の 2 種類だけでよい。
//
//   - ルールが値や有無を見るフラグ (`gh api --method` 等)
//   - 値を取らないのに、宣言しないと直後の位置引数を値として食われるフラグ
//
// 未宣言のフラグは ParseErrorsAllowlist.UnknownFlags で読み飛ばす。pflag は
// 未宣言フラグの直後のトークンが `-` で始まらなければ、それを値とみなして
// 一緒に読み飛ばす。値を取らない未宣言フラグを verb の手前に置いた形
// (`gcloud compute instances --quiet delete`) では verb を取りこぼすので、
// そういうフラグを bool で宣言しておく。
type FlagSpec func(fs *pflag.FlagSet)

// Rule は argv 1 本に対する判定の 1 単位。実装は CmdAt・Check・When・Hinted に限る。
type Rule interface {
	match(cl *Cmdline) bool
	// Describe はルールの人向けの要約。ログと方針表 (-rules-doc) に使う。
	Describe() string
}

// Group は 1 CLI 分のルール束。
type Group struct {
	// Tool はこの group が扱うコマンド名。argv のトークンに現れた group だけを評価する。
	Tool string
	// Flags は Tool の既知フラグ。同じ Tool の group が複数あるときは 1 つにだけ書く。
	Flags FlagSpec
	// Allow は deny/ask を追い越す例外。
	Allow []Rule
	Deny  []Rule
	Ask   []Rule
	// DenyReason / AskReason は対応する Deny / Ask がある場合は必須。
	DenyReason string
	AskReason  string
}

// FirstMatch は最初に一致したルールの説明を返す。
func FirstMatch(rs []Rule, cl *Cmdline) (string, bool) {
	if r, ok := FirstMatchRule(rs, cl); ok {
		return r.Describe(), true
	}
	return "", false
}

// FirstMatchRule は最初に一致したルールを返す。
func FirstMatchRule(rs []Rule, cl *Cmdline) (Rule, bool) {
	for _, r := range rs {
		if r.match(cl) {
			return r, true
		}
	}
	return nil, false
}

// Hinted は Rule に、同じ目的を果たせる別の書き方の案内を添える。
// 案内は deny / ask の理由文の後ろに付く。
type Hinted struct {
	Rule
	Hint string
}

func (h Hinted) hint() string { return h.Hint }

type hinter interface{ hint() string }

// HintOf は r の案内を返す。When に包まれていても中の案内を引く。無ければ空文字。
func HintOf(r Rule) string {
	if h, ok := r.(hinter); ok {
		return h.hint()
	}
	return ""
}

// Cmdline は簡単コマンド 1 つの argv と、tool ごとの解析結果のキャッシュ。
//
// argv は mvdan.cc/sh が切り出した簡単コマンドの引数で、Args[0] はコマンド名。
// クォートは剥がされ、展開を含む引数は空文字になっている (bashguard.CallArgs)。
type Cmdline struct {
	Args    []string
	specFor func(tool string) FlagSpec
	views   map[int]*View
}

// View は Args[i] を tool とみなして、その後ろを解析した結果。
type View struct {
	FS  *pflag.FlagSet
	Pos []string
	// OK は解析に成功したか。値の必須なフラグに値が無い形などで false になる。
	OK bool
}

// NewCmdline は argv を包む。specFor は tool 名からその FlagSpec を引く (無ければ nil)。
func NewCmdline(args []string, specFor func(tool string) FlagSpec) *Cmdline {
	return &Cmdline{Args: args, specFor: specFor, views: map[int]*View{}}
}

// viewAt は Args[i] を tool とみなした解析結果を返す。
func (c *Cmdline) viewAt(i int) *View {
	if v, ok := c.views[i]; ok {
		return v
	}
	fs := newFlagSet(c.Args[i], c.specFor(c.Args[i]))
	fs.ParseErrorsAllowlist.UnknownFlags = true
	err := fs.Parse(c.Args[i+1:])
	v := &View{FS: fs, Pos: fs.Args(), OK: err == nil}
	c.views[i] = v
	return v
}

// HeadView は Args[0] の tool の解析結果を返す。解析に失敗したら nil。
func (c *Cmdline) HeadView() *View {
	if v := c.viewAt(0); v.OK {
		return v
	}
	return nil
}

// Changed は name のフラグが指定されたかを返す。
func (v *View) Changed(names ...string) bool {
	return slices.ContainsFunc(names, v.FS.Changed)
}

// newFlagSet は出力を捨てる FlagSet を作り、spec を宣言する。
func newFlagSet(name string, spec FlagSpec) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	if spec != nil {
		spec(fs)
	}
	return fs
}

// ParseStrict は未宣言のフラグを許さずに解析する。allow の例外のように
// 「このフラグ以外が付いていないこと」を要求するルールに使う。
func ParseStrict(args []string, spec FlagSpec) (*pflag.FlagSet, []string, bool) {
	fs := newFlagSet("strict", spec)
	if err := fs.Parse(args); err != nil {
		return nil, nil, false
	}
	return fs, fs.Args(), true
}

// AnyView は argv に現れた tool のどれかの解析結果が fn を満たすかを返す。
// `op run -- curl -d ...` のようなラップ形でも内側の tool を見るため、先頭に限らない。
func AnyView(cl *Cmdline, tool string, fn func(v *View) bool) bool {
	for i, a := range cl.Args {
		if a != tool {
			continue
		}
		if v := cl.viewAt(i); v.OK && fn(v) {
			return true
		}
	}
	return false
}

// NounSpan は path と verb の間に置ける noun の形。
// 「verb を位置引数のどこで見るか」を型で表す。
type NounSpan int

const (
	// NounNone は verb が path の直後に来る (`gh stack <verb>`)。
	NounNone NounSpan = iota
	// NounOne は noun がちょうど 1 つ (`gh <noun> <verb>`)。
	NounOne
	// NounUpTo2 は noun が 1〜2 個 (`op <noun> [<noun>] <verb>`)。
	NounUpTo2
	// NounQualified は noun がコロンで連結した 1 トークン (`ant beta:sessions:events <verb>`)。
	NounQualified
	// NounLoose は verb の位置を固定しない。noun の深さが不定な CLI 用で
	// (`gcloud compute instances delete`)、path の後ろの 2 つ目以降の位置引数の
	// どこに verb が現れても一致する。
	NounLoose
)

// CmdAt は「tool の位置引数の決まった位置に来る verb が write 系か」を表すルール。
//
// 位置引数は pflag がフラグとその値を取り除いた後のもの。Verbs を []string に
// してあるのは、verb 1 個の増減が 1 行の diff で済むようにするため。
type CmdAt struct {
	// Tool はコマンド名。
	Tool string
	// Head は Tool が argv[0] のときだけ一致させる。false なら argv のどこに Tool が
	// 現れても一致する (`op run -- gcloud auth login` のラップ形を拾う)。
	Head bool
	// Path は先頭に並ぶ固定の位置引数。要素は `pr|issue` の形で選択肢を書ける。
	// 末尾が `:*` の要素は、その名前自体とコロンで続く下位の resource に一致する
	// (`beta:vaults:*` は `beta:vaults` と `beta:vaults:credentials`)。
	Path []string
	// Nouns は Path と verb の間に置ける noun の形。
	Nouns NounSpan
	// Verbs は判定対象の verb。1 行 1 verb で昇順に並べる。空なら Path までの一致で決まる。
	Verbs []string
	// Exact は verb をトークン全体で比べる。false なら前方一致で、
	// `add` が `add-iam-policy-binding` にも一致する。
	Exact bool
}

// match は argv に Tool が現れ、その位置引数が形に合うかを返す。
func (c CmdAt) match(cl *Cmdline) bool {
	for i, a := range cl.Args {
		if a != c.Tool {
			continue
		}
		if c.Head && i != 0 {
			return false
		}
		if v := cl.viewAt(i); v.OK && c.matchPos(v.Pos) {
			return true
		}
	}
	return false
}

func (c CmdAt) matchPos(pos []string) bool {
	if len(pos) < len(c.Path) {
		return false
	}
	for i, want := range c.Path {
		if !pathTokenMatch(want, pos[i]) {
			return false
		}
	}
	return len(c.Verbs) == 0 || c.verbAfter(pos, len(c.Path))
}

// verbAfter は位置 p 以降に、Nouns の形で verb が来るかを返す。
func (c CmdAt) verbAfter(pos []string, p int) bool {
	switch c.Nouns {
	case NounNone:
		return c.verbAt(pos, p)
	case NounOne:
		return p < len(pos) && IsNoun(pos[p]) && c.verbAt(pos, p+1)
	case NounUpTo2:
		return p < len(pos) && IsNoun(pos[p]) &&
			(c.verbAt(pos, p+1) || (p+1 < len(pos) && IsNoun(pos[p+1]) && c.verbAt(pos, p+2)))
	case NounQualified:
		return p < len(pos) && isQualifiedNoun(pos[p]) && c.verbAt(pos, p+1)
	case NounLoose:
		if p >= len(pos) || !startsLower(pos[p]) {
			return false
		}
		for q := p + 1; q < len(pos); q++ {
			if c.verbAt(pos, q) {
				return true
			}
		}
	}
	return false
}

func (c CmdAt) verbAt(pos []string, q int) bool {
	if q >= len(pos) {
		return false
	}
	return slices.ContainsFunc(c.Verbs, func(v string) bool {
		if c.Exact {
			return pos[q] == v
		}
		return strings.HasPrefix(pos[q], v)
	})
}

// Describe はルールの形を 1 行で表す。
func (c CmdAt) Describe() string {
	parts := []string{c.Tool}
	for _, p := range c.Path {
		parts = append(parts, strings.ReplaceAll(p, "|", " | "))
	}
	switch c.Nouns {
	case NounOne, NounQualified:
		parts = append(parts, "<noun>")
	case NounUpTo2:
		parts = append(parts, "<noun>...")
	case NounLoose:
		parts = append(parts, "...")
	}
	s := strings.Join(parts, " ")
	if len(c.Verbs) == 0 {
		return s
	}
	suffix := ""
	if !c.Exact {
		suffix = "*"
	}
	vs := make([]string, len(c.Verbs))
	for i, v := range c.Verbs {
		vs[i] = v + suffix
	}
	return s + " {" + strings.Join(vs, ", ") + "}"
}

// pathTokenMatch は Path の要素 1 つとトークンを比べる。
func pathTokenMatch(want, tok string) bool {
	for alt := range strings.SplitSeq(want, "|") {
		if base, ok := strings.CutSuffix(alt, ":*"); ok {
			if tok == base || strings.HasPrefix(tok, base+":") {
				return true
			}
			continue
		}
		if tok == alt {
			return true
		}
	}
	return false
}

// IsNoun は英小文字で始まり、残りが英小文字・ハイフン・数字だけのトークンか。
//
// 先頭を英小文字に限るのは、数字を先頭にも許すと "2" のような純粋な数値
// (位置引数に現れる ID 等) まで noun と誤認するため。`workspace-settings-v2` の
// ような末尾にバージョンの数字を持つ noun を拾うには、先頭以外の数字を受ければ足りる。
func IsNoun(tok string) bool {
	return startsLower(tok) && strings.Trim(tok[1:], "abcdefghijklmnopqrstuvwxyz-0123456789") == ""
}

// isQualifiedNoun は英小文字で始まり、英小文字・コロン・ハイフンだけのトークンか。
func isQualifiedNoun(tok string) bool {
	return startsLower(tok) && strings.Trim(tok, "abcdefghijklmnopqrstuvwxyz:-") == ""
}

func startsLower(tok string) bool { return tok != "" && tok[0] >= 'a' && tok[0] <= 'z' }

// Check は CmdAt で表せない形 (gh api の method/field 判定、curl のフラグ等) を
// 関数で書くエスケープハッチ。Desc は方針表とログに出る。
type Check struct {
	Desc string
	Fn   func(cl *Cmdline) bool
}

func (c Check) match(cl *Cmdline) bool { return c.Fn(cl) }

// Describe は Desc を返す。
func (c Check) Describe() string { return c.Desc }

// When は Rule に事前条件を付ける。Rule が一致しても Cond が false を返すなら
// 一致として扱わない。argv の字面だけでは決まらない条件 (対象が自分の作った PR か、等)
// をルール宣言の側に置くための口。
//
// Cond は Rule が一致した argv に対してだけ呼ばれる。外部を叩く述語
// (gh の問い合わせ等) を置いても、当たらないコマンドでは走らない。
//
// 置き場所は allow と opt-in が基本。Cond が false になったとき deny/ask へ落ちて
// 安全側に倒れるためで、同じ理由から外部を叩く Cond は失敗時に false を返す
// (fail-closed)。
//
// deny に置いてよいのは、同じ形を ask のルールが受け止めるときだけ。Cond が
// false になったコマンドは deny を素通りするので、ask が無いとそのまま pass に
// なる。他人が作った PR / issue を止める gh のルールがこの形で、Cond は
// 「危険だと確定したときだけ true」を返し、確定できないコマンドは ask が拾う。
type When struct {
	Rule
	Cond func(cl *Cmdline) bool
	// Note は条件の説明。方針表に出る。
	Note string
}

// match は Rule と Cond の両方が満たされたときだけ true を返す。
func (w When) match(cl *Cmdline) bool { return w.Rule.match(cl) && w.Cond(cl) }

// Describe は Rule の説明に条件を添える。
func (w When) Describe() string { return w.Rule.Describe() + " (" + w.Note + ")" }

func (w When) hint() string { return HintOf(w.Rule) }
