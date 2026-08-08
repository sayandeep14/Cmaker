#include <iostream>

#include "onnxruntime_c_api.h"

// A model-free smoke test: proves ONNX Runtime's C API is actually linked
// and callable, without requiring a model file just to scaffold a project.
// Point this at a real .onnx model (Ort::Session) once you have one - see
// https://onnxruntime.ai for the full inference API.
int main() {
    const OrtApiBase *api_base = OrtGetApiBase();
    std::cout << "Hello from cmaker (onnxruntime template)!\n";
    std::cout << "ONNX Runtime version: " << api_base->GetVersionString() << "\n";
    return 0;
}
