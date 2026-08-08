#include <iostream>

#include "llama.h"

// A model-free smoke test: proves llama.cpp's C API is actually linked and
// callable, without requiring a multi-GB model file to download just to
// scaffold a project. Point this at a real .gguf model (llama_model_load_
// from_file) once you have one - see https://github.com/ggml-org/llama.cpp
// for where to get one and the full inference API.
int main() {
    std::cout << "Hello from cmaker (llama-cpp template)!\n";
    std::cout << llama_print_system_info() << "\n";
    return 0;
}
