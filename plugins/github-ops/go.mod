module github.com/usadamasa/agents-marketplace/plugins/github-ops

// net/http と cgo のビルド時の脆弱性は go1.27.4 で修正された。go 行を下限にして、
// build-repo-scan.sh による利用者の手元のビルドも修正版に限る
go 1.27.4

require github.com/spf13/cobra v1.10.2

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)
