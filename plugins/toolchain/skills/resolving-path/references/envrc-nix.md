# .envrc と nix (flake devShell) の反映

devShell の PATH はグローバルには入れず、リポジトリごとに direnv の `.envrc` から nix-direnv の `use flake` で渡す。
`flake.nix` の書き方、バージョンの上げ方、CI への渡し方は toolchain:setting-up-nix-devshell skill が持つ。

## .envrc の型

```bash
# 評価に失敗したら前回の devShell へ黙って戻らず、direnv ごと失敗させる
nix_direnv_disallow_fallback
use flake || exit 1
```

- `use flake` は nix-direnv が提供し、direnv の stdlib には無い。
  `~/.config/direnv/direnvrc` が nix-direnv の direnvrc を `source` している端末でだけ動く。
- `nix_direnv_disallow_fallback` を置かないと、`flake.nix` の書き損じや lock 更新直後のオフラインで評価に失敗したとき、
  nix-direnv は前回の devShell を warning 付きで使い続ける。direnv hook は stderr の warning を拾えないので、
  古いバージョンのツールが黙って動くことになる。
- `|| exit 1` は、`use flake` の失敗を `.envrc` 全体の失敗にするため。direnv は関数の失敗を `direnv export` の
  exit code に伝えないので、これが無いと hook は「成功」として PATH だけの環境を書く。
- リポジトリが自分の `bin/` を PATH へ足すなら、`use flake || exit 1` の後に `PATH_add bin` を置く。
- `NIX_DIRENV_FALLBACK_NIX` (PATH に nix が無いとき nix-direnv が同梱の nix を使う既定) は `.envrc` で消さない。
  nixpkgs の packaging が意図した仕組みで、同梱の nix も同じ daemon に繋いで動く。PATH に nix が無いのはシェルの設定で直す。

## キャッシュと評価の時間

nix-direnv は `use flake` の評価結果を `.direnv/` に置き、`.envrc`、`flake.nix`、`flake.lock`、
`~/.config/direnv/direnvrc` のいずれかが変わるまで nix を呼ばない (`.gitignore` に `/.direnv` を足す)。

| 状態 | 起きること |
| ---- | ---- |
| cache hit | nix を呼ばない。sandbox 内の `direnv export bash` でも通る |
| cache 無効 (watch 対象が変わった)、nix store は warm | nix が daemon に繋いで評価する。数秒。direnv hook の timeout (15 秒) に収まる |
| cold (store に無いパッケージがある。初回、`nix flake update` の直後) | cache.nixos.org からの取得で 1 分を超えうる。hook は timeout して何も書かない |

cold のときは、端末か Claude Code の Bash で先に `nix develop --command true` を前置きなしで実行して store を温める。
Claude Code を sandbox で動かしているなら、`nix` を `excludedCommands` に入れて sandbox の外で動かす
(sandbox 内の nix は daemon に繋げない)。その後サブディレクトリへ `cd` し直せば、hook の評価が timeout (15 秒) に収まる。
温めるのは nix store だけで、nix-direnv の cache (`.direnv/`) は作らない。worktree の初回は `.direnv/` が無いので、
温めた後も `cd` の後の評価に数秒かかり、その間の Bash 呼び出しは古い env で動く
(SKILL.md「`.envrc` を反映させる」の 4 で待つ)。direnv の `warn_timeout` (5 秒) は警告を出すだけで評価を打ち切らない。
打ち切るのは hook の timeout (15 秒) で、越えると env は書かれない。
`~/.config/direnv/direnvrc` を更新した直後は、全リポジトリの cache が 1 回ずつ無効になる。

## direnv を通らない経路

| 経路 | 渡し方 |
| ---- | ---- |
| CI | direnv は動かない。CI への渡し方は toolchain:setting-up-nix-devshell skill に従う |
| スクリプト | devShell の PATH を継承した shell で動くので、スクリプト側の配線は要らない |
| Claude Code の Bash | SessionStart / CwdChanged の direnv hook が `.envrc` を反映する (SKILL.md「仕組み」)。hook は sandbox の外で動くので daemon に届く |

## 症状

`flake.nix` の書き方に起因する症状 (`does not provide attribute …` など) は、toolchain:setting-up-nix-devshell skill が扱う。

| 症状 | 原因 | 対処 |
| ---- | ---- | ---- |
| `cannot connect to socket at '/nix/var/nix/daemon-socket/socket': Operation not permitted` | nix が sandbox 内で動いた。`nix …` の形が `excludedCommands` の照合から外れた (パイプ、`$(…)`、他コマンドとの連結)、または sandbox 内の `direnv export bash` が cache 無効で nix を呼んだ | `nix` は前置きやパイプを付けずに実行する。`.envrc` の反映は hook に任せる (SKILL.md「`.envrc` を反映させる」) |
| `nix-direnv: Evaluating current devShell failed. Falling back to previous environment!` | `.envrc` に `nix_direnv_disallow_fallback` が無く、前回の devShell が使われている | 直前の nix のエラーを読む。`.envrc` を上の型にする |
| `direnv: error exit status 1` (`using flake` の後) | `use flake` が失敗して `\|\| exit 1` が効いた | その上に出ている nix のエラーを読んで直す |
| `warning: unknown setting 'eval-cores'` / `'lazy-trees'` | PATH に nix が無く、nix-direnv が同梱の上流 nix で評価した (Determinate Nix の設定を知らない) | 動くが、シェルの設定 (`.zshenv` など) で PATH を直す。`.envrc` では直さない |
| `warning: ignoring the client-specified setting 'use-xdg-base-directories'` | trusted user でないため client 側の設定が daemon に渡らない | 無害。`/etc/nix/nix.custom.conf` の `trusted-users` に足せば消えるが、trusted user は daemon に対して root 相当の権限を持つので、警告のためだけには足さない |

## 推奨しない形

| 形 | 起きること |
| ---- | ---- |
| `nix develop -c <cmd>` / `--command`、`nix shell nixpkgs#<pkg> -c <cmd>`、`nix run nixpkgs#<pkg>` | 単発は動くが、毎回 flake を評価し直す (数秒)。`.envrc` が未反映なことを隠すので、次に前置きなしで実行した `task` がまた 127 になる。`.envrc` の他の `PATH_add` や export も無い |
| `/nix/store/<hash>-<pkg>/bin/<cmd>` の絶対パス、`PATH=/nix/store/…:$PATH <cmd>` | 評価時の store path をハードコードする形。`flake.lock` を更新すると消える。excludedCommands の照合からも外れる |

どれも `.envrc` を反映させれば要らない。例外は `nix develop --command true` で、包むコマンドが無く store を温めるだけなので、
上の問題は起きない (「キャッシュと評価の時間」)。
