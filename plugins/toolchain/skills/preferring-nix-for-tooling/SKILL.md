---
name: preferring-nix-for-tooling
description: >-
  新しい CLI ツールやパッケージを環境に入れるとき、置き場所 (リポジトリの devShell、home-manager、
  OS のパッケージマネージャ) を決めるときに使う。
---

# preferring-nix-for-tooling

ツールは個人環境でも nix で管理する。何かを入れる前にここで行き先を決め、入れてから
「後で nix へ寄せる」という順番にしない。

## 行き先を決める

次の問いを上から順に見て、最初に当たったものに従う。

| 問い | 行き先 |
|---|---|
| `flake.nix` を持つ特定のリポジトリの中だけで使うか (ビルド・lint・テストのツール) | devShell (setting-up-nix-devshell) |
| direnv の外から参照されるか (global hook が `command -v` で探す、シェル設定から呼ぶ、複数リポジトリをまたいで日常的に使う CLI) | home-manager の `home.packages` |
| nixpkgs に同名 attr が無い (名前の一部で検索して確かめた上で)、ログインシェルそのもの、cask のフォント、nixpkgs 版が OS 同梱コマンドを接頭辞なしで隠してしまい接頭辞付き attr も無い | OS のパッケージマネージャ (brew など) に残す |

上の 2 行 (devShell、home-manager) のどちらにも当たらないものだけが 3 行目の対象になる。
「とりあえず OS のパッケージマネージャ」を既定にしない。

## なぜ nix を選ぶか

- バージョンの出所が `flake.lock` (devShell) や home-manager の入力に 1 本化され、`<tool> --version` で
  実際に入ったバージョンを確かめられる。
- 複数端末で同じ宣言ファイルを共有すれば、端末ごとに入れ忘れる・バージョンがずれるということが
  起きない。
- 宣言的なので、何が何のために入っているかは設定ファイルを読めば分かる。OS のパッケージマネージャに
  手で入れたものは、入れた理由も依存関係も後から追いにくい。

## 関連 skill

- devShell を実際に書く・足す・バージョンを上げるときの手順は setting-up-nix-devshell に従う。
- OS のパッケージマネージャ側の書き込み (アンインストールなど) が strict sandbox で
  `Operation not permitted` になるときは sandbox-troubleshooting を見る。
- 既存の OS パッケージマネージャの中身を棚卸しして移行プランを立てる手順は、各リポジトリの
  プロジェクトスコープ skill に従う (このリポジトリの対象外)。
