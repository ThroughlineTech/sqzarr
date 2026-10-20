//go:build !linux

package transcoder

import "os"

func safeQualificationFile(info os.FileInfo) bool { return false }
