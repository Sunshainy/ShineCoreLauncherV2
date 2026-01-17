package launcher

import (
	"crypto/md5"
	"fmt"
)

func OfflineUUID(name string) string {
	hash := md5.Sum([]byte("OfflinePlayer:" + name))
	hash[6] = (hash[6] & 0x0f) | 0x30
	hash[8] = (hash[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", hash[0:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}
