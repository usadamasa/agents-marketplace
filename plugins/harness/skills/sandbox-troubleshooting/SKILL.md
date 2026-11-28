---
name: sandbox-troubleshooting
description: >-
  strict sandbox 環境で Bash が想定外に失敗したとき (証明書検証、ネットワークやファイル書き込みの拒否、
  単発は通るのに形を変えると落ちる) の診断に使う。
  sandbox 設定 (excludedCommands, allowedDomains, allowWrite) の追加を検討するときにも引く。
  「システムプロンプトの sandbox 節にあるパスは Edit / Write ツールでも書き込めない」と brief や報告に書く前にも引く
  (sandbox が掛かるのは Bash だけで、Edit / Write ツールには掛からない)。
---

# sandbox-troubleshooting

Bash は原則 sandbox (Seatbelt) 内で動き、`sandbox.excludedCommands` のコマンドだけが外で動く。
外で動くコマンドに OS の保護は無く、防御線は PreToolUse の guard hook だけになる。

- `sandbox.*` (`sandbox.filesystem` の `allowWrite` / `denyRead` 等を含む) への変更は、
  settings ファイルの watch-and-reload で実行中のセッションにも反映される。公式ドキュメントの
  「When edits take effect」は `permissions` `hooks` `apiKeyHelper` を reload の対象として挙げ、
  `model` のようにセッション開始時だけ読む例外キーを別に列挙しており、`sandbox` はどちらにも
  明記が無い。実測でセッション内の反映が確認されており、ドキュメントの記載漏れとして
  [anthropics/claude-code#88708](https://github.com/anthropics/claude-code/issues/88708)
  が上がっている (not planned でクローズ、運営の返信は無い)。
  `excludedCommands` の照合などセッション開始時固定の部分が別にあるかは未確認。
- `allowUnsandboxedCommands: false` なら `dangerouslyDisableSandbox` はランタイムが無視する。guard-bash も deny する。
- `autoAllowBashIfSandboxed: true` なら、`permissions.allow` の `Bash(...)` は excluded のコマンドを指すものだけ意味を持つ。

## 症状別 早見表

| 症状 | 原因 | 対処 |
| ---- | ---- | ---- |
| `x509: OSStatus -26276` / `tls: failed to verify certificate` (Go 製 CLI) | Seatbelt が `trustd` への Mach IPC を遮断 | excluded 登録済みなら形を直す (「excludedCommands の照合」)。未登録なら登録する (「Go 製 CLI の TLS」) |
| 単発は通るのに、ループ・パイプ・変数前置きを付けると落ちる | 付けた形が照合から外れ、コマンド全体が sandbox 内で動いた | 素の 1 文で実行し直す |
| excluded 登録済みのコマンドが `Operation not permitted` で落ちる | 同上。sandbox 内で組込み deny や `allowWrite` 外のパスに当たった | 素の 1 文で実行し直す。途中まで進んだ状態が残る (「落ちた先が状態を壊すもの」) |
| 素の 1 文の git が `could not lock config file .git/config` で落ちる | `-C` `-c` `--git-dir=` の前置きで照合が外れた | オプションを外すか、対象リポジトリを cwd にしたセッションで実行する (「excludedCommands の照合」) |
| コマンドは成功したのに、ローカルの daemon との連携だけ壊れる | 同上。sandbox 内では loopback 接続が拒否される | 「落ちた先が状態を壊すもの」 |
| `dial tcp: i/o timeout` / DNS 解決失敗 | 接続先が `sandbox.network.allowedDomains` に無い | ドメインの追加を提案する。ネットワーク承認プロンプトは妥当なら承認してよい |
| OS 権限は正しいのに `permission denied` | 書き込み先が `sandbox.filesystem.allowWrite` に無い | パスの追加を提案する (ビルドキャッシュが典型) |
| `brew uninstall` / `brew install` / `brew untap` が `Operation not permitted` | `/opt/homebrew` への書き込みが `allowWrite` に無い | パスは足さない (ツール全体へ任意の書き込み権限を与えることになる)。ユーザーに `! brew uninstall ...` で実行してもらう。読み取り (`brew uses` `brew info` `brew autoremove --dry-run`) は sandbox 内で動く |
| `allowWrite` の範囲内なのに `Operation not permitted` | 組込み deny。`allowWrite` では上書きできない | 「Appendix: 組込み deny」 |
| ソケットは `ls` で見えるのに接続だけ失敗、`open` が `Code=-600`、bind が `PermissionError`、GUI が表示前に落ちる、`sips` が `Error 13` | sandbox が経路ごと遮断している | 「塞がれている経路」 |
| リポジトリのツールが `command not found` | sandbox ではなく、Bash の環境から direnv の env が外れた | PATH を解決する skill に従う |
| `permissionDecision:"deny"` のメッセージが出る | guard hook (sandbox とは別の層) | deny の文言に従う |
| ホーム配下のディレクトリで、Read などのツールと `find` `du` `tree` `ls -R` だけ deny される | guard-home-dir hook (sandbox とは別の層)。許すのは cwd と `~/.claude` と、guard-home-dir.yaml に書いたディレクトリだけ | 置き場を YAML に足してもらう (場所と書き方は deny の文言にある)。足さないなら `ls -l` と `rg -n` で読む。サイズは `ls -l` の合計で足りる |

sandbox 起因の失敗は再現性がある。リトライで通ったなら、実行し直した形がたまたま照合に一致しただけで原因は残っている。

## excludedCommands の照合

照合はコマンド文字列の字面に対して行われ、実際に何が実行されるかは見ない。
次の全てを満たすときだけ sandbox の外で動き、1 つでも外れるとコマンド全体が sandbox 内で動く。

- 文のつなぎは `;` と `&&` だけ
- 各文は単純コマンドで、コマンド名が excluded の対象か、cwd の絶対パスと字面が一致する `cd`
  (同じ場所でも末尾スラッシュや相対表記では外れる)
- コマンド名は bare name で書く。パターンは `<command> *` の字面に当たる。そのため絶対パス
  (`/Applications/Example.app/Contents/MacOS/example`)・symlink 経由・シェル関数やスクリプト内からの
  間接呼び出しはどれも外れる。パスの形のパターンも同じく字面の一致で、パターン側の `${HOME}` `$HOME` `~` は
  展開されず、コマンド名をクォートで囲んで呼ぶと外れる
- 変数の前置きが無い (静的な値でも外れる)
- リダイレクトは fd の複製 (`2>&1`) だけ
- 引数にコマンド置換の字面 (バッククォート、`$(`) が無い (`$VAR` `${VAR}` の展開は通る)
- git なら、値を取るグローバルオプション (`-C` `-c` `--git-dir=` `--work-tree=`) の前置きが無い。
  値を取らない `--no-pager` `--literal-pathspecs` は通る (2.1.277 の照合変更による回帰、
  [anthropics/claude-code#95455](https://github.com/anthropics/claude-code/issues/95455))

形ごとの判定は [references/excluded-command-shapes.md](references/excluded-command-shapes.md) にある。

guard-bash は、excluded のコマンドを含むのにこの形から外れるコマンドを、外れた原因を名指しして deny する。
`bash -c` と `eval` の文字列の中も見る。
`excludedCommands` は user スコープとプロジェクトの `.claude/settings*.json` から読む。
sandbox 内でも用途が成立するコマンド (`sandboxexcluded.go` の `sandboxTolerant`: go / node) と
read-only の git は deny しない。
これらも形が外れれば sandbox 内で動くので、ネットワークや組込み deny のファイルに触れたときは落ちる。
`task` は tolerant にしない。タスクの中に sandbox 内で壊れるもの (govulncheck の vuln.go.dev への TLS、
`~/.claude` への書き込み) があると、形の失敗がコードの失敗に見えるため。
`uv` は環境変数 `UV_KEYRING_PROVIDER=subprocess` (プライベートなパッケージ index の keyring 認証) が
設定されているときだけ tolerant から外れる。設定が無い `uv` は公開 PyPI だけを触るので、
形が外れて sandbox 内に落ちても困らない。設定がある環境で `cd <dir> && uv ...` のように
照合が外れると、keyring が読む gcloud の ADC を denyRead が塞いで 401 になる。そのため deny し、
`--project <絶対パス>` で渡す書き方を示す。

### 通る形への書き直し

| やりたいこと | 書き方 |
| ---- | ---- |
| 出力を絞る | ツール自身のオプション (`--jq`、`-q`)。無ければ標準出力のまま読む。`gh api` にファイルへ書くフラグは無く、contents は `-H 'Accept: application/vnd.github.raw+json'`、ジョブのログは `gh run view <run> --job <id> --log`、PR の変更ファイルは `gh pr diff <n> --name-only` で取る |
| `gh actions-lock` の結果を絞る | `--json=valid,findings` でフィールドを選ぶ。`--help` の例 (`--json 2>/dev/null \| jq .valid`) はリダイレクトとパイプで照合から外れる |
| 本文や JSON を渡す | Write ツールでファイルに書いてフラグで渡す (`--body-file`、`-F`、`--input`、`--file`) |
| 環境変数を渡す | 前置きを外す。要るなら Taskfile の `env:` に置く |
| cwd と別のリポジトリ・worktree から読む | 対象を絶対パスで引数に渡す (`cd` は前置きできない)。`gh` は `--repo` で外で動く。`git` の read-only な verb は `-C <絶対パス>` で sandbox 内でも成立する |
| cwd と別のリポジトリ・worktree へ書く | 書き込み系の git (`switch` `commit` `push` `restore` 等) は `-C` を付けた時点で sandbox 内に落ちる。別リポジトリならそれを cwd にした新セッション、同じリポジトリの別 worktree なら `EnterWorktree` で移ってから実行する。引数を取れない差分ベースの呼び出し (素の `crit` 等) も cwd のリポジトリを見るので同じ |
| 複数件を回す | excluded のコマンドだけを `;` で連結する。件数が多ければ awk などで連結コマンドを組み立てて表示させ、1 回の Bash 呼び出しとして実行する (生成したスクリプトを `.` や `bash` で読ませる形は外れる) |

### 落ちた先が状態を壊すもの

| 種類 | sandbox 内で起きること | 復旧と予防 |
| ---- | ---- | ---- |
| 作業ツリーを書き換える git (`checkout` `pull` `merge`) | index だけ進み、組込み deny のファイルの書き換えで止まる。`git status --short` が `MM` を返す | 素の `git checkout HEAD -- <path>` で戻し、素の形でやり直す |
| `-C` を付けた `git switch -c` / `git branch` | ブランチは作られ、upstream の設定だけ `.git/config` の EPERM で落ちる (`unable to write upstream branch configuration`) | そのリポジトリを cwd にしたセッションで `git branch --set-upstream-to=origin/<name>` を実行する。guard が deny するので普段はここへ落ちない |
| `git commit` | pre-commit ごと sandbox 内に落ち、hook がファイルを開けず stash も書けない。ステージした内容が塞がれたパスに触れなければ通ってしまう | 出力は切り詰めずに受ける (減らすなら `-q`) |
| loopback の daemon に生存確認をする CLI | 生きている daemon を死亡と判定して状態ファイルを消し、別ポートに建て直す。開いたままの画面やクライアントは古い daemon を向いたままになる | その CLI のデータディレクトリを `allowWrite` に入れない (照合外れを取りこぼしても書き込み拒否として落ちる) |

## 塞がれている経路

| 経路 | 症状 | 対処 |
| ---- | ---- | ---- |
| `open` (LaunchServices への Mach IPC) | 引数によらず `Code=-600 procNotFound` | ユーザーに見せるなら `Artifact` で publish する。`open *` は excluded にしない (`open -a Terminal ./tmp/x.sh` で guard を通らない任意コード実行になる) |
| listen socket の bind | ローカル HTTP サーバの起動が `PermissionError` | 立てない。同上 |
| Playwright MCP の `file:` URL | scheme を拒否される | Claude 自身が描画を見る必要があるときだけ、chrome-devtools MCP の `navigate_page` に `file://` の絶対パスを渡して `take_screenshot` を撮る。初回が白紙なら撮り直す |
| WindowServer への Mach IPC | GUI が表示前に落ちる (gpui なら `NoSupportedDeviceFound`) | ウィンドウを開くコマンドを excluded に置き、素の形で実行する |
| 起動中の GUI アプリ本体への Mach/XPC lookup (アプリ同梱の CLI) | `Connection Invalid error for service com.apple.hiservices-xpcservice`、または出力ゼロのハング。`sandbox_violations` は出ないので原因が見えない | excluded に置いて bare name で実行する。絶対パス形は試さない (返らなくなったプロセスは sandbox 内からは `ps` が `Operation not permitted`、`pgrep` が `sysmond service not found` で見えず、残ったかどうかも確かめられない) |
| Unix domain socket への `connect(2)` | ソケットは `ls` で見えるのに `connection refused` / `permission denied` (パスワードマネージャ、`docker.sock`、`gpg-agent`、`ssh-agent`) | CLI を excluded に置く。登録済みなら形の問題。例外は端末多重化ツールの socket (下の節) |
| `/var/folders` への書き込み | 引数なしの `mktemp` が落ちる。`sips` は `TMPDIR` を見ないので必ず `Error 13` で落ちる | `mktemp -d "${TMPDIR:-/tmp}/x.XXXXXX"`。`sips` は excluded に置き、素の 1 文で実行する |
| `SendUserFile` | `delivered` を返すがターミナルには何も出ない | `Artifact` を使う |

`$TMPDIR` は sandboxed と excluded のコマンドで別のディレクトリを指す。両者をまたぐ作業ファイルは `./tmp` に置く。

### 端末多重化ツールの socket

端末多重化ツール (rex など) の CLI は excluded に置かず、
socket の親ディレクトリを `allowUnixSockets` に置いて sandbox 内から呼ぶ。操作をパイプや
リダイレクトと組み合わせるためで、excluded だと guard が照合外れで落とす。

`allowUnixSockets` に `docker.sock` は足さない。sandbox 内から `docker run -v /:/host` が可能になる。

## Go 製 CLI の TLS

Seatbelt が `trustd` への Mach IPC を遮断するため、Go の crypto/x509 はシステム証明書ストアを引けない。
Anthropic の回答は "not planned" ([anthropics/claude-code#34876](https://github.com/anthropics/claude-code/issues/34876)) で、
対処は該当 CLI の `excludedCommands` への追加になる。
子プロセスは sandbox を継承するので、Go 製 CLI を呼ぶタスクランナーも同じ扱いになる。
`GODEBUG=x509usefallbackroots=1` は、依存を取る `go get` 自体が TLS で落ちるので使えない。

## 対処の順序

1. 形が照合から外れていないかを確かめ、外れていれば書き直す (設定の変更より先)
2. 接続先が足りないなら `sandbox.network.allowedDomains` への追加を提案する
3. sandbox 内では動作しない CLI なら `excludedCommands` への追加を提案する。write 操作のある CLI は、
   書き込みを止める guard-bash のルールも同じ変更に含める
4. 書き込み先が足りないなら `sandbox.filesystem.allowWrite` への追加を提案する

設定は事前に網羅せず、必要が判明した時点で足す。`excludedCommands` のパターンは `<command> *` の形。

ユーザーの `! <command>` は sandbox の外で動く (v2.1.260 以降。background session は例外で sandbox 内)。
頼むのは素の 1 文でも落ちる操作と excluded でないコマンドに限り、コマンドは 1 行で渡す。
excluded 登録済みのコマンドは素の形なら `!` と同じく外で動くので、頼む前に素の形で実行し直す。

## 誤診断

| 誤診断 | 実際 |
| ---- | ---- |
| 「TLS エラーは一時的なネットワーク問題」「リトライで通った」 | 照合の内外の差で、再現性がある。単独で実行し直した形が一致しただけ |
| 「govulncheck は sandbox の TLS 制約で手元では流れない。CI に任せる」 | excluded のコマンド (govulncheck 自身も、それを呼ぶタスクランナーも) は素の 1 文なら外で動いて通る。落ちたのは形が照合から外れて sandbox に落ちたときで、TLS は結果に過ぎない。CI に逃がす前に素の形で実行し直す |
| 「excluded に登録済みだから sandbox は無関係」「登録済みなのに効かない。sandbox が厳しくなった」 | 登録は必要条件で、形が外れれば sandbox 内で動く。設定を疑う前に素の 1 文で実行したかを確かめる |
| 「guard がブロックしているのでは」 | guard の deny には必ず `permissionDecision:"deny"` の文言が出る。無ければ sandbox |
| 「前はこの形で通った」 | 照合はバージョンで変わる。今の挙動で決める |
| 「ソケットに届かないのは `allowUnixSockets` に無いから」 | CLI が excluded なら素の呼び出しは通る。落ちたなら形の問題。例外は端末多重化ツールの socket |
| 「guard が deny しなかったので hook は正常に判定した」 | `onFailure: "block"` の無い PreToolUse hook は、起動に失敗しても非 0 で終わっても deny にならず、判定を経ずに通る。harness の guard は `onFailure: "block"` を付けているので、失敗すると止まる。それ以外の hook は、対象セッションの transcript を grep して hook の出力を確かめる |
| 「PATH に無いので絶対パスか前置きで呼ぶ」「`direnv exec` / `aqua exec` / `nix develop -c` で包む」 | guard が止める。PATH を解決する skill に従う |

## Appendix: 組込み deny

Claude Code が自分の設定とシェル・git の設定を守るための deny で、`allowWrite` より強く、設定では上書きできない。
効くのは sandbox 内の Bash だけで、Edit / Write ツールと sandbox の外で動くコマンドは書ける。
パスで塞がれるものは、システムプロンプトの sandbox 節にある `denyWithinAllow` の一覧で読める。

| 対象 | 範囲 |
| ---- | ---- |
| `.claude/settings.json` `.claude/settings.local.json` | プロジェクト root のものだけ。新規作成・書き込み・`touch` のどれも EPERM。cwd 配下にネストした `.claude/` には効かない |
| `.claude/skills` `.claude/hooks` `.claude/agents` `.claude/commands` | プロジェクト root のもの。ディレクトリの作成も EPERM。`.claude/` 直下のファイルは書ける |
| `~/.claude/` 配下 (`projects` `skills` `agents` `plugins` など) | `denyWithinAllow` の一覧のとおり |
| `~/.claude/jobs/` (= `$CLAUDE_JOB_DIR`) | background job のシステム指示は `$CLAUDE_JOB_DIR/tmp` を使えと言うが、`operation not permitted` で書けない。cwd 相対の `./tmp` を使う |
| `.git/hooks/` 配下と `.git/config` | cwd 配下のどの階層の `.git` でも。`.git` のそれ以外は書ける |
| `.gitmodules` `.gitconfig` `.bashrc` `.zshrc` `.zprofile` `.profile` `.mcp.json` `.ripgreprc` | **ファイル名**で塞がれる。cwd 配下のどの階層でも EPERM。`$TMPDIR` 配下は書ける |
| `.vscode/` `.idea/` の中 | cwd 配下のどの階層でも。ディレクトリ自体は作れる |

| やりたいこと | 対処 |
| ---- | ---- |
| 対象のファイルを編集する | Edit / Write ツールを使う。プロジェクトの settings は `/permissions` でもよい |
| コマンドに書かせる (commit 時の pre-commit、`git rm`、hook の導入) | 書き込むコマンドを `excludedCommands` に置き、素の形で実行する |
| 名前で塞がれるファイルを含むアーカイブを展開する | cwd 配下では展開先を移しても変わらない。展開するコマンドを `excludedCommands` に置く |

## この skill の更新

記述と挙動が食い違っても、進行中の作業は止めない。回避して先へ進み、更新は別セッションか subagent に渡す。
手順は [references/maintenance.md](references/maintenance.md) にある。
