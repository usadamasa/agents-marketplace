# shellcheck shell=bash
# direnv-env.sh が CLAUDE_ENV_FILE の末尾へ写す断片。Bash ツールのシェル (bash か zsh) が実行する。
# 前提: __direnv_env_bash_path に direnv の export 前の PATH、__direnv_env_hook_path に hook の PATH が入っている。
# 実行時の PATH のうち hook の PATH に無い要素 (plugin の bin/) を、direnv の PATH の後ろへ付け直す。
# zsh は変数を語分割しないので、パラメータ展開で 1 つずつ切り出す
# shellcheck disable=SC2154 # 前提の変数は env file の前段が定義する

__direnv_env_rest=$__direnv_env_bash_path:
while [ -n "$__direnv_env_rest" ]; do
  __direnv_env_p=${__direnv_env_rest%%:*}
  __direnv_env_rest=${__direnv_env_rest#*:}
  [ -n "$__direnv_env_p" ] || continue
  case ":$__direnv_env_hook_path:$PATH:" in
    *":$__direnv_env_p:"*) ;;
    *) PATH=$PATH:$__direnv_env_p ;;
  esac
done
export PATH
unset __direnv_env_bash_path __direnv_env_hook_path __direnv_env_rest __direnv_env_p
