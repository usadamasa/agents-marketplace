---
name: using-gh-actions-lock
description: >-
  GitHub Actions の依存を gh-actions-lock (`gh actions-lock`、`actions.lock`) で固定するときに使う。
  導入、pinact や SHA pin からの移行、action のバージョン上げ、検証が落ちたときが対象。
---

# using-gh-actions-lock

gh-actions-lock は GitHub 公式の gh 拡張 (`github/gh-actions-lock`)。利用者は workflow に `uses: owner/repo@v1.2.3` と
タグを書き、拡張が対応する commit SHA を `.github/workflows/actions.lock` に記録する。onboard 済みの workflow は、
ランナーが実行時に lockfile と照合する。action が内部で呼ぶ別の action (推移的依存) も同じ lockfile に入る。

lockfile の形式・フラグ・挙動は予告なく変わりうる。
この skill とフラグが食い違ったら `gh actions-lock --help` を正とする。

## コマンドの使い分け

| 目的 | コマンド |
| ---- | ---- |
| lockfile を生成・更新し、workflow を onboard する | `gh actions-lock` |
| 書き込まずに検査する (不正なら exit 1) | `gh actions-lock --no-fix` |
| frozen check。記録済みの全 pin を上流と照合し直す (CI 向け) | `gh actions-lock --verify` |
| オフラインで lockfile の網羅だけ検査する (書き込まない。推奨しない) | `gh actions-lock --verify-local` |
| 動いた `v4` や `main` を今の SHA へ進める | `gh actions-lock --relock` |
| 同じリポジトリの `./` 参照を `$/` へ書き換える | `gh actions-lock --migrate-local-actions` |

`--verify` は lockfile に載っていない workflow と、どの workflow も使っていない lockfile の記録もエラーにする。
`--verify-local` はネットワークと認証が要らない。その代わり、記録が信頼できるかは見ず、lockfile に載っていない
workflow も検査しない (新しい workflow を足して `gh actions-lock` を忘れても通る)。`--verify-local` も `./` 参照は
書き換えず、`local-action` の警告として報告する (詳細は後述の「同じリポジトリの action」節)。

拡張は `gh extension install github/gh-actions-lock` で入れる。引数に workflow のパスを渡すと対象を絞れる。
削除した workflow の記録を lockfile から落とすのは引数なしの実行だけ。

## 導入・移行の手順

1. **作業ツリーを clean にしてから始める。** 手順 3 で元の SHA を git の差分から読むので、無関係な差分を混ぜない。
   移行と同時にバージョンを上げない (手順 3 の照合ができなくなる)。
2. **`uses:` の SHA はそのままにして `gh actions-lock` を実行する。** lockfile が生成され、
   `uses: actions/checkout@<sha>  # v6.0.3` はその commit を指すタグへ書き換わり、末尾のコメントも消える
   (lockfile が無くても、コメントが無くても同じ)。書き換えるのは解決した commit にタグが一致するときだけなので、
   タグが動いていても別の commit を指すタグへ化けない。
3. **lockfile の `commit` が、元の SHA と一致することを確かめる。** 元の SHA は
   `git diff HEAD -- .github/workflows` の削除行 (`-` で始まる `uses:` の行) にある。一致すれば、タグが動いておらず、
   移行の前後で実行されるコードが同じと言える。食い違う action は、pin した後でタグが打ち直されている。
   そのまま進めず、上流のリリースを確認する。
4. **旧ツールの検証を `gh actions-lock --verify` に置き換える。** 設定例は次節にある。
   pinact を使っていたなら、設定ファイル (`.pinact.yaml`) とツールの宣言 (`aqua.yaml` など) も消し、
   `pinact` を拡張子を絞らずに grep して残りを確かめる。
5. **`gh actions-lock --no-fix --json` で findings が空になることを確かめる。** `./` 参照が残っていると
   `local-action` の警告が残り、空にならない。`gh actions-lock --migrate-local-actions` で `$/` へ移す
   (「同じリポジトリの action」節を参照)。

onboard の印は lockfile の `workflows:` に載ることで、workflow への管理コメントの追記は無い。古いバージョンが入れた
`# This workflow is managed by gh actions-lock.` は残っていても害は無く、消しても `--verify` は通る。

`v4` のような部分バージョンは、初回の pin で一番新しい完全なタグ (`v4.2.1`) へ書き換わる。
浮動タグのまま残したいときだけ `--no-narrow` を付ける。
書き換え前の浮動タグが指す commit と lockfile の `commit` が同じかは、
`gh api repos/<owner>/<repo>/commits/<tag> --jq .sha` で 1 件ずつ確かめる。

## CI と pre-commit

```yaml
# .github/workflows/ci.yaml
actions-lock:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v6.0.3
    - name: Install gh-actions-lock
      run: gh extension install github/gh-actions-lock --pin v0.4.0
      env:
        GH_TOKEN: ${{ github.token }}
    - name: actions-lock verify
      run: gh actions-lock --verify
      env:
        GH_TOKEN: ${{ github.token }}
```

```yaml
# .pre-commit-config.yaml
- id: actions-lock
  name: actions-lock verify
  language: system
  entry: gh actions-lock --verify
  pass_filenames: false
  files: ^\.github/workflows/.*\.(ya?ml|lock)$
```

- 拡張の install は public な `github/gh-actions-lock` を取得するだけなので `github.token` で足りる。
  `--verify` は lockfile に載った pin を 1 件ずつ上流と照合しに行くため、依存先に private リポジトリの
  action や composite があると `github.token` では読めない。読めるトークン (リポジトリを横断して読める
  GitHub App token など、そのリポジトリの運用に合わせたもの) を別のステップで作り、verify の `GH_TOKEN` をそちらに差し替える。
- 拡張のバージョンは `--pin` で固定し、`gh extension list` で見たローカルのバージョンと揃える。
- 自分のリポジトリが多いなら、install と検証のステップを共有の composite action にまとめて各リポジトリから `uses:` で呼べる。
  そのときは拡張のバージョンの固定を composite 側が持ち、`--relock` で各リポジトリの記録を進める。
  勤務先など他者のリポジトリからは、個人の composite action を参照しない (個人のアカウント名がそのコードに残る)。
- `@main` で参照しても、`gh actions-lock` が lockfile に commit を記録し、ランナーがその commit で実行する。
  参照先の更新を取り込むときは `gh actions-lock --relock` で記録を進める。
- lockfile そのものはランナーが実行時に検証する。CI の job が拾うのはその手前のずれで、
  lockfile に無い `uses:` や使われていない記録、記録した commit が上流から辿れなくなった pin が対象になる。
  推移的依存の記録漏れを拾うのは v0.3.0 以降 (「推移的依存が lockfile から落ちる」)。
- pre-commit の `files` には lockfile 自身も含める。lockfile だけを手で変えたコミットを拾うため。

## バージョンを上げる

1. workflow の `uses:` のタグを書き換える。
2. `gh actions-lock` を実行する。lockfile に新しいタグの記録が入り、使われなくなった記録が消える。
3. workflow と lockfile を同じコミットに入れる。

片方だけ変えると検証が落ちる。直し方は `gh actions-lock` の再実行。

Dependabot の `github-actions` ecosystem は、`uses:` のタグと lockfile を同じ commit で更新する (2026-10 に確認)。
lockfile があっても、`.github/dependabot.yml` から `github-actions` を外さなくてよい。

Dependabot の `github-actions` は、`directory: /` のとき `.github/workflows/` と root の `action.yml` だけを見る。
composite action (`.github/actions/*`、`.github/composites/*`) の `uses:` も上げるには、`directories` を使う。
`directories` には `/` とそのディレクトリ (`/.github/composites/*` など) を並べる。
並べるのは `uses:` を持つ composite のディレクトリにする。
curl でツールを入れるだけの composite は、Dependabot の更新対象にならない。拾われたかは、
マージ後に Dependabot が実行するジョブの名前にそのディレクトリが出るかで確かめる。
composite 側の bump で Dependabot が lockfile も上げるかは未確認 (2026-10)。lockfile の検証が落ちたら、
その PR のブランチで `gh actions-lock` を実行する。

Renovate が lockfile を更新するかは未確認。Renovate の PR が workflow のタグだけを上げていたら、
その PR のブランチで `gh actions-lock` を実行して lockfile を足す。

### 拡張そのものを上げる

lockfile の schema バージョン (`version:` 行) は拡張のバージョンに追従する。
古い schema の lockfile は上げた拡張でも `--verify` が通るが、初回の書き込みで `version:` 行が変わる。
次の順で 1 つの PR にまとめる。

1. `gh extension upgrade actions-lock` でローカルの拡張を上げ、`gh extension list` でバージョンを確かめる。
2. `gh actions-lock` を 1 回実行する。`Upgraded lockfile schema` と出て、差分は `version:` 行だけになる。
3. 共有 composite が同じバージョンを入れるようになっていれば、`gh actions-lock --relock` で
   composite の記録を今の commit へ進める。composite だけ進めて lockfile の schema を直さないと、次に誰かが書き込んだときに無関係な差分が混ざる。
4. `gh actions-lock --verify` が通ることを確かめる。

## 上流で pin が動いたとき

記録した commit が上流から辿れなくなった pin はエラーとして残る。`--accept-moved` は今の SHA で
上書きするフラグで、タグの打ち直しや乗っ取りを素通しにする。上流で何が起きたかを確認してから使う。

## 同じリポジトリの action (`./` と `$/`)

素の `gh actions-lock` は、同じリポジトリの composite action を指す `uses: ./.github/actions/x` を
書き換えない。`local-action` の警告 (severity warning、`valid: true`) として報告するだけで、workflow も
リポジトリ内の `action.yml` 中の `./` もそのまま残る (v0.4.1 で確認)。

`./` を見つけたら、必ず `gh actions-lock --migrate-local-actions` で `$/` へ書き換える。
workflow と `action.yml` の両方が対象になり、`Migrated N local actions to $/…` と出る。
`$/` は実行中の commit を指すので、lockfile の記録は要らず `@ref` も付けられない。書き換わった
`$/` は `./` に戻さず、そのまま commit する。

- `./` の composite が他の action を `uses:` で呼ぶなら、lockfile が強制される実行で落ちる (error)。`run:`
  だけの composite なら警告のまま動く。`--ignore-local-actions` は、`uses:` を呼ぶ composite の error を
  警告へ下げるフラグ (その推移的依存は lockfile で守られない)。`run:` だけの composite はもとから警告なので、
  付けても findings は空にならない。migrate すればどちらの finding も消える。
- actionlint v1.7.12 は `$/` を `specifying action "$/..." in invalid format because ref is missing` で
  エラーにする。actionlint を使うリポジトリでは、`.github/actionlint.yaml` の `paths.<glob>.ignore` に
  このメッセージの正規表現を足して黙らせる。

  ```yaml
  paths:
    .github/workflows/**/*.{yml,yaml}:
      ignore:
        - 'specifying action "\$/.*" in invalid format because ref is missing'
  ```

## 推移的依存が lockfile から落ちる (大文字小文字の食い違い)

lockfile の更新で、リモートの composite action から呼ばれる推移的依存が、`dependencies:` からも
composite 側の記録の `uses:` からも落ちることがある。v0.2.0 と v0.4.0 で起きることを確かめた。

起きるのは次の条件がそろったとき。

- 既存の lockfile に、依存が小文字の key (`determinatesystems/nix-installer-action@v23`) で載っている
- リモートの composite action の `action.yml` が、その依存を大文字を含む綴り
  (`DeterminateSystems/nix-installer-action@v23`) で参照している

典型は、`$/.github/actions/x` のローカル composite を `owner/repo/actions/x@main` のような
別リポジトリの composite へ移したとき。移す前に workflow 側で記録した小文字の key が残り、
綴りの違う参照と対応づけられずに落ちる。

検出はバージョンで変わる。

- v0.3.0 以降: `--verify` が `incomplete-lockfile` で落とす。
- v0.2.0: `--verify` と `--no-fix --json` のどちらも findings 無しで通る。ランナーが実行時に
  `lockfile missing pin for <owner>/<repo>@<ref>` で job を止める。

直し方もバージョンで変わる。

- v0.3.0 以降: `gh actions-lock --relock` を実行する。
- v0.2.0: `--relock` でも戻らない。lockfile を消してから `gh actions-lock` で一から作り直す。

直した後は、composite 側の記録の `uses:` に、その `action.yml` の `uses:` がすべて載っているかを目で確かめる。
移す前に workflow 側で記録されていた推移的依存の `commit` と同じ SHA になっていれば、
移行の前後で実行されるコードは変わらない。

## pinact の min_age の代わり (cooldown)

pinact の `min_age` (リリースから一定日数が経つまで pin しない) に近い機能として cooldown がある。
設定は専用のフラグではなく、リポジトリの `.github/dependabot.yml` の `github-actions` エントリの
`cooldown.default-days` を読む。無ければユーザーの設定ファイルを読み、どちらも無ければ cooldown は効かない。

- 効くのは、`v4` のような部分バージョンを完全なタグへ narrowing するときの候補選びで、日数に満たない
  リリースは候補から外れる。`default-days: 0` は Dependabot 側の設定として採用されない。
- 非対応の `semver-*-days` と `include` / `exclude` は無視され、`cooldown-config-ignored` の警告になる (ブロックしない)。
- cooldown が無いとき、直近 3 日以内のリリースを pin すると `fresh-tag` の info になる。ブロックはしない。
- バージョンを上げる側 (Dependabot の `cooldown`、Renovate の `minimumReleaseAge`) で待たせる構成も有効。
