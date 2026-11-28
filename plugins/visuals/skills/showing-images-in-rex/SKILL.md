---
name: showing-images-in-rex
description: 端末多重化ツール rex の中で動くセッションで、生成した PNG (図、スクリーンショット) をユーザーにその場で見せるときに使う。
---

# showing-images-in-rex

rex の block の隣を割り、そこで kitty graphics protocol の APC を流して PNG を描く。Claude Code 本体は画像を
端末に描かず、Bash の stdout に書いたエスケープも端末へ届かないので、画像を出す block を別に作る。

## 経路を決める

| 状況 | 手段 |
| ---- | ---- |
| `printenv REX_SESSION` が値を出し、見せるものが PNG | この skill |
| `REX_SESSION` が空 (rex の外、`claude -p`、Desktop) | `Artifact` で publish する (グローバル CLAUDE.md の経路) |
| SVG、PDF、HTML | PNG に変換してからこの skill (mmdc は `-o <name>.png`、typst は `--format png`)。変換できなければ `Artifact` |
| 相手に渡して共有してほしい | `Artifact` (block の画像はその場限りで、リンクにならない) |

## PNG を隣の block に出す

```sh
rex split --focus=false --keep-open --split=right --label img --cwd "$PWD" -- /bin/bash "${CLAUDE_SKILL_DIR}/show-image.sh" ./tmp/diagram.png
```

`show-image.sh` は実行する (Read しない)。引数は PNG のパス 1 つで、相対なら `--cwd` から解決する。
描き終わると block の最後の行にパスが出る。`rex capture -b img --trim` で見えるのはこの行だけで、
画像は text には出ない。最後の行が `not a file:` か `not a PNG:` ならスクリプトが引数で落ちている。
パスが出ていれば APC は流れており、描けたかはユーザーが目で見る。

- `--keep-open` は必須。無いとスクリプトの終了と同時に block が閉じて画像も消える
- 画像を差し替えるときは、同じ label の block を閉じてから割り直す

```sh
rex block close img
```

- 1 枚ずつ出す。並べて見せたいときは、それぞれ別の label で割る
- 画像は等倍で描かれ、block より大きい分は切れる。mmdc の `-s 2` (倍率 2) や `-w 1200` が大きすぎたら
  `-s 1` か `-w 800` で作り直す

## 使わない形

| 形 | 置き換え |
| ---- | ---- |
| 自分の Bash から `printf '\033_G…'` や `kitten icat` を打つ | Claude Code が stdout を捕捉し、端末へ届かない。`rex split` で別の block に出す |
| `open <png>` | sandbox が Launch Services を止める。`rex split` で出す |
| `SendUserFile` | ターミナルの CLI では何も表示されない |
| `Read` で PNG を読む | 自分が見るだけで、ユーザーには見えない |

## APPENDIX: なぜこの形になるか

**Claude Code 本体は画像を端末に描かない。** Read の結果、`@file`、貼り付けはどれも `[Image]` の
プレースホルダのままで (anthropics/claude-code#79706)、Bash ツールの stdout は本体が読むものなので
エスケープシーケンスは端末まで届かない。本体で描ける唯一の経路は plugin の mod の `Image` 要素
(docs: plugins/mods/reference)。rex でそれが通るかは usadamasa/agents-daemon#226 で確かめる。

**rex は kitty graphics を受ける。** preview.19 (Mac、2026-10-22) と Linux Server preview.5 で unicode placeholder を
含めて対応した。`show-image.sh` は直接転送 (`a=T,f=100`) で、base64 を 4096 byte ずつ `m=1` の chunk に割って
最後だけ `m=0` にする。`q=2` は端末からの応答を抑え、block に `OK` の文字列が残らないようにする。
2026-10-24 に 64 KB の PNG で実測した。

**block は別にする。** `rex split` が作る block は rex server が起こす別のプロセスで、Claude Code の sandbox の
外で動く。スクリプトの stdout がそのまま端末になるので、APC が絵になる。
