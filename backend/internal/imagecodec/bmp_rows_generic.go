//go:build !amd64 || purego || (linux && cgo)

package imagecodec

func unpackBGR24(dst, src []byte)  { unpackBGR24Scalar(dst, src) }
func unpackBGRX32(dst, src []byte) { unpackBGRX32Scalar(dst, src) }
