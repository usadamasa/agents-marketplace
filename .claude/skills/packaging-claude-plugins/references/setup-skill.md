# setup skill の書き方

plugin に同梱する `skills/setup/SKILL.md` の型｡
実例は [japanese-writer#8](https://github.com/usadamasa/japanese-writer/pull/8) と
[agents-daemon#4](https://github.com/usadamasa/agents-daemon/pull/4)｡

## hook と setup skill の分担

| 担い手 | 持つもの |
| ---- | ---- |
| SessionStart hook | 毎セッション黙って済ませてよい準備 (バイナリのビルド､`${CLAUDE_PLUGIN_DATA}` への配置) |
| setup skill | hook が失敗したときの診断と､利用者の同意が要る操作 |

hook だけで済むなら setup skill は要らない｡逆に hook を置かず setup skill だけにすると､
install や update のたびに利用者が setup を打つことになる｡

## description

- Claude が setup skill を読み込むべき場面として､install 直後､動いていない疑いがあるとき､
  hook やエラーメッセージが setup skill を名指ししたときの 3 つを並べる｡
- 利用者が言いそうな依頼 (「セットアップして」「<部品> をビルドして」) も書く｡
- 拾う症状を具体的に並べる (前提コマンドの欠落､server の停止､state file の未更新)｡

## 本文の構成

1. 実作業は `scripts/setup.sh` のような 1 本の script に寄せ､skill はそれを走らせて出力を読む｡
   SKILL.md からは次の形で呼ぶ｡

   ```sh
   CLAUDE_PLUGIN_DATA="${CLAUDE_PLUGIN_DATA}" "${CLAUDE_PLUGIN_ROOT}/scripts/setup.sh"
   ```

2. 検査は段階ごとに OK / NG を記録する｡順序は「前提コマンドの有無 → 書き込み先の権限 → ビルド → 動いた痕跡」｡
3. script の出力の定型文と､利用者への説明を表で対応づける｡
4. 報告の形を固定する (段階ごとの結果と､利用者に打ってもらうコマンド)｡

## 検査の書き方

- 設定ファイルに書いてあるかではなく､準備したものが期待どおりに動いた痕跡があるかを確かめる｡
  state file なら中身の時刻が新しいか､ビルドした検査器なら既知の違反を入れて拾うかを見る｡
  埋め込んだルールが古いままのバイナリも exit code 0 で動くため､起動の成否だけでは判定できない｡
- ビルドは一時ディレクトリで行い､検証を通してから差し替える｡
  ビルド入力の cksum を記録する stamp ファイルは､差し替えの後に書く｡
  失敗しても前のバイナリが残り､stamp だけが新しい状態にはならない｡
- 前提コマンドが無ければエラーで止める｡警告してスキップしない｡

## 利用者の領域と sandbox

- 利用者が持つファイル (statusline の script､settings.json､既存の symlink) を書き換える前に確認を取る｡
  plugin 自身が張った古い link (旧バージョンのパスを指すもの､宛先が消えたもの) だけは確認なしで張り直してよい｡
- sandbox に書き込みを拒まれたら､同じ引数のまま `!` を前置したコマンドを示して利用者に打ってもらう｡
  sandbox を外す方向の回避は提案しない｡
- daemon を起こす操作も同じく `!` 経由にする｡Bash から起こすと daemon が sandbox の制限を引き継ぐ｡

## README から移すもの

- 手動のビルド手順・link の張り方は README から消し､`/<plugin>:setup` への案内に置き換える｡
- state file の schema のように､コードやテストからも参照される資料は消さずに､
  それを持つ skill の `references/` へ移す｡README と二重に持たない｡
- README に残すのは､概要・前提の一覧・設定の場所・コマンドの一覧｡
