// Package fasttext provides a Go wrapper around the FastText C++ library
// for text classification (should_capture model).
package fasttext

/*
#cgo CXXFLAGS: -std=c++17 -I${SRCDIR}/include -O2
#cgo LDFLAGS: -L${SRCDIR}/lib -lfasttext -lstdc++ -lm -lpthread
#include "fasttext_bridge.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unsafe"
)

// Model holds a loaded FastText model.
type Model struct {
	handle C.fasttext_t
	mu     sync.Mutex // FastText is not thread-safe for predict
}

// Prediction holds the result of a FastText prediction.
type Prediction struct {
	Label      string
	Confidence float64
}

// Load loads a FastText model from the given path (.ftz or .bin).
func Load(path string) (*Model, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	h := C.fasttext_load(cpath)
	if h == nil {
		return nil, fmt.Errorf("fasttext: failed to load model from %s", path)
	}
	return &Model{handle: h}, nil
}

// Close releases the model resources.
func (m *Model) Close() {
	if m.handle != nil {
		C.fasttext_free(m.handle)
		m.handle = nil
	}
}

// PrepareInputCJK converts CJK text to character-separated format required by
// the Chinese should_capture model (trained with character-level tokenization).
// Punctuation is stripped to match training data preprocessing.
func PrepareInputCJK(text string) string {
	var chars []string
	for _, r := range text {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		chars = append(chars, string(r))
	}
	return strings.Join(chars, " ")
}

// PrepareInputEN converts English text to the format required by
// the English should_capture model (word-level tokenization, lowercase).
func PrepareInputEN(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// Predict runs prediction on the given text.
// The text should already be preprocessed with PrepareInput.
func (m *Model) Predict(text string) (Prediction, error) {
	if m.handle == nil {
		return Prediction{}, fmt.Errorf("fasttext: model not loaded")
	}

	ctext := C.CString(text)
	defer C.free(unsafe.Pointer(ctext))

	var labelBuf [64]C.char
	var confidence C.float

	m.mu.Lock()
	ret := C.fasttext_predict(m.handle, ctext, &labelBuf[0], 64, &confidence)
	m.mu.Unlock()

	if ret != 0 {
		return Prediction{}, fmt.Errorf("fasttext: prediction failed")
	}

	return Prediction{
		Label:      C.GoString(&labelBuf[0]),
		Confidence: float64(confidence),
	}, nil
}
