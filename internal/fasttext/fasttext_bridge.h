// C bridge for FastText C++ library — used by Go CGO
#ifndef FASTTEXT_BRIDGE_H
#define FASTTEXT_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

typedef void* fasttext_t;

// Load a FastText model from file. Returns NULL on error.
fasttext_t fasttext_load(const char* path);

// Free a loaded model.
void fasttext_free(fasttext_t model);

// Predict the top-1 label for the given text.
// label_buf: output buffer for the label string (without __label__ prefix)
// label_buf_size: size of label_buf
// confidence: output pointer for the confidence score
// Returns 0 on success, -1 on error.
int fasttext_predict(fasttext_t model, const char* text,
                     char* label_buf, int label_buf_size,
                     float* confidence);

#ifdef __cplusplus
}
#endif

#endif // FASTTEXT_BRIDGE_H
