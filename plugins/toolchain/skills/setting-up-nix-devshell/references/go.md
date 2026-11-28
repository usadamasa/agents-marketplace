# Go を使うリポジトリの devShell

SKILL.md の手順に、Go を使うリポジトリで足す判断を置く。

## devShell の go

- `env.GOTOOLCHAIN = "local"` を置く。go.mod の `go` 行が devShell の go より新しくなったとき、sandbox 内で黙って
  toolchain を落としに行って x509 で失敗するより「devShell の go が古い」と即座に分かる。代わりに go.mod の
  `toolchain` 行は devShell の中で効かなくなる (「Go のセキュリティ修正を取り込む」)。
- バージョンが変わったら、devShell の `go version` が go.mod の `go` 行以上かを見る。nixpkgs の `go_1_XX` の
  patch バージョンは go.mod の `go` 行 (例: `1.26.1`) より古いことがある。
- nixpkgs が配るツールと `packages` の配布バイナリは、derivation ごとに devShell の go とは別の go でビルドされうる。
  nixpkgs を上げて go の修正を取り込むときは、devShell の `go version` ではなく、バイナリごとに `go version <パス>` で
  見る。パスがラッパースクリプトなら、同じパッケージの実バイナリで見る。

## nixpkgs に無い Go 製ツール

`buildGoModule` でビルドして devShell に足す。ビルドに使う go は devShell の go とそろえる。per-system の let で
`go = devGo pkgs;` と `buildGoModule = pkgs.buildGoModule.override { inherit go; };` を 1 回だけ束ね、devShell の
`packages` と `buildGoModule` を呼ぶ全箇所で共有する。素の `pkgs.buildGoModule` は nixpkgs 既定の go でビルドするので、
そこだけ古い go が残る。ビルドしたバイナリには `go version <パス>` を当てて確かめる。
次のどれかに当たるツールは、ビルドと CI が通ったまま壊れる。足すたびに当てはまるかを見る。

- `-trimpath` でビルドされて GOROOT が埋め込まれない。実行時に go/packages などで Go の標準ライブラリを読むツール
  (go/packages でパッケージを読み込む静的解析器など) は、エラーを出さず、0 件の結果を exit 0 で返す。`makeWrapper` で GOROOT の既定値
  (ビルドに使った go の `share/go`) を `--set-default` し、`allowGoReference = true` を付ける。
  ビルドしたツールは、exit code だけでなく出力が空でないことまで確かめる (CI が呼ばないツールなら
  SKILL.md の「バージョンが変わったときに確かめること」に従って devShell で実行する)。
- 対象がリポジトリ直下に `go.work` を持つと、`go mod vendor` が `cannot be run in workspace mode` で落ちる。
  `env.GOWORK = "off"` を足す。依存のバージョンは `go.work` ではなくそのモジュールの `go.mod` の require で決まるので、
  上流の開発時のバージョンとずれうる。
- VCS 情報を持たず、`debug.ReadBuildInfo` のバージョンが `(devel)` になる。バージョンを出力に埋めるツール
  (例: oapi-codegen は生成コードの先頭にバージョンを書く) は生成物に差分を出し、
  CI の「生成後に `git status --porcelain` が空か」の検査を落とす。
  ツールがバージョンを上書きする変数を持っていれば `ldflags` で渡す
  (oapi-codegen は `ldflags = [ "-X main.noVCSVersionOverride=v${version}" ];`)。生成タスクを走らせて差分ゼロを確かめる。
- 既定で checkPhase (`go test`) を走らせる。テストがネットワークを使うツールは、ネットワークの無い Linux の
  build sandbox (CI) では `dial tcp: lookup …` で落ちる。oapi-codegen の `cmd/oapi-codegen` のテストは
  spec を raw.githubusercontent.com から取るので、これに当たる。macOS は build sandbox が既定で無効なので
  ローカルでは通り、気づけない。
  該当するなら `doCheck = false;` を理由のコメント付きで足す。

## 自分のモジュールを packages で出すとき

自分のモジュールを flake の `packages` として `buildGoModule` で出しているリポジトリは、依存のバージョンを上げて
`go.sum` が変わると `vendorHash` がずれ、`nix build` と CI の nix package ジョブが落ちる。依存を上げたら
`nix build .#default --no-link` を打ち、エラーの `got:` の値で `vendorHash` を付け直して同じ PR に含める。
nixpkgs を上げたときは、`nix build .#default` の後に `go version result/bin/<name>` で配布バイナリの go も見る。

## Go のセキュリティ修正を取り込む

Go のセキュリティ修正が出てから、nixpkgs の `go_1_XX` がそのバージョンになるまでには間が空く。
修正バージョンの go を持つ rev があれば SKILL.md の「バージョンを上げる」で済む。まだ無い間は次の点を見る。

- devShell の go だけを、go.dev の配布バイナリに差し替える。devShell 用の go (`devGo`) を pkgs を受ける関数として置き、
  `pkgs.lib.versionOlder pkgs.go_1_XX.version "<修正バージョン>"` のときだけ配布バイナリの derivation を返し、
  それ以外は `pkgs.go_1_XX` を返す。配布バイナリは取り出して置くだけなので、CI の devShell の準備は延びない。
  `dontFixup` は strip が darwin の署名を壊すのを避ける。

  ```nix
  devGo =
    pkgs:
    if pkgs.lib.versionOlder pkgs.go_1_27.version "1.27.4" then
      pkgs.stdenvNoCC.mkDerivation rec {
        pname = "go-bin";
        version = "1.27.4";
        src =
          let
            platform = with pkgs.stdenvNoCC.hostPlatform.go; "${GOOS}-${GOARCH}";
          in
          pkgs.fetchurl {
            url = "https://go.dev/dl/go${version}.${platform}.tar.gz";
            sha256 =
              {
                darwin-arm64 = "<16 進の sha256>";
                linux-amd64 = "<16 進の sha256>";
              }
              .${platform};
          };
        dontFixup = true;
        installPhase = ''
          mkdir -p $out/share $out/bin
          cp -r . $out/share/go
          ln -s $out/share/go/bin/go $out/share/go/bin/gofmt $out/bin/
        '';
      }
    else
      pkgs.go_1_27;
  ```

- `sha256` は `systems` に並べた全 system ぶんを `https://go.dev/dl/?mode=json&include=all` から取り、`platform` を
  キーにした attrset に書く。`pkgs.lib.fakeHash` で得られるのは手元の system の値だけ。CI の system の値が正しいかは
  PR の CI が devShell を作れたことで確かめる。
- 差し替えた go を `buildGoModule` にも渡すリポジトリは、derivation に
  `passthru = { inherit (pkgs.go_1_XX) GOOS GOARCH CGO_ENABLED; };` と
  `meta = { inherit (pkgs.go_1_XX.meta) platforms; };` を足す。無いと `attribute 'CGO_ENABLED' missing` で評価が落ちる。
- nixpkgs が追いつくと条件が偽になり channel の go に戻るので、その時点で差し替えの分岐ごと消す。修正の内容と
  外す条件はコメントに書く。外す契機には、そのリポジトリで `flake.lock` を上げる実際の経路
  (Dependabot の nix PR、update-flake-lock の workflow、手動の更新) を `.github/dependabot.yml` と
  `.github/workflows` で確かめて書く。
- nixpkgs が配るツール (goreleaser、gosec、go-task など) は、devGo を入れても nixpkgs の go でビルドされたまま残る。
  ビルドに使われた go はバイナリごとに `go version <パス>` で見る
  (go 1.26.10 の devShell でも、nixpkgs の gosec 2.29.0 は go1.26.9 でビルドされていた)。作り直すかは、
  そのツールが修正されたパッケージを実行時に通るかで決める。crypto/x509 の修正なら、TLS で外へ出る goreleaser
  (リリースの公開) は通り、手元のソースを読むだけの gosec は通らない。通らないなら nixpkgs が追いつくのを待つ。
  通るなら `pkgs.buildGoModule.override { go = devGo pkgs; }` を渡して作り直す。パッケージが `.override` で
  受ける引数名は、その nixpkgs の式で確かめる。
- 修正バージョンを下限にするときは、testdata 以外の全モジュールで go.mod の `go` 行をそのバージョンにし、
  `toolchain` 行は置かない (`go` 行と同じになる)。`GOTOOLCHAIN=local` では `toolchain` 行が効かず
  (toolchain を取りに行かない)、`go` 行だけが下限として効く。満たさなければ「症状」の表のエラーで止まる。
  `toolchain` 行だけを上げると、`GOTOOLCHAIN=local` の手元のビルド (devShell の外で PATH の go が
  `go build` する plugin の hook など) が `go` 行を満たす古い go で黙って通る。

## 症状

| 症状 | 原因 | 対処 |
| ---- | ---- | ---- |
| `go: go.mod requires go >= 1.26.1 (running go 1.26.0; GOTOOLCHAIN=local)` | nixpkgs の `go_1_XX` の patch バージョンが go.mod の `go` 行より古い | 新しい `go_1_XX` か nixpkgs の rev を選ぶ。届く rev がまだ無いなら devGo で差し替える (「Go のセキュリティ修正を取り込む」) |
| CI の devShell の準備 (普段は約 30 秒) が数分かかる | devGo が `pkgs.go_1_XX.overrideAttrs` で `src` をソースの tarball に替えていて、binary cache に無い go を job ごとにビルドしている | 配布バイナリの derivation に替える (「Go のセキュリティ修正を取り込む」) |
| `attribute 'CGO_ENABLED' missing` (`buildGoModule` の評価) | 配布バイナリの derivation に `passthru` が無い | `passthru` と `meta.platforms` を足す (「Go のセキュリティ修正を取り込む」) |
| `hash mismatch in fixed-output derivation '…-go-modules.drv'` (`nix build` や CI の nix package ジョブ) | 依存を上げて `go.sum` が変わり、`vendorHash` が古い | `nix build .#default --no-link` の `got:` で `vendorHash` を付け直す |
