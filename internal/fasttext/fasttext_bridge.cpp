#include "fasttext_bridge.h"
#include "include/fasttext.h"
#include <string>
#include <sstream>
#include <cstring>

extern "C" {

fasttext_t fasttext_load(const char* path) {
    auto* ft = new (std::nothrow) fasttext::FastText();
    if (!ft) return nullptr;
    try {
        ft->loadModel(std::string(path));
        return static_cast<fasttext_t>(ft);
    } catch (...) {
        delete ft;
        return nullptr;
    }
}

void fasttext_free(fasttext_t model) {
    if (model) {
        delete static_cast<fasttext::FastText*>(model);
    }
}

int fasttext_predict(fasttext_t model, const char* text,
                     char* label_buf, int label_buf_size,
                     float* confidence) {
    if (!model || !text) return -1;
    auto* ft = static_cast<fasttext::FastText*>(model);
    try {
        std::string input(text);
        std::istringstream iss(input);
        std::vector<std::pair<fasttext::real, std::string>> predictions;
        ft->predictLine(iss, predictions, 1, 0.0f);
        if (predictions.empty()) return -1;

        // Strip __label__ prefix
        std::string label = predictions[0].second;
        const std::string prefix = "__label__";
        if (label.substr(0, prefix.size()) == prefix) {
            label = label.substr(prefix.size());
        }

        if ((int)label.size() >= label_buf_size) return -1;
        std::strncpy(label_buf, label.c_str(), label_buf_size - 1);
        label_buf[label_buf_size - 1] = '\0';
        *confidence = predictions[0].first;
        return 0;
    } catch (...) {
        return -1;
    }
}

} // extern "C"
