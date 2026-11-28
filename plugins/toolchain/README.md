# toolchain

開発ツールの置き場所 (Nix devShell、home-manager) と、
リポジトリが固定したツールを Claude Code の Bash の PATH へ載せる方法をまとめた plugin。
リポジトリの `.envrc` を Bash の環境へ反映する hook `direnv-env.sh` を同梱する。

```sh
claude plugin install toolchain@usadamasa
```

## 同梱の hook

`direnv-env.sh` は SessionStart と CwdChanged で動き、cwd の `.envrc` を `direnv export bash` で評価して
`CLAUDE_ENV_FILE` へ書く。timeout は 15 秒。仕組みと反映されないときの対処は toolchain:resolving-path skill にある。

## 利用者の設定

- `direnv` と `jq` を PATH に置く。hook は `direnv` が見つからないとエラーで終わる
- `.envrc` で `use flake` を使うなら、`~/.config/direnv/direnvrc` で nix-direnv の direnvrc を `source` する
- Claude Code を sandbox で動かしているなら、`nix` と、aqua の lazy install を伴うコマンドを
  `sandbox.excludedCommands` に入れる。sandbox の中の nix は daemon に繋げず、lazy install は証明書の検証で落ちる
- 同じ働きの hook を settings でも設定しているなら、どちらか一方を外す。両方あると `.envrc` を 2 回評価する
