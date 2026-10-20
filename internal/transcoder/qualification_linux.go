package transcoder

import (
	"os"
	"syscall"
)

func safeQualificationFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0
}
