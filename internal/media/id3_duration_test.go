package media

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseFLACStreamInfoDuration(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	// 构造最后一个块 (0x80 | 0 = STREAMINFO)，长度 34 字节
	buf.Write([]byte{0x80, 0x00, 0x00, 34})
	streamInfo := make([]byte, 34)
	sampleRate := uint32(44100)
	totalSamples := uint64(44100 * 185) // 185 秒
	streamInfo[10] = byte(sampleRate >> 12)
	streamInfo[11] = byte(sampleRate >> 4)
	streamInfo[12] = byte((sampleRate & 0x0F) << 4)
	streamInfo[13] = byte((totalSamples >> 32) & 0x0F)
	binary.BigEndian.PutUint32(streamInfo[14:18], uint32(totalSamples&0xFFFFFFFF))
	buf.Write(streamInfo)

	tag, err := ParseFLACVorbisComment(&buf)
	if err != nil {
		t.Fatalf("ParseFLACVorbisComment error: %v", err)
	}
	if tag == nil || tag.Duration != 185 {
		t.Fatalf("FLAC tag = %+v, want Duration=185", tag)
	}
}

func TestParseID3v2TLENDuration(t *testing.T) {
	// 构造最小 ID3v2.3 标签，含一帧 TLEN = "243000"（243 秒）
	payload := append([]byte{0x00}, []byte("243000")...) // ISO-8859-1 编码字节 + 文本
	var frame bytes.Buffer
	frame.WriteString("TLEN")
	var fsize [4]byte
	binary.BigEndian.PutUint32(fsize[:], uint32(len(payload)))
	frame.Write(fsize[:])
	frame.Write([]byte{0x00, 0x00})
	frame.Write(payload)

	body := frame.Bytes()
	var tagBuf bytes.Buffer
	tagBuf.WriteString("ID3")
	tagBuf.Write([]byte{0x03, 0x00, 0x00}) // v2.3, flags=0
	n := len(body)
	tagBuf.Write([]byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	})
	tagBuf.Write(body)

	tag, err := ParseID3v2(&tagBuf)
	if err != nil {
		t.Fatalf("ParseID3v2 error: %v", err)
	}
	if tag == nil || tag.Duration != 243 {
		t.Fatalf("ID3 tag = %+v, want Duration=243", tag)
	}
}
