//go:build linux

package cmd

import (
	"io"
	"io/fs"

	"github.com/AdguardTeam/golibs/stringutil"
)

func isOpenWrt() (ok bool) {
	// First check common dedicated OpenWrt/ImmortalWrt release files directly
	for _, f := range []string{"etc/openwrt_release", "etc/immortalwrt_release", "etc/openwrt_version"} {
		if _, err := fs.Stat(RootDirFS(), f); err == nil {
			return true
		}
	}

	const etcReleasePattern = "etc/*release*"

	var err error
	ok, err = FileWalker(func(r io.Reader) (_ []string, cont bool, err error) {
		// This use of ReadAll is now safe, because FileWalker's Walk()
		// have limited r.
		var data []byte
		data, err = io.ReadAll(r)
		if err != nil {
			return nil, false, err
		}

		s := string(data)
		isOWrt := stringutil.ContainsFold(s, "openwrt") ||
			stringutil.ContainsFold(s, "immortalwrt") ||
			stringutil.ContainsFold(s, "lede")

		return nil, !isOWrt, nil
	}).Walk(RootDirFS(), etcReleasePattern)

	return err == nil && ok
}
