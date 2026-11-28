package ghauthor

// releaseLabels はリリース PR に付くラベル。tagpr は自分が作る PR に "tagpr" を付ける。
var releaseLabels = []string{"tagpr"}

// ConfirmedNotReleasePR は argv の対象がリリース PR でないと確定したときだけ true を返す。
//
// 「リリース PR だと確定したら false」の向きでは書かない。照会に失敗した形やラベルを
// 取れなかった形が true になり、リリース PR が opt-in を通る。
func ConfirmedNotReleasePR(args []string) bool {
	info, ok := authorOnly(args)
	return ok && info.labelsKnown && !info.release
}
