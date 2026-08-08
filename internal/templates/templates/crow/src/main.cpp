#include <iostream>

#include "crow.h"

int main() {
    crow::SimpleApp app;

    CROW_ROUTE(app, "/")([]() {
        return "Hello from cmaker (crow template)!\n";
    });

    CROW_ROUTE(app, "/health")([]() {
        return "ok\n";
    });

    std::cout << "Listening on http://127.0.0.1:8080 (Ctrl+C to stop)\n";
    app.port(8080).multithreaded().run();
    return 0;
}
