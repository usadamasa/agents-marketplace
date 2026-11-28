# .envrc と aqua の設定

aqua はリポジトリの `aqua.yaml` でツールのバージョンを固定する。aqua の bin はグローバルな PATH に入れず、
リポジトリごとに direnv の `.envrc` で渡す。

## .envrc の型

`use_aqua` は direnv の stdlib には無いので `.envrc` 自身で定義する。

```bash
use_aqua() {
  PATH_add "$(aqua root-dir)/bin"
  watch_file aqua.yaml aqua/policy.yaml aqua/registry.yaml
  aqua install -l
  # ローカルレジストリを使うリポジトリだけ (次節)
  if [ -f aqua/policy.yaml ]; then
    export AQUA_POLICY_CONFIG="$(expand_path aqua/policy.yaml)"
  fi
}

has aqua && use aqua
```

`aqua install -l` は `--only-link` で、shim を張るだけでダウンロードしない。実体はコマンドの初回起動時に入る
(lazy install)。先に全部落としておきたいときだけ `-l` を外す。

- `use_<name>` は、`.envrc` で `use_<name>() { ... }` を定義してから `use <name>` で呼ぶ。direnv の stdlib に
  あるものは `direnv stdlib` の出力で確かめる。stdlib に無い名前を定義せずに呼ぶと、その行だけ失敗する。
- パスを変数に入れるときは `expand_path` で絶対パスにする。子プロセスが別の cwd で読んでもずれない。
- `watch_file` には実在するファイルを並べる。ここに挙げたファイルが変わると再評価される。
- `has <cmd> &&` で前提のコマンドが無い端末でも `.envrc` 全体が落ちないようにする。

## 標準レジストリに無いパッケージ

`aqua.yaml` にローカルレジストリ (`type: local`) を足すと、aqua v2 はそのパッケージを既定で拒否する
(`code 002` `this package isn't allowed`)。許可を policy ファイルに書き、`AQUA_POLICY_CONFIG` で渡す。

- policy は `aqua-policy.yaml` という名前にしない。その名前だと aqua がディレクトリを遡って自動で見つけ、
  `aqua policy allow` するまで aqua 管理下の全コマンドが起動のたびに警告を出す。`aqua/policy.yaml` のように
  自動では探されない場所に置く。
- policy 内のローカルレジストリの `path` は **policy ファイルからの相対**で解決される (aqua.yaml からではない)。
  `aqua/policy.yaml` から `aqua/registry.yaml` を指すなら `path: registry.yaml`。ずれると許可したはずの
  パッケージが code 002 で拒否される。
- `AQUA_POLICY_CONFIG` は絶対パスにする。相対パスだと、サブディレクトリを cwd にして起動するツールの下で
  探す場所がずれる。Taskfile なら `'{{.TASKFILE_DIR}}/aqua/policy.yaml'` と書く。
- policy は実行のたびに aqua-proxy が確かめる。インストール済みでも、`AQUA_POLICY_CONFIG` の無い環境から
  呼べば code 002 で落ちる。
- 素の `aqua i` はこの分だけ code 002 で飛ばす。ローカルレジストリのパッケージは、policy を `env:` で渡している
  task を前置きなしで実行して入れる。

shim は宣言の無いパッケージにも残るので、`command -v <ツール名>` が通っても「使える」とは言えない。
動くかは実際に起動して確かめる。

## shim を別 cwd から起こすツール

PATH で見つけた実行ファイルを、別 cwd の子プロセスで使い回すツールがある。この形は aqua の shim と
食い違う。shim は起動時に自分の cwd を遡って `aqua.yaml` を探すため、`<ツール> --version` のように
リポジトリ内で単発起動する分には通るが、別のツールがその shim を自分の作業ディレクトリ (リポジトリ外の
キャッシュなど) を cwd にして呼び直すと `aqua failed … command is not found (code 004)` で落ちる。

具体例は prek (pre-commit 互換) と uv。prek は uv を「prek 実体の隣 → PATH で `<uv> --version` が通る
最初の uv → prek 管理 → ダウンロード」の順で探す (prek にはパスを名指しする設定・環境変数が無い)。
PATH にあるのが aqua の shim だと、リポジトリを cwd にした `--version` は通るので採用される。その後
prek は `~/.cache/prek/repos/<hash>` を cwd に `uv pip install` を実行し、shim がそこから `aqua.yaml` を
見つけられず 004 になる。

対処は、`.envrc` で uv の実体のディレクトリを PATH へ足し、shim より先に見つかるようにする。
lazy install 後 (`use_aqua` の呼び出しの後) に置く。

```bash
if has aqua; then
  if uv_bin="$(aqua which uv)"; then
    PATH_add "$(dirname "$uv_bin")"
  fi
fi
```

`aqua which` が失敗すると `uv_bin` は空文字になり、`dirname ""` は `.` を返す。`if` で捕まえないと
`PATH_add .` が走り、カレントディレクトリを PATH に入れてしまう。バージョンは `aqua.yaml` で固定したまま、
`use_aqua` の `watch_file aqua.yaml` で PATH も追従する。

採らなかった案は次のとおり。

| 案 | 採らなかった理由 |
| ---- | ---- |
| `.envrc` で `export AQUA_CONFIG=$PWD/aqua.yaml` | 子プロセス全部の aqua の解決を cwd に関係なく塗り替える。`AQUA_CONFIG` は設定の明示切替用で、プロジェクト固定の用途は docs に無い。walk-up と併用されるという報告もある |
| `.envrc` で `AQUA_GLOBAL_CONFIG` にリポジトリの aqua.yaml を指す | 同じ理由。`AQUA_GLOBAL_CONFIG` はユーザー全体のツール用で、docs の例もシェルのプロファイルでの export |
| `PREK_UV_SOURCE` 等、呼び出し側のツールが持つ変数 | shim 自体が `--version` を通すため、呼び出し側の選択ロジックより手前で誤って採用される。効かない |

## direnv を通らない経路

| 経路 | 渡し方 |
| ---- | ---- |
| CI | direnv は動かない。Taskfile の `env:` に書き、CI からもその task を呼ぶ。`aqua install` は不要 (lazy install) |
| Taskfile 経由でない直接呼び出し (スクリプト) | スクリプト自身が冒頭で `export AQUA_POLICY_CONFIG="$(cd "$(dirname "$0")/.." && pwd)/aqua/policy.yaml"` のように自分の位置から組み立てる。呼び出し側の前置きは Claude Code の Bash では使わない (SKILL.md「推奨しない形」) |
| Claude Code の Bash | SessionStart / CwdChanged の direnv hook が `.envrc` を反映する (SKILL.md「仕組み」) |

`aquaproj/aqua-installer` の action は major だけの浮動タグを付けないので、patch まで書く。
