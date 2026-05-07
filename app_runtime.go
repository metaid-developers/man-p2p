package main

import "man-p2p/man"

func runtimeMRC20Enabled(chainSourceEnabled bool) bool {
	if !chainSourceEnabled {
		return false
	}
	return man.Mrc20RuntimeEnabled()
}
