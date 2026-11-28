---
name: setting-up-nix-devshell
description: >-
  リポジトリの開発ツールを flake.nix の devShell で固定するとき、CI で devShell を使うとき、
  nixpkgs のバージョンを上げるときに使う。
---

# setting-up-nix-devshell

開発ツールはリポジトリの `flake.nix` の devShell に並べ、`flake.lock` が nixpkgs の rev を固定する。
ツール単位のバージョンはその rev が持つバージョンになる。nix はローカルと CI の両方で Determinate Nix を使う
(CI は nix を入れる action が入れる)。devShell を Bash の PATH へ載せる `.envrc` の型、
`command not found` のときの対処、推奨しない呼び出しの形は toolchain:resolving-path skill が持つ。

Go を使うリポジトリは [references/go.md](references/go.md) も読む。devShell の go の置き方、
nixpkgs に無い Go 製ツールの足し方、Go のセキュリティ修正の取り込み方はそちらにある。

## flake.nix で決めること

書き方は公式リファレンスに従う ([flake の形式](https://nix.dev/manual/nix/stable/command-ref/new-cli/nix3-flake#flake-format)、
[`mkShell`](https://nixos.org/manual/nixpkgs/stable/#sec-pkgs-mkShell))。
ここに置くのは、リファレンスを読んでも決まらない点だけ。

- C を使わないリポジトリは `mkShellNoCC` にする。`mkShell` は stdenv の C コンパイラ (clang、Apple SDK、cctools) まで
  PATH に載せる。
- `systems` には CI 用の `x86_64-linux` を最初から入れる。後から足すまで CI だけが落ちる。
- nixpkgs を特定の rev (他のリポジトリとそろえるときなど) に固定するなら
  `nix flake lock --override-input nixpkgs github:NixOS/nixpkgs/<rev>` を使う。lock の `original` は
  `flake.nix` の `inputs.nixpkgs.url` (`nixos-unstable` など) のまま残り、次の `nix flake update` で通常の追従に戻る。

## git 管理下のファイルしか見えない

flake は git tree を store へコピーして評価するので、untracked のファイルは無いものとして扱われる。
`flake.nix` を書いたら `git add flake.nix` を先に実行し、`nix flake lock` → `git add flake.lock` → `nix develop` の
順にする。worktree でも同じ。`Git tree '…' has uncommitted changes` の warning は無害で、staged / modified の内容が
そのまま使われる。

## バージョンとハッシュを調べる

- 実際に入るバージョンは devShell に入ってから `<tool> --version` で見る。`nix search nixpkgs` はレジストリの nixpkgs を
  引くので `flake.lock` の rev とは別物で、attr 名 (`go_1_27` など) にも当たらない。
- `fetchFromGitHub` の `hash` や `vendorHash` は `pkgs.lib.fakeHash` を置いて `nix develop --command true` を実行し、
  エラーの `got:` から読む。

## バージョンを上げる

1. `nix flake update` (nixpkgs の rev が進む) か、`flake.nix` の `inputs.nixpkgs.url` を変える
2. `git add flake.lock`
3. `nix develop --command true` で store を温める (評価のキャッシュは toolchain:resolving-path skill が扱う)
4. サブディレクトリへ `cd` して hook に再評価させ、ツールが新しい `/nix/store/` に解決するのを確かめる
   (`.envrc` の反映は toolchain:resolving-path skill に従う)
5. 「バージョンが変わったときに確かめること」を通す

## バージョンが変わったときに確かめること

バージョンを上げたときも、aqua など別の固定から nixpkgs のバージョンへ移したときも、ツールのバージョンは変わる。

- lint 系ツールは新しいルールで落ちることがある。「ルール差」か「本当の問題」かを見て、コードを直すか設定で
  除外するかを決める。バージョンを戻す `overrideAttrs` は最後の手段。
- コード生成や検査のツールは挙動が変わって通らなくなることがある。たとえば oapi-codegen 2.8.0 は、プロパティ名に
  `"` を含む openapi.yaml を `may not contain a double quote` で拒否する。生成・検査のタスクは devShell で実行し、
  生成物の差分と出力を確かめる。nixpkgs のバージョンが使えないなら、使えるバージョンを自前でビルドして足す
  (Go 製なら [references/go.md](references/go.md) の「nixpkgs に無い Go 製ツール」)。
- PR の CI が呼ばないツール (リリースでしか動かないもの、ローカルのタスクからしか呼ばないもの) は、CI が緑でも
  確かめたことにならない。devShell で 1 回実行して出力を見て、PR 本文の検証欄に書く。たとえば goreleaser の
  バージョンは大きく動きうる (2.13.2 → 2.17.1 など)。`goreleaser check` と `goreleaser release --snapshot --clean` で、
  新しいバージョンでも `.goreleaser.yaml` が通ることを確かめる。
  自前でビルドした Go 製ツールが GOROOT を持たず、0 件の結果を exit 0 で返すこともある
  ([references/go.md](references/go.md) の「nixpkgs に無い Go 製ツール」)。
  CI が呼ばない経路では、これもこの実行でしか見つからない。

## CI

`actions/checkout` の後に、nix (Determinate Nix) を入れて devShell を用意する action を 1 回通す。
ジョブごとに同じ step を並べて書かず、1 つの composite にまとめてジョブからはそれを `uses:` で呼ぶ。
足した action のバージョンの固定は、GitHub Actions の依存を lockfile で固定する手順の skill に従う。

nix store の cache を使うかは計測して決め、数字を PR 本文に書く。

nix の導入と devShell の準備はジョブごとに数十秒かかり、課金もジョブごとに分単位で切り上がる。devShell を使う検証は、
ジョブを分けて縮まる時間がこの分を上回らない限り 1 つのジョブにまとめる。

ツールのバージョンの出所は `flake.lock` だけにする。devShell に入れたツールを CI が別経路で入れていたら、
その経路を外して devShell のツールを直接呼ぶ (`golangci-lint run` など)。別経路の例は `apt-get install`、
setup-go の `go-version:`、golangci-lint-action の `version:`。外したことで変わる CI の挙動 (action が付けていた PR の annotation や
レビューコメント) は PR 本文に書く。次の経路も例外にしない。

- リリース用の workflow (tagpr など) の setup-go や goreleaser-action (`version: "~> v2"`)。nix を入れる action で devShell を
  用意して `goreleaser release --clean` を直接呼ぶ。リリース経路は PR の CI で確かめられないので、
  「バージョンが変わったときに確かめること」の CI が呼ばないツールとして devShell で確かめる。
- reviewdog/action-yamllint のように、action が自前のツールを持つもの。ツールは devShell から呼ぶ。

寄せるときは、action が持っていたキャッシュの仕組みを落とさない。setup-uv の `uv.lock` をキーにした uv cache や
setup-go の module cache は、`actions/cache` などで同じキーの復元・保存を踏襲する。

例外は nodejs で、devShell に入れると npm の global prefix が `/nix/store` になり、CI の `npm install -g` が落ちうる。
CI で `npm install -g` するなら nodejs は devShell に入れずランナーのものを使い、理由を ci.yaml のコメントに書く。

## 症状

| 症状 | 原因 | 対処 |
| ---- | ---- | ---- |
| `does not provide attribute 'devShells.<system>.default'` (CI だけで出ることが多い) | その system を `systems` に入れていない | `systems` に足す |
| `go: go.mod requires go >= …`、`hash mismatch in fixed-output derivation '…-go-modules.drv'` | Go のツールチェーンか依存の固定がずれている | [references/go.md](references/go.md) の「症状」 |
| CI の nix の導入と devShell の準備 (普段は数十秒) が数分かかる | devShell の derivation が binary cache に無く、ジョブごとにソースからビルドしている (`overrideAttrs` で `src` や `version` を替えたツール) | nixpkgs のバージョンに戻すか、上流の配布バイナリを置く derivation に替える。go は [references/go.md](references/go.md) の「症状」 |
| `path '/nix/store/…-source/<file>' does not exist` | そのファイルが untracked (「git 管理下のファイルしか見えない」) | `git add <file>` |
| `sed: 1: "…": invalid command code` 等、macOS 版の構文が通らない | `mkShellNoCC` でも stdenv の `initialPath` (GNU の coreutils、findutils、gnused、gnugrep、gawk) が PATH の先頭に残り、リポジトリ内では `sed` `grep` `find` `stat` が GNU 版になる | スクリプトを両方で動く形にする (どちらの版かを実行時に判定して書き分ける) |

## 付録: aqua などから移すとき

1. `flake.nix` を書く (「flake.nix で決めること」、Go 製ツールは [references/go.md](references/go.md)) → `git add` → lock
   (「git 管理下のファイルしか見えない」)
2. `.envrc` を `use flake` の型にし、aqua.yaml と policy を消す (toolchain:resolving-path skill)。claude を起動したシェルに
   aqua の環境が残っていると、反映前は aqua の shim が使われる (その切り分けも toolchain:resolving-path skill が扱う)
3. CI を nix を入れる action と devShell のツールの直呼びへ寄せる (「CI」)
4. バージョンが変わったツールと CI が呼ばないツールを devShell で実行する (「バージョンが変わったときに確かめること」)
