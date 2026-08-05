#include <httplib.h>
#include <iostream>

int main() {
    httplib::Server svr;

    svr.Get("/", [](const httplib::Request &, httplib::Response &res) {
        res.set_content("Hello from cmaker (backend template, cpp-httplib)!\n", "text/plain");
    });

    svr.Get("/health", [](const httplib::Request &, httplib::Response &res) {
        res.set_content("ok\n", "text/plain");
    });

    const char *host = "127.0.0.1";
    int port = 8080;
    std::cout << "Listening on http://" << host << ":" << port << " (Ctrl+C to stop)\n";
    svr.listen(host, port);
    return 0;
}
