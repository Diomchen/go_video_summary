package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

func PCM16ToWAV(pcm []byte, sampleRate, channels int) ([]byte, error) {
	const bitsPerSample = 16
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	var buf bytes.Buffer
	if _, err := buf.Write([]byte("RIFF")); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(36+len(pcm))); err != nil {
		return nil, err
	}
	if _, err := buf.Write([]byte("WAVE")); err != nil {
		return nil, err
	}
	if _, err := buf.Write([]byte("fmt ")); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(16)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(1)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(channels)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(byteRate)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(blockAlign)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample)); err != nil {
		return nil, err
	}
	if _, err := buf.Write([]byte("data")); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(pcm))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(pcm); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WAVToPCM16(data []byte) ([]byte, int, int, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("invalid wav header")
	}

	offset := 12
	var sampleRate int
	var channels int
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if offset+chunkSize > len(data) {
			return nil, 0, 0, fmt.Errorf("invalid wav chunk size")
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return nil, 0, 0, fmt.Errorf("invalid fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(data[offset : offset+2])
			channels = int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			bitsPerSample := binary.LittleEndian.Uint16(data[offset+14 : offset+16])
			if audioFormat != 1 || bitsPerSample != 16 {
				return nil, 0, 0, fmt.Errorf("unsupported wav format")
			}
		case "data":
			if sampleRate == 0 || channels == 0 {
				return nil, 0, 0, fmt.Errorf("wav fmt chunk missing")
			}
			pcm := make([]byte, chunkSize)
			copy(pcm, data[offset:offset+chunkSize])
			return pcm, sampleRate, channels, nil
		}

		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}

	return nil, 0, 0, fmt.Errorf("wav data chunk missing")
}
