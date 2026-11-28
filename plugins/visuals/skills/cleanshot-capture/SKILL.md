---
name: cleanshot-capture
description: >-
  macOS のアプリケーションウィンドウや画面を CleanShot X 経由でキャプチャするときに使う。
  sandbox で `open` / `osascript` が止まる環境でも、このコマンドは sandbox の外で動くので撮れる。
  開発中アプリの画面を撮って確認したい、UI の変更前後を見比べたい、
  操作の様子を GIF や MP4 で録画したい、といった依頼が対象。
  「スクショを撮って」「ウィンドウをキャプチャして」「画面を見せて」
  「動きを録画して」「GIF にして」と言われたときにも使う。
---

# CleanShot X によるウィンドウキャプチャ

## Overview

`cleanshot-capture` はエージェントが「いま画面に何が出ているか」を自分で確かめるための CLI。
撮ったファイルは作業ディレクトリへ置く。
visuals に同梱した Go のソースから、SessionStart hook が `~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture` へビルドする。

ウィンドウの撮影 (`window`) は macOS 標準の `screencapture` にウィンドウ ID を渡す。
矩形・全画面・録画は CleanShot X の URL scheme を非対話で叩く。
CleanShot の URL scheme には出力先を指定するパラメータが無いため、
履歴 (`mediaHistory`) の差分から実ファイルを見つけて `--out` へ書き出す。

静止画は `--out` の拡張子が形式の指定になる。
CleanShot が何を書き出すかは本体の設定で決まるため、食い違うときは PNG / JPEG を変換して要求どおりの形式で置く。
指定できるのは `.png` と `.jpg` だけで、他の拡張子はエラーになる。

## When to Use

- 開発中アプリのウィンドウを撮って、実装した UI を目で確認したい
- 画面の一部 (座標指定) や画面全体を撮りたい
- 操作の様子を録画して GIF / MP4 で残したい

**使わない場面:**

- Web ページの中身を撮る → ブラウザを操作するツールで撮る。
  ブラウザのビューポートを直接扱えるほうが速く、スクロールや DOM 状態も制御できる
- 画像ファイルの加工や変換 → CleanShot は撮影専用。切り抜きや変換は `sips` で行う

## コマンドのパス

- `~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture` の字面のまま、クォートせずに実行する。
  sandbox の外で動かす設定 (`sandbox.excludedCommands`) は字面で照合され、`~` は展開されない。
  絶対パスや `$HOME` で書くと照合から外れ、sandbox の中で動いて撮れない
- 1 回の Bash 呼び出しに cleanshot-capture の 1 文だけを書く。パイプ、リダイレクト、`cd` や変数の前置きを付けない
- 設定の形は [visuals の README の「利用者の設定」](../../README.md#利用者の設定)にある

## Quick Reference

```sh
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture list                              # 画面上のウィンドウを前面順に列挙
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture window --app Ghostty              # アプリのウィンドウを撮る
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture window --app Code --title main.go # タイトルで絞り込む
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture area --x 0 --y 33 --width 800 --height 600
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture fullscreen
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture record --app Simulator            # 録画ツールを開いて完成を待つ
~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture rex -s <session> -w <window>      # rex の裏のタブを PNG にする
```

フラグの詳細は `~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture <subcommand> --help`。
出力先の既定は `./tmp/cleanshot-<timestamp>.<ext>` で、`--json` で機械可読な結果が出る。
`--out` は作業ディレクトリ配下しか受け付けない。
このコマンドは sandbox の外で動き OS の書き込み制限が効かないので、コマンド自身が書き込み先を作業ディレクトリに縛る。

撮ったら `Read` ツールでその画像を開いて内容を確認する。パスを報告するだけでは「撮れたつもり」で終わる。

## cleanshot-capture を確かめる

初めて使うときと、呼び出しが `no such file or directory` で落ちたときに、バイナリがあるかを見る。

```sh
test -x ~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture
```

- 無ければ Go が入っているかを利用者に確かめる。SessionStart hook は Go が無いと何もせずに終わる
- hook でのビルドが失敗していれば、原因は `${CLAUDE_PLUGIN_DATA}/build-cleanshot-capture.log` にある

ビルドの置き場 (`${CLAUDE_PLUGIN_DATA}/bin`) は sandbox の中から書けないので、次のコマンドは利用者に実行してもらう。
Claude Code の中で実行するなら、先頭に `!` を付けるよう伝える。

```sh
CLAUDE_PLUGIN_DATA="${CLAUDE_PLUGIN_DATA}" "${CLAUDE_PLUGIN_ROOT}/scripts/build-cleanshot-capture.sh"
```

## 撮影前に対象を特定する

`--app` はアプリ名の完全一致を部分一致より優先する。
同じアプリのウィンドウが複数あるときは `--title` で絞るか `--index` (0 が最前面) で選ぶ。
どれを狙うか分からないときは先に `list` を見る。

`--title` は部分一致で、当たるウィンドウが複数あれば前面のものが撮られる。
あるタイトルが別のタイトルの一部になっているときは両方に当たり、別のウィンドウが撮れても exit 0 で返る。
長いほうを完全な文字列で指定するか `--index` で選ぶ。

`list` と `window --app` は通常レベル (layer 0) のウィンドウしか列挙しない。
Float on Top (`NSFloatingWindowLevel`) にしたウィンドウは開いていても一覧から消えるので、
`area` で座標を指定して撮る。一覧から消えていること自体が、level の変わった証拠になる。
透過 (translucent) の見た目も `window` では背景無しの alpha で写るため、`area` で後ろに何か置いて撮る。

## 撮り方が 2 系統ある

| サブコマンド | 撮り方 | 遮蔽 | 画面ロック |
| ---- | ---- | ---- | ---- |
| `window` | `screencapture` にウィンドウ ID を渡す | 影響しない | ディスプレイが点いていれば影響しない |
| `area` / `fullscreen` / `record` | CleanShot に画面の矩形を撮らせる | 手前のウィンドウが写る | 撮らずに止める |

`window` はウィンドウ自身のバッファを読むため、手前に何が重なっていても中身が写る。
対象を前面に出す必要が無く、ユーザーのフォーカスとクリップボードはどちらも動かない。
**開発中アプリの画面を確かめたいときは常に `window` を使う。**

`record` は CleanShot の録画ツールに矩形を渡す方式なので遮蔽の影響を受ける。
対象アプリを前面に出してから開き、それでも手前に重なるウィンドウが残っていればエラーで止まる。
前面化はアプリ単位でしか効かないため、**同じアプリの別ウィンドウ**が手前に残るケースが起きる。
エラーが出たら、ユーザーに対象ウィンドウを手前に出してもらってから撮り直す。
`--allow-occluded` で強行できるが、写る内容が保証されなくなる。

## 画面ロックとディスプレイスリープの扱い

似て見えるが別の状態で、撮影への効き方も違う。

| 状態 | `window` | `area` / `fullscreen` / `record` |
| ---- | ---- | ---- |
| 画面ロック (ディスプレイは点いている) | 中身が写る | ロック画面が写るので撮らずに止める |
| ディスプレイスリープ | 撮れない (`could not create image`) | 撮れない |

ロック中のディスプレイに出ているのはロック画面なので、`area` / `fullscreen` / `record` はそれを撮る。
撮影自体は成功して確認したつもりになるため、コマンドが撮る前にロック状態を検査してエラーで止める。
**強行するフラグは無い。**

`window` はロックの検査対象外で、ディスプレイが点いていればロック中でも中身が写る。
画面を確かめたいだけなら解除を待たずに済む。

ディスプレイが消えている間はフレームバッファが無く、ウィンドウ ID 指定でも撮れない。
典型は、`caffeinate` でシステムだけ起きていてディスプレイの消えた状態。
どのサブコマンドも撮る前に `pmset -g log` の点灯・消灯の記録を読み、
消えていれば `caffeinate -u` で起こして点くまで待ってから撮る (実測では 1 秒以内に点く)。この検査に 1 秒強かかる。

起こすのは「ユーザーの操作があった」と宣言するだけで、ロックは解除しない。
消灯と同時にロックがかかる設定の端末では、起こした後に `area` / `fullscreen` / `record` はロックの検査で止まる。
`window` はそのまま撮れる。

## rex の裏のタブを撮る

`window --app "Rex Beta"` に写るのは、Rex アプリがいま前面に出しているタブ (ユーザーが見ている画面) になる。
裏のタブや、`rex block call resize` で表示と違う大きさにしたタブは写らない。

そういうタブは `rex` サブコマンドで撮る。rex から画面の大きさと HTML を取り、headless Chrome で PNG に描くので、
ユーザーの画面は切り替わらない。対象は `-b` / `-s` / `-w` で rex と同じに指定し、`-b` か `-w` のどちらかは要る。
写るのはいまの画面の rows 行で、scrollback は入らない。

- Google Chrome (`/Applications/Google Chrome.app`) が要る。sandbox の中では起動できないので、
  「コマンドのパス」の字面で実行する
- 地の色は白で描く。Rex のテーマの色は引き継がない
- 出力先は `.png` だけ

## 録画は半自動

CleanShot の URL scheme には**録画の開始・停止コマンドが無い**。
合成キー入力での自動化には Accessibility 権限が要るため行わない。

`record` は対象ウィンドウの矩形に録画ツールを合わせて開くところまでを担う。
その後はユーザーが Record を押し、終わったら停止する。
コマンドは新しい録画ファイルが履歴に現れるまで待って `--out` へコピーする (既定 5 分、`--timeout` で変更)。

録画を頼むときは、ユーザーに「Record を押して、終わったら停止してください」と明示的に伝える。
黙って待つとタイムアウトする。

**GIF と MP4 のどちらで録るかは、録画ツール UI の設定を引き継ぐ。** API から指定できない。
GIF が要るならユーザーに GIF モードを選んでもらう。

静止画と違い、録画の形式は変換しない。`--out` の拡張子が実際の録画と食い違うとエラーになる。
撮り直しはユーザーの操作を伴うため、エラーには CleanShot 側に残った録画ファイルの場所が出る。
それをそのまま使うか、拡張子を合わせて撮り直す。
形式を決め打ちしたくなければ `--out` を省く (履歴の実ファイルの拡張子を引き継ぐ)。

## 座標系

`list` の出力と `area` の `--x/--y` は**主ディスプレイ左上を原点**とする (CGWindowList と同じ)。
CleanShot 側は左下原点で数えるが、その変換はコマンド内部で行うので呼び出し側は意識しなくてよい。

報告される `width` / `height` は実ピクセル数で、Retina では `list` が出す point の 2 倍になる。

マルチディスプレイでの副ディスプレイの座標は未検証。主ディスプレイでの利用を前提にする。

## Common Mistakes

| 症状 | 原因と対処 |
| ---- | ---- |
| rex の別のタブを撮ったのに、いま見ている画面が写る | `window --app "Rex Beta"` は前面のタブしか写さない。`rex` サブコマンドで撮る |
| 別のウィンドウが写る | `area` / `fullscreen` を使っている。`window` に変える。`window` でも起きるなら `--title` の部分一致が前面の別ウィンドウに当たっている (「撮影前に対象を特定する」) |
| `画面がロックされています` で止まる | `area` / `fullscreen` / `record` は画面ロック中に撮らない。解除を依頼するか、ウィンドウの中身だけでよければ `window` に変える |
| `could not create image` で失敗し、ウィンドウは `list` に出ている | ディスプレイスリープを疑う。エラー本文に `ディスプレイがスリープしています` と出ていればそれで、撮り直せば起こしてから撮る。起こせなければユーザーにキーボードかマウスに触れてもらう |
| `could not create image` で失敗する (上記以外) | 許可の有無で案内が変わるので、エラー本文に従う。ウィンドウ名が読めていれば画面収録の許可はあるので (`kCGWindowName` は許可が無いと空になる)、対象が描画されているかを疑う。名前も読めていなければ、システム設定 > プライバシーとセキュリティ > 画面収録 で許可してアプリを再起動する |
| `-10810` / `procNotFound` で失敗する | sandbox 内から `open` / `osascript` を直接叩いている。「コマンドのパス」の字面で cleanshot-capture を使う |
| `undefined is not an object (evaluating 'raw.map')` で `list` から落ちる | cleanshot-capture が sandbox の中で動き、ウィンドウを列挙できていない。実行したパスが「コマンドのパス」の字面と違うか、[README の「利用者の設定」](../../README.md#利用者の設定)が済んでいない。字面を見比べ、合っていれば設定が済んでいないことを利用者に伝えて止める |
| `静止画の出力先にできない拡張子です` で止まる | `--out` に `.png` `.jpg` 以外を指定している。静止画はそのどちらかで指定する |
| `録画の形式を変換できません` で止まる | `--out` の拡張子が実際の録画と違う。エラーに出る CleanShot 側のファイルを使うか、拡張子を合わせて撮り直す |
| 録画が終わらない | ユーザーが停止していない。停止操作を依頼する |
| 履歴の取り出しに失敗する | CleanShot X が起動していないか履歴が空。一度手動でキャプチャしてもらう |

## 副作用

`window` はフォーカスとクリップボードを動かさず、CleanShot の履歴にも残らない。
副作用はディスプレイがスリープしていれば起こすことだけで、これは全サブコマンドに共通。

`area` / `fullscreen` / `record` は CleanShot 経由なので次が起きる。

- `record` は対象アプリを前面に出すのでユーザーのフォーカスが移る (`--no-raise` で抑止できるが遮蔽しやすくなる)
- `action=copy` を使うためクリップボードが画像で上書きされる。`action=save` と違い、Desktop などの保存先にファイルは増えない
- 撮影物は CleanShot の履歴にも残る
