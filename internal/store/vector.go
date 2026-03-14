package store

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const Float32Size = 4 // bytes per float32

// SerializeVector 将 []float32 序列化为 []byte
func SerializeVector(vector []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, vector)
	return buf.Bytes(), err
}

// DeserializeVector 将 []byte 反序列化为 []float32
func DeserializeVector(data []byte) ([]float32, error) {
	if len(data)%Float32Size != 0 {
		return nil, fmt.Errorf("invalid vector data length: %d", len(data))
	}
	vector := make([]float32, len(data)/Float32Size)
	buf := bytes.NewReader(data)
	err := binary.Read(buf, binary.LittleEndian, &vector)
	return vector, err
}
