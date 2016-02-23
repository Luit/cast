//go:generate protoc --go_out=import_path=pb:. cast_channel.proto

// Package pb implements the Cast v2 protocol buffer spec according to
// Chromium's cast_channel.proto found here:
// https://chromium.googlesource.com/chromium/src.git/+/master/extensions/common/api/cast_channel/cast_channel.proto
// licensed under a BSD-style that can be found here:
// https://chromium.googlesource.com/chromium/src.git/+/master/LICENSE
package pb // import "luit.eu/cast/pb"
