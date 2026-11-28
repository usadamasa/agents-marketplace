---
name: resolving-path
description: >-
  Claude Code の Bash でリポジトリのツールが `command not found` になる、リポジトリが固定したバージョンと
  違うツールが動く、PATH の前置きや環境を包む呼び出しで補おうとしているときに使う。
  `.envrc`、aqua.yaml / policy を書くときにも使う。
---

# resolving-path

## 症状

- リポジトリのツールが `command not found` (exit 127) になる。`task` の中のツールだけが
  `executable file not found in $PATH` で落ちる形も同じ
- `which -a <tool>` の先頭がリポジトリの固定した場所 (aqua なら `…/aquaproj-aqua/bin/`、nix なら `/nix/store/…`) でない
  場所を指す。`command -v` だけが違う場所を指すなら、zsh のコマンドハッシュが古いまま残っている
  (「`.envrc` を反映させる」の 5)
- `cd` の直後の呼び出しだけ `command not found` になり、その次の呼び出しでは通る (「`.envrc` を反映させる」の 4)
- `.envrc` で export した変数が空
- aqua から nix へ移したリポジトリ (worktree を含む) で
  `read the policy config file: open …/aqua/policy.yaml: no such file or directory`。claude を起動したシェルの
  aqua の shim と `AQUA_POLICY_CONFIG` が、`.envrc` の反映前に使われている

## 診断

次の 2 つを別々の Bash 呼び出しで実行する。`$DIRENV_DIR` を展開せず `env` の出力で見る。

```bash
env | rg '^DIRENV_DIR='
direnv status
```

| 出力 | 状態 | 次にすること |
| ---- | ---- | ---- |
| `DIRENV_DIR` が `-<cwd のリポジトリか worktree>` | `.envrc` は反映済み | 「`.envrc` が反映済みなのに動かない」 |
| `DIRENV_DIR` が無いか別のディレクトリ、`direnv status` が `Found RC allowed 0` | `.envrc` が未反映 | 「`.envrc` を反映させる」の 2 から |
| `direnv status` が `Found RC allowed 1` (allow ファイルの日付が 1970) | `.envrc` が未許可。direnv の `whitelist.prefix` で許可済みにしている場所の外のリポジトリで起きる。元リポジトリで許可済みでも worktree は別パスなので改めて要る (`.envrc` を編集した後も同じ) | 「`.envrc` を反映させる」の 1 から |
| `direnv status` が `Found RC path` を出さない | cwd のリポジトリに `.envrc` が無い | `.envrc` を書く (nix なら [references/envrc-nix.md](references/envrc-nix.md)、aqua なら [references/envrc-aqua.md](references/envrc-aqua.md)) → 「`.envrc` を反映させる」の 1 から |

## `.envrc` を反映させる

1. 未許可なら `direnv allow` を前置きなしで実行する。許可を付けるだけで、Bash の環境はまだ変わらない。
   direnv の `whitelist.prefix` で許可済みにしている場所 (その配下の worktree を含む) では、この手順は要らない
2. 作業ディレクトリ内のサブディレクトリへ `cd` する (`cd cmd` など)
3. 別の Bash 呼び出しで `cd ..` する
4. 別の Bash 呼び出しで `env | rg '^DIRENV_DIR='` を実行し、`-<cwd のリポジトリか worktree>` を指すのを確かめる。
   前のディレクトリを指すなら hook がまだ評価している。同じコマンドを別の Bash 呼び出しで実行し直して待つ。
   `sleep` を挟んで待つ形は使わない (Bash ツールが前景の `sleep` を止める)。同じリポジトリのまま `.envrc` や
   `flake.lock` を変えたときは `DIRENV_DIR` が初めから cwd を指すので、5 の結果で確かめる
5. `which -a <tool>` の先頭がリポジトリの固定した場所 (`…/aquaproj-aqua/bin/` か `/nix/store/…`) を指すのを
   確かめる (ツール自身の `--version` でもよい)。`command -v` は zsh のコマンドハッシュが古いまま残ると、
   PATH が変わった後も前の場所を返すので、判定に使わない

4 か 5 を実行し直しても hook の timeout (15 秒) を過ぎて変わらないときは、hook が評価の途中で打ち切られ、
env を書かずに終わっている。nix の初回や `flake.lock` 更新直後に起きる。`nix develop --command true` を
前置きなしで実行して store を温めてから 2 と 3 をやり直す
([references/envrc-nix.md](references/envrc-nix.md)「キャッシュと評価の時間」)。

2 と 3 は別々の Bash 呼び出しで実行する。`cd cmd && cd ..` の 1 文では cwd が変わらず、CwdChanged が
発火しない。`.envrc` を評価して Bash の環境へ書く経路はこの hook だけなので、`direnv allow` の後にも
2 と 3 が要る (仕組みは後述)。

未許可のまま 2 と 3 を実行すると、hook は何も書かずに `direnv allow` を促す通知を出す。この通知は表に
出ないことがあるので、戻らなければ `direnv status` の `Found RC allowed` を見る (0 が許可済み)。

`.envrc` が未反映になる契機は次のとおり。

| 契機 | 起きること |
| ---- | ---- |
| EnterWorktree / ExitWorktree | CwdChanged が発火しないので変数は移動前のまま。2 から |
| セッション中に `.envrc` を編集して `direnv allow` | Bash の環境は変わらない |
| `.envrc` が未許可の場所で claude を起動 | SessionStart の hook は何も書かず、`.envrc` で足すはずの PATH が無い。許可済みなら起動時に反映される (`claude --worktree`、resume / compact 後も同じ) |
| 作業ディレクトリの外へ `cd` | Claude Code が cwd を戻す (`Shell cwd was reset to …`)。hook は戻し先で評価するので変わらない。別リポジトリを読むときは絶対パスを引数に渡す |

## `.envrc` が反映済みなのに動かない

| 症状 | 原因 | 対処 |
| ---- | ---- | ---- |
| `this package isn't allowed (code 002)` | ローカルレジストリのパッケージに policy が渡っていない、または policy 内の `path` がずれている | [references/envrc-aqua.md](references/envrc-aqua.md)「標準レジストリに無いパッケージ」 |
| `command is not found (code 004)`。`command -v` は通る | shim は宣言の無いパッケージにも残る。cwd の上の `aqua.yaml` に宣言が無いか、`aqua.yaml` の無い場所 (`$TMPDIR` 等) から呼んだ | `aqua.yaml` に足す。リポジトリ内を cwd にして実行する |
| 同じ `code 004`。リポジトリ内で `<ツール> --version` は通る | PATH にある aqua の shim を、別のツールが自分のキャッシュディレクトリ (`aqua.yaml` の無い場所) を cwd にして呼び直した | [references/envrc-aqua.md](references/envrc-aqua.md)「shim を別 cwd から起こすツール」 |
| `aqua failed … configuration file isn't found` | cwd の上に `aqua.yaml` が無い | 同上 |
| `x509: OSStatus -26276` / `tls: failed to verify certificate` | sandbox 内で lazy install した。前置きやパイプが付いて `excludedCommands` の照合から外れた形も含む | 前置きを付けず、`excludedCommands` に当たる形で実行し直す。切り分けは sandbox で失敗したときの切り分けの skill に従う |
| `aqua i -l` の後に起動すると落ちる | `-l` は shim を張るだけで、実体は初回起動で入る | 初回起動を sandbox の外で実行する |
| nix / nix-direnv のエラーや warning | devShell の側 | [references/envrc-nix.md](references/envrc-nix.md)「症状」 |
| `sed` `find` `stat` が macOS 版の構文を受け付けない、`does not provide attribute 'devShells…'` | `flake.nix` の側 | toolchain:setting-up-nix-devshell skill に従う |

## 推奨しない形

どれも「PATH に無い」を手で補う形で、`.envrc` を反映させれば要らない。

| 形 | 起きること |
| ---- | ---- |
| `direnv exec . <cmd>`、`PATH=…/aquaproj-aqua/bin:$PATH <cmd>`、`AQUA_POLICY_CONFIG=… <cmd>`、shim や実体の絶対パス | `excludedCommands` の照合から外れ、全体が sandbox 内で動く。lazy install が x509 で落ちる。`export` は次の Bash 呼び出しに残らない |
| `aqua exec -- <cmd>` | 単発は動くが、aqua は `<cmd>` 1 つしか解決せず、`<cmd>` が起こす子 (`task lint` の中の `golangci-lint`) は PATH に無く exit 127。`.envrc` の変数も無いので policy の要るパッケージは code 002 |
| `nix develop -c <cmd>`、`nix shell … -c <cmd>`、`nix run …`、`/nix/store/…/bin/<cmd>` の絶対パス、`PATH=/nix/store/…:$PATH <cmd>` | 単発は動くが毎回 flake を評価し直し、`.envrc` が未反映なことを隠す。store path の直書きは `flake.lock` を更新すると消える。例外は store を温めるだけの `nix develop --command true` (包むコマンドが無いので、この問題は起きない) |

| 言い訳 | 実際 |
| ---- | ---- |
| 「`aqua exec` ならさっき動いた」 | 単発は動く。`task` を包むと中のツールが 127、policy の要るものは 002 |
| 「`nix develop -c task lint` は動くし devShell と同じバージョンだ」 | 動くのは今だけ。次に前置きなしで実行する `task` は 127 のまま。`.envrc` を反映させれば包む必要が無い |
| 「このリポジトリには `.envrc` が無いから `aqua exec` で個別に実行する」 | 無いなら配線を足す。`use_aqua` は 10 行、`use flake` は 3 行で、以後は `task` も前置きなしで通る |
| 「worktree だから `.envrc` が違う」 | 違うのではなく未反映。サブディレクトリへ `cd` し直せば worktree の `.envrc` が読まれる |
| 「policy は前置きで渡すしかない」 | `.envrc` と Taskfile の `env:` が渡す。スクリプトなら自分で export する |

## 仕組み

Claude Code の Bash は direnv の shell hook を動かさない。見えるのは claude を起動したシェルの環境と、
hook が `CLAUDE_ENV_FILE` へ書いた `export`
([hooks リファレンス](https://code.claude.com/docs/en/hooks#persist-environment-variables))。

toolchain plugin は SessionStart と CwdChanged の hook `direnv-env.sh` を同梱する。この hook が、入力の `cwd` で
`direnv export bash` を実行して `CLAUDE_ENV_FILE` を置き換える。差分は「claude を起動したシェルの環境」から `cwd` への差分で、
毎回置き換えるので積み重ならない。書いた変数は、`excludedCommands` で sandbox の外に出したコマンドにも届く。
hook は PATH の `direnv` と `jq` を使う。

CwdChanged の hook は Bash ツールを待たせずに走り、`cd` の結果は hook の終わりを待たずに返る。差し替えた
env は、hook が書き終えた後に始まった Bash 呼び出しから効く (Claude Code 2.1.322 での実測。docs は明記していない)。
hook の timeout は plugin の `hooks/hooks.json` で 15 秒にしてあり、越えると env を書かずに打ち切られる。

作業ディレクトリの外へ `cd` すると、Claude Code は cwd をプロジェクトへ戻し、CwdChanged は再発火しない。
hook の入力では戻し先が `cwd`、`cd` の行き先が `new_cwd` に入るので、hook は `cwd` の方で評価する。
EnterWorktree / ExitWorktree は cwd を変えるが CwdChanged を起こさない。worktree へ入った後の `cd` 2 回はこのため。

PATH の解決はリポジトリごとの `.envrc` に閉じる。

## 設定の書き方

| 固定の手段 | reference |
| ---- | ---- |
| nix: `.envrc` の `use flake` と fallback の塞ぎ方、cache と hook の timeout、nix / nix-direnv の症状 | [references/envrc-nix.md](references/envrc-nix.md) |
| aqua: `.envrc` の `use_aqua`、aqua.yaml・policy・ローカルレジストリ、CI とスクリプトへの渡し方 | [references/envrc-aqua.md](references/envrc-aqua.md) |

どちらの `.envrc` でも、`.env` を読むなら素の `dotenv` ではなく direnv stdlib の `dotenv_if_exists` を使う。
素の `dotenv` は `.env` の無い場所 (worktree など) で評価のたびにエラーを出す。

`flake.nix` の devShell の書き方、aqua からの移行、バージョンの上げ方、CI での使い方は
toolchain:setting-up-nix-devshell skill が持つ。
